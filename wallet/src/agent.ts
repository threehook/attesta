import 'reflect-metadata'
import { mkdirSync } from 'node:fs'
import { join } from 'node:path'
import { AskarModule } from '@credo-ts/askar'
import { Agent } from '@credo-ts/core'
import { agentDependencies } from '@credo-ts/node'
import { OpenId4VcModule } from '@credo-ts/openid4vc'
import { NativeAskar } from '@openwallet-foundation/askar-nodejs'

export interface WalletOptions {
  /** Names the store; also its file name under `path` when persisted. */
  storeId: string
  /** Encrypts the store. */
  storeKey: string
  /** Directory that holds the SQLite store file; created if missing. Omit to keep the wallet in memory only. */
  path?: string
  /** Accept offers and issuers served over plain http, for local development. */
  allowInsecureHttp?: boolean
}

export async function createWalletAgent(options: WalletOptions) {
  if (options.path) mkdirSync(options.path, { recursive: true })
  const agent = new Agent({
    config: { allowInsecureHttpUrls: options.allowInsecureHttp ?? false },
    dependencies: agentDependencies,
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
    },
  })
  await agent.initialize()
  return agent
}

export type WalletAgent = Awaited<ReturnType<typeof createWalletAgent>>
