# Configuration

Every component is configured through environment variables. A blank value counts as not set. Lists are separated by commas.

In Kubernetes, `enableServiceLinks: false` on a pod keeps Kubernetes from injecting `<SERVICE>_PORT` and `<SERVICE>_HOST` variables that can collide with these
names; do not name a setting after a Service plus `_PORT` or `_HOST`.

- [Attesta backend](#attesta-backend)
- [Issuer API](#issuer-api)
- [Issuer pages](#issuer-pages)
- [Wallet of a system](#wallet-of-a-system)
- [Wallets for development](#wallets-for-development)
- [Examples](#examples)
- [Azure environment](#azure-environment)

## Attesta backend

| Variable | Default | Meaning |
|---|---|---|
| `ATTESTA_ADDR` | `:8080` | Address the HTTP server listens on |
| `ATTESTA_PUBLIC_URL` | `http://localhost:8080` | Where wallets reach this backend. Every presentation request carries it, so it must be the address as the wallet sees it |
| `ATTESTA_ADMIN_TOKEN` | `dev-only-insecure-admin-token` | Token for `/admin/*`, sent as the header `X-Admin-Token`. The default is for local development only |
| `ATTESTA_CORS_ORIGINS` | `http://localhost:5173` | Browser origins that may call the API |
| `ATTESTA_POLICIES_DIR` | empty | Directory with one `<policy id>.rego` per policy. Without it the backend starts with no policies |
| `ATTESTA_STATUS_URL_PREFIXES` | empty | URLs, each ending in `/`, that credential status lists may be read from. With none set every credential is denied |
| `ATTESTA_STATUS_HOSTS` | empty | `https://public=http://internal` pairs, for a status list that is reached at another address than its public one |

Decision log, one record per decision ([ADL](https://gitdocumentatie.logius.nl/publicatie/ftv/adl/1.0.0/) Level 1; the record is described in `backend/internal/adl/adl.md`):

| Variable | Default | Meaning |
|---|---|---|
| `ATTESTA_ADL_OUTPUT` | `stdout` | Where records go: any of `stdout`, `file` and `otlp`, separated by commas |
| `ATTESTA_ADL_FILE` | empty | Path of the record file for `file`; each record is synced to disk before the decision is returned |
| `ATTESTA_ADL_OTLP_ENDPOINT` | empty | Address of an OTLP/gRPC collector for `otlp`, such as `alloy.observability.svc.cluster.local:4317` |
| `ATTESTA_ADL_OTLP_INSECURE` | `false` | `true` sends to the collector without TLS |
| `ATTESTA_ADL_RESOURCE` | empty | `key=value` pairs that name the producer of the records. `service.name` defaults to `attesta-adl` and `instance_id` to the host name |

### Policies

The backend reads `ATTESTA_POLICIES_DIR` at start and again every 30 seconds. It applies what changed without a restart: a new or changed file is validated and
installed, a file that is gone takes its policy with it, and an invalid file is rejected (logged once) while the previous version stays in use. The policy id is
the file name without `.rego`. In Kubernetes the directory is a mounted ConfigMap; Kubernetes updates the mounted files within about a minute, so a change is
live within about 90 seconds.

`POST /admin/policies` with `{"id": "...", "source": "..."}` deploys one policy at once. It is held in memory until the backend restarts, and the directory
synchronisation does not remove it.

## Issuer API

All names start with `ATTESTA_ISSUER_`.

| Variable | Default | Meaning |
|---|---|---|
| `PORT` | `4001` | Port to listen on |
| `PUBLIC_URL` | `http://localhost:<PORT>` | Where wallets reach the issuer; it is in the QR codes and the issuer's metadata |
| `SEED` | dev seed | Derives the issuer's signing key and so its DID. The default is for local development only, and the policies that trust an issuer name its DID |
| `NAME` | `Gemeente Vlierdam` | The issuer's name, written into every credential for display |
| `QR_LIFETIME_SECONDS` | `86400` | How long a mailed QR code works |
| `RENEWAL_COOLDOWN` | `10m` | Least time between two new QR codes asked for an expired one |
| `DB_HOST` | required | Postgres, `host` or `host:port` |
| `DB_USER`, `DB_PASSWORD`, `DB_NAME` | `issuer`, empty, `issuer_entitlements` | The database is created when it is missing |
| `SMTP_HOST`, `SMTP_PORT` | `localhost`, `1025` | The mail server |
| `SMTP_USER`, `SMTP_PASSWORD` | empty | Credentials for the mail server |
| `SMTP_TLS` | empty | `none`, `starttls` or `tls`; empty uses STARTTLS when the server offers it |
| `MAIL_FROM` | `credentials@attesta.local` | Sender of the mails |
| `OIDC_ISSUER` | required | The OpenID Connect server staff sign in at. The API checks their token |
| `OIDC_DISCOVERY_URL` | empty | Where the server's metadata is fetched; empty means `OIDC_ISSUER`. Differs when that address is only reachable from outside |
| `OIDC_CLIENT_ID` | `issuer-gui` | Client id of the pages |
| `OIDC_ROLE` | `issuer-staff` | Role that may record and revoke credentials |
| `OIDC_APPROVER_ROLE` | `release-manager` | Role of the people who approve mandates |
| `AUTH_DISABLED` | empty | `true` leaves the API open instead of requiring `OIDC_ISSUER`; for tests and local development only |

Systems (unattended authorization) are switched on with a trust domain:

| Variable | Default | Meaning |
|---|---|---|
| `SPIFFE_TRUST_DOMAIN` | empty | The only trust domain whose workloads are accepted. Empty leaves the endpoints for workloads out |
| `SPIFFE_SOCKET` | empty | SPIFFE Workload API address the trust bundle is read from; empty uses `SPIFFE_ENDPOINT_SOCKET` |
| `SPIFFE_AUDIENCE` | `PUBLIC_URL` | What a workload's JWT-SVID must name |
| `WORKLOAD_LIFETIME` | `1h` | How long a workload's credential lasts |
| `APPROVALS_REQUIRED` | `2` | How many different people must approve a parked request |
| `APPROVAL_REQUEST_LIFETIME` | `24h` | How long a parked request waits for approval |
| `APPROVAL_LIFETIME` | `15m` | How long the Approval credential lasts |
| `APPROVAL_NOTIFY` | empty | Addresses that get a mail for a new request |

## Issuer pages

| Variable | Default | Meaning |
|---|---|---|
| `ATTESTA_ISSUER_GUI_LISTEN_PORT` | `4000` | Port to listen on |
| `ATTESTA_ISSUER_API_URL` | `http://localhost:4001` | The issuer API the pages call |
| `ATTESTA_ISSUER_OIDC_ISSUER` | required | The OpenID Connect server staff sign in at |
| `ATTESTA_ISSUER_OIDC_DISCOVERY_URL` | empty | As `OIDC_DISCOVERY_URL` of the API |
| `ATTESTA_ISSUER_OIDC_CLIENT_ID` | `issuer-gui` | Client id; the client of the same name must exist at the server |
| `ATTESTA_ISSUER_OIDC_CLIENT_SECRET` | required | Client secret |
| `ATTESTA_ISSUER_GUI_PUBLIC_URL` | required | Address of the pages as the browser sees it |
| `ATTESTA_ISSUER_AUTH_DISABLED` | empty | `true` switches sign-in off, and the sign-in settings above are then not needed; for tests and local development only |
| `ATTESTA_BACKEND_URL` | empty | An Attesta backend, for the Policy Editor and the Policy Tester. Without it they cannot check policies |
| `ATTESTA_BACKEND_ADMIN_TOKEN` | required with `ATTESTA_BACKEND_URL` | The backend's `ATTESTA_ADMIN_TOKEN` |

## Wallet of a system

The headless wallet `wallet/src/serve-cli.ts` (`pnpm --filter @attesta/wallet serve`) for a pod or a job. It keeps its credentials in a temporary folder, and
listens on `127.0.0.1` only.

| Variable | Default | Meaning |
|---|---|---|
| `ATTESTA_WALLET_PORT` | `7100` | Port to listen on |
| `ATTESTA_WALLET_VERIFIERS` | required | The client ids it answers, such as `redirect_uri:http://127.0.0.1:8080/`, separated by commas |
| `ATTESTA_WALLET_HOSTS` | empty | `https://public=http://internal` pairs: what is sent to a public address goes to the internal one |
| `ATTESTA_ALLOW_INSECURE_HTTP` | empty | `1` accepts issuers and verifiers over plain http |

The wallet's key stays in a key service and only signatures come out. Without one of the two below the wallet keeps its key in its own store.

**OpenBao** (Transit engine):

| Variable | Meaning |
|---|---|
| `ATTESTA_WALLET_KMS_URL` | Address of OpenBao. Switches it on |
| `ATTESTA_WALLET_KMS_TOKEN` | Token that may create keys and sign |
| `ATTESTA_WALLET_KMS_PREFIX` | The keys are named `<prefix>-<id>`; at start the wallet deletes the keys under its prefix, which belong to a wallet that is gone |

**Azure Key Vault:** the wallet signs in with the pod's Workload Identity (the `AZURE_*` variables that AKS adds to a pod with the label
`azure.workload.identity/use: "true"`), or as the developer after `az login`. Setting both OpenBao and Key Vault is an error.

| Variable | Meaning |
|---|---|
| `ATTESTA_WALLET_AZURE_KEY_VAULT_URL` | Address of the vault. Switches it on |
| `ATTESTA_WALLET_AZURE_KEY_MODE` | Required. `create`: a new P-256 key at every start, named `<prefix>-<id>`, and the keys of an earlier wallet under the prefix are deleted; the identity needs the role `Key Vault Crypto Officer`. `provisioned`: one existing key that the wallet only signs with and never creates or deletes; the identity needs `Key Vault Crypto User` |
| `ATTESTA_WALLET_KMS_PREFIX` | Required for `create`: the start of the key names |
| `ATTESTA_WALLET_AZURE_KEY_NAME` | Required for `provisioned`: the name of the key |

## Wallets for development

| Variable | Used by | Meaning |
|---|---|---|
| `ATTESTA_WALLET_KEY` | wallet CLI (required), desktop wallet | Key that encrypts the wallet's store. The desktop wallet otherwise keeps it in the system keychain |
| `ATTESTA_WALLET_DIR` | wallet CLI | Data folder; default `~/.attesta-wallet` |
| `ATTESTA_WALLET_DATA_DIR` | desktop wallet | Replaces its data folder |
| `ATTESTA_WALLET_PORT` | desktop wallet | Replaces the port on which pages find the wallet (default `47653`) |
| `ATTESTA_ALLOW_INSECURE_HTTP` | both | `1` accepts plain http |

The desktop wallet honours its switches only when it runs from source. An installed (packaged) app ignores them, so nothing that can set its environment can
change where it keeps its data, which key protects it or which servers it trusts over http. A packaged wallet refuses plain http.

## Examples

**Laadpalen app** (`examples/laadpalen/api`):

| Variable | Default | Meaning |
|---|---|---|
| `LAADPALEN_ADDR` | `:8787` | Address to listen on |
| `ATTESTA_URL` | `http://127.0.0.1:8080` | The Attesta beside it |
| `ATTESTA_PUBLIC_URL` | empty | Where wallets answer; the origin of this address names the application to the wallet |

**CI/CD agent** (`examples/cicd-agent`):

| Variable | Default | Meaning |
|---|---|---|
| `CICD_PORT` | `8090` | Port to listen on |
| `CICD_AGENT_TOKEN` | empty | When set, `POST /check` and `POST /tasks` need it as a bearer token |
| `ANTHROPIC_API_KEY` | empty | The model key; `/tasks` needs it, `/check` does not |
| `ATTESTA_URL` | `http://127.0.0.1:8080` | The Attesta beside it |
| `CICD_POLICY_ID` | `cicd` | The policy it asks |
| `CICD_WALLET_URL` | `http://127.0.0.1:7100` | Its wallet |
| `CICD_ISSUER_URL` | `http://attesta-issuer-api:4001` | The issuer it gets its credential from |
| `CICD_SPIFFE_AUDIENCE` | `attesta-issuer` | Audience of the JWT-SVID it asks SPIRE for |
| `SPIFFE_ENDPOINT_SOCKET` | none | SPIFFE Workload API address |
| `CICD_CREDENTIAL_RENEW_SECONDS` | `1800` | After how long it asks for a new credential |
| `CICD_APPROVAL_WAIT_SECONDS` | `0` | How long it waits for people to approve a parked action; at 0 it answers Pending |

## Azure environment

The Azure infrastructure is in `k8s/cloud/azure` (Terraform, state in HCP Terraform workspace `attesta-azure-tst`). An environment is a file
`k8s/cloud/azure/environments/<name>.tfvars`; `tst` is the test environment. Resources are named `<type>-<environment>-<application>`, such as `aks-tst-attesta`.

| Setting | Meaning |
|---|---|
| `environment` | Short name of the environment, such as `tst` or `prd` (required) |
| `application` | Default `attesta` |
| `subscription_id`, `location` | The Azure subscription, and `westeurope` by default |
| `node_count`, `node_size` | The cluster's nodes; default 1 × `Standard_B2s` |
| `api_allowed_cidrs` | Who may reach the cluster's API server, such as `["203.0.113.7/32"]` (required) |
| `key_vault_name` | Name of the Key Vault, unique in all of Azure (required) |
| `key_vault_allowed_cidrs` | Addresses besides the cluster that may reach the vault; default none |
| `key_vault_purge_protection`, `key_vault_soft_delete_days` | Keep a deleted vault and its keys, and for how many days (7 to 90) (required) |
| `key_vault_purge_on_destroy` | Purge the vault when it is destroyed so that its name is free again; cannot be combined with purge protection (required) |
| `wallet_key_mode` | `create` or `provisioned`, as for the wallet above. `create` gives the wallet's identity `Key Vault Crypto Officer` on the vault; `provisioned` makes the key `wallet_key_name` and gives the identity `Key Vault Crypto User` on that key only (required) |
| `wallet_key_name`, `wallet_namespace`, `wallet_service_account` | Key name (`cicd-agent`) and the service account (`attesta/cicd-agent`) that the wallet's identity is tied to |

The values that are not in the repository come from repository variables, which the workflow reads as `TF_VAR_*` and `make azure-apply` and `make azure-destroy`
read with `gh variable get`:

| Repository variable | Meaning |
|---|---|
| `API_ALLOWED_CIDRS` | JSON list for `api_allowed_cidrs` |
| `KEY_VAULT_ALLOWED_CIDRS` | JSON list for `key_vault_allowed_cidrs`; `[]` when not set |
| `AZURE_CLIENT_ID`, `AZURE_TENANT_ID`, `AZURE_SUBSCRIPTION_ID` | The Entra app that the workflow signs in as (OIDC, no secret); `k8s/cloud/azure/bootstrap.sh` makes it |

The secret `TF_API_TOKEN` is the HCP Terraform token. Applying runs behind the required reviewer of the GitHub environment `azure-tst`.

`make` settings: `AZURE_ENV` (default `tst`) chooses the environment file for `azure-apply` and `azure-destroy`, and `TAG=<git sha>` chooses the images that
`azure-backend-apply` and `azure-cicd-apply` deploy.
