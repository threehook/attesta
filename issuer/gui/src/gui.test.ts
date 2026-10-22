import { createServer, type Server } from 'node:http'
import type { AddressInfo } from 'node:net'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { startGui } from './gui.js'

const DIPLOMA = { type: 'Diploma Laadpalen Management', path: 'laadpalen-management', title: 'Diploma Laadpalen Management', description: 'Bewijs dat de opleiding Laadpalen Management is afgerond',
  fields: [
    { key: 'name', label: 'Naam', kind: 'text', required: true },
    { key: 'email', label: 'E-mailadres', kind: 'email', required: true },
    { key: 'validUntil', label: 'Geldig tot', kind: 'date', required: true },
  ] }
const BADGE = {
  type: 'Badge <b>', path: 'badge', title: 'Badge <b>', description: 'Toegang',
  fields: [
    { key: 'email', label: 'E-mailadres', kind: 'email', required: true },
    { key: 'colour', label: 'Kleur', kind: 'choice', required: true, options: ['red', 'blue'] },
  ],
}
const DID = 'did:key:z6MkTest'

// A stand-in for the issuer API: it answers /info and records what the pages post.
let api: Server
let apiUrl: string
let posted: { path: string; body: Record<string, string> }[]
let refuse: string | undefined
let changed: string[]
const held = [
  { id: 'e-1', type: 'Employee', title: 'Medewerker afdeling & rollen', status: 'issued', issuedAt: '2026-10-14T09:00:00Z', createdAt: '2026-10-14T08:00:00Z',
    claims: { name: 'Jerry Smith', email: 'jerry@example.com', department: 'secretariaat', organisation: 'Gemeente Utrecht', roles: 'laadpalen-aanvrager, reader' } },
  { id: 'e-2', type: 'Diploma Laadpalen Management', title: 'Diploma Laadpalen Management', status: 'waiting', issuedAt: null, createdAt: '2026-10-15T08:00:00Z',
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
        res.end(JSON.stringify({ issuerName: 'Gemeente Utrecht', did: DID, departments: ['burgerzaken', 'secretariaat'], roles: ['laadpalen-aanvrager', 'reader'], types: [DIPLOMA, BADGE] }))
        return
      }
      if (req.method === 'GET' && req.url === '/entitlements') {
        res.end(JSON.stringify({ entitlements: held }))
        return
      }
      if (req.url?.startsWith('/credential-types')) {
        if (req.method === 'GET') {
          res.end(JSON.stringify({ types: [{ ...DIPLOMA, id: 't-1', inUse: true }, { ...BADGE, id: 't-2', inUse: false }] }))
          return
        }
        changed.push(`${req.method} ${req.url} ${raw}`.trim())
        res.statusCode = refuse ? 409 : req.method === 'DELETE' ? 204 : 200
        res.end(refuse ? JSON.stringify({ error: refuse }) : req.method === 'DELETE' ? undefined : '{}')
        return
      }
      if (req.url?.startsWith('/mandates') || req.url?.startsWith('/approvals') || req.url?.startsWith('/workloads')) {
        changed.push(`${req.method} ${req.url} ${raw}`.trim())
        res.statusCode = req.method === 'DELETE' ? 204 : 200
        res.end(req.method === 'DELETE' ? undefined : (req.url === '/mandates' || req.url === '/approvals') && req.method === 'GET' ? '[]' : '{}')
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

  it('has a page for the systems under Beheer, with a form to enrol one', async () => {
    const html = await (await fetch(`${baseUrl}/workloads`)).text()
    expect(html).toContain('<a href="/workloads" aria-current="page">Systemen</a>')
    expect(html).toContain('id="workload-form"')
    expect(html).toContain('id="workloads-body"')
  })

  it('passes the enrolment and the withdrawal of a system on to the API', async () => {
    await fetch(`${baseUrl}/workloads/data`)
    await fetch(`${baseUrl}/workloads`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ spiffeId: 'spiffe://x/y', owner: 'platform', environment: 'staging' }) })
    await fetch(`${baseUrl}/workloads/w-1`, { method: 'DELETE' })
    expect(changed).toEqual([
      'GET /workloads',
      'POST /workloads {"spiffeId":"spiffe://x/y","owner":"platform","environment":"staging"}',
      'DELETE /workloads/w-1',
    ])
  })

  it('has a page for the mandates under Beheer, with a form to approve one', async () => {
    const html = await (await fetch(`${baseUrl}/mandates`)).text()
    expect(html).toContain('<a href="/mandates" aria-current="page">Mandaten</a>')
    expect(html).toContain('id="mandate-form"')
  })

  it('passes the mandates, the approvals and the systems on to the API', async () => {
    await fetch(`${baseUrl}/mandates/data`)
    await fetch(`${baseUrl}/workloads/data`)
    await fetch(`${baseUrl}/mandates`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ workload: 'w', resource: 'deploy:prod:*', expiresAt: '2026-10-23T10:00:00Z' }) })
    await fetch(`${baseUrl}/mandates/m-1/approvals`, { method: 'POST' })
    await fetch(`${baseUrl}/mandates/m-1`, { method: 'DELETE' })
    expect(changed).toEqual([
      'GET /mandates',
      'GET /workloads',
      'POST /mandates {"workload":"w","resource":"deploy:prod:*","expiresAt":"2026-10-23T10:00:00Z"}',
      'POST /mandates/m-1/approvals {}',
      'DELETE /mandates/m-1',
    ])
  })

  it('has a page for the actions that wait for approval, under Beheer', async () => {
    const html = await (await fetch(`${baseUrl}/approvals`)).text()
    expect(html).toContain('<a href="/approvals" aria-current="page">Goedkeuringen</a>')
    expect(html).toContain('id="approvals-body"')
  })

  it('passes the approvals and the rejections on to the API', async () => {
    await fetch(`${baseUrl}/approvals/data`)
    await fetch(`${baseUrl}/approvals/r-1/approvals`, { method: 'POST' })
    await fetch(`${baseUrl}/approvals/r-1`, { method: 'DELETE' })
    expect(changed).toEqual(['GET /approvals', 'POST /approvals/r-1/approvals {}', 'DELETE /approvals/r-1'])
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
    for (const path of ['employee', 'type/laadpalen-management']) {
      const html = await (await fetch(`${baseUrl}/${path}`)).text()
      expect(html).not.toContain('name="title"')
      expect(html).not.toContain('name="description"')
    }
  })

  it('accepts a date in the form with the way it asks for one', async () => {
    const html = await (await fetch(`${baseUrl}/type/laadpalen-management`)).text()

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
    const html = await (await fetch(`${baseUrl}/type/laadpalen-management`)).text()

    expect(html).toContain('name="validUntil"')
    expect(html).toContain('action="/type/laadpalen-management"')
    expect(html).toContain('<a href="/type/laadpalen-management" aria-current="page">Diploma Laadpalen Management</a>')
    expect(html).not.toContain('name="course"')
    expect((await fetch(`${baseUrl}/type/koeien-melken`)).status).toBe(404)
  })

  it('posts a diploma form to the API path of that diploma', async () => {
    const res = await fetch(`${baseUrl}/type/laadpalen-management`, form({ name: 'Jim', email: 'jim@example.com', validUntil: '31-12-2099' }))

    expect(posted).toEqual([{ path: '/entitlements/type/laadpalen-management', body: { name: 'Jim', email: 'jim@example.com', validUntil: '31-12-2099' } }])
    expect(await res.text()).toContain('<strong>jim@example.com</strong>')
    expect((await fetch(`${baseUrl}/type/koeien-melken`, form({ name: 'Jim' }))).status).toBe(404)
    expect(posted).toHaveLength(1)
  })

  it('passes on why the API refused a form', async () => {
    refuse = 'email is not an email address'

    const res = await fetch(`${baseUrl}/type/laadpalen-management`, form({ name: 'A', email: 'nope', validUntil: '31-12-2099' }))

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

  it('offers Credentials first under Beheer, and no Intrekken', async () => {
    const html = await (await fetch(baseUrl)).text()

    expect(html).toContain('<h2>Credentials</h2>')
    expect(html).not.toContain('Intrekken</a>')
  })

  it('shows the credentials page with a table to filter, sort and page, and a popup to revoke, but nothing to add', async () => {
    const html = await (await fetch(`${baseUrl}/credentials`)).text()

    expect(html).toContain('id="credentials-filter"')
    expect(html).toContain('<th data-key="id"')
    expect(html).toContain('<th data-key="email"')
    expect(html).toContain('<th data-key="issued"')
    expect(html).toContain('id="credentials-size"')
    expect(html).toContain('id="credentials-next"')
    expect(html).toContain('<dialog id="credential-revoke">')
    expect(html).not.toContain('aanmaken')
  })

  it('lets the user drag the edge of a column header on every table', async () => {
    for (const path of ['credentials', 'roles', 'credential-types']) {
      expect(await (await fetch(`${baseUrl}/${path}`)).text()).toContain("className = 'grip'")
    }
  })

  it('gives the table every credential the API lists', async () => {
    const res = await fetch(`${baseUrl}/credentials/data`)

    expect(((await res.json()) as { entitlements: { id: string }[] }).entitlements.map((c) => c.id)).toEqual(['e-1', 'e-2'])
  })

  it('revokes through the API without sending data, and passes on why it was refused', async () => {
    const res = await fetch(`${baseUrl}/credentials/e-1/revoke`, { method: 'POST' })

    expect(posted).toEqual([{ path: '/entitlements/revoke/e-1', body: {} }])
    expect(res.status).toBe(200)

    refuse = 'dit credential bestaat niet of is al ingetrokken'
    const refused = await fetch(`${baseUrl}/credentials/e-1/revoke`, { method: 'POST' })
    expect(refused.status).toBe(400)
    expect(await refused.json()).toEqual({ error: 'dit credential bestaat niet of is al ingetrokken' })
  })

  it('shows the form of the credential with its data filled in', async () => {
    const employee = await (await fetch(`${baseUrl}/credentials/e-1/correct`)).text()

    expect(employee).toContain('value="Jerry Smith"')
    expect(employee).toContain('<option value="secretariaat" selected>')
    expect(employee).toContain('value="laadpalen-aanvrager" checked')
    expect(employee).toContain('value="reader" checked')
    expect(employee).toContain('<summary>laadpalen-aanvrager, reader</summary>')
    expect(employee).toContain('action="/credentials/e-1/correct"')
    expect(employee).toContain('name="email" type="email" value="jerry@example.com" required readonly')
    expect(employee).toContain('Het e-mailadres kan niet worden gewijzigd')

    const diploma = await (await fetch(`${baseUrl}/credentials/e-2/correct`)).text()
    expect(diploma).toContain('name="validUntil"')
    expect(diploma).toContain('value="31-12-2099"')
    expect(diploma).toContain('name="email" type="email" value="jerry@example.com" required readonly')
    expect(diploma).not.toContain('name="name" value="Jerry Smith" required readonly')
    expect((await fetch(`${baseUrl}/credentials/e-9/correct`)).status).toBe(404)
  })

  it('changes a credential by posting the corrected data to the revoke endpoint of the API', async () => {
    const res = await fetch(`${baseUrl}/credentials/e-1/correct`, form({ name: 'Jerry Smith', email: 'jerry@example.com', department: 'burgerzaken' }))

    expect(posted).toEqual([{ path: '/entitlements/revoke/e-1', body: { name: 'Jerry Smith', email: 'jerry@example.com', department: 'burgerzaken' } }])
    const html = await res.text()
    expect(html).toContain('Het credential is ingetrokken.')
    expect(html).toContain('<strong>jerry@example.com</strong>')
  })

  it('says why a credential could not be changed', async () => {
    refuse = 'voor deze persoon staat al een credential van dit type klaar'

    const res = await fetch(`${baseUrl}/credentials/e-1/correct`, form({ email: 'jerry@example.com' }))

    expect(res.status).toBe(400)
    expect(await res.text()).toContain('voor deze persoon staat al een credential van dit type klaar')
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

  it('offers Credential Forms under Beheer and lists them with the ones in use marked', async () => {
    expect(await (await fetch(baseUrl)).text()).toContain('<h2>Credential Forms</h2>')
    const html = await (await fetch(`${baseUrl}/credential-types`)).text()

    expect(html).toContain('<td>Diploma Laadpalen Management</td>')
    expect(html).toContain('href="/credential-types/t-2/edit"')
    expect(html).toContain('data-delete="t-1" data-title="Diploma Laadpalen Management" disabled')
    expect(html).toContain('data-delete="t-2" data-title="Badge &#60;b&#62;">')
  })

  it('puts Credential Forms last under Beheer, after Rollen and Credentials', async () => {
    const html = await (await fetch(baseUrl)).text()

    expect(html).toMatch(/<h2>Beheer<\/h2><ul><li><a href="\/roles"[^>]*>Rollen<\/a><\/li><li><a href="\/credentials"[^>]*>Credentials<\/a><\/li><li><a href="\/credential-types"/)
  })

  it('shows the editor for a new type with the email field in place, and for an existing one with its data', async () => {
    const fresh = await (await fetch(`${baseUrl}/credential-types/new`)).text()
    expect(fresh).toContain('id="type-preview"')
    expect(fresh).toContain('"fields":[{"key":"email"')
    expect(fresh).toContain('id="type-expires"')
    expect(fresh).not.toContain('"key":"validUntil"')

    const existing = await (await fetch(`${baseUrl}/credential-types/t-2/edit`)).text()
    expect(existing).toContain('"id":"t-2"')
    expect(existing).toContain('"options":["red","blue"]')
    expect(existing).not.toContain('<b>')
    expect((await fetch(`${baseUrl}/credential-types/t-9/edit`)).status).toBe(404)
  })

  it('previews a form with the code of the real form, escaped and impossible to send', async () => {
    const res = await fetch(`${baseUrl}/credential-types/preview`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ fields: BADGE.fields.concat([{ key: 'x', label: '<i>', kind: 'date', required: false }, { key: 'price', label: 'Prijs', kind: 'number', required: true, min: '0', max: '9,5', decimals: 2 }] as never) }),
    })
    const html = await res.text()

    expect(html).toContain('<select name="colour" required><option value=""></option><option value="red">red</option>')
    expect(html).toContain('type="email"')
    expect(html).toContain('placeholder="dd-mm-jjjj"')
    expect(html).toContain('&#60;i&#62;')
    expect(html).toContain('<input name="price" type="number" step="0.01" min="0" max="9.5" required>')
    expect(html).toContain('<button type="submit" disabled>')
    expect(html).not.toContain('<i>')
  })

  it('passes a saved, changed or deleted type on to the API and says why it was refused', async () => {
    const json = (method: string, body?: object) => ({ method, headers: { 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined })
    await fetch(`${baseUrl}/credential-types`, json('POST', { title: 'Pas', fields: [] }))
    await fetch(`${baseUrl}/credential-types/t-2`, json('PUT', { title: 'Pas 2', fields: [] }))
    await fetch(`${baseUrl}/credential-types/t-2`, json('DELETE'))
    expect(changed).toEqual([
      'POST /credential-types {"title":"Pas","fields":[]}', 'PUT /credential-types/t-2 {"title":"Pas 2","fields":[]}', 'DELETE /credential-types/t-2',
    ])

    refuse = 'het credentialtype is in gebruik'
    const res = await fetch(`${baseUrl}/credential-types/t-1`, json('DELETE'))
    expect(res.status).toBe(409)
    expect(await res.json()).toEqual({ error: 'het credentialtype is in gebruik' })
  })

  it('makes the form of a type from its fields, a choice as a list with an empty first option', async () => {
    expect(await (await fetch(`${baseUrl}/type/laadpalen-management`)).text()).toContain('<label>Geldig tot <input name="validUntil"')
    expect(await (await fetch(`${baseUrl}/type/badge`)).text()).toContain('<label>Kleur <select name="colour" required><option value=""></option>')
  })
})

describe('Policy Editor', () => {
  it('is the first item under Tools, below Beheer', async () => {
    const html = await (await fetch(baseUrl)).text()

    expect(html).toMatch(/<h2>Beheer<\/h2>.*<h2>Tools<\/h2><ul><li><a href="\/policy-editor"[^>]*>Policy Editor<\/a><\/li><li><a href="\/policy-tester"[^>]*>Policy Tester<\/a><\/li><\/ul>/s)
  })

  it('shows the editor with its file buttons, and serves Monaco itself', async () => {
    const html = await (await fetch(`${baseUrl}/policy-editor`)).text()

    expect(html).toContain('<div id="pe-editor">')
    expect(html).toContain('id="pe-save-as"')
    expect((await fetch(`${baseUrl}/vendor/monaco/vs/loader.js`)).status).toBe(200)
  })

  it('says the check is unavailable when no backend is set', async () => {
    expect((await fetch(`${baseUrl}/policy-editor/diagnose`, { method: 'POST', headers: { 'content-type': 'application/json' }, body: '{"source":"x"}' })).status).toBe(503)
  })

  it('has the backend check the policy, with the admin token', async () => {
    const seen: { token?: string | string[]; body: string }[] = []
    const backend = createServer((req, res) => {
      let raw = ''
      req.on('data', (c) => (raw += c))
      req.on('end', () => {
        seen.push({ token: req.headers['x-admin-token'], body: raw })
        res.setHeader('content-type', 'application/json')
        res.end(JSON.stringify({ diagnostics: [{ severity: 'error', message: 'boom', row: 1, col: 1, endRow: 1, endCol: 2 }] }))
      })
    })
    await new Promise<void>((resolve) => backend.listen(0, resolve))
    const checking = await startGui({ port: 0, apiUrl, backend: { url: `http://localhost:${(backend.address() as AddressInfo).port}`, adminToken: 'tok' } })
    try {
      const res = await fetch(`http://localhost:${checking.port}/policy-editor/diagnose`, { method: 'POST', headers: { 'content-type': 'application/json' }, body: '{"source":"package policy"}' })

      expect(await res.json()).toEqual({ diagnostics: [{ severity: 'error', message: 'boom', row: 1, col: 1, endRow: 1, endCol: 2 }] })
      expect(seen).toEqual([{ token: 'tok', body: '{"source":"package policy"}' }])
    } finally {
      await checking.close()
      backend.close()
    }
  })
})

describe('Policy Tester', () => {
  it('shows the tester with the policy and the input side by side', async () => {
    const html = await (await fetch(`${baseUrl}/policy-tester`)).text()

    expect(html).toContain('id="pt-policy"')
    expect(html).toContain('id="pt-input"')
    expect(html).toContain('id="pt-run" disabled')
  })

  it('has the backend evaluate the policy on the input', async () => {
    const seen: { path?: string; token?: string | string[]; body: string }[] = []
    const backend = createServer((req, res) => {
      let raw = ''
      req.on('data', (c) => (raw += c))
      req.on('end', () => {
        seen.push({ path: req.url, token: req.headers['x-admin-token'], body: raw })
        res.setHeader('content-type', 'application/json')
        res.end(JSON.stringify({ result: { decision: 'Permit', reason: 'ok', obligations: [], advice: [] } }))
      })
    })
    await new Promise<void>((resolve) => backend.listen(0, resolve))
    const testing = await startGui({ port: 0, apiUrl, backend: { url: `http://localhost:${(backend.address() as AddressInfo).port}`, adminToken: 'tok' } })
    try {
      const res = await fetch(`http://localhost:${testing.port}/policy-tester/evaluate`, {
        method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ source: 'package policy', input: { resource: 'r' } }),
      })

      expect(((await res.json()) as { result: { decision: string } }).result.decision).toBe('Permit')
      expect(seen).toEqual([{ path: '/admin/policies/evaluate', token: 'tok', body: '{"source":"package policy","input":{"resource":"r"}}' }])
    } finally {
      await testing.close()
      backend.close()
    }
  })
})
