import { createOidcAuth } from './auth.js'
import { startGui } from './gui.js'

const port = Number(process.env.ATTESTA_ISSUER_GUI_LISTEN_PORT ?? 4000)
if (!Number.isInteger(port) || port < 0 || port > 65535) {
  throw new Error(`ATTESTA_ISSUER_GUI_LISTEN_PORT must be a port number, got "${process.env.ATTESTA_ISSUER_GUI_LISTEN_PORT}"`)
}
const apiUrl = process.env.ATTESTA_ISSUER_API_URL ?? 'http://localhost:4001'

const env = (name: string) => {
  const value = process.env[name]
  if (!value) throw new Error(`${name} is required (ATTESTA_ISSUER_AUTH_DISABLED=true leaves the pages open)`)
  return value
}
const auth =
  process.env.ATTESTA_ISSUER_AUTH_DISABLED === 'true'
    ? undefined
    : await createOidcAuth({
        issuer: env('ATTESTA_ISSUER_OIDC_ISSUER'),
        discoveryUrl: process.env.ATTESTA_ISSUER_OIDC_DISCOVERY_URL,
        clientId: process.env.ATTESTA_ISSUER_OIDC_CLIENT_ID ?? 'issuer-gui',
        clientSecret: env('ATTESTA_ISSUER_OIDC_CLIENT_SECRET'),
        publicUrl: env('ATTESTA_ISSUER_GUI_PUBLIC_URL'),
      })
if (!auth) console.warn('sign-in is switched off: anyone who reaches the pages can record entitlements')

const backend = process.env.ATTESTA_BACKEND_URL
  ? { url: process.env.ATTESTA_BACKEND_URL, adminToken: env('ATTESTA_BACKEND_ADMIN_TOKEN') }
  : undefined
if (!backend) console.warn('ATTESTA_BACKEND_URL is not set: the Policy Editor cannot check policies')

await startGui({ port, apiUrl, auth, backend })
console.log(`issuer pages listening on ${port}, issuer API at ${apiUrl}`)
