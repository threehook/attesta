# wallet-desktop

The desktop wallet: an Electron app around the wallet core (`wallet/`). The user's credentials and keys stay on their machine, in an encrypted store.

## What it does

- **Holds credentials.** The main window lists them with the claims they carry.
- **Takes an issuer's offer.** An `openid-credential-offer://` link, from a link the system hands to the app or pasted into the box, opens a
  confirmation showing who offers what. The credential is fetched and stored only when the user agrees.
- **Answers a verifier.** An `openid4vp://` link opens a confirmation showing who asks, which credential and which claims; the issuer is always
  shown to the verifier. The wallet discloses exactly the requested claims and nothing else, and only when the user agrees. If it holds nothing that
  answers the request, sharing is disabled.

## Running it

```sh
pnpm --filter @zk-puoi/wallet-desktop start     # builds, then starts Electron
```

`ZKPUOI_ALLOW_INSECURE_HTTP=1` lets it talk to issuers and verifiers on plain http, for local development. `ZKPUOI_WALLET_DATA_DIR` keeps the wallet's
data somewhere other than the system's per-user app folder.

A development run does not register itself as the handler for `openid-credential-offer://` and `openid4vp://` links, because that changes the system's
defaults; set `ZKPUOI_REGISTER_PROTOCOLS=1` to do so. A packaged app registers them. A link passed on the command line is opened when the app starts,
and one passed to a second launch goes to the running instance.

## Keys and storage

The wallet store (Askar, SQLite) is encrypted with a random key that is generated on first start and kept in the app's data folder, encrypted with
Electron's `safeStorage` — the system keychain on macOS, DPAPI on Windows, the secret service on Linux. The user is never asked for a password. If the
system's secure storage is not available the wallet refuses to start. `ZKPUOI_WALLET_KEY` supplies a key directly, for tests.

## How it is built

- **Main process** (`src/main`, Node, ESM): runs the Credo agent through `@zk-puoi/wallet`, owns the window and the links, and exposes four
  operations over IPC: list, prepare, approve, decline. Preparing a link only reads it; nothing is fetched into the wallet or sent to a verifier until
  approve.
- **Preload** (`src/preload`, CommonJS): exposes those operations to the page through `contextBridge` and nothing else.
- **Renderer** (`src/renderer`, React, built with Vite): the user interface. It runs sandboxed with context isolation, no Node access and a content
  security policy that allows only its own files; it cannot navigate away, open windows or ask for permissions.

## Tests

```sh
pnpm --filter @zk-puoi/wallet-desktop test        # key storage and link handling
pnpm --filter @zk-puoi/wallet-desktop test:e2e    # the real app, driven like a user, against a real issuer and a stand-in verifier
```

Setting `ZKPUOI_BACKEND_URL` (a backend whose `ZKPUOI_PUBLIC_URL` is that address, and `ZKPUOI_ADMIN_TOKEN` if not the dev default) also runs the whole
flow against it: issuer, wallet, backend and the Gno policy, for a trusted and an untrusted issuer.

Electron's own install script can finish without unpacking the binary on some Node versions, which shows up as "Electron failed to install
correctly". `scripts/ensure-electron.mjs` detects that and unpacks the downloaded archive with the system tools; `start` and `test:e2e` run it first.
