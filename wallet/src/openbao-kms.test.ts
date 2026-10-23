import 'reflect-metadata'
import { execFileSync } from 'node:child_process'
import { AskarModule } from '@credo-ts/askar'
import { Agent, Kms } from '@credo-ts/core'
import { agentDependencies } from '@credo-ts/node'
import { NativeAskar } from '@openwallet-foundation/askar-nodejs'
import { afterAll, beforeAll, describe, expect, it } from 'vitest'
import { OpenBaoKms } from './openbao-kms.js'

const container = 'attesta-wallet-openbao-test'
const url = 'http://127.0.0.1:18200'
const token = 'test-token'

function docker(...args: string[]) {
  return execFileSync('docker', args, { stdio: ['ignore', 'pipe', 'ignore'] }).toString()
}

const hasDocker = (() => {
  try {
    docker('info')
    return true
  } catch {
    return false
  }
})()

async function ready() {
  for (let i = 0; i < 50; i++) {
    try {
      if ((await fetch(`${url}/v1/sys/health`)).ok) return
    } catch {}
    await new Promise((resolve) => setTimeout(resolve, 200))
  }
  throw new Error('OpenBao did not start')
}

describe.skipIf(!hasDocker)('a wallet whose holder key is in OpenBao', () => {
  beforeAll(async () => {
    try {
      docker('rm', '-f', container)
    } catch {}
    docker('run', '-d', '--name', container, '-p', '18200:8200', '-e', `BAO_DEV_ROOT_TOKEN_ID=${token}`, 'openbao/openbao:2.4.4')
    await ready()
    await fetch(`${url}/v1/sys/mounts/transit`, { method: 'POST', headers: { 'X-Vault-Token': token }, body: JSON.stringify({ type: 'transit' }) })
  }, 120_000)

  afterAll(() => {
    try {
      docker('rm', '-f', container)
    } catch {}
  })

  it('presents with a key binding OpenBao signed, and discloses only what was asked', async () => {
    const openbao = new OpenBaoKms({ url, token })
    const agent = new Agent({
      config: {},
      dependencies: agentDependencies,
      modules: {
        askar: new AskarModule({ askar: NativeAskar, store: { id: 'openbao-kms', key: 'a-key', database: { type: 'sqlite', config: { inMemory: true } } } }),
        kms: new Kms.KeyManagementModule({ backends: [openbao], defaultBackend: 'openbao' }),
      },
    })
    await agent.initialize()
    try {
      const issuerKey = await agent.kms.createKey({ type: { kty: 'OKP', crv: 'Ed25519' }, backend: 'askar' })
      const holderKey = await agent.kms.createKey({ type: { kty: 'EC', crv: 'P-256' } })
      expect(holderKey.keyId).toMatch(/^[0-9a-f-]{36}$/)

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

      // The key lives in OpenBao: a second adapter, which has never seen it, finds it by its id.
      expect(await new OpenBaoKms({ url, token }).getPublicKey(undefined, holderKey.keyId)).toMatchObject({ kty: 'EC', crv: 'P-256' })
    } finally {
      await agent.shutdown()
    }
  }, 60_000)
})
