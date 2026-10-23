// The wallet of a system, for a pod or a pipeline job: it keeps its credentials in memory, takes a new one when the system asks, and answers the
// verifiers registered in ATTESTA_WALLET_VERIFIERS by itself. It listens on 127.0.0.1 only. ATTESTA_WALLET_HOSTS maps a public address to the one
// inside the cluster. With ATTESTA_WALLET_KMS_URL its keys are made in OpenBao and sign there.
import { randomBytes } from 'node:crypto'
import { createServer } from 'node:http'
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

const kmsUrl = process.env.ATTESTA_WALLET_KMS_URL
if (kmsUrl && !process.env.ATTESTA_WALLET_KMS_TOKEN) {
  console.error('set ATTESTA_WALLET_KMS_TOKEN, the token this wallet signs in to OpenBao with')
  process.exit(1)
}
const agent = await createWalletAgent({
  storeId: 'wallet',
  storeKey: randomBytes(32).toString('hex'),
  allowInsecureHttp: process.env.ATTESTA_ALLOW_INSECURE_HTTP === '1',
  hosts: parseHosts(process.env.ATTESTA_WALLET_HOSTS ?? ''),
  kms: kmsUrl ? new OpenBaoKms({ url: kmsUrl, token: process.env.ATTESTA_WALLET_KMS_TOKEN! }) : undefined,
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
