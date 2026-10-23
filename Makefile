IMAGE_REPO := attesta-backend
# A fresh tag per build, not a reused "dev" tag: Docker Desktop's Kubernetes has been observed keeping pods on stale image content under a reused tag
# even after a real rebuild (imagePullPolicy: IfNotPresent plus whatever caching containerd does for "already present" tags) — a unique tag every time
# sidesteps that entirely.
IMAGE := $(IMAGE_REPO):dev-$(shell date +%Y%m%d%H%M%S)
NAMESPACE := attesta
KUBE_CONTEXT := docker-desktop

# Same unique-tag reasoning as IMAGE above.
ISSUER_API_IMAGE := attesta-issuer-api:dev-$(shell date +%Y%m%d%H%M%S)
ISSUER_GUI_IMAGE := attesta-issuer-gui:dev-$(shell date +%Y%m%d%H%M%S)

LAADPALEN_API_IMAGE := laadpalen-api:dev-$(shell date +%Y%m%d%H%M%S)
LAADPALEN_GUI_IMAGE := laadpalen-gui:dev-$(shell date +%Y%m%d%H%M%S)
# The laadpalen example's own policies; its sidecar serves exactly these (ConfigMap laadpalen-policies).
LAADPALEN_POLICIES := $(wildcard examples/laadpalen/policies/*.rego)

CICD_AGENT_IMAGE := cicd-agent:dev-$(shell date +%Y%m%d%H%M%S)
CICD_WALLET_IMAGE := cicd-wallet:dev-$(shell date +%Y%m%d%H%M%S)
# The CI/CD agent example's own policies; its sidecar serves exactly these (ConfigMap cicd-policies).
CICD_POLICIES := $(wildcard examples/cicd-agent/policies/*.rego)
# The SPIFFE ID SPIRE gives the agent's pod (k8s/local/spire/clusterspiffeid.yaml), and what the issuer records about it.
CICD_SPIFFE_ID := spiffe://attesta.local/ns/attesta/sa/cicd-agent

# put-configmap(name, files): writes a ConfigMap from the files and replaces whatever was there. kubectl apply would not do: it only removes keys that an
# earlier apply wrote, so a key added with kubectl patch would stay. The YAML is piped, never echoed: sh's echo expands the \t and \n
# escapes kubectl writes for policy source, which breaks the policy.
define put-configmap
if kubectl --context $(KUBE_CONTEXT) get configmap $(1) -n $(NAMESPACE) >/dev/null 2>&1; then verb=replace; else verb=create; fi; \
kubectl --context $(KUBE_CONTEXT) create configmap $(1) -n $(NAMESPACE) $(addprefix --from-file=,$(2)) --dry-run=client -o yaml | kubectl --context $(KUBE_CONTEXT) $$verb -f -
endef

# The wildcard certificate lego obtained for the demo names (see k8s/local/ingress/ingress.yaml); never in git.
DEMO_CERT_DIR := $(HOME)/.config/attesta/lego/.lego/certificates
DEMO_CERT_NAME := _.attesta.corbencreatives.nl

.PHONY: build test docker-build k8s-apply k8s-secret k8s-delete k8s-restart k8s-logs k8s-port-forward \
	docker-build-issuer k8s-issuer-apply k8s-issuer-secret k8s-issuer-mail k8s-issuer-delete k8s-issuer-restart k8s-issuer-logs k8s-issuer-gui-logs \
	docker-build-laadpalen-api docker-build-laadpalen-gui laadpalen laadpalen-secret laadpalen-policies laadpalen-delete laadpalen-logs laadpalen-attesta-logs \
	k8s-traefik k8s-tls k8s-ingress grafana \
	k8s-spire k8s-issuer-spiffe k8s-issuer-approval-mail docker-build-cicd-agent docker-build-cicd-wallet openbao cicd cicd-secret cicd-policies cicd-api-key cicd-delete cicd-logs

build:
	cd backend && go build ./...

test:
	cd backend && go test ./...
	cd examples/laadpalen/api && go test ./...

# Builds straight into Docker Desktop's local image store, which its bundled k8s reads from directly — no registry push needed for this inner dev
# loop.
docker-build:
	docker build -t $(IMAGE) backend

# Re-applies manifests, then points the deployment at the just-built image's exact (unique) tag via `kubectl set image` — this always triggers a real
# rollout, unlike re-applying a manifest whose image: field never changes.
k8s-apply: docker-build
	kubectl --context $(KUBE_CONTEXT) apply -f k8s/local/backend/namespace.yaml
	$(MAKE) k8s-secret
	kubectl --context $(KUBE_CONTEXT) apply -f k8s/local/backend/storageclass.yaml -f k8s/local/backend/pvc.yaml -f k8s/local/backend/deployment.yaml -f k8s/local/backend/service.yaml
	kubectl --context $(KUBE_CONTEXT) set image deployment/attesta-backend attesta-backend=$(IMAGE) -n $(NAMESPACE)
	kubectl --context $(KUBE_CONTEXT) rollout status deployment/attesta-backend -n $(NAMESPACE)

# Creates the admin-token Secret with a random value if it doesn't already exist. Left alone on repeat applies so a redeploy
# doesn't require re-learning a new admin token.
k8s-secret:
	@kubectl --context $(KUBE_CONTEXT) get secret attesta-backend -n $(NAMESPACE) >/dev/null 2>&1 || \
		kubectl --context $(KUBE_CONTEXT) create secret generic attesta-backend -n $(NAMESPACE) \
			--from-literal=admin-token=$$(openssl rand -hex 32)

k8s-delete:
	kubectl --context $(KUBE_CONTEXT) delete -f k8s/local/backend/deployment.yaml -f k8s/local/backend/service.yaml --ignore-not-found
	kubectl --context $(KUBE_CONTEXT) delete secret attesta-backend -n $(NAMESPACE) --ignore-not-found
	kubectl --context $(KUBE_CONTEXT) delete -f k8s/local/backend/namespace.yaml --ignore-not-found

k8s-restart:
	kubectl --context $(KUBE_CONTEXT) rollout restart deployment/attesta-backend -n $(NAMESPACE)

k8s-logs:
	kubectl --context $(KUBE_CONTEXT) logs -n $(NAMESPACE) deploy/attesta-backend -f

# attesta-backend is ClusterIP only — this is for manual curl/debugging directly against it.
k8s-port-forward:
	kubectl --context $(KUBE_CONTEXT) port-forward -n $(NAMESPACE) svc/attesta-backend 8080:8080

# Grafana (with the decision log in Loki) is in the observability stack, not in this repo. Open http://localhost:3000 and use Explore.
grafana:
	kubectl --context $(KUBE_CONTEXT) port-forward -n observability svc/grafana 3000:3000

# The API is built with issuer/api as the context; the page with the repo root as context (see issuer/gui/Dockerfile) — the pnpm workspace install needs the other workspace packages' manifests.
docker-build-issuer:
	docker build -t $(ISSUER_API_IMAGE) issuer/api
	docker build -f issuer/gui/Dockerfile -t $(ISSUER_GUI_IMAGE) .

# Same pattern as k8s-apply. The issuer is independent of the backend; its DID (from the default seed) is the one the laadpalen
# policies trust. Reachable at https://issuer.attesta.corbencreatives.nl (k8s-ingress); staff sign in there at Keycloak, which the LoadBalancer on localhost:4000 cannot do.
# attesta-issuer is the single deployment this one replaces, its LoadBalancer would hold host port 4000, and issuer-login is the login server that is gone.
k8s-issuer-apply: docker-build-issuer
	kubectl --context $(KUBE_CONTEXT) apply -f k8s/local/backend/namespace.yaml
	kubectl --context $(KUBE_CONTEXT) delete deployment/attesta-issuer service/attesta-issuer deployment/issuer-login service/issuer-login -n $(NAMESPACE) --ignore-not-found
	$(MAKE) k8s-issuer-secret
	kubectl --context $(KUBE_CONTEXT) apply -f k8s/local/backend/storageclass.yaml -f k8s/local/issuer/postgres-data -f k8s/local/issuer/postgres -f k8s/local/issuer/keycloak
	kubectl --context $(KUBE_CONTEXT) rollout status deployment/issuer-postgres deployment/issuer-keycloak -n $(NAMESPACE)
	kubectl --context $(KUBE_CONTEXT) apply -f k8s/local/issuer/api-deployment.yaml -f k8s/local/issuer/api-service.yaml -f k8s/local/issuer/gui-deployment.yaml -f k8s/local/issuer/gui-service.yaml
	kubectl --context $(KUBE_CONTEXT) set image deployment/attesta-issuer-api attesta-issuer-api=$(ISSUER_API_IMAGE) -n $(NAMESPACE)
	kubectl --context $(KUBE_CONTEXT) set image deployment/attesta-issuer-gui attesta-issuer-gui=$(ISSUER_GUI_IMAGE) -n $(NAMESPACE)
	@if kubectl --context $(KUBE_CONTEXT) get csidriver csi.spiffe.io >/dev/null 2>&1; then $(MAKE) k8s-issuer-spiffe; fi
	kubectl --context $(KUBE_CONTEXT) rollout status deployment/attesta-issuer-api -n $(NAMESPACE)
	kubectl --context $(KUBE_CONTEXT) rollout status deployment/attesta-issuer-gui -n $(NAMESPACE)

# The issuer database's account and the staff login's secrets, random, created once. Changing the database account later does not change the database that already
# exists. The mail settings come from k8s-issuer-mail.
k8s-issuer-secret:
	@kubectl --context $(KUBE_CONTEXT) get secret attesta-issuer-smtp -n $(NAMESPACE) >/dev/null 2>&1 || \
		{ echo "the issuer needs mail settings: run make k8s-issuer-mail SMTP_HOST=smtp.gmail.com SMTP_USER=<gmail> MAIL_FROM=<gmail>"; exit 1; }
	@kubectl --context $(KUBE_CONTEXT) get secret attesta-issuer-db -n $(NAMESPACE) >/dev/null 2>&1 || \
		kubectl --context $(KUBE_CONTEXT) create secret generic attesta-issuer-db -n $(NAMESPACE) \
			--from-literal=user=issuer --from-literal=password=$$(openssl rand -hex 24)
	@kubectl --context $(KUBE_CONTEXT) get secret attesta-issuer-auth -n $(NAMESPACE) >/dev/null 2>&1 || \
		kubectl --context $(KUBE_CONTEXT) create secret generic attesta-issuer-auth -n $(NAMESPACE) \
			--from-literal=client-secret=$$(openssl rand -hex 24) --from-literal=staff-password=$$(openssl rand -hex 12) --from-literal=admin-password=$$(openssl rand -hex 12)

# Points the issuer at a real mail server: make k8s-issuer-mail SMTP_HOST=<server> SMTP_USER=<login> MAIL_FROM=<sender> [SMTP_PORT=587]. The password is asked for and
# goes straight into the Secret, never on a command line. Port 587 secures the connection with STARTTLS and port 465 with TLS.
SMTP_PORT ?= 587
SMTP_SECRET ?= attesta-issuer-smtp
k8s-issuer-mail:
	@test -n "$(SMTP_HOST)" -a -n "$(SMTP_USER)" -a -n "$(MAIL_FROM)" || { echo "usage: make k8s-issuer-mail SMTP_HOST=<server> SMTP_USER=<login> MAIL_FROM=<sender> [SMTP_PORT=587]"; exit 1; }
	@printf 'Password or key for $(SMTP_USER) at $(SMTP_HOST) (typing is hidden): '; stty -echo; read -r password; stty echo; echo; \
		printf '%s' "$$password" | kubectl --context $(KUBE_CONTEXT) create secret generic $(SMTP_SECRET) -n $(NAMESPACE) \
			--from-literal=ATTESTA_ISSUER_SMTP_HOST=$(SMTP_HOST) --from-literal=ATTESTA_ISSUER_SMTP_PORT=$(SMTP_PORT) \
			--from-literal=ATTESTA_ISSUER_SMTP_USER=$(SMTP_USER) --from-file=ATTESTA_ISSUER_SMTP_PASSWORD=/dev/stdin \
			--from-literal=ATTESTA_ISSUER_MAIL_FROM=$(MAIL_FROM) --dry-run=client -o yaml | kubectl --context $(KUBE_CONTEXT) apply -f -
	kubectl --context $(KUBE_CONTEXT) rollout restart deployment/attesta-issuer-api -n $(NAMESPACE)

# Leaves the shared "attesta" namespace alone — k8s-delete (backend) owns it — and the issuer's data: its volume claim and its Secret.
k8s-issuer-delete:
	kubectl --context $(KUBE_CONTEXT) delete -f k8s/local/issuer/postgres -f k8s/local/issuer/keycloak --ignore-not-found
	kubectl --context $(KUBE_CONTEXT) delete -f k8s/local/issuer/api-deployment.yaml -f k8s/local/issuer/api-service.yaml -f k8s/local/issuer/gui-deployment.yaml -f k8s/local/issuer/gui-service.yaml --ignore-not-found

k8s-issuer-restart:
	kubectl --context $(KUBE_CONTEXT) rollout restart deployment/attesta-issuer-api deployment/attesta-issuer-gui -n $(NAMESPACE)

k8s-issuer-logs:
	kubectl --context $(KUBE_CONTEXT) logs -n $(NAMESPACE) deploy/attesta-issuer-api -f

k8s-issuer-gui-logs:
	kubectl --context $(KUBE_CONTEXT) logs -n $(NAMESPACE) deploy/attesta-issuer-gui -f

# The laadpalen example: an app backend with attesta as its sidecar, and its page (examples/laadpalen). The page is at https://laadpalen.attesta.corbencreatives.nl and wallets
# post to https://api.attesta.corbencreatives.nl (k8s-ingress); the backend image is built here too, since the sidecar is that same image. The example needs the issuer (k8s-issuer-apply).
docker-build-laadpalen-api:
	docker build -t $(LAADPALEN_API_IMAGE) examples/laadpalen/api

# Built with the repo root as context (see examples/laadpalen/gui/Dockerfile) — the pnpm workspace install needs the root manifests.
docker-build-laadpalen-gui:
	docker build -f examples/laadpalen/gui/Dockerfile -t $(LAADPALEN_GUI_IMAGE) .

# Same pattern as k8s-apply. Applies the Secret and the policies before the pod that needs them. The two containers of laadpalen-api get their images in one go.
laadpalen: docker-build docker-build-laadpalen-api docker-build-laadpalen-gui
	kubectl --context $(KUBE_CONTEXT) apply -f k8s/local/backend/namespace.yaml
	$(MAKE) laadpalen-secret laadpalen-policies
	kubectl --context $(KUBE_CONTEXT) apply -f k8s/local/backend/storageclass.yaml -f k8s/local/laadpalen/decision-log -f k8s/local/laadpalen/api -f k8s/local/laadpalen/gui
	kubectl --context $(KUBE_CONTEXT) set image deployment/laadpalen-api laadpalen-api=$(LAADPALEN_API_IMAGE) laadpalen-attesta=$(IMAGE) -n $(NAMESPACE)
	kubectl --context $(KUBE_CONTEXT) set image deployment/laadpalen-gui laadpalen-gui=$(LAADPALEN_GUI_IMAGE) -n $(NAMESPACE)
	kubectl --context $(KUBE_CONTEXT) rollout status deployment/laadpalen-api -n $(NAMESPACE)
	kubectl --context $(KUBE_CONTEXT) rollout status deployment/laadpalen-gui -n $(NAMESPACE)

# The sidecar's admin token, random, created once. Only the sidecar itself can use it: its admin endpoint is not exposed.
laadpalen-secret:
	@kubectl --context $(KUBE_CONTEXT) get secret laadpalen-attesta -n $(NAMESPACE) >/dev/null 2>&1 || \
		kubectl --context $(KUBE_CONTEXT) create secret generic laadpalen-attesta -n $(NAMESPACE) \
			--from-literal=admin-token=$$(openssl rand -hex 32)

# Hot-deploys the example's policies: writes examples/laadpalen/policies/*.rego into the ConfigMap its sidecar watches, no restart. The app owns this
# ConfigMap, so unlike k8s-policies there is nothing else in it to keep.
laadpalen-policies:
	@$(call put-configmap,laadpalen-policies,$(LAADPALEN_POLICIES))

# Leaves the shared "attesta" namespace alone — k8s-delete (backend) owns it.
laadpalen-delete:
	kubectl --context $(KUBE_CONTEXT) delete -f k8s/local/laadpalen/api -f k8s/local/laadpalen/gui --ignore-not-found
	kubectl --context $(KUBE_CONTEXT) delete secret laadpalen-attesta -n $(NAMESPACE) --ignore-not-found
	kubectl --context $(KUBE_CONTEXT) delete configmap laadpalen-policies -n $(NAMESPACE) --ignore-not-found

laadpalen-logs:
	kubectl --context $(KUBE_CONTEXT) logs -n $(NAMESPACE) deploy/laadpalen-api -c laadpalen-api -f

laadpalen-attesta-logs:
	kubectl --context $(KUBE_CONTEXT) logs -n $(NAMESPACE) deploy/laadpalen-api -c laadpalen-attesta -f

# https for the demo: Traefik on host ports 80/443, the certificate Secret, and the Ingress for laadpalen.attesta.corbencreatives.nl,
# api.attesta.corbencreatives.nl and issuer.attesta.corbencreatives.nl.
k8s-traefik:
	helm upgrade --install traefik traefik/traefik --kube-context $(KUBE_CONTEXT) -n ingress --create-namespace \
		--set ingressClass.enabled=true --set ingressClass.isDefaultClass=true --wait

k8s-tls:
	kubectl --context $(KUBE_CONTEXT) create secret tls attesta-tls -n $(NAMESPACE) \
		--cert=$(DEMO_CERT_DIR)/$(DEMO_CERT_NAME).crt --key=$(DEMO_CERT_DIR)/$(DEMO_CERT_NAME).key --dry-run=client -o yaml \
		| kubectl --context $(KUBE_CONTEXT) apply -f -

k8s-ingress: k8s-tls
	kubectl --context $(KUBE_CONTEXT) apply -f k8s/local/ingress

# SPIRE (server, agent, CSI driver and controller manager) from the hardened Helm chart, with the identities of k8s/local/spire. Needs SPIRE's chart repository.
k8s-spire:
	helm repo add spiffe https://spiffe.github.io/helm-charts-hardened/ --force-update
	helm upgrade --install spire-crds spiffe/spire-crds --kube-context $(KUBE_CONTEXT) -n spire-mgmt --create-namespace --wait
	helm upgrade --install spire spiffe/spire --kube-context $(KUBE_CONTEXT) -n spire-mgmt -f k8s/local/spire/values.yaml --wait --timeout 10m
	kubectl --context $(KUBE_CONTEXT) apply -f k8s/local/backend/namespace.yaml
	kubectl --context $(KUBE_CONTEXT) apply -f k8s/local/spire/clusterspiffeid.yaml

# Lets the issuer recognize workloads by their JWT-SVID. Needs SPIRE (k8s-spire); k8s-issuer-apply does the same when SPIRE is installed.
k8s-issuer-spiffe:
	kubectl --context $(KUBE_CONTEXT) patch deployment/attesta-issuer-api -n $(NAMESPACE) --patch-file k8s/local/issuer/api-spiffe-patch.yaml

# Who is mailed when a workload asks for approval of an action: make k8s-issuer-approval-mail APPROVAL_NOTIFY=<address>[,<address>]. The mail goes through the issuer's mail server
# (k8s-issuer-mail), so the addresses must be real mailboxes. An empty list sends no mail.
APPROVAL_NOTIFY ?=
k8s-issuer-approval-mail:
	kubectl --context $(KUBE_CONTEXT) set env deployment/attesta-issuer-api -n $(NAMESPACE) ATTESTA_ISSUER_APPROVAL_NOTIFY="$(APPROVAL_NOTIFY)"
	kubectl --context $(KUBE_CONTEXT) rollout status deployment/attesta-issuer-api -n $(NAMESPACE)

# The CI/CD agent example (examples/cicd-agent): the agent, its wallet and an attesta, in one pod. It needs the issuer (k8s-issuer-apply) with SPIRE
# (k8s-spire), and its workload enrolled on the issuer's page Systemen.
docker-build-cicd-agent:
	docker build -t $(CICD_AGENT_IMAGE) examples/cicd-agent

# Built with the repo root as context (see wallet/Dockerfile): the pnpm workspace install needs the root manifests.
docker-build-cicd-wallet:
	docker build -f wallet/Dockerfile -t $(CICD_WALLET_IMAGE) .

# OpenBao (dev mode) as the KMS that holds the wallets' keys: Transit engine on, a policy for wallets, and a token for them in the Secret openbao-wallet.
# Run it again after OpenBao restarted, then restart the wallets: it keeps nothing on disk.
openbao:
	kubectl --context $(KUBE_CONTEXT) apply -f k8s/local/backend/namespace.yaml
	@kubectl --context $(KUBE_CONTEXT) get secret openbao-root -n $(NAMESPACE) >/dev/null 2>&1 || \
		kubectl --context $(KUBE_CONTEXT) create secret generic openbao-root -n $(NAMESPACE) --from-literal=token=$$(openssl rand -hex 24)
	kubectl --context $(KUBE_CONTEXT) apply -f k8s/local/openbao/openbao.yaml
	kubectl --context $(KUBE_CONTEXT) rollout status deployment/openbao -n $(NAMESPACE)
	@bao() { kubectl --context $(KUBE_CONTEXT) exec -i -n $(NAMESPACE) deploy/openbao -- sh -c 'BAO_ADDR=http://127.0.0.1:8200 BAO_TOKEN=$$BAO_DEV_ROOT_TOKEN_ID bao "$$@"' bao "$$@"; }; \
		bao secrets list | grep -q '^transit/' || bao secrets enable transit; \
		bao policy write attesta-wallet - < k8s/local/openbao/wallet-policy.hcl; \
		token=$$(bao token create -orphan -policy=attesta-wallet -period=768h -field=token) && \
		kubectl --context $(KUBE_CONTEXT) create secret generic openbao-wallet -n $(NAMESPACE) --from-literal=token=$$token --dry-run=client -o yaml | \
		kubectl --context $(KUBE_CONTEXT) apply -f -

cicd: docker-build docker-build-cicd-agent docker-build-cicd-wallet openbao
	$(MAKE) cicd-secret cicd-policies
	kubectl --context $(KUBE_CONTEXT) apply -f k8s/local/cicd
	kubectl --context $(KUBE_CONTEXT) set image deployment/cicd-agent cicd-agent=$(CICD_AGENT_IMAGE) cicd-wallet=$(CICD_WALLET_IMAGE) cicd-attesta=$(IMAGE) -n $(NAMESPACE)
	kubectl --context $(KUBE_CONTEXT) rollout status deployment/cicd-agent -n $(NAMESPACE)

# The agent's token for its own endpoints and its attesta's admin token, random, created once. The model key is added by cicd-api-key.
cicd-secret:
	@kubectl --context $(KUBE_CONTEXT) get secret cicd-agent -n $(NAMESPACE) >/dev/null 2>&1 || \
		kubectl --context $(KUBE_CONTEXT) create secret generic cicd-agent -n $(NAMESPACE) \
			--from-literal=token=$$(openssl rand -hex 24) --from-literal=admin-token=$$(openssl rand -hex 32)

# Gives the agent the key it needs for /tasks. The key is asked for and goes straight into the Secret, never on a command line.
cicd-api-key:
	@printf 'Anthropic API key (typing is hidden): '; stty -echo; read -r key; stty echo; echo; \
		kubectl --context $(KUBE_CONTEXT) patch secret cicd-agent -n $(NAMESPACE) --type merge -p "{\"stringData\":{\"anthropic-api-key\":\"$$key\"}}"
	kubectl --context $(KUBE_CONTEXT) rollout restart deployment/cicd-agent -n $(NAMESPACE)

# Hot-deploys the agent's policies, no restart.
cicd-policies:
	@$(call put-configmap,cicd-policies,$(CICD_POLICIES))

# Leaves the shared "attesta" namespace alone — k8s-delete (backend) owns it.
cicd-delete:
	kubectl --context $(KUBE_CONTEXT) delete -f k8s/local/cicd --ignore-not-found
	kubectl --context $(KUBE_CONTEXT) delete secret cicd-agent -n $(NAMESPACE) --ignore-not-found
	kubectl --context $(KUBE_CONTEXT) delete configmap cicd-policies -n $(NAMESPACE) --ignore-not-found

cicd-logs:
	kubectl --context $(KUBE_CONTEXT) logs -n $(NAMESPACE) deploy/cicd-agent --all-containers --prefix -f
