import { randomUUID } from 'node:crypto'
import { readdirSync, readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import pg from 'pg'

export type EntitlementStatus = 'open' | 'claimed' | 'issued' | 'revoked'

/** How long a claim holds the entitlement while the credential is being made; after that, without it being issued, the entitlement counts as open again. */
export const DEFAULT_LEASE_MS = 120_000

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
  claimedAt?: Date
  issuedAt?: Date
}

export type NewEntitlement = Pick<Entitlement, 'email' | 'type' | 'claims' | 'validUntil'>

/** Where the issuer keeps entitlements; the rest of the issuer knows nothing about the database behind it. */
export interface EntitlementStore {
  create(input: NewEntitlement): Promise<Entitlement>
  find(id: string): Promise<Entitlement | undefined>
  /** The person's entitlements that can still be claimed: open, or claimed too long ago, and not past their end. Oldest first. */
  findOpenByEmail(email: string): Promise<Entitlement[]>
  /** Takes an entitlement that can be claimed, so that no one else can take it while the credential is made; false when it cannot be claimed. */
  claim(id: string): Promise<boolean>
  /** Records that the credential was issued; false unless the entitlement is claimed. */
  markIssued(id: string): Promise<boolean>
  /** Withdraws an entitlement that was not issued yet; false when it was. */
  revoke(id: string): Promise<boolean>
  close(): Promise<void>
}

export class MemoryEntitlementStore implements EntitlementStore {
  private readonly all = new Map<string, Entitlement>()

  constructor(private readonly leaseMs = DEFAULT_LEASE_MS) {}

  private claimable(e: Entitlement, now: number) {
    const open = e.status === 'open' || (e.status === 'claimed' && e.claimedAt !== undefined && e.claimedAt.getTime() + this.leaseMs <= now)
    return open && (!e.validUntil || e.validUntil.getTime() > now)
  }

  async create({ email, type, claims, validUntil }: NewEntitlement): Promise<Entitlement> {
    const entitlement: Entitlement = { id: randomUUID(), email: email.toLowerCase(), type, claims, validUntil, status: 'open', createdAt: new Date() }
    this.all.set(entitlement.id, entitlement)
    return { ...entitlement }
  }

  async find(id: string): Promise<Entitlement | undefined> {
    const entitlement = this.all.get(id)
    return entitlement && { ...entitlement }
  }

  async findOpenByEmail(email: string): Promise<Entitlement[]> {
    const now = Date.now()
    return [...this.all.values()].filter((e) => e.email === email.toLowerCase() && this.claimable(e, now)).map((e) => ({ ...e }))
  }

  async claim(id: string): Promise<boolean> {
    const entitlement = this.all.get(id)
    if (!entitlement || !this.claimable(entitlement, Date.now())) return false
    entitlement.status = 'claimed'
    entitlement.claimedAt = new Date()
    return true
  }

  async markIssued(id: string): Promise<boolean> {
    const entitlement = this.all.get(id)
    if (entitlement?.status !== 'claimed') return false
    entitlement.status = 'issued'
    entitlement.issuedAt = new Date()
    return true
  }

  async revoke(id: string): Promise<boolean> {
    const entitlement = this.all.get(id)
    if (!entitlement || (entitlement.status !== 'open' && entitlement.status !== 'claimed')) return false
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

// Open, or claimed longer ago than the lease (in milliseconds), and not past its end.
const CLAIMABLE = `(status = 'open' OR (status = 'claimed' AND claimed_at + make_interval(secs => $lease::double precision / 1000) <= now()))
  AND (valid_until IS NULL OR valid_until > now())`

const MIGRATIONS = join(dirname(fileURLToPath(import.meta.url)), '..', 'migrations')

interface Row {
  id: string
  email: string
  type: string
  claims: Record<string, string>
  valid_until: Date | null
  status: EntitlementStatus
  created_at: Date
  claimed_at: Date | null
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
  claimedAt: r.claimed_at ?? undefined,
  issuedAt: r.issued_at ?? undefined,
})

export class PostgresEntitlementStore implements EntitlementStore {
  private constructor(
    private readonly pool: pg.Pool,
    private readonly leaseMs: number,
  ) {}

  /** Connects to the database, creating it and bringing its tables up to date when needed. */
  static async connect(options: DatabaseOptions, database: string, leaseMs = DEFAULT_LEASE_MS): Promise<PostgresEntitlementStore> {
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
    return new PostgresEntitlementStore(pool, leaseMs)
  }

  async create({ email, type, claims, validUntil }: NewEntitlement): Promise<Entitlement> {
    const { rows } = await this.pool.query<Row>(
      'INSERT INTO entitlements (id, email, type, claims, valid_until) VALUES ($1, $2, $3, $4, $5) RETURNING *',
      [randomUUID(), email.toLowerCase(), type, JSON.stringify(claims), validUntil ?? null],
    )
    return toEntitlement(rows[0])
  }

  async find(id: string): Promise<Entitlement | undefined> {
    const { rows } = await this.pool.query<Row>('SELECT * FROM entitlements WHERE id = $1', [id])
    return rows[0] && toEntitlement(rows[0])
  }

  async findOpenByEmail(email: string): Promise<Entitlement[]> {
    const { rows } = await this.pool.query<Row>(
      `SELECT * FROM entitlements WHERE email = $1 AND ${CLAIMABLE.replace('$lease', '$2')} ORDER BY created_at`,
      [email.toLowerCase(), this.leaseMs],
    )
    return rows.map(toEntitlement)
  }

  async claim(id: string): Promise<boolean> {
    const result = await this.pool.query(`UPDATE entitlements SET status = 'claimed', claimed_at = now() WHERE id = $1 AND ${CLAIMABLE.replace('$lease', '$2')}`, [id, this.leaseMs])
    return result.rowCount === 1
  }

  async markIssued(id: string): Promise<boolean> {
    const result = await this.pool.query("UPDATE entitlements SET status = 'issued', issued_at = now() WHERE id = $1 AND status = 'claimed'", [id])
    return result.rowCount === 1
  }

  async revoke(id: string): Promise<boolean> {
    const result = await this.pool.query("UPDATE entitlements SET status = 'revoked' WHERE id = $1 AND status IN ('open', 'claimed')", [id])
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
