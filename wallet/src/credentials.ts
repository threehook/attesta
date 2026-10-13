import { CredoError, Kms, SdJwtVcRecord } from '@credo-ts/core'
import type { OpenId4VciCredentialBindingResolver, OpenId4VciResolvedCredentialOffer } from '@credo-ts/openid4vc'
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
  const resolved = await agent.openid4vc.holder.resolveCredentialOffer(offerUri).catch((error: unknown) => {
    throw saidByIssuer(error) ?? error
  })
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
  const holder = agent.openid4vc.holder
  const resolvedCredentialOffer = offer.resolved
  const token = await holder.requestToken({ resolvedCredentialOffer })
  const { credentials } = await holder.requestCredentials({
    resolvedCredentialOffer,
    ...token,
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

// When the issuer refuses to give the offer, it says why in its answer (for an expired QR code, that a new one was sent); that is what the person should read.
function saidByIssuer(error: unknown): Error | undefined {
  const text = (error as { textResponse?: unknown } | null)?.textResponse
  if (typeof text !== 'string') return undefined
  try {
    const said = (JSON.parse(text) as { error_description?: unknown }).error_description
    return typeof said === 'string' && said !== '' ? new CredoError(said, { cause: error as Error }) : undefined
  } catch {
    return undefined
  }
}
