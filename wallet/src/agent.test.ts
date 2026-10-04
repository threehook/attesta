import { mkdtempSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { createWalletAgent } from './agent.js'

const folder = (name: string) => join(mkdtempSync(join(tmpdir(), 'wallet-agent-')), name)

describe('createWalletAgent', () => {
  it('opens a store in a folder whose path has spaces, as the system app folders do', async () => {
    const agent = await createWalletAgent({ storeId: 'wallet', storeKey: 'a-key', path: folder('Application Support/my wallet') })
    await agent.shutdown()
  })

  it('refuses a folder with an "@" in its path, which Askar cannot open, and says why', async () => {
    await expect(createWalletAgent({ storeId: 'wallet', storeKey: 'a-key', path: folder('@scope/wallet') })).rejects.toThrow('cannot have an "@"')
  })
})
