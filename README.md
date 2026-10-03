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
    cubic/                  the original toy circuit (x³+x+5=y); kept only as a minimal gnark-crypto/snarkjs
                             interop reference (internal/proof/snarkjs_test.go's fixtures), not otherwise used
    diploma_membership/     the circuit examples/react-gui and the backend actually run: proves membership in a
                             small Merkle-tree credential registry plus a disclosed type/issuer, without revealing
                             which credential. registry.mjs generates the fixed demo registry; build.sh compiles
                             + runs its trusted setup
examples/
  react-gui/              Vite + React example GUI (@zk-puoi/react-gui)
    src/
      App.tsx               ties the three views together via simple tab state
      components/           LoginView, WalletView (imports a demo credential from the registry), ResourceView
                             (the proof-build + authorize flow)
      lib/clients.ts         shared ApiClient/Wallet instances, circuit asset paths, registry loader
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
local Docker Desktop, not fine for anything beyond it.

### Building and exercising the circuit

```sh
cd client-lib/circuits/diploma_membership
./build.sh               # compiles diploma_membership.circom and runs a toy Groth16 trusted setup, producing
                          # build/diploma_membership_js/diploma_membership.wasm,
                          # build/diploma_membership_final.zkey, build/verification_key.json
node registry.mjs         # (re)generates the fixed demo credential registry, build/registry.json
```

`cubic`'s own `build.sh`/`prove.sh` still work the same way, for its narrower purpose (see repo layout above).

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
Requires the backend running on `:8080` (CORS is wide open for this) and `client-lib`'s diploma_membership circuit
and registry already built (`client-lib/circuits/diploma_membership/build.sh` and `registry.mjs`, see above).

```sh
cd examples/react-gui
pnpm dev       # syncs the circuit's wasm/zkey/registry.json into public/circuits (predev hook), then starts Vite on :5173
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
  blessed by gno's maintainers. `GnoVM.Evaluate` serializes all calls behind
  a single mutex; measured (`BenchmarkGnoVMEvaluate`) at ~60µs/call on an M5
  Max, i.e. a ~16k/s ceiling on one core — not a bottleneck at this project's
  scale.
- **The G2 coordinate ordering in `internal/proof`'s snarkjs JSON parsing**
  (`parseG2`'s `coords[i][0]` → `A0`, `coords[i][1]` → `A1`) was confirmed
  correct empirically, against real snarkjs output (`internal/proof/snarkjs_test.go`'s
  fixtures), not derived from a documented spec — gnark/circom/snarkjs don't
  consistently document this convention. If a future snarkjs/circom version
  changes it, every verification would start failing; the fixture-based test
  is the safety net.
- **The demo credential registry is a small, fixed set** (`client-lib/circuits/diploma_membership/registry.mjs`'s
  two example credentials, padded to a depth-3 Merkle tree) — there's no real credential issuance; adding or
  revoking a credential means regenerating the registry and redeploying its root, not a running API. Its
  trusted setup (`build.sh`) is also the same throwaway, local, non-ceremony kind as the toy cubic circuit —
  a real deployment would need an actual MPC ceremony or a transparent-setup scheme (e.g. PLONK) instead.
