import type { Server } from 'node:http'
import type { AddressInfo } from 'node:net'
import express from 'express'
import type { Auth } from './auth.js'
import { LOGO_SVG } from './logo.js'
import { renderCreated, renderEmployeeForm, renderForm, renderWelcome, type Chrome } from './page.js'

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
  descriptions: { Diploma: string; Employee: string }
}

export async function startGui(options: GuiOptions) {
  const api = options.apiUrl.replace(/\/$/, '')
  const app = express()
  app.use(express.urlencoded({ extended: false }))

  const asStaff = async (req: express.Request): Promise<Record<string, string>> => {
    const token = await options.auth?.token(req)
    return token ? { authorization: `Bearer ${token}` } : {}
  }
  const info = async (req: express.Request): Promise<Info> => {
    const res = await fetch(`${api}/info`, { headers: await asStaff(req) })
    if (!res.ok) throw new Error(`the issuer API answered ${res.status}`)
    return (await res.json()) as Info
  }
  const chromeOf = ({ issuerName, did }: Info): Chrome => ({ issuerName, did, signOut: !!options.auth })

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
  app.get('/diploma', async (req, res) => {
    const i = await info(req)
    res.type('html').send(renderForm(i.descriptions.Diploma, chromeOf(i), i.issuerName))
  })
  app.get('/employee', async (req, res) => {
    const i = await info(req)
    res.type('html').send(renderEmployeeForm(i.departments, i.descriptions.Employee, chromeOf(i)))
  })

  // The form goes to the API as JSON; its answer is either that the entitlement is recorded or the reason it was refused.
  const create = (path: string, employee: boolean): express.RequestHandler => async (req, res) => {
    const answer = await fetch(`${api}${path}`, { method: 'POST', headers: { 'content-type': 'application/json', ...(await asStaff(req)) }, body: JSON.stringify(req.body ?? {}) })
    const body = (await answer.json()) as { entitlementId?: string; error?: string }
    if (!answer.ok || !body.entitlementId) {
      res.status(answer.ok ? 502 : answer.status).json({ error: body.error ?? 'the issuer API sent an unusable answer' })
      return
    }
    res.type('html').send(renderCreated(String(req.body?.email ?? ''), chromeOf(await info(req)), employee))
  }
  app.post('/diploma', create('/entitlements/diploma', false))
  app.post('/employee', create('/entitlements/employee', true))

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
