IMAGE_REPO := zk-puoi-backend
# A fresh tag per build, not a reused "dev" tag: Docker Desktop's Kubernetes has been observed keeping pods on stale image content under a reused tag
# even after a real rebuild (imagePullPolicy: IfNotPresent plus whatever caching containerd does for "already present" tags) — a unique tag every time
# sidesteps that entirely.
IMAGE := $(IMAGE_REPO):dev-$(shell date +%Y%m%d%H%M%S)
NAMESPACE := zk-puoi
KUBE_CONTEXT := docker-desktop
# Must match internal/config's default, or ZKPUOI_ADMIN_TOKEN if that's been overridden.
ADMIN_TOKEN := dev-only-insecure-admin-token
LOCAL_PORT := 18080

# POLICY=path/to/file.gno hot-deploys that policy into the running backend (see k8s-deploy-policy). POLICY_ID
# defaults to the filename without its .gno extension, e.g. diploma_check.gno -> diploma_check.
POLICY :=
POLICY_ID := $(basename $(notdir $(POLICY)))

.PHONY: build test docker-build k8s-apply k8s-delete k8s-restart k8s-logs k8s-port-forward k8s-deploy-policy

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
	kubectl --context $(KUBE_CONTEXT) apply -f k8s/backend/namespace.yaml
	kubectl --context $(KUBE_CONTEXT) apply -f k8s/backend/deployment.yaml -f k8s/backend/service.yaml
	kubectl --context $(KUBE_CONTEXT) set image deployment/zk-puoi-backend zk-puoi-backend=$(IMAGE) -n $(NAMESPACE)
	kubectl --context $(KUBE_CONTEXT) rollout status deployment/zk-puoi-backend -n $(NAMESPACE)
	@if [ -n "$(POLICY)" ]; then $(MAKE) k8s-deploy-policy POLICY="$(POLICY)" POLICY_ID="$(POLICY_ID)"; fi

k8s-delete:
	kubectl --context $(KUBE_CONTEXT) delete -f k8s/backend/deployment.yaml -f k8s/backend/service.yaml --ignore-not-found
	kubectl --context $(KUBE_CONTEXT) delete -f k8s/backend/namespace.yaml --ignore-not-found

k8s-restart:
	kubectl --context $(KUBE_CONTEXT) rollout restart deployment/zk-puoi-backend -n $(NAMESPACE)

k8s-logs:
	kubectl --context $(KUBE_CONTEXT) logs -n $(NAMESPACE) deploy/zk-puoi-backend -f

# Exposes the service on localhost:8080 for manual/curl testing against the cluster.
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
