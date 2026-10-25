import { createHash, createPublicKey, randomBytes, randomUUID, verify } from 'node:crypto'
import { DefaultAzureCredential, type TokenCredential } from '@azure/identity'
import { KeyClient } from '@azure/keyvault-keys'
import { Kms } from '@credo-ts/core'

/** The public part of an EC P-256 key, base64url as in a JWK. */
export interface EcPublicParts {
  x: string
  y: string
}

/** What the adapter needs of a Key Vault; azureKeyVault() is the real one. */
export interface KeyVault {
  createEcKey(name: string): Promise<EcPublicParts>
  /** Null when the vault has no such key. */
  getKey(name: string): Promise<EcPublicParts | null>
  listKeyNames(): Promise<string[]>
  /** False when the key was gone already. */
  deleteKey(name: string): Promise<boolean>
  /** ES256 over the SHA-256 digest, r and s side by side as JWS wants them. */
  sign(name: string, digest: Uint8Array): Promise<Uint8Array>
}

interface Held {
  name: string
  publicJwk: Kms.KmsJwkPublic
}

/** Where the holder keys come from: made at every start (and removed with the credential), or one that was provisioned and is only used. */
export interface KeyStrategy {
  create(vault: KeyVault): Promise<Held>
  /** Whether a key id may be one of this wallet's keys. */
  owns(keyId: string): boolean
  /** Deletes the key; false when this wallet does not delete keys or the key was gone already. */
  remove(vault: KeyVault, name: string): Promise<boolean>
  /** Deletes the keys of an earlier wallet; returns how many. */
  deleteLeftovers(vault: KeyVault): Promise<number>
}

export function publicJwkOf(name: string, parts: EcPublicParts): Kms.KmsJwkPublic {
  return { kty: 'EC', crv: 'P-256', x: parts.x, y: parts.y, kid: name } as Kms.KmsJwkPublic
}

/** A new P-256 key at every start, named `<prefix>-<id>`: the wallet finds and deletes its own keys and no other system's. Needs the right to create and delete keys. */
export class KeyPerStart implements KeyStrategy {
  constructor(private readonly prefix: string) {}

  async create(vault: KeyVault): Promise<Held> {
    const name = `${this.prefix}-${randomUUID()}`
    return { name, publicJwk: publicJwkOf(name, await vault.createEcKey(name)) }
  }

  owns(keyId: string): boolean {
    return keyId.startsWith(`${this.prefix}-`) && /^[0-9a-zA-Z-]+$/.test(keyId)
  }

  remove(vault: KeyVault, name: string): Promise<boolean> {
    return vault.deleteKey(name)
  }

  /** The store is empty at every start, so keys under the prefix belong to a wallet that is gone. */
  async deleteLeftovers(vault: KeyVault): Promise<number> {
    const mine = (await vault.listKeyNames()).filter((name) => this.owns(name))
    for (const name of mine) await vault.deleteKey(name)
    return mine.length
  }
}

/** One key that exists already (made by whoever sets up the vault), shared by every credential. The wallet only signs with it and never changes the vault. */
export class ProvisionedKey implements KeyStrategy {
  constructor(private readonly name: string) {}

  async create(vault: KeyVault): Promise<Held> {
    const parts = await vault.getKey(this.name)
    if (!parts) throw new Error(`Key Vault has no key ${this.name}`)
    return { name: this.name, publicJwk: publicJwkOf(this.name, parts) }
  }

  owns(keyId: string): boolean {
    return keyId === this.name
  }

  async remove(): Promise<boolean> {
    return false
  }

  async deleteLeftovers(): Promise<number> {
    return 0
  }
}

/** The real Key Vault. Signs in as the pod when it runs on AKS with Workload Identity, as the developer after `az login`. */
export function azureKeyVault(url: string, credential: TokenCredential = new DefaultAzureCredential()): KeyVault {
  const keys = new KeyClient(url, credential)
  const signers = new Map<string, ReturnType<KeyClient['getCryptographyClient']>>()
  const partsOf = (key: { key?: { x?: Uint8Array; y?: Uint8Array } }): EcPublicParts => {
    const { x, y } = key.key ?? {}
    if (!x || !y) throw new Error('Key Vault returned a key without a public EC part')
    return { x: Buffer.from(x).toString('base64url'), y: Buffer.from(y).toString('base64url') }
  }
  const notFound = (error: unknown) => (error as { statusCode?: number }).statusCode === 404

  return {
    async createEcKey(name) {
      return partsOf(await keys.createEcKey(name, { curve: 'P-256', keyOps: ['sign', 'verify'] }))
    },
    async getKey(name) {
      return keys.getKey(name).then(partsOf, (error: unknown) => {
        if (notFound(error)) return null
        throw error
      })
    },
    async listKeyNames() {
      const names: string[] = []
      for await (const properties of keys.listPropertiesOfKeys()) names.push(properties.name)
      return names
    },
    async deleteKey(name) {
      signers.delete(name)
      return keys.beginDeleteKey(name).then(
        async (poller) => {
          await poller.pollUntilDone()
          return true
        },
        (error: unknown) => {
          if (notFound(error)) return false
          throw error
        },
      )
    },
    async sign(name, digest) {
      const signer = signers.get(name) ?? keys.getCryptographyClient(name)
      signers.set(name, signer)
      return (await signer.sign('ES256', digest)).result
    },
  }
}

/** Keeps the holder keys in Azure Key Vault: only signatures come out. Keys are ES256 (P-256). */
export class AzureKeyVaultKms implements Kms.KeyManagementService {
  static readonly backend = 'azure-key-vault'
  readonly backend = AzureKeyVaultKms.backend
  private held = new Map<string, Held>()

  constructor(
    private readonly vault: KeyVault,
    private readonly strategy: KeyStrategy,
  ) {}

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
    const held = await this.strategy.create(this.vault)
    this.remember(held)
    return { keyId: held.name, publicJwk: held.publicJwk } as never
  }

  deleteLeftovers(): Promise<number> {
    return this.strategy.deleteLeftovers(this.vault)
  }

  async importKey(): Promise<never> {
    throw new Error('A key cannot be imported into Key Vault: it never leaves it.')
  }

  async deleteKey(_context: unknown, options: { keyId: string }) {
    const held = await this.find(options.keyId)
    if (!held) return false
    const deleted = await this.strategy.remove(this.vault, held.name)
    if (deleted) for (const [id, value] of this.held) if (value === held) this.held.delete(id)
    return deleted
  }

  async sign(_context: unknown, options: { keyId: string; data: Uint8Array }) {
    const held = await this.find(options.keyId)
    if (!held) throw new Error(`Key Vault has no key ${options.keyId}`)
    const digest = createHash('sha256').update(options.data).digest()
    return { signature: new Uint8Array(await this.vault.sign(held.name, digest)) } as never
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

  private remember(held: Held) {
    this.held.set(held.name, held)
    // Credo looks a holder key up by an id it derives from the public key in the credential, so that id must find the key too.
    this.held.set(Kms.legacyKeyIdFromPublicJwk(Kms.PublicJwk.fromUnknown(held.publicJwk)), held)
  }

  // A key id is the key's name in Key Vault, so a key made before a restart is found there.
  private async find(keyId: string): Promise<Held | null> {
    const known = this.held.get(keyId)
    if (known) return known
    if (!this.strategy.owns(keyId)) return null
    try {
      const parts = await this.vault.getKey(keyId)
      if (!parts) return null
      const held = { name: keyId, publicJwk: publicJwkOf(keyId, parts) }
      this.held.set(keyId, held)
      return held
    } catch {
      return null
    }
  }
}
