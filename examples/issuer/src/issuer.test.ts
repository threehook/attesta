import { createServer } from 'node:net'
import { acceptCredentialOffer, createWalletAgent, listCredentials, previewCredentialOffer, type WalletAgent } from '@attesta/wallet'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { DEFAULT_DESCRIPTIONS, endOfDay, startIssuer, type EmployeeClaims } from './issuer.js'

async function freePort(): Promise<number> {
  const server = createServer()
  await new Promise<void>((resolve) => server.listen(0, resolve))
  const { port } = server.address() as { port: number }
  await new Promise((resolve) => server.close(resolve))
  return port
}

const employee: EmployeeClaims = {
  name: 'Jerry Smith',
  email: 'jerry@example.com',
  department: 'burgerzaken',
  organisation: 'Gemeente Vlierdam',
  diploma: 'laadpalen-management',
  diplomaValidUntil: '2031-06-30',
}

describe('diploma issuer and wallet', () => {
  let issuer: Awaited<ReturnType<typeof startIssuer>>
  let wallet: WalletAgent
  let baseUrl: string

  beforeEach(async () => {
    const port = await freePort()
    baseUrl = `http://localhost:${port}`
    issuer = await startIssuer({ port, publicUrl: baseUrl, storeKey: 'issuer-test-key', allowInsecureHttp: true, issuerName: 'Gemeente Utrecht' })
    wallet = await createWalletAgent({ storeId: 'wallet', storeKey: 'wallet-test-key', allowInsecureHttp: true })
  })

  afterEach(async () => {
    await wallet.shutdown()
    await issuer.close()
  })

  it('issues a diploma that the wallet accepts and keeps', async () => {
    const offerUri = await issuer.createOffer({ name: 'Ada Lovelace', email: 'ada@example.com', degree: 'Mathematics', university: 'Trusted University' })

    const accepted = await acceptCredentialOffer(wallet, offerUri)

    expect(accepted).toHaveLength(1)
    expect(accepted[0].type).toBe('Diploma')
    expect(accepted[0].issuer).toMatch(/^did:key:/)
    expect(accepted[0].claims).toMatchObject({ name: 'Ada Lovelace', email: 'ada@example.com', degree: 'Mathematics', university: 'Trusted University' })

    expect(await listCredentials(wallet)).toEqual(accepted)
  })

  it('shows what an offer contains before it is accepted', async () => {
    const offerUri = await issuer.createOffer({ name: 'Ada Lovelace', email: 'ada@example.com', degree: 'Mathematics', university: 'Trusted University' })

    const offer = await previewCredentialOffer(wallet, offerUri)

    expect(offer.issuer).toContain(baseUrl)
    expect(offer.types).toEqual(['Diploma'])
    expect(await listCredentials(wallet)).toEqual([])
  })

  it('creates offers over HTTP', async () => {
    const res = await fetch(`${baseUrl}/offers`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
      body: JSON.stringify({ name: 'Grace Hopper', email: 'grace@example.com', degree: 'Computer Science', university: 'Trusted University' }),
    })
    expect(res.status).toBe(200)
    const { offerUri } = (await res.json()) as { offerUri: string }

    const accepted = await acceptCredentialOffer(wallet, offerUri)
    expect(accepted[0].claims).toMatchObject({ name: 'Grace Hopper' })
  })

  it('rejects an offer request with missing fields', async () => {
    const res = await fetch(`${baseUrl}/offers`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
      body: JSON.stringify({ name: 'Ada', email: 'ada@example.com' }),
    })
    expect(res.status).toBe(400)
  })

  it('rejects an email that is not an address', async () => {
    const res = await fetch(`${baseUrl}/offers`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
      body: JSON.stringify({ name: 'Ada', email: 'not-an-address', degree: 'Mathematics', university: 'Trusted University' }),
    })
    expect(res.status).toBe(400)
  })

  it('does not honour an offer twice', async () => {
    const offerUri = await issuer.createOffer({ name: 'Ada Lovelace', email: 'ada@example.com', degree: 'Mathematics', university: 'Trusted University' })
    await acceptCredentialOffer(wallet, offerUri)

    await expect(acceptCredentialOffer(wallet, offerUri)).rejects.toThrow()
  })

  it('derives the same DID from the same seed', async () => {
    const start = async (seed?: string) => {
      const port = await freePort()
      const started = await startIssuer({ port, publicUrl: `http://localhost:${port}`, storeKey: 'k', allowInsecureHttp: true, seed })
      const did = started.did
      await started.close()
      return did
    }

    const first = await start('a seed')
    expect(await start('a seed')).toBe(first)
    expect(await start('another seed')).not.toBe(first)
    expect(await start()).not.toBe(first)
  })

  it('issues an employee credential that expires with the diploma', async () => {
    const accepted = await acceptCredentialOffer(wallet, await issuer.createEmployeeOffer(employee))

    expect(accepted).toHaveLength(1)
    expect(accepted[0].type).toBe('Employee')
    expect(accepted[0].issuer).toBe(issuer.did)
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
    const accepted = await acceptCredentialOffer(wallet, await issuer.createEmployeeOffer({ ...employee, description: 'Own text' }))
    const defaulted = await acceptCredentialOffer(wallet, await issuer.createEmployeeOffer(employee))

    expect(accepted[0].claims).toMatchObject({ issuer_name: 'Gemeente Utrecht', description: 'Own text' })
    expect(defaulted[0].claims).toMatchObject({ description: DEFAULT_DESCRIPTIONS.Employee })
  })

  it('names the logical issuer in the issuer metadata', async () => {
    const meta = (await (await fetch(`${baseUrl}/oid4vci/diploma/.well-known/openid-credential-issuer`)).json()) as { display?: { name?: string }[] }

    expect(meta.display).toEqual([{ name: 'Gemeente Utrecht' }])
  })

  it('pre-fills the university with the logical issuer', async () => {
    expect(await (await fetch(`${baseUrl}/diploma`)).text()).toContain('name="university" value="Gemeente Utrecht"')
  })

  it('shows the welcome page with a block per menu item and the issuer in the footer', async () => {
    const html = await (await fetch(baseUrl)).text()

    expect(html).toContain('Verifiable Credentials')
    expect(html).toContain('<h2>Aanvragen laadpalen</h2>')
    expect(html).toContain('<li aria-current="page">Home</li>')
    expect(html).toContain('<nav class="menu" aria-label="Menu"><ul><li><a href="/" aria-current="page">Home</a></li></ul><h2>')
    expect(html).toContain(`Uitgever: Gemeente Utrecht · ${issuer.did}`)
  })

  it('serves the logo that the header shows', async () => {
    const res = await fetch(`${baseUrl}/logo.svg`)

    expect(res.headers.get('content-type')).toContain('image/svg+xml')
    expect(await (await fetch(baseUrl)).text()).toContain('<img src="/logo.svg" alt="Gemeente Utrecht">')
  })

  it('marks the current menu item and shows the breadcrumb', async () => {
    const html = await (await fetch(`${baseUrl}/employee`)).text()

    expect(html).toContain('<a href="/employee" aria-current="page">Aanvragen laadpalen</a>')
    expect(html).toContain('<li aria-current="page">Aanvragen laadpalen</li>')
    expect(html).toContain('<li><a href="/">Home</a></li><li aria-current="page">Aanvragen laadpalen</li>')
  })

  it('pre-fills the description in both forms', async () => {
    expect(await (await fetch(`${baseUrl}/diploma`)).text()).toContain(`name="description" value="${DEFAULT_DESCRIPTIONS.Diploma}"`)
    expect(await (await fetch(`${baseUrl}/employee`)).text()).toContain(`name="description" value="${DEFAULT_DESCRIPTIONS.Employee}"`)
  })

  it('offers each credential type under its own name', async () => {
    const offer = await previewCredentialOffer(wallet, await issuer.createEmployeeOffer(employee))

    expect(offer.types).toEqual(['Employee'])
  })

  it('creates employee offers over HTTP', async () => {
    const res = await fetch(`${baseUrl}/employee/offers`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
      body: JSON.stringify(employee),
    })
    expect(res.status).toBe(200)
    const { offerUri } = (await res.json()) as { offerUri: string }

    const accepted = await acceptCredentialOffer(wallet, offerUri)
    expect(accepted[0].claims).toMatchObject({ department: 'burgerzaken' })
  })

  it.each([
    ['a missing field', { ...employee, organisation: '' }],
    ['an email that is not an address', { ...employee, email: 'nope' }],
    ['an unknown department', { ...employee, department: 'bestuursbureau' }],
    ['a date that is not a date', { ...employee, diplomaValidUntil: '2031/06/30' }],
    ['a month that does not exist, in dd-mm-yyyy', { ...employee, diplomaValidUntil: '30-13-2031' }],
    ['a day that does not exist', { ...employee, diplomaValidUntil: '2031-13-45' }],
    ['a date in the past', { ...employee, diplomaValidUntil: '2020-01-01' }],
  ])('rejects an employee offer with %s', async (_name, body) => {
    const res = await fetch(`${baseUrl}/employee/offers`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
      body: JSON.stringify(body),
    })
    expect(res.status).toBe(400)
  })

  it('accepts the valid-until date as dd-mm-yyyy', async () => {
    const res = await fetch(`${baseUrl}/employee/offers`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
      body: JSON.stringify({ ...employee, diplomaValidUntil: '30-06-2031' }),
    })
    const { offerUri } = (await res.json()) as { offerUri: string }

    expect((await acceptCredentialOffer(wallet, offerUri))[0].claims).toMatchObject({ exp: endOfDay('2031-06-30') })
  })

  it('serves the employee form', async () => {
    const html = await (await fetch(`${baseUrl}/employee`)).text()

    expect(html).toContain('name="diplomaValidUntil"')
    expect(html).toContain('<option value="burgerzaken">Burgerzaken</option>')
    expect(html).toContain('<option value="secretariaat">Secretariaat</option>')
  })

  it('still issues both credential types after a restart on the same store', async () => {
    const dir = mkdtempSync(join(tmpdir(), 'issuer-'))
    try {
      const options = { storeKey: 'k', allowInsecureHttp: true, path: dir, seed: 'restart seed' }
      const first = await startIssuer({ ...options, port: await freePort(), publicUrl: baseUrl })
      await first.close()

      const port = await freePort()
      const second = await startIssuer({ ...options, port, publicUrl: `http://localhost:${port}` })
      try {
        const diploma = await acceptCredentialOffer(wallet, await second.createOffer({ name: 'A', email: 'a@example.com', degree: 'D', university: 'U' }))
        const staff = await acceptCredentialOffer(wallet, await second.createEmployeeOffer(employee))
        expect([diploma[0].type, staff[0].type]).toEqual(['Diploma', 'Employee'])
      } finally {
        await second.close()
      }
    } finally {
      rmSync(dir, { recursive: true, force: true })
    }
  })
})
