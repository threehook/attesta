import { homedir } from 'node:os'
import { join } from 'node:path'
import { startIssuer } from './issuer.js'

// Fixes the issuer's DID so the example policy can name it; override ZKPUOI_ISSUER_SEED for anything but local development.
const DEV_SEED = 'dev-only-insecure-issuer-seed'

const port = Number(process.env.ZKPUOI_ISSUER_PORT ?? 4000)
const publicUrl = process.env.ZKPUOI_ISSUER_PUBLIC_URL ?? `http://localhost:${port}`

const issuer = await startIssuer({
  port,
  publicUrl,
  storeKey: process.env.ZKPUOI_ISSUER_KEY ?? 'dev-only-insecure-issuer-key',
  path: process.env.ZKPUOI_ISSUER_DIR ?? join(homedir(), '.zk-puoi-issuer'),
  allowInsecureHttp: publicUrl.startsWith('http://'),
  seed: process.env.ZKPUOI_ISSUER_SEED ?? DEV_SEED,
})
console.log(`issuer listening on ${port}, public url ${publicUrl}`)
console.log(`issuer DID ${issuer.did}`)
