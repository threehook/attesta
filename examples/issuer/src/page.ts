import { escape, MENU, renderPage, type Crumb } from './layout.js'

/** What the footer shows: the issuer the credentials are issued as. */
export interface Chrome {
  issuerName?: string
  did: string
}

const logoOf = ({ issuerName }: Chrome) => ({ src: '/logo.svg', alt: issuerName ?? 'Logo' })

const footerOf = ({ issuerName, did }: Chrome) => `Uitgever: ${issuerName ? `${issuerName} · ` : ''}${did}`

const [LAADPALEN] = MENU[0].items
const home: Crumb[] = [{ label: 'Home', href: '/' }]

export function renderWelcome(chrome: Chrome): string {
  const blocks = MENU.flatMap((group) =>
    group.items.map(
      (item) => `<a class="block" href="${escape(item.href)}"><p class="group">${escape(group.label)}</p><h2>${escape(item.label)}</h2><p>${escape(item.description)}</p></a>`,
    ),
  ).join('')
  return renderPage({
    title: 'Home',
    active: 'home',
    crumbs: [],
    body: `<p class="intro">Kies in het menu een verifiable credential om uit te geven aan een medewerker.</p><div class="blocks">${blocks}</div>`,
    footer: footerOf(chrome),
    logo: logoOf(chrome),
  })
}

export function renderForm(description: string, chrome: Chrome, university = ''): string {
  return renderPage({
    title: 'Diploma',
    crumbs: home,
    body: `<form class="card" method="post" action="/offers">
  <label>Naam <input name="name" required></label>
  <label>E-mailadres <input name="email" type="email" required></label>
  <label>Graad <input name="degree" required></label>
  <label>Opleidingsinstituut <input name="university" value="${escape(university)}" required></label>
  <label>Omschrijving <input name="description" value="${escape(description)}"></label>
  <button type="submit">Credential aanmaken</button>
</form>`,
    footer: footerOf(chrome),
    logo: logoOf(chrome),
  })
}

const titleCase = (s: string) => s.charAt(0).toUpperCase() + s.slice(1)

export function renderEmployeeForm(departments: string[], description: string, chrome: Chrome): string {
  const options = departments.map((d) => `<option value="${escape(d)}">${escape(titleCase(d))}</option>`).join('')
  return renderPage({
    title: LAADPALEN.label,
    crumbs: home,
    active: LAADPALEN.id,
    body: `<p class="intro">Creëer een diploma of certificaat (verifiable credential) voor één van uw werknemers.</p>
<form class="card" method="post" action="/employee/offers">
  <label>Naam <input name="name" required></label>
  <label>E-mailadres <input name="email" type="email" required></label>
  <label>Afdeling <select name="department">${options}</select></label>
  <label>Organisatie <input name="organisation" value="Gemeente Vlierdam" required></label>
  <label>Diploma <input name="diploma" value="laadpalen-management" required></label>
  <label>Diploma geldig tot <input name="diplomaValidUntil" placeholder="dd-mm-jjjj" pattern="\d{2}-\d{2}-\d{4}" title="dd-mm-jjjj" inputmode="numeric" required></label>
  <label>Omschrijving <input name="description" value="${escape(description)}"></label>
  <button type="submit">Credential aanmaken</button>
</form>`,
    footer: footerOf(chrome),
    logo: logoOf(chrome),
  })
}

/** Shown after an offer is created; `employee` keeps the menu item highlighted. */
export function renderOffer(offerUri: string, chrome: Chrome, employee: boolean): string {
  return renderPage({
    title: 'Aanbod',
    crumbs: employee ? [...home, { label: LAADPALEN.label, href: LAADPALEN.href }] : home,
    active: employee ? LAADPALEN.id : undefined,
    body: `<p class="intro">Open deze link in de wallet:</p>
<p><a href="${escape(offerUri)}"><code>${escape(offerUri)}</code></a></p>`,
    footer: footerOf(chrome),
    logo: logoOf(chrome),
  })
}
