# simple-gui

A demo of the zk-puoi flow end to end: a user proves they hold a diploma from a trusted issuer, without revealing the credential itself, and the
backend allows or denies access to a resource. See the repo root [README](../../README.md) for the architecture and the setup this example builds on.

## What it demonstrates

The GUI has three tabs.

- **Login** is a mock: any subject and roles are accepted and the backend returns a JWT. The JWT is only used for audit logging. It plays no part in
  the allow/deny decision, so the subject and roles you type have no effect on the outcome.
- **Wallet** imports credentials from the demo registry (`client-lib/circuits/diploma_membership/build/registry.json`). The registry holds two
  diplomas, and only these can be proven, because a proof needs a Merkle path into that registry:

  | Subject | Credential id | Issuer |
  |---|---|---|
  | `alice` | `diploma-trusted` | `trusted-university` |
  | `bob` | `diploma-mill` | `diploma-mill` |

- **Request resource** picks a credential and a resource, builds a Groth16 proof in the browser, and sends it to `/v1/authorize`.

The proof shows that the chosen credential is in the registry and discloses only its type and issuer. The decision then has three stages:

1. The backend verifies the proof. A forged or tampered proof is denied with `proof did not verify`.
2. It checks that the proof refers to the real registry root. Otherwise it is denied with `proof is not for the expected credential registry`.
3. It evaluates the Gno policy [`policies/diploma_check.gno`](policies/diploma_check.gno) against the disclosed type and issuer. The policy allows
   only a `Diploma` from `trusted-university`, for the resource `diploma-vault`.

The trusted issuers are the `trustedIssuers` map in the policy file. To trust another issuer, edit it and redeploy the policy.

## Running it

The backend ships with no policies, so deploy this example's policy into it.

On Docker Desktop's k8s, from the repo root:

```sh
make k8s-apply        # backend
make k8s-gui-apply    # this GUI, then hot-deploys policies/diploma_check.gno into the backend
```

Policies are held in memory only, so re-run `make k8s-gui-apply` (or `make k8s-deploy-policy POLICY=examples/simple-gui/policies/diploma_check.gno`)
after the backend restarts. Without the policy, requests fail with `unknown policy "diploma_check"`.

Locally without k8s:

```sh
cd backend && ZKPUOI_POLICIES_DIR=../examples/simple-gui/policies go run ./cmd/zk-puoi
cd examples/simple-gui && pnpm dev     # http://localhost:5173
```

This needs `pnpm install` from the repo root and the diploma_membership circuit and registry built (see the root README).

## Getting an allow and a deny

Allow: log in, import Alice's diploma (`trusted-university`) in the Wallet tab, then in Request resource select it and request `diploma-vault`.

Deny, with a valid proof from an untrusted issuer:

1. In **Login**, keep the defaults (or type anything) and click **Log in (mock)**. The Request resource tab stays disabled until you log in.
2. In **Wallet**, click **Import** on the row with issuer `diploma-mill` (subject `bob`).
3. In **Request resource**, leave Resource as `diploma-vault`.
4. In the Credential dropdown, select `Diploma — diploma-mill (bob)`. If you also imported Alice's, check which one is selected.
5. Click **Request access**.

The result is **Denied: issuer not trusted: diploma-mill**. The proof is valid and the credential really is in the registry. The policy rejects it
because `diploma-mill` isn't a trusted issuer.

Another deny: request any resource other than `diploma-vault`, even with Alice's credential. The reason is `unknown resource: <name>`.

Logging in as `bob` instead of `alice` changes nothing. Only the credential you prove matters.
