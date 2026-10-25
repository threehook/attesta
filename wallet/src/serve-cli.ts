// The wallet of a system, for a pod or a pipeline job: it keeps its credentials in a temporary folder, takes a new one when the system asks, and answers the
// verifiers registered in ATTESTA_WALLET_VERIFIERS by itself. It listens on 127.0.0.1 only. ATTESTA_WALLET_HOSTS maps a public address to the one
// inside the cluster. With ATTESTA_WALLET_KMS_URL its keys are made in OpenBao and sign there, named after ATTESTA_WALLET_KMS_PREFIX. With
// ATTESTA_WALLET_AZURE_KEY_VAULT_URL they are in Azure Key Vault instead: ATTESTA_WALLET_AZURE_KEY_MODE is `create` (a key at every start, named after
// ATTESTA_WALLET_KMS_PREFIX) or `provisioned` (the existing key ATTESTA_WALLET_AZURE_KEY_NAME).
import { randomBytes } from 'node:crypto'
import { mkdtempSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { createServer } from 'node:http'
import { AzureKeyVaultKms, KeyPerStart, ProvisionedKey, azureKeyVault, type KeyStrategy } from './azure-key-vault-kms.js'
import { acceptCredential, createWalletAgent, listCredentials, resolvePresentationRequest, submitPresentation } from './index.js'
import { parseHosts } from './hosts.js'
import { OpenBaoKms } from './openbao-kms.js'
import { createHandler } from './serve.js'

const verifiers = (process.env.ATTESTA_WALLET_VERIFIERS ?? '')
  .split(',')
  .map((prefix) => prefix.trim())
  .filter((prefix) => prefix !== '')
if (verifiers.length === 0) {
  console.error('set ATTESTA_WALLET_VERIFIERS to the client ids this wallet answers, such as redirect_uri:https://attesta.example/, separated by commas')
  process.exit(1)
}
const port = Number(process.env.ATTESTA_WALLET_PORT ?? 7100)

function fail(message: string): never {
  console.error(message)
  process.exit(1)
}

const kmsUrl = process.env.ATTESTA_WALLET_KMS_URL
if (kmsUrl && !process.env.ATTESTA_WALLET_KMS_TOKEN) fail('set ATTESTA_WALLET_KMS_TOKEN, the token this wallet signs in to OpenBao with')
const kmsPrefix = process.env.ATTESTA_WALLET_KMS_PREFIX
if (kmsUrl && !kmsPrefix) fail('set ATTESTA_WALLET_KMS_PREFIX, the name that this system\'s keys in OpenBao start with, such as cicd-agent')

const azureVaultUrl = process.env.ATTESTA_WALLET_AZURE_KEY_VAULT_URL
if (azureVaultUrl && kmsUrl) fail('set either ATTESTA_WALLET_KMS_URL (OpenBao) or ATTESTA_WALLET_AZURE_KEY_VAULT_URL (Key Vault), not both')

function azureStrategy(): KeyStrategy {
  const mode = process.env.ATTESTA_WALLET_AZURE_KEY_MODE
  if (mode === 'create') {
    return new KeyPerStart(kmsPrefix ?? fail('set ATTESTA_WALLET_KMS_PREFIX, the name that this system\'s keys in Key Vault start with, such as cicd-agent'))
  }
  if (mode === 'provisioned') {
    return new ProvisionedKey(process.env.ATTESTA_WALLET_AZURE_KEY_NAME ?? fail('set ATTESTA_WALLET_AZURE_KEY_NAME, the key in Key Vault to sign with'))
  }
  return fail('set ATTESTA_WALLET_AZURE_KEY_MODE to create or provisioned')
}

const kms = azureVaultUrl
  ? new AzureKeyVaultKms(azureKeyVault(azureVaultUrl), azureStrategy())
  : kmsUrl
    ? new OpenBaoKms({ url: kmsUrl, token: process.env.ATTESTA_WALLET_KMS_TOKEN!, prefix: kmsPrefix! })
    : undefined
// The store is empty at every start, so keys under the prefix belong to a wallet that is gone.
if (kms) console.log(`deleted ${await kms.deleteLeftovers()} keys of an earlier wallet`)
const agent = await createWalletAgent({
  storeId: 'wallet',
  // A store in memory is lost after 30 minutes; the key is new at every start, so the folder is too.
  path: mkdtempSync(join(tmpdir(), 'attesta-wallet-')),
  storeKey: randomBytes(32).toString('hex'),
  allowInsecureHttp: process.env.ATTESTA_ALLOW_INSECURE_HTTP === '1',
  hosts: parseHosts(process.env.ATTESTA_WALLET_HOSTS ?? ''),
  kms,
})
const server = createServer(
  createHandler(
    {
      accept: (link) => acceptCredential(agent, link, { replaceHeld: true }),
      resolve: (link) => resolvePresentationRequest(agent, link),
      submit: (request) => submitPresentation(agent, request),
      held: () => listCredentials(agent),
    },
    verifiers,
  ),
)
server.listen(port, '127.0.0.1', () => console.log(`wallet listening on 127.0.0.1:${port}`))

for (const signal of ['SIGINT', 'SIGTERM'] as const) {
  process.once(signal, () => {
    server.close()
    void agent.shutdown().finally(() => process.exit(0))
  })
}
