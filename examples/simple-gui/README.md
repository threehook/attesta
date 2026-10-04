# simple-gui

The relying application in the demo: a web page that wants proof of a diploma before it gives access to a resource. It asks the attesta backend to start
an authorization, shows the user a link for their wallet, and displays the decision once the wallet has answered. It never sees the credential, only
the allow or deny and the reason.

## How it works

1. You fill in the resource, the credential type (`Diploma`) and the claims to ask for besides the email (`degree`), then press **Request access**.
2. The page calls the backend (`POST /v1/authorize/requests`) and shows an `openid4vp://` link. It then polls the backend for the decision.
3. You open the link in your wallet (`wallet-desktop`). The wallet shows who is asking and for what, and answers the backend directly with a presentation that discloses
   only the requested claims. The email, which identifies the holder, is always among them.
4. The backend verifies the presentation, runs the Gno policy [`policies/diploma_check.gno`](policies/diploma_check.gno) with the resource, the
   credential type and the issuer's DID, and the page shows **Allowed** or **Denied** with the policy's reason. When allowed it also shows the email the holder was identified by and the issuer that
   vouched for it.

The policy allows the `diploma-vault` resource for a `Diploma` credential from a trusted issuer. Trusted issuers are the DIDs in its `trustedIssuers`
map; it lists the demo issuer (`examples/issuer`, started with its default seed). To trust another issuer, add its DID and redeploy the policy.

## Running it

The backend needs `ATTESTA_PUBLIC_URL` set to the address the wallet reaches it at (`k8s/local/backend/deployment.yaml` sets
`http://localhost:4173`, the page's LoadBalancer, which proxies to the backend). Deploy the policy into it, start the issuer, get a credential into the
wallet, then run the page:

```sh
make k8s-apply               # the backend (applies the policy set from its ConfigMap)
make simple-gui              # the page in Docker Desktop's k8s, and its policy diploma_check.gno added to the backend, live
make k8s-issuer-apply         # the demo issuer in k8s (or `cd examples/issuer && pnpm start`); it prints its DID
                              # open http://localhost:4000, fill the form, copy the offer link
ATTESTA_ALLOW_INSECURE_HTTP=1 pnpm --filter @attesta/wallet-desktop start   # the wallet: paste the offer link and confirm
cd examples/simple-gui && pnpm dev            # http://localhost:5173
                                              # paste the link the page shows into the wallet and confirm
```

`pnpm dev` talks to the backend at `http://localhost:8080`; set `VITE_API_BASE_URL` to change that. The page needs the workspace's `@attesta/client`
built after changes to it (`pnpm --filter @attesta/client build`), because Vite doesn't watch across the package boundary.

## Getting an allow and a deny

- **Allow:** present a diploma from the demo issuer. Its DID is in the policy.
- **Deny, untrusted issuer:** start a second issuer with another seed (`ATTESTA_ISSUER_SEED=other ATTESTA_ISSUER_PORT=4001 pnpm start`), accept a
  diploma from it into the wallet, and present that one. The presentation is valid, but the policy replies `issuer not trusted: did:key:...`.
- **Deny, other resource:** request any resource other than `diploma-vault`; the policy replies `unknown resource: <name>`.
