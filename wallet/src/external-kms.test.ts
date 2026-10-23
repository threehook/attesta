import 'reflect-metadata'
import { createPublicKey, generateKeyPairSync, randomBytes, randomUUID, sign, verify, type KeyObject } from 'node:crypto'
import { AskarModule } from '@credo-ts/askar'
import { Agent, Kms, SdJwtVcRecord } from '@credo-ts/core'
import { agentDependencies } from '@credo-ts/node'
import { NativeAskar } from '@openwallet-foundation/askar-nodejs'
import { describe, expect, it } from 'vitest'
import { removeCredential } from './credentials.js'

// Stands in for a remote KMS or a TPM: the private keys stay in here and only signatures come out.
class ExternalKms implements Kms.KeyManagementService {
  readonly backend = 'external'
  signatures = 0
  private keys = new Map<string, { privateKey: KeyObject; publicJwk: Kms.KmsJwkPublic }>()

  isOperationSupported(_context: unknown, operation: Kms.KmsOperation): boolean {
    if (operation.operation === 'randomBytes' || operation.operation === 'deleteKey') return true
    if (operation.operation === 'createKey') return operation.type.kty === 'EC' && operation.type.crv === 'P-256'
    if (operation.operation === 'sign' || operation.operation === 'verify') return operation.algorithm === 'ES256'
    return false
  }

  async getPublicKey(_context: unknown, keyId: string) {
    return this.keys.get(keyId)?.publicJwk ?? null
  }

  async createKey() {
    const { privateKey, publicKey } = generateKeyPairSync('ec', { namedCurve: 'P-256' })
    const keyId = randomUUID()
    const publicJwk = { ...publicKey.export({ format: 'jwk' }), kid: keyId } as Kms.KmsJwkPublic
    this.keys.set(keyId, { privateKey, publicJwk })
    // Credo looks a holder key up by an id it derives from the public key in the credential, so that id must find the key too.
    this.keys.set(Kms.legacyKeyIdFromPublicJwk(Kms.PublicJwk.fromUnknown(publicJwk)), { privateKey, publicJwk })
    return { keyId, publicJwk } as never
  }

  async importKey(): Promise<never> {
    throw new Error('not supported')
  }

  async deleteKey(_context: unknown, options: { keyId: string }) {
    return this.keys.delete(options.keyId)
  }

  async sign(_context: unknown, options: { keyId: string; data: Uint8Array }) {
    const key = this.keys.get(options.keyId)
    if (!key) throw new Error('unknown key')
    this.signatures++
    return { signature: new Uint8Array(sign('sha256', options.data, { key: key.privateKey, dsaEncoding: 'ieee-p1363' })) } as never
  }

  async verify(_context: unknown, options: { key: { keyId?: string; publicJwk?: Kms.KmsJwkPublic }; data: Uint8Array; signature: Uint8Array }) {
    const publicJwk = options.key.publicJwk ?? this.keys.get(options.key.keyId ?? '')?.publicJwk
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
}

describe('a wallet whose holder key is in an external KMS', () => {
  it('presents with a key binding the KMS signed, and discloses only what was asked', async () => {
    const external = new ExternalKms()
    const agent = new Agent({
      config: {},
      dependencies: agentDependencies,
      modules: {
        askar: new AskarModule({ askar: NativeAskar, store: { id: 'external-kms', key: 'a-key', database: { type: 'sqlite', config: { inMemory: true } } } }),
        kms: new Kms.KeyManagementModule({ backends: [external], defaultBackend: 'external' }),
      },
    })
    await agent.initialize()
    try {
      // The issuer's key is in the wallet's own store; the holder's key is made by the external KMS.
      const issuerKey = await agent.kms.createKey({ type: { kty: 'OKP', crv: 'Ed25519' }, backend: 'askar' })
      const holderKey = await agent.kms.createKey({ type: { kty: 'EC', crv: 'P-256' } })
      expect(await external.getPublicKey(undefined, holderKey.keyId)).not.toBeNull()

      const did = await agent.dids.create({ method: 'key', options: { keyId: issuerKey.keyId } } as never)
      const issued = await agent.sdJwtVc.sign({
        payload: { vct: 'Employee', department: 'burgerzaken', email: 'a@corp.test' },
        holder: { method: 'jwk', jwk: Kms.PublicJwk.fromUnknown({ ...holderKey.publicJwk, kid: holderKey.keyId }) },
        issuer: { method: 'did', didUrl: did.didState.didDocument!.verificationMethod![0]!.id },
        disclosureFrame: { _sd: ['department', 'email'] },
      } as never)
      const audience = 'https://verifier.test'
      const presentation = await agent.sdJwtVc.present({
        sdJwtVc: issued.compact,
        presentationFrame: { department: true },
        verifierMetadata: { audience, nonce: 'n-1', issuedAt: Math.floor(Date.now() / 1000) },
      } as never)

      expect(external.signatures).toBe(1)
      const verified = await agent.sdJwtVc.verify({ compactSdJwtVc: presentation, keyBinding: { audience, nonce: 'n-1' } } as never)
      expect(verified.isValid).toBe(true)
      const claims = (verified as unknown as { sdJwtVc: { prettyClaims: Record<string, unknown> } }).sdJwtVc.prettyClaims
      expect(claims.department).toBe('burgerzaken')
      expect(claims.email).toBeUndefined()
    } finally {
      await agent.shutdown()
    }
  })

  it('deletes the holder key together with the credential', async () => {
    const external = new ExternalKms()
    const agent = new Agent({
      config: {},
      dependencies: agentDependencies,
      modules: {
        askar: new AskarModule({ askar: NativeAskar, store: { id: 'external-kms-remove', key: 'a-key', database: { type: 'sqlite', config: { inMemory: true } } } }),
        kms: new Kms.KeyManagementModule({ backends: [external], defaultBackend: 'external' }),
      },
    })
    await agent.initialize()
    try {
      const issuerKey = await agent.kms.createKey({ type: { kty: 'OKP', crv: 'Ed25519' }, backend: 'askar' })
      const holderKey = await agent.kms.createKey({ type: { kty: 'EC', crv: 'P-256' } })
      const did = await agent.dids.create({ method: 'key', options: { keyId: issuerKey.keyId } } as never)
      const issued = await agent.sdJwtVc.sign({
        payload: { vct: 'Employee', email: 'a@corp.test' },
        holder: { method: 'jwk', jwk: Kms.PublicJwk.fromUnknown({ ...holderKey.publicJwk, kid: holderKey.keyId }) },
        issuer: { method: 'did', didUrl: did.didState.didDocument!.verificationMethod![0]!.id },
      } as never)
      const record = new SdJwtVcRecord({ credentialInstances: [{ compactSdJwtVc: issued.compact, kmsKeyId: holderKey.keyId }] })
      await agent.sdJwtVc.store({ record })

      await removeCredential(agent as never, record.id)

      expect(await agent.sdJwtVc.getAll()).toHaveLength(0)
      expect(await external.getPublicKey(undefined, holderKey.keyId)).toBeNull()
    } finally {
      await agent.shutdown()
    }
  })
})
