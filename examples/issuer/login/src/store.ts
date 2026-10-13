import { randomUUID } from 'node:crypto'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { type DatabaseOptions, openDatabase } from '@attesta/issuer-pg'
import type pg from 'pg'

export type LoginStatus = 'pending' | 'mailed' | 'confirmed' | 'used'

/** One holder's attempt to log in, from the client's authorization request to the code it exchanges for the login result. */
export interface LoginRequest {
  id: string
  clientId: string
  redirectUri: string
  state?: string
  codeChallenge: string
  scope?: string
  nonce?: string
  status: LoginStatus
  email?: string
  mailsSent: number
  expiresAt: Date
}

export type NewLoginRequest = Pick<LoginRequest, 'clientId' | 'redirectUri' | 'state' | 'codeChallenge' | 'scope' | 'nonce' | 'expiresAt'>

/** Secrets (the link's token, the authorization code) are only ever given to the store as hashes. */
export interface LoginStore {
  create(input: NewLoginRequest): Promise<LoginRequest>
  find(id: string): Promise<LoginRequest | undefined>
  /** Records that a link was mailed to the address, replacing an earlier one; false when the request has run out, was confirmed, or has used up its mails. */
  mailed(id: string, email: string, tokenHash: string, tokenExpiresAt: Date, maxMails: number): Promise<boolean>
  /** Turns a mailed link into an authorization code, once; undefined when the link is unknown, used or expired. */
  confirm(tokenHash: string, codeHash: string, codeExpiresAt: Date): Promise<LoginRequest | undefined>
  /** Takes an authorization code, once; undefined when the code is unknown, used or expired. */
  redeem(codeHash: string): Promise<LoginRequest | undefined>
  /** How many mails went to the address since then. */
  mailsSince(email: string, since: Date): Promise<number>
  recordMail(email: string): Promise<void>
  close(): Promise<void>
}

const none = <T>(v: T | null | undefined) => v ?? undefined

export class MemoryLoginStore implements LoginStore {
  private readonly requests = new Map<string, LoginRequest & { tokenHash?: string; tokenExpiresAt?: Date; codeHash?: string; codeExpiresAt?: Date }>()
  private readonly mails: { email: string; at: number }[] = []

  async create(input: NewLoginRequest) {
    const request = { ...input, id: randomUUID(), status: 'pending' as const, mailsSent: 0 }
    this.requests.set(request.id, request)
    return { ...request }
  }

  async find(id: string) {
    const r = this.requests.get(id)
    return r && { ...r }
  }

  async mailed(id: string, email: string, tokenHash: string, tokenExpiresAt: Date, maxMails: number) {
    const r = this.requests.get(id)
    if (!r || r.expiresAt.getTime() <= Date.now() || (r.status !== 'pending' && r.status !== 'mailed') || r.mailsSent >= maxMails) return false
    Object.assign(r, { status: 'mailed', email: email.toLowerCase(), tokenHash, tokenExpiresAt, mailsSent: r.mailsSent + 1 })
    return true
  }

  async confirm(tokenHash: string, codeHash: string, codeExpiresAt: Date) {
    const r = [...this.requests.values()].find((x) => x.tokenHash === tokenHash)
    if (!r || r.status !== 'mailed' || !r.tokenExpiresAt || r.tokenExpiresAt.getTime() <= Date.now() || r.expiresAt.getTime() <= Date.now()) return undefined
    Object.assign(r, { status: 'confirmed', tokenHash: undefined, tokenExpiresAt: undefined, codeHash, codeExpiresAt })
    return { ...r }
  }

  async redeem(codeHash: string) {
    const r = [...this.requests.values()].find((x) => x.codeHash === codeHash)
    if (!r || r.status !== 'confirmed' || !r.codeExpiresAt || r.codeExpiresAt.getTime() <= Date.now()) return undefined
    r.status = 'used'
    r.codeHash = undefined
    return { ...r }
  }

  async mailsSince(email: string, since: Date) {
    return this.mails.filter((m) => m.email === email.toLowerCase() && m.at >= since.getTime()).length
  }

  async recordMail(email: string) {
    this.mails.push({ email: email.toLowerCase(), at: Date.now() })
  }

  async close() {}
}

const MIGRATIONS = join(dirname(fileURLToPath(import.meta.url)), '..', 'migrations')

interface Row {
  id: string
  client_id: string
  redirect_uri: string
  state: string | null
  code_challenge: string
  scope: string | null
  nonce: string | null
  status: LoginStatus
  email: string | null
  mails_sent: number
  expires_at: Date
}

const toRequest = (r: Row): LoginRequest => ({
  id: r.id,
  clientId: r.client_id,
  redirectUri: r.redirect_uri,
  state: none(r.state),
  codeChallenge: r.code_challenge,
  scope: none(r.scope),
  nonce: none(r.nonce),
  status: r.status,
  email: none(r.email),
  mailsSent: r.mails_sent,
  expiresAt: r.expires_at,
})

export class PostgresLoginStore implements LoginStore {
  private constructor(private readonly pool: pg.Pool) {}

  /** Connects to the database, creating it and bringing its tables up to date when needed. */
  static async connect(options: DatabaseOptions, database: string) {
    return new PostgresLoginStore(await openDatabase(options, database, MIGRATIONS))
  }

  async create(i: NewLoginRequest) {
    const { rows } = await this.pool.query<Row>(
      'INSERT INTO login_requests (id, client_id, redirect_uri, state, code_challenge, scope, nonce, expires_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING *',
      [randomUUID(), i.clientId, i.redirectUri, i.state ?? null, i.codeChallenge, i.scope ?? null, i.nonce ?? null, i.expiresAt],
    )
    return toRequest(rows[0])
  }

  async find(id: string) {
    const { rows } = await this.pool.query<Row>('SELECT * FROM login_requests WHERE id = $1', [id])
    return rows[0] && toRequest(rows[0])
  }

  async mailed(id: string, email: string, tokenHash: string, tokenExpiresAt: Date, maxMails: number) {
    const result = await this.pool.query(
      `UPDATE login_requests SET status = 'mailed', email = $2, token_hash = $3, token_expires_at = $4, mails_sent = mails_sent + 1
       WHERE id = $1 AND expires_at > now() AND status IN ('pending', 'mailed') AND mails_sent < $5`,
      [id, email.toLowerCase(), tokenHash, tokenExpiresAt, maxMails],
    )
    return result.rowCount === 1
  }

  async confirm(tokenHash: string, codeHash: string, codeExpiresAt: Date) {
    const { rows } = await this.pool.query<Row>(
      `UPDATE login_requests SET status = 'confirmed', token_hash = NULL, token_expires_at = NULL, code_hash = $2, code_expires_at = $3
       WHERE token_hash = $1 AND status = 'mailed' AND token_expires_at > now() AND expires_at > now() RETURNING *`,
      [tokenHash, codeHash, codeExpiresAt],
    )
    return rows[0] && toRequest(rows[0])
  }

  async redeem(codeHash: string) {
    const { rows } = await this.pool.query<Row>(
      "UPDATE login_requests SET status = 'used', code_hash = NULL WHERE code_hash = $1 AND status = 'confirmed' AND code_expires_at > now() RETURNING *",
      [codeHash],
    )
    return rows[0] && toRequest(rows[0])
  }

  async mailsSince(email: string, since: Date) {
    const { rows } = await this.pool.query<{ n: string }>('SELECT count(*) AS n FROM login_mails WHERE email = $1 AND sent_at >= $2', [email.toLowerCase(), since])
    return Number(rows[0].n)
  }

  async recordMail(email: string) {
    await this.pool.query('INSERT INTO login_mails (email) VALUES ($1)', [email.toLowerCase()])
  }

  async close() {
    await this.pool.end()
  }
}
