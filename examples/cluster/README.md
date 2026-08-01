# mcgate Cluster Example (Kubernetes)

This example deploys the mcgate gateway in a **3-node cluster** on
Kubernetes (local [Kind](https://kind.sigs.k8s.io/) cluster). The three
gateway replicas form a GoAkt actor cluster: the tool registry, circuit
breakers, and session actors are distributed across all nodes. Any replica
can handle any MCP request regardless of which node owns the underlying actor.

## Architecture

```
Internet
    │
    ▼
 nginx (NodePort :30051)
    │
    ▼
gateway-http (ClusterIP :8080)   ← K8s load-balances only to ready pods
    │
    ├── gateway-0 (StatefulSet pod)
    ├── gateway-1 (StatefulSet pod)  ← GoAkt actor cluster (gossip + remoting)
    └── gateway-2 (StatefulSet pod)
              │
              ▼
       mcp-everything:3001  ← HTTP MCP backend (server-everything)
              │
       otel-collector:4318  ← OpenTelemetry traces
              │
         Jaeger UI :16686
```

### StatefulSet DNS Discovery

Each gateway pod is reachable in the cluster at a stable DNS name:

```
gateway-{0,1,2}.gateway.default.svc.cluster.local
```

The `KubernetesDiscovery` provider in [discovery.go](discovery.go) wraps
GoAkt's built-in Kubernetes discovery. It queries the k8s API with a label
selector to find Running+Ready peers, so the peer list is fully dynamic.
A `ClusterRole` granting `pods: get/watch/list` is included in
[k8s/k8s.yaml](k8s/k8s.yaml).

## Prerequisites

| Tool                                                                 | Purpose                  |
|----------------------------------------------------------------------|--------------------------|
| [Kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation) | Local Kubernetes cluster |
| [kubectl](https://kubernetes.io/docs/tasks/tools/)                   | Cluster management       |
| [Docker](https://docs.docker.com/get-docker/)                        | Image build              |
| curl                                                                 | `make test`              |

## Quick Start

```bash
# Run all commands from examples/cluster/

# 1. Create a local Kubernetes cluster (one-time)
make cluster-create

# 2. Build the gateway image and load it into Kind
make image

# 3. Deploy the full stack
make cluster-up

# 4. In a separate terminal: expose the gateway on localhost:8080
make port-forward

# 5. Run a quick smoke test
make test

# 6. (Optional) View traces in Jaeger
make port-forward-jaeger   # then open http://localhost:16686

# 7. Tear everything down
make cluster-down
```

Or run steps 2–3 together:

```bash
make deploy
```

## Environment Variables

| Variable                      | Default             | Description                                                  |
|-------------------------------|---------------------|--------------------------------------------------------------|
| `HTTP_ADDR`                   | `:8080`             | HTTP listen address                                          |
| `CLUSTER_ENABLED`             | `false`             | Enable GoAkt actor clustering (set to `true` in k8s)         |
| `NAMESPACE`                   | `default`           | Kubernetes namespace to search for peers                     |
| `DISCOVERY_PORT_NAME`         | `discovery-port`    | Named port on the pod used for gossip discovery              |
| `PEERS_PORT_NAME`             | `peers-port`        | Named port on the pod used for memberlist                    |
| `REMOTING_PORT_NAME`          | `remoting-port`     | Named port on the pod used for actor remoting                |
| `DISCOVERY_PORT`              | `3322`              | Numeric discovery port                                       |
| `PEERS_PORT`                  | `3320`              | Numeric peers port                                           |
| `REMOTING_PORT`               | `3321`              | Numeric remoting port                                        |
| `POD_LABEL_NAME`              | `gateway`           | `app.kubernetes.io/name` label for peer discovery            |
| `POD_LABEL_COMPONENT`         | `MCPGateway`        | `app.kubernetes.io/component` label for peer discovery       |
| `POD_LABEL_PART_OF`           | `mcgate-cluster` | `app.kubernetes.io/part-of` label for peer discovery         |
| `MCP_TOOL_URL`                | _(empty)_           | HTTP MCP backend URL (e.g. `http://mcp-everything:3001/mcp`) |
| `DEFAULT_TENANT`              | `default`           | Tenant ID for requests without `X-Tenant-ID`                 |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | _(empty)_           | OTLP endpoint; enables tracing when set                      |

## Local Single-Node Run

> **Note:** When using `make cluster-up`, the MCP tool server (`mcp-everything`)
> is deployed automatically as a Kubernetes pod — you do **not** need to run
> `npx` manually. The steps below are only for running the gateway locally
> without a Kubernetes cluster.

Clustering is disabled by default (`CLUSTER_ENABLED=false`), so you only need
to start a local MCP backend and point `MCP_TOOL_URL` at it:

```bash
# Terminal 1 — start a local MCP tool server (requires Node.js / npx)
npx -y @modelcontextprotocol/server-everything streamableHttp --port 3001

# Terminal 2 — start the gateway (single-node, cluster disabled by default)
MCP_TOOL_URL=http://localhost:3001/mcp go run ./examples/cluster
```

Then test it:

```bash
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -H "X-Tenant-ID: default" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'
```

## Kubernetes Manifests

| File                                 | Purpose                                                          |
|--------------------------------------|------------------------------------------------------------------|
| `k8s/kind-config.yaml`               | Kind single-node cluster config                                  |
| `k8s/k8s.yaml`                       | StatefulSet, headless service, ClusterIP service, mcp-everything |
| `k8s/nginx-config.yaml`              | Nginx reverse proxy config (SSE-aware)                           |
| `k8s/nginx-deployment.yaml`          | Nginx deployment                                                 |
| `k8s/nginx-service.yaml`             | NodePort service (external access on :30051)                     |
| `k8s/otel-collector-config.yaml`     | OTEL collector pipeline                                          |
| `k8s/otel-collector-deployment.yaml` | OTEL collector deployment + service                              |
| `k8s/jaeger-deployment.yaml`         | Jaeger all-in-one deployment + service                           |

## MCP Endpoint

Once `make port-forward` is running:

| Endpoint                       | Description                             |
|--------------------------------|-----------------------------------------|
| `http://localhost:8080/mcp`    | MCP Streamable HTTP (POST)              |
| `http://localhost:8080/health` | Health check                            |
| `http://localhost:16686`       | Jaeger trace UI (separate port-forward) |

Pass `X-Tenant-ID: default` (or any tenant ID configured in `DEFAULT_TENANT`)
on each request. Requests without the header are automatically attributed to
the default tenant.

## How Clustering Works

1. Each gateway pod starts the GoAkt actor system with `ClusterConfig.Enabled = true`.
2. `StatefulSetDiscovery.DiscoverPeers` returns all pod DNS names; GoAkt uses
   gossip (memberlist) on `PEERS_PORT` to maintain cluster membership.
3. The tool registry singleton is distributed across the cluster via GoAkt's
   `SpawnSingleton`; all nodes see the same registered tools.
4. When a request arrives on any node, GoAkt's location-transparent routing
   forwards the message to whichever node owns the session actor.
5. Nginx load-balances HTTP requests across all ready pods, so clients can
   connect to any node.
