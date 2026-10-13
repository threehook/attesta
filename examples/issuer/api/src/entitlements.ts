import { randomUUID } from 'node:crypto'
import { readdirSync, readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import pg from 'pg'

export type EntitlementStatus = 'open' | 'issued' | 'revoked'

/** The right of one person to receive a credential, for as long as it is open and not past its end. */
export interface Entitlement {
  id: string
  /** Lowercase; identifies the person to the issuer. */
  email: string
  /** The credential type to issue. */
  type: string
  claims: Record<string, string>
  /** After this moment the entitlement no longer counts; absent means it does not end. */
  validUntil?: Date
  status: EntitlementStatus
  createdAt: Date
  issuedAt?: Date
}

export type NewEntitlement = Pick<Entitlement, 'email' | 'type' | 'claims' | 'validUntil'>

/** Where the issuer keeps entitlements; the rest of the issuer knows nothing about the database behind it. */
export interface EntitlementStore {
  create(input: NewEntitlement): Promise<Entitlement>
  /** The person's entitlements that are open and not past their end, oldest first. */
  findOpenByEmail(email: string): Promise<Entitlement[]>
  /** Marks an open entitlement as issued; false when it is not open, so two requests cannot both issue it. */
  markIssued(id: string): Promise<boolean>
  /** Withdraws an open entitlement; false when it is not open. */
  revoke(id: string): Promise<boolean>
  close(): Promise<void>
}

export class MemoryEntitlementStore implements EntitlementStore {
  private readonly all = new Map<string, Entitlement>()

  async create({ email, type, claims, validUntil }: NewEntitlement): Promise<Entitlement> {
    const entitlement: Entitlement = { id: randomUUID(), email: email.toLowerCase(), type, claims, validUntil, status: 'open', createdAt: new Date() }
    this.all.set(entitlement.id, entitlement)
    return { ...entitlement }
  }

  async findOpenByEmail(email: string): Promise<Entitlement[]> {
    const now = Date.now()
    return [...this.all.values()]
      .filter((e) => e.email === email.toLowerCase() && e.status === 'open' && (!e.validUntil || e.validUntil.getTime() > now))
      .map((e) => ({ ...e }))
  }

  async markIssued(id: string): Promise<boolean> {
    const entitlement = this.all.get(id)
    if (entitlement?.status !== 'open') return false
    entitlement.status = 'issued'
    entitlement.issuedAt = new Date()
    return true
  }

  async revoke(id: string): Promise<boolean> {
    const entitlement = this.all.get(id)
    if (entitlement?.status !== 'open') return false
    entitlement.status = 'revoked'
    return true
  }

  async close() {}
}

export interface DatabaseOptions {
  /** `host` or `host:port`. */
  host: string
  user: string
  password: string
}

const MIGRATIONS = join(dirname(fileURLToPath(import.meta.url)), '..', 'migrations')

interface Row {
  id: string
  email: string
  type: string
  claims: Record<string, string>
  valid_until: Date | null
  status: EntitlementStatus
  created_at: Date
  issued_at: Date | null
}

const toEntitlement = (r: Row): Entitlement => ({
  id: r.id,
  email: r.email,
  type: r.type,
  claims: r.claims,
  validUntil: r.valid_until ?? undefined,
  status: r.status,
  createdAt: r.created_at,
  issuedAt: r.issued_at ?? undefined,
})

export class PostgresEntitlementStore implements EntitlementStore {
  private constructor(private readonly pool: pg.Pool) {}

  /** Connects to the database, creating it and bringing its tables up to date when needed. */
  static async connect(options: DatabaseOptions, database: string): Promise<PostgresEntitlementStore> {
    const [host, port] = options.host.split(':')
    const config = { host, port: port ? Number(port) : 5432, user: options.user, password: options.password }
    const admin = new pg.Client({ ...config, database: 'postgres' })
    await admin.connect()
    try {
      // The lock keeps replicas that start together from creating the database at the same moment.
      await admin.query('SELECT pg_advisory_lock(7010)')
      const exists = await admin.query('SELECT 1 FROM pg_database WHERE datname = $1', [database])
      if (exists.rowCount === 0) await admin.query(`CREATE DATABASE ${pg.escapeIdentifier(database)}`)
    } finally {
      await admin.end()
    }
    const pool = new pg.Pool({ ...config, database })
    try {
      await migrate(pool)
    } catch (error) {
      await pool.end()
      throw error
    }
    return new PostgresEntitlementStore(pool)
  }

  async create({ email, type, claims, validUntil }: NewEntitlement): Promise<Entitlement> {
    const { rows } = await this.pool.query<Row>(
      'INSERT INTO entitlements (id, email, type, claims, valid_until) VALUES ($1, $2, $3, $4, $5) RETURNING *',
      [randomUUID(), email.toLowerCase(), type, JSON.stringify(claims), validUntil ?? null],
    )
    return toEntitlement(rows[0])
  }

  async findOpenByEmail(email: string): Promise<Entitlement[]> {
    const { rows } = await this.pool.query<Row>(
      "SELECT * FROM entitlements WHERE email = $1 AND status = 'open' AND (valid_until IS NULL OR valid_until > now()) ORDER BY created_at",
      [email.toLowerCase()],
    )
    return rows.map(toEntitlement)
  }

  async markIssued(id: string): Promise<boolean> {
    const result = await this.pool.query("UPDATE entitlements SET status = 'issued', issued_at = now() WHERE id = $1 AND status = 'open'", [id])
    return result.rowCount === 1
  }

  async revoke(id: string): Promise<boolean> {
    const result = await this.pool.query("UPDATE entitlements SET status = 'revoked' WHERE id = $1 AND status = 'open'", [id])
    return result.rowCount === 1
  }

  async close() {
    await this.pool.end()
  }
}

// Applies the .sql files in migrations/ that have not run yet, in name order. The lock keeps replicas that start together from racing.
async function migrate(pool: pg.Pool) {
  const client = await pool.connect()
  try {
    await client.query('SELECT pg_advisory_lock(7011)')
    await client.query('CREATE TABLE IF NOT EXISTS schema_migrations (name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())')
    const done = new Set((await client.query<{ name: string }>('SELECT name FROM schema_migrations')).rows.map((r) => r.name))
    for (const name of readdirSync(MIGRATIONS).filter((f) => f.endsWith('.sql')).sort()) {
      if (done.has(name)) continue
      await client.query('BEGIN')
      try {
        await client.query(readFileSync(join(MIGRATIONS, name), 'utf8'))
        await client.query('INSERT INTO schema_migrations (name) VALUES ($1)', [name])
        await client.query('COMMIT')
      } catch (error) {
        await client.query('ROLLBACK')
        throw error
      }
    }
  } finally {
    await client.query('SELECT pg_advisory_unlock(7011)').catch(() => {})
    client.release()
  }
}
