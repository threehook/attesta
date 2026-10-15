import { escape, renderPage, type Crumb, type MenuGroup, type MenuItem, type Page } from './layout.js'

/** What the footer shows: the issuer the credentials are issued as. */
export interface Chrome {
  issuerName?: string
  did: string
  /** The diplomas the issuer gives; each has a form and a menu item of its own. */
  diplomas: DiplomaInfo[]
  /** Whether the pages offer a way to sign out. */
  signOut?: boolean
}

const signOutOf = ({ signOut }: Chrome) => signOut

const logoOf = ({ issuerName }: Chrome) => ({ src: '/logo.svg', alt: issuerName ?? 'Logo' })

const footerOf = ({ issuerName, did }: Chrome) => `Uitgever: ${issuerName ? `${issuerName} · ` : ''}${did}`

/** A diploma the issuer gives, as the API describes it. */
export interface DiplomaInfo {
  type: string
  path: string
  title: string
  description: string
}

const EMPLOYEE: MenuItem = {
  id: 'employee',
  label: 'Medewerker',
  href: '/employee',
  description: 'Geef een medewerker een verifiable credential met afdeling en rollen.',
}

const diplomaItem = (d: DiplomaInfo): MenuItem => ({ id: `diploma-${d.path}`, label: d.title, href: `/diploma/${d.path}`, description: d.description })

const REVOKE: MenuItem = {
  id: 'revoke',
  label: 'Intrekken',
  href: '/revoke',
  description: 'Trek een credential in en maak er eventueel een gecorrigeerd credential voor in de plaats.',
}

const menuOf = (chrome: Chrome): MenuGroup[] => [
  { label: 'Verifiable Credentials', items: [EMPLOYEE, ...chrome.diplomas.map(diplomaItem)] },
  { label: 'Beheer', items: [REVOKE] },
]

/** What a form or a page after it is for: the employee credential, or the diploma at this path. */
export type Kind = { employee: true } | { diploma: DiplomaInfo }
const itemOf = (kind: Kind): MenuItem => ('employee' in kind ? EMPLOYEE : diplomaItem(kind.diploma))
const home: Crumb[] = [{ label: 'Home', href: '/' }]

// What every page shares: the menu, the logo, the footer and the way to sign out.
const shell = (chrome: Chrome, page: Pick<Page, 'title' | 'crumbs' | 'active' | 'body'>): string =>
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

const datePattern = '\\d{2}-\\d{2}-\\d{4}'

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

/** What a form starts with: nothing for a new credential, the recorded data for a correction. */
type Values = Record<string, string>

const attr = (name: string, value: string | undefined) => (value === undefined ? '' : ` ${name}="${escape(value)}"`)

const diplomaFields = (v: Values = {}) => `
  <label>Naam <input name="name"${attr('value', v.name)} required></label>
  <label>E-mailadres <input name="email" type="email"${attr('value', v.email)} required></label>
  <label>Geldig tot <input name="validUntil" placeholder="dd-mm-jjjj" pattern="${datePattern}" title="dd-mm-jjjj" inputmode="numeric"${attr('value', v.validUntil)} required></label>`

const employeeFields = (departments: string[], roles: string[], v: Values = {}) => {
  const options = departments.map((d) => `<option value="${escape(d)}"${d === v.department ? ' selected' : ''}>${escape(titleCase(d))}</option>`).join('')
  const chosen = (v.roles ?? '').split(',').map((r) => r.trim()).filter(Boolean)
  const roleOptions = roles.map((r) => `<label><input type="checkbox" name="roles" value="${escape(r)}"${chosen.includes(r) ? ' checked' : ''}>${escape(r)}</label>`).join('')
  const none = 'Kies rol(len)'
  return `
  <label>Naam <input name="name"${attr('value', v.name)} required></label>
  <label>E-mailadres <input name="email" type="email"${attr('value', v.email)} required></label>
  <label>Afdeling <select name="department">${options}</select></label>
  <label>Organisatie <input name="organisation" value="${escape(v.organisation ?? 'Gemeente Vlierdam')}" required></label>
  <div class="field"><span>Rol(len)</span>
    <details class="multi" data-none="${none}"><summary>${escape(chosen.length ? chosen.join(', ') : none)}</summary><div class="options">${roleOptions}</div></details></div>`
}

const titleCase = (s: string) => s.charAt(0).toUpperCase() + s.slice(1)

/** The form for one diploma: the person, and the day the diploma stops counting. */
export function renderDiplomaForm(diploma: DiplomaInfo, chrome: Chrome): string {
  const item = diplomaItem(diploma)
  return shell(chrome, {
    title: item.label,
    crumbs: home,
    active: item.id,
    body: `<p class="intro">${escape(item.description)}.</p>
<form class="card" method="post" action="${escape(item.href)}">${diplomaFields()}
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
}

const crumbsToRevoke: Crumb[] = [...home, { label: REVOKE.label, href: REVOKE.href }]

const CLAIM_LABELS: [string, string][] = [
  ['name', 'Naam'],
  ['email', 'E-mailadres'],
  ['department', 'Afdeling'],
  ['organisation', 'Organisatie'],
  ['roles', 'Rol(len)'],
  ['validUntil', 'Geldig tot'],
]

const day = (iso: string) => new Intl.DateTimeFormat('en-GB', { timeZone: 'Europe/Amsterdam', day: '2-digit', month: '2-digit', year: 'numeric' }).format(new Date(iso)).replaceAll('/', '-')

/** yyyy-mm-dd, as the issuer stores a date, as the dd-mm-jjjj the forms take. */
const asFormDate = (value: string | undefined) => (value && /^\d{4}-\d{2}-\d{2}$/.test(value) ? value.split('-').reverse().join('-') : value)

/** Looks up the credentials of one person; `credentials` is undefined until an e-mail address was asked for. */
export function renderRevoke(chrome: Chrome, email: string, credentials?: ActiveCredential[], problem?: string): string {
  const items = (credentials ?? [])
    .map((c) => {
      const claims = CLAIM_LABELS.filter(([key]) => c.claims[key])
        .map(([key, label]) => `<dt>${label}</dt><dd>${escape(key === 'validUntil' ? (asFormDate(c.claims[key]) ?? '') : c.claims[key])}</dd>`)
        .join('')
      const state = c.status === 'issued' && c.issuedAt ? `Uitgegeven op ${day(c.issuedAt)}` : 'Staat klaar om op te halen'
      const id = encodeURIComponent(c.id)
      return `<section class="item"><h2>${escape(c.title)}</h2><p class="meta">${state}</p><dl>${claims}</dl>
<div class="actions">
  <form method="post" action="/revoke/${id}" onsubmit="return confirm('Dit credential intrekken?')"><button class="danger" type="submit">Intrekken</button></form>
  <a href="/revoke/${id}/correct?email=${encodeURIComponent(email)}">Intrekken en corrigeren</a>
</div></section>`
    })
    .join('')
  const result =
    credentials === undefined
      ? ''
      : credentials.length === 0
        ? `<p>Er is geen credential om in te trekken voor <strong>${escape(email)}</strong>.</p>`
        : items
  return shell(chrome, {
    title: REVOKE.label,
    crumbs: home,
    active: REVOKE.id,
    body: `<p class="intro">${escape(REVOKE.description)}</p>
<form class="card" method="get" action="/revoke">
  <label>E-mailadres <input name="email" type="email" value="${escape(email)}" required></label>
  <button type="submit">Zoeken</button>
</form>
${problem ? `<p>${escape(problem)}</p>` : ''}<div style="margin-top:20px">${result}</div>`,
  })
}

/** The form of a credential with its data filled in, to withdraw it and record the corrected one. */
export function renderCorrection(c: ActiveCredential, email: string, departments: string[], roles: string[], chrome: Chrome): string {
  const employee = c.type === 'Employee'
  const values: Values = { ...c.claims, validUntil: asFormDate(c.claims.validUntil) ?? '' }
  return shell(chrome, {
    title: 'Intrekken en corrigeren',
    crumbs: [...home, { label: REVOKE.label, href: `${REVOKE.href}?email=${encodeURIComponent(email)}` }],
    active: REVOKE.id,
    body: `<p class="intro">${escape(c.title)}: het huidige credential wordt ingetrokken en de ontvanger krijgt een e-mail met een QR-code voor het gecorrigeerde credential.</p>
<form class="card" method="post" action="/revoke/${encodeURIComponent(c.id)}/correct">${employee ? employeeFields(departments, roles, values) : diplomaFields(values)}
  <button type="submit">Intrekken en vervangen</button>
</form>${employee ? `<script>${MULTI_SELECT}</script>` : ''}`,
  })
}

/** Shown after a credential was revoked; `replacedFor` is the address the corrected credential was mailed to. */
export function renderRevoked(chrome: Chrome, replacedFor?: string): string {
  return shell(chrome, {
    title: 'Credential ingetrokken',
    crumbs: crumbsToRevoke,
    active: REVOKE.id,
    body: replacedFor
      ? `<p class="intro">Het credential is ingetrokken. Er is een e-mail met een QR-code voor het gecorrigeerde credential gestuurd naar <strong>${escape(replacedFor)}</strong>.</p>`
      : `<p class="intro">Het credential is ingetrokken.</p>`,
  })
}

/** Shown when the issuer API refused to revoke; `back` is where the person can try again. */
export function renderNotRevoked(message: string, chrome: Chrome, back: string): string {
  return shell(chrome, {
    title: 'Niet ingetrokken',
    crumbs: crumbsToRevoke,
    active: REVOKE.id,
    body: `<p class="intro">${escape(message)}</p><p><a href="${escape(back)}">Terug</a></p>`,
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
