import { escape } from './layout.js'
import { MONACO_CSS, MONACO_SCRIPTS, PROBLEMS, THEME_SWITCH } from './monaco.js'
import { POLICY_EDITOR, home, shell, type Chrome } from './page.js'

const CSS = `
details.pe-help{max-width:88rem;background:var(--surface);border:1px solid var(--border);border-radius:8px;padding:12px 16px}
details.pe-help summary{cursor:pointer;font-weight:600}
details.pe-help pre{margin:8px 0;padding:10px;background:var(--bg);border:1px solid var(--border);border-radius:6px;overflow:auto}
details.pe-help dt{font-family:ui-monospace,monospace}
details.pe-help dd{margin:0 0 6px 16px;color:var(--muted)}
`

const HELP = `<details class="pe-help">
  <summary>Hulp bij het schrijven van een policy</summary>
  <p>Een policy is een Rego-bestand in <code>package policy</code> met een regel <code>decision</code>. Die geeft het besluit en de reden terug:
  <code>Permit</code>, <code>Deny</code>, <code>NotApplicable</code> of <code>Indeterminate</code>.</p>
  <p>De gegevens waarover de policy beslist staan in <code>input</code>:</p>
  <dl>
    <dt>input.resource</dt><dd>waar de applicatie toegang toe vraagt</dd>
    <dt>input.credentials[&quot;Employee&quot;]</dt><dd>een aangeboden credential, op naam van het soort credential</dd>
    <dt>input.credentials[&quot;Employee&quot;].issuer</dt><dd>de DID van de uitgever van dat credential</dd>
    <dt>input.credentials[&quot;Employee&quot;].claims.&lt;naam&gt;</dt><dd>een onthuld gegeven, altijd tekst</dd>
    <dt>input.userRoles</dt><dd>de lijst rollen die de applicatie meestuurt</dd>
  </dl>
  <p>Een voorbeeld van <code>input</code>:</p>
<pre>{
  "resource": "laadpaal-aanvraag",
  "userRoles": [],
  "credentials": {
    "Employee": { "issuer": "did:key:z6Mk…", "claims": { "email": "jerry@example.com", "department": "secretariaat", "roles": "laadpalen-aanvrager" } },
    "Diploma Laadpalen Management": { "issuer": "did:key:z6Mk…", "claims": { "validUntil": "2099-12-31" } }
  }
}</pre>
</details>`

// The code of the page: the check while typing, and opening and saving files on the user's own disk.
const SCRIPT = String.raw`(() => {
  const TEMPLATE = 'package policy\n\ndefault decision := {"decision": "Deny", "reason": "Niet toegestaan"}\n\ndecision := {"decision": "Permit", "reason": "Toegestaan"} if {\n\tinput.credentials.Employee.claims.department == "secretariaat"\n}\n'

  bootMonaco(() => {
    const model = monaco.editor.createModel(TEMPLATE, 'rego')
    const editor = monaco.editor.create($('pe-editor'), {
      model, automaticLayout: true, glyphMargin: true, minimap: { enabled: false }, scrollBeyondLastLine: false, fontSize: 14,
      theme: themeName(), tabSize: 2, insertSpaces: false,
    })
    themeSwitch()

    let handle = null, name = null, saved = TEMPLATE
    const dirty = () => model.getValue() !== saved
    const showFile = () => { $('pe-file').textContent = (name ?? 'Naamloos') + (dirty() ? ' \u2022 niet opgeslagen' : '') }
    const load = (text, h, n) => { handle = h; name = n; saved = text; model.setValue(text); showFile(); check() }

    // The check: every change asks the backend, and only the newest answer is shown.
    const decorations = editor.createDecorationsCollection([])
    const show = (list) => showProblems(editor, model, decorations, list)
    let timer, seq = 0
    async function check() {
      const mine = ++seq
      setStatus('Controleren\u2026')
      let list
      try {
        const res = await fetch('/policy-editor/diagnose', { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ source: model.getValue() }) })
        if (!res.ok) throw new Error(String(res.status))
        list = (await res.json()).diagnostics
      } catch {
        if (mine === seq) { show([]); setStatus('De controle is niet beschikbaar', 'bad') }
        return
      }
      if (mine === seq) show(list)
    }
    model.onDidChangeContent(() => { showFile(); clearTimeout(timer); timer = setTimeout(check, 400) })

    // Files: the browser's own pickers, so the user chooses any place on the disk.
    const write = async (h) => { const w = await h.createWritable(); await w.write(model.getValue()); await w.close() }
    const fail = (e) => { if (e.name !== 'AbortError') $('pe-message').textContent = 'Dit lukte niet: ' + e.message }
    const leave = () => !dirty() || confirm('De wijzigingen zijn niet opgeslagen. Doorgaan zonder opslaan?')
    async function saveAs() {
      try {
        const h = await showSaveFilePicker({ suggestedName: name ?? 'policy.rego', types: REGO_FILE })
        await write(h)
        handle = h; name = h.name; saved = model.getValue(); showFile(); $('pe-message').textContent = ''
      } catch (e) { fail(e) }
    }
    async function save() {
      if (!handle) return saveAs()
      try { await write(handle); saved = model.getValue(); showFile(); $('pe-message').textContent = '' } catch (e) { fail(e) }
    }
    $('pe-new').addEventListener('click', () => { if (leave()) { $('pe-message').textContent = ''; load(TEMPLATE, null, null) } })
    $('pe-open').addEventListener('click', async () => {
      if (!leave()) return
      try {
        const [h] = await showOpenFilePicker({ types: REGO_FILE })
        load(await (await h.getFile()).text(), h, h.name)
        $('pe-message').textContent = ''
      } catch (e) { fail(e) }
    })
    $('pe-save').addEventListener('click', save)
    $('pe-save-as').addEventListener('click', saveAs)
    editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, save)
    addEventListener('beforeunload', (e) => { if (dirty()) e.preventDefault() })

    if (!canFiles) {
      for (const id of ['pe-new', 'pe-open', 'pe-save', 'pe-save-as']) $(id).disabled = true
      $('pe-message').textContent = 'Openen en opslaan werkt alleen in Chrome of Edge.'
    }
    showFile()
    check()
  })
})()`

/** The editor for Rego policies: a code editor with the problems of the policy marked in it and listed below, and files on the user's own disk. */
export function renderPolicyEditor(chrome: Chrome): string {
  return shell(chrome, {
    title: POLICY_EDITOR.label,
    crumbs: home,
    active: POLICY_EDITOR.id,
    headerExtra: THEME_SWITCH,
    body: `<style>${MONACO_CSS}${CSS}</style>
<p class="intro">${escape(POLICY_EDITOR.description)}</p>
<div class="pe-bar">
  <button type="button" id="pe-new">Nieuw</button>
  <button type="button" id="pe-open" class="secondary">Openen</button>
  <button type="button" id="pe-save">Opslaan</button>
  <button type="button" id="pe-save-as" class="secondary">Opslaan als</button>
  <span class="file" id="pe-file"></span>
</div>
<p class="error" id="pe-message" role="alert"></p>
<div id="pe-editor"></div>
${PROBLEMS}
${HELP}
${MONACO_SCRIPTS}
<script>${SCRIPT}</script>`,
  })
}
