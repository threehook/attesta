# Attesta

Authorization with verifiable credentials. An application asks the backend to start an authorization; the user's wallet answers with an SD-JWT
presentation that discloses only what was asked; the backend verifies it and a Gno policy decides.

## Components

- `backend/` (Go, module `attesta/backend`): SD-JWT VC presentations are verified with `sdjwt-go` (issuer signature, disclosures, key binding; issuers
  are Ed25519 `did:key`), `internal/presentation` is the OpenID4VP verifier (unsigned requests, `redirect_uri` client id, DCQL, `direct_post`, one answer
  per request, 5 minute lifetime), `internal/status` reads the issuer's Token Status List (only from `ATTESTA_STATUS_URL_PREFIXES`, `ATTESTA_STATUS_HOSTS` maps a public host to an internal one) and denies a revoked credential, one without a status reference, and one whose list cannot be read, `internal/authz` runs Gno policies in-process (gnovm), `internal/scripts` stores policy source (loaded from `ATTESTA_POLICIES_DIR`, which
  it watches, and hot-deployed through `POST /admin/policies`; hot deployment without a restart is a requirement), `internal/httpapi` is the HTTP layer.
- `github.com/threehook/sdjwt-go` (own public repo, MIT, checked out beside this one as `../sdjwt-go`): issues and verifies SD-JWT VC; the backend and the Go issuer use it by tag.
- `client-lib/` (`@attesta/client`): typed client for the backend API.
- `github.com/threehook/enforcer-go` (own public repo, MIT, checked out beside this one as `../enforcer-go`): Go library an application uses to enforce Attesta's decisions: start an
  authorization, read the outcome, act only on Permit and carry out its obligations. `examples/laadpalen/api` depends on it by tag; the application keeps its own rules.
- `wallet/` (`@attesta/wallet`): wallet core, a Credo holder agent with an encrypted Askar store; also a headless CLI.
- `wallet-desktop/` (`@attesta/wallet-desktop`): the Electron app around the core. The user confirms every credential that is added, can remove one, and a credential of a type already held from that issuer is refused until the old one is removed; it reaches the wallet as a QR code in a mail, which the user uploads in the wallet. A disclosure is confirmed in a popup (the window stays closed), unless the user chose "Always share" for that application (kept in settings.json in its data folder). There is deliberately no switch to share with every application.
- `issuer/`: the issuer, split in `api/` (Go, module `attesta/issuer`: OpenID4VCI endpoints, entitlements and QR-code sessions in Postgres, and the mail with the QR code; credential types `Employee`, department and roles only, and one type per diploma, today `Diploma Laadpalen Management`, each with a form of its own; revoking a credential, with or without a corrected one, the roles an employee can be given (create, rename, soft delete; delete refused while in use), and the signed status list at `/status/1`) and `gui/` (the pages, which only call the API, among them `/revoke` and `/roles`). Staff sign in at an OpenID Connect server (Keycloak in the cluster, a customer's own elsewhere); the API checks their token and the role `issuer-staff`.
- `examples/laadpalen/`: relying app with Attesta as its sidecar (`api/` Go backend, `gui/` page, `policies/request_laadpaal.gno`); see its README.
- `k8s/local/{backend,issuer,laadpalen}` + `Makefile`: Docker Desktop Kubernetes, namespace `attesta`.

Flow: page `POST /v1/authorize/requests` -> `openid4vp://` link -> user pastes it into the wallet and confirms -> wallet posts to
`/v1/authorize/requests/{id}/response` -> backend verifies, policy decides -> page polls `GET /v1/authorize/requests/{id}`. Every request asks for the
holder's `email` claim; it identifies the person (issuer DID + email). An allowed outcome carries it as `subject`, a denial does not. The policy sees
(resource, credential type, issuer DID, the requested claims as strings). A request asks for a list of `credentials` (each its own type and claims; the first identifies the person, `credentialType` with `claims` is
shorthand for one); the wallet then answers with one presentation per credential, all of the same person (same email), and for more than one the policy's
`AuthorizeWithOthers` gets the others by type (claims and issuers). laadpalen asks for an `Employee` (department, roles) and the diploma `Diploma Laadpalen Management`.

## Commands

```sh
cd backend && go test ./...                      # backend; ATTESTA_* variables configure the server (ATTESTA_PUBLIC_URL matters for wallets)
pnpm install                                     # repo root; pnpm-workspace.yaml allows Askar's and koffi's install scripts
pnpm --filter @attesta/client build              # client-lib/dist is not in git; build it before the page's build or typecheck
pnpm -r typecheck && pnpm -r test                # all TS packages
pnpm --filter @attesta/wallet-desktop test:e2e   # the real Electron app against the Go issuer (needs Docker and Go: Postgres and Mailpit in containers) and a stand-in verifier
ATTESTA_BACKEND_URL=http://localhost:8080 ATTESTA_ADMIN_TOKEN=<token> ATTESTA_E2E_ISSUER_PORT=4099 pnpm --filter @attesta/wallet-desktop exec playwright test backend.spec.ts   # that backend needs ATTESTA_STATUS_URL_PREFIXES=http://localhost:4099/status/
make k8s-apply k8s-issuer-apply                  # build and deploy
make k8s-policies POLICIES="<a.gno> <b.gno>"      # hot deploy, no restart: replaces the set in the ConfigMap the backend watches; k8s-apply keeps it unless given POLICIES
make k8s-policies-add POLICIES=<a.gno>           # hot deploy of one policy, the others stay
make laadpalen                                   # the laadpalen app + its Attesta sidecar (page https://laadpalen.attesta.corbencreatives.nl, wallet posts to https://api.attesta.corbencreatives.nl); laadpalen-policies hot-deploys its policy
```

Manual test, with the packaged wallet (it refuses plain http, and is the only way to run the desktop wallet by hand): `pnpm --filter @attesta/wallet-desktop package`, open `release/mac-arm64/attesta wallet.app` once so it becomes
the `openid4vp://` handler, then use https://laadpalen.attesta.corbencreatives.nl and https://issuer.attesta.corbencreatives.nl. Setup: `make k8s-traefik k8s-ingress` (Traefik in
namespace `ingress`, Ingress in `k8s/local/ingress`); the names are in the deSEC zone `attesta.corbencreatives.nl` and resolve to 127.0.0.1, so a router with DNS rebind protection needs
that domain as an exception. The wildcard certificate is from `lego` (DNS-01 at deSEC, files in `~/.config/attesta/lego`; renew by hand every 90 days); `make k8s-tls` loads it.
The issuer's staff user is `staff`, its password is in the Secret `attesta-issuer-auth` (key `staff-password`). The admin token is in the Secret `attesta-backend` (`kubectl -n attesta get secret attesta-backend -o jsonpath='{.data.admin-token}' | base64 -d`).

## Things learned the hard way

- Askar's SQLite store cannot open a path containing `@`; Electron's default data folder came from the scoped package name, so the app has
  `productName` "attesta wallet" and `createWalletAgent` refuses such a path.
- Electron 39's installer hangs on Node 24.16+/26.1+ (nodejs/node#62557); Electron 44 fixes it and downloads its binary on first run, not during install.
- Kubernetes injects `<SERVICE>_PORT`-style variables; with Service `attesta-issuer-api` that is `ATTESTA_ISSUER_API_PORT`, close to our own `ATTESTA_ISSUER_PORT`. Deployments set
  `enableServiceLinks: false`. Avoid config variable names equal to a Service name plus `_PORT`/`_HOST`.
- The Makefile does `kubectl apply` then `kubectl set image` with a unique tag; re-applying a manifest alone resets the image to the nonexistent `:dev`.
- The old and new LoadBalancers share host port 4000.
- Desktop wallet dev switches (`ATTESTA_WALLET_KEY`, `ATTESTA_WALLET_DATA_DIR`, `ATTESTA_ALLOW_INSECURE_HTTP`) are honoured only when the app is not packaged.
- A new `enforcer-go` tag is not visible to `go mod tidy` until sum.golang.org drops its cached 404 (about 30 minutes); `GOPROXY=direct GONOSUMDB=github.com/threehook/enforcer-go` skips the wait.
- pnpm 12 blocks install scripts that are not listed in `allowBuilds`; `-s` is not a pnpm flag.

## Words

- **Entitlement:** the issuer's lasting right of one person (by email) to one credential. It is issued once. Not an "offer": an offer is a one-time message, which is what this replaced.
- **QR code:** what the issuer mails to claim an entitlement; it has a lifetime (24 hours) and works once. The lifetime belongs to the QR code, not to the entitlement.
- The standard and the library keep their own names, and so do we where they are fixed: `openid-credential-offer://`, `credential_offer_uri`, the `/offers/<id>` path, Credo's `createCredentialOffer`. Nothing we name ourselves says "offer".

## How the owner works

- Explain simply and briefly, with the point first; long or abstract explanations are unwelcome. "First explain" means explain and wait.
- Do not commit or push unless asked; when asked to commit, do it in a few logical steps with the Co-Authored-By trailer. Ask before destructive or
  outward-facing actions; when told to delete something, look at it first.
- Comments: short, one needed fact, up to 150 characters per line. No progress or roadmap text in comments or the root README; the README's limits
  section lists only real, verified limits. If something is already fixed, say so in one sentence and stop.
