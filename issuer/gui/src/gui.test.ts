import { createServer, type Server } from 'node:http'
import type { AddressInfo } from 'node:net'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { startGui } from './gui.js'

const DIPLOMA = { type: 'Diploma Laadpalen Management', path: 'laadpalen-management', title: 'Diploma Laadpalen Management', description: 'Bewijs dat de opleiding Laadpalen Management is afgerond' }
const DID = 'did:key:z6MkTest'

// A stand-in for the issuer API: it answers /info and records what the pages post.
let api: Server
let apiUrl: string
let posted: { path: string; body: Record<string, string> }[]
let refuse: string | undefined
let changed: string[]
const held = [
  { id: 'e-1', type: 'Employee', title: 'Medewerker afdeling & rollen', status: 'issued', issuedAt: '2026-10-14T09:00:00Z',
    claims: { name: 'Jerry Smith', email: 'jerry@example.com', department: 'secretariaat', organisation: 'Gemeente Utrecht', roles: 'laadpalen-aanvrager, reader' } },
  { id: 'e-2', type: 'Diploma Laadpalen Management', title: 'Diploma Laadpalen Management', status: 'waiting', issuedAt: null,
    claims: { name: 'Jerry Smith', email: 'jerry@example.com', validUntil: '2099-12-31' } },
]

beforeEach(async () => {
  posted = []
  refuse = undefined
  changed = []
  api = createServer((req, res) => {
    let raw = ''
    req.on('data', (chunk) => (raw += chunk))
    req.on('end', () => {
      res.setHeader('content-type', 'application/json')
      if (req.method === 'GET' && req.url === '/info') {
        res.end(JSON.stringify({ issuerName: 'Gemeente Utrecht', did: DID, departments: ['burgerzaken', 'secretariaat'], roles: ['laadpalen-aanvrager', 'reader'], diplomas: [DIPLOMA] }))
        return
      }
      if (req.method === 'GET' && req.url?.startsWith('/entitlements?')) {
        const email = new URL(req.url, 'http://x').searchParams.get('email')
        res.statusCode = email === 'nope' ? 400 : 200
        res.end(JSON.stringify(email === 'nope' ? { error: 'email is not an email address' } : { entitlements: email === 'jerry@example.com' ? held : [] }))
        return
      }
      if (req.url?.startsWith('/roles')) {
        if (req.method === 'GET') {
          res.end(JSON.stringify({ roles: [{ id: 'r-1', name: 'laadpalen-aanvrager', inUse: true }, { id: 'r-2', name: 'reader', inUse: false }] }))
          return
        }
        changed.push(`${req.method} ${req.url} ${raw}`.trim())
        res.statusCode = refuse ? 409 : req.method === 'DELETE' ? 204 : 200
        res.end(refuse ? JSON.stringify({ error: refuse }) : req.method === 'DELETE' ? undefined : '{}')
        return
      }
      posted.push({ path: req.url ?? '', body: raw ? JSON.parse(raw) : {} })
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

    expect(html).toContain('<details class="multi" data-none="Kies rol(len)"><summary>Kies rol(len)</summary>')
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

  it('offers Intrekken in the menu and on the welcome page', async () => {
    const html = await (await fetch(baseUrl)).text()

    expect(html).toContain('<h2>Beheer</h2>')
    expect(html).toContain('<h2>Intrekken</h2>')
  })

  it('lists what a person holds, with the date it was issued, and a button to revoke each', async () => {
    const html = await (await fetch(`${baseUrl}/revoke?email=jerry%40example.com`)).text()

    expect(html).toContain('Medewerker afdeling &#38; rollen')
    expect(html).toContain('Uitgegeven op 14-10-2026')
    expect(html).toContain('Staat klaar om op te halen')
    expect(html).toContain('<dd>31-12-2099</dd>')
    expect(html).toContain('action="/revoke/e-1"')
    expect(html).toContain('href="/revoke/e-2/correct?email=jerry%40example.com"')
  })

  it('says so when a person holds nothing, and why a search was refused', async () => {
    expect(await (await fetch(`${baseUrl}/revoke?email=nobody%40example.com`)).text()).toContain('Er is geen credential om in te trekken')
    const refused = await fetch(`${baseUrl}/revoke?email=nope`)
    expect(refused.status).toBe(400)
    expect(await refused.text()).toContain('email is not an email address')
  })

  it('revokes through the API without sending data', async () => {
    const res = await fetch(`${baseUrl}/revoke/e-1`, { method: 'POST' })

    expect(posted).toEqual([{ path: '/entitlements/revoke/e-1', body: {} }])
    expect(await res.text()).toContain('Het credential is ingetrokken.')
  })

  it('passes on why a revocation was refused', async () => {
    refuse = 'dit credential bestaat niet of is al ingetrokken'

    const res = await fetch(`${baseUrl}/revoke/e-1`, { method: 'POST' })

    expect(res.status).toBe(400)
    expect(await res.text()).toContain('dit credential bestaat niet of is al ingetrokken')
  })

  it('shows the form of the credential with its data filled in', async () => {
    const employee = await (await fetch(`${baseUrl}/revoke/e-1/correct?email=jerry%40example.com`)).text()

    expect(employee).toContain('value="Jerry Smith"')
    expect(employee).toContain('<option value="secretariaat" selected>')
    expect(employee).toContain('value="laadpalen-aanvrager" checked')
    expect(employee).toContain('value="reader" checked')
    expect(employee).toContain('<summary>laadpalen-aanvrager, reader</summary>')
    expect(employee).toContain('action="/revoke/e-1/correct"')

    const diploma = await (await fetch(`${baseUrl}/revoke/e-2/correct?email=jerry%40example.com`)).text()
    expect(diploma).toContain('name="validUntil"')
    expect(diploma).toContain('value="31-12-2099"')
    expect((await fetch(`${baseUrl}/revoke/e-9/correct?email=jerry%40example.com`)).status).toBe(404)
  })

  it('revokes and replaces by posting the corrected data to the same endpoint', async () => {
    const res = await fetch(`${baseUrl}/revoke/e-1/correct`, form({ name: 'Jerry Smith', email: 'jerry@example.com', department: 'burgerzaken' }))

    expect(posted).toEqual([{ path: '/entitlements/revoke/e-1', body: { name: 'Jerry Smith', email: 'jerry@example.com', department: 'burgerzaken' } }])
    const html = await res.text()
    expect(html).toContain('Het credential is ingetrokken.')
    expect(html).toContain('<strong>jerry@example.com</strong>')
  })

  it('offers Rollen under Beheer', async () => {
    expect(await (await fetch(baseUrl)).text()).toContain('<h2>Rollen</h2>')
  })

  it('shows the roles page with a table to sort and page, and popups to add, change and delete', async () => {
    const html = await (await fetch(`${baseUrl}/roles`)).text()

    expect(html).toContain('id="role-add"')
    expect(html).toContain('<th data-key="id"')
    expect(html).toContain('<th data-key="name"')
    expect(html).toContain('id="roles-filter"')
    expect(html).toContain('id="roles-size"')
    expect(html).toContain('id="roles-next"')
    expect(html).toContain('<dialog id="role-edit">')
    expect(html).toContain('<dialog id="role-delete">')
  })

  it('gives the table the roles the API has', async () => {
    const res = await fetch(`${baseUrl}/roles/data`)

    expect(((await res.json()) as { roles: { name: string }[] }).roles.map((r) => r.name)).toEqual(['laadpalen-aanvrager', 'reader'])
  })

  it('creates, renames and deletes a role through the API', async () => {
    const json = (method: string, name?: string) => ({ method, headers: { 'Content-Type': 'application/json' }, body: name ? JSON.stringify({ name }) : undefined })
    await fetch(`${baseUrl}/roles`, json('POST', 'auditor'))
    await fetch(`${baseUrl}/roles/r-2`, json('PUT', 'readers'))
    const res = await fetch(`${baseUrl}/roles/r-2`, json('DELETE'))

    expect(changed).toEqual(['POST /roles {"name":"auditor"}', 'PUT /roles/r-2 {"name":"readers"}', 'DELETE /roles/r-2'])
    expect(res.status).toBe(204)
  })

  it('passes on why a role could not be changed', async () => {
    refuse = 'de rol is in gebruik bij een medewerker'

    const res = await fetch(`${baseUrl}/roles/r-1`, { method: 'DELETE' })

    expect(res.status).toBe(409)
    expect(await res.json()).toEqual({ error: 'de rol is in gebruik bij een medewerker' })
  })
})
