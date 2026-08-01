# Full-Config Example

This example demonstrates the **majority of the mcgate configuration** in a single runnable program. Use it as a reference when building production-ready gateways.

## Configuration Sections Covered

| Section             | What It Demonstrates                                                                                 |
|---------------------|------------------------------------------------------------------------------------------------------|
| **Runtime**         | Session idle timeout, request timeout, startup/shutdown timeouts, health probe interval              |
| **Telemetry**       | OpenTelemetry OTLP endpoint (optional; set `OTEL_EXPORTER_OTLP_ENDPOINT` to export)                  |
| **Audit**           | Durable `FileSink` for invocation events (policy, invocation complete/failed, health, circuit state) |
| **Credentials**     | Pluggable credential provider (env-based example; set `MCP_DEMO_API_KEY` to exercise)                |
| **Tenants**         | Per-tenant quotas: `RequestsPerMinute`, `ConcurrentSessions`                                         |
| **HealthProbe**     | Health actor probe interval                                                                          |
| **Gateway options** | `WithLogger`, `WithMetrics`, `WithTracing`                                                           |
| **Tools**           | Mixed stdio (filesystem) and HTTP (everything) transports                                            |

## Cluster (Commented)

The example includes a commented `Cluster` config block showing the structure for Kubernetes or DNS-SD discovery. Cluster mode requires actual infrastructure (pods, DNS records) and is not enabled in this example.

## Prerequisites

- **Go** 1.21+
- **Node.js** and **npx**

For the HTTP tool, run the MCP server in another terminal:

```bash
npx -y @modelcontextprotocol/server-everything streamableHttp
```

## How to Run

From the **repository root**:

```bash
# Terminal 1: start the HTTP MCP server (optional)
npx -y @modelcontextprotocol/server-everything streamableHttp

# Terminal 2: run the example
make -C examples/full-config run
# or
go run ./examples/full-config
```

From **anywhere**:

```bash
go run github.com/tochemey/mcgate/examples/full-config
```

## Environment Variables

| Variable                      | Description                                                                |
|-------------------------------|----------------------------------------------------------------------------|
| `MCP_FS_ROOT`                 | Directory root for the filesystem MCP server (default: current directory). |
| `MCP_HTTP_URL`                | URL of the HTTP MCP server (default: `http://localhost:3001/mcp`).         |
| `MCP_AUDIT_DIR`               | Directory for the audit log (default: `$TMPDIR/mcgate-audit`).          |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OTLP endpoint for OpenTelemetry export (optional).                         |
| `MCP_DEMO_API_KEY`            | Demo API key for the env credential provider (optional).                   |

## Makefile Targets

- **run** — Run the example.
- **build** — Build the binary into `bin/full-config-example`.
- **clean** — Remove the `bin/` directory.

## What You'll See

1. **Config summary** — Runtime timeouts, tenant quotas, audit path, optional OTLP endpoint.
2. **Registered tools** — `filesystem (stdio)` and `everything (http)` (if HTTP server is running).
3. **Filesystem invocation** — Result of `list_directory`.
4. **HTTP invocation** — Result of `get-sum` (if the HTTP server is running).
5. **Audit log** — Last 10 audit events (type, tenant, tool, outcome, timestamp).

## Related

- [mcgate README](../../README.md) — Gateway API and full configuration reference.
- [filesystem example](../filesystem/README.md) — Minimal stdio-only example.
- [audit-http example](../audit-http/README.md) — Audit + HTTP transports.
