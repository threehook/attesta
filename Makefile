IMAGE := zk-puoi-backend:dev
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
# loop.
docker-build:
	docker build -t $(IMAGE) backend

# Re-applies manifests and forces a rollout restart so freshly built pods pick up the image that was just rebuilt under the same tag (imagePullPolicy:
# IfNotPresent would otherwise keep running whatever was already pulled for that tag). Add POLICY=path/to/file.gno to also hot-deploy that policy
# once the rollout is ready, e.g.: make k8s-apply POLICY=backend/policies/diploma_check.gno
k8s-apply: docker-build
	kubectl --context $(KUBE_CONTEXT) apply -f k8s/backend/namespace.yaml
	kubectl --context $(KUBE_CONTEXT) apply -f k8s/backend/deployment.yaml -f k8s/backend/service.yaml
	kubectl --context $(KUBE_CONTEXT) rollout restart deployment/zk-puoi-backend -n $(NAMESPACE)
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
