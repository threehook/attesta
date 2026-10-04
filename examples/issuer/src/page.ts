const escape = (s: string) => s.replace(/[&<>"']/g, (c) => `&#${c.charCodeAt(0)};`)

const style = `body{font-family:system-ui,sans-serif;max-width:32rem;margin:3rem auto;padding:0 1rem}
label{display:block;margin:.75rem 0}input{width:100%;padding:.4rem;box-sizing:border-box}
code{word-break:break-all}`

export function renderForm(): string {
  return `<!doctype html><meta charset="utf-8"><title>Diploma issuer</title><style>${style}</style>
<h1>Diploma issuer</h1>
<form method="post" action="/offers">
  <label>Name <input name="name" required></label>
  <label>Email <input name="email" type="email" required></label>
  <label>Degree <input name="degree" required></label>
  <label>University <input name="university" required></label>
  <button type="submit">Create credential offer</button>
</form>`
}

export function renderOffer(offerUri: string): string {
  return `<!doctype html><meta charset="utf-8"><title>Credential offer</title><style>${style}</style>
<h1>Credential offer</h1>
<p>Open this link in the wallet:</p>
<p><a href="${escape(offerUri)}"><code>${escape(offerUri)}</code></a></p>
<p><a href="/">Issue another</a></p>`
}
