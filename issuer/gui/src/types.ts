import { typeFields, type FieldInfo, type TypeInfo } from './fields.js'
import { escape } from './layout.js'
import { CREDENTIAL_TYPES, home, shell, type Chrome } from './page.js'

/** A credential type as the API lists it for the management pages. */
export interface ManagedType extends TypeInfo {
  id: string
  inUse: boolean
}

const crumbs = [...home, { label: CREDENTIAL_TYPES.label, href: CREDENTIAL_TYPES.href }]

// The list asks for a delete in a popup and reloads when the API accepted it; a type in use shows why it stays.
const LIST_SCRIPT = `(() => {
  const dialog = document.getElementById('type-delete')
  let target = null
  document.querySelectorAll('button[data-delete]').forEach((b) => b.addEventListener('click', () => {
    target = b
    document.getElementById('type-delete-name').textContent = b.dataset.title
    document.getElementById('type-delete-error').textContent = ''
    dialog.showModal()
  }))
  document.querySelectorAll('dialog [data-close]').forEach((b) => b.addEventListener('click', () => b.closest('dialog').close()))
  document.getElementById('type-delete-yes').addEventListener('click', async () => {
    const res = await fetch('/credential-types/' + encodeURIComponent(target.dataset.delete), { method: 'DELETE' })
    if (res.ok) { location.reload(); return }
    const answer = await res.json().catch(() => ({}))
    document.getElementById('type-delete-error').textContent = answer.error || 'Verwijderen is niet gelukt.'
  })
})()`

export function renderTypes(types: ManagedType[], chrome: Chrome): string {
  const rows = types
    .map(
      (t) => `<tr><td>${escape(t.title)}</td><td class="mono">${escape(t.type)}</td><td>${t.fields.length}</td><td>${t.inUse ? 'Ja' : 'Nee'}</td>
<td><a href="/credential-types/${encodeURIComponent(t.id)}/edit">Wijzigen</a></td>
<td><button type="button" class="danger" data-delete="${escape(t.id)}" data-title="${escape(t.title)}"${t.inUse ? ' disabled title="Er is een credential van dit type"' : ''}>Verwijderen</button></td></tr>`,
    )
    .join('')
  return shell(chrome, {
    title: CREDENTIAL_TYPES.label,
    crumbs: home,
    active: CREDENTIAL_TYPES.id,
    body: `<p class="intro">${escape(CREDENTIAL_TYPES.description)}</p>
<form class="toolbar" method="get" action="/credential-types/new"><button type="submit">Credential type aanmaken</button></form>
<table class="data">
  <thead><tr><th>Titel</th><th>Type</th><th>Velden</th><th>In gebruik</th><th>Wijzigen</th><th>Verwijderen</th></tr></thead>
  <tbody>${rows || '<tr><td colspan="6">Er zijn nog geen credential types.</td></tr>'}</tbody>
</table>
<dialog id="type-delete">
  <h2>Credential type verwijderen</h2>
  <p>Weet u zeker dat u <strong id="type-delete-name"></strong> wilt verwijderen? Het menu-item verdwijnt.</p>
  <p class="error" id="type-delete-error" role="alert"></p>
  <div class="actions"><button type="button" class="danger" id="type-delete-yes">Ja, verwijderen</button><button type="button" class="secondary" data-close>Annuleren</button></div>
</dialog>
<script>${LIST_SCRIPT}</script>`,
  })
}

/** The form of a type as it will look, made by the same code as the real form. Nothing in it can be sent. */
export function renderPreview(fields: FieldInfo[]): string {
  return `<form class="card" onsubmit="return false">${typeFields(fields)}
  <button type="submit" disabled>Credential aanmaken</button>
</form>`
}

// The editor keeps the type in `state`; a change redraws the preview from the GUI, which renders it with the code of the real form.
const EDITOR_SCRIPT = `(() => {
  const $ = (id) => document.getElementById(id)
  const initial = JSON.parse($('type-data').textContent)
  const state = { title: initial.title, description: initial.description, fields: initial.fields.map((f) => ({ ...f, touched: !!f.key })) }
  const locked = initial.inUse
  const KINDS = { text: 'Tekst', email: 'E-mailadres', date: 'Datum', choice: 'Keuzelijst' }

  const key = (label) => {
    const words = label.normalize('NFD').replace(/[^A-Za-z0-9 ]/g, '').split(' ').filter(Boolean)
    return words.map((w, i) => (i ? w[0].toUpperCase() + w.slice(1) : w[0].toLowerCase() + w.slice(1))).join('')
  }
  let timer
  const preview = () => {
    clearTimeout(timer)
    timer = setTimeout(async () => {
      const res = await fetch('/credential-types/preview', { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ fields: state.fields }) })
      $('type-preview').innerHTML = res.ok ? await res.text() : ''
    }, 150)
  }
  const el = (tag, props = {}, ...children) => { const e = Object.assign(document.createElement(tag), props); e.append(...children); return e }
  const labelled = (text, input) => el('label', {}, text + ' ', input)

  const rows = () => {
    const list = $('type-fields')
    list.replaceChildren()
    state.fields.forEach((f, i) => {
      const fixed = (f.key === 'email' && f.kind === 'email') || (f.key === 'validUntil' && f.kind === 'date')
      const label = el('input', { value: f.label, disabled: locked })
      const name = el('input', { value: f.key, disabled: locked || fixed })
      const kind = el('select', { disabled: locked || fixed }, ...Object.entries(KINDS).map(([v, t]) => el('option', { value: v, textContent: t, selected: v === f.kind })))
      const required = el('input', { type: 'checkbox', checked: f.required, disabled: locked || fixed })
      const options = el('textarea', { value: (f.options || []).join('\\n'), rows: 3, disabled: locked })
      const optionsBox = labelled('Opties (een per regel)', options)
      optionsBox.hidden = f.kind !== 'choice'
      label.addEventListener('input', () => {
        f.label = label.value
        if (!f.touched && !fixed) { f.key = key(f.label); name.value = f.key }
        preview()
      })
      name.addEventListener('input', () => { f.touched = true; f.key = name.value; preview() })
      kind.addEventListener('change', () => { f.kind = kind.value; optionsBox.hidden = f.kind !== 'choice'; preview() })
      required.addEventListener('change', () => { f.required = required.checked; preview() })
      options.addEventListener('input', () => { f.options = options.value.split('\\n').map((o) => o.trim()).filter(Boolean); preview() })
      const move = (by) => el('button', { type: 'button', className: 'secondary', textContent: by < 0 ? '↑' : '↓', disabled: locked, ariaLabel: by < 0 ? 'Omhoog' : 'Omlaag' })
      const up = move(-1), down = move(1)
      up.addEventListener('click', () => { if (i > 0) { state.fields.splice(i - 1, 0, ...state.fields.splice(i, 1)); rows(); preview() } })
      down.addEventListener('click', () => { if (i < state.fields.length - 1) { state.fields.splice(i + 1, 0, ...state.fields.splice(i, 1)); rows(); preview() } })
      const remove = el('button', { type: 'button', className: 'danger', textContent: 'Verwijderen', disabled: locked || fixed })
      remove.addEventListener('click', () => { state.fields.splice(i, 1); rows(); preview() })
      list.append(el('section', { className: 'item' },
        labelled('Label', label), labelled('Naam van het veld', name), labelled('Soort', kind),
        el('label', {}, required, ' Verplicht'), optionsBox,
        el('div', { className: 'actions' }, up, down, remove)))
    })
  }

  const expires = $('type-expires')
  expires.checked = state.fields.some((f) => f.key === 'validUntil')
  expires.disabled = locked
  expires.addEventListener('change', () => {
    state.fields = state.fields.filter((f) => f.key !== 'validUntil')
    if (expires.checked) state.fields.push({ key: 'validUntil', label: 'Geldig tot', kind: 'date', required: true, options: [], touched: true })
    rows(); preview()
  })

  $('type-title').addEventListener('input', (e) => { state.title = e.target.value })
  $('type-description').addEventListener('input', (e) => { state.description = e.target.value })
  $('type-add').addEventListener('click', () => { state.fields.push({ key: '', label: '', kind: 'text', required: false, options: [] }); rows(); preview() })
  $('type-form').addEventListener('submit', async (e) => {
    e.preventDefault()
    const fields = state.fields.map(({ key, label, kind, required, options }) => ({ key, label, kind, required, options: kind === 'choice' ? options : [] }))
    const url = initial.id ? '/credential-types/' + encodeURIComponent(initial.id) : '/credential-types'
    const res = await fetch(url, { method: initial.id ? 'PUT' : 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ title: state.title, description: state.description, fields }) })
    if (res.ok) { location.href = '/credential-types'; return }
    const answer = await res.json().catch(() => ({}))
    $('type-error').textContent = answer.error || 'Opslaan is niet gelukt.'
  })
  rows()
  preview()
})()`

// JSON inside a script element ends at the first "</"; "<" is written as an escape so no title can end it.
const jsonForScript = (value: unknown) => JSON.stringify(value).replaceAll('<', '\\u003c')

const EMPTY: ManagedType = {
  id: '',
  inUse: false,
  type: '',
  path: '',
  title: '',
  description: '',
  fields: [{ key: 'email', label: 'E-mailadres', kind: 'email', required: true }],
}

/** The editor for a new type, or for `type`: its title, description and fields on the left, the form as it will look on the right. */
export function renderTypeEditor(type: ManagedType | undefined, chrome: Chrome): string {
  const t = type ?? EMPTY
  return shell(chrome, {
    title: type ? 'Credential type wijzigen' : 'Credential type aanmaken',
    crumbs,
    active: CREDENTIAL_TYPES.id,
    body: `${t.inUse ? '<p class="intro">Er is een credential van dit type: alleen de titel en de beschrijving kunnen worden gewijzigd.</p>' : ''}
<div class="editor">
  <form id="type-form" class="card">
    <label>Titel <input id="type-title" value="${escape(t.title)}" required></label>
    <label>Beschrijving <input id="type-description" value="${escape(t.description)}"></label>
    <h2>Velden</h2>
    <p class="intro">Het veld <code>email</code> staat vast: het identificeert de persoon.</p>
    <label><input type="checkbox" id="type-expires"> Het credential verloopt op een einddatum</label>
    <div id="type-fields"></div>
    <p><button type="button" class="secondary" id="type-add"${t.inUse ? ' disabled' : ''}>Veld toevoegen</button></p>
    <p class="error" id="type-error" role="alert"></p>
    <div class="actions"><button type="submit">Opslaan</button><a href="/credential-types">Annuleren</a></div>
  </form>
  <div><h2>Voorbeeld</h2><div id="type-preview"></div></div>
</div>
<script type="application/json" id="type-data">${jsonForScript(t)}</script>
<script>${EDITOR_SCRIPT}</script>`,
  })
}
