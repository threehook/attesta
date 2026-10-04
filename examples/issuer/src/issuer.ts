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
import { renderForm, renderOffer } from './page.js'

export interface DiplomaClaims {
  name: string
  /** Identifies the person; verifiers ask for it. */
  email: string
  degree: string
  university: string
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
const CREDENTIAL_TYPE = 'Diploma'

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
            const claims = issuanceSession.issuanceMetadata as unknown as DiplomaClaims

            const dids = agentContext.resolve(DidsApi)
            const [issuerDid] = await dids.getCreatedDids({ method: 'key' })
            const didDocument = await dids.resolveDidDocument(issuerDid.did)
            const verificationMethod = didDocument.verificationMethod?.[0]
            if (!verificationMethod) throw new Error('the issuer DID has no verification method')

            return {
              type: 'credentials',
              format: 'dc+sd-jwt',
              credentials: holderBinding.keys.map((holder) => ({
                payload: { vct: CREDENTIAL_TYPE, name: claims.name, email: claims.email, degree: claims.degree, university: claims.university },
                holder,
                issuer: { method: 'did', didUrl: verificationMethod.id },
                disclosureFrame: { _sd: ['name', 'email', 'degree', 'university'] },
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
  if (!(await agent.openid4vc.issuer.getAllIssuers()).some((issuer) => issuer.issuerId === ISSUER_ID)) {
    await agent.openid4vc.issuer.createIssuer({
      issuerId: ISSUER_ID,
      credentialConfigurationsSupported: {
        [CREDENTIAL_TYPE]: {
          format: OpenId4VciCredentialFormatProfile.SdJwtVc,
          scope: CREDENTIAL_TYPE,
          vct: CREDENTIAL_TYPE,
          proof_types_supported: { jwt: { proof_signing_alg_values_supported: ['EdDSA', 'ES256'] } },
          cryptographic_binding_methods_supported: ['did:key', 'jwk'],
        },
      },
    })
  }

  async function createOffer(claims: DiplomaClaims): Promise<string> {
    const { credentialOffer } = await agent.openid4vc.issuer.createCredentialOffer({
      issuerId: ISSUER_ID,
      credentialConfigurationIds: [CREDENTIAL_TYPE],
      preAuthorizedCodeFlowConfig: {},
      issuanceMetadata: { ...claims },
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
    const offerUri = await createOffer({ name, email, degree, university })
    if (req.accepts(['json', 'html']) === 'json') {
      res.json({ offerUri })
    } else {
      res.type('html').send(renderOffer(offerUri))
    }
  })

  const [issuerDid] = await agent.dids.getCreatedDids({ method: 'key' })

  const server: Server = await new Promise((resolve) => {
    const s = app.listen(options.port, () => resolve(s))
  })

  return {
    agent,
    /** The DID that signs every credential; verifiers trust the issuer by this identifier. */
    did: issuerDid.did,
    createOffer,
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
