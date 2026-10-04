# issuer

A demo credential issuer. It stands for an organization such as a university: it signs diploma credentials (SD-JWT VC, type `Diploma`) with its own
key and hands them to a wallet through OpenID4VCI. It is independent of the zk-puoi backend; the backend only learns of it by being configured to
trust the issuer's DID.

- `GET /` is a form for creating a credential offer (name, email, degree, university); `POST /offers` (JSON or form) does the same and returns the offer link
  as `{ "offerUri": ... }`. The email is what identifies the person to verifiers.
- The OpenID4VCI endpoints (issuer metadata, token, credential) are served under `/oid4vci`.
- The issuer's identity is a `did:key` created on first start and kept in its store, so the DID is stable across restarts.

```sh
pnpm start      # ZKPUOI_ISSUER_PORT (4000), ZKPUOI_ISSUER_PUBLIC_URL, ZKPUOI_ISSUER_DIR, ZKPUOI_ISSUER_KEY
pnpm test       # issues credentials to an in-process wallet
```

`ZKPUOI_ISSUER_PUBLIC_URL` must be the address wallets reach the issuer at: it appears in every offer and in the issuer metadata.
