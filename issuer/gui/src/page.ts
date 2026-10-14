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

const menuOf = (chrome: Chrome): MenuGroup[] => [{ label: 'Verifiable Credentials', items: [EMPLOYEE, ...chrome.diplomas.map(diplomaItem)] }]

/** What a form or a page after it is for: the employee credential, or the diploma at this path. */
export type Kind = { employee: true } | { diploma: DiplomaInfo }
const itemOf = (kind: Kind): MenuItem => ('employee' in kind ? EMPLOYEE : diplomaItem(kind.diploma))
const home: Crumb[] = [{ label: 'Home', href: '/' }]

// What every page shares: the menu, the logo, the footer and the way to sign out.
const shell = (chrome: Chrome, page: Pick<Page, 'title' | 'crumbs' | 'active' | 'body'>): string =>
  renderPage({ ...page, menu: menuOf(chrome), footer: footerOf(chrome), logo: logoOf(chrome), signOut: signOutOf(chrome) })

// Lets a drop-down with check boxes show what is chosen, and close when the page is clicked elsewhere.
const MULTI_SELECT = `document.querySelectorAll('details.multi').forEach((d) => {
  const summary = d.querySelector('summary'), none = summary.textContent
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

/** The form for one diploma: the person, and the day the diploma stops counting. */
export function renderDiplomaForm(diploma: DiplomaInfo, chrome: Chrome): string {
  const item = diplomaItem(diploma)
  return shell(chrome, {
    title: item.label,
    crumbs: home,
    active: item.id,
    body: `<p class="intro">${escape(item.description)}.</p>
<form class="card" method="post" action="${escape(item.href)}">
  <label>Naam <input name="name" required></label>
  <label>E-mailadres <input name="email" type="email" required></label>
  <label>Geldig tot <input name="validUntil" placeholder="dd-mm-jjjj" pattern="${datePattern}" title="dd-mm-jjjj" inputmode="numeric" required></label>
  <button type="submit">Credential aanmaken</button>
</form>`,
  })
}

const titleCase = (s: string) => s.charAt(0).toUpperCase() + s.slice(1)

export function renderEmployeeForm(departments: string[], roles: string[], chrome: Chrome): string {
  const options = departments.map((d) => `<option value="${escape(d)}">${escape(titleCase(d))}</option>`).join('')
  const roleOptions = roles.map((r) => `<label><input type="checkbox" name="roles" value="${escape(r)}">${escape(r)}</label>`).join('')
  return shell(chrome, {
    title: EMPLOYEE.label,
    crumbs: home,
    active: EMPLOYEE.id,
    body: `<p class="intro">${escape(EMPLOYEE.description)}</p>
<form class="card" method="post" action="/employee">
  <label>Naam <input name="name" required></label>
  <label>E-mailadres <input name="email" type="email" required></label>
  <label>Afdeling <select name="department">${options}</select></label>
  <label>Organisatie <input name="organisation" value="Gemeente Vlierdam" required></label>
  <div class="field"><span>Rol(len)</span>
    <details class="multi"><summary>Kies rol(len)</summary><div class="options">${roleOptions}</div></details></div>
  <button type="submit">Credential aanmaken</button>
</form>
<script>${MULTI_SELECT}</script>`,
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
