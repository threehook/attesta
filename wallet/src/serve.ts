import type { IncomingMessage, RequestListener, ServerResponse } from 'node:http'
import type { HeldCredential } from './credentials.js'
import type { PresentationRequest } from './presentations.js'

/** What the service needs from a wallet; the real one is built on the agent in serve-cli.ts. */
export interface ServedWallet {
  /** Takes the credential an `openid-credential-offer://` link gives, replacing the one held of the same type. */
  accept(link: string): Promise<HeldCredential[]>
  /** Reads an `openid4vp://` request. */
  resolve(link: string): Promise<PresentationRequest>
  /** Answers a request that was read. */
  submit(request: PresentationRequest): Promise<{ status: number }>
  held(): Promise<HeldCredential[]>
}

/** A verifier may be answered when its client id starts with one of the registered prefixes. With none registered, none may. */
export function verifierAllowed(clientId: string, registered: string[]): boolean {
  return registered.some((prefix) => prefix !== '' && clientId.startsWith(prefix))
}

const MAX_BODY = 1 << 20

class Refused extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message)
  }
}

async function readLink(req: IncomingMessage, field: string, scheme: string): Promise<string> {
  let raw = ''
  for await (const chunk of req) {
    raw += chunk
    if (raw.length > MAX_BODY) throw new Refused(413, 'the request is too large')
  }
  let body: unknown
  try {
    body = JSON.parse(raw)
  } catch {
    throw new Refused(400, 'the request is not JSON')
  }
  const link = (body as Record<string, unknown> | null)?.[field]
  if (typeof link !== 'string' || !link.startsWith(`${scheme}:`)) throw new Refused(400, `${field} must be a ${scheme}: link`)
  return link
}

function reply(res: ServerResponse, status: number, body: unknown): void {
  res.writeHead(status, { 'content-type': 'application/json' }).end(JSON.stringify(body))
}

/**
 * The wallet of a system, answering by itself: no one confirms anything. It answers only the verifiers registered for it, and it is meant to listen on
 * the local address of the system it belongs to.
 */
export function createHandler(wallet: ServedWallet, verifiers: string[]): RequestListener {
  return async (req, res) => {
    try {
      const route = `${req.method} ${req.url}`
      if (route === 'GET /healthz') return reply(res, 200, { held: (await wallet.held()).length })
      if (route === 'POST /credentials') {
        const held = await wallet.accept(await readLink(req, 'offer', 'openid-credential-offer'))
        return reply(res, 200, { credentials: held.map((credential) => ({ type: credential.type, issuer: credential.issuer })) })
      }
      if (route === 'POST /present') {
        const request = await wallet.resolve(await readLink(req, 'link', 'openid4vp'))
        if (!verifierAllowed(request.verifier, verifiers)) throw new Refused(403, 'this verifier is not registered for this wallet')
        if (!request.satisfiable) throw new Refused(409, 'the wallet holds no credential that answers the request')
        const answered = await wallet.submit(request)
        return reply(res, 200, { verifier: request.verifier, status: answered.status })
      }
      if (['/healthz', '/credentials', '/present'].includes(req.url ?? '')) throw new Refused(405, 'method not allowed')
      throw new Refused(404, 'not found')
    } catch (error) {
      if (error instanceof Refused) return reply(res, error.status, { error: error.message })
      reply(res, 502, { error: error instanceof Error ? error.message : String(error) })
    }
  }
}
