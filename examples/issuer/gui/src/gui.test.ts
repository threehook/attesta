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
        res.end(JSON.stringify({ issuerName: 'Gemeente Utrecht', did: DID, departments: ['burgerzaken', 'secretariaat'], descriptions: DESCRIPTIONS, credentialIssuer: 'https://issuer.example/oid4vci/diploma', credentialTypes: ['Employee'] }))
        return
      }
      posted.push({ path: req.url ?? '', body: JSON.parse(raw) })
      if (refuse) {
        res.statusCode = 400
        res.end(JSON.stringify({ error: refuse }))
        return
      }
      res.end(JSON.stringify({ offerUri: 'openid-credential-offer://?x=1' }))
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

  it('serves the employee form', async () => {
    const html = await (await fetch(`${baseUrl}/employee`)).text()

    expect(html).toContain('name="diplomaValidUntil"')
    expect(html).toContain('<option value="burgerzaken">Burgerzaken</option>')
    expect(html).toContain('<option value="secretariaat">Secretariaat</option>')
  })

  it('offers the holder a link that opens the wallet to ask for a credential, with no secret in it', async () => {
    const html = await (await fetch(`${baseUrl}/ophalen`)).text()

    const href = /href="(openid-credential-offer:[^"]+)"/.exec(html)![1].replaceAll('&#38;', '&')
    const offer = JSON.parse(decodeURIComponent(href.split('credential_offer=')[1]))
    expect(offer).toEqual({ credential_issuer: 'https://issuer.example/oid4vci/diploma', credential_configuration_ids: ['Employee'], grants: { authorization_code: {} } })
    expect(html).toContain('<a href="/ophalen" aria-current="page">Mijn credential ophalen</a>')
    expect(html).toContain('<li><a href="/">Home</a></li><li aria-current="page">Mijn credential ophalen</li>')
  })

  it('posts a form to the API and shows the offer link', async () => {
    const res = await fetch(`${baseUrl}/employee/offers`, form({ name: 'Jerry Smith', email: 'jerry@example.com', department: 'burgerzaken' }))

    expect(posted).toEqual([{ path: '/employee/offers', body: { name: 'Jerry Smith', email: 'jerry@example.com', department: 'burgerzaken' } }])
    const html = await res.text()
    expect(html).toContain('<code>openid-credential-offer://?x=1</code>')
    expect(html).toContain('<li><a href="/">Home</a></li><li><a href="/employee">Aanvragen laadpalen</a></li><li aria-current="page">Aanbod</li>')
  })

  it('passes on why the API refused a form', async () => {
    refuse = 'email is not an email address'

    const res = await fetch(`${baseUrl}/offers`, form({ name: 'A', email: 'nope', degree: 'D', university: 'U' }))

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
