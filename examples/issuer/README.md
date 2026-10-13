# issuer

A demo credential issuer. It stands for an organization such as a university: it signs diploma credentials (SD-JWT VC, type `Diploma`) with its own
key and hands them to a wallet through OpenID4VCI. It is independent of the Attesta backend; the backend only learns of it by being configured to
trust the issuer's DID. It has these parts:

- `api/` (`@attesta/issuer-api`) holds the signing key and the OpenID4VCI endpoints (issuer metadata, token, credential) under `/oid4vci`. Its JSON API
  creates offers: `POST /offers` for a `Diploma`, `POST /employee/offers` for an `Employee` (name, email, `department` of `burgerzaken` or `secretariaat`,
  organisation, diploma course and the date the diploma is valid until; the credential expires at the end of that day, and a date in the past is refused),
  both answering `{ "offerUri": ..., "entitlementId": ... }`. A request records an entitlement for the person's email; the offer holds no claims and only leads the wallet
  to that entitlement. Posting the same form again gives a new offer to the same entitlement. The credential is signed from the entitlement when the wallet asks for it: the issuer claims
  it first, so no one else can take it, and marks it issued when the library reports the credential made. A claim that is not followed by that runs out after two minutes
  (`entitlementLeaseMs`), and the entitlement can be claimed again until it passes its end or is revoked. `GET /info` tells the pages the issuer's name and DID, the departments and the default descriptions.
  The email is what identifies the person to verifiers. The issuer's identity is a `did:key` created on first start and kept in its store, so the DID is stable across restarts.
  The API keeps its store and its entitlements (`src/entitlements.ts`, the SQL in `migrations/`) in Postgres when `ATTESTA_ISSUER_DB_HOST` is set. Without it they live in `ATTESTA_ISSUER_DIR`
  (SQLite) and in memory, which loses them when the API stops.
- `login/` (`@attesta/issuer-login`) is the authorization server holders log in at. The holder types their email address and gets a link by mail; opening it shows a button, and pressing
  it sends the browser back to the wallet. It gives the issuer a signed result with the email in it, and keeps its requests in its own Postgres database (`issuer_login`).
  It limits the mails per request and per address. The issuer API chains to it, so a wallet can ask for its credential itself: the issuer makes a session when the wallet asks, and signs
  the credential of the open entitlement for the email that logged in (`ATTESTA_ISSUER_LOGIN_URL` switches this on). The mails go to an SMTP server; in the cluster that is Mailpit,
  whose web page is at http://localhost:8025. The login server's address in the issuer metadata is internal (`http://issuer-login:4002/login`), because the pods cannot reach the public names.
- `pg/` (`@attesta/issuer-pg`) is what the API and the login server share to use Postgres: creating a database and applying SQL migrations.
- `gui/` (`@attesta/issuer-gui`) serves the pages: `/` and the forms at `/diploma` and `/employee`. A form posts to the page, which passes it to the API and shows the offer link.
  The page has no key and no store; it only calls the API.

```sh
pnpm --filter @attesta/issuer-api start   # ATTESTA_ISSUER_PORT (4001), ATTESTA_ISSUER_PUBLIC_URL, ATTESTA_ISSUER_DIR, ATTESTA_ISSUER_KEY, ATTESTA_ISSUER_NAME,
                                          # ATTESTA_ISSUER_DB_HOST (host:port), ATTESTA_ISSUER_DB_USER, ATTESTA_ISSUER_DB_PASSWORD
pnpm --filter @attesta/issuer-login start # ATTESTA_LOGIN_PORT (4002), ATTESTA_LOGIN_PUBLIC_URL, ATTESTA_LOGIN_ISSUER, ATTESTA_LOGIN_CLIENT_ID/_SECRET, ATTESTA_LOGIN_REDIRECT_URIS,
                                          # ATTESTA_LOGIN_SEED, ATTESTA_LOGIN_DB_HOST/_USER/_PASSWORD, ATTESTA_LOGIN_SMTP_HOST/_PORT, ATTESTA_LOGIN_MAIL_FROM
pnpm --filter @attesta/issuer-gui start   # ATTESTA_ISSUER_GUI_LISTEN_PORT (4000), ATTESTA_ISSUER_API_URL (http://localhost:4001)
pnpm --filter @attesta/issuer-api test    # issues credentials to an in-process wallet; with ATTESTA_TEST_POSTGRES=host:port (and _USER, _PASSWORD) it also runs against Postgres
pnpm --filter @attesta/issuer-gui test    # the pages against a stand-in API
```

`ATTESTA_ISSUER_PUBLIC_URL` must be the address wallets reach the API at: it appears in every offer and in the issuer metadata.

In Docker Desktop's Kubernetes, `make k8s-issuer-apply` builds both images and deploys them (`k8s/local/issuer/`); the pages are then at http://localhost:4000.
The Ingress sends `/oid4vci` on the issuer's address to the API, `/login` to the login server and everything else to the pages, so creating offers is not reachable from outside the cluster.
The signing key comes from the seed, so the DID stays the same across restarts. The Postgres deployment (`k8s/local/issuer/postgres`) keeps its data on a volume
claim that `make k8s-issuer-delete` leaves in place, with the account in the Secret `attesta-issuer-db` (`make k8s-issuer-secret`).
