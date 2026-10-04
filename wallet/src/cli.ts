// Headless wallet for development: `accept <offer-uri>` stores the offered credentials, `list` shows what is held, `present <request-uri>` answers a
// presentation request.
import { homedir } from 'node:os'
import { join } from 'node:path'
import { acceptCredentialOffer, createWalletAgent, listCredentials, resolvePresentationRequest, submitPresentation } from './index.js'

const dir = process.env.ATTESTA_WALLET_DIR ?? join(homedir(), '.attesta-wallet')
const key = process.env.ATTESTA_WALLET_KEY
if (!key) {
  console.error('set ATTESTA_WALLET_KEY to the key that encrypts the wallet store')
  process.exit(1)
}

const [command, arg] = process.argv.slice(2)
if (command !== 'list' && !((command === 'accept' || command === 'present') && arg)) {
  console.error('usage: cli.ts accept <offer-uri> | cli.ts present <request-uri> | cli.ts list')
  process.exit(1)
}

const agent = await createWalletAgent({
  storeId: 'wallet',
  storeKey: key,
  path: dir,
  allowInsecureHttp: process.env.ATTESTA_ALLOW_INSECURE_HTTP === '1',
})
try {
  if (command === 'present') {
    const request = await resolvePresentationRequest(agent, arg)
    console.log(`${request.verifier} asks for ${JSON.stringify(request.requested)}`)
    console.log(`answered with status ${(await submitPresentation(agent, request)).status}`)
  } else {
    const credentials = command === 'accept' ? await acceptCredentialOffer(agent, arg) : await listCredentials(agent)
    console.log(JSON.stringify(credentials, null, 2))
  }
} finally {
  await agent.shutdown()
}
