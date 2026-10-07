IMAGE_REPO := attesta-backend
# A fresh tag per build, not a reused "dev" tag: Docker Desktop's Kubernetes has been observed keeping pods on stale image content under a reused tag
# even after a real rebuild (imagePullPolicy: IfNotPresent plus whatever caching containerd does for "already present" tags) — a unique tag every time
# sidesteps that entirely.
IMAGE := $(IMAGE_REPO):dev-$(shell date +%Y%m%d%H%M%S)
NAMESPACE := attesta
KUBE_CONTEXT := docker-desktop

ISSUER_IMAGE_REPO := attesta-issuer
# Same unique-tag reasoning as IMAGE above.
ISSUER_IMAGE := $(ISSUER_IMAGE_REPO):dev-$(shell date +%Y%m%d%H%M%S)

LAADPALEN_API_IMAGE := laadpalen-api:dev-$(shell date +%Y%m%d%H%M%S)
LAADPALEN_GUI_IMAGE := laadpalen-gui:dev-$(shell date +%Y%m%d%H%M%S)
# The laadpalen example's own policies; its sidecar serves exactly these (ConfigMap laadpalen-policies).
LAADPALEN_POLICIES := $(wildcard examples/laadpalen/policies/*.gno)

# put-configmap(name, files): writes a ConfigMap from the files and replaces whatever was there. kubectl apply would not do: it only removes keys that an
# earlier apply wrote, so a key added with kubectl patch (k8s-policies-add) would stay. The YAML is piped, never echoed: sh's echo expands the \t and \n
# escapes kubectl writes for Go source, which breaks the policy.
define put-configmap
if kubectl --context $(KUBE_CONTEXT) get configmap $(1) -n $(NAMESPACE) >/dev/null 2>&1; then verb=replace; else verb=create; fi; \
kubectl --context $(KUBE_CONTEXT) create configmap $(1) -n $(NAMESPACE) $(addprefix --from-file=,$(2)) --dry-run=client -o yaml | kubectl --context $(KUBE_CONTEXT) $$verb -f -
endef

# POLICIES lists the .gno files the backend serves, e.g. POLICIES="a.gno b.gno"; each file's name without .gno is its policy ID. They go into the ConfigMap
# attesta-policies, which the running backend watches. Left empty, k8s-apply keeps the ConfigMap as it is.
POLICIES :=
# The wildcard certificate lego obtained for the demo names (see k8s/local/ingress/ingress.yaml); never in git.
DEMO_CERT_DIR := $(HOME)/.config/attesta/lego/.lego/certificates
DEMO_CERT_NAME := _.attesta.corbencreatives.nl

.PHONY: build test docker-build k8s-apply k8s-secret k8s-delete k8s-restart k8s-logs k8s-port-forward k8s-policies k8s-policies-add k8s-ensure-policies \
	docker-build-issuer k8s-issuer-apply k8s-issuer-delete k8s-issuer-restart k8s-issuer-logs \
	docker-build-laadpalen-api docker-build-laadpalen-gui laadpalen laadpalen-secret laadpalen-policies laadpalen-delete laadpalen-logs laadpalen-attesta-logs \
	k8s-traefik k8s-tls k8s-ingress

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
	$(MAKE) k8s-secret k8s-ensure-policies
	kubectl --context $(KUBE_CONTEXT) apply -f k8s/local/backend/deployment.yaml -f k8s/local/backend/service.yaml
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

# Hot-deploys policies: writes POLICIES into the ConfigMap the backend reads, and the running backend picks the change up (within about a minute, as
# Kubernetes updates the mounted files) without a restart. Files left out of POLICIES are removed from the backend.
# Usage: make k8s-policies POLICIES="examples/laadpalen/policies/sign_in.gno other.gno"
k8s-policies:
	@if [ -z "$(POLICIES)" ]; then echo "usage: make k8s-policies POLICIES=\"path/to/a.gno path/to/b.gno\""; exit 1; fi
	kubectl --context $(KUBE_CONTEXT) apply -f k8s/local/backend/namespace.yaml
	@$(call put-configmap,attesta-policies,$(POLICIES))

# Like k8s-policies, but only adds or updates the named files and leaves the other policies in the ConfigMap alone. Also live, no restart.
# Usage: make k8s-policies-add POLICIES=examples/laadpalen/policies/sign_in.gno
k8s-policies-add:
	@if [ -z "$(POLICIES)" ]; then echo "usage: make k8s-policies-add POLICIES=\"path/to/a.gno\""; exit 1; fi
	kubectl --context $(KUBE_CONTEXT) apply -f k8s/local/backend/namespace.yaml
	@kubectl --context $(KUBE_CONTEXT) get configmap attesta-policies -n $(NAMESPACE) >/dev/null 2>&1 || \
		kubectl --context $(KUBE_CONTEXT) create configmap attesta-policies -n $(NAMESPACE)
	kubectl --context $(KUBE_CONTEXT) patch configmap attesta-policies -n $(NAMESPACE) --type merge -p "$$(kubectl create configmap attesta-policies \
		$(addprefix --from-file=,$(POLICIES)) --dry-run=client -o json | python3 -c 'import json,sys; print(json.dumps({"data": json.load(sys.stdin)["data"]}))')"

# Makes sure the ConfigMap exists before the backend starts: from POLICIES when given, empty when it does not exist yet, as it is when POLICIES is omitted.
k8s-ensure-policies:
	@if [ -n "$(POLICIES)" ]; then $(MAKE) k8s-policies POLICIES="$(POLICIES)"; \
	else kubectl --context $(KUBE_CONTEXT) get configmap attesta-policies -n $(NAMESPACE) >/dev/null 2>&1 || \
		kubectl --context $(KUBE_CONTEXT) create configmap attesta-policies -n $(NAMESPACE); fi

# Built with the repo root as context (see examples/issuer/Dockerfile) — the pnpm workspace install needs the other workspace packages' manifests.
docker-build-issuer:
	docker build -f examples/issuer/Dockerfile -t $(ISSUER_IMAGE) .

# Same pattern as k8s-apply. The demo issuer is independent of the backend; its DID (from the default seed) is the one the laadpalen
# policies trust. Reachable at https://issuer.attesta.corbencreatives.nl (k8s-ingress) and from the host at http://localhost:4000 through the LoadBalancer.
k8s-issuer-apply: docker-build-issuer
	kubectl --context $(KUBE_CONTEXT) apply -f k8s/local/backend/namespace.yaml
	kubectl --context $(KUBE_CONTEXT) apply -f k8s/local/issuer/deployment.yaml -f k8s/local/issuer/service.yaml
	kubectl --context $(KUBE_CONTEXT) set image deployment/attesta-issuer attesta-issuer=$(ISSUER_IMAGE) -n $(NAMESPACE)
	kubectl --context $(KUBE_CONTEXT) rollout status deployment/attesta-issuer -n $(NAMESPACE)

# Leaves the shared "attesta" namespace alone — k8s-delete (backend) owns it.
k8s-issuer-delete:
	kubectl --context $(KUBE_CONTEXT) delete -f k8s/local/issuer/deployment.yaml -f k8s/local/issuer/service.yaml --ignore-not-found

k8s-issuer-restart:
	kubectl --context $(KUBE_CONTEXT) rollout restart deployment/attesta-issuer -n $(NAMESPACE)

k8s-issuer-logs:
	kubectl --context $(KUBE_CONTEXT) logs -n $(NAMESPACE) deploy/attesta-issuer -f

# The laadpalen example: an app backend with attesta as its sidecar, and its page (examples/laadpalen). The page is at https://laadpalen.attesta.corbencreatives.nl and wallets
# post to https://api.attesta.corbencreatives.nl (k8s-ingress); the backend image is built here too, since the sidecar is that same image. The example needs the demo issuer (k8s-issuer-apply).
docker-build-laadpalen-api:
	docker build -t $(LAADPALEN_API_IMAGE) examples/laadpalen/api

# Built with the repo root as context (see examples/laadpalen/gui/Dockerfile) — the pnpm workspace install needs the root manifests.
docker-build-laadpalen-gui:
	docker build -f examples/laadpalen/gui/Dockerfile -t $(LAADPALEN_GUI_IMAGE) .

# Same pattern as k8s-apply. Applies the Secret and the policies before the pod that needs them. The two containers of laadpalen-api get their images in one go.
laadpalen: docker-build docker-build-laadpalen-api docker-build-laadpalen-gui
	kubectl --context $(KUBE_CONTEXT) apply -f k8s/local/backend/namespace.yaml
	$(MAKE) laadpalen-secret laadpalen-policies
	kubectl --context $(KUBE_CONTEXT) apply -f k8s/local/laadpalen/api -f k8s/local/laadpalen/gui
	kubectl --context $(KUBE_CONTEXT) set image deployment/laadpalen-api api=$(LAADPALEN_API_IMAGE) attesta=$(IMAGE) -n $(NAMESPACE)
	kubectl --context $(KUBE_CONTEXT) set image deployment/laadpalen-gui laadpalen-gui=$(LAADPALEN_GUI_IMAGE) -n $(NAMESPACE)
	kubectl --context $(KUBE_CONTEXT) rollout status deployment/laadpalen-api -n $(NAMESPACE)
	kubectl --context $(KUBE_CONTEXT) rollout status deployment/laadpalen-gui -n $(NAMESPACE)

# The sidecar's admin token, random, created once. Only the sidecar itself can use it: its admin endpoint is not exposed.
laadpalen-secret:
	@kubectl --context $(KUBE_CONTEXT) get secret laadpalen-attesta -n $(NAMESPACE) >/dev/null 2>&1 || \
		kubectl --context $(KUBE_CONTEXT) create secret generic laadpalen-attesta -n $(NAMESPACE) \
			--from-literal=admin-token=$$(openssl rand -hex 32)

# Hot-deploys the example's policies: writes examples/laadpalen/policies/*.gno into the ConfigMap its sidecar watches, no restart. The app owns this
# ConfigMap, so unlike k8s-policies there is nothing else in it to keep.
laadpalen-policies:
	@$(call put-configmap,laadpalen-policies,$(LAADPALEN_POLICIES))

# Leaves the shared "attesta" namespace alone — k8s-delete (backend) owns it.
laadpalen-delete:
	kubectl --context $(KUBE_CONTEXT) delete -f k8s/local/laadpalen/api -f k8s/local/laadpalen/gui --ignore-not-found
	kubectl --context $(KUBE_CONTEXT) delete secret laadpalen-attesta -n $(NAMESPACE) --ignore-not-found
	kubectl --context $(KUBE_CONTEXT) delete configmap laadpalen-policies -n $(NAMESPACE) --ignore-not-found

laadpalen-logs:
	kubectl --context $(KUBE_CONTEXT) logs -n $(NAMESPACE) deploy/laadpalen-api -c api -f

laadpalen-attesta-logs:
	kubectl --context $(KUBE_CONTEXT) logs -n $(NAMESPACE) deploy/laadpalen-api -c attesta -f

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
