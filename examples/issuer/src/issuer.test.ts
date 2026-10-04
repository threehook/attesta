import { createServer } from 'node:net'
import { acceptCredentialOffer, createWalletAgent, listCredentials, previewCredentialOffer, type WalletAgent } from '@attesta/wallet'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { startIssuer } from './issuer.js'

async function freePort(): Promise<number> {
  const server = createServer()
  await new Promise<void>((resolve) => server.listen(0, resolve))
  const { port } = server.address() as { port: number }
  await new Promise((resolve) => server.close(resolve))
  return port
}

describe('diploma issuer and wallet', () => {
  let issuer: Awaited<ReturnType<typeof startIssuer>>
  let wallet: WalletAgent
  let baseUrl: string

  beforeEach(async () => {
    const port = await freePort()
    baseUrl = `http://localhost:${port}`
    issuer = await startIssuer({ port, publicUrl: baseUrl, storeKey: 'issuer-test-key', allowInsecureHttp: true })
    wallet = await createWalletAgent({ storeId: 'wallet', storeKey: 'wallet-test-key', allowInsecureHttp: true })
  })

  afterEach(async () => {
    await wallet.shutdown()
    await issuer.close()
  })

  it('issues a diploma that the wallet accepts and keeps', async () => {
    const offerUri = await issuer.createOffer({ name: 'Ada Lovelace', email: 'ada@example.com', degree: 'Mathematics', university: 'Trusted University' })

    const accepted = await acceptCredentialOffer(wallet, offerUri)

    expect(accepted).toHaveLength(1)
    expect(accepted[0].type).toBe('Diploma')
    expect(accepted[0].issuer).toMatch(/^did:key:/)
    expect(accepted[0].claims).toMatchObject({ name: 'Ada Lovelace', email: 'ada@example.com', degree: 'Mathematics', university: 'Trusted University' })

    expect(await listCredentials(wallet)).toEqual(accepted)
  })

  it('shows what an offer contains before it is accepted', async () => {
    const offerUri = await issuer.createOffer({ name: 'Ada Lovelace', email: 'ada@example.com', degree: 'Mathematics', university: 'Trusted University' })

    const offer = await previewCredentialOffer(wallet, offerUri)

    expect(offer.issuer).toContain(baseUrl)
    expect(offer.types).toEqual(['Diploma'])
    expect(await listCredentials(wallet)).toEqual([])
  })

  it('creates offers over HTTP', async () => {
    const res = await fetch(`${baseUrl}/offers`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
      body: JSON.stringify({ name: 'Grace Hopper', email: 'grace@example.com', degree: 'Computer Science', university: 'Trusted University' }),
    })
    expect(res.status).toBe(200)
    const { offerUri } = (await res.json()) as { offerUri: string }

    const accepted = await acceptCredentialOffer(wallet, offerUri)
    expect(accepted[0].claims).toMatchObject({ name: 'Grace Hopper' })
  })

  it('rejects an offer request with missing fields', async () => {
    const res = await fetch(`${baseUrl}/offers`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
      body: JSON.stringify({ name: 'Ada', email: 'ada@example.com' }),
    })
    expect(res.status).toBe(400)
  })

  it('rejects an email that is not an address', async () => {
    const res = await fetch(`${baseUrl}/offers`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
      body: JSON.stringify({ name: 'Ada', email: 'not-an-address', degree: 'Mathematics', university: 'Trusted University' }),
    })
    expect(res.status).toBe(400)
  })

  it('does not honour an offer twice', async () => {
    const offerUri = await issuer.createOffer({ name: 'Ada Lovelace', email: 'ada@example.com', degree: 'Mathematics', university: 'Trusted University' })
    await acceptCredentialOffer(wallet, offerUri)

    await expect(acceptCredentialOffer(wallet, offerUri)).rejects.toThrow()
  })

  it('derives the same DID from the same seed', async () => {
    const start = async (seed?: string) => {
      const port = await freePort()
      const started = await startIssuer({ port, publicUrl: `http://localhost:${port}`, storeKey: 'k', allowInsecureHttp: true, seed })
      const did = started.did
      await started.close()
      return did
    }

    const first = await start('a seed')
    expect(await start('a seed')).toBe(first)
    expect(await start('another seed')).not.toBe(first)
    expect(await start()).not.toBe(first)
  })
})
