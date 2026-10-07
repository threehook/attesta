// The look and feel of the attesta example apps: header with breadcrumb and current action, menu on the left, one-line footer.
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

/** The first menu item, without a label above it. */
export const HOME: MenuItem = { id: 'home', label: 'Home', href: '/', description: '' }

export const MENU: MenuGroup[] = [
  {
    label: 'Verifiable Credentials',
    items: [
      {
        id: 'laadpalen',
        label: 'Aanvragen laadpalen',
        href: '/employee',
        description:
          'Geef een medewerker een verifiable credential uit met afdeling, organisatie en diploma. Met deze verifiable credential mag de medewerker laadpalen aanvragen.',
      },
    ],
  },
]

const theme = `
:root{
  --menu-w:260px;--logo-bg:#efe8f6;
  --bg:#f4f6f9;--surface:#fff;--text:#1b2433;--muted:#5d6b82;--border:#d9dfe8;
  --brand:#14315c;--brand-text:#fff;--brand-muted:#b9c6dc;--accent:#1f5fbf;--accent-text:#fff;--accent-soft:#e6eefb;--focus:#7aa7ec;--label-bg:#c9d7ee;--label-text:#14315c;
}
@media (prefers-color-scheme:dark){:root{
  --bg:#12171f;--surface:#1b222d;--text:#e6eaf1;--muted:#98a4b8;--border:#2c3646;
  --brand:#0e2242;--brand-text:#f2f5fa;--brand-muted:#9db0d1;--accent:#5b92e5;--accent-text:#0b1220;--accent-soft:#22304a;--focus:#5b92e5;--label-bg:#2f3f5c;--label-text:#c3d3f0;
}}
*{box-sizing:border-box}
html,body{height:100%}
body{margin:0;background:var(--bg);color:var(--text);font:14px/1.5 system-ui,-apple-system,"Segoe UI",sans-serif;
  display:grid;grid-template-columns:var(--menu-w) 1fr;grid-template-rows:auto 1fr auto;grid-template-areas:"header header" "menu main" "footer footer"}
header{grid-area:header;min-height:88px;background:var(--brand);color:var(--brand-text);display:grid;grid-template-columns:var(--menu-w) 1fr}
header .title{padding:14px 32px 18px;align-self:center}
.logo{position:relative;border:1px solid var(--brand-muted);background:var(--logo-bg)}
.logo img{position:absolute;inset:6px;width:calc(100% - 12px);height:calc(100% - 12px);object-fit:contain}
.crumbs ol{display:flex;flex-wrap:wrap;gap:6px;list-style:none;margin:0;padding:0;font-size:12px;color:var(--brand-muted)}
.crumbs li+li::before{content:"/";margin-right:6px}
.crumbs a{color:inherit;text-decoration:none}.crumbs a:hover{text-decoration:underline}
.crumbs [aria-current]{color:var(--brand-text)}
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
.intro{max-width:44rem;margin:0 0 20px;color:var(--muted)}
.blocks{display:grid;grid-template-columns:repeat(auto-fill,minmax(260px,1fr));gap:16px}
.block{display:block;background:var(--surface);border:1px solid var(--border);border-radius:8px;padding:18px;color:inherit;text-decoration:none}
.block:hover{border-color:var(--accent)}
.block h2{margin:0 0 6px;font-size:16px;font-weight:600}
.block .group{margin:0 0 4px;font-size:11px;font-weight:600;letter-spacing:.06em;text-transform:uppercase;color:var(--muted)}
.block p{margin:0;color:var(--muted)}
form.card{background:var(--surface);border:1px solid var(--border);border-radius:8px;padding:20px;max-width:36rem}
label{display:block;margin:0 0 14px;font-weight:600;font-size:13px}
input,select{display:block;width:100%;margin-top:4px;padding:8px 10px;font:inherit;font-weight:400;color:var(--text);background:var(--bg);
  border:1px solid var(--border);border-radius:6px}
input:focus,select:focus,button:focus-visible,a:focus-visible{outline:2px solid var(--focus);outline-offset:1px}
button{padding:9px 16px;font:inherit;font-weight:600;color:var(--accent-text);background:var(--accent);border:0;border-radius:6px;cursor:pointer}
button:hover{filter:brightness(1.08)}
code{word-break:break-all}
@media (max-width:720px){
  body{grid-template-columns:1fr;grid-template-areas:"header" "menu" "main" "footer"}
  nav.menu{border-right:0;border-bottom:1px solid var(--border)}
  :root{--menu-w:120px}
  header .title,main,footer{padding-left:16px;padding-right:16px}
}`

export interface Page {
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
  const menu = list([HOME]) + MENU.map((group) => `<h2>${escape(group.label)}</h2>${list(group.items)}`).join('')
  const footer = page.footer ? `<footer title="${escape(page.footer)}">${escape(page.footer)}</footer>` : '<footer>&nbsp;</footer>'
  return `<!doctype html><html lang="nl"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>${escape(page.title)}</title><style>${theme}</style>
<header><div class="logo">${page.logo ? `<img src="${escape(page.logo.src)}" alt="${escape(page.logo.alt)}">` : ''}</div><div class="title"><nav class="crumbs" aria-label="Kruimelpad"><ol>${crumbs}</ol></nav><h1>${escape(page.title)}</h1></div></header>
<nav class="menu" aria-label="Menu">${menu}</nav>
<main>${page.body}</main>
${footer}`
}
