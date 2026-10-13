import { randomUUID } from 'node:crypto'
import pg from 'pg'
import { afterAll, afterEach, describe, expect, it } from 'vitest'
import { MemoryEntitlementStore, PostgresEntitlementStore, type EntitlementStore } from './entitlements.js'

// The Postgres store runs against ATTESTA_TEST_POSTGRES=host:port, with ATTESTA_TEST_POSTGRES_USER and _PASSWORD; without it only the in-memory store is tested.
const postgres = process.env.ATTESTA_TEST_POSTGRES
const database = postgres
  ? { host: postgres, user: process.env.ATTESTA_TEST_POSTGRES_USER ?? 'postgres', password: process.env.ATTESTA_TEST_POSTGRES_PASSWORD ?? '' }
  : undefined

const employee = { type: 'Employee', claims: { name: 'Jerry Smith', department: 'burgerzaken' } }
const hour = 3_600_000

function contract(name: string, open: () => Promise<EntitlementStore>) {
  describe(name, () => {
    let store: EntitlementStore | undefined
    let email: string
    const start = async () => {
      store = await open()
      email = `${randomUUID()}@example.com`
      return store
    }
    afterEach(async () => {
      await store?.close()
    })

    it('keeps what it was given, and finds it by email in any case', async () => {
      const s = await start()
      const created = await s.create({ ...employee, email: email.toUpperCase() })

      const found = await s.findOpenByEmail(email)

      expect(found).toEqual([created])
      expect(created).toMatchObject({ email, type: 'Employee', claims: employee.claims, status: 'open' })
      expect(created.issuedAt).toBeUndefined()
    })

    it('finds only the entitlements of that person', async () => {
      const s = await start()
      await s.create({ ...employee, email: `other-${email}` })

      expect(await s.findOpenByEmail(email)).toEqual([])
    })

    it('lists a person\'s entitlements oldest first', async () => {
      const s = await start()
      const first = await s.create({ ...employee, email })
      const second = await s.create({ type: 'Diploma', claims: {}, email })

      expect((await s.findOpenByEmail(email)).map((e) => e.id)).toEqual([first.id, second.id])
    })

    it('hides an entitlement once it is past its end', async () => {
      const s = await start()
      await s.create({ ...employee, email, validUntil: new Date(Date.now() - hour) })
      const current = await s.create({ ...employee, email, validUntil: new Date(Date.now() + hour) })

      expect((await s.findOpenByEmail(email)).map((e) => e.id)).toEqual([current.id])
    })

    it('issues an entitlement once', async () => {
      const s = await start()
      const { id } = await s.create({ ...employee, email })

      expect(await s.markIssued(id)).toBe(true)
      expect(await s.markIssued(id)).toBe(false)
      expect(await s.findOpenByEmail(email)).toEqual([])
    })

    it('lets only one of two simultaneous requests issue it', async () => {
      const s = await start()
      const { id } = await s.create({ ...employee, email })

      const results = await Promise.all([s.markIssued(id), s.markIssued(id)])

      expect(results.filter(Boolean)).toHaveLength(1)
    })

    it('does not issue a revoked entitlement, nor revoke an issued one', async () => {
      const s = await start()
      const revoked = await s.create({ ...employee, email })
      const issued = await s.create({ ...employee, email })

      expect(await s.revoke(revoked.id)).toBe(true)
      expect(await s.markIssued(issued.id)).toBe(true)

      expect(await s.markIssued(revoked.id)).toBe(false)
      expect(await s.revoke(issued.id)).toBe(false)
      expect(await s.findOpenByEmail(email)).toEqual([])
    })

    it('knows nothing of an id it was never given', async () => {
      const s = await start()

      expect(await s.markIssued(randomUUID())).toBe(false)
      expect(await s.revoke(randomUUID())).toBe(false)
    })
  })
}

contract('in-memory entitlements', async () => new MemoryEntitlementStore())

describe.skipIf(!database)('postgres entitlements', () => {
  afterAll(async () => {
    const [host, port] = database!.host.split(':')
    const admin = new pg.Client({ host, port: Number(port ?? 5432), user: database!.user, password: database!.password, database: 'postgres' })
    await admin.connect()
    for (const name of [`entitlements_test_${process.pid}`, `entitlements_test_race_${process.pid}`]) {
      await admin.query(`DROP DATABASE IF EXISTS ${pg.escapeIdentifier(name)} WITH (FORCE)`)
    }
    await admin.end()
  })

  contract('as a store', () => PostgresEntitlementStore.connect(database!, `entitlements_test_${process.pid}`))

  it('keeps entitlements across a restart and migrates only once', async () => {
    const name = `entitlements_test_${process.pid}`
    const first = await PostgresEntitlementStore.connect(database!, name)
    const { id, email } = await first.create({ ...employee, email: `${randomUUID()}@example.com` })
    await first.close()

    const second = await PostgresEntitlementStore.connect(database!, name)
    try {
      expect((await second.findOpenByEmail(email)).map((e) => e.id)).toEqual([id])
    } finally {
      await second.close()
    }
  })

  it('lets several replicas start together', async () => {
    const name = `entitlements_test_race_${process.pid}`
    const stores = await Promise.all([1, 2, 3].map(() => PostgresEntitlementStore.connect(database!, name)))
    await Promise.all(stores.map((s) => s.close()))
  })
})
