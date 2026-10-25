import 'reflect-metadata'
import { AskarModule } from '@credo-ts/askar'
import { Agent, Kms } from '@credo-ts/core'
import { agentDependencies } from '@credo-ts/node'
import { p256 } from '@noble/curves/nist.js'
import { NativeAskar } from '@openwallet-foundation/askar-nodejs'
import { describe, expect, it } from 'vitest'
import { AzureKeyVaultKms, KeyPerStart, ProvisionedKey, type KeyVault } from './azure-key-vault-kms.js'

// Stands in for the vault: the private keys stay in here and only signatures come out.
class FakeVault implements KeyVault {
  signatures = 0
  private keys = new Map<string, { secretKey: Uint8Array; x: string; y: string }>()

  async createEcKey(name: string) {
    const { secretKey } = p256.keygen()
    // Uncompressed point: 0x04, x, y.
    const point = p256.getPublicKey(secretKey, false)
    const parts = { x: Buffer.from(point.slice(1, 33)).toString('base64url'), y: Buffer.from(point.slice(33)).toString('base64url') }
    this.keys.set(name, { secretKey, ...parts })
    return parts
  }

  async getKey(name: string) {
    return this.keys.get(name) ?? null
  }

  async listKeyNames() {
    return [...this.keys.keys()]
  }

  async deleteKey(name: string) {
    return this.keys.delete(name)
  }

  async sign(name: string, digest: Uint8Array) {
    const key = this.keys.get(name)
    if (!key) throw new Error(`no key ${name}`)
    this.signatures++
    // Like the vault, it signs the digest itself and hashes nothing again; the result is r and s side by side.
    return p256.sign(digest, key.secretKey, { prehash: false })
  }
}

async function walletWith(kms: AzureKeyVaultKms) {
  const agent = new Agent({
    config: {},
    dependencies: agentDependencies,
    modules: {
      askar: new AskarModule({ askar: NativeAskar, store: { id: 'azure-kms', key: 'a-key', database: { type: 'sqlite', config: { inMemory: true } } } }),
      kms: new Kms.KeyManagementModule({ backends: [kms], defaultBackend: 'azure-key-vault' }),
    },
  })
  await agent.initialize()
  return agent
}

describe('a wallet whose holder key is in Key Vault', () => {
  it('presents with a key binding the vault signed, and discloses only what was asked', async () => {
    const vault = new FakeVault()
    const agent = await walletWith(new AzureKeyVaultKms(vault, new KeyPerStart('agent-a')))
    try {
      const issuerKey = await agent.kms.createKey({ type: { kty: 'OKP', crv: 'Ed25519' }, backend: 'askar' })
      const holderKey = await agent.kms.createKey({ type: { kty: 'EC', crv: 'P-256' } })
      expect(holderKey.keyId).toMatch(/^agent-a-[0-9a-f-]{36}$/)

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

      const verified = await agent.sdJwtVc.verify({ compactSdJwtVc: presentation, keyBinding: { audience, nonce: 'n-1' } } as never)
      expect(verified.isValid).toBe(true)
      const claims = (verified as unknown as { sdJwtVc: { prettyClaims: Record<string, unknown> } }).sdJwtVc.prettyClaims
      expect(claims.department).toBe('burgerzaken')
      expect(claims.email).toBeUndefined()
      expect(vault.signatures).toBe(1)

      // The key lives in the vault: a second adapter, which has never seen it, finds it by its id.
      const other = new AzureKeyVaultKms(vault, new KeyPerStart('agent-a'))
      expect(await other.getPublicKey(undefined, holderKey.keyId)).toMatchObject({ kty: 'EC', crv: 'P-256' })
    } finally {
      await agent.shutdown()
    }
  }, 60_000)

  it('deletes the keys under its own prefix and leaves those of another system', async () => {
    const vault = new FakeVault()
    const gone = new AzureKeyVaultKms(vault, new KeyPerStart('agent-gone'))
    const other = new AzureKeyVaultKms(vault, new KeyPerStart('agent-other'))
    const left = ((await gone.createKey()) as { keyId: string }).keyId
    await gone.createKey()
    const kept = ((await other.createKey()) as { keyId: string }).keyId

    expect(await new AzureKeyVaultKms(vault, new KeyPerStart('agent-gone')).deleteLeftovers()).toBe(2)

    expect(await new AzureKeyVaultKms(vault, new KeyPerStart('agent-gone')).getPublicKey(undefined, left)).toBeNull()
    expect(await new AzureKeyVaultKms(vault, new KeyPerStart('agent-other')).getPublicKey(undefined, kept)).not.toBeNull()
    expect(await new AzureKeyVaultKms(vault, new KeyPerStart('agent-gone')).getPublicKey(undefined, kept)).toBeNull()
  })

  it('treats a key that is gone already as deleted', async () => {
    const vault = new FakeVault()
    const kms = new AzureKeyVaultKms(vault, new KeyPerStart('agent-twice'))
    const { keyId } = (await kms.createKey()) as { keyId: string }
    await new AzureKeyVaultKms(vault, new KeyPerStart('agent-twice')).deleteLeftovers()

    expect(await kms.deleteKey(undefined, { keyId })).toBe(false)
  })

  it('uses the provisioned key for every credential and never changes the vault', async () => {
    const vault = new FakeVault()
    await vault.createEcKey('cicd-agent')
    const kms = new AzureKeyVaultKms(vault, new ProvisionedKey('cicd-agent'))

    const first = (await kms.createKey()) as { keyId: string }
    const second = (await kms.createKey()) as { keyId: string }

    expect(first.keyId).toBe('cicd-agent')
    expect(second.keyId).toBe('cicd-agent')
    expect(await kms.deleteKey(undefined, { keyId: 'cicd-agent' })).toBe(false)
    expect(await kms.deleteLeftovers()).toBe(0)
    expect(await vault.getKey('cicd-agent')).not.toBeNull()
    expect(await kms.getPublicKey(undefined, 'another-key')).toBeNull()
  })

  it('signs with the provisioned key so that the signature verifies', async () => {
    const vault = new FakeVault()
    await vault.createEcKey('cicd-agent')
    const kms = new AzureKeyVaultKms(vault, new ProvisionedKey('cicd-agent'))
    const data = new TextEncoder().encode('to be signed')

    const { signature } = (await kms.sign(undefined, { keyId: 'cicd-agent', data })) as unknown as { signature: Uint8Array }

    const publicJwk = (await kms.getPublicKey(undefined, 'cicd-agent'))!
    expect(((await kms.verify(undefined, { key: { publicJwk }, data, signature })) as { verified: boolean }).verified).toBe(true)
    expect(((await kms.verify(undefined, { key: { publicJwk }, data: new TextEncoder().encode('other'), signature })) as { verified: boolean }).verified).toBe(
      false,
    )
  })

  it('refuses to start when the provisioned key is not in the vault', async () => {
    const kms = new AzureKeyVaultKms(new FakeVault(), new ProvisionedKey('missing'))

    await expect(kms.createKey()).rejects.toThrow('Key Vault has no key missing')
  })
})
