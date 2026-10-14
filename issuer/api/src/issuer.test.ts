import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { createWalletAgent, listCredentials, type WalletAgent } from '@attesta/wallet'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { DEFAULT_DESCRIPTIONS, endOfDay, type EmployeeClaims } from './issuer.js'
import { fetchCredential, receive, startTestIssuer, type TestIssuer } from './testing.js'

async function until<T>(read: () => Promise<T>, done: (value: T) => boolean): Promise<T> {
  for (let i = 0; i < 50; i++) {
    const value = await read()
    if (done(value)) return value
    await new Promise((resolve) => setTimeout(resolve, 100))
  }
  throw new Error('gave up waiting')
}

const LEASE_MS = 300

const employee: EmployeeClaims = {
  name: 'Jerry Smith',
  email: 'jerry@example.com',
  department: 'burgerzaken',
  organisation: 'Gemeente Vlierdam',
  diploma: 'laadpalen-management',
  diplomaValidUntil: '2031-06-30',
}
const diploma = { name: 'Ada Lovelace', email: 'ada@example.com', degree: 'Mathematics', university: 'Trusted University' }

const post = (url: string, body: unknown) => fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })

describe('issuer and wallet', () => {
  let test: TestIssuer
  let wallet: WalletAgent

  beforeEach(async () => {
    test = await startTestIssuer({ issuerName: 'Gemeente Utrecht', entitlementLeaseMs: LEASE_MS })
    wallet = await createWalletAgent({ storeId: 'wallet', storeKey: 'wallet-test-key', allowInsecureHttp: true })
  })

  afterEach(async () => {
    await wallet.shutdown()
    await test.close()
  })

  it('issues a diploma that the wallet keeps', async () => {
    await test.issuer.createDiplomaEntitlement(diploma)

    const accepted = await fetchCredential(wallet, test, 'Diploma', diploma.email)

    expect(accepted).toHaveLength(1)
    expect(accepted[0].type).toBe('Diploma')
    expect(accepted[0].issuer).toMatch(/^did:key:/)
    expect(accepted[0].claims).toMatchObject({ name: 'Ada Lovelace', email: 'ada@example.com', degree: 'Mathematics', university: 'Trusted University' })
    expect(await listCredentials(wallet)).toEqual(accepted)
  })

  it('records an entitlement over HTTP, and the holder can then fetch it', async () => {
    const res = await post(`${test.url}/entitlements/diploma`, { name: 'Grace Hopper', email: 'grace@example.com', degree: 'Computer Science', university: 'Trusted University' })
    expect(res.status).toBe(200)
    const { entitlementId } = (await res.json()) as { entitlementId: string }

    expect(await test.issuer.entitlements.find(entitlementId)).toMatchObject({ email: 'grace@example.com', type: 'Diploma', status: 'open' })
    const accepted = await receive(wallet, test, 'grace@example.com')
    expect(accepted[0].claims).toMatchObject({ name: 'Grace Hopper' })
  })

  it('rejects an entitlement request with missing fields', async () => {
    expect((await post(`${test.url}/entitlements/diploma`, { name: 'Ada', email: 'ada@example.com' })).status).toBe(400)
  })

  it('rejects an email that is not an address', async () => {
    expect((await post(`${test.url}/entitlements/diploma`, { ...diploma, email: 'not-an-address' })).status).toBe(400)
  })

  it('has no endpoint that makes a link', async () => {
    expect((await post(`${test.url}/offers`, diploma)).status).toBe(404)
    expect((await post(`${test.url}/employee/offers`, employee)).status).toBe(404)
  })

  it('derives the same DID from the same seed', async () => {
    const start = async (seed?: string) => {
      const started = await startTestIssuer({ seed })
      const did = started.issuer.did
      await started.close()
      return did
    }

    const first = await start('a seed')
    expect(await start('a seed')).toBe(first)
    expect(await start('another seed')).not.toBe(first)
    expect(await start()).not.toBe(first)
  })

  it('issues an employee credential that expires with the diploma', async () => {
    await test.issuer.createEmployeeEntitlement(employee)

    const accepted = await fetchCredential(wallet, test, 'Employee', employee.email)

    expect(accepted).toHaveLength(1)
    expect(accepted[0].type).toBe('Employee')
    expect(accepted[0].issuer).toBe(test.issuer.did)
    expect(accepted[0].claims).toMatchObject({
      name: 'Jerry Smith',
      email: 'jerry@example.com',
      department: 'burgerzaken',
      organisation: 'Gemeente Vlierdam',
      diploma: 'laadpalen-management',
      exp: endOfDay('2031-06-30'),
    })
  })

  it('puts the logical issuer and a description in the credential', async () => {
    await test.issuer.createEmployeeEntitlement({ ...employee, description: 'Own text' })
    await test.issuer.createEmployeeEntitlement({ ...employee, email: 'plain@example.com' })

    const accepted = await fetchCredential(wallet, test, 'Employee', employee.email)
    const defaulted = await fetchCredential(wallet, test, 'Employee', 'plain@example.com')

    expect(accepted[0].claims).toMatchObject({ issuer_name: 'Gemeente Utrecht', description: 'Own text' })
    expect(defaulted[0].claims).toMatchObject({ description: DEFAULT_DESCRIPTIONS.Employee })
  })

  it('names the logical issuer in the issuer metadata', async () => {
    const meta = (await (await fetch(`${test.url}/oid4vci/diploma/.well-known/openid-credential-issuer`)).json()) as { display?: { name?: string }[] }

    expect(meta.display).toEqual([{ name: 'Gemeente Utrecht' }])
  })

  it('tells the pages which issuer it is and what the forms ask for', async () => {
    const info = await (await fetch(`${test.url}/info`)).json()

    expect(info).toEqual({
      issuerName: 'Gemeente Utrecht',
      did: test.issuer.did,
      departments: ['burgerzaken', 'secretariaat'],
      descriptions: DEFAULT_DESCRIPTIONS,
    })
  })

  it('records an entitlement with the claims on file, and marks it issued once the wallet has the credential', async () => {
    await test.issuer.createEmployeeEntitlement(employee)
    const [open] = await test.issuer.entitlements.findOpenByEmail('JERRY@example.com')
    expect(open).toMatchObject({ type: 'Employee', status: 'open', claims: { name: 'Jerry Smith', department: 'burgerzaken', description: DEFAULT_DESCRIPTIONS.Employee } })

    const accepted = await fetchCredential(wallet, test, 'Employee', employee.email)

    expect(accepted[0].claims).toMatchObject({ name: 'Jerry Smith', email: 'jerry@example.com', department: 'burgerzaken' })
    expect(await test.issuer.entitlements.findOpenByEmail('jerry@example.com')).toEqual([])
    expect(await until(() => test.issuer.entitlements.find(open.id), (e) => e?.status === 'issued')).toMatchObject({ status: 'issued' })
  })

  it('issues nothing for an entitlement that was revoked', async () => {
    const entitlement = await test.issuer.createEmployeeEntitlement(employee)
    await test.issuer.mailQrCode(entitlement)
    await test.issuer.entitlements.revoke(entitlement.id)

    await expect(receive(wallet, test, employee.email)).rejects.toMatchObject({
      response: { credentialErrorResponseResult: { data: { error: 'credential_request_denied' } } },
    })

    expect(await listCredentials(wallet)).toEqual([])
  })

  it('makes one entitlement for the same person and credential, and another for a different one', async () => {
    await test.issuer.createEmployeeEntitlement(employee)
    await test.issuer.createEmployeeEntitlement(employee)
    await test.issuer.createEmployeeEntitlement({ ...employee, diploma: 'other-course' })

    expect(await test.issuer.entitlements.findOpenByEmail(employee.email)).toHaveLength(2)
  })

  it('answers an entitlement request with the entitlement it made', async () => {
    const body = (await (await post(`${test.url}/entitlements/employee`, employee)).json()) as { entitlementId: string }

    expect(await test.issuer.entitlements.find(body.entitlementId)).toMatchObject({ email: 'jerry@example.com', status: 'open' })
  })

  it('lists each credential type under its own name', async () => {
    const meta = (await (await fetch(`${test.url}/oid4vci/diploma/.well-known/openid-credential-issuer`)).json()) as { credential_configurations_supported: Record<string, unknown> }

    expect(Object.keys(meta.credential_configurations_supported).sort()).toEqual(['Diploma', 'Employee'])
  })

  it.each([
    ['a missing field', { ...employee, organisation: '' }],
    ['an email that is not an address', { ...employee, email: 'nope' }],
    ['an unknown department', { ...employee, department: 'bestuursbureau' }],
    ['a date that is not a date', { ...employee, diplomaValidUntil: '2031/06/30' }],
    ['a month that does not exist, in dd-mm-yyyy', { ...employee, diplomaValidUntil: '30-13-2031' }],
    ['a day that does not exist', { ...employee, diplomaValidUntil: '2031-13-45' }],
    ['a date in the past', { ...employee, diplomaValidUntil: '2020-01-01' }],
  ])('rejects an employee entitlement with %s', async (_name, body) => {
    expect((await post(`${test.url}/entitlements/employee`, body)).status).toBe(400)
  })

  it('accepts the valid-until date as dd-mm-yyyy', async () => {
    await post(`${test.url}/entitlements/employee`, { ...employee, diplomaValidUntil: '30-06-2031' })

    expect((await receive(wallet, test, employee.email))[0].claims).toMatchObject({ exp: endOfDay('2031-06-30') })
  })

  it('still issues both credential types after a restart on the same store', async () => {
    const dir = mkdtempSync(join(tmpdir(), 'issuer-'))
    try {
      const first = await startTestIssuer({ path: dir, seed: 'restart seed' })
      await first.issuer.createDiplomaEntitlement({ ...diploma, email: 'a@example.com' })
      await first.close()

      const second = await startTestIssuer({ path: dir, seed: 'restart seed' })
      try {
        await second.issuer.createDiplomaEntitlement({ ...diploma, email: 'a@example.com' })
        await second.issuer.createEmployeeEntitlement(employee)
        const diplomaCredential = await fetchCredential(wallet, second, 'Diploma', 'a@example.com')
        const staff = await fetchCredential(wallet, second, 'Employee', employee.email)
        expect([diplomaCredential[0].type, staff[0].type]).toEqual(['Diploma', 'Employee'])
      } finally {
        await second.close()
      }
    } finally {
      rmSync(dir, { recursive: true, force: true })
    }
  })
})

// Runs against ATTESTA_TEST_POSTGRES=host:port with ATTESTA_TEST_POSTGRES_USER and _PASSWORD; skipped without it.
describe.skipIf(!process.env.ATTESTA_TEST_POSTGRES)('issuer on postgres', () => {
  it('keeps its store and its entitlements in Postgres, and issues after a restart', async () => {
    const database = {
      host: process.env.ATTESTA_TEST_POSTGRES!,
      user: process.env.ATTESTA_TEST_POSTGRES_USER ?? 'postgres',
      password: process.env.ATTESTA_TEST_POSTGRES_PASSWORD ?? '',
    }
    const wallet = await createWalletAgent({ storeId: 'wallet-pg', storeKey: 'wallet-test-key', allowInsecureHttp: true })
    const first = await startTestIssuer({ database, seed: 'postgres seed', storeKey: 'k' })
    const { id } = await first.issuer.createEmployeeEntitlement({ ...employee, email: 'waiting@example.com' })
    const did = first.issuer.did
    await first.close()

    const second = await startTestIssuer({ database, seed: 'postgres seed', storeKey: 'k' })
    try {
      expect(second.issuer.did).toBe(did)
      expect((await second.issuer.entitlements.findOpenByEmail('waiting@example.com')).map((e) => e.id)).toContain(id)
      const staff = await fetchCredential(wallet, second, 'Employee', 'waiting@example.com')
      expect(staff[0].claims).toMatchObject({ email: 'waiting@example.com' })
      expect(await until(() => second.issuer.entitlements.find(id), (e) => e?.status === 'issued')).toMatchObject({ status: 'issued' })
    } finally {
      await second.close()
      await wallet.shutdown()
    }
  })
})
