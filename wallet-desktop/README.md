# wallet-desktop

The desktop wallet: an Electron app around the wallet core (`wallet/`). The user's credentials and keys stay on their machine, in an encrypted store.

## What it does

- **Holds credentials.** The Credentials page lists them with the claims they carry.
- **Takes an issuer's offer.** An `openid-credential-offer://` link, from a link the system hands to the app or pasted on the Link openen page, opens a
  confirmation showing who offers what. The credential is fetched and stored only when the user agrees.
- **Answers a verifier.** An `openid4vp://` link that the system hands to the app never opens the window. The wallet discloses exactly the requested
  claims and nothing else (the issuer is always shown to the verifier), and a small popup asks first: Delen, Altijd delen, or Weigeren. "Altijd delen" makes
  that application, kept as the origin it answers to, get answers with no popup at all; a notification says what was shared. If the wallet holds nothing
  that answers the request, a popup says so and nothing is shared. A link pasted into the window is confirmed in the window instead, with the same
  "Altijd delen met <application>" choice.
- **Signs in.** A request that asks for nothing but the email is a sign-in. The link can name the identity to answer with (`login_hint` for the email,
  `issuer_hint` for the issuer's DID); the wallet narrows the request to that credential and takes the parameters off. The popup says who the user
  would sign in as (email and DID) and offers Bevestigen or Annuleren, with a checkbox "Altijd aanmelden, zonder de knop Aanmelden". Ticked, that
  identity signs in to that application with no popup; its other requests are still asked about, and "Altijd delen" does not sign in.
- **Tells pages who the user is.** The wallet listens on `127.0.0.1:47653` (`ATTESTA_WALLET_PORT` in a development run) and answers
  `GET /identities?type=<credential type>&application=<origin>` with the email and issuer of each credential of that type, and whether that identity
  signs in automatically to that application. Any page in a browser on this machine can read this; nothing else of the wallet is exposed. If the
  port is taken the wallet still works, and a page cannot offer the choice.
- **Sharing settings.** The Delen page lists the identities that sign in automatically (with the application) and the applications the user
  always shares with, each with a Verwijderen button. There is no switch to share with every application: any page could then read what a request asks for. Offers from an issuer are always
  confirmed in the window. The lists are kept in `settings.json` in the wallet's data folder; a missing or damaged file means asking every time.

## Running it

`pnpm --filter @attesta/wallet-desktop package` builds an unsigned macOS app into `release/mac-arm64/attesta wallet.app` (electron-builder, settings in
`package.json`). Open it once: the app registers itself as the handler for `openid-credential-offer://` and `openid4vp://` links, which it lists in its
`Info.plist`. A link passed on the command line is opened when the app starts, and one passed to a second launch goes to the running instance. Quit and
reopen the app after a new package.

Four environment variables exist for the end-to-end tests, which launch the app from source: `ATTESTA_ALLOW_INSECURE_HTTP=1` lets the wallet talk to
issuers and verifiers on plain http, `ATTESTA_WALLET_DATA_DIR` keeps its data somewhere other than the system's per-user app folder,
`ATTESTA_WALLET_KEY` supplies the store key directly, and `ATTESTA_WALLET_PORT` moves the port pages find the wallet on. An installed (packaged) app
ignores all four (`src/main/dev-switches.ts`).

The packaged app ignores the dev switches, so it refuses plain `http`; it only works with https issuers and verifiers. The app is not packed into an asar archive (`asar: false`): Askar looks for its native library next to its own code, and `dlopen` cannot read from inside an archive. Unsigned, Gatekeeper asks you to allow it on a copy that was downloaded; one built here opens normally.

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
