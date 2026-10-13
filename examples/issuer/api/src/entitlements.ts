import { randomUUID } from 'node:crypto'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { openDatabase, type DatabaseOptions } from '@attesta/issuer-pg'
import type pg from 'pg'

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
    return new PostgresEntitlementStore(await openDatabase(options, database, MIGRATIONS), leaseMs)
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
