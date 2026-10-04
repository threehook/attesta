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
import { renderEmployeeForm, renderForm, renderOffer } from './page.js'

export interface DiplomaClaims {
  name: string
  /** Identifies the person; verifiers ask for it. */
  email: string
  degree: string
  university: string
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
}

const ISSUER_ID = 'diploma'
const DIPLOMA = 'Diploma'
const EMPLOYEE = 'Employee'
type CredentialType = typeof DIPLOMA | typeof EMPLOYEE

/** Seconds since the epoch at the end of the given YYYY-MM-DD day, UTC. */
export function endOfDay(date: string): number {
  return Math.floor(Date.parse(`${date}T23:59:59Z`) / 1000)
}

// What a credential holds: the payload that is signed, and which of its claims the holder may disclose selectively.
function credentialContent(type: CredentialType, metadata: Record<string, string>) {
  if (type === EMPLOYEE) {
    const { name, email, department, organisation, diploma, diplomaValidUntil } = metadata
    return {
      payload: { vct: EMPLOYEE, name, email, department, organisation, diploma, exp: endOfDay(diplomaValidUntil) },
      sd: ['name', 'email', 'department', 'organisation', 'diploma'],
    }
  }
  const { name, email, degree, university } = metadata
  return { payload: { vct: DIPLOMA, name, email, degree, university }, sd: ['name', 'email', 'degree', 'university'] }
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
            const { payload, sd } = credentialContent(type, claims)

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
  if ((await agent.openid4vc.issuer.getAllIssuers()).some((issuer) => issuer.issuerId === ISSUER_ID)) {
    await agent.openid4vc.issuer.updateIssuerMetadata({ issuerId: ISSUER_ID, credentialConfigurationsSupported })
  } else {
    await agent.openid4vc.issuer.createIssuer({ issuerId: ISSUER_ID, credentialConfigurationsSupported })
  }

  async function createOffer(type: CredentialType, claims: DiplomaClaims | EmployeeClaims): Promise<string> {
    const { credentialOffer } = await agent.openid4vc.issuer.createCredentialOffer({
      issuerId: ISSUER_ID,
      credentialConfigurationIds: [type],
      preAuthorizedCodeFlowConfig: {},
      issuanceMetadata: { type, ...claims },
    })
    return credentialOffer
  }

  // Registered after the OpenID4VCI routes, as the module recommends.
  app.use(express.json())
  app.use(express.urlencoded({ extended: false }))
  app.get('/', (_req, res) => {
    res.type('html').send(renderForm())
  })
  app.post('/offers', async (req, res) => {
    const { name, email, degree, university } = req.body ?? {}
    if (![name, email, degree, university].every((v) => typeof v === 'string' && v.trim() !== '')) {
      res.status(400).json({ error: 'name, email, degree and university are required' })
      return
    }
    if (!/^[^@\s]+@[^@\s]+$/.test(email)) {
      res.status(400).json({ error: 'email is not an email address' })
      return
    }
    respondWithOffer(req, res, await createOffer(DIPLOMA, { name, email, degree, university }))
  })
  app.get('/employee', (_req, res) => {
    res.type('html').send(renderEmployeeForm(DEPARTMENTS))
  })
  app.post('/employee/offers', async (req, res) => {
    const { name, email, department, organisation, diploma, diplomaValidUntil } = req.body ?? {}
    if (![name, email, department, organisation, diploma, diplomaValidUntil].every((v) => typeof v === 'string' && v.trim() !== '')) {
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
    if (!/^\d{4}-\d{2}-\d{2}$/.test(diplomaValidUntil) || Number.isNaN(endOfDay(diplomaValidUntil))) {
      res.status(400).json({ error: 'diplomaValidUntil must be a date, YYYY-MM-DD' })
      return
    }
    if (endOfDay(diplomaValidUntil) * 1000 <= Date.now()) {
      res.status(400).json({ error: 'diplomaValidUntil is in the past; a wallet refuses an expired credential' })
      return
    }
    respondWithOffer(req, res, await createOffer(EMPLOYEE, { name, email, department, organisation, diploma, diplomaValidUntil }))
  })

  const [issuerDid] = await agent.dids.getCreatedDids({ method: 'key' })

  function respondWithOffer(req: express.Request, res: express.Response, offerUri: string) {
    if (req.accepts(['json', 'html']) === 'json') {
      res.json({ offerUri })
    } else {
      res.type('html').send(renderOffer(offerUri))
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
