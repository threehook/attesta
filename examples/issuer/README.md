# issuer

A demo credential issuer. It stands for an organization such as a university: it signs diploma credentials (SD-JWT VC, type `Diploma`) with its own
key and hands them to a wallet through OpenID4VCI. It is independent of the attesta backend; the backend only learns of it by being configured to
trust the issuer's DID.

- `GET /` is a form for creating a credential offer (name, email, degree, university); `POST /offers` (JSON or form) does the same and returns the offer link
  as `{ "offerUri": ... }`. The email is what identifies the person to verifiers.
- `GET /employee` and `POST /employee/offers` do the same for an `Employee` credential: an employee of an organisation (name, email, `department` of
  `burgerzaken` or `secretariaat`, organisation, diploma course and the date the diploma is valid until). The credential expires at the end of that day, and
  a date in the past is refused. Both credential types are signed with the same DID.
- The OpenID4VCI endpoints (issuer metadata, token, credential) are served under `/oid4vci`.
- The issuer's identity is a `did:key` created on first start and kept in its store, so the DID is stable across restarts.

```sh
pnpm start      # ATTESTA_ISSUER_PORT (4000), ATTESTA_ISSUER_PUBLIC_URL, ATTESTA_ISSUER_DIR, ATTESTA_ISSUER_KEY
pnpm test       # issues credentials to an in-process wallet
```

`ATTESTA_ISSUER_PUBLIC_URL` must be the address wallets reach the issuer at: it appears in every offer and in the issuer metadata.

In Docker Desktop's Kubernetes, `make k8s-issuer-apply` builds the image and deploys it (`k8s/local/issuer/`); the form is then at http://localhost:4000, which is
also the address written into every offer. The signing key comes from the seed, so the DID stays the same across restarts although the container's store does not.

