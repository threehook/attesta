// The laadpalen backend's own contract; the page never talks to attesta.

export interface Subject {
  issuer: string
  email: string
}

export interface Submitted {
  requestId: string
  // The openid4vp:// link the employee opens in their wallet.
  authorizationRequest: string
}

export interface Status {
  status: 'pending' | 'done' | 'expired'
  // attesta's decision on the employee, and why.
  authorized: boolean
  reason?: string
  subject?: Subject
  // What became of the request itself; only present for an authorized employee.
  result?: { granted: boolean; reason: string }
  debug: { authorizationRequest: string; outcome?: unknown }
}

async function json<T>(res: Response): Promise<T> {
  const body = (await res.json().catch(() => ({}))) as { error?: string }
  if (!res.ok) throw new Error(body.error ?? `HTTP ${res.status}`)
  return body as T
}

export async function submit(postcode: string, huisnummer: string): Promise<Submitted> {
  return json(
    await fetch('/api/request-laadpaal', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ postcode, huisnummer }),
    }),
  )
}

export async function status(requestId: string): Promise<Status> {
  return json(await fetch(`/api/request-laadpaal/${encodeURIComponent(requestId)}`))
}
