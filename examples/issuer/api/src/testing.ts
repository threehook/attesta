// What tests need to get a credential into a wallet the way a holder does: a login server that catches its mails, an issuer that logs holders in at it,
// and the holder's browser played in code. Nothing in the issuer itself depends on this.
import { createServer } from 'node:net'
import { finishIssuerLogin, type HeldCredential, needsLogin, previewCredentialOffer, startIssuerLogin, type WalletAgent } from '@attesta/wallet'
import { startLogin } from '../../login/src/login.js'
import type { Mail } from '../../login/src/mail.js'
import { MemoryLoginStore } from '../../login/src/store.js'
import { type IssuerOptions, startIssuer } from './issuer.js'

export const WALLET_REDIRECT = 'attesta-wallet://callback'

export async function freePort(): Promise<number> {
  const server = createServer()
  await new Promise<void>((resolve) => server.listen(0, resolve))
  const { port } = server.address() as { port: number }
  await new Promise((resolve) => server.close(resolve))
  return port
}

export interface TestIssuer {
  issuer: Awaited<ReturnType<typeof startIssuer>>
  login: Awaited<ReturnType<typeof startLogin>>
  /** The address wallets reach the issuer at. */
  url: string
  /** The mails the login server sent, newest last. */
  mails: Mail[]
  close(): Promise<void>
}

/** Starts the login server and an issuer that logs holders in at it. `extra` adds to or replaces the issuer's options. */
export async function startTestIssuer(extra: Partial<IssuerOptions> = {}): Promise<TestIssuer> {
  const mails: Mail[] = []
  const [issuerPort, loginPort] = [await freePort(), await freePort()]
  const url = `http://localhost:${issuerPort}`
  const loginUrl = `http://localhost:${loginPort}/login`
  const login = await startLogin({
    port: loginPort,
    issuer: loginUrl,
    publicUrl: loginUrl,
    basePath: '/login',
    client: { clientId: 'attesta-issuer', clientSecret: 'secret', redirectUris: [`${url}/oid4vci/diploma/redirect`] },
    store: new MemoryLoginStore(),
    mailer: { send: async (mail) => void mails.push(mail) },
    seed: 'test login seed',
  })
  const issuer = await startIssuer({
    port: issuerPort,
    publicUrl: url,
    storeKey: 'test-issuer-key',
    allowInsecureHttp: true,
    login: { issuer: loginUrl, clientId: 'attesta-issuer', clientSecret: 'secret', walletRedirectUris: [WALLET_REDIRECT] },
    ...extra,
  })
  return {
    issuer,
    login,
    url,
    mails,
    async close() {
      await issuer.close()
      await login.close()
    },
  }
}

/** What the issuer's page gives a holder: a link that names the issuer and the credential, and no secret. */
export const requestLink = (issuerUrl: string, type: string) =>
  `openid-credential-offer://?credential_offer=${encodeURIComponent(
    JSON.stringify({ credential_issuer: `${issuerUrl}/oid4vci/diploma`, credential_configuration_ids: [type], grants: { authorization_code: {} } }),
  )}`

const form = (fields: Record<string, string>) => ({ method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, body: new URLSearchParams(fields) })

/**
 * The holder's browser: opens the address the wallet gave, logs in as `email` with the link from the mail, and returns the address the browser ends with,
 * which is the one that opens the wallet.
 */
export async function browserLogin(address: string, email: string, mails: Mail[]): Promise<string> {
  const toLogin = await fetch(address, { redirect: 'manual' })
  const page = new URL(toLogin.headers.get('location') ?? '')
  const emailPage = await (await fetch(page)).text()
  const before = mails.length
  await fetch(`${page.origin}/login/email`, form({ request: /name="request" value="([^"]+)"/.exec(emailPage)![1], email }))
  const mail = mails[before]
  if (!mail) throw new Error('no mail was sent')
  const token = new URL(/(https?:\/\/\S+)/.exec(mail.text)![1]).searchParams.get('token')!
  const done = await (await fetch(`${page.origin}/login/confirm`, form({ token }))).text()
  const back = await fetch(/<a class="button" href="([^"]+)"/.exec(done)![1].replaceAll('&#38;', '&'), { redirect: 'manual' })
  return back.headers.get('location')!
}

/** A headless wallet asks for a credential of this type and the holder logs in as `email`. */
export async function fetchCredential(wallet: WalletAgent, test: TestIssuer, type: string, email: string): Promise<HeldCredential[]> {
  const offer = await previewCredentialOffer(wallet, requestLink(test.url, type))
  if (!needsLogin(offer)) throw new Error('expected an offer that needs a login')
  const started = await startIssuerLogin(wallet, offer, WALLET_REDIRECT)
  return finishIssuerLogin(wallet, started, await browserLogin(started.authorizationRequestUrl, email, test.mails))
}
