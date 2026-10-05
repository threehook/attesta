# laadpalen

A relying application with attesta as its **sidecar**. Municipality employees submit requests for a laadpaal (EV charging point) for a citizen's address.
Who may submit is attesta's decision, made from a credential in the employee's own wallet; what becomes of the request is the application's own business.

## What's here

- `api/` is the application's backend (Go, standard library only). The page calls it and nothing else. It asks the sidecar next to it whether the employee
  may submit, and then applies its own address rules (unknown address, a laadpaal already there, no electric vehicle). It records every decided request
  with the employee's `subject` (issuer and email).
- `gui/` is the page (Vite + React), in Dutch. The employee enters an address, opens the link it shows in the wallet, and sees the outcome.
- `policies/request_laadpaal.gno` is the application's policy: an `Employee` credential from a trusted issuer, for the `burgerzaken` department and
  a `laadpalen-management` diploma. Secretariaat employees are refused. The credential's own expiry (the diploma's last day) is checked before the policy runs.

## How it fits together

```
browser ──► page (nginx) ──► laadpalen-api ──► attesta (sidecar, 127.0.0.1:8080)      one pod: laadpalen-api
                                                    ▲
wallet ─────────────────────────────────────────────┘ :8080 (wallets: https://api.attesta.corbencreatives.nl)
```

1. The page posts the address to `POST /api/request-laadpaal`. The backend asks attesta for a request (`policyId` and `resource` `request_laadpaal`,
   credential `Employee`, claims `department` and `diploma`) and returns the `openid4vp://` link.
2. The employee opens the link in the wallet and confirms what is shared. The wallet posts its answer to attesta.
3. The page polls `GET /api/request-laadpaal/{id}`. When attesta has decided, an authorized submission is carried out once: the address rules run and the
   outcome is recorded.
4. An authorized outcome also starts a **session**: the status response sets an HttpOnly cookie, and for 30 minutes (from the proof, not extended by use)
   the employee's next submissions skip attesta and the wallet. The backend applies the address rules and records them with the same `subject`. The page
   shows who is signed in (`GET /api/session`) and has "Afmelden" (`DELETE /api/session`) to ask for the wallet again. Sessions live in memory, so a restart
   signs everyone out. Authorization is decided when the session starts: a credential that changes or expires afterwards is not noticed until it ends.

This is a workforce setup: the employees, their wallets, the application and attesta all belong to one organisation and run inside its network. attesta
has a single API and does not tell callers apart; which workloads may reach which is the platform's concern (network rules, service identity). The Service
`laadpalen-attesta` is a LoadBalancer only because the demo wallet runs on the host; a wallet inside the cluster would use the internal address.

## Running it

Docker Desktop's Kubernetes, with the shared namespace. The demo issuer must be deployed, since it signs the credentials and its DID is the one the policy trusts:

```sh
make k8s-issuer-apply     # the demo issuer; form at http://localhost:4000, employee credentials at /employee
make laadpalen            # builds and deploys the backend with its sidecar, and the page: http://localhost:4174
ATTESTA_ALLOW_INSECURE_HTTP=1 pnpm --filter @attesta/wallet-desktop start
```

Get a credential: open http://localhost:4000/employee, fill in the form (department Burgerzaken or Secretariaat; the diploma date must be in the future),
and paste the offer link into the wallet. Then open http://localhost:4174, enter an address (1111BB 2 is free, 1111AA 1 has a laadpaal, 1111DD 4 has no
electric vehicle), press "Dien aanvraag in", and paste the link the page shows into the wallet.

The wallet reaches attesta at http://localhost:4175 (the `laadpalen-attesta` Service, container port 8080), which is `ATTESTA_PUBLIC_URL` in `k8s/local/laadpalen/api/deployment.yaml`.

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
