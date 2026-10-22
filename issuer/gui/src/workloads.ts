import { COLUMN_RESIZE, escape } from './layout.js'
import { home, shell, WORKLOADS, type Chrome } from './page.js'

// The list comes from /workloads/data. Every text goes in with textContent. An answer of the API is shown as it is.
const SCRIPT = `(() => {
  const $ = (id) => document.getElementById(id)
  const cell = (tr, text) => { const td = document.createElement('td'); td.textContent = text; tr.append(td); return td }
  const when = (iso) => new Intl.DateTimeFormat('nl-NL', { timeZone: 'Europe/Amsterdam', dateStyle: 'short', timeStyle: 'short' }).format(new Date(iso))
  const say = async (res, fallback) => { $('workload-error').textContent = res.ok ? '' : ((await res.json().catch(() => ({}))).error || fallback) }

  const load = async () => {
    const res = await fetch('/workloads/data')
    if (!res.ok) return
    const body = $('workloads-body')
    body.replaceChildren()
    const list = await res.json()
    for (const w of list) {
      const tr = document.createElement('tr')
      cell(tr, w.spiffeId).className = 'mono'; cell(tr, w.owner); cell(tr, w.environment); cell(tr, when(w.createdAt))
      const b = document.createElement('button'); b.type = 'button'; b.textContent = 'Intrekken'; b.className = 'danger'
      b.addEventListener('click', async () => { await say(await fetch('/workloads/' + encodeURIComponent(w.id), { method: 'DELETE' }), 'Intrekken is niet gelukt.'); load() })
      const td = document.createElement('td'); td.append(b); tr.append(td)
      body.append(tr)
    }
    if (!list.length) { const tr = document.createElement('tr'); cell(tr, 'Geen systemen.').colSpan = 5; body.append(tr) }
  }

  $('workload-form').addEventListener('submit', async (e) => {
    e.preventDefault()
    const res = await fetch('/workloads', {
      method: 'POST', headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ spiffeId: $('workload-spiffe').value, owner: $('workload-owner').value, environment: $('workload-environment').value }),
    })
    await say(res, 'Toevoegen is niet gelukt.')
    if (res.ok) { $('workload-spiffe').value = ''; $('workload-owner').value = ''; $('workload-environment').value = '' }
    load()
  })
  load()
})()`

/** The enrolled systems, and a form to enrol one. */
export function renderWorkloads(chrome: Chrome): string {
  return shell(chrome, {
    title: WORKLOADS.label,
    crumbs: home,
    active: WORKLOADS.id,
    body: `<p class="intro">${escape(WORKLOADS.description)}</p>
<form id="workload-form">
  <label>Systeem <input id="workload-spiffe" placeholder="spiffe://attesta.local/ns/attesta/sa/cicd-agent" required></label>
  <label>Eigenaar <input id="workload-owner" placeholder="platform" required></label>
  <label>Omgeving <input id="workload-environment" list="workload-environments" placeholder="staging" required></label>
  <datalist id="workload-environments"><option value="staging"><option value="production"></datalist>
  <div class="actions"><button type="submit">Toevoegen</button></div>
</form>
<p class="error" id="workload-error" role="alert"></p>
<div class="tablebox">
<table class="data">
  <thead><tr><th>Systeem</th><th>Eigenaar</th><th>Omgeving</th><th>Toegevoegd</th><th>Intrekken</th></tr></thead>
  <tbody id="workloads-body"></tbody>
</table>
</div>
<script>${COLUMN_RESIZE}</script>
<script>${SCRIPT}</script>`,
  })
}
