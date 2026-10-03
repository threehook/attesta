# zk-puoi

A Go backend that authorizes requests based on zero-knowledge proofs submitted
by clients, rather than classic authentication. A ZK proof can attest to facts
about a user — e.g. "I hold a diploma issued by a trusted university" — without
revealing the underlying credential. The proof's disclosed public signals are
then run through an **authorization policy written in Gno** and evaluated
in-process by [gnovm](https://github.com/gnolang/gno), gno.land's VM, embedded
directly in the backend (no blockchain involved).

This is a monorepo: the backend, a TypeScript client library, and an example
React GUI all live here together.

> **Status: early scaffolding.** This README is kept up to date as the codebase evolves —
> check the repo layout and known risks below before assuming more than what's described here.

## Architecture

```
React GUI (examples/react-gui)
  │  1. mock login -> JWT with role(s)            (identity/audit only)
  │  2. wallet holds mock Verifiable Credentials   (e.g. a diploma)
  │  3. client-lib builds a ZK proof in-browser    (circom + snarkjs)
  ▼
POST /v1/authorize { resource, proof, publicSignals }
  │
  ▼
backend (Go)
  │  4. verify proof (gnark-crypto, against the    -> reject if invalid
  │     circuit's circom-exported verification key)
  │  5. evaluate business policy over publicSignals
  │     via an embedded gnovm Machine running a
  │     .gno policy package                        -> allow/deny + reason
  ▼
{ allow: bool, reason: string }
```

Key design decisions (see commit history / discussion for rationale):

| Area | Decision |
|---|---|
| ZK proving | circom + snarkjs own the circuit, its trusted setup, and proving — entirely client-side |
| ZK verifying | Go backend verifies with `gnark-crypto`'s pairing primitives directly against snarkjs's exported verification key; the full `gnark` module (circuit compiler, its own Setup/Prove) isn't a dependency at all — Groth16 verification is protocol-level math, not tied to whichever toolchain produced the circuit |
| Gno integration | gnovm embedded in-process as a library (`gnovm/pkg/gnolang`) — no gno.land chain/node |
| TS workspace | pnpm workspace (root `pnpm-workspace.yaml`) linking `client-lib` and `examples/react-gui` via `workspace:*` |
| Verifiable Credentials | lightweight mock VCs for the MVP (no DID/signature infra yet) |

## Repo layout

```
backend/
  cmd/
    zk-puoi/          server entrypoint
    spike/            throwaway PoC validating embedded-gnovm policy calls (superseded
                       by internal/authz, kept as a minimal reference)
  internal/
    config/            env-based configuration
    auth/              mock login issuing a JWT (identity/audit only, not authz)
    proof/             Verifier: checks a snarkjs Groth16 proof with gnark-crypto against a verification key
    authz/             Evaluator backed by an embedded gnovm interpreter
    scripts/           policy source store (startup load + admin hot-deploy)
    httpapi/           HTTP handlers wiring the above together
  policies/            .gno authorization policy packages, loaded at startup
  Dockerfile
client-lib/                TypeScript package @zk-puoi/client
  src/
    proof.ts             buildProof(): wraps snarkjs.groth16.fullProve for browser or Node use
    api.ts               typed client for backend/internal/httpapi (login, authorize, admin deploy)
    wallet.ts             mock Verifiable Credential store (localStorage, or in-memory outside a browser)
    types.ts              shared wire types matching the backend's JSON exactly
  circuits/
    cubic/             circom circuit (toy, standing in for a real credential circuit), its build script, and a
                        CLI prove.sh for manually exercising /v1/authorize without a browser client
examples/
  react-gui/              Vite + React example GUI (@zk-puoi/react-gui)
    src/
      App.tsx               ties the three views together via simple tab state
      components/           LoginView, WalletView, ResourceView (the proof-build + authorize flow)
      lib/clients.ts         shared ApiClient/Wallet instances, circuit asset paths
    public/circuits/        synced copy of client-lib's circuit build output (gitignored, see
                             the sync-circuit script) — served as static files for snarkjs to fetch
k8s/
  backend/            namespace/deployment/service manifests for Docker Desktop's k8s
go.work               Go workspace covering backend/
pnpm-workspace.yaml   client-lib + examples/react-gui
Makefile              build/test/docker/k8s targets (see Development workflow)
```

## Development workflow

The dev/deploy/test cycle runs through Docker Desktop's built-in Kubernetes, driven by the
root `Makefile` — not `docker run` or a bare `go run` against a "real" environment. Quick
local iteration without a container still works too:

```sh
cd backend
go run ./cmd/zk-puoi      # listens on :8080, loads backend/policies at startup
go test ./...
```

`go run`'s default `ZKPUOI_VERIFICATION_KEY` assumes `client-lib/` is checked out next to
`backend/` and already has a built circuit (see below).

For the k8s loop (requires Docker Desktop running, with Kubernetes enabled):

```sh
make docker-build       # builds from the repo root (not backend/) into Docker Desktop's local
                         # image store, tagged uniquely per build — no registry push needed
make k8s-apply           # applies k8s/backend/*.yaml, then points the deployment at that
                          # exact new tag via `kubectl set image`, which always triggers a
                          # real rollout (see the Makefile's own comment on why the tag can't
                          # just be reused — Docker Desktop's k8s has been seen serving stale
                          # image content under a repeated tag)
make k8s-logs             # tail the running pod's logs
make k8s-port-forward     # expose the service on localhost:8080 for curl/Postman
make k8s-delete           # tear down the namespace
make k8s-apply POLICY=backend/policies/diploma_check.gno  # also hot-deploy a policy after rollout
```

`internal/config` falls back to insecure dev defaults (JWT secret, admin token) when their
env vars aren't set, which is what `k8s/backend/deployment.yaml` relies on for now — fine for
local Docker Desktop, not fine for anything beyond it (see Known risks).

### Building and exercising the circuit

```sh
cd client-lib/circuits/cubic
./build.sh              # compiles cubic.circom and runs a toy Groth16 trusted setup, producing
                         # build/cubic_js/cubic.wasm, build/cubic_final.zkey, build/verification_key.json
./prove.sh 3             # generates a real proof for private x=3, prints proof.json/public.json
                         # ready to paste into a POST /v1/authorize body (no browser client needed)
```

### client-lib

```sh
cd client-lib
npm install
npm test          # vitest — proof.test.ts builds a real proof against the committed circuit artifacts
npm run typecheck
npm run build      # emits dist/ (ESM + .d.ts); excludes *.test.ts via tsconfig.build.json
```

### examples/react-gui

Install once from the repo root with `pnpm install` (links `@zk-puoi/client` into the app via the workspace).
Requires the backend running on `:8080` (CORS is wide open for this, see Known risks) and `client-lib`'s circuit
already built (`client-lib/circuits/cubic/build.sh`, see above).

```sh
cd examples/react-gui
pnpm dev       # syncs the circuit's wasm/zkey into public/circuits (predev hook), then starts Vite on :5173
pnpm build      # same sync, then tsc -b && vite build
```

If you change `client-lib`'s source, rebuild it (`cd client-lib && npm run build`) and restart Vite — the
workspace link points at `client-lib/dist`, which Vite doesn't watch across the package boundary.

## Known risks / open items

- **gnovm embedding is not an upstream-stable API.** `gnovm/pkg/test.ProdStore`
  (used to build the base `gno.Store`) is commented upstream as backing the
  `gno run`/`gno test` CLI, not production systems. `internal/authz.GnoVM`
  builds that store once and forks a `TransactionStore` per call (never
  `Write()`-ing it back) rather than reusing one long-lived store directly —
  the latter makes `store.SetCachePackage` panic on the second call with the
  same package path, which is exactly what happens under real traffic. This
  works and is tested (including a hot-reload test), but the approach isn't
  blessed by gno's maintainers; worth raising with the gno community, and
  worth a concurrency/throughput benchmark (`GnoVM.Evaluate` currently
  serializes all calls behind a single mutex).
- **GNOROOT in the container is minimal.** The Docker image bundles
  `gnovm/stdlibs` and `examples` so `GNOROOT` resolves without a `go`
  toolchain at runtime, but no current policy imports anything from either
  tree — this is unexercised. Revisit once a policy actually imports a gno
  stdlib package.
- **Dev-only insecure defaults.** `ZKPUOI_JWT_SECRET` and
  `ZKPUOI_ADMIN_TOKEN` default to hardcoded values (`internal/config`) and
  `k8s/backend/deployment.yaml` doesn't override them. Fine for local Docker
  Desktop; must become real secrets before this goes anywhere else.
- **The toy circuit's trusted setup is throwaway and local**
  (`client-lib/circuits/cubic/build.sh`), not a real ceremony. Irrelevant for
  a circuit with no real secrets, but a real credential circuit will need an
  actual decision here (MPC ceremony, or a transparent-setup scheme).
- **The G2 coordinate ordering in `internal/proof`'s snarkjs JSON parsing**
  (`parseG2`'s `coords[i][0]` → `A0`, `coords[i][1]` → `A1`) was confirmed
  correct empirically, against real snarkjs output (`internal/proof/snarkjs_test.go`'s
  fixtures), not derived from a documented spec — gnark/circom/snarkjs don't
  consistently document this convention. If a future snarkjs/circom version
  changes it, every verification would start failing; the fixture-based test
  is the safety net.
- **Docker Desktop's Kubernetes has been observed running a stale image under a reused tag** even after a
  genuine `docker build` produced new content (`docker run` against the fresh image showed correct behavior;
  the k8s pod under the same tag didn't, across two separate rebuild/redeploy cycles). The Makefile now tags
  every build uniquely and uses `kubectl set image` instead of relying on a static manifest's `image:` field —
  this reliably forces a real rollout, but the root cause (containerd-side image cache/tag resolution,
  presumably) wasn't fully diagnosed.
- **`npm audit` reports 3 high-severity advisories in `client-lib`**, all transitive through `snarkjs`
  (`underscore`/`jsonpath`/`bfj`, an unbounded-recursion DoS). Upstream snarkjs's own dependency tree, not
  something this project controls without patching or forking; low real-world impact here since nothing feeds
  attacker-controlled input through those specific code paths, but unresolved.
- **The backend's CORS policy (`internal/httpapi.withCORS`) allows any origin, unconditionally.** Needed for
  `examples/react-gui`'s dev server to call a local backend at all; must become an allowlist before this is
  reachable by anything other than a trusted local dev setup.
- **`ApiClient`'s default `fetch` must be wrapped, not passed directly** (`(...args) => fetch(...args)`, not
  bare `fetch`) — a detached `fetch` reference loses the `this === window` binding browsers require internally
  and fails at call time with "Illegal invocation". Node's global `fetch` doesn't have this quirk, so
  `client-lib`'s own (Node-run) tests never caught it; only the real browser run against `examples/react-gui`
  did. Worth remembering before trusting Node-only test coverage for anything `fetch`-shaped.
