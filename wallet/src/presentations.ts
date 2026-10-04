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
  resolved: Resolved
}

/** Reads an OpenID4VP authorization request (an `openid4vp://` link) and checks it against the held credentials. */
export async function resolvePresentationRequest(agent: WalletAgent, requestUri: string): Promise<PresentationRequest> {
  const resolved = await agent.openid4vc.holder.resolveOpenId4VpAuthorizationRequest(requestUri)
  if (!resolved.dcql) throw new CredoError('only DCQL presentation requests are supported')
  return {
    verifier: resolved.verifier.effectiveClientId,
    requested: resolved.dcql.queryResult.credentials.map((query) => ({
      type: query.meta && 'vct_values' in query.meta ? (query.meta.vct_values ?? []) : [],
      claims: (query.claims ?? []).map((claim) => ('path' in claim ? claim.path.join('.') : '')),
    })),
    satisfiable: resolved.dcql.queryResult.can_be_satisfied,
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
