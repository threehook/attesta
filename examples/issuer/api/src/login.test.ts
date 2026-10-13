import { createServer } from 'node:net'
import { createWalletAgent, listCredentials, type WalletAgent } from '@attesta/wallet'
import { Kms } from '@credo-ts/core'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { startLogin } from '../../login/src/login.js'
import type { Mail } from '../../login/src/mail.js'
import { MemoryLoginStore } from '../../login/src/store.js'
import { startIssuer } from './issuer.js'

async function freePort(): Promise<number> {
  const server = createServer()
  await new Promise<void>((resolve) => server.listen(0, resolve))
  const { port } = server.address() as { port: number }
  await new Promise((resolve) => server.close(resolve))
  return port
}

async function until<T>(read: () => Promise<T>, done: (value: T) => boolean): Promise<T> {
  for (let i = 0; i < 50; i++) {
    const value = await read()
    if (done(value)) return value
    await new Promise((resolve) => setTimeout(resolve, 100))
  }
  throw new Error('gave up waiting')
}

const WALLET_REDIRECT = 'attesta-wallet://callback'
const employee = { name: 'Jerry Smith', email: 'jerry@example.com', department: 'burgerzaken' as const, organisation: 'Gemeente Vlierdam', diploma: 'laadpalen-management', diplomaValidUntil: '2031-06-30' }
const diploma = { name: 'Jerry Smith', email: 'jerry@example.com', degree: 'Mathematics', university: 'Trusted University' }

const form = (fields: Record<string, string>) => ({ method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, body: new URLSearchParams(fields) })
const unescape = (s: string) => s.replaceAll('&#38;', '&')

// The holder's way through the browser: the issuer sends them to the login server, they type their email, open the link in the mail and press the button.
describe('a wallet that asks for its credential, with a holder who logs in', () => {
  let issuer: Awaited<ReturnType<typeof startIssuer>>
  let login: Awaited<ReturnType<typeof startLogin>>
  let wallet: WalletAgent
  let issuerUrl: string
  let mails: Mail[]

  beforeEach(async () => {
    mails = []
    const [issuerPort, loginPort] = [await freePort(), await freePort()]
    issuerUrl = `http://localhost:${issuerPort}`
    const loginUrl = `http://localhost:${loginPort}/login`
    login = await startLogin({
      port: loginPort,
      issuer: loginUrl,
      publicUrl: loginUrl,
      basePath: '/login',
      client: { clientId: 'attesta-issuer', clientSecret: 'secret', redirectUris: [`${issuerUrl}/oid4vci/diploma/redirect`] },
      store: new MemoryLoginStore(),
      mailer: { send: async (mail) => void mails.push(mail) },
      seed: 'login test seed',
    })
    issuer = await startIssuer({
      port: issuerPort,
      publicUrl: issuerUrl,
      storeKey: 'login-test-key',
      allowInsecureHttp: true,
      login: { issuer: loginUrl, clientId: 'attesta-issuer', clientSecret: 'secret', walletRedirectUris: [WALLET_REDIRECT] },
    })
    wallet = await createWalletAgent({ storeId: 'wallet', storeKey: 'wallet-test-key', allowInsecureHttp: true })
  })

  afterEach(async () => {
    await wallet.shutdown()
    await issuer.close()
    await login.close()
  })

  /** The wallet starts from the issuer's address and the credential type; the holder logs in as `email`. Returns the credentials the wallet got. */
  async function fetchCredential(type: string, email: string) {
    const holder = wallet.openid4vc.holder
    const offer = `openid-credential-offer://?credential_offer=${encodeURIComponent(
      JSON.stringify({ credential_issuer: `${issuerUrl}/oid4vci/diploma`, credential_configuration_ids: [type], grants: { authorization_code: {} } }),
    )}`
    const resolved = await holder.resolveCredentialOffer(offer)
    const auth = await holder.resolveOpenId4VciAuthorizationRequest(resolved, { clientId: 'attesta-wallet', redirectUri: WALLET_REDIRECT, scope: [type] })
    if (!('authorizationRequestUrl' in auth)) throw new Error('expected a redirect to the browser')

    // The browser: the issuer's authorize address leads to the login server's email page.
    const toLogin = await fetch(auth.authorizationRequestUrl, { redirect: 'manual' })
    const emailPage = await (await fetch(toLogin.headers.get('location')!)).text()
    const loginBase = new URL(toLogin.headers.get('location')!)
    const requestId = /name="request" value="([^"]+)"/.exec(emailPage)![1]
    await fetch(`${loginBase.origin}/login/email`, form({ request: requestId, email }))

    // The mail program: the link opens a page with a button.
    const mail = mails.at(-1)
    if (!mail) throw new Error('no mail was sent')
    const token = new URL(/(https?:\/\/\S+)/.exec(mail.text)![1]).searchParams.get('token')!
    const done = await (await fetch(`${loginBase.origin}/login/confirm`, form({ token }))).text()

    // Back through the issuer to the wallet's own address.
    const back = await fetch(unescape(/<a class="button" href="([^"]+)"/.exec(done)![1]), { redirect: 'manual' })
    const code = new URL(back.headers.get('location')!).searchParams.get('code')!
    const token2 = await holder.requestToken({ resolvedCredentialOffer: resolved, code, clientId: 'attesta-wallet', codeVerifier: auth.codeVerifier, redirectUri: WALLET_REDIRECT })
    const { credentials } = await holder.requestCredentials({
      resolvedCredentialOffer: resolved,
      ...token2,
      clientId: 'attesta-wallet',
      credentialBindingResolver: async () => {
        const { publicJwk } = await wallet.kms.createKey({ type: { kty: 'OKP', crv: 'Ed25519' } })
        return { method: 'jwk', keys: [Kms.PublicJwk.fromPublicJwk(publicJwk)] }
      },
    })
    return credentials
  }

  it('gives the wallet the credential of the entitlement for the email that logged in, and marks it issued', async () => {
    await issuer.createEmployeeOffer(employee)
    const [open] = await issuer.entitlements.findOpenByEmail(employee.email)

    const credentials = await fetchCredential('Employee', 'Jerry@Example.com')

    expect(credentials).toHaveLength(1)
    expect(wallet.sdJwtVc.fromCompact((credentials[0].record as { firstCredential: { compact: string } }).firstCredential.compact).prettyClaims).toMatchObject({
      vct: 'Employee',
      name: 'Jerry Smith',
      email: 'jerry@example.com',
      department: 'burgerzaken',
    })
    expect(await until(() => issuer.entitlements.find(open.id), (e) => e?.status === 'issued')).toMatchObject({ status: 'issued' })
  })

  it('issues nothing to a holder who has no entitlement', async () => {
    await issuer.createEmployeeOffer(employee)

    await expect(fetchCredential('Employee', 'someone.else@example.com')).rejects.toThrow()

    expect(await issuer.entitlements.findOpenByEmail(employee.email)).toHaveLength(1)
    expect(await listCredentials(wallet)).toEqual([])
  })

  it('gives the credential type that was asked for', async () => {
    await issuer.createOffer(diploma)
    await issuer.createEmployeeOffer(employee)

    const credentials = await fetchCredential('Employee', employee.email)

    expect(wallet.sdJwtVc.fromCompact((credentials[0].record as { firstCredential: { compact: string } }).firstCredential.compact).prettyClaims).toMatchObject({ vct: 'Employee' })
    expect((await issuer.entitlements.findOpenByEmail(employee.email)).map((e) => e.type)).toEqual(['Diploma'])
  })

  it('gives an entitlement only once', async () => {
    await issuer.createEmployeeOffer(employee)
    const [open] = await issuer.entitlements.findOpenByEmail(employee.email)
    await fetchCredential('Employee', employee.email)
    await until(() => issuer.entitlements.find(open.id), (e) => e?.status === 'issued')

    await expect(fetchCredential('Employee', employee.email)).rejects.toThrow()
  })
})
