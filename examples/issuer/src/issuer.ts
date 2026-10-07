import 'reflect-metadata'
import { createHash, createPrivateKey } from 'node:crypto'
import { mkdirSync } from 'node:fs'
import type { Server } from 'node:http'
import { join } from 'node:path'
import { AskarModule } from '@credo-ts/askar'
import { Agent, DidsApi, type KeyDidCreateOptions } from '@credo-ts/core'
import { agentDependencies } from '@credo-ts/node'
import { OpenId4VciCredentialFormatProfile, OpenId4VcModule } from '@credo-ts/openid4vc'
import { NativeAskar } from '@openwallet-foundation/askar-nodejs'
import express from 'express'
import { LOGO_SVG } from './logo.js'
import { renderEmployeeForm, renderForm, renderOffer, renderWelcome } from './page.js'

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

export interface IssuerOptions {
  port: number
  /** Where wallets reach this issuer; it appears in every offer and in the issuer's metadata. */
  publicUrl: string
  storeKey: string
  /** Directory that holds the SQLite store file with the issuer's signing key; created if missing. Omit to keep it in memory only. */
  path?: string
  allowInsecureHttp?: boolean
  /** Derives the issuer's signing key (and so its DID) from this text, instead of generating a random key on first start. */
  seed?: string
  /** The logical issuer, e.g. "Gemeente Utrecht": written into every credential and the issuer metadata, for display only. Trust is by DID. */
  issuerName?: string
}

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
  if (options.path) mkdirSync(options.path, { recursive: true })
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
          database: { type: 'sqlite', config: options.path ? { path: join(options.path, 'issuer.sqlite') } : { inMemory: true } },
        },
      }),
      openid4vc: new OpenId4VcModule({
        app,
        issuer: {
          baseUrl: `${options.publicUrl}/oid4vci`,
          credentialRequestToCredentialMapper: async ({ agentContext, holderBinding, issuanceSession }) => {
            const { type, ...claims } = issuanceSession.issuanceMetadata as unknown as { type: CredentialType } & Record<string, string>
            const { payload, sd } = credentialContent(type, claims, options.issuerName)

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

  // The issuer's identity is a did:key created once and kept in the store, so its DID is stable across restarts.
  if ((await agent.dids.getCreatedDids({ method: 'key' })).length === 0) {
    const key = options.seed ? await importSeedKey(agent, options.seed) : await agent.kms.createKey({ type: { kty: 'OKP', crv: 'Ed25519' } })
    await agent.dids.create<KeyDidCreateOptions>({ method: 'key', options: { keyId: key.keyId } })
  }
  // An issuer kept in the store by an earlier version may lack a credential type, so the supported types are written on every start.
  const credentialConfigurationsSupported = { [DIPLOMA]: sdJwtConfiguration(DIPLOMA), [EMPLOYEE]: sdJwtConfiguration(EMPLOYEE) }
  const display = options.issuerName ? [{ name: options.issuerName }] : undefined
  if ((await agent.openid4vc.issuer.getAllIssuers()).some((issuer) => issuer.issuerId === ISSUER_ID)) {
    await agent.openid4vc.issuer.updateIssuerMetadata({ issuerId: ISSUER_ID, credentialConfigurationsSupported, display })
  } else {
    await agent.openid4vc.issuer.createIssuer({ issuerId: ISSUER_ID, credentialConfigurationsSupported, display })
  }

  async function createOffer(type: CredentialType, claims: DiplomaClaims | EmployeeClaims): Promise<string> {
    const { credentialOffer } = await agent.openid4vc.issuer.createCredentialOffer({
      issuerId: ISSUER_ID,
      credentialConfigurationIds: [type],
      preAuthorizedCodeFlowConfig: {},
      issuanceMetadata: { type, ...claims, description: claims.description?.trim() || DEFAULT_DESCRIPTIONS[type] },
    })
    return credentialOffer
  }

  // Registered after the OpenID4VCI routes, as the module recommends.
  app.use(express.json())
  app.use(express.urlencoded({ extended: false }))
  const [issuerDid] = await agent.dids.getCreatedDids({ method: 'key' })
  const chrome = { issuerName: options.issuerName, did: issuerDid.did }
  app.get('/logo.svg', (_req, res) => {
    res.type('image/svg+xml').send(LOGO_SVG)
  })
  app.get('/', (_req, res) => {
    res.type('html').send(renderWelcome(chrome))
  })
  app.get('/diploma', (_req, res) => {
    res.type('html').send(renderForm(DEFAULT_DESCRIPTIONS[DIPLOMA], chrome, options.issuerName))
  })
  app.post('/offers', async (req, res) => {
    const { name, email, degree, university, description } = req.body ?? {}
    if (![name, email, degree, university].every((v) => typeof v === 'string' && v.trim() !== '')) {
      res.status(400).json({ error: 'name, email, degree and university are required' })
      return
    }
    if (!/^[^@\s]+@[^@\s]+$/.test(email)) {
      res.status(400).json({ error: 'email is not an email address' })
      return
    }
    respondWithOffer(req, res, false, await createOffer(DIPLOMA, { name, email, degree, university, description: asText(description) }))
  })
  app.get('/employee', (_req, res) => {
    res.type('html').send(renderEmployeeForm(DEPARTMENTS, DEFAULT_DESCRIPTIONS[EMPLOYEE], chrome))
  })
  app.post('/employee/offers', async (req, res) => {
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
    respondWithOffer(req, res, true, await createOffer(EMPLOYEE, { name, email, department, organisation, diploma, diplomaValidUntil, description: asText(description) }))
  })

  function respondWithOffer(req: express.Request, res: express.Response, employee: boolean, offerUri: string) {
    if (req.accepts(['json', 'html']) === 'json') {
      res.json({ offerUri })
    } else {
      res.type('html').send(renderOffer(offerUri, chrome, employee))
    }
  }

  const server: Server = await new Promise((resolve) => {
    const s = app.listen(options.port, () => resolve(s))
  })

  return {
    agent,
    /** The DID that signs every credential; verifiers trust the issuer by this identifier. */
    did: issuerDid.did,
    createOffer: (claims: DiplomaClaims) => createOffer(DIPLOMA, claims),
    createEmployeeOffer: (claims: EmployeeClaims) => createOffer(EMPLOYEE, claims),
    async close() {
      await new Promise((resolve) => server.close(resolve))
      await agent.shutdown()
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
