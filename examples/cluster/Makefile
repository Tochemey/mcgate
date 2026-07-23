# Makefile for the portcullis cluster example using Kind (Kubernetes in Docker).
#
# Prerequisites:
#   - Kind:    https://kind.sigs.k8s.io/docs/user/quick-start/#installation
#   - kubectl: https://kubernetes.io/docs/tasks/tools/
#   - Docker:  https://docs.docker.com/get-docker/
#   - curl:    for the test target
#
# Quick start:
#   make cluster-create    # Create Kind cluster (one-time)
#   make image             # Build and load image into Kind
#   make cluster-up        # Deploy all components
#   make port-forward      # Expose nginx on localhost:8080
#   make test              # Send a test tool invocation
#   make cluster-down      # Tear down deployments
#
# Run from: examples/cluster/

.DEFAULT_GOAL := help

CLUSTER_NAME ?= portcullis-cluster
IMAGE_NAME   ?= gateway:dev-cluster
K8S_DIR      := ./k8s
REPO_ROOT    := ../..

.PHONY: help
help:
	@echo "portcullis Cluster Example — Kind (Kubernetes in Docker)"
	@echo ""
	@echo "Targets:"
	@echo "  deploy               Build image, load into Kind, and deploy (recommended)"
	@echo "  cluster-create       Create a new Kind cluster"
	@echo "  cluster-recreate     Delete and recreate the cluster"
	@echo "  cluster-delete       Delete the Kind cluster"
	@echo "  image                Build the Docker image and load into Kind"
	@echo "  cluster-up           Deploy tool server, gateway cluster, nginx, and tracing"
	@echo "  cluster-down         Remove all deployments"
	@echo "  status               Show cluster and pod status"
	@echo "  logs                 Tail logs from gateway pods"
	@echo "  port-forward         Forward nginx service to localhost:8080"
	@echo "  port-forward-jaeger  Forward Jaeger UI to localhost:16686"
	@echo "  test                 Send test MCP tool invocations (platform-admin tools/list and partner-api policy denial)"
	@echo "  test-resilience      Verify tool access, kill a gateway node, re-verify"
	@echo "  dashboard-install    Install the Kubernetes dashboard (one-time)"
	@echo "  dashboard            Open the Kubernetes dashboard in the browser"

.PHONY: cluster-create
cluster-create:
	@echo "Creating Kind cluster '$(CLUSTER_NAME)'..."
	kind create cluster --name $(CLUSTER_NAME) --config $(K8S_DIR)/kind-config.yaml --wait 2m
	@echo "Kind cluster created. Run 'make deploy' to deploy."

.PHONY: cluster-delete
cluster-delete:
	@echo "Deleting Kind cluster '$(CLUSTER_NAME)'..."
	kind delete cluster --name $(CLUSTER_NAME)

.PHONY: cluster-recreate
cluster-recreate: cluster-delete cluster-create

.PHONY: image
image:
	@echo "Building Docker image (build context: $(REPO_ROOT))..."
	docker build -f ./Dockerfile -t $(IMAGE_NAME) $(REPO_ROOT)
	@echo "Loading image into Kind cluster..."
	kind load docker-image $(IMAGE_NAME) --name $(CLUSTER_NAME)
	@echo "Image loaded."

.PHONY: deploy
deploy: image cluster-up

.PHONY: cluster-up
cluster-up:
	@echo "Deploying tracing stack (Jaeger + OTEL Collector)..."
	kubectl apply -f $(K8S_DIR)/jaeger-deployment.yaml
	kubectl create configmap otel-collector-config \
	  --from-file=config.yaml=$(K8S_DIR)/otel-collector-config.yaml \
	  --dry-run=client -o yaml | kubectl apply -f -
	kubectl apply -f $(K8S_DIR)/otel-collector-deployment.yaml
	@echo "Waiting for OTEL Collector to be ready..."
	kubectl wait --for=condition=available --timeout=60s deployment/otel-collector
	@echo "Deploying MCP tool server (mcp-everything)..."
	kubectl apply -f $(K8S_DIR)/tools.yaml
	@echo "Waiting for mcp-everything to be ready (npx downloads the package on first run)..."
	kubectl wait --for=condition=available --timeout=180s deployment/mcp-everything
	@echo "Deploying gateway cluster (RBAC + StatefulSet)..."
	kubectl apply -f $(K8S_DIR)/gateway.yaml
	@echo "Waiting for gateway pods to be ready..."
	@for i in 0 1 2; do \
		echo "  Waiting for gateway-$$i..."; \
		elapsed=0; \
		until [ "$$(kubectl get pod gateway-$$i -o jsonpath='{.status.containerStatuses[?(@.name=="gateway")].ready}' 2>/dev/null)" = "true" ]; do \
			sleep 5; elapsed=$$((elapsed+5)); \
			if [ $$elapsed -ge 300 ]; then \
				echo "  Timeout waiting for gateway-$$i. Pod status:"; \
				kubectl get pod gateway-$$i -o wide 2>/dev/null; \
				kubectl describe pod gateway-$$i 2>/dev/null | tail -20; \
				exit 1; \
			fi; \
		done; \
		echo "  gateway-$$i is ready"; \
	done
	@echo "Deploying nginx ingress..."
	kubectl apply -f $(K8S_DIR)/nginx-config.yaml
	kubectl apply -f $(K8S_DIR)/nginx-deployment.yaml
	kubectl apply -f $(K8S_DIR)/nginx-service.yaml
	@echo ""
	@echo "All components deployed."
	@echo "Run 'make port-forward' to access the MCP gateway at http://localhost:8080/mcp"

.PHONY: cluster-down
cluster-down:
	@echo "Removing deployments..."
	kubectl delete -f $(K8S_DIR)/nginx-service.yaml --ignore-not-found
	kubectl delete -f $(K8S_DIR)/nginx-deployment.yaml --ignore-not-found
	kubectl delete -f $(K8S_DIR)/nginx-config.yaml --ignore-not-found
	@# Scale gateway to 0 first so OrderedReady terminates pods one at a time
	@# (gateway-2 → gateway-1 → gateway-0). Each departing node still has live
	@# peers to replicate actor state to, preventing the simultaneous-shutdown
	@# "failed to replicate state to any peer" error.
	-kubectl scale statefulset/gateway --replicas=0 --ignore-not-found=true 2>/dev/null || true
	@echo "Waiting for gateway pods to terminate (ordered)..."
	-kubectl wait pod -l app.kubernetes.io/name=gateway --for=delete --timeout=90s 2>/dev/null || true
	kubectl delete -f $(K8S_DIR)/gateway.yaml --ignore-not-found
	kubectl delete clusterrolebinding gateway-discovery --ignore-not-found
	kubectl delete clusterrole gateway-discovery --ignore-not-found
	kubectl delete -f $(K8S_DIR)/tools.yaml --ignore-not-found
	kubectl delete -f $(K8S_DIR)/otel-collector-deployment.yaml --ignore-not-found
	kubectl delete configmap otel-collector-config --ignore-not-found
	kubectl delete -f $(K8S_DIR)/jaeger-deployment.yaml --ignore-not-found
	@echo "Deployments removed."

.PHONY: status
status:
	@echo "=== Cluster Info ==="
	kubectl cluster-info --context kind-$(CLUSTER_NAME)
	@echo ""
	@echo "=== Pods ==="
	kubectl get pods -o wide
	@echo ""
	@echo "=== Services ==="
	kubectl get svc

.PHONY: logs
logs:
	kubectl logs -l app.kubernetes.io/name=gateway -f --tail=1000

.PHONY: port-forward
port-forward:
	@echo "Waiting for nginx to be ready..."
	kubectl wait --for=condition=available --timeout=60s deployment/nginx
	@echo "Forwarding nginx (port 80) to localhost:8080..."
	@echo "MCP endpoint: http://localhost:8080/mcp"
	@echo "Health check: http://localhost:8080/health"
	kubectl port-forward service/nginx 8080:80

.PHONY: port-forward-jaeger
port-forward-jaeger:
	@echo "Forwarding Jaeger UI to localhost:16686..."
	@echo "Jaeger UI: http://localhost:16686"
	kubectl port-forward service/jaeger-collector 16686:16686

.PHONY: test
test:
	@echo "=== Health check ==="
	curl -sf http://localhost:8080/health && echo " (ok)" || echo " (FAILED — is port-forward running?)"
	@echo ""
	@echo "=== platform-admin: tools list ==="
	@# MCP Streamable HTTP requires initialize → initialized → request.
	@# Step 1: initialize and capture the session ID from the response header.
	@SESSION=$$(curl -sf -X POST http://localhost:8080/mcp \
	  -H "Content-Type: application/json" \
	  -H "X-Tenant-ID: platform-admin" \
	  -H "X-Client-ID: admin-cli" \
	  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test-client","version":"1.0"}}}' \
	  -D - 2>/dev/null | grep -i "^mcp-session-id:" | tr -d '\r\n' | awk -F': ' '{print $$2}'); \
	if [ -z "$$SESSION" ]; then echo "ERROR: no session ID returned"; exit 1; fi; \
	echo "  session=$$SESSION"; \
	curl -sf -X POST http://localhost:8080/mcp \
	  -H "Content-Type: application/json" \
	  -H "X-Tenant-ID: platform-admin" \
	  -H "X-Client-ID: admin-cli" \
	  -H "Mcp-Session-Id: $$SESSION" \
	  -d '{"jsonrpc":"2.0","method":"initialized","params":{}}' > /dev/null; \
	curl -sf -X POST http://localhost:8080/mcp \
	  -H "Content-Type: application/json" \
	  -H "X-Tenant-ID: platform-admin" \
	  -H "X-Client-ID: admin-cli" \
	  -H "Mcp-Session-Id: $$SESSION" \
	  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' \
	  | head -c 2000 && echo "" || true
	@echo ""
	@echo "=== partner-api: invoke 'echo' (policy should deny — gateway tool 'everything' has no read- prefix) ==="
	@SESSION=$$(curl -sf -X POST http://localhost:8080/mcp \
	  -H "Content-Type: application/json" \
	  -H "X-Tenant-ID: partner-api" \
	  -H "X-Client-ID: partner-client" \
	  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test-client","version":"1.0"}}}' \
	  -D - 2>/dev/null | grep -i "^mcp-session-id:" | tr -d '\r\n' | awk -F': ' '{print $$2}'); \
	if [ -z "$$SESSION" ]; then echo "ERROR: no session ID returned"; exit 1; fi; \
	echo "  session=$$SESSION"; \
	curl -sf -X POST http://localhost:8080/mcp \
	  -H "Content-Type: application/json" \
	  -H "X-Tenant-ID: partner-api" \
	  -H "X-Client-ID: partner-client" \
	  -H "Mcp-Session-Id: $$SESSION" \
	  -d '{"jsonrpc":"2.0","method":"initialized","params":{}}' > /dev/null; \
	curl -sf -X POST http://localhost:8080/mcp \
	  -H "Content-Type: application/json" \
	  -H "X-Tenant-ID: partner-api" \
	  -H "X-Client-ID: partner-client" \
	  -H "Mcp-Session-Id: $$SESSION" \
	  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"echo","arguments":{"message":"hello"}}}' \
	  | head -c 1000 && echo "" || true

.PHONY: test-resilience
test-resilience:
	@echo "=== Resilience test: list tools, kill gateway-1, re-list ==="
	@echo "--- Before: tools list via platform-admin ---"
	@SESSION=$$(curl -sf -X POST http://localhost:8080/mcp \
	  -H "Content-Type: application/json" \
	  -H "X-Tenant-ID: platform-admin" \
	  -H "X-Client-ID: admin-cli" \
	  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test-client","version":"1.0"}}}' \
	  -D - 2>/dev/null | grep -i "^mcp-session-id:" | tr -d '\r\n' | awk -F': ' '{print $$2}'); \
	if [ -z "$$SESSION" ]; then echo "ERROR: no session ID returned"; exit 1; fi; \
	echo "  session=$$SESSION"; \
	curl -sf -X POST http://localhost:8080/mcp \
	  -H "Content-Type: application/json" \
	  -H "X-Tenant-ID: platform-admin" \
	  -H "X-Client-ID: admin-cli" \
	  -H "Mcp-Session-Id: $$SESSION" \
	  -d '{"jsonrpc":"2.0","method":"initialized","params":{}}' > /dev/null; \
	curl -sf -X POST http://localhost:8080/mcp \
	  -H "Content-Type: application/json" \
	  -H "X-Tenant-ID: platform-admin" \
	  -H "X-Client-ID: admin-cli" \
	  -H "Mcp-Session-Id: $$SESSION" \
	  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' \
	  | head -c 500 && echo "" || true
	@echo ""
	@echo "--- Deleting gateway-1 pod ---"
	kubectl delete pod gateway-1
	@echo "--- Waiting 5 seconds for cluster to rebalance ---"
	sleep 5
	@echo "--- After: tools list still works via remaining nodes ---"
	@SESSION=$$(curl -sf -X POST http://localhost:8080/mcp \
	  -H "Content-Type: application/json" \
	  -H "X-Tenant-ID: platform-admin" \
	  -H "X-Client-ID: admin-cli" \
	  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test-client","version":"1.0"}}}' \
	  -D - 2>/dev/null | grep -i "^mcp-session-id:" | tr -d '\r\n' | awk -F': ' '{print $$2}'); \
	if [ -z "$$SESSION" ]; then echo "ERROR: no session ID returned"; exit 1; fi; \
	echo "  session=$$SESSION"; \
	curl -sf -X POST http://localhost:8080/mcp \
	  -H "Content-Type: application/json" \
	  -H "X-Tenant-ID: platform-admin" \
	  -H "X-Client-ID: admin-cli" \
	  -H "Mcp-Session-Id: $$SESSION" \
	  -d '{"jsonrpc":"2.0","method":"initialized","params":{}}' > /dev/null; \
	curl -sf -X POST http://localhost:8080/mcp \
	  -H "Content-Type: application/json" \
	  -H "X-Tenant-ID: platform-admin" \
	  -H "X-Client-ID: admin-cli" \
	  -H "Mcp-Session-Id: $$SESSION" \
	  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' \
	  | head -c 500 && echo "" || true
	@echo ""
	@echo "--- gateway-1 will be rescheduled automatically by the StatefulSet ---"
	kubectl get pods -l app.kubernetes.io/name=gateway

.PHONY: dashboard-install
dashboard-install:
	@echo "Installing Kubernetes dashboard..."
	kubectl apply -f https://raw.githubusercontent.com/kubernetes/dashboard/v2.7.0/aio/deploy/recommended.yaml
	@echo "Creating dashboard admin service account..."
	-kubectl create serviceaccount dashboard-admin -n kubernetes-dashboard 2>/dev/null || true
	-kubectl create clusterrolebinding dashboard-admin \
	  --clusterrole=cluster-admin \
	  --serviceaccount=kubernetes-dashboard:dashboard-admin 2>/dev/null || true
	@echo "Waiting for dashboard to be ready..."
	kubectl wait --for=condition=available --timeout=120s deployment/kubernetes-dashboard -n kubernetes-dashboard
	@echo "Dashboard installed."

.PHONY: dashboard
dashboard: dashboard-install
	@echo ""
	@echo "=========================================="
	@echo "Kubernetes Dashboard"
	@echo "=========================================="
	@echo "URL: http://localhost:8001/api/v1/namespaces/kubernetes-dashboard/services/https:kubernetes-dashboard:/proxy/"
	@echo ""
	@echo "Login token (copy and paste at the dashboard login page):"
	@kubectl -n kubernetes-dashboard create token dashboard-admin 2>/dev/null || \
	  kubectl -n kubernetes-dashboard get secret \
	    $$(kubectl -n kubernetes-dashboard get sa dashboard-admin -o jsonpath='{.secrets[0].name}') \
	    -o jsonpath='{.data.token}' 2>/dev/null | base64 -d; echo
	@echo ""
	@echo "Starting kubectl proxy (Ctrl+C to stop)..."
	kubectl proxy
