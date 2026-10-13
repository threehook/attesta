import { SmtpMailer } from './mail.js'
import { startLogin } from './login.js'
import { MemoryLoginStore, PostgresLoginStore } from './store.js'

const env = (name: string, fallback: string) => process.env[name] || fallback

const port = Number(env('ATTESTA_LOGIN_PORT', '4002'))
if (!Number.isInteger(port) || port < 0 || port > 65535) {
  throw new Error(`ATTESTA_LOGIN_PORT must be a port number, got "${process.env.ATTESTA_LOGIN_PORT}"`)
}
const publicUrl = env('ATTESTA_LOGIN_PUBLIC_URL', `http://localhost:${port}/login`).replace(/\/$/, '')
const basePath = new URL(publicUrl).pathname.replace(/\/$/, '')
const dbHost = process.env.ATTESTA_LOGIN_DB_HOST
const store = dbHost
  ? await PostgresLoginStore.connect(
      { host: dbHost, user: env('ATTESTA_LOGIN_DB_USER', 'issuer'), password: env('ATTESTA_LOGIN_DB_PASSWORD', '') },
      env('ATTESTA_LOGIN_DB_NAME', 'issuer_login'),
    )
  : new MemoryLoginStore()

await startLogin({
  port,
  issuer: env('ATTESTA_LOGIN_ISSUER', publicUrl),
  publicUrl,
  basePath,
  client: {
    clientId: env('ATTESTA_LOGIN_CLIENT_ID', 'attesta-issuer'),
    clientSecret: env('ATTESTA_LOGIN_CLIENT_SECRET', 'dev-only-insecure-login-secret'),
    redirectUris: env('ATTESTA_LOGIN_REDIRECT_URIS', 'http://localhost:4001/oid4vci/diploma/redirect').split(','),
  },
  store,
  mailer: new SmtpMailer({ host: env('ATTESTA_LOGIN_SMTP_HOST', 'localhost'), port: Number(env('ATTESTA_LOGIN_SMTP_PORT', '1025')) }, env('ATTESTA_LOGIN_MAIL_FROM', 'aanmelden@attesta.local')),
  seed: env('ATTESTA_LOGIN_SEED', 'dev-only-insecure-login-seed'),
  issuerName: env('ATTESTA_LOGIN_ISSUER_NAME', 'Gemeente Vlierdam'),
})
console.log(`login server listening on ${port}, public url ${publicUrl}`)
