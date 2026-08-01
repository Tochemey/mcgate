# Quota-Assess Example

This example efficiently assesses **tenant quota enforcement** in a real-world scenario. It configures a tenant with tight limits (RequestsPerMinute=5, ConcurrentSessions=2), then runs concurrent and sequential invocations to verify that the gateway correctly throttles excess requests.

## What It Demonstrates

- **ConcurrentSessions** — Phase 1 fires 4 concurrent invocations with different client IDs. Expect 2 allowed, 2 throttled (CONCURRENCY_LIMIT_REACHED).
- **RequestsPerMinute** — Phase 2 fires 7 sequential invocations with the same client. Expect 5 allowed, 2 throttled (RATE_LIMITED).
- **Summary** — Prints allowed vs throttled counts and ✓/⚠ for each phase.

## Prerequisites

- **Go** 1.21+
- **Node.js** and **npx**

## How to Run

From the **repository root**:

```bash
make -C examples/quota-assess run
# or
go run ./examples/quota-assess
```

From **anywhere**:

```bash
go run github.com/tochemey/mcgate/examples/quota-assess
```

## Environment Variables

| Variable      | Description                                                                |
|---------------|----------------------------------------------------------------------------|
| `MCP_FS_ROOT` | Directory root for the filesystem MCP server (default: current directory). |

## Makefile Targets

- **run** — Run the quota assessment.
- **build** — Build the binary into `bin/quota-assess-example`.
- **clean** — Remove the `bin/` directory.

## Related

- [mcgate README](../../README.md) — Gateway API and configuration.
- [full-config example](../full-config/README.md) — Comprehensive configuration example.
- [filesystem example](../filesystem/README.md) — Minimal stdio-only example.
