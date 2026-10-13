import { randomUUID } from 'node:crypto'
import pg from 'pg'
import { afterAll, afterEach, describe, expect, it } from 'vitest'
import { type LoginStore, MemoryLoginStore, PostgresLoginStore } from './store.js'

// The Postgres store runs against ATTESTA_TEST_POSTGRES=host:port, with ATTESTA_TEST_POSTGRES_USER and _PASSWORD; without it only the in-memory store is tested.
const postgres = process.env.ATTESTA_TEST_POSTGRES
const database = postgres
  ? { host: postgres, user: process.env.ATTESTA_TEST_POSTGRES_USER ?? 'postgres', password: process.env.ATTESTA_TEST_POSTGRES_PASSWORD ?? '' }
  : undefined

const hour = 3_600_000
const input = () => ({ clientId: 'c', redirectUri: 'https://x/cb', state: 's', codeChallenge: 'ch', scope: 'openid email', nonce: undefined, expiresAt: new Date(Date.now() + hour) })
const later = (ms = hour) => new Date(Date.now() + ms)
const unique = (label: string) => `${label}-${randomUUID()}`

function contract(name: string, open: () => Promise<LoginStore>) {
  describe(name, () => {
    let store: LoginStore | undefined
    const start = async () => (store = await open())
    afterEach(async () => {
      await store?.close()
    })

    it('keeps a request and finds it again', async () => {
      const s = await start()
      const made = await s.create(input())

      expect(await s.find(made.id)).toEqual(made)
      expect(made).toMatchObject({ status: 'pending', mailsSent: 0, state: 's', scope: 'openid email' })
      expect(await s.find(randomUUID())).toBeUndefined()
    })

    it('turns a mailed link into a code once', async () => {
      const s = await start()
      const { id } = await s.create(input())
      const token = randomUUID()
      expect(await s.mailed(id, 'Jerry@Example.com', token, later(), 3)).toBe(true)

      const confirmed = await s.confirm(token, 'code-1', later())

      expect(confirmed).toMatchObject({ id, status: 'confirmed', email: 'jerry@example.com' })
      expect(await s.confirm(token, 'code-2', later())).toBeUndefined()
    })

    it('does not confirm a link that is unknown or expired', async () => {
      const s = await start()
      const old = unique('old')
      const { id } = await s.create(input())
      await s.mailed(id, 'a@example.com', old, later(-1000), 3)

      expect(await s.confirm(old, unique('c'), later())).toBeUndefined()
      expect(await s.confirm('never-sent', unique('c'), later())).toBeUndefined()
    })

    it('replaces the earlier link when another is mailed, and stops at the limit', async () => {
      const s = await start()
      const [first, second, third] = [unique('first'), unique('second'), unique('third')]
      const { id } = await s.create(input())
      await s.mailed(id, 'a@example.com', first, later(), 2)
      await s.mailed(id, 'a@example.com', second, later(), 2)

      expect(await s.mailed(id, 'a@example.com', third, later(), 2)).toBe(false)
      expect(await s.confirm(first, unique('c'), later())).toBeUndefined()
      expect(await s.confirm(second, unique('c'), later())).toBeDefined()
    })

    it('does not mail for a request that ran out or was confirmed', async () => {
      const s = await start()
      const [t, x, y] = [unique('t'), unique('x'), unique('y')]
      const gone = await s.create({ ...input(), expiresAt: later(-1000) })
      const done = await s.create(input())
      await s.mailed(done.id, 'a@example.com', t, later(), 3)
      await s.confirm(t, unique('c'), later())

      expect(await s.mailed(gone.id, 'a@example.com', x, later(), 3)).toBe(false)
      expect(await s.mailed(done.id, 'a@example.com', y, later(), 3)).toBe(false)
    })

    it('redeems a code once, and not after it ran out', async () => {
      const s = await start()
      const [t, t2, code, late] = [unique('t'), unique('t2'), unique('code'), unique('late')]
      const { id } = await s.create(input())
      await s.mailed(id, 'a@example.com', t, later(), 3)
      await s.confirm(t, code, later())

      expect(await s.redeem(code)).toMatchObject({ id, status: 'used', email: 'a@example.com' })
      expect(await s.redeem(code)).toBeUndefined()

      const other = await s.create(input())
      await s.mailed(other.id, 'b@example.com', t2, later(), 3)
      await s.confirm(t2, late, later(-1000))
      expect(await s.redeem(late)).toBeUndefined()
    })

    it('lets only one of two simultaneous requests redeem a code', async () => {
      const s = await start()
      const [t, code] = [unique('t'), unique('code')]
      const { id } = await s.create(input())
      await s.mailed(id, 'a@example.com', t, later(), 3)
      await s.confirm(t, code, later())

      const results = await Promise.all([s.redeem(code), s.redeem(code)])

      expect(results.filter(Boolean)).toHaveLength(1)
    })

    it('counts the mails to an address in a window', async () => {
      const s = await start()
      const address = `${randomUUID()}@example.com`
      await s.recordMail(address.toUpperCase())
      await s.recordMail(address)

      expect(await s.mailsSince(address, later(-hour))).toBe(2)
      expect(await s.mailsSince(address, later(hour))).toBe(0)
      expect(await s.mailsSince(`other-${address}`, later(-hour))).toBe(0)
    })
  })
}

contract('in-memory login store', async () => new MemoryLoginStore())

describe.skipIf(!database)('postgres login store', () => {
  const name = `login_test_${process.pid}`
  afterAll(async () => {
    const [host, port] = database!.host.split(':')
    const admin = new pg.Client({ host, port: Number(port ?? 5432), user: database!.user, password: database!.password, database: 'postgres' })
    await admin.connect()
    await admin.query(`DROP DATABASE IF EXISTS ${pg.escapeIdentifier(name)} WITH (FORCE)`)
    await admin.end()
  })

  contract('as a store', () => PostgresLoginStore.connect(database!, name))
})
