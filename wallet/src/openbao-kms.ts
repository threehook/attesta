import { createPublicKey, randomBytes, randomUUID, verify } from 'node:crypto'
import { Kms } from '@credo-ts/core'

export interface OpenBaoOptions {
  /** Address of OpenBao, such as http://openbao.attesta.svc:8200. */
  url: string
  /** Token that may create keys and sign under the mount. */
  token: string
  /** Where the Transit engine is mounted. */
  mount?: string
  /** Names this wallet's keys, `<prefix>-<id>`, so it can find and delete its own and no other wallet's. */
  prefix: string
  fetch?: typeof fetch
}

interface Held {
  name: string
  publicJwk: Kms.KmsJwkPublic
}

class OpenBaoError extends Error {
  constructor(readonly status: number, message: string) {
    super(message)
  }
}

/** Keeps the holder keys in OpenBao's Transit engine: only signatures come out. Keys are ES256 (P-256). */
export class OpenBaoKms implements Kms.KeyManagementService {
  static readonly backend = 'openbao'
  readonly backend = OpenBaoKms.backend
  private readonly base: string
  private readonly doFetch: typeof fetch
  private held = new Map<string, Held>()

  constructor(private readonly options: OpenBaoOptions) {
    this.base = `${options.url.replace(/\/+$/, '')}/v1/${options.mount ?? 'transit'}`
    this.doFetch = options.fetch ?? fetch
  }

  isOperationSupported(_context: unknown, operation: Kms.KmsOperation): boolean {
    if (operation.operation === 'randomBytes' || operation.operation === 'deleteKey') return true
    if (operation.operation === 'createKey') return operation.type.kty === 'EC' && operation.type.crv === 'P-256'
    if (operation.operation === 'sign' || operation.operation === 'verify') return operation.algorithm === 'ES256'
    return false
  }

  async getPublicKey(_context: unknown, keyId: string) {
    return (await this.find(keyId))?.publicJwk ?? null
  }

  async createKey() {
    const name = `${this.options.prefix}-${randomUUID()}`
    await this.call('POST', `/keys/${name}`, { type: 'ecdsa-p256' })
    const publicJwk = await this.readPublicJwk(name)
    const held = { name, publicJwk }
    this.held.set(name, held)
    // Credo looks a holder key up by an id it derives from the public key in the credential, so that id must find the key too.
    this.held.set(Kms.legacyKeyIdFromPublicJwk(Kms.PublicJwk.fromUnknown(publicJwk)), held)
    return { keyId: name, publicJwk } as never
  }

  /** Deletes every key under this wallet's prefix. A wallet that starts with an empty store holds none of them, so they belong to a wallet that is gone. */
  async deleteLeftovers(): Promise<number> {
    const { keys } = await this.call<{ keys: string[] }>('GET', '/keys?list=true').catch((error: unknown) => {
      if (error instanceof OpenBaoError && error.status === 404) return { keys: [] }
      throw error
    })
    const mine = keys.filter((name) => name.startsWith(`${this.options.prefix}-`))
    for (const name of mine) {
      await this.call('POST', `/keys/${name}/config`, { deletion_allowed: true })
      await this.call('DELETE', `/keys/${name}`)
    }
    return mine.length
  }

  async importKey(): Promise<never> {
    throw new Error('A key cannot be imported into OpenBao: it never leaves it.')
  }

  async deleteKey(_context: unknown, options: { keyId: string }) {
    const held = await this.find(options.keyId)
    if (!held) return false
    await this.call('POST', `/keys/${held.name}/config`, { deletion_allowed: true })
    await this.call('DELETE', `/keys/${held.name}`)
    for (const [id, value] of this.held) if (value === held) this.held.delete(id)
    return true
  }

  async sign(_context: unknown, options: { keyId: string; data: Uint8Array }) {
    const held = await this.find(options.keyId)
    if (!held) throw new Error(`OpenBao has no key ${options.keyId}`)
    const body = { input: Buffer.from(options.data).toString('base64'), hash_algorithm: 'sha2-256', marshaling_algorithm: 'jws' }
    const { signature } = await this.call<{ signature: string }>('POST', `/sign/${held.name}`, body)
    // The signature looks like vault:v1:<base64url>, r and s side by side as JWS wants them.
    return { signature: new Uint8Array(Buffer.from(signature.slice(signature.lastIndexOf(':') + 1), 'base64url')) } as never
  }

  async verify(_context: unknown, options: { key: { keyId?: string; publicJwk?: Kms.KmsJwkPublic }; data: Uint8Array; signature: Uint8Array }) {
    const publicJwk = options.key.publicJwk ?? (await this.find(options.key.keyId ?? ''))?.publicJwk
    if (!publicJwk) return { verified: false } as never
    const publicKey = createPublicKey({ key: publicJwk as never, format: 'jwk' })
    const verified = verify('sha256', options.data, { key: publicKey, dsaEncoding: 'ieee-p1363' }, options.signature)
    return (verified ? { verified, publicJwk } : { verified }) as never
  }

  async encrypt(): Promise<never> {
    throw new Error('not supported')
  }

  async decrypt(): Promise<never> {
    throw new Error('not supported')
  }

  randomBytes(_context: unknown, options: { length: number }) {
    return randomBytes(options.length)
  }

  // A key id is the key's name in OpenBao, so a key made before a restart is found there.
  private async find(keyId: string): Promise<Held | null> {
    const known = this.held.get(keyId)
    if (known) return known
    if (!keyId.startsWith(`${this.options.prefix}-`) || !/^[0-9a-zA-Z-]+$/.test(keyId)) return null
    try {
      const held = { name: keyId, publicJwk: await this.readPublicJwk(keyId) }
      this.held.set(keyId, held)
      return held
    } catch {
      return null
    }
  }

  private async readPublicJwk(name: string): Promise<Kms.KmsJwkPublic> {
    const { keys, latest_version: latest } = await this.call<{ keys: Record<string, { public_key: string }>; latest_version: number }>('GET', `/keys/${name}`)
    const jwk = createPublicKey(keys[String(latest)]!.public_key).export({ format: 'jwk' })
    return { ...jwk, kid: name } as Kms.KmsJwkPublic
  }

  private async call<T = unknown>(method: string, path: string, body?: unknown): Promise<T> {
    const response = await this.doFetch(`${this.base}${path}`, {
      method,
      headers: { 'X-Vault-Token': this.options.token, 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
    })
    if (!response.ok) throw new OpenBaoError(response.status, `OpenBao answered ${response.status} to ${method} ${path}: ${await response.text()}`)
    const text = await response.text()
    return (text === '' ? {} : (JSON.parse(text) as { data: T }).data) as T
  }
}
