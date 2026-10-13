const escape = (s: string) => s.replace(/[&<>"']/g, (c) => `&#${c.charCodeAt(0)};`)

// A plain page in the colours of the issuer's pages: the holder sees these in a browser, one at a time.
function page(title: string, body: string, issuerName?: string): string {
  return `<!doctype html><html lang="nl"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>${escape(title)}</title><style>
:root{--bg:#f4f6f9;--surface:#fff;--text:#1b2433;--muted:#5d6b82;--border:#d9dfe8;--brand:#14315c;--accent:#1f5fbf;--accent-text:#fff}
@media (prefers-color-scheme:dark){:root{--bg:#12171f;--surface:#1b222d;--text:#e6eaf1;--muted:#98a4b8;--border:#2c3646;--brand:#0e2242;--accent:#5b92e5;--accent-text:#0b1220}}
body{margin:0;background:var(--bg);color:var(--text);font:14px/1.5 system-ui,-apple-system,"Segoe UI",sans-serif}
header{background:var(--brand);color:#fff;padding:20px 32px;font-size:20px;font-weight:600}
main{max-width:30rem;margin:32px auto;padding:0 16px}
.card{background:var(--surface);border:1px solid var(--border);border-radius:8px;padding:20px}
h1{font-size:18px;margin:0 0 12px}p{margin:0 0 14px;color:var(--muted)}
label{display:block;font-weight:600;font-size:13px}
input{display:block;width:100%;margin:4px 0 14px;padding:8px 10px;font:inherit;color:var(--text);background:var(--bg);border:1px solid var(--border);border-radius:6px}
button,a.button{display:inline-block;padding:9px 16px;font:inherit;font-weight:600;color:var(--accent-text);background:var(--accent);border:0;border-radius:6px;cursor:pointer;text-decoration:none}
</style><header>${escape(issuerName ?? 'Aanmelden')}</header><main><div class="card"><h1>${escape(title)}</h1>${body}</div></main>`
}

export const emailPage = (action: string, requestId: string, issuerName?: string, error?: string) =>
  page(
    'Aanmelden',
    `<p>Vul uw e-mailadres in. U krijgt een e-mail met een link waarmee u zich aanmeldt.</p>${error ? `<p role="alert">${escape(error)}</p>` : ''}
<form method="post" action="${escape(action)}"><input type="hidden" name="request" value="${escape(requestId)}">
<label>E-mailadres <input name="email" type="email" autocomplete="email" required autofocus></label><button type="submit">Stuur link</button></form>`,
    issuerName,
  )

export const sentPage = (issuerName?: string) =>
  page('Controleer uw e-mail', '<p>Als dit adres kan worden gebruikt, is er een e-mail met een link onderweg. De link is kort geldig en werkt één keer.</p>', issuerName)

export const confirmPage = (action: string, token: string, issuerName?: string) =>
  page(
    'Bevestig het aanmelden',
    `<p>Druk op de knop om u aan te melden.</p><form method="post" action="${escape(action)}"><input type="hidden" name="token" value="${escape(token)}"><button type="submit">Aanmelden</button></form>`,
    issuerName,
  )

/** The page after a login: it sends the browser back to the client, with a link in case that does not happen by itself. */
export const donePage = (redirect: string, issuerName?: string) =>
  `${page('U bent aangemeld', `<p>U kunt terug naar de app.</p><a class="button" href="${escape(redirect)}">Verder</a>`, issuerName).replace('<meta charset="utf-8">', `<meta charset="utf-8"><meta http-equiv="refresh" content="0;url=${escape(redirect)}">`)}`

export const errorPage = (message: string, issuerName?: string) => page('Aanmelden mislukt', `<p>${escape(message)}</p>`, issuerName)
