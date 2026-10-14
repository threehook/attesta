# issuer

A credential issuer. It stands for an organization such as a university or a municipality: it signs credentials (SD-JWT VC, types `Employee` and
`Diploma Laadpalen Management`) with its own key and hands them to a wallet through OpenID4VCI. It is independent of the Attesta backend; the backend only learns of it by being
configured to trust the issuer's DID. It has two parts:

- `api/` (Go, module `attesta/issuer`) holds the signing key, the entitlements and the OpenID4VCI endpoints (issuer metadata, token, credential, and the link behind each QR code) under `/oid4vci`.
  - An **entitlement** is the right of the person with an email address to receive one credential. `POST /entitlements/employee` records one for an `Employee`
    (name, email, `department` of `burgerzaken` or `secretariaat`, organisation, and `roles` as a list of names from the table `roles`, which the credential holds as a list;
    an Employee has no end date and is withdrawn by revoking it, which is not built yet). Each diploma is a credential type of its own with its own form and address: `POST /entitlements/diploma/laadpalen-management` records one for
    `Diploma Laadpalen Management` (name, email and the required `validUntil`; it holds no course claim, the type is the course). A credential with a `validUntil` expires at
    the end of that day, and a date in the past is refused. Both answer `{ "entitlementId": ..., "mailedTo": ... }`. A verifier that needs an Employee and a diploma asks for
    both in one request.
  - **Title and description are fixed by the credential type** and are not asked in the forms: an Employee is titled "Medewerker afdeling & rollen", a diploma has the title in
    `credential.Diplomas` ("Diploma Laadpalen Management"), and each holder reads a description of what the credential is for. The title is a plain, always visible claim.
    Another diploma is one more entry in `credential.Diplomas` (type, path, title, description); its form and menu item appear by themselves, and the policy that needs it
    names its type.
  - **The roles an employee can be given are rows of the table `roles`** (`name`, no comma, not blank), not code. A deployment starts with `laadpalen-aanvrager` and `reader`; add
    one with `INSERT INTO roles (name) VALUES ('...')` and the form offers it at once. The form is a list to choose from, and the API refuses a role that is not in the table.
  - **Recording an entitlement mails the person a QR code**, as the image `credential.png` in the mail. The code holds a link with a one-time code for that
    entitlement. The person saves the image and uploads it in their wallet, which then fetches the credential by itself. The code works once and
    for 24 hours (`ATTESTA_ISSUER_QR_LIFETIME_SECONDS`).
  - **An expired code is replaced.** When a wallet comes with an expired code and the entitlement is still open, the issuer mails a new code and tells the wallet so
    (`410`, `qr_expired`); at most one per ten minutes per entitlement (`ATTESTA_ISSUER_RENEWAL_COOLDOWN`). When the credential was already fetched or withdrawn it mails nothing (`qr_gone`).
  - An entitlement is issued once. Posting the same data again, also at the same moment, returns the same entitlement while it is unissued (the database allows one per person, type and data) and mails a new code. A person has one of each credential at a time (one of each credential type): while one is waiting with other data, or issued and not ended, a new one of it is
    refused (`409`) and nothing is mailed. It has to be revoked first (revoking is not built yet) or run out. The credential is signed from the entitlement when the
    wallet asks for it: the issuer claims it first, so no one else can take it, and marks it issued once the credential is made. A claim that is not
    followed by that runs out after two minutes, and the entitlement can be claimed again until it passes its end or is revoked.
  - It keeps its entitlements and the sessions of its QR codes in Postgres (`ATTESTA_ISSUER_DB_HOST`, `_USER`, `_PASSWORD`, and `_NAME`, default `issuer_entitlements`, which it
    creates when missing; the SQL is in `internal/postgres/migrations/`). The mail goes to the SMTP server in `ATTESTA_ISSUER_SMTP_HOST`/`_PORT`
    from `ATTESTA_ISSUER_MAIL_FROM`, logging in with `ATTESTA_ISSUER_SMTP_USER`/`_PASSWORD` when a user is set. A user means a secured connection: STARTTLS (port 587, the default) or TLS from the start
    (port 465), and the issuer refuses to send its password over an unsecured one (`ATTESTA_ISSUER_SMTP_TLS` = `none`, `starttls` or `tls` overrides). In the cluster these settings are one Secret,
    `attesta-issuer-smtp`. It starts as Mailpit, a mail catcher whose page is at http://localhost:8025 and which keeps every mail and delivers none. For real delivery run
    `make k8s-issuer-mail SMTP_HOST=<server> SMTP_USER=<login> MAIL_FROM=<sender>`; it asks for the password and restarts the API. The sender must be an address or domain that the mail server allows.
  - The issuer's identity is the `did:key` of an Ed25519 key derived from `ATTESTA_ISSUER_SEED`, so the DID is stable across restarts and replicas; use another seed than the default outside local development.
    The email is what identifies the person to verifiers.
  - **Only signed-in staff record entitlements.** `/info` and `/entitlements/*` need an access token (`Authorization: Bearer`) from an OpenID Connect server
    (`ATTESTA_ISSUER_OIDC_ISSUER`, the name in its tokens; `_OIDC_DISCOVERY_URL` when its metadata is reached at another address; `_OIDC_CLIENT_ID`, default `issuer-gui`).
    The token must be issued to that client and carry the role `issuer-staff` (`_OIDC_ROLE`; in `realm_access.roles` or `roles`). The wallet endpoints under `/oid4vci`
    and `/healthz` need no token. Without `_OIDC_ISSUER` the issuer refuses to start; `ATTESTA_ISSUER_AUTH_DISABLED=true` leaves the endpoints open, for tests and local development only.
- `gui/` (`@attesta/issuer-gui`) serves the pages: `/` and the forms at `/employee` and `/diploma/<path>`. A form posts to the page, which passes it to the API and says that a mail
  with a QR code is on its way. The page has no key and no store; it only calls the API. Staff sign in at the OpenID Connect server (authorization code with PKCE,
  confidential client): `ATTESTA_ISSUER_OIDC_ISSUER`, `_OIDC_DISCOVERY_URL`, `_OIDC_CLIENT_ID`, `_OIDC_CLIENT_SECRET` and `ATTESTA_ISSUER_GUI_PUBLIC_URL` (where staff reach the pages, which
    the server sends them back to). The session lives in the page's memory, so a restart signs everyone out; the access token is renewed with the refresh token, and "Afmelden" ends the session at the server too.
    `ATTESTA_ISSUER_AUTH_DISABLED=true` opens the pages.

```sh
cd issuer/api && go run ./cmd/issuer      # ATTESTA_ISSUER_PORT (4001), ATTESTA_ISSUER_PUBLIC_URL, ATTESTA_ISSUER_SEED, ATTESTA_ISSUER_NAME,
                                          # ATTESTA_ISSUER_DB_HOST/_USER/_PASSWORD/_NAME, ATTESTA_ISSUER_SMTP_HOST/_PORT, ATTESTA_ISSUER_MAIL_FROM,
                                          # ATTESTA_ISSUER_OIDC_ISSUER (or ATTESTA_ISSUER_AUTH_DISABLED=true)
pnpm --filter @attesta/issuer-gui start   # ATTESTA_ISSUER_GUI_LISTEN_PORT (4000), ATTESTA_ISSUER_API_URL (http://localhost:4001), ATTESTA_ISSUER_OIDC_* (or AUTH_DISABLED=true)
cd issuer/api && go test ./...            # needs Docker: Postgres and Mailpit run in containers
pnpm --filter @attesta/issuer-gui test    # the pages against a stand-in API and a stand-in login server
```

`ATTESTA_ISSUER_PUBLIC_URL` must be the address wallets reach the API at: it appears in the QR codes and in the issuer metadata.

In Docker Desktop's Kubernetes, `make k8s-issuer-apply` builds both images and deploys them with Postgres, Mailpit and Keycloak (`k8s/local/issuer/`). Staff sign in at https://issuer.attesta.corbencreatives.nl
as user `staff`; the password is in the Secret `attesta-issuer-auth` (`kubectl -n attesta get secret attesta-issuer-auth -o jsonpath='{.data.staff-password}' | base64 -d`, and `admin-password` for Keycloak's console at `/auth`).
Keycloak's realm `attesta` comes from the ConfigMap `issuer-keycloak-realm`; users made by hand are gone after a restart. A customer points the issuer at its own server instead and needs a client `issuer-gui`
(redirect URI `<pages address>/callback`) and the role `issuer-staff`. The sign-in works only at the https address, not at http://localhost:4000, because the server sends staff back to the address in `ATTESTA_ISSUER_GUI_PUBLIC_URL`.
The Ingress sends `/oid4vci`, `/.well-known` and `/auth` on the issuer's address to the API and Keycloak and everything else to the pages, so recording entitlements needs a sign-in.
The Postgres deployment keeps its data on a volume claim that `make k8s-issuer-delete` leaves in place, with the account in the Secret `attesta-issuer-db` (`make k8s-issuer-secret`).
The signing key comes from the seed, so the DID stays the same across restarts.
