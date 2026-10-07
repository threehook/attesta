# attesta

Authorization with verifiable credentials. An application asks the backend to start an authorization; the user's wallet answers with an SD-JWT
presentation that discloses only what was asked; the backend verifies it and a Gno policy decides.

## Components

- `backend/` (Go, module `attesta/backend`): `internal/sdjwt` verifies SD-JWT VC presentations (issuer signature, disclosures, key binding; issuers
  are Ed25519 `did:key`), `internal/presentation` is the OpenID4VP verifier (unsigned requests, `redirect_uri` client id, DCQL, `direct_post`, one answer
  per request, 5 minute lifetime), `internal/authz` runs Gno policies in-process (gnovm), `internal/scripts` stores policy source (loaded from `ATTESTA_POLICIES_DIR`, which
  it watches, and hot-deployed through `POST /admin/policies`; hot deployment without a restart is a requirement), `internal/httpapi` is the HTTP layer.
- `client-lib/` (`@attesta/client`): typed client for the backend API.
- `wallet/` (`@attesta/wallet`): wallet core, a Credo holder agent with an encrypted Askar store; also a headless CLI.
- `wallet-desktop/` (`@attesta/wallet-desktop`): the Electron app around the core. The user confirms every offer. A disclosure is confirmed in a popup (the window stays closed), unless the user chose "Always share" for that application (kept in settings.json in its data folder). There is deliberately no switch to share with every application.
- `examples/issuer/`: demo issuer (Express + Credo, OpenID4VCI). `examples/laadpalen/`: relying app with attesta as its sidecar (`api/` Go backend, `gui/` page, `policies/request_laadpaal.gno`); see its README.
- `k8s/local/{backend,issuer,laadpalen}` + `Makefile`: Docker Desktop Kubernetes, namespace `attesta`.

Flow: page `POST /v1/authorize/requests` -> `openid4vp://` link -> user pastes it into the wallet and confirms -> wallet posts to
`/v1/authorize/requests/{id}/response` -> backend verifies, policy decides -> page polls `GET /v1/authorize/requests/{id}`. Every request asks for the
holder's `email` claim; it identifies the person (issuer DID + email). An allowed outcome carries it as `subject`, a denial does not. The policy sees
(resource, credential type, issuer DID, the requested claims as strings).

## Commands

```sh
cd backend && go test ./...                      # backend; ATTESTA_* variables configure the server (ATTESTA_PUBLIC_URL matters for wallets)
pnpm install                                     # repo root; pnpm-workspace.yaml allows Askar's and koffi's install scripts
pnpm --filter @attesta/client build              # client-lib/dist is not in git; build it before the page's build or typecheck
pnpm -r typecheck && pnpm -r test                # all TS packages
pnpm --filter @attesta/wallet-desktop test:e2e   # the real Electron app against a real issuer and a stand-in verifier
ATTESTA_BACKEND_URL=http://localhost:8080 ATTESTA_ADMIN_TOKEN=<token> pnpm --filter @attesta/wallet-desktop exec playwright test backend.spec.ts
make k8s-apply k8s-issuer-apply                  # build and deploy
make k8s-policies POLICIES="<a.gno> <b.gno>"      # hot deploy, no restart: replaces the set in the ConfigMap the backend watches; k8s-apply keeps it unless given POLICIES
make k8s-policies-add POLICIES=<a.gno>           # hot deploy of one policy, the others stay
make laadpalen                                   # the laadpalen app + its attesta sidecar (page https://laadpalen.attesta.corbencreatives.nl, wallet posts to https://api.attesta.corbencreatives.nl); laadpalen-policies hot-deploys its policy
```

Manual test, with the packaged wallet (it refuses plain http, and is the only way to run the desktop wallet by hand): `pnpm --filter @attesta/wallet-desktop package`, open `release/mac-arm64/attesta wallet.app` once so it becomes
the `openid4vp://` handler, then use https://laadpalen.attesta.corbencreatives.nl and https://issuer.attesta.corbencreatives.nl. Setup: `make k8s-traefik k8s-ingress` (Traefik in
namespace `ingress`, Ingress in `k8s/local/ingress`); the names are in the deSEC zone `attesta.corbencreatives.nl` and resolve to 127.0.0.1, so a router with DNS rebind protection needs
that domain as an exception. The wildcard certificate is from `lego` (DNS-01 at deSEC, files in `~/.config/attesta/lego`; renew by hand every 90 days); `make k8s-tls` loads it.
The admin token is in the Secret `attesta-backend` (`kubectl -n attesta get secret attesta-backend -o jsonpath='{.data.admin-token}' | base64 -d`).

## Things learned the hard way

- Askar's SQLite store cannot open a path containing `@`; Electron's default data folder came from the scoped package name, so the app has
  `productName` "attesta wallet" and `createWalletAgent` refuses such a path.
- Electron 39's installer hangs on Node 24.16+/26.1+ (nodejs/node#62557); Electron 44 fixes it and downloads its binary on first run, not during install.
- Kubernetes injects `<SERVICE>_PORT`-style variables; with Service `attesta-issuer` that is `ATTESTA_ISSUER_PORT`, our own setting. Deployments set
  `enableServiceLinks: false`. Avoid config variable names equal to a Service name plus `_PORT`/`_HOST`.
- The Makefile does `kubectl apply` then `kubectl set image` with a unique tag; re-applying a manifest alone resets the image to the nonexistent `:dev`.
- The issuer image is Debian-based because Askar's Linux library is built for glibc. The old and new LoadBalancers share host port 4000.
- Desktop wallet dev switches (`ATTESTA_WALLET_KEY`, `ATTESTA_WALLET_DATA_DIR`, `ATTESTA_ALLOW_INSECURE_HTTP`) are honoured only when the app is not packaged.
- pnpm 12 blocks install scripts that are not listed in `allowBuilds`; `-s` is not a pnpm flag.

## How the owner works

- Explain simply and briefly, with the point first; long or abstract explanations are unwelcome. "First explain" means explain and wait.
- Do not commit or push unless asked; when asked to commit, do it in a few logical steps with the Co-Authored-By trailer. Ask before destructive or
  outward-facing actions; when told to delete something, look at it first.
- Comments: short, one needed fact, up to 150 characters per line. No progress or roadmap text in comments or the root README; the README's limits
  section lists only real, verified limits. If something is already fixed, say so in one sentence and stop.
