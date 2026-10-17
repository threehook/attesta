import { COLUMN_RESIZE, escape } from './layout.js'
import { CREDENTIALS, home, shell, type Chrome } from './page.js'

// The credentials table: the list comes from /credentials/data, and filtering, sorting and paging happen here. A revocation goes to the GUI, which passes it
// on to the API; the table is read again when the popup closes. Changing a credential is a page of its own, since every type has other fields.
const SCRIPT = `(() => {
  const $ = (id) => document.getElementById(id)
  const state = { items: [], filter: '', key: 'created', dir: -1, size: 10, page: 0 }
  const dialog = $('credential-revoke')
  let target = null

  const day = (iso) => new Intl.DateTimeFormat('en-GB', { timeZone: 'Europe/Amsterdam', day: '2-digit', month: '2-digit', year: 'numeric' }).format(new Date(iso)).replaceAll('/', '-')
  const ends = (c) => (c.claims.validUntil ? c.claims.validUntil.split('-').reverse().join('-') : '')
  const row = (c) => ({
    ...c, email: c.claims.email || '', created: c.createdAt, issued: c.issuedAt || '', end: c.claims.validUntil || '',
    state: c.status === 'issued' ? 'Uitgegeven' : 'Klaar om op te halen',
  })
  const cell = (tr, text) => { const td = document.createElement('td'); td.textContent = text; tr.append(td); return td }
  const cellOf = (tr, node) => { const td = document.createElement('td'); td.append(node); tr.append(td) }

  const render = () => {
    const f = state.filter.toLowerCase()
    const rows = state.items.filter((c) => [c.id, c.email, c.title, c.state, ...Object.values(c.claims)].some((v) => String(v).toLowerCase().includes(f)))
    rows.sort((a, b) => String(a[state.key]).localeCompare(String(b[state.key])) * state.dir)
    const pages = Math.max(1, Math.ceil(rows.length / state.size))
    state.page = Math.min(state.page, pages - 1)
    const from = state.page * state.size, shown = rows.slice(from, from + state.size)
    const body = $('credentials-body')
    body.replaceChildren()
    for (const c of shown) {
      const tr = document.createElement('tr')
      cell(tr, c.id).className = 'mono'; cell(tr, c.email); cell(tr, c.title); cell(tr, c.state)
      cell(tr, c.issuedAt ? day(c.issuedAt) : '–'); cell(tr, ends(c) || '–')
      const change = document.createElement('button')
      change.type = 'button'; change.textContent = 'Wijzigen'
      change.addEventListener('click', () => { location.href = '/credentials/' + encodeURIComponent(c.id) + '/correct' })
      cellOf(tr, change)
      const revoke = document.createElement('button')
      revoke.type = 'button'; revoke.className = 'danger'; revoke.textContent = 'Intrekken'
      revoke.addEventListener('click', () => openRevoke(c))
      cellOf(tr, revoke)
      body.append(tr)
    }
    if (!shown.length) { const tr = document.createElement('tr'); const td = cell(tr, 'Geen credentials gevonden.'); td.colSpan = 8; body.append(tr) }
    $('credentials-range').textContent = rows.length ? (from + 1) + '–' + (from + shown.length) + ' van ' + rows.length : '0 van 0'
    $('credentials-prev').disabled = state.page === 0
    $('credentials-next').disabled = state.page >= pages - 1
    $('credentials-first').disabled = state.page === 0
    $('credentials-last').disabled = state.page >= pages - 1
    $('credentials-pages').textContent = 'Pagina ' + (state.page + 1) + ' van ' + pages
    document.querySelectorAll('th[data-key]').forEach((th) => {
      th.setAttribute('aria-sort', th.dataset.key === state.key ? (state.dir > 0 ? 'ascending' : 'descending') : 'none')
    })
  }
  const load = async () => {
    const res = await fetch('/credentials/data')
    if (res.ok) { state.items = (await res.json()).entitlements.map(row); render() }
  }

  function openRevoke(c) {
    target = c
    $('credential-revoke-title').textContent = c.title + ' van ' + c.email
    $('credential-revoke-error').textContent = ''
    dialog.showModal()
  }
  $('credential-revoke-yes').addEventListener('click', async () => {
    const res = await fetch('/credentials/' + encodeURIComponent(target.id) + '/revoke', { method: 'POST' })
    if (res.ok) { dialog.close(); return }
    const answer = await res.json().catch(() => ({}))
    $('credential-revoke-error').textContent = answer.error || 'Intrekken is niet gelukt.'
  })
  document.querySelectorAll('dialog [data-close]').forEach((b) => b.addEventListener('click', () => b.closest('dialog').close()))
  dialog.addEventListener('close', load)

  $('credentials-filter').addEventListener('input', (e) => { state.filter = e.target.value; state.page = 0; render() })
  $('credentials-size').addEventListener('change', (e) => { state.size = Number(e.target.value); state.page = 0; render() })
  $('credentials-first').addEventListener('click', () => { state.page = 0; render() })
  $('credentials-prev').addEventListener('click', () => { state.page--; render() })
  $('credentials-next').addEventListener('click', () => { state.page++; render() })
  $('credentials-last').addEventListener('click', () => { state.page = Infinity; render() })
  document.querySelectorAll('th[data-key] button').forEach((b) => b.addEventListener('click', () => {
    const key = b.closest('th').dataset.key
    state.dir = state.key === key ? -state.dir : 1
    state.key = key
    render()
  }))
  load()
})()`

const sortable = (key: string, label: string, sort = 'none') => `<th data-key="${key}" aria-sort="${sort}"><button type="button" class="sort">${label}</button></th>`

/** All credentials in a table that can be filtered, sorted and paged, each with a way to change or revoke it. Nothing is added here. */
export function renderCredentials(chrome: Chrome): string {
  return shell(chrome, {
    title: CREDENTIALS.label,
    crumbs: home,
    active: CREDENTIALS.id,
    body: `<p class="intro">${escape(CREDENTIALS.description)}</p>
<div class="toolbar">
  <input id="credentials-filter" type="search" placeholder="Filter op e-mailadres, credential, status of id" aria-label="Filter">
</div>
<div class="tablebox">
<table class="data">
  <thead><tr>
    ${sortable('id', 'ID')}${sortable('email', 'E-mailadres')}${sortable('title', 'Credential')}${sortable('state', 'Status')}${sortable('issued', 'Uitgegeven op')}${sortable('end', 'Geldig tot')}
    <th>Wijzigen</th><th>Intrekken</th>
  </tr></thead>
  <tbody id="credentials-body"></tbody>
</table>
<div class="pager">
  <label>Credentials per pagina <select id="credentials-size"><option>5</option><option selected>10</option><option>25</option><option>50</option></select></label>
  <span id="credentials-range"></span>
  <span class="nav">
    <button type="button" id="credentials-first" aria-label="Eerste pagina">&laquo;</button>
    <button type="button" id="credentials-prev">Vorige</button>
    <span id="credentials-pages"></span>
    <button type="button" id="credentials-next">Volgende</button>
    <button type="button" id="credentials-last" aria-label="Laatste pagina">&raquo;</button>
  </span>
</div>
</div>
<dialog id="credential-revoke">
  <h2>Credential intrekken</h2>
  <p>Weet u zeker dat u <strong id="credential-revoke-title"></strong> wilt intrekken?</p>
  <p class="error" id="credential-revoke-error" role="alert"></p>
  <div class="actions"><button type="button" class="danger" id="credential-revoke-yes">Ja, intrekken</button><button type="button" class="secondary" data-close>Annuleren</button></div>
</dialog>
<script>${COLUMN_RESIZE}</script>
<script>${SCRIPT}</script>`,
  })
}
