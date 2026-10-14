import { createServer, type Server } from 'node:http'
import type { AddressInfo } from 'node:net'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { startGui } from './gui.js'

const DIPLOMA = { path: 'laadpalen-management', title: 'Diploma Laadpalen Management', description: 'Bewijs dat de opleiding Laadpalen Management is afgerond' }
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
        res.end(JSON.stringify({ issuerName: 'Gemeente Utrecht', did: DID, departments: ['burgerzaken', 'secretariaat'], roles: ['laadpalen-aanvrager', 'reader'], diplomas: [DIPLOMA] }))
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
  it('shows the welcome page with a block per menu item and the issuer in the footer', async () => {
    const html = await (await fetch(baseUrl)).text()

    expect(html).toContain('Verifiable Credentials')
    expect(html).toContain('<h2>Medewerker</h2>')
    expect(html).toContain('<h2>Diploma Laadpalen Management</h2>')
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

    expect(html).toContain('<a href="/employee" aria-current="page">Medewerker</a>')
    expect(html).toContain('<li aria-current="page">Medewerker</li>')
    expect(html).toContain('<li><a href="/">Home</a></li><li aria-current="page">Medewerker</li>')
  })

  it('has no fields for the title or the description: they are fixed by the credential', async () => {
    for (const path of ['employee', 'diploma/laadpalen-management']) {
      const html = await (await fetch(`${baseUrl}/${path}`)).text()
      expect(html).not.toContain('name="title"')
      expect(html).not.toContain('name="description"')
    }
  })

  it('accepts a date in the form with the way it asks for one', async () => {
    const html = await (await fetch(`${baseUrl}/diploma/laadpalen-management`)).text()

    const pattern = new RegExp(`^(?:${/name="validUntil"[^>]*pattern="([^"]+)"/.exec(html)![1]})$`)

    expect(pattern.test('12-10-2027')).toBe(true)
    expect(pattern.test('30-06-2031')).toBe(true)
    expect(pattern.test('2027-10-12')).toBe(false)
    expect(pattern.test('12/10/2027')).toBe(false)
  })

  it('serves the employee form with a drop-down of the roles to tick, not to type', async () => {
    const html = await (await fetch(`${baseUrl}/employee`)).text()

    expect(html).toContain('<details class="multi"><summary>Kies rol(len)</summary>')
    expect(html).toContain('<input type="checkbox" name="roles" value="laadpalen-aanvrager">laadpalen-aanvrager')
    expect(html).toContain('<input type="checkbox" name="roles" value="reader">reader')
    expect(html).not.toContain('<input name="roles"')
    expect(html).not.toContain('name="diploma')
    expect(html).not.toContain('name="validUntil"')
    expect(html).toContain('<option value="burgerzaken">Burgerzaken</option>')
    expect(html).toContain('<option value="secretariaat">Secretariaat</option>')
  })

  it('passes the chosen roles on as a list, one or several or none', async () => {
    await fetch(`${baseUrl}/employee`, { method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, body: 'name=A&email=a%40b.c&roles=reader&roles=laadpalen-aanvrager' })
    await fetch(`${baseUrl}/employee`, { method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, body: 'name=B&email=b%40b.c&roles=reader' })
    await fetch(`${baseUrl}/employee`, form({ name: 'C', email: 'c@b.c' }))

    expect(posted.map((p) => p.body.roles)).toEqual([['reader', 'laadpalen-aanvrager'], ['reader'], undefined])
  })

  it('posts a form to the API and says that a mail with a QR code is on its way', async () => {
    const res = await fetch(`${baseUrl}/employee`, form({ name: 'Jerry Smith', email: 'jerry@example.com', department: 'burgerzaken' }))

    expect(posted).toEqual([{ path: '/entitlements/employee', body: { name: 'Jerry Smith', email: 'jerry@example.com', department: 'burgerzaken' } }])
    const html = await res.text()
    expect(html).toContain('<strong>jerry@example.com</strong>')
    expect(html).toContain('e-mail met een QR-code')
    expect(html).not.toContain('PIN')
    expect(html).not.toContain('openid-credential')
    expect(html).toContain('<li><a href="/">Home</a></li><li><a href="/employee">Medewerker</a></li><li aria-current="page">Credential klaargezet</li>')
  })

  it('has a form and a menu item of its own for each diploma, with its title and an end date, and no course to type', async () => {
    const html = await (await fetch(`${baseUrl}/diploma/laadpalen-management`)).text()

    expect(html).toContain('name="validUntil"')
    expect(html).toContain('action="/diploma/laadpalen-management"')
    expect(html).toContain('<a href="/diploma/laadpalen-management" aria-current="page">Diploma Laadpalen Management</a>')
    expect(html).not.toContain('name="course"')
    expect((await fetch(`${baseUrl}/diploma/koeien-melken`)).status).toBe(404)
  })

  it('posts a diploma form to the API path of that diploma', async () => {
    const res = await fetch(`${baseUrl}/diploma/laadpalen-management`, form({ name: 'Jim', email: 'jim@example.com', validUntil: '31-12-2099' }))

    expect(posted).toEqual([{ path: '/entitlements/diploma/laadpalen-management', body: { name: 'Jim', email: 'jim@example.com', validUntil: '31-12-2099' } }])
    expect(await res.text()).toContain('<strong>jim@example.com</strong>')
    expect((await fetch(`${baseUrl}/diploma/koeien-melken`, form({ name: 'Jim' }))).status).toBe(404)
    expect(posted).toHaveLength(1)
  })

  it('passes on why the API refused a form', async () => {
    refuse = 'email is not an email address'

    const res = await fetch(`${baseUrl}/diploma/laadpalen-management`, form({ name: 'A', email: 'nope', validUntil: '31-12-2099' }))

    expect(res.status).toBe(400)
    expect(await res.text()).toContain('email is not an email address')
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
