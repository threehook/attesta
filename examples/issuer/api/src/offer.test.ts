import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { createWalletAgent, listCredentials, previewCredentialOffer, readQrImage, type WalletAgent } from '@attesta/wallet'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { MemoryEntitlementStore } from './entitlements.js'
import { offerLinkFor, qrImageFor, receive, startTestIssuer, type TestIssuer } from './testing.js'

const employee = { name: 'Jerry Smith', email: 'jerry@example.com', department: 'burgerzaken' as const, organisation: 'Gemeente Vlierdam', diploma: 'laadpalen-management', diplomaValidUntil: '2031-06-30' }
const diploma = { name: 'Jerry Smith', email: 'jerry@example.com', degree: 'Mathematics', university: 'Trusted University' }
const post = (url: string, body: unknown) => fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
const denied = { response: { credentialErrorResponseResult: { data: { error: 'credential_request_denied' } } } }

async function until<T>(read: () => Promise<T>, done: (value: T) => boolean): Promise<T> {
  for (let i = 0; i < 50; i++) {
    const value = await read()
    if (done(value)) return value
    await new Promise((resolve) => setTimeout(resolve, 100))
  }
  throw new Error('gave up waiting')
}

// A credential reaches a person in a QR code in a mail; the wallet reads the code and does the rest.
describe('a QR code that is mailed', () => {
  let test: TestIssuer
  let wallet: WalletAgent

  beforeEach(async () => {
    test = await startTestIssuer({ issuerName: 'Gemeente Utrecht' })
    wallet = await createWalletAgent({ storeId: 'wallet', storeKey: 'wallet-test-key', allowInsecureHttp: true })
  })

  afterEach(async () => {
    await wallet.shutdown()
    await test.close()
  })

  it('mails the person a QR code as an image, in the mail and as an attachment', async () => {
    await test.issuer.sendOffer(await test.issuer.createEmployeeEntitlement(employee))

    expect(test.sent).toHaveLength(1)
    const mail = test.sent[0]
    expect(mail).toMatchObject({ to: 'jerry@example.com', subject: 'Uw credential staat klaar bij Gemeente Utrecht' })
    expect(mail.attachments).toHaveLength(2)
    expect(mail.attachments![0]).toMatchObject({ filename: 'credential.png', contentType: 'image/png' })
    expect(mail.attachments![0].cid).toBeUndefined()
    expect(mail.attachments![1]).toMatchObject({ contentType: 'image/png', cid: 'credential-qr' })
    expect(mail.attachments![1].content).toEqual(mail.attachments![0].content)
    expect(mail.html).toContain('cid:credential-qr')
    expect(mail.text).toContain('24 uur')
    expect(mail.text).toContain('QR-code uploaden')
  })

  it('holds the offer in the code, and nothing secret in the text of the mail', async () => {
    await test.issuer.sendOffer(await test.issuer.createEmployeeEntitlement(employee))

    const link = offerLinkFor(test, 'jerry@example.com')

    expect(link).toMatch(new RegExp(`^openid-credential-offer://\\?credential_offer_uri=${encodeURIComponent(`${test.url}/oid4vci/diploma/offers/`)}`))
    expect(test.sent[0].text).not.toContain('credential_offer')
    expect(test.sent[0].html).not.toContain('credential_offer')
  })

  it('gives the wallet the credential, and marks the entitlement issued', async () => {
    const entitlement = await test.issuer.createEmployeeEntitlement(employee)
    await test.issuer.sendOffer(entitlement)

    const accepted = await receive(wallet, test, employee.email)

    expect(accepted[0]).toMatchObject({ type: 'Employee', claims: { name: 'Jerry Smith', email: 'jerry@example.com', department: 'burgerzaken' } })
    expect(await until(() => test.issuer.entitlements.find(entitlement.id), (e) => e?.status === 'issued')).toMatchObject({ status: 'issued' })
  })

  it('does the same for every credential type', async () => {
    await test.issuer.sendOffer(await test.issuer.createDiplomaEntitlement(diploma))

    const accepted = await receive(wallet, test, diploma.email)

    expect(accepted[0]).toMatchObject({ type: 'Diploma', claims: { degree: 'Mathematics' } })
  })

  it('works once', async () => {
    await test.issuer.sendOffer(await test.issuer.createEmployeeEntitlement(employee))
    await receive(wallet, test, employee.email)

    await expect(receive(wallet, test, employee.email)).rejects.toThrow()
  })

  it('issues nothing for an entitlement that was revoked after the mail', async () => {
    const entitlement = await test.issuer.createEmployeeEntitlement(employee)
    await test.issuer.sendOffer(entitlement)
    await test.issuer.entitlements.revoke(entitlement.id)

    await expect(receive(wallet, test, employee.email)).rejects.toMatchObject(denied)

    expect(await listCredentials(wallet)).toEqual([])
  })

  it('gives a holder who lost their credential a new one only when the organisation makes a new entitlement', async () => {
    const first = await test.issuer.createEmployeeEntitlement(employee)
    await test.issuer.sendOffer(first)
    await receive(wallet, test, employee.email)
    await until(() => test.issuer.entitlements.find(first.id), (e) => e?.status === 'issued')
    await expect(receive(wallet, test, employee.email)).rejects.toThrow()

    const second = await test.issuer.createEmployeeEntitlement(employee)
    await test.issuer.sendOffer(second)
    const again = await receive(wallet, test, employee.email)

    expect(second.id).not.toBe(first.id)
    expect(again).toHaveLength(1)
    await until(() => test.issuer.entitlements.find(second.id), (e) => e?.status === 'issued')
  })

  it('gives the holder their entitlement again after an attempt that did not end in a credential', async () => {
    const short = await startTestIssuer({ entitlementLeaseMs: 300 })
    try {
      const { id } = await short.issuer.createEmployeeEntitlement(employee)
      // An attempt that took the entitlement and then broke off.
      await short.issuer.entitlements.claim(id)
      await short.issuer.sendOffer((await short.issuer.entitlements.find(id))!)
      await expect(receive(wallet, short, employee.email)).rejects.toMatchObject(denied)

      await new Promise((resolve) => setTimeout(resolve, 400))
      await short.issuer.sendOffer((await short.issuer.entitlements.find(id))!)

      expect((await receive(wallet, short, employee.email))[0].type).toBe('Employee')
    } finally {
      await short.close()
    }
  })

  it('marks an entitlement issued after a restart, when the issuer stopped before it could', async () => {
    // Until told otherwise this store cannot record that a credential was made, as if the issuer had stopped just then.
    class Forgetful extends MemoryEntitlementStore {
      forget = true
      override async markIssued(id: string) {
        return this.forget ? false : super.markIssued(id)
      }
    }
    const store = new Forgetful()
    const dir = mkdtempSync(join(tmpdir(), 'issuer-'))
    try {
      const first = await startTestIssuer({ path: dir, seed: 'restart seed', entitlements: store })
      const entitlement = await first.issuer.createEmployeeEntitlement(employee)
      await first.issuer.sendOffer(entitlement)
      expect(await receive(wallet, first, employee.email)).toHaveLength(1)
      expect(await store.find(entitlement.id)).toMatchObject({ status: 'claimed' })
      await first.close()

      store.forget = false
      const second = await startTestIssuer({ path: dir, seed: 'restart seed', entitlements: store })
      await second.close()

      expect(await store.find(entitlement.id)).toMatchObject({ status: 'issued' })
    } finally {
      rmSync(dir, { recursive: true, force: true })
    }
  })
})

describe('a QR code that has expired', () => {
  let wallet: WalletAgent
  let test: TestIssuer

  beforeEach(async () => {
    test = await startTestIssuer({ offerLifetimeSeconds: 1, renewalCooldownMs: 1500 })
    wallet = await createWalletAgent({ storeId: 'wallet', storeKey: 'wallet-test-key', allowInsecureHttp: true })
  })

  afterEach(async () => {
    await wallet.shutdown()
    await test.close()
  })

  const expire = () => new Promise((resolve) => setTimeout(resolve, 1500))

  it('makes the issuer mail a new QR code, which works', async () => {
    await test.issuer.sendOffer(await test.issuer.createEmployeeEntitlement(employee))
    const old = offerLinkFor(test, employee.email)
    await expire()

    await expect(previewCredentialOffer(wallet, old)).rejects.toThrow('De QR-code is verlopen. Er is een nieuwe QR-code naar uw e-mail gestuurd.')

    expect(test.sent).toHaveLength(2)
    expect(qrImageFor(test, employee.email)).not.toEqual(test.sent[0].attachments![0].content)
    expect((await receive(wallet, test, employee.email))[0].type).toBe('Employee')
  })

  it('tells the wallet that a new code was sent', async () => {
    await test.issuer.sendOffer(await test.issuer.createEmployeeEntitlement(employee))
    const old = offerLinkFor(test, employee.email)
    await expire()

    const res = await fetch(decodeURIComponent(old.split('credential_offer_uri=')[1]))

    expect(res.status).toBe(410)
    expect(await res.json()).toEqual({ error: 'offer_expired', error_description: 'De QR-code is verlopen. Er is een nieuwe QR-code naar uw e-mail gestuurd.' })
  })

  it('does not mail again within the cooldown, and says one was sent', async () => {
    await test.issuer.sendOffer(await test.issuer.createEmployeeEntitlement(employee))
    const old = decodeURIComponent(offerLinkFor(test, employee.email).split('credential_offer_uri=')[1])
    await expire()
    await fetch(old)

    const again = await fetch(old)

    expect(again.status).toBe(410)
    expect(((await again.json()) as { error_description: string }).error_description).toContain('al een nieuwe')
    expect(test.sent).toHaveLength(2)
  })

  it('mails again once the cooldown is over', async () => {
    await test.issuer.sendOffer(await test.issuer.createEmployeeEntitlement(employee))
    const old = decodeURIComponent(offerLinkFor(test, employee.email).split('credential_offer_uri=')[1])
    await expire()
    await fetch(old)
    await expire()

    await fetch(old)

    expect(test.sent).toHaveLength(3)
  })

  it('mails nothing when the credential was already fetched or withdrawn', async () => {
    const fetched = await test.issuer.createEmployeeEntitlement(employee)
    await test.issuer.sendOffer(fetched)
    const old = decodeURIComponent(offerLinkFor(test, employee.email).split('credential_offer_uri=')[1])
    await test.issuer.entitlements.revoke(fetched.id)
    await expire()

    const res = await fetch(old)

    expect(res.status).toBe(410)
    expect(((await res.json()) as { error: string }).error).toBe('offer_gone')
    expect(test.sent).toHaveLength(1)
  })

  it('knows nothing of an offer that never existed', async () => {
    const res = await fetch(`${test.url}/oid4vci/diploma/offers/00000000-0000-4000-8000-000000000000`)

    expect(res.status).toBe(404)
    expect(test.sent).toHaveLength(0)
  })
})

describe('recording an entitlement over HTTP', () => {
  let test: TestIssuer

  beforeEach(async () => {
    test = await startTestIssuer()
  })

  afterEach(async () => {
    await test.close()
  })

  it('mails the QR code and says so', async () => {
    const res = await post(`${test.url}/entitlements/employee`, employee)

    expect(res.status).toBe(200)
    const body = (await res.json()) as { entitlementId: string; mailedTo: string }
    expect(body).toEqual({ entitlementId: expect.any(String), mailedTo: 'jerry@example.com' })
    expect(await test.issuer.entitlements.find(body.entitlementId)).toMatchObject({ status: 'open' })
    expect(qrImageFor(test, 'jerry@example.com').length).toBeGreaterThan(0)
  })

  it('mails again, with a new code, when the same entitlement is recorded again', async () => {
    const first = (await (await post(`${test.url}/entitlements/employee`, employee)).json()) as { entitlementId: string }
    const second = (await (await post(`${test.url}/entitlements/employee`, employee)).json()) as { entitlementId: string }

    expect(second.entitlementId).toBe(first.entitlementId)
    expect(test.sent).toHaveLength(2)
    expect(readQrImage(test.sent[0].attachments![0].content)).not.toBe(readQrImage(test.sent[1].attachments![0].content))
    expect(await test.issuer.entitlements.findOpenByEmail(employee.email)).toHaveLength(1)
  })

  it('keeps the entitlement and says so when the mail cannot be sent', async () => {
    const broken = await startTestIssuer({
      mailer: {
        send: async () => {
          throw new Error('mail server down')
        },
      },
    })
    try {
      const res = await post(`${broken.url}/entitlements/employee`, employee)

      expect(res.status).toBe(502)
      const body = (await res.json()) as { error: string; entitlementId: string }
      expect(body.error).toContain('e-mail kon niet worden verstuurd')
      expect(await broken.issuer.entitlements.find(body.entitlementId)).toMatchObject({ status: 'open' })
    } finally {
      await broken.close()
    }
  })

  it('has no way to log in for a credential', async () => {
    expect((await fetch(`${test.url}/login/authorize`)).status).toBe(404)
  })
})
