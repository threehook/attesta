const escape = (s: string) => s.replace(/[&<>"']/g, (c) => `&#${c.charCodeAt(0)};`)

const style = `body{font-family:system-ui,sans-serif;max-width:32rem;margin:3rem auto;padding:0 1rem}
label{display:block;margin:.75rem 0}input{width:100%;padding:.4rem;box-sizing:border-box}
code{word-break:break-all}`

export function renderForm(description: string, university = ''): string {
  return `<!doctype html><html lang="nl"><meta charset="utf-8"><title>Diploma-uitgever</title><style>${style}</style>
<h1>Creëer een diploma of certificaat (verifiable credential) voor één van uw werknemers</h1>
<form method="post" action="/offers">
  <label>Naam <input name="name" required></label>
  <label>E-mailadres <input name="email" type="email" required></label>
  <label>Graad <input name="degree" required></label>
  <label>Opleidingsinstituut <input name="university" value="${escape(university)}" required></label>
  <label>Omschrijving <input name="description" value="${escape(description)}"></label>
  <button type="submit">Maak aanbod</button>
</form>`
}

const titleCase = (s: string) => s.charAt(0).toUpperCase() + s.slice(1)

export function renderEmployeeForm(departments: string[], description: string): string {
  const options = departments.map((d) => `<option value="${escape(d)}">${escape(titleCase(d))}</option>`).join('')
  return `<!doctype html><html lang="nl"><meta charset="utf-8"><title>Medewerker-uitgever</title><style>${style}select{padding:.4rem}</style>
<h1>Medewerker-ID</h1>
<p><a href="/">Geef in plaats daarvan een diploma uit</a></p>
<form method="post" action="/employee/offers">
  <label>Naam <input name="name" required></label>
  <label>E-mailadres <input name="email" type="email" required></label>
  <label>Afdeling <select name="department">${options}</select></label>
  <label>Organisatie <input name="organisation" value="Gemeente Vlierdam" required></label>
  <label>Diploma <input name="diploma" value="laadpalen-management" required></label>
  <label>Diploma geldig tot <input name="diplomaValidUntil" type="date" required></label>
  <label>Omschrijving <input name="description" value="${escape(description)}"></label>
  <button type="submit">Maak aanbod</button>
</form>`
}

export function renderOffer(offerUri: string): string {
  return `<!doctype html><meta charset="utf-8"><title>Aanbod</title><style>${style}</style>
<h1>Aanbod</h1>
<p>Open deze link in de wallet:</p>
<p><a href="${escape(offerUri)}"><code>${escape(offerUri)}</code></a></p>
<p><a href="/">Geef er nog een uit</a></p>`
}
