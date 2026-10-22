import { describe, expect, it, vi } from 'vitest'
import { mappedFetch, parseHosts } from './hosts.js'

describe('parseHosts', () => {
  it('reads pairs, ignores blanks and a trailing slash', () => {
    expect(parseHosts('https://a.example=http://a:1, https://b.example/=http://b:2/ ,')).toEqual({ 'https://a.example': 'http://a:1', 'https://b.example': 'http://b:2' })
    expect(parseHosts('')).toEqual({})
  })

  it('refuses what is not a pair of addresses', () => {
    for (const text of ['a', '=http://a', 'https://a.example', 'https://a.example=ftp://a', 'https://a.example/x=http://a']) {
      expect(() => parseHosts(text), text).toThrow(/public-address=internal-address/)
    }
  })
})

describe('mappedFetch', () => {
  const hosts = { 'https://issuer.example': 'http://issuer:4001' }

  it('sends a public address to its internal address, keeping path and query', async () => {
    const base = vi.fn(async (_input: string | URL | Request, _init?: RequestInit) => new Response('ok'))
    const fetchIt = mappedFetch(base as unknown as typeof fetch, hosts)
    await fetchIt('https://issuer.example/oid4vci/offers/1?x=2', { method: 'POST' })
    await fetchIt(new URL('https://issuer.example/status/1'))
    expect(base.mock.calls[0]).toEqual(['http://issuer:4001/oid4vci/offers/1?x=2', { method: 'POST' }])
    expect(base.mock.calls[1]![0]).toBe('http://issuer:4001/status/1')
  })

  it('leaves other addresses, and names that only start the same, alone', async () => {
    const base = vi.fn(async (_input: string | URL | Request, _init?: RequestInit) => new Response('ok'))
    const fetchIt = mappedFetch(base as unknown as typeof fetch, hosts)
    await fetchIt('https://other.example/x')
    await fetchIt('https://issuer.example.evil.test/x')
    expect(base.mock.calls.map((call) => call[0])).toEqual(['https://other.example/x', 'https://issuer.example.evil.test/x'])
  })

  it('maps a request object too', async () => {
    const base = vi.fn(async (_input: string | URL | Request, _init?: RequestInit) => new Response('ok'))
    await mappedFetch(base as unknown as typeof fetch, hosts)(new Request('https://issuer.example/oid4vci/token', { method: 'POST', body: 'a=b' }))
    expect((base.mock.calls[0]![0] as Request).url).toBe('http://issuer:4001/oid4vci/token')
  })
})
