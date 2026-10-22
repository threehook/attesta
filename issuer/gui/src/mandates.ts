import { COLUMN_RESIZE, escape } from './layout.js'
import { home, MANDATES, shell, type Chrome } from './page.js'

// The list comes from /mandates/data and the systems from /workloads/data. Every text goes in with textContent. An answer of the API is shown as it is.
const SCRIPT = `(() => {
  const $ = (id) => document.getElementById(id)
  const cell = (tr, text) => { const td = document.createElement('td'); td.textContent = text; tr.append(td); return td }
  const when = (iso) => new Intl.DateTimeFormat('nl-NL', { timeZone: 'Europe/Amsterdam', dateStyle: 'short', timeStyle: 'short' }).format(new Date(iso))
  const say = async (res, fallback) => { $('mandate-error').textContent = res.ok ? '' : ((await res.json().catch(() => ({}))).error || fallback) }
  const button = (tr, label, className, onClick) => {
    const b = document.createElement('button'); b.type = 'button'; b.textContent = label; if (className) b.className = className
    b.addEventListener('click', onClick); const td = document.createElement('td'); td.append(b); tr.append(td)
    return b
  }

  const load = async () => {
    const [systems, mandates] = await Promise.all([fetch('/workloads/data'), fetch('/mandates/data')])
    if (systems.ok) {
      const chosen = $('mandate-workload').value
      $('mandate-workload').replaceChildren(...(await systems.json()).map((w) => new Option(w.spiffeId + ' (' + w.environment + ')', w.spiffeId)))
      $('mandate-workload').value = chosen
    }
    if (!mandates.ok) return
    const body = $('mandates-body')
    body.replaceChildren()
    const list = await mandates.json()
    for (const m of list) {
      const tr = document.createElement('tr')
      cell(tr, m.workload).className = 'mono'; cell(tr, m.resource).className = 'mono'; cell(tr, when(m.expiresAt)); cell(tr, m.approvers.join(', '))
      const approve = button(tr, m.approvedByMe ? 'Goedgekeurd door u' : 'Goedkeuren', '', async () => { await say(await fetch('/mandates/' + encodeURIComponent(m.id) + '/approvals', { method: 'POST' }), 'Goedkeuren is niet gelukt.'); load() })
      approve.disabled = m.approvedByMe
      button(tr, 'Beëindigen', 'danger', async () => { await say(await fetch('/mandates/' + encodeURIComponent(m.id), { method: 'DELETE' }), 'Beëindigen is niet gelukt.'); load() })
      body.append(tr)
    }
    if (!list.length) { const tr = document.createElement('tr'); cell(tr, 'Geen mandaten.').colSpan = 6; body.append(tr) }
  }

  $('mandate-form').addEventListener('submit', async (e) => {
    e.preventDefault()
    const res = await fetch('/mandates', {
      method: 'POST', headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ workload: $('mandate-workload').value, resource: $('mandate-resource').value, expiresAt: new Date($('mandate-until').value).toISOString() }),
    })
    await say(res, 'Goedkeuren is niet gelukt.')
    if (res.ok) $('mandate-resource').value = ''
    load()
  })
  load()
})()`

/** The active mandates, and a form to approve a new one. */
export function renderMandates(chrome: Chrome): string {
  return shell(chrome, {
    title: MANDATES.label,
    crumbs: home,
    active: MANDATES.id,
    body: `<p class="intro">${escape(MANDATES.description)}</p>
<form id="mandate-form">
  <label>Systeem <select id="mandate-workload" required></select></label>
  <label>Bron <input id="mandate-resource" placeholder="deploy:prod:v1.4.*" required></label>
  <label>Geldig tot <input id="mandate-until" type="datetime-local" required></label>
  <div class="actions"><button type="submit">Goedkeuren</button></div>
</form>
<p class="error" id="mandate-error" role="alert"></p>
<div class="tablebox">
<table class="data">
  <thead><tr><th>Systeem</th><th>Bron</th><th>Geldig tot</th><th>Goedgekeurd door</th><th>Goedkeuren</th><th>Beëindigen</th></tr></thead>
  <tbody id="mandates-body"></tbody>
</table>
</div>
<script>${COLUMN_RESIZE}</script>
<script>${SCRIPT}</script>`,
  })
}
