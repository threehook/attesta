import { createServer, type Server } from 'node:http'
import type { AddressInfo } from 'node:net'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { startGui } from './gui.js'

const DESCRIPTIONS = { Diploma: 'Diploma uitgereikt door het opleidingsinstituut', Employee: 'Autorisatie om laadpalen aan te vragen' }
const DID = 'did:key:z6MkTest'

// A stand-in for the issuer API: it answers /info and records what the pages post.
let api: Server
let apiUrl: string
let posted: { path: string; body: Record<string, string> }[]
let refuse: string | undefined

beforeEach(async () => {
  posted = []
  refuse = undefined
  api = createServer((req, res) => {
    let raw = ''
    req.on('data', (chunk) => (raw += chunk))
    req.on('end', () => {
      res.setHeader('content-type', 'application/json')
      if (req.method === 'GET' && req.url === '/info') {
        res.end(JSON.stringify({ issuerName: 'Gemeente Utrecht', did: DID, departments: ['burgerzaken', 'secretariaat'], descriptions: DESCRIPTIONS }))
        return
      }
      posted.push({ path: req.url ?? '', body: JSON.parse(raw) })
      if (refuse) {
        res.statusCode = 400
        res.end(JSON.stringify({ error: refuse }))
        return
      }
      res.end(JSON.stringify({ entitlementId: 'e-1', mailedTo: 'x' }))
    })
  })
  await new Promise<void>((resolve) => api.listen(0, resolve))
  apiUrl = `http://localhost:${(api.address() as AddressInfo).port}`
})

afterEach(async () => {
  await new Promise((resolve) => api.close(resolve))
})

let gui: Awaited<ReturnType<typeof startGui>>
let baseUrl: string

beforeEach(async () => {
  gui = await startGui({ port: 0, apiUrl })
  baseUrl = `http://localhost:${gui.port}`
})

afterEach(async () => {
  await gui.close()
})

const form = (fields: Record<string, string>) => ({
  method: 'POST',
  headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
  body: new URLSearchParams(fields),
})

describe('issuer pages', () => {
  it('pre-fills the university with the logical issuer', async () => {
    expect(await (await fetch(`${baseUrl}/diploma`)).text()).toContain('name="university" value="Gemeente Utrecht"')
  })

  it('shows the welcome page with a block per menu item and the issuer in the footer', async () => {
    const html = await (await fetch(baseUrl)).text()

    expect(html).toContain('Verifiable Credentials')
    expect(html).toContain('<h2>Aanvragen laadpalen</h2>')
    expect(html).toContain('<li aria-current="page">Home</li>')
    expect(html).toContain('<nav class="menu" aria-label="Menu"><ul><li><a href="/" aria-current="page">Home</a></li></ul><h2>')
    expect(html).toContain(`Uitgever: Gemeente Utrecht · ${DID}`)
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
    expect(await (await fetch(`${baseUrl}/diploma`)).text()).toContain(`name="description" value="${DESCRIPTIONS.Diploma}"`)
    expect(await (await fetch(`${baseUrl}/employee`)).text()).toContain(`name="description" value="${DESCRIPTIONS.Employee}"`)
  })

  it('accepts a date in the form with the way it asks for one', async () => {
    const html = await (await fetch(`${baseUrl}/employee`)).text()

    const pattern = new RegExp(`^(?:${/name="diplomaValidUntil"[^>]*pattern="([^"]+)"/.exec(html)![1]})$`)

    expect(pattern.test('12-10-2027')).toBe(true)
    expect(pattern.test('30-06-2031')).toBe(true)
    expect(pattern.test('2027-10-12')).toBe(false)
    expect(pattern.test('12/10/2027')).toBe(false)
  })

  it('serves the employee form', async () => {
    const html = await (await fetch(`${baseUrl}/employee`)).text()

    expect(html).toContain('name="diplomaValidUntil"')
    expect(html).toContain('<option value="burgerzaken">Burgerzaken</option>')
    expect(html).toContain('<option value="secretariaat">Secretariaat</option>')
  })

  it('posts a form to the API and says that a mail with a QR code is on its way', async () => {
    const res = await fetch(`${baseUrl}/employee`, form({ name: 'Jerry Smith', email: 'jerry@example.com', department: 'burgerzaken' }))

    expect(posted).toEqual([{ path: '/entitlements/employee', body: { name: 'Jerry Smith', email: 'jerry@example.com', department: 'burgerzaken' } }])
    const html = await res.text()
    expect(html).toContain('<strong>jerry@example.com</strong>')
    expect(html).toContain('e-mail met een QR-code')
    expect(html).not.toContain('PIN')
    expect(html).not.toContain('openid-credential-offer')
    expect(html).toContain('<li><a href="/">Home</a></li><li><a href="/employee">Aanvragen laadpalen</a></li><li aria-current="page">Credential klaargezet</li>')
  })

  it('passes on why the API refused a form', async () => {
    refuse = 'email is not an email address'

    const res = await fetch(`${baseUrl}/diploma`, form({ name: 'A', email: 'nope', degree: 'D', university: 'U' }))

    expect(res.status).toBe(400)
    expect(await res.json()).toEqual({ error: 'email is not an email address' })
  })

  it('says so when the API cannot be reached', async () => {
    const lonely = await startGui({ port: 0, apiUrl: 'http://127.0.0.1:1' })
    try {
      expect((await fetch(`http://localhost:${lonely.port}`)).status).toBe(502)
    } finally {
      await lonely.close()
    }
  })
})
