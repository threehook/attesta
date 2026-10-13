import { CredoError, Kms, SdJwtVcRecord } from '@credo-ts/core'
import type { OpenId4VciCredentialBindingResolver, OpenId4VciRequestTokenResponse, OpenId4VciResolvedCredentialOffer } from '@credo-ts/openid4vc'
import { preAuthorizedCodeGrantIdentifier } from '@credo-ts/openid4vc'
import type { WalletAgent } from './agent.js'

export interface HeldCredential {
  id: string
  /** Credential type (the SD-JWT VC `vct`). */
  type: string
  issuer: string
  /** The claims the holder can disclose. */
  claims: Record<string, unknown>
}

// Binds each credential to a fresh key held in the agent's key store, so credentials can't be linked through the holder key.
function bindToNewKey(agent: WalletAgent): OpenId4VciCredentialBindingResolver {
  return async () => {
    const { publicJwk } = await agent.kms.createKey({ type: { kty: 'OKP', crv: 'Ed25519' } })
    return { method: 'jwk', keys: [Kms.PublicJwk.fromPublicJwk(publicJwk)] }
  }
}

/** What an issuer offers, in a form a consent screen can show. */
export interface CredentialOffer {
  /** The issuer's address. */
  issuer: string
  /** The offered credential types. */
  types: string[]
  resolved: OpenId4VciResolvedCredentialOffer
}

/** Reads an OpenID4VCI credential offer (an `openid-credential-offer://` link) without accepting it. */
export async function previewCredentialOffer(agent: WalletAgent, offerUri: string): Promise<CredentialOffer> {
  const resolved = await agent.openid4vc.holder.resolveCredentialOffer(offerUri)
  return {
    issuer: resolved.credentialOfferPayload.credential_issuer,
    types: Object.values(resolved.offeredCredentialConfigurations).map((config) =>
      'vct' in config && typeof config.vct === 'string' ? config.vct : config.scope ?? config.format,
    ),
    resolved,
  }
}

/** Accepts an OpenID4VCI credential offer (pre-authorized code flow) and stores the SD-JWT VCs it yields. */
export async function acceptCredentialOffer(agent: WalletAgent, offerUri: string): Promise<HeldCredential[]> {
  return acceptPreviewedOffer(agent, await previewCredentialOffer(agent, offerUri))
}

/** Accepts an offer that was read with previewCredentialOffer. */
export async function acceptPreviewedOffer(agent: WalletAgent, offer: CredentialOffer): Promise<HeldCredential[]> {
  const resolvedCredentialOffer = offer.resolved
  const token = await agent.openid4vc.holder.requestToken({ resolvedCredentialOffer })
  return collect(agent, resolvedCredentialOffer, token)
}

const CLIENT_ID = 'attesta-wallet'

/** Whether the holder gets the credential by logging in at the issuer, instead of with a one-time code in the offer. */
export function needsLogin(offer: CredentialOffer): boolean {
  const grants = offer.resolved.credentialOfferPayload.grants
  return Boolean(grants?.authorization_code) && !grants?.[preAuthorizedCodeGrantIdentifier]
}

/** A login at the issuer that has started: the address to open in a browser, and what the wallet needs to finish when the browser comes back. */
export interface IssuerLogin {
  offer: CredentialOffer
  /** The address the issuer sends the browser back to; the wallet must be what opens it. */
  redirectUri: string
  codeVerifier?: string
  /** Where the holder logs in: to be opened in a browser. */
  authorizationRequestUrl: string
}

/** Starts the login for an offer that needs one; `redirectUri` is where the browser is sent afterwards, an address that opens this wallet. */
export async function startIssuerLogin(agent: WalletAgent, offer: CredentialOffer, redirectUri: string): Promise<IssuerLogin> {
  const request = await agent.openid4vc.holder.resolveOpenId4VciAuthorizationRequest(offer.resolved, { clientId: CLIENT_ID, redirectUri })
  if (!('authorizationRequestUrl' in request)) {
    throw new CredoError('the issuer asks for something other than a login, which this wallet cannot do')
  }
  return { offer, redirectUri, codeVerifier: request.codeVerifier, authorizationRequestUrl: request.authorizationRequestUrl }
}

/** Finishes a login with the address the browser was sent back to, and stores the credentials the issuer then gives. */
export async function finishIssuerLogin(agent: WalletAgent, login: IssuerLogin, redirectedTo: string): Promise<HeldCredential[]> {
  const holder = agent.openid4vc.holder
  const resolvedCredentialOffer = login.offer.resolved
  const { authorizationCode } = holder.parseAuthorizationCodeFromAuthorizationResponse({ resolvedCredentialOffer, authorizationResponseRedirectUrl: redirectedTo })
  const token = await holder.requestToken({
    resolvedCredentialOffer,
    code: authorizationCode,
    clientId: CLIENT_ID,
    codeVerifier: login.codeVerifier,
    redirectUri: login.redirectUri,
  })
  return collect(agent, resolvedCredentialOffer, token, CLIENT_ID)
}

// Asks the issuer for the credentials with the token, and keeps them.
async function collect(
  agent: WalletAgent,
  resolvedCredentialOffer: OpenId4VciResolvedCredentialOffer,
  token: OpenId4VciRequestTokenResponse,
  clientId?: string,
): Promise<HeldCredential[]> {
  const { credentials } = await agent.openid4vc.holder.requestCredentials({
    resolvedCredentialOffer,
    ...token,
    clientId,
    credentialBindingResolver: bindToNewKey(agent),
  })

  const held: HeldCredential[] = []
  for (const { record } of credentials) {
    if (!(record instanceof SdJwtVcRecord)) {
      throw new CredoError('the issuer sent a credential that is not an SD-JWT VC; only SD-JWT VC is supported')
    }
    await agent.sdJwtVc.store({ record })
    held.push(describe(agent, record))
  }
  return held
}

export async function listCredentials(agent: WalletAgent): Promise<HeldCredential[]> {
  const records = await agent.sdJwtVc.getAll()
  return records.map((record) => describe(agent, record))
}

function describe(agent: WalletAgent, record: SdJwtVcRecord): HeldCredential {
  const credential = agent.sdJwtVc.fromCompact(record.firstCredential.compact)
  return {
    id: record.id,
    type: String(credential.payload.vct),
    issuer: String(credential.payload.iss),
    claims: credential.prettyClaims,
  }
}
