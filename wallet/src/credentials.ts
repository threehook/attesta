import { CredoError, Kms, SdJwtVcRecord } from '@credo-ts/core'
import type { OpenId4VciCredentialBindingResolver, OpenId4VciResolvedCredentialOffer } from '@credo-ts/openid4vc'
import type { WalletAgent } from './agent.js'
import { standingOf } from './status.js'

export interface HeldCredential {
  id: string
  /** Credential type (the SD-JWT VC `vct`). */
  type: string
  issuer: string
  /** The claims the holder can disclose. */
  claims: Record<string, unknown>
}

// Binds each credential to a fresh key held in the agent's key store, so credentials can't be linked through the holder key.
// A default key store that cannot make Ed25519 keys, such as OpenBao, gets P-256 ones.
function bindToNewKey(agent: WalletAgent): OpenId4VciCredentialBindingResolver {
  return async () => {
    const config = agent.dependencyManager.resolve(Kms.KeyManagementModuleConfig)
    const ed25519 = { kty: 'OKP', crv: 'Ed25519' } as const
    const type = !config.defaultBackend.isOperationSupported(undefined as never, { operation: 'createKey', type: ed25519 }) ? ({ kty: 'EC', crv: 'P-256' } as const) : ed25519
    const { publicJwk } = await agent.kms.createKey({ type })
    return { method: 'jwk', keys: [Kms.PublicJwk.fromPublicJwk(publicJwk)] }
  }
}

// Where a held credential came from (the issuer's address), kept with it so that a new credential of the same type from the same issuer is recognised.
const ISSUER_URL = 'attesta/issuerUrl'

/** What the wallet read from a link or QR code, in a form a consent screen can show. The standard calls the link a credential offer. */
export interface CredentialPreview {
  /** The issuer's address. */
  issuer: string
  /** The credential types the issuer gives. */
  types: string[]
  resolved: OpenId4VciResolvedCredentialOffer
  /** Credentials held of the same type from the same issuer that no longer count (revoked or ended); accepting the new one removes them. */
  replaces: HeldCredential[]
  /** Whether the new credential replaces a held one of the same type from the same issuer even while that still counts. */
  replaceHeld: boolean
}

export interface PreviewOptions {
  /** A system that renews its own credential replaces the held one when the new one is in; a person has to remove it first. */
  replaceHeld?: boolean
}

/** Reads an `openid-credential-offer://` link (the text of a QR code from an issuer) without accepting what it gives. */
export async function previewCredential(agent: WalletAgent, link: string, options: PreviewOptions = {}): Promise<CredentialPreview> {
  const resolved = await agent.openid4vc.holder.resolveCredentialOffer(link).catch((error: unknown) => {
    throw saidByIssuer(error) ?? error
  })
  const preview: CredentialPreview = {
    issuer: resolved.credentialOfferPayload.credential_issuer,
    types: Object.values(resolved.offeredCredentialConfigurations).map((config) =>
      'vct' in config && typeof config.vct === 'string' ? config.vct : config.scope ?? config.format,
    ),
    resolved,
    replaces: [],
    replaceHeld: options.replaceHeld ?? false,
  }
  preview.replaces = await replacedBy(agent, preview)
  return preview
}

/**
 * The held credentials of a type the preview gives, from the same issuer, that no longer count: revoked by the issuer or past their end. A held credential of
 * unknown origin counts as the same issuer. One that still counts (or whose standing cannot be established) refuses the new credential, before the link is used.
 */
async function replacedBy(agent: WalletAgent, preview: CredentialPreview): Promise<HeldCredential[]> {
  const replaces: HeldCredential[] = []
  for (const record of await agent.sdJwtVc.getAll()) {
    const held = describe(agent, record)
    const origin = record.metadata.get<{ url: string }>(ISSUER_URL)?.url
    if (!preview.types.includes(held.type) || (origin !== undefined && origin !== preview.issuer)) continue
    const standing = await standingOf(agent.sdJwtVc.fromCompact(record.firstCredential.compact).payload)
    if (preview.replaceHeld || standing === 'revoked' || standing === 'ended') {
      replaces.push(held)
      continue
    }
    throw new CredoError(`U heeft al een credential "${titleOf(held)}" van deze uitgever. Verwijder dat eerst om een nieuw credential toe te voegen.`)
  }
  return replaces
}

/** What the holder calls a credential: its title, or else its type. */
export const titleOf = (held: HeldCredential): string => (typeof held.claims.title === 'string' && held.claims.title !== '' ? held.claims.title : held.type)

/** Takes what a link gives (pre-authorized code flow) and stores the SD-JWT VCs it yields. */
export async function acceptCredential(agent: WalletAgent, link: string, options: PreviewOptions = {}): Promise<HeldCredential[]> {
  return acceptPreviewed(agent, await previewCredential(agent, link, options))
}

/** Takes the credential that was read with previewCredential. */
export async function acceptPreviewed(agent: WalletAgent, preview: CredentialPreview): Promise<HeldCredential[]> {
  const replaces = await replacedBy(agent, preview)
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
    record.metadata.set(ISSUER_URL, { url: preview.issuer })
    await agent.sdJwtVc.store({ record })
    held.push(describe(agent, record))
  }
  for (const old of replaces) await removeCredential(agent, old.id)
  return held
}

export async function listCredentials(agent: WalletAgent): Promise<HeldCredential[]> {
  const records = await agent.sdJwtVc.getAll()
  return records.map((record) => describe(agent, record))
}

/** Removes a held credential and the key it was bound to. */
export async function removeCredential(agent: WalletAgent, id: string): Promise<void> {
  const { credentialInstances } = await agent.sdJwtVc.getById(id)
  await agent.sdJwtVc.deleteById(id)
  for (const { kmsKeyId } of credentialInstances) if (kmsKeyId !== undefined) await agent.kms.deleteKey({ keyId: kmsKeyId })
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
