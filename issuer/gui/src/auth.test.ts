import { createSign, generateKeyPairSync } from 'node:crypto'
import { createServer, type Server } from 'node:http'
import type { AddressInfo } from 'node:net'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { createOidcAuth } from './auth.js'
import { startGui } from './gui.js'

const b64 = (v: Buffer | string) => Buffer.from(v).toString('base64url')

// A stand-in OpenID Connect server: it signs the user in without asking, hands out codes and tokens, and records what it was asked.
let login: Server
let loginUrl: string
let tokenRequests: URLSearchParams[]
let authorizeQuery: URLSearchParams | undefined
let expiresIn: number
let refuseRenewal = false
const { privateKey, publicKey } = generateKeyPairSync('rsa', { modulusLength: 2048 })

const idToken = (nonce: string) => {
  const head = b64(JSON.stringify({ alg: 'RS256', kid: 'k' }))
  const now = Math.floor(Date.now() / 1000)
  const body = b64(JSON.stringify({ iss: loginUrl, aud: 'issuer-gui', sub: 'u', nonce, iat: now, exp: now + 60 }))
  return `${head}.${body}.${createSign('RSA-SHA256').update(`${head}.${body}`).sign(privateKey).toString('base64url')}`
}

let nonce = ''
beforeEach(async () => {
  tokenRequests = []
  authorizeQuery = undefined
  expiresIn = 300
  refuseRenewal = false
  login = createServer((req, res) => {
    const url = new URL(req.url ?? '', loginUrl)
    let raw = ''
    req.on('data', (c) => (raw += c))
    req.on('end', () => {
      res.setHeader('content-type', 'application/json')
      if (url.pathname === '/.well-known/openid-configuration') {
        res.end(JSON.stringify({ issuer: loginUrl, authorization_endpoint: `${loginUrl}/authorize`, token_endpoint: `${loginUrl}/token`, jwks_uri: `${loginUrl}/keys`, end_session_endpoint: `${loginUrl}/logout` }))
      } else if (url.pathname === '/keys') {
        res.end(JSON.stringify({ keys: [{ ...publicKey.export({ format: 'jwk' }), kid: 'k', alg: 'RS256', use: 'sig' }] }))
      } else if (url.pathname === '/authorize') {
        authorizeQuery = url.searchParams
        nonce = url.searchParams.get('nonce') ?? ''
        res.statusCode = 302
        res.setHeader('location', `${url.searchParams.get('redirect_uri')}?code=c1&state=${url.searchParams.get('state')}&iss=${encodeURIComponent(loginUrl)}`)
        res.end()
      } else if (url.pathname === '/token') {
        const form = new URLSearchParams(raw)
        tokenRequests.push(form)
        if (refuseRenewal && form.get('grant_type') === 'refresh_token') {
          res.statusCode = 400
          res.end(JSON.stringify({ error: 'invalid_grant' }))
          return
        }
        res.end(JSON.stringify({ token_type: 'Bearer', access_token: `access-${tokenRequests.length}`, refresh_token: 'refresh', expires_in: expiresIn, id_token: idToken(nonce) }))
      } else {
        res.statusCode = 404
        res.end('{}')
      }
    })
  })
  await new Promise<void>((resolve) => login.listen(0, resolve))
  loginUrl = `http://localhost:${(login.address() as AddressInfo).port}`
})
afterEach(() => new Promise((resolve) => login.close(resolve)))

// A stand-in for the issuer API that records the token it was given.
let api: Server
let apiUrl: string
let bearers: (string | undefined)[]
beforeEach(async () => {
  bearers = []
  api = createServer((req, res) => {
    bearers.push(req.headers.authorization)
    res.setHeader('content-type', 'application/json')
    res.end(JSON.stringify({ issuerName: 'Gemeente Utrecht', did: 'did:key:z6MkTest', departments: ['burgerzaken'], roles: [], diplomas: [] }))
  })
  await new Promise<void>((resolve) => api.listen(0, resolve))
  apiUrl = `http://localhost:${(api.address() as AddressInfo).port}`
})
afterEach(() => new Promise((resolve) => api.close(resolve)))

let gui: Awaited<ReturnType<typeof startGui>>
let base: string
beforeEach(async () => {
  const reserved = createServer()
  await new Promise<void>((resolve) => reserved.listen(0, resolve))
  const port = (reserved.address() as AddressInfo).port
  await new Promise((resolve) => reserved.close(resolve))
  base = `http://localhost:${port}`
  const auth = await createOidcAuth({ issuer: loginUrl, clientId: 'issuer-gui', clientSecret: 'secret', publicUrl: base })
  gui = await startGui({ port, apiUrl, auth })
})
afterEach(() => gui.close())

/** Follows redirects by hand, keeping cookies, until a page answers. */
async function browse(start: string, jar = new Map<string, string>()) {
  let url = start
  for (let i = 0; i < 10; i++) {
    const res = await fetch(url, { redirect: 'manual', headers: { cookie: [...jar].map(([k, v]) => `${k}=${v}`).join('; ') } })
    for (const c of res.headers.getSetCookie()) {
      const [pair] = c.split(';')
      const [k, v] = pair.split('=')
      if (/Max-Age=0/.test(c)) jar.delete(k)
      else jar.set(k, v)
    }
    const next = res.headers.get('location')
    if (res.status < 300 || res.status >= 400 || !next) return { res, jar }
    url = new URL(next, url).href
  }
  throw new Error('too many redirects')
}

describe('signing in', () => {
  it('sends someone who is not signed in to the login server and never shows a page', async () => {
    const res = await fetch(`${base}/employee`, { redirect: 'manual' })

    expect(res.status).toBe(302)
    expect(res.headers.get('location')).toBe('/login')
    expect(bearers).toHaveLength(0)
  })

  it('asks the login server with PKCE, state and nonce', async () => {
    await browse(`${base}/login`)

    expect(authorizeQuery?.get('response_type')).toBe('code')
    expect(authorizeQuery?.get('client_id')).toBe('issuer-gui')
    expect(authorizeQuery?.get('redirect_uri')).toBe(`${base}/callback`)
    expect(authorizeQuery?.get('code_challenge_method')).toBe('S256')
    expect(authorizeQuery?.get('code_challenge')).toBeTruthy()
    expect(authorizeQuery?.get('state')).toBeTruthy()
    expect(authorizeQuery?.get('nonce')).toBeTruthy()
  })

  it('shows the pages after the login and gives the API the access token', async () => {
    const { res } = await browse(`${base}/employee`)

    expect(res.status).toBe(200)
    expect(await res.text()).toContain('Afmelden')
    expect(bearers).toEqual(['Bearer access-1'])
    expect(tokenRequests[0].get('code_verifier')).toBeTruthy()
  })

  it('refuses a return that this browser did not start', async () => {
    const res = await fetch(`${base}/callback?code=c1&state=made-up`, { redirect: 'manual' })

    expect(res.status).toBe(400)
    expect(tokenRequests).toHaveLength(0)
  })

  it('refuses a return with a state that another browser started', async () => {
    const mine = await browse(`${base}/login`)
    const state = authorizeQuery?.get('state')
    const other = await fetch(`${base}/callback?code=c1&state=${state}`, { redirect: 'manual' })

    expect(mine.res.status).toBe(200)
    expect(other.status).toBe(400)
  })

  it('renews an access token that is about to expire', async () => {
    expiresIn = 10
    const { res } = await browse(`${base}/employee`)

    expect(res.status).toBe(200)
    expect(tokenRequests.slice(1).every((r) => r.get('grant_type') === 'refresh_token')).toBe(true)
    expect(tokenRequests.length).toBeGreaterThan(1)
    expect(bearers.at(-1)).toBe(`Bearer access-${tokenRequests.length}`)
  })

  it('sends staff back to the login when the login server refuses to renew', async () => {
    expiresIn = 10
    const { jar } = await browse(`${base}/employee`)
    refuseRenewal = true
    const again = await fetch(`${base}/employee`, { redirect: 'manual', headers: { cookie: [...jar].map(([k, v]) => `${k}=${v}`).join('; ') } })

    expect(again.headers.get('location')).toBe('/login')
  })

  it('signs out at the login server and ends the session', async () => {
    const { jar } = await browse(`${base}/employee`)
    const out = await fetch(`${base}/logout`, { redirect: 'manual', headers: { cookie: [...jar].map(([k, v]) => `${k}=${v}`).join('; ') } })

    expect(out.headers.get('location')).toContain(`${loginUrl}/logout?`)
    expect(out.headers.get('location')).toContain('id_token_hint=')
    const sessionCookie = out.headers.getSetCookie().find((c) => c.startsWith('attesta_issuer_session='))
    expect(sessionCookie).toContain('Max-Age=0')
    const after = await fetch(`${base}/employee`, { redirect: 'manual', headers: { cookie: [...jar].map(([k, v]) => `${k}=${v}`).join('; ') } })
    expect(after.headers.get('location')).toBe('/login')
  })

  it('keeps the logo open for the probes', async () => {
    expect((await fetch(`${base}/logo.svg`)).status).toBe(200)
  })
})
