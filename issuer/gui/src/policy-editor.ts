import { escape } from './layout.js'
import { POLICY_EDITOR, home, shell, type Chrome } from './page.js'

const CSS = `
.pe-bar{display:flex;flex-wrap:wrap;gap:12px;align-items:center;margin:0 0 12px}
.pe-bar .file{margin-left:auto;color:var(--muted)}
.pe-theme{display:inline-flex;border:1px solid var(--border);border-radius:6px;overflow:hidden}
.pe-theme button{border-radius:0;padding:9px 14px;color:var(--text);background:var(--surface);font-weight:400}
.pe-theme button[aria-pressed=true]{color:var(--accent-text);background:var(--accent);font-weight:600}
#pe-editor{height:50vh;min-height:280px;border:1px solid var(--border);border-radius:6px;overflow:hidden}
#pe-status{margin:12px 0 6px;font-weight:600}
#pe-status.ok{color:#1a7f37}#pe-status.bad{color:var(--danger)}
#pe-problems{list-style:none;margin:0 0 16px;padding:0;max-width:88rem}
#pe-problems li button{display:block;width:100%;text-align:left;padding:6px 10px;margin:0 0 2px;color:var(--text);background:var(--surface);
  border:1px solid var(--border);border-left:4px solid var(--danger);border-radius:4px;font-weight:400}
#pe-problems li.warning button{border-left-color:#b7791f}
#pe-problems .at{font-family:ui-monospace,monospace;color:var(--muted);margin-right:8px}
.pe-line-error{background:rgba(179,38,30,.14)}.pe-line-warning{background:rgba(183,121,31,.16)}
.pe-glyph-error,.pe-glyph-warning{margin-left:4px;width:8px!important;height:8px!important;margin-top:6px;border-radius:50%}
.pe-glyph-error{background:var(--danger)}.pe-glyph-warning{background:#b7791f}
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
    <dt>input.type</dt><dd>het soort credential, bijvoorbeeld Employee</dd>
    <dt>input.issuer</dt><dd>de DID van de uitgever van het credential</dd>
    <dt>input.claims.&lt;naam&gt;</dt><dd>een onthuld gegeven, altijd tekst</dd>
    <dt>input.userRoles</dt><dd>de lijst rollen die de applicatie meestuurt</dd>
    <dt>input.others.&lt;type&gt;</dt><dd>een ander credential van dezelfde persoon, met <code>issuer</code> en <code>claims</code></dd>
  </dl>
  <p>Een voorbeeld van <code>input</code>:</p>
<pre>{
  "resource": "laadpaal-aanvraag",
  "type": "Employee",
  "issuer": "did:key:z6Mk…",
  "claims": { "email": "jerry@example.com", "department": "secretariaat", "roles": "laadpalen-aanvrager" },
  "userRoles": [],
  "others": { "Diploma Laadpalen Management": { "issuer": "did:key:z6Mk…", "claims": { "validUntil": "2099-12-31" } } }
}</pre>
</details>`

// The code of the page: Monaco with Rego highlighting, the check while typing, and opening and saving files on the user's own disk.
const SCRIPT = String.raw`(() => {
  const TEMPLATE = 'package policy\n\ndefault decision := {"decision": "Deny", "reason": "Niet toegestaan"}\n\ndecision := {"decision": "Permit", "reason": "Toegestaan"} if {\n\tinput.type == "Employee"\n}\n'
  const TYPES = [{ description: 'Rego-policy', accept: { 'text/plain': ['.rego'] } }]
  const $ = (id) => document.getElementById(id)
  const canFiles = typeof window.showOpenFilePicker === 'function' && typeof window.showSaveFilePicker === 'function'

  self.MonacoEnvironment = {
    getWorkerUrl: () => 'data:text/javascript;charset=utf-8,' + encodeURIComponent(
      "self.MonacoEnvironment={baseUrl:'" + location.origin + "/vendor/monaco/'};importScripts('" + location.origin + "/vendor/monaco/vs/base/worker/workerMain.js');"),
  }
  require.config({ paths: { vs: '/vendor/monaco/vs' } })
  require(['vs/editor/editor.main'], () => {
    // The background is the user's choice, kept in this browser; until then it follows the system.
    const system = matchMedia('(prefers-color-scheme: dark)')
    let chosen = null
    try { chosen = localStorage.getItem('policy-editor-theme') } catch {}
    const isDark = () => (chosen ? chosen === 'dark' : system.matches)
    monaco.languages.register({ id: 'rego' })
    monaco.languages.setLanguageConfiguration('rego', {
      comments: { lineComment: '#' },
      brackets: [['{', '}'], ['[', ']'], ['(', ')']],
      autoClosingPairs: [{ open: '{', close: '}' }, { open: '[', close: ']' }, { open: '(', close: ')' }, { open: '"', close: '"' }],
    })
    monaco.languages.setMonarchTokensProvider('rego', {
      keywords: ['package', 'import', 'as', 'default', 'else', 'not', 'some', 'every', 'in', 'with', 'if', 'contains', 'true', 'false', 'null'],
      tokenizer: {
        root: [
          [/#.*$/, 'comment'],
          [/[a-zA-Z_]\w*/, { cases: { '@keywords': 'keyword', '@default': 'identifier' } }],
          [/"([^"\\]|\\.)*"/, 'string'],
          [/\x60[^\x60]*\x60/, 'string'],
          [/\d+(\.\d+)?/, 'number'],
          [/[{}()[\]]/, '@brackets'],
        ],
      },
    })

    const model = monaco.editor.createModel(TEMPLATE, 'rego')
    const editor = monaco.editor.create($('pe-editor'), {
      model, automaticLayout: true, glyphMargin: true, minimap: { enabled: false }, scrollBeyondLastLine: false, fontSize: 14,
      theme: isDark() ? 'vs-dark' : 'vs', tabSize: 2, insertSpaces: false,
    })
    const paint = () => {
      monaco.editor.setTheme(isDark() ? 'vs-dark' : 'vs')
      $('pe-dark').setAttribute('aria-pressed', String(isDark()))
      $('pe-light').setAttribute('aria-pressed', String(!isDark()))
    }
    const choose = (theme) => { chosen = theme; try { localStorage.setItem('policy-editor-theme', theme) } catch {} paint() }
    $('pe-dark').addEventListener('click', () => choose('dark'))
    $('pe-light').addEventListener('click', () => choose('light'))
    system.addEventListener('change', paint)
    paint()

    let handle = null, name = null, saved = TEMPLATE
    const dirty = () => model.getValue() !== saved
    const showFile = () => { $('pe-file').textContent = (name ?? 'Naamloos') + (dirty() ? ' • niet opgeslagen' : '') }
    const load = (text, h, n) => { handle = h; name = n; saved = text; model.setValue(text); showFile(); check() }

    // The check: every change asks the backend, and only the newest answer is shown.
    let decorations = editor.createDecorationsCollection([])
    let timer, seq = 0
    const status = (text, cls) => { const s = $('pe-status'); s.textContent = text; s.className = cls || '' }
    const show = (list) => {
      const severity = (d) => (d.severity === 'warning' ? monaco.MarkerSeverity.Warning : monaco.MarkerSeverity.Error)
      monaco.editor.setModelMarkers(model, 'rego', list.map((d) => ({
        severity: severity(d), message: d.message,
        startLineNumber: d.row, startColumn: d.col, endLineNumber: d.endRow, endColumn: d.endCol,
      })))
      decorations.set(list.map((d) => ({
        range: new monaco.Range(d.row, 1, d.row, 1),
        options: { isWholeLine: true, className: 'pe-line-' + d.severity, glyphMarginClassName: 'pe-glyph-' + d.severity },
      })))
      const ul = $('pe-problems')
      ul.replaceChildren(...list.map((d) => {
        const li = document.createElement('li'), b = document.createElement('button'), at = document.createElement('span')
        li.className = d.severity
        b.type = 'button'
        at.className = 'at'
        at.textContent = 'Regel ' + d.row + ', positie ' + d.col
        b.append(at, d.message)
        b.addEventListener('click', () => {
          editor.setPosition({ lineNumber: d.row, column: d.col })
          editor.revealLineInCenter(d.row)
          editor.focus()
        })
        li.append(b)
        return li
      }))
      const errors = list.filter((d) => d.severity === 'error').length, warnings = list.length - errors
      if (!list.length) status('Geen problemen gevonden', 'ok')
      else status([errors && errors + (errors === 1 ? ' fout' : ' fouten'), warnings && warnings + (warnings === 1 ? ' waarschuwing' : ' waarschuwingen')].filter(Boolean).join(', '), 'bad')
    }
    async function check() {
      const mine = ++seq
      status('Controleren…')
      let list
      try {
        const res = await fetch('/policy-editor/diagnose', { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ source: model.getValue() }) })
        if (!res.ok) throw new Error(String(res.status))
        list = (await res.json()).diagnostics
      } catch {
        if (mine === seq) { show([]); status('De controle is niet beschikbaar', 'bad') }
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
        const h = await showSaveFilePicker({ suggestedName: name ?? 'policy.rego', types: TYPES })
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
        const [h] = await showOpenFilePicker({ types: TYPES })
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
    body: `<style>${CSS}</style>
<p class="intro">${escape(POLICY_EDITOR.description)}</p>
<div class="pe-bar">
  <button type="button" id="pe-new">Nieuw</button>
  <button type="button" id="pe-open" class="secondary">Openen</button>
  <button type="button" id="pe-save">Opslaan</button>
  <button type="button" id="pe-save-as" class="secondary">Opslaan als</button>
  <span class="pe-theme" role="group" aria-label="Achtergrond van de editor">
    <button type="button" id="pe-dark" aria-pressed="false">Donker</button>
    <button type="button" id="pe-light" aria-pressed="false">Licht</button>
  </span>
  <span class="file" id="pe-file"></span>
</div>
<p class="error" id="pe-message" role="alert"></p>
<div id="pe-editor"></div>
<p id="pe-status" role="status"></p>
<ul id="pe-problems"></ul>
${HELP}
<script src="/vendor/monaco/vs/loader.js"></script>
<script>${SCRIPT}</script>`,
  })
}
