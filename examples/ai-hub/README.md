# AI Hub Example

A real-world multi-tenant AI tool hub that runs the mcgate gateway as an
operational control plane. This is the most comprehensive example in the
repository — run it to see every major feature in action in a single program.

## Scenario

A platform team operates an AI tool hub accessible to two tenant categories:

| Tenant           | Description                                                  | Custom Evaluator         |
|------------------|--------------------------------------------------------------|--------------------------|
| `platform-admin` | Internal operators — full access, high rate limits           | None                     |
| `partner-api`    | External API partners — restricted to `read-` prefixed tools | `partnerPolicyEvaluator` |

## Features Covered

| Feature               | Details                                                                               |
|-----------------------|---------------------------------------------------------------------------------------|
| **Egress — stdio**    | `filesystem` tool via `npx @modelcontextprotocol/server-filesystem`                   |
| **Egress — HTTP**     | `everything` tool via `npx @modelcontextprotocol/server-everything` (optional)        |
| **Ingress**           | `Gateway.Handler` with `headerIdentityResolver` reading `X-Tenant-ID` / `X-Client-ID` |
| **Pluggable Policy**  | `partnerPolicyEvaluator` denies non-`read-` tools for `partner-api`                   |
| **Credential Broker** | `envAPIKeyProvider` injects API keys from `MCP_APIKEY_<TOOL>` env vars                |
| **Audit**             | `FileSink` writes NDJSON events to `<MCP_AUDIT_DIR>/audit.log`                        |
| **Runtime config**    | Session idle timeout, request timeout, startup/shutdown timeouts, health probe        |
| **Telemetry**         | `WithMetrics()` + `WithTracing()` (exports when `OTEL_EXPORTER_OTLP_ENDPOINT` is set) |
| **Admin — status**    | `GetGatewayStatus`, `GetToolStatus` before and after invocations                      |
| **Admin — sessions**  | `ListSessions` shows active sessions                                                  |
| **Admin — drain**     | `DrainTool` stops new sessions; status reflects `draining=true`                       |
| **Admin — circuit**   | `ResetCircuit` closes the circuit breaker; status reflects `circuit=closed`           |
| **MCP client**        | `StreamableClientTransport` + `headerInjectTransport` connecting through the ingress  |

## Program Flow

```
A. Admin API — startup snapshot
   GetGatewayStatus, GetToolStatus per tool, ListSessions

B. Ingress — mount Gateway.Handler at /mcp on a local HTTP server

C. Egress — direct invocations via gw.Invoke (platform-admin)
   filesystem (stdio) → list_directory
   everything (HTTP)  → get-sum  [if server-everything is running]

D. Ingress — MCP client over HTTP (platform-admin)
   Connect → ListTools → CallTool (filesystem list_directory)

E. Pluggable Policy — partner-api denied by partnerPolicyEvaluator
   filesystem call → DENIED (no read- prefix)

F. Admin API — operational control
   GetGatewayStatus (post-invocation session counts)
   ListSessions
   DrainTool("filesystem")  → draining=true
   ResetCircuit("filesystem") → circuit=closed

G. Audit log — last 15 events from FileSink
```

## Prerequisites

- **Go** 1.21+
- **Node.js** and **npx**

For the optional HTTP tool, run in a separate terminal:

```bash
npx -y @modelcontextprotocol/server-everything streamableHttp
```

## How to Run

From the **repository root**:

```bash
go run ./examples/ai-hub
# or
make -C examples/ai-hub run
```

From **anywhere**:

```bash
go run github.com/tochemey/mcgate/examples/ai-hub
```

## Environment Variables

| Variable                      | Description                                                       | Default                     |
|-------------------------------|-------------------------------------------------------------------|-----------------------------|
| `MCP_FS_ROOT`                 | Filesystem root for the stdio MCP server                          | `.` (current directory)     |
| `MCP_HTTP_URL`                | URL of the HTTP MCP server                                        | `http://localhost:3001/mcp` |
| `MCP_AUDIT_DIR`               | Directory for the audit log                                       | `$TMPDIR/mcgate-ai-hub`  |
| `MCP_ADDR`                    | `host:port` for the HTTP ingress server                           | `127.0.0.1:0` (random port) |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OTLP endpoint for OpenTelemetry export                            | (disabled)                  |
| `MCP_APIKEY_FILESYSTEM`       | API key injected by the credential broker for the filesystem tool | (none)                      |
| `MCP_APIKEY_EVERYTHING`       | API key injected by the credential broker for the everything tool | (none)                      |

## Expected Output

```
╔══════════════════════════════════════╗
║   mcgate  ·  AI Tool Hub Example  ║
╚══════════════════════════════════════╝
  Filesystem root : .
  HTTP tool URL   : http://localhost:3001/mcp
  Audit dir       : /tmp/mcgate-ai-hub

A. Admin API — startup snapshot
────────────────────────────────
Gateway: running=true   tools=2  sessions=0
  tool=filesystem     transport=stdio  state=enabled  circuit=closed    draining=false
  tool=everything     transport=http   state=enabled  circuit=closed    draining=false
Active sessions: 0

B. Ingress — MCP Streamable HTTP server
────────────────────────────────────────
MCP endpoint: http://127.0.0.1:<port>/mcp
IdentityResolver: reads X-Tenant-ID and X-Client-ID headers

C. Egress — direct invocation (platform-admin)
───────────────────────────────────────────────
filesystem (direct): status=success    duration=...
everything  (direct): status=success    duration=...  [if running]

D. Ingress — MCP client over HTTP (platform-admin)
────────────────────────────────────────────────────
Session tools: 2
  - filesystem
  - everything
filesystem (ingress): succeeded, content items=1

E. Pluggable Policy — partner-api evaluation
─────────────────────────────────────────────
Invoking filesystem as partner-api (evaluator should deny: no read- prefix) ...
  DENIED (POLICY_DENIED): partner tenant "partner-api" may only call read-only tools ...

F. Admin API — operational control
────────────────────────────────────
Gateway: tools=2  sessions=0
Active sessions: 0 (sessions were already closed)

Draining tool "filesystem" ...
  filesystem: draining=true   circuit=closed

Resetting circuit for "filesystem" ...
  filesystem: draining=true   circuit=closed

G. Audit log (last 15 events)
───────────────────────────────
  ...  INVOCATION_COMPLETE   tenant=platform-admin  tool=filesystem  outcome=success
  ...  POLICY_DENIED         tenant=partner-api     tool=filesystem  outcome=denied
  ...

╔══════════════════════════════╗
║   AI Hub example complete    ║
╚══════════════════════════════╝
```

## Related

- [mcgate README](../../README.md) — Full API reference and configuration guide.
- [ingress example](../ingress/README.md) — Focused ingress walkthrough.
- [admin-policy example](../admin-policy/README.md) — Focused admin API and policy walkthrough.
- [full-config example](../full-config/README.md) — Focused configuration reference.
- [quota-assess example](../quota-assess/README.md) — Built-in quota enforcement.
