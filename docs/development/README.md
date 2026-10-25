# Attesta, technical README

What Attesta is and does is in the [README at the root](../../README.md). This one is for people who build, run or extend it. Paths are relative to the repository root.

A Go backend that authorizes requests based on verifiable credentials that users present from their own wallet. An application asks the backend to
start an authorization; the user's wallet answers with an SD-JWT presentation that discloses only what was asked for; the backend verifies it and
runs the result through an **authorization policy written in Rego**, evaluated in-process by [OPA](https://www.openpolicyagent.org/),
embedded directly in the backend.

Presentations use selective disclosure (SD-JWT): the holder reveals only the requested claims, but the issuer's signature
and the holder's key are the same in every presentation, so verifiers that compare notes can tell it is the same credential.

This is a monorepo: the backend, a TypeScript client library, a wallet core and a desktop wallet, the issuer (API and pages, with a Policy Editor and Policy Tester for writing and trying policies), and an example (the laadpalen app, a relying application).

## Architecture

How the pieces fit together, with the end-to-end flow and the design of the next steps, is in [architecture/architecture.md](../architecture/architecture.md).

Key design decisions:

| Area | Decision |
|---|---|
| Credentials | SD-JWT VC, issued with OpenID4VCI (pre-authorized code) and presented with OpenID4VP (DCQL query, `direct_post`) |
| Issuer identity | an Ed25519 `did:key`; the key is in the DID, so resolving needs no network. Which issuers are trusted is the policy's decision, by DID |
| Verifier identity | requests are unsigned and identify the verifier by its response URI (`redirect_uri` client identifier prefix), so there is no verifier key to manage |
| Identity | the holder is identified by the `email` claim of their credential, together with the issuer's DID. Every request asks the wallet for it, a presentation without it is rejected, and an allowed outcome carries it as `subject`; a denial does not, since an untrusted issuer's claims prove nothing |
| Revocation | the issuer publishes a signed [Token Status List](https://datatracker.ietf.org/doc/draft-ietf-oauth-status-list/) (1 bit per credential) and every credential points to its bit. The backend reads the list of the credential's issuer, only from the URLs it is configured with, and denies a revoked credential with the reason "credential has been revoked". A credential without a status reference, and a list that cannot be read or has expired, are denied |
| Replay protection | each request carries a fresh nonce and state and can be answered once; the key-binding JWT must name this request's nonce and response URI and be at most five minutes old |
| Policy authoring | the issuer's pages have a **Policy Editor** (Rego with the problems, line and position, marked as you type; open, create and save policies on your own disk) and a **Policy Tester** (run an opened policy on an input of your own and see the decision, reason, obligations and advice). Both call the backend's admin endpoints `POST /admin/policies/diagnose` and `POST /admin/policies/evaluate`, which nothing is stored by. Claim names and credential types in `input.credentials` are free, so a policy can use a claim no credential carries yet |
| Policy integration | OPA embedded in-process as a library (`github.com/open-policy-agent/opa/v1/rego`), no OPA server. A policy is one `.rego` file in `package policy` that defines the rule `decision`, an object with `decision` (`Permit`, `Deny`, `NotApplicable` or `Indeterminate`, XACML; anything else is an error), `reason`, and the lists `obligations` and `advice`; a rule that is undefined gives `Indeterminate`. The input is `resource`, `userRoles` and `credentials`. `credentials` holds each presented credential by credential type, with its `issuer` and its `claims` (the claims the request asked for, the email included; a request fails unless the wallet disclosed all of them; text claims arrive as they are, other values as JSON); a request asks for each type at most once, and the credentials must all be of the same person (the same email). `userRoles` comes from the `Att-User-Roles` header (comma-separated) of the application's request, is empty without it, and is not verified by Attesta. Every item of `obligations` and `advice` is one statement: an object with an `id` and `attributes`, an object of string values. An application must carry out the obligations that come with a decision or not grant access; it may ignore advice. Policies may not call `http.send`, `net.lookup_ip_addr`, `opa.runtime`, `time.now_ns`, `rand.intn` or `uuid.rfc4122`, and an evaluation is cut off after 500 ms. |
| Decision log | one [ADL](https://gitdocumentatie.logius.nl/publicatie/ftv/adl/1.0.0/) Level 1 record per decision, to stdout and over OTLP; see `backend/internal/adl/adl.md` |
| Wallet | a desktop app (Electron) on [Credo](https://github.com/openwallet-foundation/credo-ts); keys and credentials live in an encrypted Askar store on the user's machine, and the user confirms every credential it adds, and every disclosure unless they chose to always share with that application |
| TS workspace | pnpm workspace (root `pnpm-workspace.yaml`) linking `client-lib`, `wallet`, `wallet-desktop`, `issuer/{api,gui}` and `examples/laadpalen/gui` |

## Repo layout

```
backend/
  cmd/
    attesta/          server entrypoint
  internal/
    config/            env-based configuration
    sdjwt/             verifies SD-JWT VC presentations: issuer signature, disclosures, key binding; resolves did:key issuers
    presentation/      OpenID4VP verifier: builds requests, checks the wallet's answer, keeps the decision for polling
    status/            reads the issuer's status list and tells whether a credential was revoked
    authz/             Evaluator backed by an embedded OPA engine
    scripts/           policy source store (loaded from a directory that is watched, plus admin hot-deploy)
    httpapi/           HTTP handlers wiring the above together
  Dockerfile
client-lib/                TypeScript package @attesta/client: typed client for the backend API (authorization requests, outcome polling, policy deploy)
wallet/                    @attesta/wallet: the wallet core — a Credo holder agent that takes credentials from issuers' links, answers presentation requests, and has a CLI
wallet-desktop/            @attesta/wallet-desktop: the Electron desktop wallet around the core (confirmations, credential list, link handling)
issuer/                    credential issuer; see its README
  api/                     Go (module attesta/issuer): OpenID4VCI, entitlements, roles, credential types, status list, the mail with the QR code
  gui/                     @attesta/issuer-gui: the pages, including the Policy Editor and Policy Tester
examples/
  cicd-agent/              a CI/CD agent with a wallet and an Attesta of its own; see its README
  laadpalen/               a relying application with Attesta as its sidecar: Go backend (api/), React page (gui/), policy (policies/); see its README
k8s/
  local/              manifests for Docker Desktop's local k8s
    backend/          namespace/deployment/service for the Go backend
    issuer/           deployments/services for the issuer: api, gui, Postgres and Keycloak
    laadpalen/        the laadpalen app: one pod with its backend and the Attesta sidecar, and its page
    ingress/          Ingress for the issuer, the laadpalen page and the sidecar's API; Traefik terminates https (make k8s-traefik k8s-ingress)
  cloud/              manifests (backend, cicd) and Terraform (azure) for AKS
pnpm-workspace.yaml   client-lib, wallet, wallet-desktop, issuer/gui and examples/laadpalen/gui
Makefile              build/test/docker/k8s targets (see Development workflow)
```

## GUIs

| GUI | Where | Used for |
|---|---|---|
| Issuer | `issuer/gui` | The organisation's staff records an Employee credential or a diploma for a person, manages roles and credential forms, and revokes or corrects credentials. The person gets a mail with a QR code, uploads it in their wallet, and the wallet fetches the credential. Under Tools: the Policy Editor and Policy Tester. The pages are in Dutch. |
| Laadpalen page | `examples/laadpalen/gui` | The employee's page of the laadpalen app: sign in with the wallet, then request a laadpaal. Attesta decides from the credential in the wallet. |
| Wallet | `wallet-desktop` | The employee's wallet: reads the QR code from the mail, confirms every credential it adds, and confirms every disclosure unless the employee chose to always share with that application. |

Each GUI is described, with screenshots, in the README of its module: [issuer](../../issuer/README.md), [laadpalen](../../examples/laadpalen/README.md) and [wallet-desktop](../../wallet-desktop/README.md). They share one look: a header with the logo and a breadcrumb, a menu on the left, and a one-line footer.

## Development workflow

The dev/deploy/test cycle for the backend and the example page runs through Docker Desktop's built-in Kubernetes, driven by the root `Makefile`.
Quick local iteration without a container works too:

```sh
cd backend
go run ./cmd/attesta      # listens on :8080, no policies unless ATTESTA_POLICIES_DIR is set
go test ./...
```

Every setting, with its default, is in [configuration.md](../configuration.md).

For the k8s loop (requires Docker Desktop running, with Kubernetes enabled):

```sh
make docker-build       # builds into Docker Desktop's local image store, tagged uniquely per build — no registry push needed
make k8s-apply           # applies k8s/local/backend/*.yaml, then points the deployment at that exact new tag via
                          # `kubectl set image`, which always triggers a real rollout (see the Makefile's own
                          # comment on why the tag can't just be reused)
make k8s-logs             # tail the running pod's logs
make k8s-port-forward     # expose the service on localhost:8080 for curl/Postman
make k8s-delete           # tear down the namespace
make k8s-issuer-apply    # deploys the issuer; open its pages and sign in as staff
make laadpalen           # deploys examples/laadpalen (needs the issuer): the page is reached through the Ingress (needs make k8s-traefik k8s-ingress once)
make laadpalen-policies  # hot-deploys that app's policies into its sidecar, no restart
```

The policies of a backend are loaded from a directory that is watched, and hot-deployed through `POST /admin/policies`; see [configuration.md](../configuration.md#policies). The laadpalen sidecar gets its directory from the ConfigMap `laadpalen-policies` (`make laadpalen-policies`).

```sh
pnpm install                          # from the repo root; allows the install scripts listed in pnpm-workspace.yaml
pnpm --filter @attesta/client test     # client library
pnpm --filter @attesta/issuer-gui test  # the issuer's pages
cd issuer/api && go test ./...          # the issuer's API (needs Docker)
pnpm --filter @attesta/wallet-desktop test:e2e   # the desktop wallet, driven like a user
```

Askar, the wallet's storage, ships a native library that its install script downloads from the OpenWallet Foundation's GitHub releases.

### Trying the whole flow

The laadpalen app and its policies are in [examples/laadpalen](../../examples/laadpalen/README.md):

```sh
make k8s-issuer-apply laadpalen                                # the issuer and the laadpalen app
pnpm --filter @attesta/wallet-desktop package                  # the wallet: record credentials on the issuer's pages, then upload the QR code from the mail on the wallet's "QR-code uploaden" page (needs https)
```

The wallet core also has a headless CLI (`cd wallet && pnpm cli accept|present|list`), handy for scripts.

## Known risks and limits

- **Presentations are linkable.** SD-JWT hides undisclosed claims but not the issuer's signature or the holder's key, so a verifier, or a verifier
  together with the issuer, can correlate presentations of the same credential.
- **The email is as reliable as the issuer's checks on it.** Attesta only checks that it is disclosed and signed by the issuer; which issuers to trust is the policy's decision.
- **Only Ed25519 `did:key` issuers are supported.** A `did:key` is the key itself, so replacing an issuer's key gives a new DID, and the policies that trust it must be updated.

## License

MIT, see [LICENSE](../../LICENSE).
