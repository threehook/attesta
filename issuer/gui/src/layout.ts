// The look and feel of the Attesta example apps: header with breadcrumb and current action, menu on the left, one-line footer.
export const escape = (s: string) => s.replace(/[&<>"']/g, (c) => `&#${c.charCodeAt(0)};`)

export interface Crumb {
  label: string
  href?: string
}

export interface MenuItem {
  id: string
  label: string
  href: string
  description: string
}

export interface MenuGroup {
  label: string
  items: MenuItem[]
}

const APP_NAME = 'Attesta VC Uitgever'
const APP_MARK = `<svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 2.5 4.5 5.5v5.8c0 4.6 3.1 8.6 7.5 10.2 4.4-1.6 7.5-5.6 7.5-10.2V5.5z"/><path d="m8.5 12.2 2.4 2.4 4.6-5"/></svg>`

/** The first menu item, without a label above it. */
export const HOME: MenuItem = { id: 'home', label: 'Home', href: '/', description: '' }

const theme = `
:root{
  --menu-w:260px;--logo-bg:#efe8f6;
  --bg:#f4f6f9;--surface:#fff;--text:#1b2433;--muted:#5d6b82;--border:#d9dfe8;
  --brand:#14315c;--brand-text:#fff;--brand-muted:#b9c6dc;--accent:#1f5fbf;--accent-text:#fff;--accent-soft:#e6eefb;--focus:#7aa7ec;--label-bg:#c9d7ee;--label-text:#14315c;--danger:#b3261e;--danger-text:#fff;
}
@media (prefers-color-scheme:dark){:root{
  --bg:#12171f;--surface:#1b222d;--text:#e6eaf1;--muted:#98a4b8;--border:#2c3646;
  --brand:#0e2242;--brand-text:#f2f5fa;--brand-muted:#9db0d1;--accent:#5b92e5;--accent-text:#0b1220;--accent-soft:#22304a;--focus:#5b92e5;--label-bg:#2f3f5c;--label-text:#c3d3f0;--danger:#e0605a;--danger-text:#1b0a09;
}}
*{box-sizing:border-box}
[hidden]{display:none!important}
html,body{height:100%}
body{margin:0;background:var(--bg);color:var(--text);font:14px/1.5 system-ui,-apple-system,"Segoe UI",sans-serif;
  display:grid;grid-template-columns:var(--menu-w) 1fr;grid-template-rows:auto 1fr auto;grid-template-areas:"header header" "menu main" "footer footer"}
header{grid-area:header;position:relative;min-height:88px;background:var(--brand);color:var(--brand-text);display:grid;grid-template-columns:var(--menu-w) 1fr}
header .title{padding:14px 32px 18px;align-self:center}
.logo{position:relative;border:1px solid var(--brand-muted);background:var(--logo-bg)}
.logo img{position:absolute;inset:6px;width:calc(100% - 12px);height:calc(100% - 12px);object-fit:contain}
header .app{display:flex;align-items:center;gap:8px;position:absolute;right:32px;font-size:12px;font-weight:700;letter-spacing:.06em;text-transform:uppercase;white-space:nowrap}
.crumbs ol{display:flex;flex-wrap:wrap;gap:6px;list-style:none;margin:0;padding:0;font-size:12px;color:var(--brand-muted)}
.crumbs li+li::before{content:"/";margin-right:6px}
.crumbs a{color:inherit;text-decoration:none}.crumbs a:hover{text-decoration:underline}
.crumbs [aria-current]{color:var(--brand-text)}
header .app .user{text-transform:none;letter-spacing:0;font-size:13px;padding-left:12px;border-left:1px solid var(--brand-muted)}
header .extra{position:absolute;right:32px;bottom:12px}
header h1{margin:6px 0 0;font-size:24px;line-height:1.25;font-weight:600}
nav.menu{grid-area:menu;background:var(--surface);border-right:1px solid var(--border);padding:20px 12px}
.menu h2{margin:0 0 6px;padding:6px 12px;border-radius:6px;background:var(--label-bg);font-size:11px;font-weight:600;letter-spacing:.06em;text-transform:uppercase;color:var(--label-text)}
.menu ul{list-style:none;margin:0 0 20px;padding:0}
.menu a{display:block;padding:8px 12px;border-radius:6px;color:var(--text);text-decoration:none}
.menu a:hover{background:var(--accent-soft)}
.menu a[aria-current]{box-shadow:inset 3px 0 var(--accent);background:var(--accent-soft);color:var(--accent);font-weight:600}
main{grid-area:main;padding:28px 32px;min-width:0}
footer{grid-area:footer;background:var(--surface);border-top:1px solid var(--border);padding:6px 32px;font-size:12px;color:var(--muted);
  white-space:nowrap;overflow:hidden;text-overflow:ellipsis;min-height:30px}
.intro{max-width:88rem;margin:0 0 20px;color:var(--muted)}
.blocks{display:grid;grid-template-columns:repeat(auto-fill,minmax(260px,1fr));gap:16px}
.block{display:block;background:var(--surface);border:1px solid var(--border);border-radius:8px;padding:18px;color:inherit;text-decoration:none}
.block:hover{border-color:var(--accent)}
.block h2{margin:0 0 6px;font-size:16px;font-weight:600}
.block .group{margin:0 0 4px;font-size:11px;font-weight:600;letter-spacing:.06em;text-transform:uppercase;color:var(--muted)}
.block p{margin:0;color:var(--muted)}
form.card{background:var(--surface);border:1px solid var(--border);border-radius:8px;padding:20px;max-width:36rem}
label{display:block;margin:0 0 14px;font-weight:600;font-size:13px}
input,select,textarea{display:block;width:100%;margin-top:4px;padding:8px 10px;font:inherit;font-weight:400;color:var(--text);background:var(--bg);
  border:1px solid var(--border);border-radius:6px}
input[readonly]{color:var(--muted);background:var(--border);border-style:dashed;cursor:not-allowed}
input:focus,select:focus,textarea:focus,button:focus-visible,a:focus-visible{outline:2px solid var(--focus);outline-offset:1px}
button{padding:9px 16px;font:inherit;font-weight:600;color:var(--accent-text);background:var(--accent);border:0;border-radius:6px;cursor:pointer}
button:hover{filter:brightness(1.08)}
code{word-break:break-all}
button.danger{background:var(--danger);color:var(--danger-text)}
.item{background:var(--surface);border:1px solid var(--border);border-radius:8px;padding:16px;max-width:88rem;margin:0 0 16px}
.item h2{margin:0 0 2px;font-size:16px;font-weight:600}
.item .meta{margin:0 0 10px;color:var(--muted)}
.item dl{display:grid;grid-template-columns:max-content 1fr;gap:2px 16px;margin:0 0 14px}
.item dt{color:var(--muted)}.item dd{margin:0}
.actions{display:flex;flex-wrap:wrap;gap:16px;align-items:center}
.actions form{margin:0}
.toolbar{display:flex;gap:16px;align-items:center;margin:0 0 16px}
.toolbar input{margin:0;max-width:20rem}
.tablebox{width:fit-content;max-width:112rem;overflow-x:auto}
table.data th{position:relative}
table.data.sized th,table.data.sized td{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.grip{position:absolute;top:0;right:0;width:8px;height:100%;border-right:1px solid color-mix(in srgb,var(--border) 30%,transparent);cursor:col-resize;user-select:none;touch-action:none}
th:last-child .grip{border-right-color:transparent}
.grip:hover,.grip.active{background:var(--accent-soft)}
table.data{width:100%;border-collapse:collapse;background:var(--surface);border:1px solid var(--border);border-radius:8px}
table.data th,table.data td{padding:8px 12px;text-align:left;border-bottom:1px solid var(--border)}
table.data th{font-size:13px}
table.data td .mono,td.mono{font-family:ui-monospace,monospace;font-size:12px;color:var(--muted)}
table.data td button{padding:5px 12px}
table.data td button:disabled{opacity:.4;cursor:not-allowed}
button.sort{padding:0;background:none;color:inherit;font-weight:600}
th[aria-sort=ascending] button.sort::after{content:" \\25B2";font-size:10px}
th[aria-sort=descending] button.sort::after{content:" \\25BC";font-size:10px}
.pager{display:flex;flex-wrap:wrap;gap:16px;align-items:center;justify-content:space-between;margin:12px 0 0;color:var(--muted)}
.pager label{display:flex;align-items:center;gap:8px;margin:0;font-weight:400}
.pager select{width:auto;margin:0}
.pager .nav{display:flex;gap:8px;align-items:center}
.pager button{padding:5px 12px}
.pager button:disabled{opacity:.4;cursor:not-allowed}
button.secondary{background:var(--surface);color:var(--text);border:1px solid var(--border)}
dialog{border:1px solid var(--border);border-radius:8px;background:var(--surface);color:var(--text);padding:20px;min-width:20rem;max-width:90vw}
dialog::backdrop{background:rgba(0,0,0,.4)}
dialog h2{margin:0 0 12px;font-size:16px}
.error{color:var(--danger);min-height:1.2em;margin:0 0 10px}
.field{margin:0 0 14px}
.editor{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr);gap:24px;align-items:start}
.editor h2{margin:0 0 8px;font-size:16px}
.editor form.card{max-width:none}
.editor .item{max-width:none}
.editor .item .actions{margin-top:4px}
.editor input[type=checkbox]{display:inline-block;width:auto;margin:0 4px 0 0}
.field>span{display:block;font-weight:600;font-size:13px}
details.multi{margin-top:4px;position:relative}
details.multi summary{list-style:none;cursor:pointer;padding:8px 32px 8px 10px;border:1px solid var(--border);border-radius:6px;background:var(--bg);
  white-space:nowrap;overflow:hidden;text-overflow:ellipsis;position:relative}
details.multi summary::-webkit-details-marker{display:none}
details.multi summary::after{content:"";position:absolute;right:12px;top:50%;width:6px;height:6px;margin-top:-5px;border:solid var(--muted);border-width:0 2px 2px 0;transform:rotate(45deg)}
details.multi[open] summary::after{margin-top:-1px;transform:rotate(-135deg)}
details.multi .options{position:absolute;z-index:2;left:0;right:0;margin-top:2px;padding:4px;background:var(--surface);border:1px solid var(--border);border-radius:6px;
  box-shadow:0 4px 12px rgba(0,0,0,.15);max-height:14rem;overflow:auto}
details.multi .options label{display:flex;align-items:center;gap:8px;margin:0;padding:6px 8px;border-radius:4px;font-weight:400;cursor:pointer}
details.multi .options label:hover{background:var(--accent-soft)}
details.multi .options input{width:auto;margin:0}
@media (max-width:720px){
  .editor{grid-template-columns:1fr}
  body{grid-template-columns:1fr;grid-template-areas:"header" "menu" "main" "footer"}
  nav.menu{border-right:0;border-bottom:1px solid var(--border)}
  :root{--menu-w:120px}
  header .title,main,footer{padding-left:16px;padding-right:16px}
  header .app,header .extra{right:16px}
}`

// Lets the user drag the right edge of a column header to widen or narrow that column. The first drag fixes the widths the table has by then.
export const COLUMN_RESIZE = `document.querySelectorAll('table.data').forEach((table) => {
  const heads = [...table.querySelectorAll('thead th')]
  const total = () => heads.reduce((sum, h) => sum + parseFloat(h.style.width), 0)
  for (const th of heads) {
    const grip = document.createElement('span')
    grip.className = 'grip'
    grip.setAttribute('aria-hidden', 'true')
    th.append(grip)
    grip.addEventListener('pointerdown', (e) => {
      e.preventDefault()
      grip.setPointerCapture(e.pointerId)
      grip.classList.add('active')
      if (!table.classList.contains('sized')) {
        for (const h of heads) h.style.width = h.offsetWidth + 'px'
        table.classList.add('sized')
        table.style.tableLayout = 'fixed'
        table.style.width = total() + 'px'
      }
      const start = e.clientX, base = parseFloat(th.style.width)
      const move = (ev) => {
        th.style.width = Math.max(48, base + ev.clientX - start) + 'px'
        table.style.width = total() + 'px'
      }
      const stop = () => {
        grip.classList.remove('active')
        grip.removeEventListener('pointermove', move)
        grip.removeEventListener('pointerup', stop)
      }
      grip.addEventListener('pointermove', move)
      grip.addEventListener('pointerup', stop)
    })
  }
})`

export interface Page {
  /** The menu below the home item. */
  menu: MenuGroup[]
  /** The current action: the header title and the last breadcrumb. */
  title: string
  /** The path to the current action, without the last crumb. */
  crumbs: Crumb[]
  /** The menu item to highlight. */
  active?: string
  body: string
  /** The logo in the header; the block stays empty without one. */
  logo?: { src: string; alt: string }
  /** Important information for the one-line footer, if there is any. */
  footer?: string
  /** Something for the bottom right of the header. */
  headerExtra?: string
  /** Adds a sign-out link below the menu. */
  signOut?: boolean
  /** Who is signed in, shown at the top right of the header. */
  user?: string
}

export function renderPage(page: Page): string {
  const crumbs = [...page.crumbs, { label: page.title }]
    .map((c, i, all) =>
      i === all.length - 1
        ? `<li aria-current="page">${escape(c.label)}</li>`
        : `<li>${c.href ? `<a href="${escape(c.href)}">${escape(c.label)}</a>` : escape(c.label)}</li>`,
    )
    .join('')
  const list = (items: MenuItem[]) =>
    `<ul>${items.map((i) => `<li><a href="${escape(i.href)}"${i.id === page.active ? ' aria-current="page"' : ''}>${escape(i.label)}</a></li>`).join('')}</ul>`
  const menu = list([HOME]) + page.menu.map((group) => `<h2>${escape(group.label)}</h2>${list(group.items)}`).join('')
  const signOut = page.signOut ? `<ul><li><a href="/logout">Afmelden</a></li></ul>` : ''
  const footer = page.footer ? `<footer title="${escape(page.footer)}">${escape(page.footer)}</footer>` : '<footer>&nbsp;</footer>'
  return `<!doctype html><html lang="nl"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>${APP_NAME}</title><style>${theme}</style>
<header><div class="logo">${page.logo ? `<img src="${escape(page.logo.src)}" alt="${escape(page.logo.alt)}">` : ''}</div><div class="title"><div class="app">${APP_MARK}${APP_NAME}${page.user ? `<span class="user" title="Aangemeld als ${escape(page.user)}">${escape(page.user)}</span>` : ''}</div><nav class="crumbs" aria-label="Kruimelpad"><ol>${crumbs}</ol></nav><h1>${escape(page.title)}</h1></div>${page.headerExtra ? `<div class="extra">${page.headerExtra}</div>` : ''}</header>
<nav class="menu" aria-label="Menu">${menu}${signOut}</nav>
<main>${page.body}</main>
${footer}`
}
