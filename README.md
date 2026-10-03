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

> **Status: early scaffolding.** This README is kept up to date as each phase
> below lands — check it before assuming anything beyond what's listed as done.

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
backend/            Go module (zk-puoi/backend)
  cmd/spike/         throwaway PoC validating embedded-gnovm policy calls
  policies/          .gno authorization policy packages
  (planned: cmd/zk-puoi, internal/api, internal/auth, internal/proof,
   internal/authz, internal/gnoadapter, internal/scripts, circuits/)
client-lib/          (planned) TypeScript zk-puoi client library
examples/
  react-gui/         (planned) Vite + React example GUI
go.work              Go workspace covering backend/
```

## Plan status

- [x] **Phase 1 — gnovm embedding spike.** Validated that a `.gno` policy
      package can be loaded into an embedded `gnovm.Machine` and its exported
      function called from Go, entirely in-process. See
      `backend/cmd/spike/main.go` and `backend/policies/diploma_check.gno`.
      Run it: `cd backend && go run ./cmd/spike`
- [ ] **Phase 2 — backend core.** HTTP API skeleton, mock login/JWT,
      `ProofVerifier` (gnark) with a toy circuit, `internal/authz` built on
      the spike's embedding pattern, policy loader (startup + hot-deploy).
- [ ] **Phase 3 — client-lib.** API client, snarkjs proof-building wrapper,
      mock wallet/VC store, shared types.
- [ ] **Phase 4 — examples/react-gui.** Mock login, wallet view, resource
      request flow wired to the real backend.
- [ ] **Phase 5 — real example circuit + policy.** `diploma_membership.circom`
      proving selective disclosure of credential type/issuer; matching
      `.gno` policy with a trusted-issuer allowlist.
- [ ] **Phase 6 — polish.** docker-compose for local dev, tests, CI, docs.

## Known risks / open items

- `gnovm/pkg/test.ProdStore`, used by the spike to build the gno store, is
  upstream-commented as not intended for production systems (it backs the
  `gno run`/`gno test` CLI). The planned approach for Phase 2 is to build the
  store once at startup and reuse it across short-lived per-call `Machine`
  instances — needs a latency benchmark before relying on it long-term.
