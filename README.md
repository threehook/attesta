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
  │  4. verify proof (gnark Groth16)               -> reject if invalid
  │  5. evaluate business policy over publicSignals
  │     via an embedded gnovm Machine running a
  │     .gno policy package                        -> allow/deny + reason
  ▼
{ allow: bool, reason: string }
```

Key design decisions (see commit history / discussion for rationale):

| Area | Decision |
|---|---|
| ZK proving | circom + snarkjs in the browser client; gnark Groth16 verification in the Go backend |
| Gno integration | gnovm embedded in-process as a library (`gnovm/pkg/gnolang`) — no gno.land chain/node |
| TS workspace | pnpm workspaces across `client-lib` and `examples/react-gui` |
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
    proof/             ProofVerifier + a toy gnark Groth16 circuit standing in for a real credential circuit
    authz/             Evaluator backed by an embedded gnovm interpreter
    scripts/           policy source store (startup load + admin hot-deploy)
    httpapi/           HTTP handlers wiring the above together
  policies/            .gno authorization policy packages, loaded at startup
  Dockerfile
client-lib/          (planned) TypeScript zk-puoi client library
examples/
  react-gui/         (planned) Vite + React example GUI
k8s/
  backend/            namespace/deployment/service manifests for Docker Desktop's k8s
go.work               Go workspace covering backend/
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

For the k8s loop (requires Docker Desktop running, with Kubernetes enabled):

```sh
make docker-build       # docker build -t zk-puoi-backend:dev backend
                         # (Docker Desktop's k8s reads from the same local image store —
                         # no registry push needed)
make k8s-apply           # applies k8s/backend/*.yaml, rebuilds the image first, and
                          # rollout-restarts so new pods pick it up
make k8s-logs             # tail the running pod's logs
make k8s-port-forward     # expose the service on localhost:8080 for curl/Postman
make k8s-delete           # tear down the namespace
```

`internal/config` falls back to insecure dev defaults (JWT secret, admin token) when their
env vars aren't set, which is what `k8s/backend/deployment.yaml` relies on for now — fine for
local Docker Desktop, not fine for anything beyond it (see Known risks).

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
- **`POST /v1/dev/prove`** generates toy-circuit proofs server-side purely so
  `/v1/authorize` can be tested with curl without a real browser-based ZK
  client. It's registered unconditionally by `cmd/zk-puoi` today — make it
  opt-out (or remove it) once a real client exists.
- **The toy circuit's trusted setup is throwaway and process-local**
  (`proof.NewCubicScheme`), per `groth16.Setup`'s own docs. Irrelevant for a
  circuit with no real secrets, but a real credential circuit will need an
  actual decision here (MPC ceremony, or a transparent-setup scheme).
