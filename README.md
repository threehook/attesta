# zk-puoi

A Go backend that authorizes requests based on verifiable credentials that users present from their own wallet. An application asks the backend to
start an authorization; the user's wallet answers with an SD-JWT presentation that discloses only what was asked for; the backend verifies it and
runs the result through an **authorization policy written in Gno**, evaluated in-process by [gnovm](https://github.com/gnolang/gno), gno.land's VM,
embedded directly in the backend (no blockchain involved).

Despite the name, presentations use selective disclosure (SD-JWT), not zero-knowledge proofs: the holder reveals only the requested claims, but the
issuer's signature and the holder's key are the same in every presentation, so verifiers that compare notes can tell it is the same credential.

This is a monorepo: the backend, a TypeScript client library, a wallet core, and two examples (a demo issuer and a relying web page).

## Architecture

```
Issuer (examples/issuer)                  Wallet (wallet-desktop/, around the Credo holder agent in wallet/)
  signs SD-JWT VC credentials  ─OpenID4VCI─►  holds credentials, keys bound per credential
                                                     ▲   │ 3. answers with an SD-JWT presentation (direct_post)
Relying application (examples/simple-gui)            │   ▼
  1. POST /v1/authorize/requests ───────────►  backend (Go)
  2. hands the openid4vp:// link to the wallet     4. verify issuer signature, disclosures, key binding
  5. polls GET /v1/authorize/requests/{id}         5. evaluate the Gno policy (resource, type, issuer DID)
        ◄── { status: done, allow, reason, subject }
```

Key design decisions:

| Area | Decision |
|---|---|
| Credentials | SD-JWT VC, issued with OpenID4VCI (pre-authorized code) and presented with OpenID4VP (DCQL query, `direct_post`) |
| Issuer identity | an Ed25519 `did:key`; the key is in the DID, so resolving needs no network. Which issuers are trusted is the policy's decision, by DID |
| Verifier identity | requests are unsigned and identify the verifier by its response URI (`redirect_uri` client identifier prefix), so there is no verifier key to manage |
| Identity | the holder is identified by the `email` claim of their credential, together with the issuer's DID. Every request asks the wallet for it, a presentation without it is rejected, and an allowed outcome carries it as `subject`; a denial does not, since an untrusted issuer's claims prove nothing |
| Replay protection | each request carries a fresh nonce and state and can be answered once; the key-binding JWT must name this request's nonce and response URI and be at most five minutes old |
| Gno integration | gnovm embedded in-process as a library (`gnovm/pkg/gnolang`) — no gno.land chain/node |
| Wallet | a desktop app (Electron) on [Credo](https://github.com/openwallet-foundation/credo-ts); keys and credentials live in an encrypted Askar store on the user's machine, and the user confirms every offer and every disclosure |
| TS workspace | pnpm workspace (root `pnpm-workspace.yaml`) linking `client-lib`, `wallet`, `wallet-desktop`, `examples/issuer` and `examples/simple-gui` |

## Repo layout

```
backend/
  cmd/
    zk-puoi/          server entrypoint
    spike/            throwaway PoC validating embedded-gnovm policy calls (superseded
                       by internal/authz, kept as a minimal reference)
  internal/
    config/            env-based configuration
    sdjwt/             verifies SD-JWT VC presentations: issuer signature, disclosures, key binding; resolves did:key issuers
    presentation/      OpenID4VP verifier: builds requests, checks the wallet's answer, keeps the decision for polling
    authz/             Evaluator backed by an embedded gnovm interpreter
    scripts/           policy source store (startup load + admin hot-deploy)
    httpapi/           HTTP handlers wiring the above together
  Dockerfile
client-lib/                TypeScript package @zk-puoi/client: typed client for the backend API (authorization requests, outcome polling, policy deploy)
wallet/                    @zk-puoi/wallet: the wallet core — a Credo holder agent that accepts credential offers, answers presentation requests, and has a CLI
wallet-desktop/            @zk-puoi/wallet-desktop: the Electron desktop wallet around the core (confirmations, credential list, link handling)
examples/
  issuer/                  demo credential issuer (Express + Credo): issues Diploma credentials through OpenID4VCI, form at /
  simple-gui/              Vite + React relying page: asks the backend for a Diploma presentation and shows the decision
    policies/                diploma_check.gno, the Gno policy this example requests; deploy it with
                             `make k8s-deploy-policy` (the backend ships with no policies)
k8s/
  local/              manifests for Docker Desktop's local k8s
    backend/          namespace/deployment/service for the Go backend
    gui/              deployment/service for examples/simple-gui
    issuer/           deployment/service for examples/issuer (LoadBalancer on localhost:4000)
  cloud/              (empty for now)
pnpm-workspace.yaml   client-lib + wallet + wallet-desktop + examples
Makefile              build/test/docker/k8s targets (see Development workflow)
```

## Development workflow

The dev/deploy/test cycle for the backend and the example page runs through Docker Desktop's built-in Kubernetes, driven by the root `Makefile`.
Quick local iteration without a container works too:

```sh
cd backend
go run ./cmd/zk-puoi      # listens on :8080, no policies unless ZKPUOI_POLICIES_DIR is set
go test ./...
```

Configuration comes from the environment: `ZKPUOI_ADDR`, `ZKPUOI_PUBLIC_URL` (where wallets reach the backend: it appears in every presentation
request), `ZKPUOI_POLICIES_DIR`, `ZKPUOI_ADMIN_TOKEN`, `ZKPUOI_CORS_ORIGINS`. `internal/config` falls back to an insecure dev default for the admin token when its variable
isn't set — fine for local Docker Desktop, not fine for anything beyond it.

For the k8s loop (requires Docker Desktop running, with Kubernetes enabled):

```sh
make docker-build       # builds into Docker Desktop's local image store, tagged uniquely per build — no registry push needed
make k8s-apply           # applies k8s/local/backend/*.yaml, then points the deployment at that exact new tag via
                          # `kubectl set image`, which always triggers a real rollout (see the Makefile's own
                          # comment on why the tag can't just be reused)
make k8s-logs             # tail the running pod's logs
make k8s-port-forward     # expose the service on localhost:8080 for curl/Postman
make k8s-delete           # tear down the namespace
make k8s-gui-apply       # deploys examples/simple-gui, then hot-deploys its policy into the backend
make k8s-issuer-apply    # deploys the demo issuer; open http://localhost:4000 for its form
```

Policies are held in memory only, so a backend restart empties the store; `make k8s-deploy-policy POLICY=<file.gno>` puts one back.

### TypeScript packages

```sh
pnpm install                          # from the repo root; allows the install scripts listed in pnpm-workspace.yaml
pnpm --filter @zk-puoi/client test     # client library
pnpm --filter @zk-puoi/issuer test     # issues credentials to an in-process wallet
pnpm --filter @zk-puoi/wallet-desktop test:e2e   # the desktop wallet, driven like a user
```

Askar, the wallet's storage, ships a native library that its install script downloads from the OpenWallet Foundation's GitHub releases.

### Trying the whole flow

With the backend running (`ZKPUOI_PUBLIC_URL` set to its address, for example `http://localhost:8080`) and `examples/simple-gui/policies/diploma_check.gno`
deployed:

```sh
cd examples/issuer && pnpm start                 # prints the issuer DID; form at http://localhost:4000 (or `make k8s-issuer-apply`)
ZKPUOI_ALLOW_INSECURE_HTTP=1 pnpm --filter @zk-puoi/wallet-desktop start    # the wallet: paste the offer link from the issuer form, confirm
cd examples/simple-gui && pnpm dev                # http://localhost:5173 — "Request access", then paste the page's link into the wallet and confirm
```

The wallet core also has a headless CLI (`cd wallet && pnpm cli accept|present|list`), handy for scripts.

`examples/issuer/scripts/e2e.ts` runs the same flow unattended against a running backend.

## Known risks and limits

- **gnovm embedding is not an upstream-stable API.** `gnovm/pkg/test.ProdStore`
  (used to build the base `gno.Store`) is commented upstream as backing the
  `gno run`/`gno test` CLI, not production systems. `internal/authz.GnoVM`
  builds that store once and forks a `TransactionStore` per call (never
  `Write()`-ing it back) rather than reusing one long-lived store directly —
  the latter makes `store.SetCachePackage` panic on the second call with the
  same package path, which is exactly what happens under real traffic. This
  works and is tested (including a hot-reload test), but the approach isn't
  blessed by gno's maintainers. `GnoVM.Evaluate` serializes all calls behind
  a single mutex; measured (`BenchmarkGnoVMEvaluate`) at ~60µs/call on an M5
  Max, i.e. a ~16k/s ceiling on one core — not a bottleneck at this project's
  scale.
- **Presentations are linkable.** SD-JWT hides undisclosed claims but not the issuer's signature or the holder's key, so a verifier, or a verifier
  together with the issuer, can correlate presentations of the same credential.
- **No revocation.** The backend does not check a credential's status; a credential is valid until it expires.
- **The policy sees the resource, the credential type and the issuer's DID only**, not the disclosed claims, and the email is not checked beyond being disclosed
  and signed by the issuer. Only Ed25519 `did:key` issuers are accepted.
