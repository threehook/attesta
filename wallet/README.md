# wallet

The wallet core: a [Credo](https://github.com/openwallet-foundation/credo-ts) holder agent that accepts OpenID4VCI credential offers and stores
SD-JWT VC credentials in an encrypted Askar store (SQLite). It has no user interface and imports nothing from Electron; `wallet-desktop` is the desktop app around
it.

- `createWalletAgent({ storeId, storeKey, path })` opens the store, or keeps it in memory when `path` is omitted.
- `previewCredentialOffer(agent, offerUri)` reads an offer and reports who offers what without accepting it; `acceptPreviewedOffer` then accepts it.
- `acceptCredentialOffer(agent, offerUri)` does both in one step, running the pre-authorized code flow and binds each credential to a fresh key held in the agent's key store.
- `listCredentials(agent)` returns the held credentials with their type, issuer and claims.
- `resolvePresentationRequest(agent, link)` reads an `openid4vp://` request from a verifier and reports who asks for what and whether the wallet can answer;
  `submitPresentation(agent, request)` answers it, disclosing only the requested claims.

## Headless use

```sh
export ATTESTA_WALLET_KEY=<key that encrypts the store>   # ATTESTA_WALLET_DIR defaults to ~/.attesta-wallet
pnpm cli accept '<offer uri from the issuer>'            # ATTESTA_ALLOW_INSECURE_HTTP=1 for an issuer on plain http
pnpm cli present '<openid4vp link from an application>'
pnpm cli list
```

Askar's native library is downloaded by an install script that `pnpm-workspace.yaml` allows for `@openwallet-foundation/askar-nodejs`.
