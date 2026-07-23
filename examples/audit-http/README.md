# Audit + HTTP Example

This example demonstrates **durable auditing** and **HTTP-based egress** with portcullis:

- **FileSink** — Invocation events (policy decision, invocation complete/failed, health transitions, circuit state) are written to `audit.log` as NDJSON.
- **HTTP transport** — An MCP tool is reached over HTTP (e.g. `@modelcontextprotocol/server-everything` in streamable HTTP mode).
- **Mixed transports** — One stdio tool (filesystem) and one HTTP tool (everything).

## What It Demonstrates

- **Audit config** — Setting `cfg.Audit.Sink` to a `FileSink` so the JournalActor writes events to disk.
- **HTTP tool** — Configuring a tool with `Transport: mcp.TransportHTTP` and `HTTP: &mcp.HTTPTransportConfig{URL: "..."}`.
- **Invocation flow** — Invoking both the filesystem tool (stdio) and the everything tool (HTTP), then printing the audit log.
- **Audit event types** — `invocation_complete`, `invocation_failed`, `policy_decision`, `health_transition`, `circuit_state_change`.

## Prerequisites

- **Go** 1.21+
- **Node.js** and **npx**

For the HTTP tool to work, run the MCP server in another terminal:

```bash
npx -y @modelcontextprotocol/server-everything streamableHttp
```

This starts the server on port 3001 by default. If the HTTP server is not running, the filesystem invocation will still succeed and audit events will be written; the HTTP invocation will fail with a clear message.

## How to Run

From the **repository root**:

```bash
# Terminal 1: start the HTTP MCP server
npx -y @modelcontextprotocol/server-everything streamableHttp

# Terminal 2: run the example
make -C examples/audit-http run
# or
go run ./examples/audit-http
```

From **anywhere**:

```bash
go run github.com/tochemey/portcullis/examples/audit-http
```

## Environment Variables

| Variable        | Description                                                                |
|-----------------|----------------------------------------------------------------------------|
| `MCP_FS_ROOT`   | Directory root for the filesystem MCP server (default: current directory). |
| `MCP_HTTP_URL`  | URL of the HTTP MCP server (default: `http://localhost:3001/mcp`).         |
| `MCP_AUDIT_DIR` | Directory for the audit log (default: `$TMPDIR/portcullis-audit`).          |

## Makefile Targets

- **run** — Run the example.
- **build** — Build the binary into `bin/audit-http-example`.
- **clean** — Remove the `bin/` directory.

## What You'll See

1. **Log output** from the gateway.
2. **Registered tools** — `filesystem (stdio)` and `everything (http)`.
3. **Filesystem invocation** — Status and result of `list_directory`.
4. **HTTP invocation** — Status and result of `get-sum` (if the HTTP server is running).
5. **Audit log** — The last 20 audit events (type, tenant, tool, outcome, timestamp).

## Code Flow

1. **Audit sink** — Create `FileSink` in the audit directory; pass it to `cfg.Audit.Sink`.
2. **Tools** — Register filesystem (stdio) and everything (HTTP).
3. **Gateway** — Start, invoke both tools, stop.
4. **Audit log** — Close the sink (flush), read `audit.log`, and print the last 20 events.

## Related

- [portcullis README](../../README.md) — Gateway API and configuration.
- [filesystem example](filesystem/README.md) — Simple stdio-only example.
- [MCP server-everything](https://www.npmjs.com/package/@modelcontextprotocol/server-everything) — HTTP MCP server used by this example.
