# issuer

A demo credential issuer. It stands for an organization such as a university: it signs diploma credentials (SD-JWT VC, type `Diploma`) with its own
key and hands them to a wallet through OpenID4VCI. It is independent of the Attesta backend; the backend only learns of it by being configured to
trust the issuer's DID. It has two parts:

- `api/` (`@attesta/issuer-api`) holds the signing key and the OpenID4VCI endpoints (issuer metadata, token, credential) under `/oid4vci`. Its JSON API
  creates offers: `POST /offers` for a `Diploma`, `POST /employee/offers` for an `Employee` (name, email, `department` of `burgerzaken` or `secretariaat`,
  organisation, diploma course and the date the diploma is valid until; the credential expires at the end of that day, and a date in the past is refused),
  both answering `{ "offerUri": ... }`. `GET /info` tells the pages the issuer's name and DID, the departments and the default descriptions.
  The email is what identifies the person to verifiers. The issuer's identity is a `did:key` created on first start and kept in its store, so the DID is stable across restarts.
- `gui/` (`@attesta/issuer-gui`) serves the pages: `/` and the forms at `/diploma` and `/employee`. A form posts to the page, which passes it to the API and shows the offer link.
  The page has no key and no store; it only calls the API.

```sh
pnpm --filter @attesta/issuer-api start   # ATTESTA_ISSUER_PORT (4001), ATTESTA_ISSUER_PUBLIC_URL, ATTESTA_ISSUER_DIR, ATTESTA_ISSUER_KEY, ATTESTA_ISSUER_NAME
pnpm --filter @attesta/issuer-gui start   # ATTESTA_ISSUER_GUI_LISTEN_PORT (4000), ATTESTA_ISSUER_API_URL (http://localhost:4001)
pnpm --filter @attesta/issuer-api test    # issues credentials to an in-process wallet
pnpm --filter @attesta/issuer-gui test    # the pages against a stand-in API
```

`ATTESTA_ISSUER_PUBLIC_URL` must be the address wallets reach the API at: it appears in every offer and in the issuer metadata.

In Docker Desktop's Kubernetes, `make k8s-issuer-apply` builds both images and deploys them (`k8s/local/issuer/`); the pages are then at http://localhost:4000.
The Ingress sends `/oid4vci` on the issuer's address to the API and everything else to the pages, so creating offers is not reachable from outside the cluster.
The signing key comes from the seed, so the DID stays the same across restarts although the container's store does not.
