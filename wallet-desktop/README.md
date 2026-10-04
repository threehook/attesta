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
pnpm --filter @attesta/wallet-desktop start     # builds, then starts Electron
```

Three environment variables are for development and tests only: `ATTESTA_ALLOW_INSECURE_HTTP=1` lets the wallet talk to issuers and verifiers on plain
http, `ATTESTA_WALLET_DATA_DIR` keeps its data somewhere other than the system's per-user app folder, and `ATTESTA_WALLET_KEY` supplies the store key
directly. An installed (packaged) app ignores all three (`src/main/dev-switches.ts`).

A development run does not register itself as the handler for `openid-credential-offer://` and `openid4vp://` links, because that changes the system's
defaults; set `ATTESTA_REGISTER_PROTOCOLS=1` to do so. A packaged app registers them. A link passed on the command line is opened when the app starts,
and one passed to a second launch goes to the running instance.

## Keys and storage

The wallet store (Askar, SQLite) is encrypted with a random key that is generated on first start and kept in the app's data folder, encrypted with
Electron's `safeStorage` — the system keychain on macOS, DPAPI on Windows, the secret service on Linux. The user is never asked for a password. If the
system's secure storage is not available the wallet refuses to start.

## How it is built

- **Main process** (`src/main`, Node, ESM): runs the Credo agent through `@attesta/wallet`, owns the window and the links, and exposes four
  operations over IPC: list, prepare, approve, decline. Preparing a link only reads it; nothing is fetched into the wallet or sent to a verifier until
  approve.
- **Preload** (`src/preload`, CommonJS): exposes those operations to the page through `contextBridge` and nothing else.
- **Renderer** (`src/renderer`, React, built with Vite): the user interface. It runs sandboxed with context isolation, no Node access and a content
  security policy that allows only its own files; it cannot navigate away, open windows or ask for permissions.

## Tests

```sh
pnpm --filter @attesta/wallet-desktop test        # key storage and link handling
pnpm --filter @attesta/wallet-desktop test:e2e    # the real app, driven like a user, against a real issuer and a stand-in verifier
```

Setting `ATTESTA_BACKEND_URL` (a backend whose `ATTESTA_PUBLIC_URL` is that address, and `ATTESTA_ADMIN_TOKEN` if not the dev default) also runs the whole
flow against it: issuer, wallet, backend and the Gno policy, for a trusted and an untrusted issuer.

Electron 40 and later no longer download their binary during `pnpm install`: the first `electron` run (or first `require('electron')`, which is what the
tests do) fetches it from GitHub releases and unpacks it. Earlier versions unpacked it with `extract-zip`, which hangs on Node 24.16 and 26.1 and later
and leaves the install half done ("Electron failed to install correctly"); newer ones use their own extractor.
