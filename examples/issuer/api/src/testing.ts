// What tests need to get a credential into a wallet the way a holder does: an issuer whose mail is kept in memory, and the QR code in that mail read back
// the way the wallet reads it. Nothing in the issuer itself depends on this.
import { createServer } from 'node:net'
import { acceptCredentialOffer, type HeldCredential, readQrImage, type WalletAgent } from '@attesta/wallet'
import { type IssuerOptions, startIssuer } from './issuer.js'
import type { Mail } from './mail.js'

export async function freePort(): Promise<number> {
  const server = createServer()
  await new Promise<void>((resolve) => server.listen(0, resolve))
  const { port } = server.address() as { port: number }
  await new Promise((resolve) => server.close(resolve))
  return port
}

export interface TestIssuer {
  issuer: Awaited<ReturnType<typeof startIssuer>>
  /** The address wallets reach the issuer at. */
  url: string
  /** The mails the issuer sent, newest last. */
  sent: Mail[]
  close(): Promise<void>
}

/** Starts an issuer that keeps its mail in memory. `extra` adds to or replaces the issuer's options. */
export async function startTestIssuer(extra: Partial<IssuerOptions> = {}): Promise<TestIssuer> {
  const sent: Mail[] = []
  const port = await freePort()
  const url = `http://localhost:${port}`
  const issuer = await startIssuer({
    port,
    publicUrl: url,
    storeKey: 'test-issuer-key',
    allowInsecureHttp: true,
    mailer: { send: async (mail) => void sent.push(mail) },
    ...extra,
  })
  return { issuer, url, sent, close: () => issuer.close() }
}

/** The QR code image in the newest mail to `to`, as the person would save it from the mail. */
export function qrImageFor(test: TestIssuer, to: string): Buffer {
  const mail = [...test.sent].reverse().find((m) => m.to.toLowerCase() === to.toLowerCase())
  const attachment = mail?.attachments?.find((a) => a.contentType === 'image/png')
  if (!attachment) throw new Error(`no mail with a QR code was sent to ${to}`)
  return attachment.content
}

/** The offer link in the QR code of the newest mail to `to`. */
export const offerLinkFor = (test: TestIssuer, to: string): string => readQrImage(qrImageFor(test, to))

/** The wallet takes the QR code from the newest mail to `to` and fetches the credential. */
export const receive = (wallet: WalletAgent, test: TestIssuer, to: string): Promise<HeldCredential[]> => acceptCredentialOffer(wallet, offerLinkFor(test, to))

/** The issuer mails the person's open entitlement of this type, as it does when it is recorded, and the wallet fetches the credential from the QR code. */
export async function fetchCredential(wallet: WalletAgent, test: TestIssuer, type: string, email: string): Promise<HeldCredential[]> {
  const entitlement = (await test.issuer.entitlements.findOpenByEmail(email)).find((e) => e.type === type)
  if (!entitlement) throw new Error(`there is no open ${type} entitlement for ${email}`)
  await test.issuer.sendOffer(entitlement)
  return receive(wallet, test, email)
}
