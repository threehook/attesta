import { homedir } from 'node:os'
import { join } from 'node:path'
import { startIssuer } from './issuer.js'
import { SmtpMailer, type SmtpTls } from './mail.js'

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

// Where the mail goes: a mail catcher unless the organisation set a real server. The settings come from one Secret in the cluster.
const smtpTls = process.env.ATTESTA_ISSUER_SMTP_TLS
if (smtpTls && !['none', 'starttls', 'tls'].includes(smtpTls)) throw new Error(`ATTESTA_ISSUER_SMTP_TLS must be none, starttls or tls, got "${smtpTls}"`)
const mailer = new SmtpMailer(
  {
    host: process.env.ATTESTA_ISSUER_SMTP_HOST || 'localhost',
    port: Number(process.env.ATTESTA_ISSUER_SMTP_PORT || 1025),
    user: process.env.ATTESTA_ISSUER_SMTP_USER || undefined,
    password: process.env.ATTESTA_ISSUER_SMTP_PASSWORD || undefined,
    tls: smtpTls as SmtpTls | undefined,
  },
  process.env.ATTESTA_ISSUER_MAIL_FROM || 'credentials@attesta.local',
)

const issuer = await startIssuer({
  mailer,
  qrLifetimeSeconds: process.env.ATTESTA_ISSUER_QR_LIFETIME_SECONDS ? Number(process.env.ATTESTA_ISSUER_QR_LIFETIME_SECONDS) : undefined,
  port,
  publicUrl,
  storeKey: process.env.ATTESTA_ISSUER_KEY ?? 'dev-only-insecure-issuer-key',
  path: process.env.ATTESTA_ISSUER_DIR ?? join(homedir(), '.attesta-issuer'),
  database,
  allowInsecureHttp: publicUrl.startsWith('http://'),
  seed: process.env.ATTESTA_ISSUER_SEED ?? DEV_SEED,
  issuerName: process.env.ATTESTA_ISSUER_NAME ?? 'Gemeente Vlierdam',
})
console.log(`issuer listening on ${port}, public url ${publicUrl}`)
console.log(`issuer DID ${issuer.did}`)
