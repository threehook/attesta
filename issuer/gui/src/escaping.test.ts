import { createServer, type Server } from 'node:http'
import type { AddressInfo } from 'node:net'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { typeFields, type FieldInfo } from './fields.js'
import { startGui } from './gui.js'
import { renderPreview, renderTypeEditor } from './types.js'

// Text that tries to leave the place it is written in: a tag, a quoted attribute, a script, a comment.
const PAYLOADS = [
  '<script>alert(1)</script>',
  '"><img src=x onerror=alert(1)>',
  "' onfocus='alert(1)",
  '" autofocus onfocus="alert(1)',
  '</script><script>alert(1)</script>',
  '<!--',
  '</select><script>alert(1)</script>',
  '</label><iframe srcdoc=x>',
  '&lt;b&gt;&amp;',
  'javascript:alert(1)',
  ' <svg onload=1>',
  '`onmouseover=`',
]

const TAGS = new Set(['form', 'label', 'input', 'select', 'option', 'button'])
const ATTRIBUTES = new Set(['class', 'name', 'type', 'step', 'min', 'max', 'value', 'pattern', 'title', 'placeholder', 'inputmode', 'required', 'readonly', 'selected', 'disabled', 'onsubmit'])
const TAG = /^<(\/?)([a-z]+)((?:\s+[a-z-]+(?:="[^"<>]*")?)*)\s*>$/

/** Fails unless the HTML is made of the form's own tags and attributes: nothing a payload wrote is a tag, an attribute or script. */
function onlyOurMarkup(html: string): void {
  for (const part of html.split(/(<[^<>]*>)/)) {
    if (part.startsWith('<')) {
      const tag = TAG.exec(part)
      expect(tag, `not one of our tags: ${part}`).not.toBeNull()
      expect(TAGS.has(tag![2]!), `tag ${tag![2]} is not one of ours`).toBe(true)
      // Without their quoted values, what is left of a tag is its attributes' names.
      for (const name of tag![3]!.replace(/="[^"<>]*"/g, '').matchAll(/\s([a-z-]+)/g)) expect(ATTRIBUTES.has(name[1]!), `attribute ${name[1]} in ${part}`).toBe(true)
      if (part.includes(' onsubmit')) expect(part).toContain('onsubmit="return false"')
    } else {
      expect(part, 'text with a tag in it').not.toMatch(/[<>]/)
    }
  }
}

const decode = (s: string) => s.replace(/&#(\d+);/g, (_m, code) => String.fromCharCode(Number(code)))

const fieldsWith = (payload: string): FieldInfo[] =>
  (['text', 'email', 'date', 'choice', 'number'] as const).map((kind) => ({
    key: payload,
    label: payload,
    kind,
    required: true,
    options: kind === 'choice' ? [payload] : undefined,
    min: kind === 'number' ? payload : undefined,
    max: kind === 'number' ? payload : undefined,
    decimals: kind === 'number' ? (payload as never) : undefined,
  }))

describe('the form of a credential type', () => {
  it.each(PAYLOADS)('writes %j as text and as attribute values, never as markup', (payload) => {
    const fields = fieldsWith(payload)
    onlyOurMarkup(typeFields(fields))
    onlyOurMarkup(typeFields(fields, Object.fromEntries(fields.map((f) => [f.key, payload])), [payload]))
    onlyOurMarkup(renderPreview(fields))
  })

  it('writes what was typed so that it reads back the same', () => {
    const payload = '"><img src=x onerror=alert(1)>'
    const html = typeFields([{ key: 'name', label: payload, kind: 'text', required: false }], { name: payload })
    expect(decode(html)).toContain(`<label>${payload} `)
    expect(decode(html)).toContain(`value="${payload}"`)
  })
})

describe('the editor of a credential type', () => {
  const chrome = { issuerName: 'Gemeente Utrecht', did: 'did:key:z6Mk', types: [] }

  it.each(PAYLOADS)('keeps %j in the title, the description and the data it hands to the script', (payload) => {
    const type = {
      id: 't-1', type: payload, path: 'x', title: payload, description: payload, inUse: false,
      fields: [{ key: 'email', label: payload, kind: 'email' as const, required: true, options: [payload] }],
    }
    const html = renderTypeEditor(type as never, chrome)

    expect(decode(/id="type-title" value="([^"]*)"/.exec(html)![1]!)).toBe(payload)
    expect(decode(/id="type-description" value="([^"]*)"/.exec(html)![1]!)).toBe(payload)
    // The data is JSON in a script tag. If a payload ended the tag, what the page reads back would be cut off and would not parse.
    const data = /<script type="application\/json" id="type-data">([\s\S]*?)<\/script>/.exec(html)![1]!
    expect(data).not.toContain('<')
    expect(JSON.parse(data)).toEqual(type)
  })
})

describe('the preview and the form pages over HTTP', () => {
  let api: Server
  let gui: Awaited<ReturnType<typeof startGui>>
  let base: string
  const evil = { id: 't-1', type: PAYLOADS[1], path: 'evil', title: PAYLOADS[0], description: PAYLOADS[4], inUse: false, fields: fieldsWith(PAYLOADS[3]!) }

  beforeEach(async () => {
    api = createServer((req, res) => {
      res.setHeader('content-type', 'application/json')
      if (req.url === '/info') res.end(JSON.stringify({ issuerName: 'X', did: 'did:key:z6Mk', departments: [], roles: [], types: [evil] }))
      else if (req.url === '/credential-types') res.end(JSON.stringify({ types: [evil] }))
      else res.end('{}')
    })
    await new Promise<void>((resolve) => api.listen(0, resolve))
    gui = await startGui({ port: 0, apiUrl: `http://localhost:${(api.address() as AddressInfo).port}` })
    base = `http://localhost:${gui.port}`
  })

  afterEach(async () => {
    await gui.close()
    await new Promise((resolve) => api.close(resolve))
  })

  const preview = (body: unknown) => fetch(`${base}/credential-types/preview`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })

  it.each(PAYLOADS)('answers a preview of %j with markup of the form only', async (payload) => {
    const res = await preview({ fields: fieldsWith(payload) })
    expect(res.status).toBe(200)
    expect(res.headers.get('content-type')).toContain('text/html')
    onlyOurMarkup(await res.text())
  })

  it('does not echo what it cannot use: a field that is not text is an error, not markup', async () => {
    for (const label of [5, null, ['<script>1</script>'], { replace: '<script>1</script>' }]) {
      const res = await preview({ fields: [{ key: 'a', label, kind: 'text', required: false }] })
      expect(await res.text()).not.toContain('<script')
    }
    const notAList = await preview({ fields: '<script>1</script>' })
    expect(await notAList.text()).not.toContain('<script')
  })

  it('writes the type that is edited, and the form that records it, without markup of its own', async () => {
    const editor = await (await fetch(`${base}/credential-types/t-1/edit`)).text()
    expect(editor).not.toContain('<script>alert(1)</script>')
    expect(editor).not.toContain('<img src=x')
    const recording = await (await fetch(`${base}/type/evil`)).text()
    expect(recording).not.toContain('<script>alert(1)</script>')
    expect(recording).not.toContain('<img src=x')
  })
})
