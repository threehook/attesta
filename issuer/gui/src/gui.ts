import type { Server } from 'node:http'
import type { AddressInfo } from 'node:net'
import express from 'express'
import type { Auth } from './auth.js'
import { LOGO_SVG } from './logo.js'
import {
  renderCorrection,
  renderCreated,
  renderEmployeeForm,
  renderNotRevoked,
  renderRefused,
  renderRevoke,
  renderRevoked,
  renderRoles,
  renderTypeForm,
  renderWelcome,
  type ActiveCredential,
  type Chrome,
  type Kind,
  type TypeInfo,
} from './page.js'
import { renderPreview, renderTypeEditor, renderTypes, type ManagedType } from './types.js'

export interface GuiOptions {
  port: number
  /** Where the issuer API is reached; the pages ask it for what they show and to record every entitlement. */
  apiUrl: string
  /** Who may use the pages; without it they are open, which is for tests and local development only. */
  auth?: Auth
}

/** What the issuer API tells the pages. */
interface Info {
  issuerName?: string
  did: string
  departments: string[]
  roles: string[]
  types: TypeInfo[]
}

// A form sends one chosen role as text and several as a list; the API takes a list, and none when nothing was chosen.
function withRolesAsList(form: Record<string, unknown>): Record<string, unknown> {
  if (!('roles' in form)) return form
  return { ...form, roles: Array.isArray(form.roles) ? form.roles : [form.roles] }
}

export async function startGui(options: GuiOptions) {
  const api = options.apiUrl.replace(/\/$/, '')
  const app = express()
  app.use(express.urlencoded({ extended: false }))
  app.use(express.json())

  const asStaff = async (req: express.Request): Promise<Record<string, string>> => {
    const token = await options.auth?.token(req)
    return token ? { authorization: `Bearer ${token}` } : {}
  }
  const info = async (req: express.Request): Promise<Info> => {
    const res = await fetch(`${api}/info`, { headers: await asStaff(req) })
    if (!res.ok) throw new Error(`the issuer API answered ${res.status}`)
    return (await res.json()) as Info
  }
  const chromeOf = ({ issuerName, did, types }: Info): Chrome => ({ issuerName, did, types, signOut: !!options.auth })

  // Express 5 passes a rejected handler to the error handler below. The logo stays open: the login page and the probes use it.
  app.get('/logo.svg', (_req, res) => {
    res.type('image/svg+xml').send(LOGO_SVG)
  })
  if (options.auth) {
    app.use(options.auth.routes)
    app.use(options.auth.guard)
  }
  app.get('/', async (req, res) => {
    res.type('html').send(renderWelcome(chromeOf(await info(req))))
  })
  app.get('/employee', async (req, res) => {
    const i = await info(req)
    res.type('html').send(renderEmployeeForm(i.departments, i.roles, chromeOf(i)))
  })
  app.get('/type/:path', async (req, res) => {
    const i = await info(req)
    const type = i.types.find((t) => t.path === req.params.path)
    if (!type) {
      res.status(404).type('text').send('Dit credential type bestaat niet.')
      return
    }
    res.type('html').send(renderTypeForm(type, chromeOf(i)))
  })

  // The form goes to the API as JSON; its answer is either that the entitlement is recorded or the reason it was refused.
  const create = (path: string, kind: Kind): express.RequestHandler => async (req, res) => {
    const answer = await fetch(`${api}${path}`, { method: 'POST', headers: { 'content-type': 'application/json', ...(await asStaff(req)) }, body: JSON.stringify(withRolesAsList(req.body ?? {})) })
    const body = (await answer.json()) as { entitlementId?: string; error?: string }
    if (!answer.ok || !body.entitlementId) {
      res.status(answer.ok ? 502 : answer.status).type('html').send(renderRefused(body.error ?? 'De uitgever stuurde een onbruikbaar antwoord.', chromeOf(await info(req)), kind))
      return
    }
    res.type('html').send(renderCreated(String(req.body?.email ?? ''), chromeOf(await info(req)), kind))
  }
  app.post('/employee', create('/entitlements/employee', { employee: true }))
  app.post('/type/:path', async (req, res, next) => {
    const type = (await info(req)).types.find((t) => t.path === req.params.path)
    if (!type) {
      res.status(404).type('text').send('Dit credential type bestaat niet.')
      return
    }
    await create(`/entitlements/type/${type.path}`, { type })(req, res, next)
  })

  // Revoking: look a person up, revoke what they hold, or revoke it and record the corrected data in its place.
  const activeOf = async (req: express.Request, email: string): Promise<{ list?: ActiveCredential[]; error?: string }> => {
    const answer = await fetch(`${api}/entitlements?email=${encodeURIComponent(email)}`, { headers: await asStaff(req) })
    const body = (await answer.json()) as { entitlements?: ActiveCredential[]; error?: string }
    return answer.ok ? { list: body.entitlements ?? [] } : { error: body.error ?? 'De uitgever stuurde een onbruikbaar antwoord.' }
  }
  const queryEmail = (req: express.Request) => (typeof req.query.email === 'string' ? req.query.email.trim() : '')
  const post = async (req: express.Request, path: string, body?: unknown) => {
    const answer = await fetch(`${api}${path}`, {
      method: 'POST',
      headers: { ...(body === undefined ? {} : { 'content-type': 'application/json' }), ...(await asStaff(req)) },
      body: body === undefined ? undefined : JSON.stringify(body),
    })
    return { ok: answer.ok, status: answer.status, body: (await answer.json()) as { error?: string; mailedTo?: string; entitlementId?: string } }
  }

  app.get('/revoke', async (req, res) => {
    const email = queryEmail(req)
    const chrome = chromeOf(await info(req))
    if (!email) {
      res.type('html').send(renderRevoke(chrome, ''))
      return
    }
    const { list, error } = await activeOf(req, email)
    res.status(error ? 400 : 200).type('html').send(renderRevoke(chrome, email, list, error))
  })
  app.post('/revoke/:id', async (req, res) => {
    const answer = await post(req, `/entitlements/revoke/${encodeURIComponent(req.params.id)}`)
    const chrome = chromeOf(await info(req))
    if (!answer.ok) {
      res.status(answer.status).type('html').send(renderNotRevoked(answer.body.error ?? 'De uitgever stuurde een onbruikbaar antwoord.', chrome, '/revoke'))
      return
    }
    res.type('html').send(renderRevoked(chrome))
  })
  app.get('/revoke/:id/correct', async (req, res) => {
    const email = queryEmail(req)
    const { list } = email ? await activeOf(req, email) : { list: undefined }
    const credential = list?.find((c) => c.id === req.params.id)
    if (!credential) {
      res.status(404).type('text').send('Dit credential bestaat niet of is al ingetrokken.')
      return
    }
    const i = await info(req)
    res.type('html').send(renderCorrection(credential, email, i.departments, i.roles, chromeOf(i)))
  })
  app.post('/revoke/:id/correct', async (req, res) => {
    const answer = await post(req, `/entitlements/revoke/${encodeURIComponent(req.params.id)}`, withRolesAsList(req.body ?? {}))
    const chrome = chromeOf(await info(req))
    if (!answer.ok) {
      res.status(answer.status).type('html').send(renderNotRevoked(answer.body.error ?? 'De uitgever stuurde een onbruikbaar antwoord.', chrome, '/revoke'))
      return
    }
    res.type('html').send(renderRevoked(chrome, String(req.body?.email ?? '')))
  })

  // Roles: the table asks for the list and sends changes as JSON; the API refuses a role an employee holds, and the page shows why.
  const passOn = (method: string, path: string): express.RequestHandler => async (req, res) => {
    const answer = await fetch(`${api}${path.replace(':id', encodeURIComponent(req.params.id as string))}`, {
      method,
      headers: { ...(method === 'GET' || method === 'DELETE' ? {} : { 'content-type': 'application/json' }), ...(await asStaff(req)) },
      body: method === 'GET' || method === 'DELETE' ? undefined : JSON.stringify({ name: req.body?.name ?? '' }),
    })
    res.status(answer.status).type('json').send(await answer.text())
  }
  app.get('/roles', async (req, res) => {
    res.type('html').send(renderRoles(chromeOf(await info(req))))
  })
  app.get('/roles/data', passOn('GET', '/roles'))
  app.post('/roles', passOn('POST', '/roles'))
  app.put('/roles/:id', passOn('PUT', '/roles/:id'))
  app.delete('/roles/:id', passOn('DELETE', '/roles/:id'))

  // Credential types: the list and the editor are pages; the editor saves as JSON, which the GUI passes on to the API, and asks for the preview.
  const managed = async (req: express.Request): Promise<ManagedType[]> => {
    const answer = await fetch(`${api}/credential-types`, { headers: await asStaff(req) })
    if (!answer.ok) throw new Error(`the issuer API answered ${answer.status}`)
    return ((await answer.json()) as { types: ManagedType[] }).types
  }
  const forward = (method: string, path: string): express.RequestHandler => async (req, res) => {
    const withBody = method === 'POST' || method === 'PUT'
    const answer = await fetch(`${api}${path.replace(':id', encodeURIComponent(req.params.id as string))}`, {
      method,
      headers: { ...(withBody ? { 'content-type': 'application/json' } : {}), ...(await asStaff(req)) },
      body: withBody ? JSON.stringify(req.body ?? {}) : undefined,
    })
    res.status(answer.status).type('json').send(await answer.text())
  }
  app.get('/credential-types', async (req, res) => {
    res.type('html').send(renderTypes(await managed(req), chromeOf(await info(req))))
  })
  app.get('/credential-types/new', async (req, res) => {
    res.type('html').send(renderTypeEditor(undefined, chromeOf(await info(req))))
  })
  app.get('/credential-types/:id/edit', async (req, res) => {
    const type = (await managed(req)).find((t) => t.id === req.params.id)
    if (!type) {
      res.status(404).type('text').send('Dit credential type bestaat niet.')
      return
    }
    res.type('html').send(renderTypeEditor(type, chromeOf(await info(req))))
  })
  app.post('/credential-types/preview', (req, res) => {
    const fields = Array.isArray(req.body?.fields) ? req.body.fields : []
    res.type('html').send(renderPreview(fields))
  })
  app.post('/credential-types', forward('POST', '/credential-types'))
  app.put('/credential-types/:id', forward('PUT', '/credential-types/:id'))
  app.delete('/credential-types/:id', forward('DELETE', '/credential-types/:id'))

  app.use((err: unknown, _req: express.Request, res: express.Response, _next: express.NextFunction) => {
    console.error(err)
    res.status(502).json({ error: 'kon de uitgever niet bereiken' })
  })

  const server: Server = await new Promise((resolve) => {
    const s = app.listen(options.port, () => resolve(s))
  })
  return {
    port: (server.address() as AddressInfo).port,
    async close() {
      await new Promise((resolve) => server.close(resolve))
    },
  }
}
