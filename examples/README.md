# mcgate Examples

Concrete, runnable examples demonstrating the mcgate gateway API.

## filesystem

Starts the gateway with the MCP filesystem server, lists registered tools, and invokes `list_directory` to show directory contents. See **[examples/filesystem/README.md](filesystem/README.md)** for a full walkthrough, code flow, and environment variables.

## audit-http

Demonstrates **durable auditing** (FileSink) and **HTTP-based egress** (MCP tool over HTTP). Uses both stdio (filesystem) and HTTP (server-everything) tools, then prints the audit log. See **[examples/audit-http/README.md](audit-http/README.md)** for setup and environment variables.

## full-config

Demonstrates the **majority of the gateway configuration** in one example: Runtime, Telemetry, Audit, Credentials, Tenants, HealthProbe, gateway options (WithMetrics, WithTracing), and mixed stdio + HTTP tools. Use as a reference for production-ready setups. See **[examples/full-config/README.md](full-config/README.md)** for a full walkthrough.

## quota-assess

Efficiently assesses **tenant quota enforcement** in a real-world scenario: tight limits (RPM=5, ConcurrentSessions=2), concurrent invocations to hit session limit, sequential invocations to hit rate limit. See **[examples/quota-assess/README.md](quota-assess/README.md)** for setup and usage.

## ingress

Demonstrates the **MCP Streamable HTTP ingress** layer: implementing `IdentityResolver` to read `X-Tenant-ID` / `X-Client-ID` headers, mounting `Gateway.Handler` on a standard `net/http` server, and connecting an MCP client via `StreamableClientTransport` with header injection. See **[examples/ingress/README.md](ingress/README.md)** for architecture and environment variables.

## admin-policy

Demonstrates the **Admin/Operational API** (`GetGatewayStatus`, `GetToolStatus`, `ListSessions`, `DrainTool`, `ResetCircuit`) and the **Pluggable Policy Engine** (`PolicyEvaluator` that restricts partner tenants to read-only tools). See **[examples/admin-policy/README.md](admin-policy/README.md)** for details and expected output.

## ai-hub

**Real-world multi-tenant AI tool hub** covering the full breadth of the gateway library in a single runnable program: stdio egress, HTTP egress, HTTP ingress with identity resolution, pluggable policy evaluation, credential broker, durable audit, runtime config, telemetry, and the complete admin API. Start here when building a production gateway. See **[examples/ai-hub/README.md](ai-hub/README.md)** for the program flow and expected output.
