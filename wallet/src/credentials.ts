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

/** What the wallet read from a link or QR code, in a form a consent screen can show. The standard calls the link a credential offer. */
export interface CredentialPreview {
  /** The issuer's address. */
  issuer: string
  /** The credential types the issuer gives. */
  types: string[]
  resolved: OpenId4VciResolvedCredentialOffer
}

/** Reads an `openid-credential-offer://` link (the text of a QR code from an issuer) without accepting what it gives. */
export async function previewCredential(agent: WalletAgent, link: string): Promise<CredentialPreview> {
  const resolved = await agent.openid4vc.holder.resolveCredentialOffer(link).catch((error: unknown) => {
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

/** Takes what a link gives (pre-authorized code flow) and stores the SD-JWT VCs it yields. */
export async function acceptCredential(agent: WalletAgent, link: string): Promise<HeldCredential[]> {
  return acceptPreviewed(agent, await previewCredential(agent, link))
}

/** Takes the credential that was read with previewCredential. */
export async function acceptPreviewed(agent: WalletAgent, preview: CredentialPreview): Promise<HeldCredential[]> {
  const holder = agent.openid4vc.holder
  const resolvedCredentialOffer = preview.resolved
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

// When the issuer refuses to give what the link points to, it says why in its answer (for an expired QR code, that a new one was sent); that is what the person should read.
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
