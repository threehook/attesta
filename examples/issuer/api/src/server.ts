import { homedir } from 'node:os'
import { join } from 'node:path'
import { startIssuer } from './issuer.js'

// Fixes the issuer's DID so the example policy can name it; override ATTESTA_ISSUER_SEED for anything but local development.
const DEV_SEED = 'dev-only-insecure-issuer-seed'

const port = Number(process.env.ATTESTA_ISSUER_PORT ?? 4001)
if (!Number.isInteger(port) || port < 0 || port > 65535) {
  throw new Error(`ATTESTA_ISSUER_PORT must be a port number, got "${process.env.ATTESTA_ISSUER_PORT}"`)
}
const publicUrl = process.env.ATTESTA_ISSUER_PUBLIC_URL ?? `http://localhost:${port}`

const dbHost = process.env.ATTESTA_ISSUER_DB_HOST
const database = dbHost
  ? { host: dbHost, user: process.env.ATTESTA_ISSUER_DB_USER ?? 'issuer', password: process.env.ATTESTA_ISSUER_DB_PASSWORD ?? '' }
  : undefined

const loginUrl = process.env.ATTESTA_ISSUER_LOGIN_URL
const login = loginUrl
  ? {
      issuer: loginUrl,
      clientId: process.env.ATTESTA_ISSUER_LOGIN_CLIENT_ID ?? 'attesta-issuer',
      clientSecret: process.env.ATTESTA_ISSUER_LOGIN_CLIENT_SECRET ?? 'dev-only-insecure-login-secret',
      walletRedirectUris: (process.env.ATTESTA_ISSUER_WALLET_REDIRECT_URIS ?? 'attesta-wallet://callback').split(','),
    }
  : undefined

const issuer = await startIssuer({
  port,
  publicUrl,
  storeKey: process.env.ATTESTA_ISSUER_KEY ?? 'dev-only-insecure-issuer-key',
  path: process.env.ATTESTA_ISSUER_DIR ?? join(homedir(), '.attesta-issuer'),
  database,
  allowInsecureHttp: publicUrl.startsWith('http://') || Boolean(loginUrl?.startsWith('http://')),
  login,
  seed: process.env.ATTESTA_ISSUER_SEED ?? DEV_SEED,
  issuerName: process.env.ATTESTA_ISSUER_NAME ?? 'Gemeente Vlierdam',
})
console.log(`issuer listening on ${port}, public url ${publicUrl}`)
console.log(`issuer DID ${issuer.did}`)
