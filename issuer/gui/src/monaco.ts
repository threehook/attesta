// What the Policy Editor and the Policy Tester share: Monaco with Rego highlighting, the background switch and the list of a policy's problems.
export const MONACO_CSS = `
.pe-bar{display:flex;flex-wrap:wrap;gap:12px;align-items:center;margin:0 0 12px}
.pe-bar .file{margin-left:auto;color:var(--muted)}
.pe-theme{display:inline-flex;gap:2px;padding:2px;border:1px solid var(--brand-muted);border-radius:6px}
.pe-theme button{display:flex;align-items:center;padding:5px 8px;border-radius:4px;color:var(--brand-muted);background:transparent;font-weight:400}
.pe-theme button:hover{color:var(--brand-text);filter:none}
.pe-theme button[aria-pressed=true]{color:#fff;background:var(--accent)}
#pe-editor,.pe-box{height:50vh;min-height:280px;border:1px solid var(--border);border-radius:6px;overflow:hidden}
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
`

/** The switch for the background of the editors; it goes to the bottom right of the header. */
export const THEME_SWITCH = `<span class="pe-theme" role="group" aria-label="Achtergrond van de editor">
  <button type="button" id="pe-dark" aria-pressed="false" title="Donkere achtergrond" aria-label="Donkere achtergrond"><svg width="14" height="14" viewBox="0 0 16 16" fill="none" aria-hidden="true"><path d="M13.5 9.7A5.8 5.8 0 0 1 6.3 2.5a5.8 5.8 0 1 0 7.2 7.2Z" stroke="currentColor" stroke-width="1.2" stroke-linejoin="round"/></svg></button>
  <button type="button" id="pe-light" aria-pressed="false" title="Lichte achtergrond" aria-label="Lichte achtergrond"><svg width="14" height="14" viewBox="0 0 16 16" fill="none" aria-hidden="true"><circle cx="8" cy="8" r="3.2" stroke="currentColor" stroke-width="1.2"/><path d="M8 1.2v1.6M8 13.2v1.6M14.8 8h-1.6M2.8 8H1.2M12.7 3.3l-1.1 1.1M4.4 11.6l-1.1 1.1M12.7 12.7l-1.1-1.1M4.4 4.4 3.3 3.3" stroke="currentColor" stroke-width="1.2" stroke-linecap="round"/></svg></button>
</span>`

/** The place where the problems of a policy are listed, under the editors. */
export const PROBLEMS = `<p id="pe-status" role="status"></p>
<ul id="pe-problems"></ul>`

const SHARED_SCRIPT = String.raw`const $ = (id) => document.getElementById(id)
const canFiles = typeof window.showOpenFilePicker === 'function' && typeof window.showSaveFilePicker === 'function'

// The background is the user's choice, kept in this browser; until then it follows the system.
const system = matchMedia('(prefers-color-scheme: dark)')
let chosenTheme = null
try { chosenTheme = localStorage.getItem('policy-editor-theme') } catch {}
const isDark = () => (chosenTheme ? chosenTheme === 'dark' : system.matches)
const themeName = () => (isDark() ? 'vs-dark' : 'vs')
function themeSwitch() {
  const paint = () => {
    monaco.editor.setTheme(themeName())
    $('pe-dark').setAttribute('aria-pressed', String(isDark()))
    $('pe-light').setAttribute('aria-pressed', String(!isDark()))
  }
  const choose = (theme) => { chosenTheme = theme; try { localStorage.setItem('policy-editor-theme', theme) } catch {} paint() }
  $('pe-dark').addEventListener('click', () => choose('dark'))
  $('pe-light').addEventListener('click', () => choose('light'))
  system.addEventListener('change', paint)
  paint()
}

// Loads Monaco from this server and teaches it Rego, then calls ready.
function bootMonaco(ready) {
  self.MonacoEnvironment = {
    getWorkerUrl: () => 'data:text/javascript;charset=utf-8,' + encodeURIComponent(
      "self.MonacoEnvironment={baseUrl:'" + location.origin + "/vendor/monaco/'};importScripts('" + location.origin + "/vendor/monaco/vs/base/worker/workerMain.js');"),
  }
  require.config({ paths: { vs: '/vendor/monaco/vs' } })
  require(['vs/editor/editor.main'], () => {
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
    ready()
  })
}

const REGO_FILE = [{ description: 'Rego-policy', accept: { 'text/plain': ['.rego'] } }]

// Marks every problem in the editor (squiggle, line and dot in the margin) and lists them below it; a click on one goes to its place.
function showProblems(editor, model, decorations, list) {
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
  if (!list.length) setStatus('Geen problemen gevonden', 'ok')
  else setStatus([errors && errors + (errors === 1 ? ' fout' : ' fouten'), warnings && warnings + (warnings === 1 ? ' waarschuwing' : ' waarschuwingen')].filter(Boolean).join(', '), 'bad')
}

function setStatus(text, cls) { const s = $('pe-status'); s.textContent = text; s.className = cls || '' }`

/** The scripts that load Monaco and define the shared functions; every page that shows an editor puts them before its own script. */
export const MONACO_SCRIPTS = `<script src="/vendor/monaco/vs/loader.js"></script>
<script>${SHARED_SCRIPT}</script>`
