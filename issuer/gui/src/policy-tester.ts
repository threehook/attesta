import { escape } from './layout.js'
import { MONACO_CSS, MONACO_SCRIPTS, PROBLEMS, THEME_SWITCH } from './monaco.js'
import { POLICY_TESTER, home, shell, type Chrome } from './page.js'

const CSS = `
.pt-panes{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr);gap:16px;align-items:start}
.pt-panes h2{margin:0 0 6px;font-size:14px}
.pt-panes .pe-box{height:42vh}
.pt-bar{display:flex;flex-wrap:wrap;gap:8px;align-items:center;margin:0 0 6px}
.pt-bar .file{color:var(--muted)}
#pt-result{max-width:88rem;margin:16px 0 0}
#pt-result .decision{display:inline-block;padding:4px 14px;border-radius:6px;font-weight:700;color:#fff;background:#5d6b82}
#pt-result .decision.Permit{background:#1a7f37}#pt-result .decision.Deny{background:var(--danger)}#pt-result .decision.Indeterminate{background:#b7791f}
#pt-result .reason{margin:8px 0}
#pt-result table{border-collapse:collapse;margin:6px 0 12px}
#pt-result th,#pt-result td{padding:4px 12px;text-align:left;border-bottom:1px solid var(--border)}
#pt-result h3{margin:12px 0 0;font-size:13px}
@media (max-width:720px){.pt-panes{grid-template-columns:1fr}}
`

// The code of the page: a policy from the user's disk, an input to edit, and the decision the backend gives.
const SCRIPT = String.raw`(() => {
  const SAMPLE = JSON.stringify({
    resource: 'laadpaal-aanvraag',
    userRoles: [],
    credentials: { Employee: { issuer: 'did:key:z6Mk…', claims: { email: 'jerry@example.com', department: 'secretariaat' } } },
  }, null, 2) + '\n'
  const HINT = '# Open een policy met de knop Policy openen.\n'
  const INPUT_FILE = [{ description: 'Input (JSON)', accept: { 'application/json': ['.json'] } }]
  const el = (tag, text, cls) => { const e = document.createElement(tag); if (text !== undefined) e.textContent = text; if (cls) e.className = cls; return e }

  bootMonaco(() => {
    const options = { automaticLayout: true, minimap: { enabled: false }, scrollBeyondLastLine: false, fontSize: 14, theme: themeName(), tabSize: 2 }
    const policyModel = monaco.editor.createModel(HINT, 'rego')
    const policy = monaco.editor.create($('pt-policy'), { ...options, model: policyModel, glyphMargin: true, readOnly: true })
    const inputModel = monaco.editor.createModel(SAMPLE, 'json')
    monaco.editor.create($('pt-input'), { ...options, model: inputModel })
    themeSwitch()
    const decorations = policy.createDecorationsCollection([])
    const message = (text) => { $('pt-message').textContent = text }
    const fail = (e) => { if (e.name !== 'AbortError') message('Dit lukte niet: ' + e.message) }

    let hasPolicy = false
    $('pt-open-policy').addEventListener('click', async () => {
      try {
        const [h] = await showOpenFilePicker({ types: REGO_FILE })
        policyModel.setValue(await (await h.getFile()).text())
        $('pt-policy-name').textContent = h.name
        hasPolicy = true
        $('pt-run').disabled = false
        showProblems(policy, policyModel, decorations, [])
        setStatus('')
        $('pt-result').replaceChildren()
        message('')
      } catch (e) { fail(e) }
    })

    let inputHandle = null
    $('pt-open-input').addEventListener('click', async () => {
      try {
        const [h] = await showOpenFilePicker({ types: INPUT_FILE })
        inputModel.setValue(await (await h.getFile()).text())
        inputHandle = h
        $('pt-input-name').textContent = h.name
        message('')
      } catch (e) { fail(e) }
    })
    $('pt-save-input').addEventListener('click', async () => {
      try {
        const h = await showSaveFilePicker({ suggestedName: 'input.json', types: INPUT_FILE })
        const w = await h.createWritable()
        await w.write(inputModel.getValue())
        await w.close()
        inputHandle = h
        $('pt-input-name').textContent = h.name
        message('')
      } catch (e) { fail(e) }
    })

    const statements = (title, list) => {
      const box = el('div')
      if (!list.length) return box
      const table = el('table'), head = el('tr')
      head.append(el('th', 'Id'), el('th', 'Attributen'))
      table.append(head)
      for (const s of list) {
        const row = el('tr')
        row.append(el('td', s.id), el('td', Object.entries(s.attributes).map(([k, v]) => k + ' = ' + v).join(', ')))
        table.append(row)
      }
      box.append(el('h3', title), table)
      return box
    }
    const showResult = (r) => {
      $('pt-result').replaceChildren(el('span', r.decision, 'decision ' + r.decision), el('p', r.reason || 'Geen reden opgegeven', 'reason'),
        statements('Verplichtingen', r.obligations), statements('Advies', r.advice))
    }

    $('pt-run').addEventListener('click', async () => {
      message('')
      $('pt-result').replaceChildren()
      let input
      try { input = JSON.parse(inputModel.getValue()) } catch (e) { message('De input is geen geldige JSON: ' + e.message); return }
      let res
      try {
        res = await fetch('/policy-tester/evaluate', { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ source: policyModel.getValue(), input }) })
      } catch { message('De test is niet beschikbaar'); return }
      const body = await res.json().catch(() => ({}))
      if (!res.ok) { message(body.error ?? 'De test is niet beschikbaar'); return }
      if (body.diagnostics) {
        showProblems(policy, policyModel, decorations, body.diagnostics)
        message('Los eerst de problemen van de policy op.')
        return
      }
      showProblems(policy, policyModel, decorations, [])
      setStatus('')
      showResult(body.result)
    })

    if (!canFiles) {
      for (const id of ['pt-open-policy', 'pt-open-input', 'pt-save-input']) $(id).disabled = true
      message('Openen en opslaan werkt alleen in Chrome of Edge.')
    }
  })
})()`

/** The tester for Rego policies: a policy opened from the user's disk, an input to edit, and the decision the policy gives for it. */
export function renderPolicyTester(chrome: Chrome): string {
  return shell(chrome, {
    title: POLICY_TESTER.label,
    crumbs: home,
    active: POLICY_TESTER.id,
    headerExtra: THEME_SWITCH,
    body: `<style>${MONACO_CSS}${CSS}</style>
<p class="intro">${escape(POLICY_TESTER.description)}</p>
<p class="error" id="pt-message" role="alert"></p>
<div class="pt-panes">
  <div>
    <h2>Policy</h2>
    <div class="pt-bar"><button type="button" id="pt-open-policy" class="secondary">Policy openen</button><span class="file" id="pt-policy-name"></span></div>
    <div id="pt-policy" class="pe-box"></div>
  </div>
  <div>
    <h2>Input</h2>
    <div class="pt-bar">
      <button type="button" id="pt-open-input" class="secondary">Input openen</button>
      <button type="button" id="pt-save-input" class="secondary">Input opslaan</button>
      <span class="file" id="pt-input-name"></span>
    </div>
    <div id="pt-input" class="pe-box"></div>
  </div>
</div>
<div class="pe-bar" style="margin-top:12px"><button type="button" id="pt-run" disabled>Test uitvoeren</button></div>
<div id="pt-result" role="status"></div>
${PROBLEMS}
${MONACO_SCRIPTS}
<script>${SCRIPT}</script>`,
  })
}
