// End to end against a running backend: issue a diploma to an in-process wallet, deploy a policy that trusts this issuer (and one that does not),
// then run the authorization flow with each. Needs ATTESTA_BACKEND_URL (default http://localhost:8080), ATTESTA_ADMIN_TOKEN and a backend whose
// ATTESTA_PUBLIC_URL is the same address. Run: pnpm tsx scripts/e2e.ts
import { createServer } from 'node:http'
import type { AddressInfo } from 'node:net'
import { acceptCredentialOffer, createWalletAgent, resolvePresentationRequest, submitPresentation } from '@attesta/wallet'
import { startIssuer } from '../src/issuer.js'

const backend = (process.env.ATTESTA_BACKEND_URL ?? 'http://localhost:8080').replace(/\/$/, '')
const adminToken = process.env.ATTESTA_ADMIN_TOKEN ?? 'dev-only-insecure-admin-token'

const port = await freePort()
const issuer = await startIssuer({ port, publicUrl: `http://localhost:${port}`, storeKey: 'e2e', allowInsecureHttp: true })
const wallet = await createWalletAgent({ storeId: 'e2e', storeKey: 'e2e', allowInsecureHttp: true })

try {
  const [{ did }] = await issuer.agent.dids.getCreatedDids({ method: 'key' })
  await acceptCredentialOffer(wallet, await issuer.createOffer({ name: 'Ada Lovelace', email: 'ada@example.com', degree: 'Mathematics', university: 'Trusted University' }))

  await deployPolicy('e2e_trusted', did)
  await deployPolicy('e2e_untrusting', 'did:key:z6MkhaXgBZDvotDkL5257faiztiGiC2QtKLGpbnnEGta2doK')

  for (const policyId of ['e2e_trusted', 'e2e_untrusting']) {
    const started = await post('/v1/authorize/requests', { resource: 'diploma-vault', policyId, credentialType: 'Diploma', claims: ['degree'] })
    const { requestId, authorizationRequest } = (await started.json()) as { requestId: string; authorizationRequest: string }

    const request = await resolvePresentationRequest(wallet, authorizationRequest)
    console.log(`${policyId}: ${request.verifier} asks for`, JSON.stringify(request.requested))
    const { status } = await submitPresentation(wallet, request)

    const outcome = await (await fetch(`${backend}/v1/authorize/requests/${requestId}`)).json()
    console.log(`${policyId}: wallet got ${status}, decision`, JSON.stringify(outcome))
  }
} finally {
  await wallet.shutdown()
  await issuer.close()
}

async function deployPolicy(id: string, trustedIssuer: string) {
  const source = `package policy

func Authorize(resource string, credType string, issuer string) (bool, string) {
	if resource != "diploma-vault" {
		return false, "unknown resource: " + resource
	}
	if credType != "Diploma" {
		return false, "unexpected credential type: " + credType
	}
	if issuer != "${trustedIssuer}" {
		return false, "issuer not trusted: " + issuer
	}
	return true, "credential accepted for resource " + resource
}
`
  const res = await post('/admin/policies', { id, source }, { 'X-Admin-Token': adminToken })
  if (!res.ok) throw new Error(`deploying policy ${id}: ${res.status} ${await res.text()}`)
}

function post(path: string, body: unknown, headers: Record<string, string> = {}) {
  return fetch(backend + path, { method: 'POST', headers: { 'Content-Type': 'application/json', ...headers }, body: JSON.stringify(body) })
}

async function freePort(): Promise<number> {
  const server = createServer()
  await new Promise<void>((resolve) => server.listen(0, resolve))
  const { port } = server.address() as AddressInfo
  await new Promise((resolve) => server.close(resolve))
  return port
}
