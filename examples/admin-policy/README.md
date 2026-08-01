# Admin API + Pluggable Policy Example

This example demonstrates the **Admin/Operational API** and the **Pluggable Policy Engine** of mcgate.

## What It Covers

### Admin / Operational API

| Method             | What the example shows                                                |
|--------------------|-----------------------------------------------------------------------|
| `GetGatewayStatus` | Running state, tool count, session count after startup                |
| `GetToolStatus`    | Circuit state, session count, drain flag for `filesystem`             |
| `ListSessions`     | Active sessions (empty at startup)                                    |
| `DrainTool`        | Stops accepting new sessions; `GetToolStatus.Draining` becomes `true` |
| `ResetCircuit`     | Closes a tripped circuit; `GetToolStatus.Circuit` becomes `closed`    |

### Pluggable Policy Engine

| Tenant             | Evaluator                                                     | Outcome                                       |
|--------------------|---------------------------------------------------------------|-----------------------------------------------|
| `ops-tenant`       | None (default)                                                | All tool calls allowed                        |
| `read-only-tenant` | `readOnlyEvaluator` — only allows tools prefixed with `read-` | `filesystem` call denied with `POLICY_DENIED` |

## Prerequisites

- **Go** 1.21+
- **Node.js** and **npx**

## How to Run

From the **repository root**:

```bash
go run ./examples/admin-policy
# or
make -C examples/admin-policy run
```

From **anywhere**:

```bash
go run github.com/tochemey/mcgate/examples/admin-policy
```

## Environment Variables

| Variable      | Description                        | Default                 |
|---------------|------------------------------------|-------------------------|
| `MCP_FS_ROOT` | Filesystem root for the MCP server | `.` (current directory) |

## What You'll See

**Admin API section:**
1. Gateway status: running, 1 tool, 0 sessions.
2. Tool status: circuit=closed, sessions=0, draining=false.
3. After `DrainTool`: draining=true.
4. After `ResetCircuit`: circuit=closed.

**Pluggable Policy section:**
1. `ops-tenant` invocation: `ALLOWED` — no custom evaluator, built-in checks pass.
2. `read-only-tenant` invocation: `DENIED` — `readOnlyEvaluator` blocks `filesystem` because it doesn't match the `read-` prefix.

## Implementing Your Own PolicyEvaluator

```go
type myEvaluator struct{ opaClient *opa.Client }

func (e *myEvaluator) Evaluate(ctx context.Context, in mcp.PolicyInput) *mcp.RuntimeError {
    allowed, err := e.opaClient.Check(ctx, in.TenantID, in.ToolID)
    if err != nil || !allowed {
        return mcp.NewRuntimeError(mcp.ErrCodePolicyDenied, "OPA denied the request")
    }
    return nil
}
```

Attach it to a tenant in `mcp.Config`:

```go
mcp.TenantConfig{
    ID:        "enterprise",
    Evaluator: &myEvaluator{opaClient: opaClient},
}
```

## Related

- [mcgate README](../../README.md) — Admin API reference and Pluggable Policy Engine docs.
- [quota-assess example](../quota-assess/README.md) — Built-in quota enforcement (RPM and concurrency).
- [full-config example](../full-config/README.md) — Comprehensive config with audit, credentials, telemetry.
