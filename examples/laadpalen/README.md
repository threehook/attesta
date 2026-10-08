# laadpalen

A relying application with attesta as its **sidecar**. Municipality employees submit requests for a laadpaal (EV charging point) for a citizen's address.
Who may submit is attesta's decision, made from a credential in the employee's own wallet; what becomes of the request is the application's own business.

## What's here

- `api/` is the application's backend (Go, standard library only). The page calls it and nothing else. It asks the sidecar next to it who the employee
  is (sign-in) and whether they may submit, and then applies its own address rules (unknown address, a laadpaal already there, no electric vehicle).
  It records every decided request with the employee's `subject` (issuer and email).
- `gui/` is the page (Vite + React), in Dutch. The employee signs in, enters an address and sees the outcome; the page hands each step to the wallet itself.
- `policies/sign_in.gno` signs in an `Employee` credential from a trusted issuer; nothing but the identity is read.
- `policies/request_laadpaal.gno` decides a request: an `Employee` credential from a trusted issuer, for the `burgerzaken` department and
  a `laadpalen-management` diploma, and the role `laadpalen-aanvrager`, which this app gives an employee (`LAADPALEN_USER_ROLES`) and sends to attesta
  in the `Att-User-Roles` header. Secretariaat employees are refused. The credential's own expiry (the diploma's last day) is checked before the policy runs.

## How it fits together

```
browser ──► page (nginx) ──► laadpalen-api ──► attesta (sidecar, 127.0.0.1:8080)      one pod: laadpalen-api
                                                    ▲
wallet ─────────────────────────────────────────────┘ :8080 (wallets: https://api.attesta.corbencreatives.nl)
```

1. **Sign in.** The page asks the wallet on this machine for the user's identities (email and issuer DID) and shows them in a dropdown;
   `GET /api/config` gives the origin the wallet knows this app by. "Aanmelden" posts to `POST /api/sign-in`. The backend asks attesta for a request
   (`sign_in` for both `policyId` and `resource`, credential `Employee`, no claims beyond the email every request asks) and returns the `openid4vp://`
   link. The page adds the chosen identity to it (`login_hint`, `issuer_hint`) and opens it, so the system starts the wallet. The wallet's popup names the
   identity and offers to sign in without the button next time; if the user chose that, the page signs in by itself when it is opened.
2. The page polls `GET /api/sign-in/{id}`. An authorized outcome starts a **session**: the status response sets an HttpOnly cookie, and for 30 minutes
   (from the sign-in, not extended by use) the employee is signed in. The page shows who (`GET /api/session`) and has "Afmelden" (`DELETE /api/session`).
   Sessions live in memory, so a restart signs everyone out.
3. **Request a laadpaal.** Only in a session: the page posts the address to `POST /api/request-laadpaal`, which answers 401 without one. The backend asks
   attesta for a request (`request_laadpaal`, credential `Employee`, claims `department` and `diploma`) and returns a link. The wallet asks what to
   share, or answers on its own when the user chose to always share with this application.
4. The page polls `GET /api/request-laadpaal/{id}`. When attesta has decided, an authorized submission is carried out once: the address rules run and the
   outcome is recorded. The credential shown must be the signed-in employee's, otherwise the request is refused.

This is a workforce setup: the employees, their wallets, the application and attesta all belong to one organisation and run inside its network. attesta
has a single API and does not tell callers apart; which workloads may reach which is the platform's concern (network rules, service identity). The Service
`laadpalen-attesta` is a LoadBalancer only because the demo wallet runs on the host; a wallet inside the cluster would use the internal address.

## Running it

Docker Desktop's Kubernetes, with the shared namespace. The demo issuer must be deployed, since it signs the credentials and its DID is the one the policy trusts:

```sh
make k8s-issuer-apply     # the demo issuer; form at http://localhost:4000, employee credentials at /employee
make laadpalen            # builds and deploys the backend with its sidecar, and the page: http://localhost:4174
pnpm --filter @attesta/wallet-desktop package    # the wallet: open the app once; it needs https, see CLAUDE.md for the names
```

Get a credential: open http://localhost:4000/employee, fill in the form (department Burgerzaken or Secretariaat; the diploma date must be in the future),
and paste the offer link into the wallet. Then open http://localhost:4174, sign in and enter an address (1111BB 2 is free, 1111AA 1 has a laadpaal, 1111DD 4 has no
electric vehicle). Press "Aanmelden", then "Dien aanvraag in". The page opens the wallet each time; the link is also under "Toon API-aanroep" for a wallet
that has to be given it by hand.

The wallet reaches attesta at http://localhost:4175 (the `laadpalen-attesta` Service, container port 8080), which is `ATTESTA_PUBLIC_URL` in `k8s/local/laadpalen/api/deployment.yaml`.

## Roles

The roles are the application's own: `LAADPALEN_USER_ROLES` in `k8s/local/laadpalen/api/deployment.yaml` lists them by email, as
`ada@example.com=laadpalen-aanvrager;bob@example.com=reader`. A laadpaal request carries the signed-in employee's roles in `Att-User-Roles`; an employee
who is not listed has none, and the policy then refuses with "Niet geautoriseerd vanwege rol". The sign-in sends no roles.

The demo employees (issue each an employee credential at http://localhost:4000/employee):

| Employee | Email | Department | Role | Outcome |
|---|---|---|---|---|
| Jerry Smith | jsmith@vlierdam.nl | Burgerzaken | `laadpalen-aanvrager` | allowed |
| Tom de Vries | tdvries@vlierdam.nl | Burgerzaken | none | refused, "Niet geautoriseerd vanwege rol" |
| Sanne Bakker | sbakker@vlierdam.nl | Secretariaat | none | refused, "Niet geautoriseerd vanwege afdeling" |

## Policies

`make laadpalen-policies` writes `policies/*.gno` into the ConfigMap `laadpalen-policies`. The sidecar watches it, so a changed policy is live within about a
minute, with no restart. The application owns this ConfigMap: the file set is the whole set.

## Tests and logs

```sh
cd examples/laadpalen/api && go test ./...         # the backend against a fake sidecar, and the sidecar client
make laadpalen-logs laadpalen-attesta-logs         # the backend's log (who submitted what) and the sidecar's
make laadpalen-delete
```

`backend/internal/authz/examples_test.go` runs the policy through the real gnovm.
