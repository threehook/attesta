// Issues a diploma to an in-process wallet, has the wallet answer two OpenID4VP requests (one asking for the degree and the email, one for the degree
// only), and records the presentations the verifier receives. The Go verifier tests replay them (backend/internal/presentation/testdata/credo-presentation.json).
// Run: pnpm tsx scripts/sdjwt-fixture.ts <output file>
import { writeFileSync } from 'node:fs'
import { createServer } from 'node:http'
import type { AddressInfo } from 'node:net'
import { createWalletAgent, resolvePresentationRequest, submitPresentation } from '@attesta/wallet'
import { fetchCredential, startTestIssuer } from '../src/testing.js'

const output = process.argv[2]
if (!output) throw new Error('usage: sdjwt-fixture.ts <output file>')

let received: (form: URLSearchParams) => void = () => {}
const capture = createServer((req, res) => {
  let body = ''
  req.on('data', (chunk) => {
    body += chunk
  })
  req.on('end', () => {
    received(new URLSearchParams(body))
    res.writeHead(200, { 'Content-Type': 'application/json' }).end('{}')
  })
})
await new Promise<void>((resolve) => capture.listen(0, resolve))
const responseUri = `http://localhost:${(capture.address() as AddressInfo).port}/response`

const test = await startTestIssuer()
const issuer = test.issuer
const wallet = await createWalletAgent({ storeId: 'fixture', storeKey: 'fixture', allowInsecureHttp: true })

async function present(claims: string[], nonce: string, state: string) {
  const request = new URLSearchParams({
    response_type: 'vp_token',
    client_id: `redirect_uri:${responseUri}`,
    response_uri: responseUri,
    response_mode: 'direct_post',
    nonce,
    state,
    dcql_query: JSON.stringify({
      credentials: [
        { id: 'credential', format: 'dc+sd-jwt', meta: { vct_values: ['Diploma'] }, claims: claims.map((claim) => ({ path: [claim] })) },
      ],
    }),
    client_metadata: JSON.stringify({
      vp_formats_supported: { 'dc+sd-jwt': { 'sd-jwt_alg_values': ['EdDSA', 'ES256'], 'kb-jwt_alg_values': ['EdDSA', 'ES256'] } },
    }),
  })
  const answer = new Promise<URLSearchParams>((resolve) => {
    received = resolve
  })
  await submitPresentation(wallet, await resolvePresentationRequest(wallet, `openid4vp://?${request}`))
  const form = await answer
  return { nonce, state: form.get('state'), vpToken: form.get('vp_token') }
}

try {
  await issuer.createDiplomaEntitlement({ name: 'Ada Lovelace', email: 'ada@example.com', degree: 'Mathematics', university: 'Trusted University' })
  await fetchCredential(wallet, test, 'Diploma', 'ada@example.com')
  const issuedAt = Math.floor(Date.now() / 1000)
  const full = await present(['degree', 'email'], 'fixture-nonce', 'fixture-state')
  const withoutEmail = await present(['degree'], 'fixture-nonce-2', 'fixture-state-2')

  writeFileSync(
    output,
    `${JSON.stringify({ issuer: issuer.did, audience: `redirect_uri:${responseUri}`, issuedAt, full, withoutEmail }, null, 2)}\n`,
  )
  console.log('wrote', output)
} finally {
  await wallet.shutdown()
  await test.close()
  capture.close()
}
