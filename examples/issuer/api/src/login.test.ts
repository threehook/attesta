import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { createWalletAgent, listCredentials, type WalletAgent } from '@attesta/wallet'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { MemoryEntitlementStore } from './entitlements.js'
import { fetchCredential, startTestIssuer, type TestIssuer } from './testing.js'

async function until<T>(read: () => Promise<T>, done: (value: T) => boolean): Promise<T> {
  for (let i = 0; i < 50; i++) {
    const value = await read()
    if (done(value)) return value
    await new Promise((resolve) => setTimeout(resolve, 100))
  }
  throw new Error('gave up waiting')
}

const employee = { name: 'Jerry Smith', email: 'jerry@example.com', department: 'burgerzaken' as const, organisation: 'Gemeente Vlierdam', diploma: 'laadpalen-management', diplomaValidUntil: '2031-06-30' }
const diploma = { name: 'Jerry Smith', email: 'jerry@example.com', degree: 'Mathematics', university: 'Trusted University' }
const denied = { response: { credentialErrorResponseResult: { data: { error: 'credential_request_denied' } } } }

// The holder's whole way: the wallet asks the issuer for a credential, the holder logs in with the link from the mail, and the wallet gets what is theirs.
describe('a wallet that asks for its credential, with a holder who logs in', () => {
  let test: TestIssuer
  let wallet: WalletAgent

  beforeEach(async () => {
    test = await startTestIssuer()
    wallet = await createWalletAgent({ storeId: 'wallet', storeKey: 'wallet-test-key', allowInsecureHttp: true })
  })

  afterEach(async () => {
    await wallet.shutdown()
    await test.close()
  })

  it('gives the wallet the credential of the entitlement for the email that logged in, and marks it issued', async () => {
    await test.issuer.createEmployeeEntitlement(employee)
    const [open] = await test.issuer.entitlements.findOpenByEmail(employee.email)

    const credentials = await fetchCredential(wallet, test, 'Employee', 'Jerry@Example.com')

    expect(credentials).toHaveLength(1)
    expect(credentials[0]).toMatchObject({ type: 'Employee', claims: { name: 'Jerry Smith', email: 'jerry@example.com', department: 'burgerzaken' } })
    expect(await until(() => test.issuer.entitlements.find(open.id), (e) => e?.status === 'issued')).toMatchObject({ status: 'issued' })
  })

  it('issues nothing to a holder who has no entitlement, and says so', async () => {
    await test.issuer.createEmployeeEntitlement(employee)

    await expect(fetchCredential(wallet, test, 'Employee', 'someone.else@example.com')).rejects.toMatchObject(denied)

    expect(await test.issuer.entitlements.findOpenByEmail(employee.email)).toHaveLength(1)
    expect(await listCredentials(wallet)).toEqual([])
  })

  it('gives the credential type that was asked for', async () => {
    await test.issuer.createDiplomaEntitlement(diploma)
    await test.issuer.createEmployeeEntitlement(employee)

    const credentials = await fetchCredential(wallet, test, 'Employee', employee.email)

    expect(credentials[0].type).toBe('Employee')
    expect((await test.issuer.entitlements.findOpenByEmail(employee.email)).map((e) => e.type)).toEqual(['Diploma'])
  })

  it('gives an entitlement only once', async () => {
    await test.issuer.createEmployeeEntitlement(employee)
    const [open] = await test.issuer.entitlements.findOpenByEmail(employee.email)
    await fetchCredential(wallet, test, 'Employee', employee.email)
    await until(() => test.issuer.entitlements.find(open.id), (e) => e?.status === 'issued')

    await expect(fetchCredential(wallet, test, 'Employee', employee.email)).rejects.toMatchObject(denied)
  })

  it('gives a holder who lost their credential a new one only when the organisation makes a new entitlement', async () => {
    const first = await test.issuer.createEmployeeEntitlement(employee)
    await fetchCredential(wallet, test, 'Employee', employee.email)
    await until(() => test.issuer.entitlements.find(first.id), (e) => e?.status === 'issued')
    await expect(fetchCredential(wallet, test, 'Employee', employee.email)).rejects.toMatchObject(denied)

    const second = await test.issuer.createEmployeeEntitlement(employee)
    const again = await fetchCredential(wallet, test, 'Employee', employee.email)

    expect(second.id).not.toBe(first.id)
    expect(again).toHaveLength(1)
    await until(() => test.issuer.entitlements.find(second.id), (e) => e?.status === 'issued')
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
      await test.close()
      const first = await startTestIssuer({ path: dir, seed: 'restart seed', entitlements: store })
      const { id } = await store.create({ email: employee.email, type: 'Employee', claims: { ...employee, description: 'd' } })
      expect(await fetchCredential(wallet, first, 'Employee', employee.email)).toHaveLength(1)
      expect(await store.find(id)).toMatchObject({ status: 'claimed' })
      await first.close()

      store.forget = false
      test = await startTestIssuer({ path: dir, seed: 'restart seed', entitlements: store })

      expect(await store.find(id)).toMatchObject({ status: 'issued' })
    } finally {
      rmSync(dir, { recursive: true, force: true })
    }
  })
})
