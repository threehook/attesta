import 'reflect-metadata'
import { createHash, createPrivateKey } from 'node:crypto'
import { mkdirSync } from 'node:fs'
import type { Server } from 'node:http'
import { join } from 'node:path'
import { AskarModule } from '@credo-ts/askar'
import { Agent, DidsApi, type KeyDidCreateOptions } from '@credo-ts/core'
import { agentDependencies } from '@credo-ts/node'
import { OpenId4VciCredentialFormatProfile, OpenId4VcIssuanceSessionRecord, OpenId4VcIssuanceSessionRepository, OpenId4VcIssuanceSessionState, type OpenId4VcIssuanceSessionStateChangedEvent, OpenId4VcIssuerEvents, OpenId4VcModule } from '@credo-ts/openid4vc'
import { NativeAskar } from '@openwallet-foundation/askar-nodejs'
import { Oauth2ErrorCodes, Oauth2ServerErrorResponseError } from '@openid4vc/oauth2'
import express from 'express'
import type { DatabaseOptions } from '@attesta/issuer-pg'
import { MemoryEntitlementStore, PostgresEntitlementStore, type Entitlement, type EntitlementStore } from './entitlements.js'

export interface DiplomaClaims {
  name: string
  /** Identifies the person; verifiers ask for it. */
  email: string
  degree: string
  university: string
  /** Shown to the holder with the credential; defaults to DEFAULT_DESCRIPTIONS.Diploma. */
  description?: string
}

export type Department = 'burgerzaken' | 'secretariaat'
export const DEPARTMENTS: Department[] = ['burgerzaken', 'secretariaat']

/** An employee of an organisation: where they work, and the diploma that entitles them to act. */
export interface EmployeeClaims {
  name: string
  /** Identifies the person; verifiers ask for it. */
  email: string
  department: Department
  organisation: string
  /** The course the employee holds a diploma for. */
  diploma: string
  /** Last day the diploma is valid, as YYYY-MM-DD. The credential expires at the end of that day (UTC). */
  diplomaValidUntil: string
  /** Shown to the holder with the credential; defaults to DEFAULT_DESCRIPTIONS.Employee. */
  description?: string
}

/** The authorization server that identifies the holder, reached by its `issuer` address; this issuer is its client. */
export interface LoginServer {
  issuer: string
  clientId: string
  clientSecret: string
  /** Where the holder's wallet is sent back to after the login. */
  walletRedirectUris: string[]
}

export interface IssuerOptions {
  port: number
  /** Where wallets reach this issuer; it appears in every offer and in the issuer's metadata. */
  publicUrl: string
  storeKey: string
  /** Directory that holds the SQLite store file with the issuer's signing key; created if missing. Omit to keep it in memory only. Ignored with `database`. */
  path?: string
  /** A Postgres server for everything the issuer keeps: its store and its entitlements. Without it they live in `path`, or in memory. */
  database?: DatabaseOptions
  /** Where entitlements are kept, instead of `database` or memory. */
  entitlements?: EntitlementStore
  /** How long a claim holds an entitlement while the credential is made, in milliseconds. */
  entitlementLeaseMs?: number
  /** The login server holders log in at when their wallet asks for a credential: the only way a credential is issued. */
  login: LoginServer
  allowInsecureHttp?: boolean
  /** Derives the issuer's signing key (and so its DID) from this text, instead of generating a random key on first start. */
  seed?: string
  /** The logical issuer, e.g. "Gemeente Utrecht": written into every credential and the issuer metadata, for display only. Trust is by DID. */
  issuerName?: string
}

/** The database for entitlements; the library's own store gets another, named after its store id. */
const ENTITLEMENTS_DATABASE = 'issuer_entitlements'
const ISSUER_ID = 'diploma'
const DIPLOMA = 'Diploma'
const EMPLOYEE = 'Employee'
type CredentialType = typeof DIPLOMA | typeof EMPLOYEE

/** Pre-filled in the forms and used when a request leaves the description out. */
export const DEFAULT_DESCRIPTIONS: Record<CredentialType, string> = {
  [DIPLOMA]: 'Diploma uitgereikt door het opleidingsinstituut',
  [EMPLOYEE]: 'Autorisatie om laadpalen aan te vragen voor burgers van onze gemeente',
}

/** Seconds since the epoch at the end of the given YYYY-MM-DD day, UTC. */
// The form sends dd-mm-yyyy; API clients may send YYYY-MM-DD.
const toIsoDate = (v: string) => v.trim().replace(/^(\d{2})-(\d{2})-(\d{4})$/, '$3-$2-$1')

// What the wallet is told when there is nothing to issue: a standard OpenID4VCI error, so it can say so to the person.
const noEntitlement = () =>
  new Oauth2ServerErrorResponseError({ error: Oauth2ErrorCodes.CredentialRequestDenied, error_description: 'there is no open entitlement for you' })

// The email the login server vouched for: the library verified the signed result before it let the wallet go on.
function loggedInEmail(session: OpenId4VcIssuanceSessionRecord): string {
  const idToken = session.chainedIdentity?.externalAccessTokenResponse?.id_token
  const email = typeof idToken === 'string' ? (JSON.parse(Buffer.from(idToken.split('.')[1], 'base64url').toString()) as { email?: unknown }).email : undefined
  if (typeof email !== 'string' || email === '') throw new Error('the login did not give an email address')
  return email
}

const sameClaims = (a: Record<string, string>, b: Record<string, string>) =>
  Object.keys(a).length === Object.keys(b).length && Object.entries(a).every(([key, value]) => b[key] === value)

const asText = (v: unknown) => (typeof v === 'string' ? v : undefined)

export function endOfDay(date: string): number {
  return Math.floor(Date.parse(`${date}T23:59:59Z`) / 1000)
}

// What a credential holds: the payload that is signed, and which of its claims the holder may disclose selectively.
function credentialContent(type: CredentialType, metadata: Record<string, string>, issuerName?: string) {
  const { description } = metadata
  const common = { issuer_name: issuerName, description }
  if (type === EMPLOYEE) {
    const { name, email, department, organisation, diploma, diplomaValidUntil } = metadata
    return {
      payload: { vct: EMPLOYEE, ...common, name, email, department, organisation, diploma, exp: endOfDay(diplomaValidUntil) },
      sd: ['description', 'name', 'email', 'department', 'organisation', 'diploma'],
    }
  }
  const { name, email, degree, university } = metadata
  return { payload: { vct: DIPLOMA, ...common, name, email, degree, university }, sd: ['description', 'name', 'email', 'degree', 'university'] }
}

const sdJwtConfiguration = (type: CredentialType) => ({
  format: OpenId4VciCredentialFormatProfile.SdJwtVc as const,
  scope: type,
  vct: type,
  proof_types_supported: { jwt: { proof_signing_alg_values_supported: ['EdDSA', 'ES256'] } },
  cryptographic_binding_methods_supported: ['did:key', 'jwk'],
})

export async function startIssuer(options: IssuerOptions) {
  if (options.path && !options.database) mkdirSync(options.path, { recursive: true })
  const entitlements: EntitlementStore = options.entitlements ?? (options.database ? await PostgresEntitlementStore.connect(options.database, ENTITLEMENTS_DATABASE, options.entitlementLeaseMs) : new MemoryEntitlementStore(options.entitlementLeaseMs))
  // The credential is made from the entitlement the holder logged in for: their oldest open one of the requested type.
  async function claimForHolder(email: string, type: string): Promise<Entitlement> {
    for (const entitlement of await entitlements.findOpenByEmail(email)) {
      if (entitlement.type === type && (await entitlements.claim(entitlement.id))) return entitlement
    }
    throw noEntitlement()
  }

  const app = express()
  const agent = new Agent({
    config: { allowInsecureHttpUrls: options.allowInsecureHttp ?? false },
    dependencies: agentDependencies,
    modules: {
      askar: new AskarModule({
        askar: NativeAskar,
        store: {
          id: 'issuer',
          key: options.storeKey,
          database: options.database
            ? { type: 'postgres', config: { host: options.database.host }, credentials: { account: options.database.user, password: options.database.password } }
            : { type: 'sqlite', config: options.path ? { path: join(options.path, 'issuer.sqlite') } : { inMemory: true } },
        },
      }),
      openid4vc: new OpenId4VcModule({
        app,
        issuer: {
          baseUrl: `${options.publicUrl}/oid4vci`,
          getDynamicIssuanceSession: ({ requestedCredentialConfigurations, clientId }) => {
            return { authorizationFlow: 'chained', authorizationServerUrl: options.login.issuer, credentialConfigurationIds: Object.keys(requestedCredentialConfigurations), clientId }
          },
          credentialRequestToCredentialMapper: async ({ agentContext, holderBinding, issuanceSession, credentialConfigurationId }) => {
            const entitlement = await claimForHolder(loggedInEmail(issuanceSession), credentialConfigurationId)
            // The session remembers its entitlement, so that it is marked issued when the credential is made, also after a restart.
            issuanceSession.issuanceMetadata = { ...issuanceSession.issuanceMetadata, entitlementId: entitlement.id }
            await agentContext.dependencyManager.resolve(OpenId4VcIssuanceSessionRepository).update(agentContext, issuanceSession)
            const { payload, sd } = credentialContent(entitlement.type as CredentialType, entitlement.claims, options.issuerName)

            const dids = agentContext.resolve(DidsApi)
            const [issuerDid] = await dids.getCreatedDids({ method: 'key' })
            const didDocument = await dids.resolveDidDocument(issuerDid.did)
            const verificationMethod = didDocument.verificationMethod?.[0]
            if (!verificationMethod) throw new Error('the issuer DID has no verification method')

            return {
              type: 'credentials',
              format: 'dc+sd-jwt',
              credentials: holderBinding.keys.map((holder) => ({
                payload,
                holder,
                issuer: { method: 'did', didUrl: verificationMethod.id },
                disclosureFrame: { _sd: sd },
              })),
            }
          },
        },
      }),
    },
  })
  await agent.initialize()

  // The entitlement is issued once the library has made the credential; without this event the claim runs out and the entitlement can be claimed again.
  agent.events.on<OpenId4VcIssuanceSessionStateChangedEvent>(OpenId4VcIssuerEvents.IssuanceSessionStateChanged, async ({ payload: { issuanceSession } }) => {
    if (issuanceSession.state !== OpenId4VcIssuanceSessionState.Completed) return
    const id = (issuanceSession.issuanceMetadata as { entitlementId?: string } | undefined)?.entitlementId
    if (!id) return
    try {
      await entitlements.markIssued(id)
    } catch (error) {
      console.error(`could not record that entitlement ${id} was issued`, error)
    }
  })

  // A credential made just before the issuer stopped may not have been marked issued; its session still names the entitlement.
  const sessions = await agent.dependencyManager.resolve(OpenId4VcIssuanceSessionRepository).findByQuery(agent.context, { state: OpenId4VcIssuanceSessionState.Completed })
  for (const session of sessions) {
    const id = (session.issuanceMetadata as { entitlementId?: string } | undefined)?.entitlementId
    if (id) await entitlements.markIssued(id)
  }

  // The issuer's identity is a did:key created once and kept in the store, so its DID is stable across restarts.
  if ((await agent.dids.getCreatedDids({ method: 'key' })).length === 0) {
    const key = options.seed ? await importSeedKey(agent, options.seed) : await agent.kms.createKey({ type: { kty: 'OKP', crv: 'Ed25519' } })
    await agent.dids.create<KeyDidCreateOptions>({ method: 'key', options: { keyId: key.keyId } })
  }
  // An issuer kept in the store by an earlier version may lack a credential type, so the supported types are written on every start.
  const credentialConfigurationsSupported = { [DIPLOMA]: sdJwtConfiguration(DIPLOMA), [EMPLOYEE]: sdJwtConfiguration(EMPLOYEE) }
  const display = options.issuerName ? [{ name: options.issuerName }] : undefined
  const authorizationServerConfigs = [
    {
      type: 'chained' as const,
      issuer: options.login.issuer,
      clientAuthentication: { type: 'clientSecret' as const, clientId: options.login.clientId, clientSecret: options.login.clientSecret },
      scopesMapping: { [DIPLOMA]: ['openid', 'email'], [EMPLOYEE]: ['openid', 'email'] },
      redirectUris: options.login.walletRedirectUris,
    },
  ]
  if ((await agent.openid4vc.issuer.getAllIssuers()).some((issuer) => issuer.issuerId === ISSUER_ID)) {
    await agent.openid4vc.issuer.updateIssuerMetadata({ issuerId: ISSUER_ID, credentialConfigurationsSupported, display, authorizationServerConfigs })
  } else {
    await agent.openid4vc.issuer.createIssuer({ issuerId: ISSUER_ID, credentialConfigurationsSupported, display, authorizationServerConfigs })
  }

  // Records that this person may have this credential. The same person with the same data again gets the entitlement they already have, while it is open.
  async function createEntitlement(type: CredentialType, claims: DiplomaClaims | EmployeeClaims): Promise<Entitlement> {
    const stored = { ...claims, description: claims.description?.trim() || DEFAULT_DESCRIPTIONS[type] } as Record<string, string>
    const validUntil = type === EMPLOYEE ? new Date(endOfDay((claims as EmployeeClaims).diplomaValidUntil) * 1000) : undefined
    const same = (await entitlements.findOpenByEmail(claims.email)).find((e) => e.type === type && sameClaims(e.claims, stored))
    return same ?? (await entitlements.create({ email: claims.email, type, claims: stored, validUntil }))
  }

  // Registered after the OpenID4VCI routes, as the module recommends.
  app.use(express.json())
  const [issuerDid] = await agent.dids.getCreatedDids({ method: 'key' })
  // What the page needs to show the forms and the issuer it issues as.
  app.get('/info', (_req, res) => {
    res.json({
      issuerName: options.issuerName,
      did: issuerDid.did,
      departments: DEPARTMENTS,
      descriptions: DEFAULT_DESCRIPTIONS,
      // What a wallet needs to ask for a credential itself: the issuer's identifier, and the types it can ask for.
      credentialIssuer: `${options.publicUrl}/oid4vci/${ISSUER_ID}`,
      credentialTypes: [DIPLOMA, EMPLOYEE],
    })
  })
  app.post('/entitlements/diploma', async (req, res) => {
    const { name, email, degree, university, description } = req.body ?? {}
    if (![name, email, degree, university].every((v) => typeof v === 'string' && v.trim() !== '')) {
      res.status(400).json({ error: 'name, email, degree and university are required' })
      return
    }
    if (!/^[^@\s]+@[^@\s]+$/.test(email)) {
      res.status(400).json({ error: 'email is not an email address' })
      return
    }
    res.json({ entitlementId: (await createEntitlement(DIPLOMA, { name, email, degree, university, description: asText(description) })).id })
  })
  app.post('/entitlements/employee', async (req, res) => {
    const { name, email, department, organisation, diploma, diplomaValidUntil: validUntil, description } = req.body ?? {}
    if (![name, email, department, organisation, diploma, validUntil].every((v) => typeof v === 'string' && v.trim() !== '')) {
      res.status(400).json({ error: 'name, email, department, organisation, diploma and diplomaValidUntil are required' })
      return
    }
    if (!/^[^@\s]+@[^@\s]+$/.test(email)) {
      res.status(400).json({ error: 'email is not an email address' })
      return
    }
    if (!DEPARTMENTS.includes(department)) {
      res.status(400).json({ error: `department must be one of ${DEPARTMENTS.join(', ')}` })
      return
    }
    const diplomaValidUntil = toIsoDate(validUntil)
    if (!/^\d{4}-\d{2}-\d{2}$/.test(diplomaValidUntil) || Number.isNaN(endOfDay(diplomaValidUntil))) {
      res.status(400).json({ error: 'diplomaValidUntil must be a date, dd-mm-yyyy or YYYY-MM-DD' })
      return
    }
    if (endOfDay(diplomaValidUntil) * 1000 <= Date.now()) {
      res.status(400).json({ error: 'diplomaValidUntil is in the past; a wallet refuses an expired credential' })
      return
    }
    res.json({ entitlementId: (await createEntitlement(EMPLOYEE, { name, email, department, organisation, diploma, diplomaValidUntil, description: asText(description) })).id })
  })

  const server: Server = await new Promise((resolve) => {
    const s = app.listen(options.port, () => resolve(s))
  })

  return {
    agent,
    /** The DID that signs every credential; verifiers trust the issuer by this identifier. */
    did: issuerDid.did,
    entitlements,
    createDiplomaEntitlement: (claims: DiplomaClaims) => createEntitlement(DIPLOMA, claims),
    createEmployeeEntitlement: (claims: EmployeeClaims) => createEntitlement(EMPLOYEE, claims),
    async close() {
      await new Promise((resolve) => server.close(resolve))
      await agent.shutdown()
      await entitlements.close()
    },
  }
}

// An Ed25519 key derived from the seed: SHA-256 of the text is the private key.
async function importSeedKey(agent: Agent, seed: string) {
  const pkcs8Ed25519Prefix = Buffer.from('302e020100300506032b657004220420', 'hex')
  const privateKey = createPrivateKey({
    key: Buffer.concat([pkcs8Ed25519Prefix, createHash('sha256').update(seed).digest()]),
    format: 'der',
    type: 'pkcs8',
  })
  const jwk = privateKey.export({ format: 'jwk' })
  return agent.kms.importKey({ privateJwk: { kty: 'OKP', crv: 'Ed25519', x: jwk.x as string, d: jwk.d as string } })
}
