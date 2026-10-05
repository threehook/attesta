// The wallet on this machine tells the page which identities the user has; the page never sees anything else of the wallet.
const WALLET = 'http://127.0.0.1:47653'
const CREDENTIAL_TYPE = 'Employee'

export interface Identity {
  email: string
  // The DID of the issuer that vouches for the email.
  issuer: string
  // Whether this identity signs in to this application without the button.
  automatic: boolean
}

// application is the origin the wallet knows this app by. Rejects when the wallet is not running or does not answer.
export async function identities(application: string): Promise<Identity[]> {
  const query = new URLSearchParams({ type: CREDENTIAL_TYPE, application })
  const res = await fetch(`${WALLET}/identities?${query}`, { signal: AbortSignal.timeout(3000) })
  if (!res.ok) throw new Error(`wallet answered ${res.status}`)
  return ((await res.json()) as { identities: Identity[] }).identities
}

// Names the identity the wallet must answer with; the wallet reads these two parameters and takes them off.
export function withIdentity(link: string, identity: Identity): string {
  const hint = new URLSearchParams({ login_hint: identity.email, issuer_hint: identity.issuer })
  return `${link}${link.includes('?') ? '&' : '?'}${hint}`
}

export function shortDid(did: string): string {
  return did.length > 24 ? `${did.slice(0, 16)}…${did.slice(-6)}` : did
}
