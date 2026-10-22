import type { AddressInfo } from 'node:net'
import { createServer, type Server } from 'node:http'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { HeldCredential } from './credentials.js'
import type { PresentationRequest } from './presentations.js'
import { createHandler, verifierAllowed, type ServedWallet } from './serve.js'

const workload: HeldCredential = { id: '1', type: 'Workload', issuer: 'did:key:z6', claims: { workload: 'spiffe://corp.test/ns/cicd/sa/agent' } }
const registered = ['redirect_uri:https://attesta.example/']

function request(over: Partial<PresentationRequest> = {}): PresentationRequest {
  return { verifier: 'redirect_uri:https://attesta.example/v1/authorize/requests/r1/response', requested: [], satisfiable: true, resolved: {} as never, ...over }
}

let server: Server | undefined
let base: string
let wallet: { [K in keyof ServedWallet]: ReturnType<typeof vi.fn> }

async function start(verifiers: string[]) {
  const started = createServer(createHandler(wallet as unknown as ServedWallet, verifiers))
  server = started
  await new Promise<void>((resolve) => started.listen(0, '127.0.0.1', resolve))
  base = `http://127.0.0.1:${(started.address() as AddressInfo).port}`
}

const post = (path: string, body: unknown) => fetch(base + path, { method: 'POST', body: typeof body === 'string' ? body : JSON.stringify(body) })

beforeEach(() => {
  wallet = {
    accept: vi.fn(async () => [workload]),
    resolve: vi.fn(async () => request()),
    submit: vi.fn(async () => ({ status: 200 })),
    held: vi.fn(async () => [workload]),
  }
})
afterEach(async () => {
  await new Promise<void>((resolve) => (server ? server.close(() => resolve()) : resolve()))
  server = undefined
})

describe('verifierAllowed', () => {
  it('allows a client id that starts with a registered prefix and nothing when none is registered', () => {
    expect(verifierAllowed('redirect_uri:https://attesta.example/v1/x', registered)).toBe(true)
    expect(verifierAllowed('redirect_uri:https://evil.example/', registered)).toBe(false)
    expect(verifierAllowed('redirect_uri:https://attesta.example/', [])).toBe(false)
    expect(verifierAllowed('anything', [''])).toBe(false)
  })
})

describe('the wallet service', () => {
  it('says how many credentials it holds', async () => {
    await start(registered)
    const res = await fetch(`${base}/healthz`)
    expect(res.status).toBe(200)
    expect(await res.json()).toEqual({ held: 1 })
  })

  it('takes the credential an offer gives and tells the type, not the claims', async () => {
    await start(registered)
    const res = await post('/credentials', { offer: 'openid-credential-offer://?x=1' })
    expect(res.status).toBe(200)
    expect(await res.json()).toEqual({ credentials: [{ type: 'Workload', issuer: 'did:key:z6' }] })
    expect(wallet.accept).toHaveBeenCalledWith('openid-credential-offer://?x=1')
  })

  it('refuses what is not an offer link or not JSON', async () => {
    await start(registered)
    expect((await post('/credentials', { offer: 'https://example.test' })).status).toBe(400)
    expect((await post('/credentials', { offer: 5 })).status).toBe(400)
    expect((await post('/credentials', '{')).status).toBe(400)
    expect(wallet.accept).not.toHaveBeenCalled()
  })

  it('answers a registered verifier by itself', async () => {
    await start(registered)
    const res = await post('/present', { link: 'openid4vp://?x=1' })
    expect(res.status).toBe(200)
    expect(await res.json()).toEqual({ verifier: request().verifier, status: 200 })
    expect(wallet.submit).toHaveBeenCalledTimes(1)
  })

  it('answers no verifier that is not registered, and none when nothing is registered', async () => {
    for (const verifiers of [['redirect_uri:https://other.example/'], []]) {
      await start(verifiers)
      const res = await post('/present', { link: 'openid4vp://?x=1' })
      expect(res.status).toBe(403)
      expect(wallet.submit).not.toHaveBeenCalled()
      await new Promise<void>((resolve) => server!.close(() => resolve()))
    }
    server = undefined
  })

  it('does not answer a request the wallet holds nothing for', async () => {
    wallet.resolve.mockResolvedValue(request({ satisfiable: false }))
    await start(registered)
    expect((await post('/present', { link: 'openid4vp://?x=1' })).status).toBe(409)
    expect(wallet.submit).not.toHaveBeenCalled()
  })

  it('refuses a link that is not a presentation request', async () => {
    await start(registered)
    expect((await post('/present', { link: 'openid-credential-offer://?x=1' })).status).toBe(400)
    expect(wallet.resolve).not.toHaveBeenCalled()
  })

  it('says what went wrong when the wallet fails', async () => {
    wallet.resolve.mockRejectedValue(new Error('the verifier is unreachable'))
    await start(registered)
    const res = await post('/present', { link: 'openid4vp://?x=1' })
    expect(res.status).toBe(502)
    expect(await res.json()).toEqual({ error: 'the verifier is unreachable' })
  })

  it('knows its routes and methods', async () => {
    await start(registered)
    expect((await fetch(`${base}/nothing`)).status).toBe(404)
    expect((await fetch(`${base}/present`)).status).toBe(405)
  })
})
