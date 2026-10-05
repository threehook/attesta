import { CredoError } from '@credo-ts/core'
import type { WalletAgent } from './agent.js'

type Resolved = Awaited<ReturnType<WalletAgent['openid4vc']['holder']['resolveOpenId4VpAuthorizationRequest']>>

/** What a verifier asks for, in a form a consent screen can show. */
export interface PresentationRequest {
  /** Who is asking, as the request identifies them. */
  verifier: string
  /** The credential types and claims asked for. */
  requested: Array<{ type: string[]; claims: string[] }>
  /** Whether the wallet holds credentials that answer the request. */
  satisfiable: boolean
  /** The identity the link asked the wallet to answer with (`login_hint` and `issuer_hint`), if it named one. */
  hint?: { email: string; issuer: string }
  resolved: Resolved
}

const HINT_EMAIL = 'login_hint'
const HINT_ISSUER = 'issuer_hint'

/**
 * Takes the identity hint off a link and narrows the request's DCQL query to that identity: the claim `email` and the credential's issuer (`iss`)
 * must have the hinted values, so the wallet answers with the credential that was chosen. A link with no hint, or no inline query, is left as it is.
 */
export function applyIdentityHint(requestUri: string): { uri: string; hint?: { email: string; issuer: string } } {
  let url: URL
  try {
    url = new URL(requestUri)
  } catch {
    return { uri: requestUri }
  }
  const email = url.searchParams.get(HINT_EMAIL)
  const issuer = url.searchParams.get(HINT_ISSUER)
  url.searchParams.delete(HINT_EMAIL)
  url.searchParams.delete(HINT_ISSUER)
  if (!email || !issuer) return { uri: url.toString() === requestUri ? requestUri : url.toString() }

  const query = url.searchParams.get('dcql_query')
  if (query) {
    const parsed = JSON.parse(query) as { credentials?: Array<{ claims?: Array<Record<string, unknown>> }> }
    for (const credential of parsed.credentials ?? []) {
      const claims = (credential.claims ?? []).filter((claim) => !isPath(claim, 'iss'))
      credential.claims = claims.map((claim) => (isPath(claim, 'email') ? { ...claim, values: [email] } : claim))
      credential.claims.push({ path: ['iss'], values: [issuer] })
    }
    url.searchParams.set('dcql_query', JSON.stringify(parsed))
  }
  return { uri: url.toString(), hint: { email, issuer } }
}

const isPath = (claim: Record<string, unknown>, name: string) => Array.isArray(claim.path) && claim.path.length === 1 && claim.path[0] === name

/** Reads an OpenID4VP authorization request (an `openid4vp://` link) and checks it against the held credentials. */
export async function resolvePresentationRequest(agent: WalletAgent, requestUri: string): Promise<PresentationRequest> {
  const { uri, hint } = applyIdentityHint(requestUri)
  const resolved = await agent.openid4vc.holder.resolveOpenId4VpAuthorizationRequest(uri)
  if (!resolved.dcql) throw new CredoError('only DCQL presentation requests are supported')
  return {
    verifier: resolved.verifier.effectiveClientId,
    requested: resolved.dcql.queryResult.credentials.map((query) => ({
      type: query.meta && 'vct_values' in query.meta ? (query.meta.vct_values ?? []) : [],
      // The issuer condition added for a hint is not something the verifier asked to see.
      claims: (query.claims ?? []).map((claim) => ('path' in claim ? claim.path.join('.') : '')).filter((claim) => !(hint && claim === 'iss')),
    })),
    satisfiable: resolved.dcql.queryResult.can_be_satisfied,
    hint,
    resolved,
  }
}

/** Answers a resolved request with the held credentials that satisfy it, disclosing only the requested claims. */
export async function submitPresentation(agent: WalletAgent, request: PresentationRequest): Promise<{ status: number }> {
  const { dcql, authorizationRequestPayload } = request.resolved
  if (!dcql || !request.satisfiable) throw new CredoError('the wallet holds no credential that satisfies the request')
  const holder = agent.openid4vc.holder
  const { serverResponse } = await holder.acceptOpenId4VpAuthorizationRequest({
    authorizationRequestPayload,
    dcql: { credentials: holder.selectCredentialsForDcqlRequest(dcql.queryResult) },
  })
  if (!serverResponse) throw new CredoError('the request does not ask for the response to be posted to the verifier')
  return { status: serverResponse.status }
}
