import { createHash, createPrivateKey, createPublicKey, generateKeyPairSync, randomBytes, sign, timingSafeEqual } from 'node:crypto'
import type { Server } from 'node:http'
import express from 'express'
import type { Mailer } from './mail.js'
import { confirmPage, donePage, emailPage, errorPage, sentPage } from './pages.js'
import type { LoginStore } from './store.js'

export interface Client {
  clientId: string
  clientSecret: string
  /** The addresses the browser may be sent back to, exactly. */
  redirectUris: string[]
}

export interface LoginOptions {
  port: number
  /** The server's identifier, as the client reaches it for the token and the keys: the `iss` of the login result. */
  issuer: string
  /** Where the holder's browser reaches this server, with the path the routes sit under. */
  publicUrl: string
  /** The path every route sits under, e.g. `/login`; it must be the path of `publicUrl`. */
  basePath: string
  client: Client
  store: LoginStore
  mailer: Mailer
  /** Derives the key that signs the login result, instead of a random one. */
  seed?: string
  /** Named in the pages and in the mail. */
  issuerName?: string
  requestMs?: number
  tokenMs?: number
  codeMs?: number
  maxMailsPerRequest?: number
  /** Mails to one address within `mailWindowMs`; more are silently not sent. */
  maxMailsPerAddress?: number
  mailWindowMs?: number
}

const b64u = (b: Buffer | string) => Buffer.from(b).toString('base64url')
const hash = (s: string) => createHash('sha256').update(s).digest('hex')
const secret = () => b64u(randomBytes(32))
const same = (a: string, b: string) => a.length === b.length && timingSafeEqual(Buffer.from(a), Buffer.from(b))
const MAIL = /^[^@\s]+@[^@\s]+\.[^@\s]+$/

// An Ed25519 key derived from the seed: SHA-256 of the text is the private key.
function signingKey(seed?: string) {
  if (!seed) return undefined
  const prefix = Buffer.from('302e020100300506032b657004220420', 'hex')
  return createPrivateKey({ key: Buffer.concat([prefix, createHash('sha256').update(seed).digest()]), format: 'der', type: 'pkcs8' })
}

export async function startLogin(options: LoginOptions) {
  const { store, mailer, client, basePath, issuer, publicUrl } = options
  const requestMs = options.requestMs ?? 15 * 60_000
  const tokenMs = options.tokenMs ?? 10 * 60_000
  const codeMs = options.codeMs ?? 60_000
  const maxMailsPerRequest = options.maxMailsPerRequest ?? 3
  const maxMailsPerAddress = options.maxMailsPerAddress ?? 5
  const mailWindowMs = options.mailWindowMs ?? 10 * 60_000

  const privateKey = signingKey(options.seed) ?? generateKeyPairSync('ed25519').privateKey
  const jwk = { ...(createPublicKey(privateKey).export({ format: 'jwk' }) as object), alg: 'EdDSA', use: 'sig' }
  const kid = b64u(createHash('sha256').update(JSON.stringify(jwk)).digest()).slice(0, 16)
  const publicJwk = { ...jwk, kid }

  const app = express()
  app.use(express.urlencoded({ extended: false }))
  const router = express.Router()
  const name = options.issuerName

  const metadata = () => ({
    issuer,
    authorization_endpoint: `${publicUrl}/authorize`,
    token_endpoint: `${issuer}/token`,
    jwks_uri: `${issuer}/jwks`,
    response_types_supported: ['code'],
    grant_types_supported: ['authorization_code'],
    code_challenge_methods_supported: ['S256'],
    token_endpoint_auth_methods_supported: ['client_secret_post', 'client_secret_basic'],
    id_token_signing_alg_values_supported: ['EdDSA'],
    subject_types_supported: ['public'],
    scopes_supported: ['openid', 'email'],
    authorization_response_iss_parameter_supported: true,
  })
  router.get('/.well-known/openid-configuration', (_req, res) => res.json(metadata()))
  router.get('/.well-known/oauth-authorization-server', (_req, res) => res.json(metadata()))
  router.get('/jwks', (_req, res) => res.json({ keys: [publicJwk] }))

  router.get('/authorize', async (req, res) => {
    const q = req.query as Record<string, string | undefined>
    // Until the client and the return address are known, nothing is sent back to that address.
    if (q.client_id !== client.clientId || !q.redirect_uri || !client.redirectUris.includes(q.redirect_uri)) {
      res.status(400).type('html').send(errorPage('Deze aanvraag is niet geldig.', name))
      return
    }
    if (q.response_type !== 'code' || q.code_challenge_method !== 'S256' || !q.code_challenge) {
      res.status(400).type('html').send(errorPage('Deze aanvraag is niet geldig.', name))
      return
    }
    const request = await store.create({
      clientId: q.client_id,
      redirectUri: q.redirect_uri,
      state: q.state,
      codeChallenge: q.code_challenge,
      scope: q.scope,
      nonce: q.nonce,
      expiresAt: new Date(Date.now() + requestMs),
    })
    res.type('html').send(emailPage(`${basePath}/email`, request.id, name))
  })

  router.post('/email', async (req, res) => {
    const { request: requestId, email } = req.body as { request?: string; email?: string }
    const request = requestId ? await store.find(requestId) : undefined
    if (!request || request.expiresAt.getTime() <= Date.now() || (request.status !== 'pending' && request.status !== 'mailed')) {
      res.status(400).type('html').send(errorPage('Deze aanvraag is verlopen. Begin opnieuw vanuit de app.', name))
      return
    }
    if (!email || !MAIL.test(email.trim())) {
      res.status(400).type('html').send(emailPage(`${basePath}/email`, request.id, name, 'Dit is geen e-mailadres.'))
      return
    }
    const address = email.trim().toLowerCase()
    // The answer is the same whether or not a mail was sent, so the page tells nothing about the address.
    if ((await store.mailsSince(address, new Date(Date.now() - mailWindowMs))) < maxMailsPerAddress) {
      const token = secret()
      if (await store.mailed(request.id, address, hash(token), new Date(Date.now() + tokenMs), maxMailsPerRequest)) {
        await store.recordMail(address)
        await mailer.send({
          to: address,
          subject: `Aanmelden${name ? ` bij ${name}` : ''}`,
          text: `U wilt zich aanmelden${name ? ` bij ${name}` : ''}.\n\nOpen deze link om aan te melden:\n${publicUrl}/confirm?token=${token}\n\nDe link is ${Math.round(tokenMs / 60_000)} minuten geldig en werkt één keer.\nHebt u dit niet aangevraagd? Dan kunt u dit bericht negeren.\n`,
        })
      }
    }
    res.type('html').send(sentPage(name))
  })

  // Opening the link only shows a button: a mail program that fetches links to check them must not complete the login.
  router.get('/confirm', (req, res) => {
    const token = typeof req.query.token === 'string' ? req.query.token : ''
    res.type('html').send(confirmPage(`${basePath}/confirm`, token, name))
  })

  router.post('/confirm', async (req, res) => {
    const token = typeof req.body?.token === 'string' ? req.body.token : ''
    const code = secret()
    const request = token ? await store.confirm(hash(token), hash(code), new Date(Date.now() + codeMs)) : undefined
    if (!request) {
      res.status(400).type('html').send(errorPage('Deze link is verlopen of al gebruikt. Vraag een nieuwe aan vanuit de app.', name))
      return
    }
    const back = new URL(request.redirectUri)
    back.searchParams.set('code', code)
    if (request.state) back.searchParams.set('state', request.state)
    back.searchParams.set('iss', issuer)
    res.type('html').send(donePage(back.toString(), name))
  })

  router.post('/token', async (req, res) => {
    const body = req.body as Record<string, string | undefined>
    const basic = /^Basic (.+)$/.exec(req.headers.authorization ?? '')?.[1]
    const [basicId, basicSecret] = basic ? Buffer.from(basic, 'base64').toString().split(':') : []
    const clientId = basicId ?? body.client_id ?? ''
    const clientSecret = basicSecret ?? body.client_secret ?? ''
    if (clientId !== client.clientId || !same(clientSecret, client.clientSecret)) {
      res.status(401).json({ error: 'invalid_client' })
      return
    }
    if (body.grant_type !== 'authorization_code' || !body.code) {
      res.status(400).json({ error: 'unsupported_grant_type' })
      return
    }
    const request = await store.redeem(hash(body.code))
    const verifier = body.code_verifier ?? ''
    if (!request || request.clientId !== clientId || request.redirectUri !== body.redirect_uri || !same(b64u(createHash('sha256').update(verifier).digest()), request.codeChallenge)) {
      res.status(400).json({ error: 'invalid_grant' })
      return
    }
    const now = Math.floor(Date.now() / 1000)
    const head = b64u(JSON.stringify({ alg: 'EdDSA', typ: 'JWT', kid }))
    const claims = b64u(
      JSON.stringify({
        iss: issuer,
        sub: hash(request.email ?? '').slice(0, 32),
        aud: clientId,
        iat: now,
        exp: now + 300,
        email: request.email,
        email_verified: true,
        ...(request.nonce ? { nonce: request.nonce } : {}),
      }),
    )
    const idToken = `${head}.${claims}.${b64u(sign(null, Buffer.from(`${head}.${claims}`), privateKey))}`
    res.set('Cache-Control', 'no-store').json({ access_token: secret(), token_type: 'Bearer', expires_in: 300, scope: request.scope, id_token: idToken })
  })

  app.use(basePath, router)
  const server: Server = await new Promise((resolve) => {
    const s = app.listen(options.port, () => resolve(s))
  })
  return {
    port: (server.address() as { port: number }).port,
    async close() {
      await new Promise((resolve) => server.close(resolve))
      await store.close()
    },
  }
}
