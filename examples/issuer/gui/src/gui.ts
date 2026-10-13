import type { Server } from 'node:http'
import type { AddressInfo } from 'node:net'
import express from 'express'
import { LOGO_SVG } from './logo.js'
import { renderCreated, renderEmployeeForm, renderForm, renderRetrieve, renderWelcome, type Chrome } from './page.js'

export interface GuiOptions {
  port: number
  /** Where the issuer API is reached; the pages ask it for what they show and to record every entitlement. */
  apiUrl: string
}

/** What the issuer API tells the pages. */
interface Info {
  issuerName?: string
  did: string
  departments: string[]
  descriptions: { Diploma: string; Employee: string }
  credentialIssuer: string
  credentialTypes: string[]
}

export async function startGui(options: GuiOptions) {
  const api = options.apiUrl.replace(/\/$/, '')
  const app = express()
  app.use(express.urlencoded({ extended: false }))

  const info = async (): Promise<Info> => {
    const res = await fetch(`${api}/info`)
    if (!res.ok) throw new Error(`the issuer API answered ${res.status}`)
    return (await res.json()) as Info
  }
  const chromeOf = ({ issuerName, did }: Info): Chrome => ({ issuerName, did })

  // Express 5 passes a rejected handler to the error handler below.
  app.get('/logo.svg', (_req, res) => {
    res.type('image/svg+xml').send(LOGO_SVG)
  })
  app.get('/', async (_req, res) => {
    res.type('html').send(renderWelcome(chromeOf(await info())))
  })
  app.get('/diploma', async (_req, res) => {
    const i = await info()
    res.type('html').send(renderForm(i.descriptions.Diploma, chromeOf(i), i.issuerName))
  })
  app.get('/ophalen', async (_req, res) => {
    const i = await info()
    res.type('html').send(renderRetrieve(i.credentialIssuer, i.credentialTypes, chromeOf(i)))
  })
  app.get('/employee', async (_req, res) => {
    const i = await info()
    res.type('html').send(renderEmployeeForm(i.departments, i.descriptions.Employee, chromeOf(i)))
  })

  // The form goes to the API as JSON; its answer is either that the entitlement is recorded or the reason it was refused.
  const create = (path: string, employee: boolean): express.RequestHandler => async (req, res) => {
    const answer = await fetch(`${api}${path}`, { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify(req.body ?? {}) })
    const body = (await answer.json()) as { entitlementId?: string; error?: string }
    if (!answer.ok || !body.entitlementId) {
      res.status(answer.ok ? 502 : answer.status).json({ error: body.error ?? 'the issuer API sent an unusable answer' })
      return
    }
    res.type('html').send(renderCreated(String(req.body?.email ?? ''), chromeOf(await info()), employee))
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
