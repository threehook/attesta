import { createHash, createPublicKey, type JsonWebKeyInput, randomBytes, verify } from 'node:crypto'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { type LoginOptions, startLogin } from './login.js'
import type { Mail, Mailer } from './mail.js'
import { MemoryLoginStore } from './store.js'

const REDIRECT = 'attesta-wallet://callback'
const CLIENT = { clientId: 'attesta-issuer', clientSecret: 'secret', redirectUris: [REDIRECT, 'https://issuer.example/cb'] }

class CatchingMailer implements Mailer {
  mails: Mail[] = []
  async send(mail: Mail) {
    this.mails.push(mail)
  }
}

describe('login server', () => {
  let mailer: CatchingMailer
  let server: Awaited<ReturnType<typeof startLogin>> | undefined
  let base: string
  let issuer: string

  async function start(extra: Partial<LoginOptions> = {}) {
    server = await startLogin({
      port: 0,
      issuer: 'http://login.internal/login',
      publicUrl: 'https://issuer.example/login',
      basePath: '/login',
      client: CLIENT,
      store: new MemoryLoginStore(),
      mailer,
      seed: 'test seed',
      issuerName: 'Gemeente Utrecht',
      ...extra,
    })
    base = `http://localhost:${server.port}/login`
    issuer = 'http://login.internal/login'
  }
  beforeEach(() => {
    mailer = new CatchingMailer()
  })
  afterEach(async () => {
    await server?.close()
    server = undefined
  })

  const pkce = () => {
    const verifier = randomBytes(32).toString('base64url')
    return { verifier, challenge: createHash('sha256').update(verifier).digest('base64url') }
  }
  const authorize = (challenge: string, extra: Record<string, string> = {}) =>
    fetch(
      `${base}/authorize?${new URLSearchParams({ response_type: 'code', client_id: CLIENT.clientId, redirect_uri: REDIRECT, state: 'st', scope: 'openid email', code_challenge: challenge, code_challenge_method: 'S256', ...extra })}`,
    )
  const form = (fields: Record<string, string>) => ({ method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, body: new URLSearchParams(fields) })
  const requestId = async (res: Response) => /name="request" value="([^"]+)"/.exec(await res.text())![1]
  const linkIn = (mail: Mail) => /(https?:\/\/\S+\/confirm\?token=\S+)/.exec(mail.text)![1].replace('https://issuer.example', `http://localhost:${server!.port}`)
  const tokenIn = (mail: Mail) => new URL(linkIn(mail)).searchParams.get('token')!
  const redirectIn = (html: string) => new URL(/<a class="button" href="([^"]+)"/.exec(html)![1].replaceAll('&#38;', '&'))

  /** The holder's whole way: the email page, the mail, the link, the button. Returns what the client is sent back with. */
  async function login(email: string, challenge: string) {
    const id = await requestId(await authorize(challenge))
    await fetch(`${base}/email`, form({ request: id, email }))
    const mail = mailer.mails.at(-1)!
    const done = await fetch(`${base}/confirm`, form({ token: tokenIn(mail) }))
    return redirectIn(await done.text())
  }
  const exchange = (code: string, verifier: string, extra: Record<string, string> = {}) =>
    fetch(`${base}/token`, form({ grant_type: 'authorization_code', code, code_verifier: verifier, redirect_uri: REDIRECT, client_id: CLIENT.clientId, client_secret: CLIENT.clientSecret, ...extra }))

  it('logs a holder in with the link from the mail, and gives the client a signed result with their email', async () => {
    await start()
    const { verifier, challenge } = pkce()

    const back = await login('Jerry@Example.com', challenge)

    expect(`${back.protocol}//${back.host}${back.pathname}`).toBe('attesta-wallet://callback')
    expect(back.searchParams.get('state')).toBe('st')
    expect(back.searchParams.get('iss')).toBe(issuer)
    const res = await exchange(back.searchParams.get('code')!, verifier)
    expect(res.status).toBe(200)
    const body = (await res.json()) as { id_token: string; token_type: string; scope: string }
    expect(body).toMatchObject({ token_type: 'Bearer', scope: 'openid email' })
    const [head, claims, signature] = body.id_token.split('.')
    const jwks = (await (await fetch(`${base}/jwks`)).json()) as { keys: (JsonWebKeyInput['key'] & { kid: string })[] }
    expect(JSON.parse(Buffer.from(head, 'base64url').toString())).toMatchObject({ alg: 'EdDSA', kid: jwks.keys[0].kid })
    expect(verify(null, Buffer.from(`${head}.${claims}`), createPublicKey({ key: jwks.keys[0], format: 'jwk' }), Buffer.from(signature, 'base64url'))).toBe(true)
    expect(JSON.parse(Buffer.from(claims, 'base64url').toString())).toMatchObject({ iss: issuer, aud: CLIENT.clientId, email: 'jerry@example.com', email_verified: true })
  })

  it('mails the link to the address and names the issuer', async () => {
    await start()
    await login('jerry@example.com', pkce().challenge)

    expect(mailer.mails).toHaveLength(1)
    expect(mailer.mails[0]).toMatchObject({ to: 'jerry@example.com', subject: 'Aanmelden bij Gemeente Utrecht' })
    expect(mailer.mails[0].text).toContain('https://issuer.example/login/confirm?token=')
  })

  it('describes itself at the discovery address, with the browser address for the holder and its own address for the client', async () => {
    await start()

    const meta = await (await fetch(`${base}/.well-known/openid-configuration`)).json()

    expect(meta).toMatchObject({
      issuer,
      authorization_endpoint: 'https://issuer.example/login/authorize',
      token_endpoint: `${issuer}/token`,
      jwks_uri: `${issuer}/jwks`,
      code_challenge_methods_supported: ['S256'],
    })
  })

  it('does not finish a login when the link is only opened', async () => {
    await start()
    const id = await requestId(await authorize(pkce().challenge))
    await fetch(`${base}/email`, form({ request: id, email: 'a@example.com' }))
    const link = linkIn(mailer.mails[0])

    const opened = await fetch(link)

    expect(await opened.text()).toContain('<button type="submit">Aanmelden</button>')
    expect((await fetch(`${base}/confirm`, form({ token: tokenIn(mailer.mails[0]) }))).status).toBe(200)
  })

  it('accepts a link once', async () => {
    await start()
    await login('a@example.com', pkce().challenge)

    const again = await fetch(`${base}/confirm`, form({ token: tokenIn(mailer.mails[0]) }))

    expect(again.status).toBe(400)
  })

  it('refuses a link that ran out', async () => {
    await start({ tokenMs: 50 })
    const id = await requestId(await authorize(pkce().challenge))
    await fetch(`${base}/email`, form({ request: id, email: 'a@example.com' }))
    await new Promise((resolve) => setTimeout(resolve, 100))

    expect((await fetch(`${base}/confirm`, form({ token: tokenIn(mailer.mails[0]) }))).status).toBe(400)
  })

  it.each([
    ['an unknown client', { client_id: 'someone-else' }],
    ['a return address that is not registered', { redirect_uri: 'https://evil.example/cb' }],
    ['no PKCE', { code_challenge_method: 'plain' }],
    ['another response type', { response_type: 'token' }],
  ])('refuses an authorization request with %s, and sends nothing to that address', async (_name, extra) => {
    await start()

    const res = await authorize(pkce().challenge, extra)

    expect(res.status).toBe(400)
    expect(res.headers.get('location')).toBeNull()
  })

  it('refuses something that is not an email address, and lets the holder try again', async () => {
    await start()
    const id = await requestId(await authorize(pkce().challenge))

    const res = await fetch(`${base}/email`, form({ request: id, email: 'nope' }))

    expect(res.status).toBe(400)
    expect(mailer.mails).toEqual([])
    expect(await requestId(res)).toBe(id)
  })

  it('exchanges a code once', async () => {
    await start()
    const { verifier, challenge } = pkce()
    const code = (await login('a@example.com', challenge)).searchParams.get('code')!

    expect((await exchange(code, verifier)).status).toBe(200)
    expect((await exchange(code, verifier)).status).toBe(400)
  })

  it('refuses a code with the wrong verifier, return address or client secret', async () => {
    await start()
    const { verifier, challenge } = pkce()
    const code = async () => (await login('a@example.com', challenge)).searchParams.get('code')!

    expect((await exchange(await code(), pkce().verifier)).status).toBe(400)
    expect((await exchange(await code(), verifier, { redirect_uri: 'https://issuer.example/cb' })).status).toBe(400)
    expect((await exchange(await code(), verifier, { client_secret: 'wrong' })).status).toBe(401)
  })

  it('accepts the client secret in a Basic header too', async () => {
    await start()
    const { verifier, challenge } = pkce()
    const code = (await login('a@example.com', challenge)).searchParams.get('code')!

    const res = await fetch(`${base}/token`, {
      ...form({ grant_type: 'authorization_code', code, code_verifier: verifier, redirect_uri: REDIRECT }),
      headers: { 'Content-Type': 'application/x-www-form-urlencoded', Authorization: `Basic ${Buffer.from(`${CLIENT.clientId}:${CLIENT.clientSecret}`).toString('base64')}` },
    })

    expect(res.status).toBe(200)
  })

  it('refuses a code that ran out', async () => {
    await start({ codeMs: 50 })
    const { verifier, challenge } = pkce()
    const code = (await login('a@example.com', challenge)).searchParams.get('code')!
    await new Promise((resolve) => setTimeout(resolve, 100))

    expect((await exchange(code, verifier)).status).toBe(400)
  })

  it('sends only so many mails for one request, and says the same each time', async () => {
    await start({ maxMailsPerRequest: 2 })
    const id = await requestId(await authorize(pkce().challenge))

    const answers = []
    for (let i = 0; i < 3; i++) answers.push(await (await fetch(`${base}/email`, form({ request: id, email: 'a@example.com' }))).text())

    expect(mailer.mails).toHaveLength(2)
    expect(new Set(answers).size).toBe(1)
  })

  it('sends only so many mails to one address, whatever the request', async () => {
    await start({ maxMailsPerAddress: 2 })

    for (let i = 0; i < 4; i++) {
      const id = await requestId(await authorize(pkce().challenge))
      await fetch(`${base}/email`, form({ request: id, email: 'a@example.com' }))
    }

    expect(mailer.mails).toHaveLength(2)
  })

  it('does not let a link complete a request other than the one it was mailed for', async () => {
    await start()
    const first = pkce()
    const second = pkce()
    await login('a@example.com', first.challenge)
    const code = (await login('b@example.com', second.challenge)).searchParams.get('code')!

    expect((await exchange(code, first.verifier)).status).toBe(400)
    expect((await exchange(code, second.verifier)).status).toBe(400)
  })
})
