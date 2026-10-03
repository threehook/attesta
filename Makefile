IMAGE_REPO := zk-puoi-backend
# A fresh tag per build, not a reused "dev" tag: Docker Desktop's Kubernetes has been observed keeping pods on stale image content under a reused tag
# even after a real rebuild (imagePullPolicy: IfNotPresent plus whatever caching containerd does for "already present" tags) — a unique tag every time
# sidesteps that entirely.
IMAGE := $(IMAGE_REPO):dev-$(shell date +%Y%m%d%H%M%S)
NAMESPACE := zk-puoi
KUBE_CONTEXT := docker-desktop
# Lazily expanded (= not :=): only read from the cluster when a target that actually uses it (k8s-deploy-policy) expands it, not on every `make` invocation.
ADMIN_TOKEN = $(shell kubectl --context $(KUBE_CONTEXT) get secret zk-puoi-backend -n $(NAMESPACE) -o jsonpath='{.data.admin-token}' 2>/dev/null | base64 -d)
LOCAL_PORT := 18080

GUI_IMAGE_REPO := zk-puoi-gui
# Same unique-tag reasoning as IMAGE above.
GUI_IMAGE := $(GUI_IMAGE_REPO):dev-$(shell date +%Y%m%d%H%M%S)
# Vite's own preview-server port; used for the k8s-gui-port-forward fallback, matching the LoadBalancer's own port.
GUI_LOCAL_PORT := 4173

# POLICY=path/to/file.gno hot-deploys that policy into the running backend (see k8s-deploy-policy). POLICY_ID
# defaults to the filename without its .gno extension, e.g. diploma_check.gno -> diploma_check.
POLICY :=
POLICY_ID := $(basename $(notdir $(POLICY)))

.PHONY: build test docker-build k8s-apply k8s-secret k8s-delete k8s-restart k8s-logs k8s-port-forward k8s-deploy-policy \
	docker-build-gui k8s-gui-apply k8s-gui-delete k8s-gui-restart k8s-gui-logs k8s-gui-port-forward

build:
	cd backend && go build ./...

test:
	cd backend && go test ./...

# Builds straight into Docker Desktop's local image store, which its bundled k8s reads from directly — no registry push needed for this inner dev
# loop. Context is the repo root (not backend/) because the image also needs client-lib's circom-exported verification key.
docker-build:
	docker build -f backend/Dockerfile -t $(IMAGE) .

# Re-applies manifests, then points the deployment at the just-built image's exact (unique) tag via `kubectl set image` — this always triggers a real
# rollout, unlike re-applying a manifest whose image: field never changes. Add POLICY=path/to/file.gno to also hot-deploy that policy once the rollout
# is ready, e.g.: make k8s-apply POLICY=backend/policies/diploma_check.gno
k8s-apply: docker-build
	kubectl --context $(KUBE_CONTEXT) apply -f k8s/local/backend/namespace.yaml
	$(MAKE) k8s-secret
	kubectl --context $(KUBE_CONTEXT) apply -f k8s/local/backend/deployment.yaml -f k8s/local/backend/service.yaml
	kubectl --context $(KUBE_CONTEXT) set image deployment/zk-puoi-backend zk-puoi-backend=$(IMAGE) -n $(NAMESPACE)
	kubectl --context $(KUBE_CONTEXT) rollout status deployment/zk-puoi-backend -n $(NAMESPACE)
	@if [ -n "$(POLICY)" ]; then $(MAKE) k8s-deploy-policy POLICY="$(POLICY)" POLICY_ID="$(POLICY_ID)"; fi

# Creates the JWT-signing-secret/admin-token Secret with random values if it doesn't already exist. Left alone on repeat applies so a redeploy
# doesn't invalidate already-issued JWTs or require re-learning a new admin token.
k8s-secret:
	@kubectl --context $(KUBE_CONTEXT) get secret zk-puoi-backend -n $(NAMESPACE) >/dev/null 2>&1 || \
		kubectl --context $(KUBE_CONTEXT) create secret generic zk-puoi-backend -n $(NAMESPACE) \
			--from-literal=jwt-secret=$$(openssl rand -hex 32) \
			--from-literal=admin-token=$$(openssl rand -hex 32)

k8s-delete:
	kubectl --context $(KUBE_CONTEXT) delete -f k8s/local/backend/deployment.yaml -f k8s/local/backend/service.yaml --ignore-not-found
	kubectl --context $(KUBE_CONTEXT) delete secret zk-puoi-backend -n $(NAMESPACE) --ignore-not-found
	kubectl --context $(KUBE_CONTEXT) delete -f k8s/local/backend/namespace.yaml --ignore-not-found

k8s-restart:
	kubectl --context $(KUBE_CONTEXT) rollout restart deployment/zk-puoi-backend -n $(NAMESPACE)

k8s-logs:
	kubectl --context $(KUBE_CONTEXT) logs -n $(NAMESPACE) deploy/zk-puoi-backend -f

# zk-puoi-backend is ClusterIP only (the GUI's nginx reverse-proxies to it in-cluster) — this is for manual curl/debugging directly against it.
k8s-port-forward:
	kubectl --context $(KUBE_CONTEXT) port-forward -n $(NAMESPACE) svc/zk-puoi-backend 8080:8080

# Hot-deploys a .gno policy file into the already-running backend via POST /admin/policies — no rebuild or restart.
# Usage: make k8s-deploy-policy POLICY=backend/policies/diploma_check.gno [POLICY_ID=name]
k8s-deploy-policy:
	@if [ -z "$(POLICY)" ]; then echo "usage: make k8s-deploy-policy POLICY=path/to/file.gno [POLICY_ID=name]"; exit 1; fi
	@if [ ! -f "$(POLICY)" ]; then echo "no such file: $(POLICY)"; exit 1; fi
	kubectl --context $(KUBE_CONTEXT) port-forward -n $(NAMESPACE) svc/zk-puoi-backend $(LOCAL_PORT):8080 \
		>/tmp/zk-puoi-port-forward.log 2>&1 & echo $$! >/tmp/zk-puoi-port-forward.pid
	@for i in 1 2 3 4 5; do curl -sf -o /dev/null http://localhost:$(LOCAL_PORT)/healthz && break; sleep 1; done
	python3 -c "import json,sys; print(json.dumps({'id': sys.argv[1], 'source': open(sys.argv[2]).read()}))" \
			"$(POLICY_ID)" "$(POLICY)" \
		| curl -sf -X POST http://localhost:$(LOCAL_PORT)/admin/policies \
			-H "X-Admin-Token: $(ADMIN_TOKEN)" -H "Content-Type: application/json" --data-binary @- \
		&& echo "\npolicy '$(POLICY_ID)' deployed" || echo "policy deploy failed"
	@kill $$(cat /tmp/zk-puoi-port-forward.pid) 2>/dev/null; rm -f /tmp/zk-puoi-port-forward.pid

# Built with the repo root as context (see examples/react-gui/Dockerfile) — the pnpm workspace install needs client-lib's package.json too.
docker-build-gui:
	docker build -f examples/react-gui/Dockerfile -t $(GUI_IMAGE) .

# Same pattern as k8s-apply. Depends on the backend already being applied (shares its namespace).
k8s-gui-apply: docker-build-gui
	kubectl --context $(KUBE_CONTEXT) apply -f k8s/local/backend/namespace.yaml
	kubectl --context $(KUBE_CONTEXT) apply -f k8s/local/gui/deployment.yaml -f k8s/local/gui/service.yaml
	kubectl --context $(KUBE_CONTEXT) set image deployment/zk-puoi-gui zk-puoi-gui=$(GUI_IMAGE) -n $(NAMESPACE)
	kubectl --context $(KUBE_CONTEXT) rollout status deployment/zk-puoi-gui -n $(NAMESPACE)

# Leaves the shared "zk-puoi" namespace alone — k8s-delete (backend) owns it.
k8s-gui-delete:
	kubectl --context $(KUBE_CONTEXT) delete -f k8s/local/gui/deployment.yaml -f k8s/local/gui/service.yaml --ignore-not-found

k8s-gui-restart:
	kubectl --context $(KUBE_CONTEXT) rollout restart deployment/zk-puoi-gui -n $(NAMESPACE)

k8s-gui-logs:
	kubectl --context $(KUBE_CONTEXT) logs -n $(NAMESPACE) deploy/zk-puoi-gui -f

# Fallback only — zk-puoi-gui is a LoadBalancer Service, already auto-forwarded to localhost.
k8s-gui-port-forward:
	kubectl --context $(KUBE_CONTEXT) port-forward -n $(NAMESPACE) svc/zk-puoi-gui $(GUI_LOCAL_PORT):80
