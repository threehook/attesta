import 'reflect-metadata'
import { mkdirSync } from 'node:fs'
import { join } from 'node:path'
import { AskarModule } from '@credo-ts/askar'
import { Agent, Kms } from '@credo-ts/core'
import { agentDependencies } from '@credo-ts/node'
import { OpenId4VcModule } from '@credo-ts/openid4vc'
import { NativeAskar } from '@openwallet-foundation/askar-nodejs'
import { mappedFetch } from './hosts.js'

export interface WalletOptions {
  /** Names the store; also its file name under `path` when persisted. */
  storeId: string
  /** Encrypts the store. */
  storeKey: string
  /** Directory that holds the SQLite store file; created if missing. Omit to keep the wallet in memory only. */
  path?: string
  /** Accept links and issuers served over plain http, for local development. */
  allowInsecureHttp?: boolean
  /** Public addresses that are reached at another address, such as inside a cluster: what is sent to the key goes to its value. */
  hosts?: Record<string, string>
  /** Holds the holder keys instead of the wallet's own store, such as OpenBao; the key then never leaves it. */
  kms?: Kms.KeyManagementService
}

export async function createWalletAgent(options: WalletOptions) {
  // Askar builds a database URL from the path, and an "@" in it is read as the start of a host, so the store cannot be created.
  if (options.path?.includes('@')) {
    throw new Error(`The wallet's data folder cannot have an "@" in its path (${options.path}); choose another folder.`)
  }
  if (options.path) mkdirSync(options.path, { recursive: true })
  const agent = new Agent({
    config: { allowInsecureHttpUrls: options.allowInsecureHttp ?? false },
    dependencies: options.hosts ? { ...agentDependencies, fetch: mappedFetch(agentDependencies.fetch, options.hosts) } : agentDependencies,
    modules: {
      askar: new AskarModule({
        askar: NativeAskar,
        store: {
          id: options.storeId,
          key: options.storeKey,
          database: { type: 'sqlite', config: options.path ? { path: join(options.path, `${options.storeId}.sqlite`) } : { inMemory: true } },
        },
      }),
      openid4vc: new OpenId4VcModule(),
      ...(options.kms && { kms: new Kms.KeyManagementModule({ backends: [options.kms], defaultBackend: options.kms.backend }) }),
    },
  })
  await agent.initialize()
  return agent
}

export type WalletAgent = Awaited<ReturnType<typeof createWalletAgent>>
