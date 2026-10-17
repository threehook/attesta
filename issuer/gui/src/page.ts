import { asFormDate, typeFields, type TypeInfo, type Values } from './fields.js'
import { COLUMN_RESIZE, escape, renderPage, type Crumb, type MenuGroup, type MenuItem, type Page } from './layout.js'

export type { TypeInfo } from './fields.js'

/** What the footer shows: the issuer the credentials are issued as. */
export interface Chrome {
  issuerName?: string
  did: string
  /** The credential types the issuer gives besides Employee; each has a form and a menu item of its own. */
  types: TypeInfo[]
  /** Whether the pages offer a way to sign out. */
  signOut?: boolean
}

const signOutOf = ({ signOut }: Chrome) => signOut

const logoOf = ({ issuerName }: Chrome) => ({ src: '/logo.svg', alt: issuerName ?? 'Logo' })

const footerOf = ({ issuerName, did }: Chrome) => `Uitgever: ${issuerName ? `${issuerName} · ` : ''}${did}`

const EMPLOYEE: MenuItem = {
  id: 'employee',
  label: 'Medewerker',
  href: '/employee',
  description: 'Geef een medewerker een verifiable credential met afdeling en rollen.',
}

const typeItem = (t: TypeInfo): MenuItem => ({ id: `type-${t.path}`, label: t.title, href: `/type/${t.path}`, description: t.description })

export const CREDENTIALS: MenuItem = {
  id: 'credentials',
  label: 'Credentials',
  href: '/credentials',
  description: 'Bekijk alle credentials en wijzig ze of trek ze in. Wijzigen trekt het credential in en maakt een gecorrigeerd credential voor in de plaats.',
}

const ROLES: MenuItem = {
  id: 'roles',
  label: 'Rollen',
  href: '/roles',
  description: 'Maak rollen aan, wijzig ze en verwijder ze. Een rol die een medewerker heeft, kan niet worden verwijderd.',
}

export const CREDENTIAL_TYPES: MenuItem = {
  id: 'credential-types',
  label: 'Credential Forms',
  href: '/credential-types',
  description: 'Maak eigen credential forms. Elk formulier krijgt een menu-item onder Verifiable Credentials.',
}

const menuOf = (chrome: Chrome): MenuGroup[] => [
  { label: 'Verifiable Credentials', items: [EMPLOYEE, ...chrome.types.map(typeItem)] },
  { label: 'Beheer', items: [ROLES, CREDENTIALS, CREDENTIAL_TYPES] },
]

/** What a form or a page after it is for: the employee credential, or the type at this path. */
export type Kind = { employee: true } | { type: TypeInfo }
const itemOf = (kind: Kind): MenuItem => ('employee' in kind ? EMPLOYEE : typeItem(kind.type))
export const home: Crumb[] = [{ label: 'Home', href: '/' }]

// What every page shares: the menu, the logo, the footer and the way to sign out.
export const shell = (chrome: Chrome, page: Pick<Page, 'title' | 'crumbs' | 'active' | 'body'>): string =>
  renderPage({ ...page, menu: menuOf(chrome), footer: footerOf(chrome), logo: logoOf(chrome), signOut: signOutOf(chrome) })

// Lets a drop-down with check boxes show what is chosen, and close when the page is clicked elsewhere.
const MULTI_SELECT = `document.querySelectorAll('details.multi').forEach((d) => {
  const summary = d.querySelector('summary'), none = d.dataset.none
  d.addEventListener('change', () => {
    const chosen = [...d.querySelectorAll('input:checked')].map((i) => i.value)
    summary.textContent = chosen.length ? chosen.join(', ') : none
  })
  document.addEventListener('click', (e) => { if (!d.contains(e.target)) d.open = false })
})`

export function renderWelcome(chrome: Chrome): string {
  const blocks = menuOf(chrome)
    .flatMap((group) =>
      group.items.map(
        (item) => `<a class="block" href="${escape(item.href)}"><p class="group">${escape(group.label)}</p><h2>${escape(item.label)}</h2><p>${escape(item.description)}</p></a>`,
      ),
    )
    .join('')
  return shell(chrome, {
    title: 'Home',
    active: 'home',
    crumbs: [],
    body: `<p class="intro">Maak een keuze in het menu om verder te gaan.</p><div class="blocks">${blocks}</div>`,
  })
}

const attr = (name: string, value: string | undefined) => (value === undefined ? '' : ` ${name}="${escape(value)}"`)

const employeeFields = (departments: string[], roles: string[], v: Values = {}, emailReadonly = false) => {
  const options = departments.map((d) => `<option value="${escape(d)}"${d === v.department ? ' selected' : ''}>${escape(titleCase(d))}</option>`).join('')
  const chosen = (v.roles ?? '').split(',').map((r) => r.trim()).filter(Boolean)
  const roleOptions = roles.map((r) => `<label><input type="checkbox" name="roles" value="${escape(r)}"${chosen.includes(r) ? ' checked' : ''}>${escape(r)}</label>`).join('')
  const none = 'Kies rol(len)'
  return `
  <label>Naam <input name="name"${attr('value', v.name)} required></label>
  <label>E-mailadres <input name="email" type="email"${attr('value', v.email)} required${emailReadonly ? ' readonly' : ''}></label>
  <label>Afdeling <select name="department">${options}</select></label>
  <label>Organisatie <input name="organisation" value="${escape(v.organisation ?? 'Gemeente Vlierdam')}" required></label>
  <div class="field"><span>Rol(len)</span>
    <details class="multi" data-none="${none}"><summary>${escape(chosen.length ? chosen.join(', ') : none)}</summary><div class="options">${roleOptions}</div></details></div>`
}

const titleCase = (s: string) => s.charAt(0).toUpperCase() + s.slice(1)

/** The form for one credential type, made from its fields. */
export function renderTypeForm(type: TypeInfo, chrome: Chrome): string {
  const item = typeItem(type)
  return shell(chrome, {
    title: item.label,
    crumbs: home,
    active: item.id,
    body: `<p class="intro">${escape(item.description)}.</p>
<form class="card" method="post" action="${escape(item.href)}">${typeFields(type.fields)}
  <button type="submit">Credential aanmaken</button>
</form>`,
  })
}

export function renderEmployeeForm(departments: string[], roles: string[], chrome: Chrome): string {
  return shell(chrome, {
    title: EMPLOYEE.label,
    crumbs: home,
    active: EMPLOYEE.id,
    body: `<p class="intro">${escape(EMPLOYEE.description)}</p>
<form class="card" method="post" action="/employee">${employeeFields(departments, roles)}
  <button type="submit">Credential aanmaken</button>
</form>
<script>${MULTI_SELECT}</script>`,
  })
}

/** A credential that can be revoked, as the issuer API lists it. */
export interface ActiveCredential {
  id: string
  type: string
  title: string
  /** `waiting` is recorded but not fetched by the wallet yet. */
  status: 'waiting' | 'issued'
  claims: Record<string, string>
  issuedAt: string | null
  createdAt: string
}

const crumbsToCredentials: Crumb[] = [...home, { label: CREDENTIALS.label, href: CREDENTIALS.href }]

/** The form of a credential with its data filled in, to withdraw it and record the corrected one. */
export function renderCorrection(c: ActiveCredential, departments: string[], roles: string[], chrome: Chrome): string {
  const employee = c.type === 'Employee'
  const fields = chrome.types.find((t) => t.type === c.type)?.fields ?? []
  const values: Values = { ...c.claims }
  for (const f of fields) if (f.kind === 'date') values[f.key] = asFormDate(c.claims[f.key]) ?? ''
  return shell(chrome, {
    title: 'Credential wijzigen',
    crumbs: crumbsToCredentials,
    active: CREDENTIALS.id,
    body: `<p class="intro">${escape(c.title)}: het huidige credential wordt ingetrokken en de ontvanger krijgt een e-mail met een QR-code voor het gecorrigeerde credential.</p>
<p class="intro">Het e-mailadres kan niet worden gewijzigd: het identificeert de persoon, dus een ander adres is een credential voor iemand anders. Is het adres fout? Trek dit credential dan in en maak een nieuw credential aan.</p>
<form class="card" method="post" action="/credentials/${encodeURIComponent(c.id)}/correct">${employee ? employeeFields(departments, roles, values, true) : typeFields(fields, values, ['email'])}
  <button type="submit">Wijzigen</button>
</form>${employee ? `<script>${MULTI_SELECT}</script>` : ''}`,
  })
}

/** Shown after a credential was changed: the old one is revoked and the corrected one was mailed to `email`. */
export function renderChanged(chrome: Chrome, email: string): string {
  return shell(chrome, {
    title: 'Credential gewijzigd',
    crumbs: crumbsToCredentials,
    active: CREDENTIALS.id,
    body: `<p class="intro">Het credential is ingetrokken. Er is een e-mail met een QR-code voor het gecorrigeerde credential gestuurd naar <strong>${escape(email)}</strong>.</p>
<p><a href="${escape(CREDENTIALS.href)}">Terug naar de credentials</a></p>`,
  })
}

/** Shown when the issuer API refused to change a credential. */
export function renderNotChanged(message: string, chrome: Chrome): string {
  return shell(chrome, {
    title: 'Niet gewijzigd',
    crumbs: crumbsToCredentials,
    active: CREDENTIALS.id,
    body: `<p class="intro">${escape(message)}</p><p><a href="${escape(CREDENTIALS.href)}">Terug naar de credentials</a></p>`,
  })
}

/** Shown after an entitlement is recorded; `kind` keeps its menu item highlighted. */
export function renderCreated(email: string, chrome: Chrome, kind: Kind): string {
  const item = itemOf(kind)
  return shell(chrome, {
    title: 'Credential klaargezet',
    crumbs: [...home, { label: item.label, href: item.href }],
    active: item.id,
    body: `<p class="intro">Er is een e-mail met een QR-code gestuurd naar <strong>${escape(email)}</strong>. De ontvanger zet die in de wallet, en het credential komt er vanzelf in.</p>`,
  })
}

/** Shown when the issuer API refused the form; the person who filled it in reads why and can go back. */
export function renderRefused(message: string, chrome: Chrome, kind: Kind): string {
  const item = itemOf(kind)
  return shell(chrome, {
    title: 'Niet vastgelegd',
    crumbs: [...home, { label: item.label, href: item.href }],
    active: item.id,
    body: `<p class="intro">${escape(message)}</p><p><a href="${escape(item.href)}">Terug naar het formulier</a></p>`,
  })
}

// The roles table: the list comes from /roles/data, and filtering, sorting and paging happen here. A change goes to the GUI, which passes it on to the API;
// the table is read again when a popup closes.
const ROLES_SCRIPT = `(() => {
  const $ = (id) => document.getElementById(id)
  const state = { roles: [], filter: '', key: 'name', dir: 1, size: 10, page: 0 }
  const edit = $('role-edit'), remove = $('role-delete')
  let target = null

  const cell = (tr, text) => { const td = document.createElement('td'); td.textContent = text; tr.append(td); return td }
  const action = (tr, label, role, cls, open) => {
    const b = document.createElement('button')
    b.type = 'button'; b.textContent = label; if (cls) b.className = cls
    b.addEventListener('click', () => open(role))
    const td = document.createElement('td'); td.append(b); tr.append(td)
  }
  const render = () => {
    const f = state.filter.toLowerCase()
    const rows = state.roles.filter((r) => r.name.toLowerCase().includes(f) || r.id.includes(f))
    rows.sort((a, b) => a[state.key].localeCompare(b[state.key]) * state.dir)
    const pages = Math.max(1, Math.ceil(rows.length / state.size))
    state.page = Math.min(state.page, pages - 1)
    const from = state.page * state.size, shown = rows.slice(from, from + state.size)
    const body = $('roles-body')
    body.replaceChildren()
    for (const r of shown) {
      const tr = document.createElement('tr')
      cell(tr, r.id).className = 'mono'; cell(tr, r.name)
      action(tr, 'Wijzigen', r, '', openEdit); action(tr, 'Verwijderen', r, 'danger', openDelete)
      body.append(tr)
    }
    if (!shown.length) { const tr = document.createElement('tr'); const td = cell(tr, 'Geen rollen gevonden.'); td.colSpan = 4; body.append(tr) }
    $('roles-range').textContent = rows.length ? (from + 1) + '–' + (from + shown.length) + ' van ' + rows.length : '0 van 0'
    $('roles-prev').disabled = state.page === 0
    $('roles-next').disabled = state.page >= pages - 1
    $('roles-first').disabled = state.page === 0
    $('roles-last').disabled = state.page >= pages - 1
    $('roles-pages').textContent = 'Pagina ' + (state.page + 1) + ' van ' + pages
    document.querySelectorAll('th[data-key]').forEach((th) => {
      th.setAttribute('aria-sort', th.dataset.key === state.key ? (state.dir > 0 ? 'ascending' : 'descending') : 'none')
    })
  }
  const load = async () => {
    const res = await fetch('/roles/data')
    if (res.ok) { state.roles = (await res.json()).roles; render() }
  }

  function openEdit(role) {
    target = role
    $('role-edit-title').textContent = role ? 'Rol wijzigen' : 'Rol aanmaken'
    $('role-edit-name').value = role ? role.name : ''
    $('role-edit-error').textContent = ''
    edit.showModal()
  }
  function openDelete(role) {
    target = role
    $('role-delete-name').textContent = role.name
    $('role-delete-error').textContent = ''
    $('role-delete-ask').hidden = role.inUse
    $('role-delete-yes').hidden = role.inUse
    $('role-delete-busy').hidden = !role.inUse
    $('role-delete-close').textContent = role.inUse ? 'Sluiten' : 'Annuleren'
    remove.showModal()
  }
  const send = async (method, url, body, errorBox, dialog) => {
    const res = await fetch(url, { method, headers: { 'content-type': 'application/json' }, body })
    if (res.ok) { dialog.close(); return }
    const answer = await res.json().catch(() => ({}))
    errorBox.textContent = answer.error || 'De wijziging is niet gelukt.'
  }
  $('role-add').addEventListener('click', () => openEdit(null))
  $('role-edit-form').addEventListener('submit', (e) => {
    e.preventDefault()
    const body = JSON.stringify({ name: $('role-edit-name').value })
    send(target ? 'PUT' : 'POST', target ? '/roles/' + encodeURIComponent(target.id) : '/roles', body, $('role-edit-error'), edit)
  })
  $('role-delete-yes').addEventListener('click', () => send('DELETE', '/roles/' + encodeURIComponent(target.id), undefined, $('role-delete-error'), remove))
  document.querySelectorAll('dialog [data-close]').forEach((b) => b.addEventListener('click', () => b.closest('dialog').close()))
  for (const d of [edit, remove]) d.addEventListener('close', load)

  $('roles-filter').addEventListener('input', (e) => { state.filter = e.target.value; state.page = 0; render() })
  $('roles-size').addEventListener('change', (e) => { state.size = Number(e.target.value); state.page = 0; render() })
  $('roles-first').addEventListener('click', () => { state.page = 0; render() })
  $('roles-prev').addEventListener('click', () => { state.page--; render() })
  $('roles-next').addEventListener('click', () => { state.page++; render() })
  $('roles-last').addEventListener('click', () => { state.page = Infinity; render() })
  document.querySelectorAll('th[data-key] button').forEach((b) => b.addEventListener('click', () => {
    const key = b.closest('th').dataset.key
    state.dir = state.key === key ? -state.dir : 1
    state.key = key
    render()
  }))
  load()
})()`

/** The roles in a table that can be filtered, sorted and paged, with popups to add, change and delete one. */
export function renderRoles(chrome: Chrome): string {
  return shell(chrome, {
    title: ROLES.label,
    crumbs: home,
    active: ROLES.id,
    body: `<p class="intro">${escape(ROLES.description)}</p>
<div class="toolbar">
  <button type="button" id="role-add">Rol aanmaken</button>
  <input id="roles-filter" type="search" placeholder="Filter op id of naam" aria-label="Filter">
</div>
<div class="tablebox">
<table class="data">
  <thead><tr>
    <th data-key="id" aria-sort="none"><button type="button" class="sort">ID</button></th>
    <th data-key="name" aria-sort="ascending"><button type="button" class="sort">Rol</button></th>
    <th>Wijzigen</th><th>Verwijderen</th>
  </tr></thead>
  <tbody id="roles-body"></tbody>
</table>
<div class="pager">
  <label>Rollen per pagina <select id="roles-size"><option>5</option><option selected>10</option><option>25</option><option>50</option></select></label>
  <span id="roles-range"></span>
  <span class="nav">
    <button type="button" id="roles-first" aria-label="Eerste pagina">&laquo;</button>
    <button type="button" id="roles-prev">Vorige</button>
    <span id="roles-pages"></span>
    <button type="button" id="roles-next">Volgende</button>
    <button type="button" id="roles-last" aria-label="Laatste pagina">&raquo;</button>
  </span>
</div>
</div>
<dialog id="role-edit">
  <form id="role-edit-form">
    <h2 id="role-edit-title"></h2>
    <label>Naam <input id="role-edit-name" required></label>
    <p class="error" id="role-edit-error" role="alert"></p>
    <div class="actions"><button type="submit">Opslaan</button><button type="button" class="secondary" data-close>Annuleren</button></div>
  </form>
</dialog>
<dialog id="role-delete">
  <h2>Rol verwijderen</h2>
  <p id="role-delete-ask">Weet u zeker dat u de rol <strong id="role-delete-name"></strong> wilt verwijderen?</p>
  <p id="role-delete-busy" hidden>Deze rol kan niet worden verwijderd: er is minimaal één medewerker met deze rol.</p>
  <p class="error" id="role-delete-error" role="alert"></p>
  <div class="actions"><button type="button" class="danger" id="role-delete-yes">Ja, verwijderen</button><button type="button" class="secondary" id="role-delete-close" data-close>Annuleren</button></div>
</dialog>
<script>${COLUMN_RESIZE}</script>
<script>${ROLES_SCRIPT}</script>`,
  })
}
