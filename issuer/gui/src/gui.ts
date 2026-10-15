import type { Server } from 'node:http'
import type { AddressInfo } from 'node:net'
import express from 'express'
import type { Auth } from './auth.js'
import { LOGO_SVG } from './logo.js'
import {
  renderCorrection,
  renderCreated,
  renderDiplomaForm,
  renderEmployeeForm,
  renderNotRevoked,
  renderRefused,
  renderRevoke,
  renderRevoked,
  renderRoles,
  renderWelcome,
  type ActiveCredential,
  type Chrome,
  type DiplomaInfo,
  type Kind,
} from './page.js'

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
  diplomas: DiplomaInfo[]
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
  const chromeOf = ({ issuerName, did, diplomas }: Info): Chrome => ({ issuerName, did, diplomas, signOut: !!options.auth })

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
  app.get('/diploma/:path', async (req, res) => {
    const i = await info(req)
    const diploma = i.diplomas.find((d) => d.path === req.params.path)
    if (!diploma) {
      res.status(404).type('text').send('Dit diploma bestaat niet.')
      return
    }
    res.type('html').send(renderDiplomaForm(diploma, chromeOf(i)))
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
  app.post('/diploma/:path', async (req, res, next) => {
    const diploma = (await info(req)).diplomas.find((d) => d.path === req.params.path)
    if (!diploma) {
      res.status(404).type('text').send('Dit diploma bestaat niet.')
      return
    }
    await create(`/entitlements/diploma/${diploma.path}`, { diploma })(req, res, next)
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
