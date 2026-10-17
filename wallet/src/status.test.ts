import { generateKeyPairSync, sign, type KeyObject } from 'node:crypto'
import { createServer, type Server } from 'node:http'
import type { AddressInfo } from 'node:net'
import { deflateSync } from 'node:zlib'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { standingOf } from './status.js'

const BASE58 = '123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz'

function didOf(key: KeyObject): string {
  const raw = Buffer.concat([Buffer.from([0xed, 0x01]), Buffer.from(key.export({ format: 'jwk' }).x!, 'base64url')])
  let n = BigInt('0x' + raw.toString('hex'))
  let out = ''
  while (n > 0n) {
    out = BASE58[Number(n % 58n)] + out
    n /= 58n
  }
  return 'did:key:z' + out
}

const issuer = generateKeyPairSync('ed25519')
const stranger = generateKeyPairSync('ed25519')
const did = didOf(issuer.publicKey)
const now = Date.parse('2026-10-17T12:00:00Z')

const b64 = (value: unknown) => Buffer.from(JSON.stringify(value)).toString('base64url')

// A status list token as the issuer signs it: one bit per credential, a set bit means revoked.
function list(revoked: number[], over: { signer?: KeyObject; sub?: string; iss?: string; exp?: number; typ?: string } = {}): string {
  const bits = Buffer.alloc(16)
  for (const i of revoked) bits[Math.floor(i / 8)]! |= 1 << i % 8
  const head = b64({ alg: 'EdDSA', typ: over.typ ?? 'statuslist+jwt' })
  const body = b64({
    iss: over.iss ?? did, sub: over.sub ?? uri, iat: now / 1000, exp: over.exp ?? now / 1000 + 3600,
    status_list: { bits: 1, lst: deflateSync(bits).toString('base64url') },
  })
  return `${head}.${body}.${sign(null, Buffer.from(`${head}.${body}`), over.signer ?? issuer.privateKey).toString('base64url')}`
}

let server: Server
let answer: { status: number; body: string }
let uri: string

beforeEach(async () => {
  answer = { status: 200, body: '' }
  server = createServer((_req, res) => res.writeHead(answer.status).end(answer.body))
  await new Promise<void>((resolve) => server.listen(0, resolve))
  uri = `http://localhost:${(server.address() as AddressInfo).port}/status/1`
})

afterEach(async () => {
  await new Promise((resolve) => server.close(resolve))
})

const credential = (idx: number, extra: Record<string, unknown> = {}) => ({ iss: did, vct: 'Employee', status: { status_list: { idx, uri } }, ...extra })
const standing = (payload: Record<string, unknown>) => standingOf(payload, { now: () => now })

describe('standingOf', () => {
  it('reads the bit of the credential in the list the issuer signed', async () => {
    answer.body = list([3, 9])

    expect(await standing(credential(3))).toBe('revoked')
    expect(await standing(credential(9))).toBe('revoked')
    expect(await standing(credential(4))).toBe('valid')
  })

  it('knows a credential past its end without asking the issuer', async () => {
    answer.body = list([])

    expect(await standing(credential(0, { exp: now / 1000 - 1 }))).toBe('ended')
    expect(await standing(credential(0, { exp: now / 1000 + 60 }))).toBe('valid')
  })

  it('does not take a list it cannot trust to mean anything', async () => {
    const cases: Record<string, string> = {
      'signed by someone else': list([3], { signer: stranger.privateKey }),
      'for another uri': list([3], { sub: uri + '/other' }),
      'from another issuer': list([3], { iss: didOf(stranger.publicKey) }),
      expired: list([3], { exp: now / 1000 - 1 }),
      'another type': list([3], { typ: 'jwt' }),
      'not a token': 'revoked',
      'an index beyond it': list([3]),
    }
    for (const [name, body] of Object.entries(cases)) {
      answer.body = body
      expect(await standing(credential(name === 'an index beyond it' ? 1000 : 3)), name).toBe('unknown')
    }
  })

  it('says unknown when the issuer cannot be reached or the credential has no status reference', async () => {
    answer = { status: 500, body: 'down' }
    expect(await standing(credential(3))).toBe('unknown')
    expect(await standing({ iss: did, vct: 'Employee' })).toBe('unknown')
    expect(await standing({ ...credential(3), iss: 'did:web:example.com' })).toBe('unknown')
    server.close()
    expect(await standing(credential(3))).toBe('unknown')
  })
})
