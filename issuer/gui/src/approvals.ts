import { COLUMN_RESIZE, escape } from './layout.js'
import { APPROVALS, home, shell, type Chrome } from './page.js'

// The list comes from /approvals/data. Every text goes in with textContent. An answer of the API is shown as it is.
const SCRIPT = `(() => {
  const $ = (id) => document.getElementById(id)
  const cell = (tr, text) => { const td = document.createElement('td'); td.textContent = text; tr.append(td); return td }
  const when = (iso) => new Intl.DateTimeFormat('nl-NL', { timeZone: 'Europe/Amsterdam', dateStyle: 'short', timeStyle: 'short' }).format(new Date(iso))
  const say = async (res, fallback) => { $('approval-error').textContent = res.ok ? '' : ((await res.json().catch(() => ({}))).error || fallback) }
  const button = (tr, label, className, onClick) => {
    const b = document.createElement('button'); b.type = 'button'; b.textContent = label; if (className) b.className = className
    b.addEventListener('click', onClick); const td = document.createElement('td'); td.append(b); tr.append(td)
    return b
  }

  const span = (seconds) => {
    const minutes = Math.round(seconds / 60)
    if (minutes < 60) return minutes + (minutes === 1 ? ' minuut' : ' minuten')
    const hours = Math.round(minutes / 60)
    return hours + ' uur'
  }

  const load = async () => {
    const res = await fetch('/approvals/data')
    if (!res.ok) return
    const body = $('approvals-body')
    body.replaceChildren()
    const list = await res.json()
    for (const r of list) {
      const tr = document.createElement('tr')
      cell(tr, r.workload).className = 'mono'; cell(tr, r.resource).className = 'mono'
      cell(tr, Object.entries(r.action).map(([k, v]) => k + ': ' + v).join(', '))
      cell(tr, when(r.createdAt)); cell(tr, when(r.expiresAt)); cell(tr, span(r.validSeconds))
      cell(tr, r.approvers.length + ' van ' + r.required + (r.approvers.length ? ' (' + r.approvers.join(', ') + ')' : ''))
      const approve = button(tr, r.approvedByMe ? 'Goedgekeurd door u' : 'Goedkeuren', '', async () => { await say(await fetch('/approvals/' + encodeURIComponent(r.id) + '/approvals', { method: 'POST' }), 'Goedkeuren is niet gelukt.'); load() })
      approve.disabled = r.approvedByMe
      button(tr, 'Afwijzen', 'danger', async () => { await say(await fetch('/approvals/' + encodeURIComponent(r.id), { method: 'DELETE' }), 'Afwijzen is niet gelukt.'); load() })
      body.append(tr)
    }
    if (!list.length) { const tr = document.createElement('tr'); cell(tr, 'Niets wacht op goedkeuring. Een aanvraag verschijnt hier zodra een systeem een handeling wil uitvoeren waarvoor geen mandaat is.').colSpan = 9; body.append(tr) }
  }
  load()
})()`

/** The actions of systems that wait for approval. */
export function renderApprovals(chrome: Chrome): string {
  return shell(chrome, {
    title: APPROVALS.label,
    crumbs: home,
    active: APPROVALS.id,
    body: `<p class="intro">${escape(APPROVALS.description)}</p>
<p class="error" id="approval-error" role="alert"></p>
<div class="tablebox">
<table class="data">
  <thead><tr><th>Systeem</th><th>Bron</th><th>Handeling</th><th>Aangevraagd</th><th>Aanvraag geldig tot</th><th>Geldig na goedkeuring</th><th>Goedgekeurd door</th><th>Goedkeuren</th><th>Afwijzen</th></tr></thead>
  <tbody id="approvals-body"></tbody>
</table>
</div>
<script>${COLUMN_RESIZE}</script>
<script>${SCRIPT}</script>`,
  })
}
