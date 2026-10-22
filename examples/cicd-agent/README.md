# cicd-agent

A CI/CD agent (Python, [Claude Agent SDK](https://docs.claude.com/en/api/agent-sdk/overview)) that no person is at the screen for. It has a SPIFFE identity, a wallet of its own and an Attesta beside it,
and every tool it has asks Attesta first. The design is in [`docs/unattended-authorization.md`](../../docs/unattended-authorization.md).

## What's here

- `src/cicd_agent/identity.py` gets the agent's credential: it asks SPIRE for a JWT-SVID, shows it to the issuer's `POST /workloads/offer`, and hands the offer link to its wallet. The credential
  ends after an hour; the agent asks for a new one after half of that, and at once when Attesta did not accept the one it holds (a revoked one, for example).
- `src/cicd_agent/gate.py` asks Attesta whether an action may happen, hands the request to the wallet and reads the decision. Anything that goes wrong is a denial, with the reason.
- `src/cicd_agent/tools.py` is all the agent has: `run_tests` and `deploy`, as tools of the SDK, with none of the built-in tools (no shell, no files). A `PreToolUse` hook asks the gate before every
  tool call, so a tool runs only after Permit, and refuses any other tool. `LoggingRunner` only records the actions; a runner that builds and deploys takes its place.
  When the policy denies a production deploy with the advice `request_approval` (no mandate, or one that does not cover it), the gate parks the action at the issuer. It then waits
  `CICD_APPROVAL_WAIT_SECONDS` (default 0) for two people to approve it on the issuer's page Goedkeuringen; without waiting it answers Pending, and the next ask finds the approval.
  The gate asks again with the Approval in place of the Mandate. The Approval is for the exact resource and lasts 15 minutes.
- `src/cicd_agent/actions.py` says what each tool is to the gate: the resource (`deploy:<environment>:<version>`), the concrete action (`application`, `version`, `environment`, which the policy sees as
  `input.action`) and whether a mandate is needed. `src/cicd_agent/guard.py` turns a tool name and its arguments into that question, for any host: the hook uses it, and so can a worker of Managed Agents.
  A name or arguments it does not know are denied without asking.
- `src/cicd_agent/middleware.py` is the gate as WSGI middleware for a service: `gated(app, guard, action_of)` asks before it handles a request and answers 403 with the reason when the answer is not Permit.
- `src/cicd_agent/service.py` serves `GET /healthz`, `POST /check` (`{"resource": "deploy:staging:v1.4.2", "mandate": false}`: the gate's decision, without the model) and `POST /tasks` (`{"task": "..."}`: the agent carries
  it out; needs `ANTHROPIC_API_KEY`). `CICD_AGENT_TOKEN` makes both POST calls need that bearer token.
- `policies/cicd.rego` permits `run_tests` and `deploy:staging:<version>` for the agent's own SPIFFE ID and the environment `staging`, from the trusted issuer. `deploy:prod:<version>` also needs a
  Mandate from that issuer whose `resource` covers it and which two different people approved; the `deploy` tool then asks for the Mandate too.

## How it fits together

One pod, three containers on 127.0.0.1: the agent, its wallet (`wallet/`, run as `serve-cli.ts`) and Attesta (the backend image) with this example's policy. Only the agent gets the SPIFFE socket.
The issuer is the shared one, reached at its Service; it recognizes the agent by `spiffe://attesta.local/ns/attesta/sa/cicd-agent`.

```
agent ──► issuer /workloads/offer ──► offer link ──► wallet            (JWT-SVID from SPIRE)
agent ──► Attesta ──► request link ──► wallet ──► Attesta ──► decision (Permit or Deny)
```

## Run and test

```sh
cd examples/cicd-agent && pip install -e '.[dev]' && pytest     # Python 3.10 or later
make k8s-spire              # SPIRE in the cluster
make k8s-issuer-apply       # the issuer, with the SPIFFE socket and trust domain
make cicd cicd-enroll       # the pod, and the enrollment of its SPIFFE ID
make cicd-api-key           # only for /tasks
make cicd-policies          # hot-deploys policies/*.rego, no restart
```

Ask the gate without the model, from inside the cluster:

```sh
kubectl -n attesta port-forward svc/cicd-agent 8090:8090
TOKEN=$(kubectl -n attesta get secret cicd-agent -o jsonpath='{.data.token}' | base64 -d)
curl -X POST localhost:8090/check -H "authorization: Bearer $TOKEN" -d '{"resource":"deploy:staging"}'
```

## Limits

- An Approval is handed out once and lasts 15 minutes; a second deployment of the same version needs a new approval. Attesta itself keeps no state, so the use-once is the issuer's.
- A system has one mandate at a time, so a second release waits for the first to end or be covered by its pattern (`deploy:prod:v1.4.*`).
- The wallet keeps its key and credential in memory, so a new pod gets a new credential. Every request for one makes an entitlement and a position in the issuer's status list.
- `cicd-enroll` writes the enrollment into the issuer's database; the staff API (`POST /workloads`) needs a staff sign-in that only the issuer's pages have.
- Any pod that has the token can send tasks to the agent.
