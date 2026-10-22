import { randomBytes } from 'node:crypto'
import type { Request, RequestHandler, Response } from 'express'
import express from 'express'
import * as client from 'openid-client'

export interface OidcOptions {
  /** The OpenID Connect server's name as it writes it in its tokens. */
  issuer: string
  /** Where its metadata is fetched; defaults to the issuer. It differs when the issuer's address is only reachable from outside. */
  discoveryUrl?: string
  clientId: string
  clientSecret: string
  /** Where staff reach these pages; the server sends them back to it. */
  publicUrl: string
}

/** Signed-in staff: guards the pages and gives the access token the issuer API checks. */
export interface Auth {
  routes: express.Router
  /** Sends anyone who is not signed in to the login. */
  guard: RequestHandler
  /** The signed-in person's access token, renewed when it is about to expire; undefined when the session is over. */
  token(req: Request): Promise<string | undefined>
  /** The signed-in person's name for the pages: the user name the login server knows, else the email address. */
  user(req: Request): string | undefined
}

interface Session {
  accessToken: string
  refreshToken?: string
  expiresAt: number
  idToken?: string
  user?: string
}

interface Pending {
  verifier: string
  nonce: string
}

const nameOf = (claims?: Record<string, unknown>): string | undefined =>
  [claims?.["preferred_username"], claims?.["email"]].find((v): v is string => typeof v === 'string' && v !== '')

const COOKIE = 'attesta_issuer_session'
const LOGIN_COOKIE = 'attesta_issuer_login'
const RENEW_BEFORE_MS = 30_000

const cookieOf = (req: Request, name: string): string | undefined =>
  req.headers.cookie
    ?.split(';')
    .map((c) => c.trim().split('='))
    .find(([n]) => n === name)?.[1]

export async function createOidcAuth(options: OidcOptions): Promise<Auth> {
  const publicUrl = options.publicUrl.replace(/\/$/, '')
  const secure = publicUrl.startsWith('https:')
  const discovery = `${(options.discoveryUrl ?? options.issuer).replace(/\/$/, '')}/.well-known/openid-configuration`

  // The metadata is read from where the server is reachable, but it names itself as tokens do, which openid-client's own discovery would refuse.
  let config: client.Configuration | undefined
  const configuration = async () => {
    if (config) return config
    const res = await fetch(discovery)
    if (!res.ok) throw new Error(`the login server answered ${res.status} for ${discovery}`)
    const c = new client.Configuration({ ...((await res.json()) as client.ServerMetadata), issuer: options.issuer }, options.clientId, options.clientSecret)
    // Inside the cluster the login server is reached over plain http.
    if (discovery.startsWith('http:')) client.allowInsecureRequests(c)
    return (config = c)
  }

  const sessions = new Map<string, Session>()
  const pending = new Map<string, Pending>()
  const set = (res: Response, name: string, value: string, maxAgeSeconds?: number) =>
    res.append('Set-Cookie', `${name}=${value}; Path=/; HttpOnly; SameSite=Lax${secure ? '; Secure' : ''}${maxAgeSeconds === undefined ? '' : `; Max-Age=${maxAgeSeconds}`}`)

  const routes = express.Router()
  routes.get('/login', async (_req, res) => {
    const c = await configuration()
    const verifier = client.randomPKCECodeVerifier()
    const state = client.randomState()
    const nonce = client.randomNonce()
    pending.set(state, { verifier, nonce })
    setTimeout(() => pending.delete(state), 10 * 60_000).unref()
    set(res, LOGIN_COOKIE, state, 600)
    res.redirect(
      client
        .buildAuthorizationUrl(c, {
          redirect_uri: `${publicUrl}/callback`,
          scope: 'openid',
          state,
          nonce,
          code_challenge: await client.calculatePKCECodeChallenge(verifier),
          code_challenge_method: 'S256',
        })
        .href,
    )
  })

  routes.get('/callback', async (req, res) => {
    const state = String(req.query.state ?? '')
    const login = pending.get(state)
    // The state must come back with the browser that started the login.
    if (!login || cookieOf(req, LOGIN_COOKIE) !== state) {
      res.status(400).type('text').send('De aanmelding is niet gestart of verlopen. Probeer het opnieuw.')
      return
    }
    pending.delete(state)
    set(res, LOGIN_COOKIE, '', 0)
    const tokens = await client.authorizationCodeGrant(await configuration(), new URL(`${publicUrl}${req.originalUrl}`), {
      pkceCodeVerifier: login.verifier,
      expectedState: state,
      expectedNonce: login.nonce,
    })
    const id = randomBytes(32).toString('hex')
    sessions.set(id, {
      accessToken: tokens.access_token,
      refreshToken: tokens.refresh_token,
      expiresAt: Date.now() + (tokens.expires_in ?? 60) * 1000,
      idToken: tokens.id_token,
      user: nameOf(tokens.claims()),
    })
    set(res, COOKIE, id)
    res.redirect('/')
  })

  routes.get('/logout', async (req, res) => {
    const id = cookieOf(req, COOKIE)
    const session = id ? sessions.get(id) : undefined
    if (id) sessions.delete(id)
    set(res, COOKIE, '', 0)
    const c = await configuration()
    if (!session?.idToken || !c.serverMetadata().end_session_endpoint) {
      res.redirect('/login')
      return
    }
    res.redirect(client.buildEndSessionUrl(c, { id_token_hint: session.idToken, post_logout_redirect_uri: `${publicUrl}/` }).href)
  })

  const token = async (req: Request) => {
    const id = cookieOf(req, COOKIE)
    const session = id ? sessions.get(id) : undefined
    if (!id || !session) return undefined
    if (session.expiresAt - Date.now() > RENEW_BEFORE_MS) return session.accessToken
    try {
      if (!session.refreshToken) throw new Error('no refresh token')
      const renewed = await client.refreshTokenGrant(await configuration(), session.refreshToken)
      session.accessToken = renewed.access_token
      session.refreshToken = renewed.refresh_token ?? session.refreshToken
      session.expiresAt = Date.now() + (renewed.expires_in ?? 60) * 1000
      session.idToken = renewed.id_token ?? session.idToken
      return session.accessToken
    } catch {
      sessions.delete(id)
      return undefined
    }
  }

  const guard: RequestHandler = async (req, res, next) => {
    if (await token(req)) return next()
    res.redirect('/login')
  }

  const user = (req: Request) => {
    const id = cookieOf(req, COOKIE)
    return id ? sessions.get(id)?.user : undefined
  }

  return { routes, guard, token, user }
}
