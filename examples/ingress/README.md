# Ingress Example

This example demonstrates the **MCP Streamable HTTP ingress** layer of portcullis.

## What It Covers

| Feature              | Details                                                                             |
|----------------------|-------------------------------------------------------------------------------------|
| **IdentityResolver** | Reads `X-Tenant-ID` and `X-Client-ID` headers; rejects sessions with missing tenant |
| **Gateway.Handler**  | Returns an `http.Handler` for MCP Streamable HTTP; mounted at `/mcp`                |
| **net/http server**  | Standard `http.Server` with the gateway handler mounted                             |
| **MCP client**       | `sdkmcp.StreamableClientTransport` connecting to the local server                   |
| **Header injection** | Custom `http.RoundTripper` that adds identity headers to every client request       |
| **ListTools**        | Lists tools visible in the MCP session                                              |
| **CallTool**         | Calls the `filesystem` tool through the HTTP ingress end-to-end                     |

## Architecture

```
MCP client (go-sdk)
  │  POST /mcp  +  X-Tenant-ID / X-Client-ID headers
  ▼
Gateway.Handler (mcp.IngressConfig)
  │  IdentityResolver extracts tenant + client
  ▼
portcullis Gateway (actor system)
  │  routes invocation to filesystem tool supervisor
  ▼
npx @modelcontextprotocol/server-filesystem (child process)
```

## Prerequisites

- **Go** 1.21+
- **Node.js** and **npx**

## How to Run

From the **repository root**:

```bash
go run ./examples/ingress
# or
make -C examples/ingress run
```

From **anywhere**:

```bash
go run github.com/tochemey/portcullis/examples/ingress
```

## Environment Variables

| Variable        | Description                                   | Default                     |
|-----------------|-----------------------------------------------|-----------------------------|
| `MCP_FS_ROOT`   | Filesystem root for the MCP server            | `.` (current directory)     |
| `MCP_ADDR`      | `host:port` for the HTTP server               | `127.0.0.1:0` (random port) |
| `MCP_TENANT_ID` | Tenant identity injected into client requests | `ingress-tenant`            |
| `MCP_CLIENT_ID` | Client identity injected into client requests | `ingress-client`            |

## What You'll See

1. The listening address and identity headers used by the client.
2. The list of tools advertised to the MCP session.
3. The result of calling the `list_directory` tool through the HTTP ingress.

## Related

- [portcullis README](../../README.md) — `Gateway.Handler` API reference and `IngressConfig` fields.
- [filesystem example](../filesystem/README.md) — Minimal stdio-only example (no HTTP ingress).
- [full-config example](../full-config/README.md) — Comprehensive config with audit, credentials, telemetry.
