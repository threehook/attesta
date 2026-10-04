IMAGE_REPO := attesta-backend
# A fresh tag per build, not a reused "dev" tag: Docker Desktop's Kubernetes has been observed keeping pods on stale image content under a reused tag
# even after a real rebuild (imagePullPolicy: IfNotPresent plus whatever caching containerd does for "already present" tags) — a unique tag every time
# sidesteps that entirely.
IMAGE := $(IMAGE_REPO):dev-$(shell date +%Y%m%d%H%M%S)
NAMESPACE := attesta
KUBE_CONTEXT := docker-desktop

GUI_IMAGE_REPO := attesta-gui
# Same unique-tag reasoning as IMAGE above.
GUI_IMAGE := $(GUI_IMAGE_REPO):dev-$(shell date +%Y%m%d%H%M%S)
# Vite's own preview-server port; used for the k8s-gui-port-forward fallback, matching the LoadBalancer's own port.
GUI_LOCAL_PORT := 4173

ISSUER_IMAGE_REPO := attesta-issuer
# Same unique-tag reasoning as IMAGE above.
ISSUER_IMAGE := $(ISSUER_IMAGE_REPO):dev-$(shell date +%Y%m%d%H%M%S)

# POLICIES lists the .gno files the backend serves, e.g. POLICIES="a.gno b.gno"; each file's name without .gno is its policy ID. They go into the ConfigMap
# attesta-policies, which the running backend watches. Left empty, k8s-apply keeps the ConfigMap as it is.
POLICIES :=
# The policy examples/simple-gui asks of the backend; `make simple-gui` adds it.
SIMPLE_GUI_POLICIES := examples/simple-gui/policies/diploma_check.gno

.PHONY: build test docker-build k8s-apply k8s-secret k8s-delete k8s-restart k8s-logs k8s-port-forward k8s-policies k8s-policies-add k8s-ensure-policies \
	docker-build-gui simple-gui k8s-gui-delete k8s-gui-restart k8s-gui-logs k8s-gui-port-forward \
	docker-build-issuer k8s-issuer-apply k8s-issuer-delete k8s-issuer-restart k8s-issuer-logs

build:
	cd backend && go build ./...

test:
	cd backend && go test ./...

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

# attesta-backend is ClusterIP only (the GUI's nginx reverse-proxies to it in-cluster) — this is for manual curl/debugging directly against it.
k8s-port-forward:
	kubectl --context $(KUBE_CONTEXT) port-forward -n $(NAMESPACE) svc/attesta-backend 8080:8080

# Hot-deploys policies: writes POLICIES into the ConfigMap the backend reads, and the running backend picks the change up (within about a minute, as
# Kubernetes updates the mounted files) without a restart. Files left out of POLICIES are removed from the backend.
# Usage: make k8s-policies POLICIES="examples/simple-gui/policies/diploma_check.gno other.gno"
k8s-policies:
	@if [ -z "$(POLICIES)" ]; then echo "usage: make k8s-policies POLICIES=\"path/to/a.gno path/to/b.gno\""; exit 1; fi
	kubectl --context $(KUBE_CONTEXT) apply -f k8s/local/backend/namespace.yaml
	kubectl --context $(KUBE_CONTEXT) create configmap attesta-policies -n $(NAMESPACE) $(addprefix --from-file=,$(POLICIES)) --dry-run=client -o yaml \
		| kubectl --context $(KUBE_CONTEXT) apply -f -

# Like k8s-policies, but only adds or updates the named files and leaves the other policies in the ConfigMap alone. Also live, no restart.
# Usage: make k8s-policies-add POLICIES=examples/simple-gui/policies/diploma_check.gno
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

# Built with the repo root as context (see examples/simple-gui/Dockerfile) — the pnpm workspace install needs client-lib's package.json too.
docker-build-gui:
	docker build -f examples/simple-gui/Dockerfile -t $(GUI_IMAGE) .

# Same pattern as k8s-apply, then adds the page's policy (SIMPLE_GUI_POLICIES) to the backend's, leaving its other policies alone. Depends on the
# backend already being applied (shares its namespace).
simple-gui: docker-build-gui
	kubectl --context $(KUBE_CONTEXT) apply -f k8s/local/backend/namespace.yaml
	kubectl --context $(KUBE_CONTEXT) apply -f k8s/local/gui/deployment.yaml -f k8s/local/gui/service.yaml
	kubectl --context $(KUBE_CONTEXT) set image deployment/attesta-gui attesta-gui=$(GUI_IMAGE) -n $(NAMESPACE)
	kubectl --context $(KUBE_CONTEXT) rollout status deployment/attesta-gui -n $(NAMESPACE)
	$(MAKE) k8s-policies-add POLICIES="$(SIMPLE_GUI_POLICIES)"

# Leaves the shared "attesta" namespace alone — k8s-delete (backend) owns it.
k8s-gui-delete:
	kubectl --context $(KUBE_CONTEXT) delete -f k8s/local/gui/deployment.yaml -f k8s/local/gui/service.yaml --ignore-not-found

k8s-gui-restart:
	kubectl --context $(KUBE_CONTEXT) rollout restart deployment/attesta-gui -n $(NAMESPACE)

k8s-gui-logs:
	kubectl --context $(KUBE_CONTEXT) logs -n $(NAMESPACE) deploy/attesta-gui -f

# Fallback only — attesta-gui is a LoadBalancer Service, already auto-forwarded to localhost.
k8s-gui-port-forward:
	kubectl --context $(KUBE_CONTEXT) port-forward -n $(NAMESPACE) svc/attesta-gui $(GUI_LOCAL_PORT):80

# Built with the repo root as context (see examples/issuer/Dockerfile) — the pnpm workspace install needs the other workspace packages' manifests.
docker-build-issuer:
	docker build -f examples/issuer/Dockerfile -t $(ISSUER_IMAGE) .

# Same pattern as k8s-apply. The demo issuer is independent of the backend; its DID (from the default seed) is the one examples/simple-gui's
# policy trusts. Reachable from the host at http://localhost:4000 through the LoadBalancer.
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
