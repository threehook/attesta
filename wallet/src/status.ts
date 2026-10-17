import { createPublicKey, verify, type KeyObject } from 'node:crypto'
import { inflateSync } from 'node:zlib'

/** Where a held credential stands: still good, withdrawn by its issuer, past its end, or not to be established. */
export type Standing = 'valid' | 'revoked' | 'ended' | 'unknown'

const BASE58 = '123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz'
const MAX_LIST_BYTES = 4 << 20

// The Ed25519 key in a did:key, as the issuers here are.
function keyOfDid(did: string): KeyObject | undefined {
  if (!did.startsWith('did:key:z')) return undefined
  const encoded = did.slice('did:key:z'.length)
  let n = 0n
  for (const c of encoded) {
    const i = BASE58.indexOf(c)
    if (i < 0) return undefined
    n = n * 58n + BigInt(i)
  }
  let hex = n.toString(16)
  if (hex.length % 2) hex = '0' + hex
  const bytes = Buffer.concat([Buffer.alloc(/^1*/.exec(encoded)![0].length), Buffer.from(hex, 'hex')])
  if (bytes.length !== 34 || bytes[0] !== 0xed || bytes[1] !== 0x01) return undefined
  return createPublicKey({ key: { kty: 'OKP', crv: 'Ed25519', x: bytes.subarray(2).toString('base64url') }, format: 'jwk' })
}

const json = (part: string) => JSON.parse(Buffer.from(part, 'base64url').toString('utf8')) as Record<string, any>

export interface StandingOptions {
  now?: () => number
  fetch?: typeof fetch
}

/**
 * Reads the issuer's signed status list (draft-ietf-oauth-status-list) and the credential's own end. Only a list that the issuer signed, that is for this
 * credential's uri and has not expired counts; anything else is 'unknown', which is never taken to mean the credential is gone.
 */
export async function standingOf(payload: Record<string, any>, { now = Date.now, fetch: get = fetch }: StandingOptions = {}): Promise<Standing> {
  if (typeof payload.exp === 'number' && payload.exp * 1000 <= now()) return 'ended'
  try {
    const { idx, uri } = payload.status?.status_list ?? {}
    const key = typeof payload.iss === 'string' ? keyOfDid(payload.iss) : undefined
    if (!key || typeof uri !== 'string' || !Number.isInteger(idx) || idx < 0) return 'unknown'
    const answer = await get(uri, { signal: AbortSignal.timeout(5000) })
    if (!answer.ok) return 'unknown'
    const token = await answer.text()
    const parts = token.trim().split('.')
    if (parts.length !== 3 || token.length > MAX_LIST_BYTES) return 'unknown'
    const header = json(parts[0]!)
    if (header.typ !== 'statuslist+jwt' || header.alg !== 'EdDSA') return 'unknown'
    if (!verify(null, Buffer.from(`${parts[0]}.${parts[1]}`), key, Buffer.from(parts[2]!, 'base64url'))) return 'unknown'
    const claims = json(parts[1]!)
    if (claims.iss !== payload.iss || claims.sub !== uri || (typeof claims.exp === 'number' && claims.exp * 1000 <= now())) return 'unknown'
    if (claims.status_list?.bits !== 1 || typeof claims.status_list.lst !== 'string') return 'unknown'
    const bits = inflateSync(Buffer.from(claims.status_list.lst, 'base64url'))
    if (idx >= bits.length * 8) return 'unknown'
    return (bits[Math.floor(idx / 8)]! & (1 << idx % 8)) !== 0 ? 'revoked' : 'valid'
  } catch {
    return 'unknown'
  }
}
