# wallet

The wallet core: a [Credo](https://github.com/openwallet-foundation/credo-ts) holder agent that takes credentials from OpenID4VCI links, such as the QR code an issuer mails, and stores
SD-JWT VC credentials in an encrypted Askar store (SQLite). It has no user interface and imports nothing from Electron; `wallet-desktop` is the desktop app around
it.

- `createWalletAgent({ storeId, storeKey, path })` opens the store, or keeps it in memory when `path` is omitted (a store in memory is lost 30 minutes after it opens, so use a `path` for a wallet that runs longer).
- `previewCredential(agent, link)` reads a link and reports who gives what without taking it; `acceptPreviewed` then takes it.
- `acceptCredential(agent, link)` does both in one step, running the pre-authorized code flow and binds each credential to a fresh key held in the agent's key store.
- `readQrImage(image)` reads the text of the QR code in a PNG or JPEG image: the link to a credential, as an issuer mails it. It throws a readable message when the image holds no QR code.
- `listCredentials(agent)` returns the held credentials with their type, issuer and claims.
- `resolvePresentationRequest(agent, link)` reads an `openid4vp://` request from a verifier and reports who asks for what and whether the wallet can answer;
  `submitPresentation(agent, request)` answers it, disclosing only the requested claims.

## The wallet of a system

`pnpm serve` (`src/serve-cli.ts`) is the wallet of a system such as a pod or a pipeline job. It keeps its credentials in a temporary folder, encrypted with a key that only exists in memory, with their keys made at start (in OpenBao when `ATTESTA_WALLET_KMS_URL`, `ATTESTA_WALLET_KMS_TOKEN` and `ATTESTA_WALLET_KMS_PREFIX` are set: P-256 keys that sign there, named after the prefix, and the keys of an earlier wallet with that prefix are deleted at start), and answers by itself: nobody
confirms anything. It listens on 127.0.0.1 only.

| Call | What it does |
| --- | --- |
| `POST /credentials` `{ "offer": "openid-credential-offer://..." }` | Takes the credential the link gives and replaces the held one of the same type from the same issuer, even while that still counts. Answers the type and issuer, not the claims. |
| `POST /present` `{ "link": "openid4vp://..." }` | Answers the request, disclosing only the claims asked for. Only for a verifier whose client id starts with one of `ATTESTA_WALLET_VERIFIERS` (comma-separated, such as `redirect_uri:http://127.0.0.1:8080/`); with none set the wallet does not start. 403 for any other verifier, 409 when it holds nothing that answers. |
| `GET /healthz` | How many credentials it holds. |

`ATTESTA_WALLET_PORT` (7100), `ATTESTA_ALLOW_INSECURE_HTTP=1` for a verifier or issuer on plain http, and `ATTESTA_WALLET_HOSTS` (`https://public.example=http://internal:4001,...`) for a public
name that does not resolve inside the cluster: what is meant for the first goes to the second.

## Headless use

```sh
export ATTESTA_WALLET_KEY=<key that encrypts the store>   # ATTESTA_WALLET_DIR defaults to ~/.attesta-wallet
pnpm cli accept '<link from the issuer>'            # ATTESTA_ALLOW_INSECURE_HTTP=1 for an issuer on plain http
pnpm cli present '<openid4vp link from an application>'
pnpm cli list
```

Askar's native library is downloaded by an install script that `pnpm-workspace.yaml` allows for `@openwallet-foundation/askar-nodejs`.
