# issuer

A demo credential issuer. It stands for an organization such as a university or a municipality: it signs credentials (SD-JWT VC, types `Diploma` and
`Employee`) with its own key and hands them to a wallet through OpenID4VCI. It is independent of the Attesta backend; the backend only learns of it by being
configured to trust the issuer's DID. It has two parts:

- `api/` (`@attesta/issuer-api`) holds the signing key, the entitlements and the OpenID4VCI endpoints (issuer metadata, token, credential, and the link behind each QR code) under `/oid4vci`.
  - An **entitlement** is the right of the person with an email address to receive one credential. `POST /entitlements/diploma` records one for a `Diploma`,
    `POST /entitlements/employee` for an `Employee` (name, email, `department` of `burgerzaken` or `secretariaat`, organisation, diploma course and the date the
    diploma is valid until; the credential expires at the end of that day, and a date in the past is refused). Both answer `{ "entitlementId": ..., "mailedTo": ... }`.
  - **Recording an entitlement mails the person a QR code**, as an image in the mail and as the attachment `credential.png`. The code holds a link with a one-time code for that
    entitlement. The person saves the image and uploads it in their wallet, which then fetches the credential by itself. The code works once and
    for 24 hours (`ATTESTA_ISSUER_QR_LIFETIME_SECONDS`).
  - **An expired code is replaced.** When a wallet comes with an expired code and the entitlement is still open, the issuer mails a new code and tells the wallet so
    (`410`, `qr_expired`); at most one per ten minutes per entitlement. When the credential was already fetched or withdrawn it mails nothing (`qr_gone`).
  - An entitlement is issued once. Posting the same data again returns the same entitlement while it is open and mails a new code; for a holder who lost their
    credential, the organisation posts the form again after the first was issued, which makes a new entitlement. The credential is signed from the entitlement when the
    wallet asks for it: the issuer claims it first, so no one else can take it, and marks it issued when the library reports the credential made. A claim that is not
    followed by that runs out after two minutes (`entitlementLeaseMs`), and the entitlement can be claimed again until it passes its end or is revoked.
  - It keeps its store and its entitlements (`src/entitlements.ts`, the SQL in `migrations/`) in Postgres when `ATTESTA_ISSUER_DB_HOST` is set (`_USER`, `_PASSWORD`); without it
    they live in `ATTESTA_ISSUER_DIR` (SQLite) and in memory, which loses them when the API stops. The mail goes to the SMTP server in `ATTESTA_ISSUER_SMTP_HOST`/`_PORT`
    from `ATTESTA_ISSUER_MAIL_FROM`; in the cluster that is Mailpit, a mail catcher whose page is at http://localhost:8025.
  - The issuer's identity is a `did:key` created on first start and kept in its store, so the DID is stable across restarts. The email is what identifies the person to verifiers.
- `gui/` (`@attesta/issuer-gui`) serves the pages: `/` and the forms at `/diploma` and `/employee`. A form posts to the page, which passes it to the API and says that a mail
  with a QR code is on its way. The page has no key and no store; it only calls the API.

```sh
pnpm --filter @attesta/issuer-api start   # ATTESTA_ISSUER_PORT (4001), ATTESTA_ISSUER_PUBLIC_URL, ATTESTA_ISSUER_DIR, ATTESTA_ISSUER_KEY, ATTESTA_ISSUER_NAME,
                                          # ATTESTA_ISSUER_DB_HOST/_USER/_PASSWORD, ATTESTA_ISSUER_SMTP_HOST/_PORT, ATTESTA_ISSUER_MAIL_FROM
pnpm --filter @attesta/issuer-gui start   # ATTESTA_ISSUER_GUI_LISTEN_PORT (4000), ATTESTA_ISSUER_API_URL (http://localhost:4001)
pnpm --filter @attesta/issuer-api test    # gets credentials into an in-process wallet through the QR codes; with ATTESTA_TEST_POSTGRES=host:port (and _USER, _PASSWORD) it also runs against Postgres
pnpm --filter @attesta/issuer-gui test    # the pages against a stand-in API
```

`ATTESTA_ISSUER_PUBLIC_URL` must be the address wallets reach the API at: it appears in the QR codes and in the issuer metadata.

In Docker Desktop's Kubernetes, `make k8s-issuer-apply` builds both images and deploys them with Postgres and Mailpit (`k8s/local/issuer/`); the pages are then at http://localhost:4000.
The Ingress sends `/oid4vci` on the issuer's address to the API and everything else to the pages, so recording entitlements is not reachable from outside the cluster except through the page's forms.
The Postgres deployment keeps its data on a volume claim that `make k8s-issuer-delete` leaves in place, with the account in the Secret `attesta-issuer-db` (`make k8s-issuer-secret`).
The signing key comes from the seed, so the DID stays the same across restarts.
