import { describe, expect, it } from 'vitest'
import { applyIdentityHint } from './presentations.js'

const link = (extra: Record<string, string> = {}) =>
  `openid4vp://?${new URLSearchParams({
    client_id: 'redirect_uri:https://api.example/response',
    state: 's 1',
    dcql_query: JSON.stringify({ credentials: [{ id: 'c', format: 'dc+sd-jwt', meta: { vct_values: ['Employee'] }, claims: [{ path: ['email'] }] }] }),
    ...extra,
  })}`

const queryOf = (uri: string) => JSON.parse(new URL(uri).searchParams.get('dcql_query') ?? '{}')

describe('applyIdentityHint', () => {
  it('leaves a link without a hint exactly as it is', () => {
    const uri = link()
    expect(applyIdentityHint(uri)).toEqual({ uri })
  })

  it('narrows the query to the hinted email and issuer and takes the hint off the link', () => {
    const { uri, hint } = applyIdentityHint(link({ login_hint: 'jerry@example.com', issuer_hint: 'did:key:z6Mk1' }))
    expect(hint).toEqual({ email: 'jerry@example.com', issuer: 'did:key:z6Mk1' })
    expect(new URL(uri).searchParams.has('login_hint')).toBe(false)
    expect(new URL(uri).searchParams.has('issuer_hint')).toBe(false)
    expect(new URL(uri).searchParams.get('state')).toBe('s 1')
    expect(queryOf(uri).credentials[0].claims).toEqual([
      { path: ['email'], values: ['jerry@example.com'] },
      { path: ['iss'], values: ['did:key:z6Mk1'] },
    ])
  })

  it('ignores a hint that names only one of the two', () => {
    const { uri, hint } = applyIdentityHint(link({ login_hint: 'jerry@example.com' }))
    expect(hint).toBeUndefined()
    expect(queryOf(uri).credentials[0].claims).toEqual([{ path: ['email'] }])
    expect(new URL(uri).searchParams.has('login_hint')).toBe(false)
  })

  it('does not let a verifier add a second issuer condition', () => {
    const query = { credentials: [{ id: 'c', claims: [{ path: ['email'] }, { path: ['iss'], values: ['did:key:other'] }] }] }
    const { uri } = applyIdentityHint(link({ login_hint: 'a@b.c', issuer_hint: 'did:key:z6Mk1', dcql_query: JSON.stringify(query) }))
    expect(queryOf(uri).credentials[0].claims).toEqual([
      { path: ['email'], values: ['a@b.c'] },
      { path: ['iss'], values: ['did:key:z6Mk1'] },
    ])
  })

  it('leaves something that is not a link alone', () => {
    expect(applyIdentityHint('not a link')).toEqual({ uri: 'not a link' })
  })
})
