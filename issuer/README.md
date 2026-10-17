# issuer

A credential issuer. It stands for an organization such as a university or a municipality: staff record that a person is entitled to a credential, the person gets a
QR code by mail, and their wallet fetches the signed credential (SD-JWT VC) over OpenID4VCI. The issuer is independent of the Attesta backend; the backend only learns of it
by being configured to trust the issuer's DID.

## The idea in short

```
staff (pages) ──► API records an entitlement ──► mail with QR code ──► person uploads it in the wallet ──► wallet fetches the signed credential
                                                                                                         (OpenID4VCI, works once, 24 hours)
```

1. A staff member fills in a form (an employee, or a diploma) in the pages.
2. The API stores an **entitlement**: the right of the person with that email address to receive one credential.
3. The person is mailed a **QR code**. They save the image and upload it in their wallet, which fetches the credential by itself.
4. Later, staff can **revoke** the credential, optionally replacing it with a corrected one. Verifiers read the issuer's signed **status list** to see what is revoked.

The email address is what identifies the person to verifiers. The issuer's identity is the `did:key` of an Ed25519 key derived from `ATTESTA_ISSUER_SEED`, so the DID stays
the same across restarts and replicas; use another seed than the default outside local development.

## Parts

| Part | What it does |
| --- | --- |
| `api/` (Go, module `attesta/issuer`) | Holds the signing key, the entitlements (in Postgres), the roles, the status list, the mail, and the OpenID4VCI endpoints for wallets under `/oid4vci`. |
| `gui/` (`@attesta/issuer-gui`) | The pages staff use. It has no key and no store; it only calls the API. |

The pages: `/` (welcome), `/employee` and `/type/<path>` (forms), `/roles` (manage roles), `/credential-types` (menu: Credential Forms; define credential types with an editor and preview, one menu item each), `/credentials` (every credential that is waiting or issued and has not ended, in a table to filter, sort and page; revoke one, or change it: it is revoked and the corrected data recorded in its place). `GET /entitlements` lists them, `GET /entitlements?email=` those of one person. Changing a credential never changes its email address (the API refuses it): that is another person, so the credential is revoked and a new one made.

## Credentials

Two kinds, each with a title and description (never asked in a form; the title is a plain, always visible claim):

- **Employee** (`POST /entitlements/employee`): name, email, `department` (`burgerzaken` or `secretariaat`), organisation, and `roles`, a list of names from the table `roles`.
  Titled "Medewerker afdeling & rollen". It has no end date; it ends by being revoked.
- **Credential types** defined by staff, one credential type and one form each; `Diploma Laadpalen Management` (`POST /entitlements/type/laadpalen-management`) is there from the start:
  name, email and the required `validUntil`. The type is the course, so it holds no course claim. A date field named `validUntil` ends the credential at the end of that day, and a date in the past is refused.
  A type is a row of the table `credential_types`: title, description and fields (key, label, kind `text`, `email`, `date`, `choice` or `number`, required, options). The field `email` is required and
  identifies the person. The credential type verifiers ask for is the title at creation and, like the path, does not change; the fields of a type that has a credential not revoked cannot change, and it cannot be deleted.
  A `number` has an optional `min` and `max` (decimal texts) and `decimals` (0 to 6); the claim is the number as text with a point and exactly those decimals, and a comma is accepted as input.
  `GET/POST /credential-types` and `PUT/DELETE /credential-types/{id}` manage them.

Both answer `{ "entitlementId": ..., "mailedTo": ... }`. A verifier that needs an Employee and a diploma asks for both in one request.

### Roles

The roles an employee can be given are rows of the table `roles` (a name without comma, not blank), not code. A deployment starts with `laadpalen-aanvrager`.

- Staff manage them at `/roles`, or through `GET /roles`, `POST /roles`, `PUT /roles/<id>` (rename) and `DELETE /roles/<id>`. The employee form offers a new role at once; the API refuses a role that is not in the table.
- Delete is soft (`deleted_at`), and the name can be used again.
- A role held by an employee credential that is not revoked is in use: deleting it is refused with `409`.
- Renaming is allowed; credentials already issued keep the old name.

## Lifecycle of an entitlement

**Recording.** Recording an entitlement mails the person a QR code, as the image `credential.png`. The code holds a link with a one-time code for that entitlement. It works once and
for 24 hours (`ATTESTA_ISSUER_QR_LIFETIME_SECONDS`).

**One credential per type.** A person has one of each credential type at a time.
- Posting the same data again returns the same entitlement while it is unissued (the database allows one per person, type and data, also at the same moment) and mails a new code.
- While one is waiting with other data, or issued and not ended, a new one is refused (`409`) and nothing is mailed. It has to be revoked first or run out.

**Issuing.** The credential is signed from the entitlement when the wallet asks for it. The issuer claims the entitlement first, so no one else can take it, and marks it issued
once the credential is made. A claim not followed by that runs out after two minutes, and the entitlement can be claimed again until it passes its end or is revoked.

**Expired code.** When a wallet comes with an expired code and the entitlement is still open, the issuer mails a new code and tells the wallet so (`410`, `qr_expired`), at most
one per ten minutes per entitlement (`ATTESTA_ISSUER_RENEWAL_COOLDOWN`). When the credential was already fetched or withdrawn it mails nothing (`qr_gone`).

**Revoking.**
- `POST /entitlements/revoke/<id>` without a body withdraws an entitlement, issued or not; the person may then get a new credential of that type.
- With the corrected data of the credential in the body (the fields of its form) it withdraws the entitlement and records the corrected one in one step, and mails its QR code.
- `GET /entitlements?email=` lists what a person holds.
- Each credential gets a position in a status list when its entitlement is first claimed, and carries it as `status.status_list` (`idx`, `uri`). The list is signed by the issuer's key
  and published, open, at `<ATTESTA_ISSUER_PUBLIC_URL>/status/1` as `application/statuslist+jwt`, with a `ttl` of one minute and an `exp` of one hour; it is never smaller than 16384 positions.

## Who may use it

Only signed-in staff record entitlements. `/info`, `/entitlements` and `/roles` need an access token (`Authorization: Bearer`) from an OpenID Connect server. The token must be
issued to the client `issuer-gui` and carry the role `issuer-staff` (in `realm_access.roles` or `roles`). The wallet endpoints under `/oid4vci`, the status list and `/healthz` need no token.

Staff sign in through the pages (authorization code with PKCE, confidential client). The session lives in the page's memory, so a restart signs everyone out; the access token is
renewed with the refresh token, and "Afmelden" ends the session at the server too.

Without `ATTESTA_ISSUER_OIDC_ISSUER` the API refuses to start. `ATTESTA_ISSUER_AUTH_DISABLED=true` leaves the API and the pages open, for tests and local development only.

## Configuration

| Variable | Meaning |
| --- | --- |
| `ATTESTA_ISSUER_PORT` | API port (4001). |
| `ATTESTA_ISSUER_PUBLIC_URL` | The address wallets reach the API at; it appears in the QR codes and in the issuer metadata. |
| `ATTESTA_ISSUER_SEED` | Derives the signing key and so the DID. |
| `ATTESTA_ISSUER_NAME` | The issuer's name, used in mails and pages. |
| `ATTESTA_ISSUER_DB_HOST`, `_USER`, `_PASSWORD`, `_NAME` | Postgres; `_NAME` defaults to `issuer_entitlements`, which is created when missing. The SQL is in `internal/postgres/migrations/`. |
| `ATTESTA_ISSUER_SMTP_HOST`, `_PORT`, `ATTESTA_ISSUER_MAIL_FROM` | The SMTP server and sender of the mails. |
| `ATTESTA_ISSUER_SMTP_USER`, `_PASSWORD`, `_TLS` | A user means a secured connection: STARTTLS (port 587, the default) or TLS from the start (port 465). The API refuses to send its password over an unsecured one; `_TLS` = `none`, `starttls` or `tls` overrides. |
| `ATTESTA_ISSUER_OIDC_ISSUER`, `_OIDC_DISCOVERY_URL`, `_OIDC_CLIENT_ID`, `_OIDC_ROLE` | The OpenID Connect server: the name in its tokens, where its metadata is reached when that differs, the client (default `issuer-gui`) and the role (default `issuer-staff`). |
| `ATTESTA_ISSUER_OIDC_CLIENT_SECRET`, `ATTESTA_ISSUER_GUI_PUBLIC_URL` | Pages only: the client secret, and where staff reach the pages (the server sends them back there). |
| `ATTESTA_ISSUER_GUI_LISTEN_PORT`, `ATTESTA_ISSUER_API_URL` | Pages only: their port (4000) and where the API is (`http://localhost:4001`). |
| `ATTESTA_ISSUER_QR_LIFETIME_SECONDS`, `ATTESTA_ISSUER_RENEWAL_COOLDOWN` | How long a QR code works, and the minimum time between renewed codes. |

## Run and test

```sh
cd issuer/api && go run ./cmd/issuer      # the API
pnpm --filter @attesta/issuer-gui start   # the pages
cd issuer/api && go test ./...            # needs Docker: Postgres and Mailpit run in containers
pnpm --filter @attesta/issuer-gui test    # the pages against a stand-in API and a stand-in login server
```

## Mail

In the cluster the SMTP settings are one Secret, `attesta-issuer-smtp`, made with
`make k8s-issuer-mail SMTP_HOST=smtp.gmail.com SMTP_USER=<gmail> MAIL_FROM=<gmail>` (Gmail needs 2-Step Verification and an app password). It asks for the password and restarts the API.
The sender must be an address or domain that the mail server allows. `make k8s-issuer-apply` stops until the Secret exists.

## Deploy (Docker Desktop Kubernetes)

`make k8s-issuer-apply` builds both images and deploys them with Postgres and Keycloak (`k8s/local/issuer/`).

- **Sign in** at https://issuer.attesta.corbencreatives.nl as user `staff`. The password is in the Secret `attesta-issuer-auth`:
  `kubectl -n attesta get secret attesta-issuer-auth -o jsonpath='{.data.staff-password}' | base64 -d` (and `admin-password` for Keycloak's console at `/auth`).
  The sign-in works only at the https address, not at http://localhost:4000, because the server sends staff back to the address in `ATTESTA_ISSUER_GUI_PUBLIC_URL`.
- **Keycloak** realm `attesta` comes from the ConfigMap `issuer-keycloak-realm`; users made by hand are gone after a restart. A customer points the issuer at its own server instead and
  needs a client `issuer-gui` (redirect URI `<pages address>/callback`) and the role `issuer-staff`.
- **Ingress** sends `/oid4vci`, `/.well-known`, `/status` and `/auth` on the issuer's address to the API and Keycloak, and everything else to the pages, so recording entitlements needs a sign-in.
- **Postgres** keeps its data on a volume claim that `make k8s-issuer-delete` leaves in place, with the account in the Secret `attesta-issuer-db` (`make k8s-issuer-secret`).
- **Signing key** comes from the seed, so the DID stays the same across restarts.
