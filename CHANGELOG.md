# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **MCP 2026-07-28 (stateless) ingress**: the Streamable HTTP ingress is stateless: every request is self-contained and any gateway node can serve it; clients on the previous protocol revision are served through the same path. Built on MCP Go SDK v1.7.0-pre.1. The tenant-visible tool surface is cached per request cycle so stateless construction stays cheap.
- **MCP resources**: `resources/list`, `resources/templates/list`, and `resources/read` proxied through the full actor pipeline; resource metadata discovered and cached at registration.
- **gRPC ingress** (`MCPToolService`: `ListTools`, `CallTool`, `CallToolStream`) with metadata-based identity resolution, Bearer-token interceptors, tool-name TTL cache, and server-streaming progress.
- **gRPC egress**: invoke gRPC services as tools via proto descriptor sets or server reflection, with dynamic protobuf construction, JSON Schema derivation, mTLS, and streaming.
- **Enterprise-managed authorization** (RFC 9728 + MCP extension) on all ingress transports, with `IdentityMapper`, scope propagation to policy, and RFC 8693 `TokenExchanger` for minting downstream tokens.
- **Credential injection into backends**: resolved credentials are applied as child-process env vars (stdio), request headers (HTTP), and call metadata (gRPC).
- **Router pool** (`Runtime.RouterPoolSize`, default 8): invocations are distributed round-robin so one slow backend call no longer serializes the gateway.
- **Tenant-scoped tool listing**: allowlist-guarded tools are hidden from tenants outside the configured tenant set on every ingress.
- **Audit overflow policy** (`Audit.OverflowPolicy`): `block` (default, lossless backpressure) or `drop_newest` (bounded writer queue that never stalls the request path).
- `StdioTransportConfig.IsolateEnv` to stop tool child processes from inheriting the gateway's full environment; `WSConfig.MaxMessageSize` (default 4 MiB).
- Audit event stream: `Gateway.SubscribeAudit`/`UnsubscribeAudit` for in-process fan-out alongside the `AuditSink`; dead-letter logging and `portcullis.actor.dead_letter` metric.

### Changed

- **Module renamed** from `github.com/tochemey/goakt-mcp` to `github.com/tochemey/portcullis`; the root package is now `portcullis`, the actor system, MCP handshake identities, and OpenTelemetry metric/span prefixes (`portcullis.*`) follow suit.
- Sessions migrated from child actors to GoAkt grains (virtual actors): on-demand activation, idle passivation via the grain engine, executor construction in `OnActivate` (fixing an executor leak), and streaming goroutines coordinated with deactivation.
- Circuit breaker is a first-class `mcp.CircuitBreaker` state machine with generation-correlated outcome reporting (stale results are discarded) and a `Release` path for admissions that never reach the backend; health probes are read-only and never consume probe slots.
- Registrar stores tools in a dedicated `toolCatalog`; re-registering a tool refreshes the running supervisor in place, preserving session accounting and circuit state.
- Router pre-execution pipeline unified across sync and streaming paths with uniform audit/telemetry emission.
- Upgraded to GoAkt v4.4.x and MCP Go SDK v1.7.0-pre.1; migrated off deprecated GoAkt APIs (`GrainOf`, `remote.WithTLS`).
- README rewritten around the stateless-edge / supervised-core positioning.

### Fixed

- Streaming invocations were killed the moment the router handler returned (execution context was cancelled); streams are now detached and bounded by the tool's own timeout.
- Circuit breaker could wedge permanently in half-open: probe slots leaked via health probes, backpressure rejections, and post-admission pipeline failures.
- Client disconnects during streaming no longer count as backend failures (they released a probe slot and tripped breakers); abandoned gRPC streams no longer leak a goroutine and server stream.
- gRPC ingress: `EnterpriseAuth` is now enforced by the handlers themselves; `ListTools` requires authentication and identity; response MIME types were written under the wrong key.
- WebSocket ingress: same-origin policy by default (was allow-all, a cross-site hijacking risk) and bounded message size (was unbounded, an OOM risk).
- The nested `{"name": ...}` call shape can no longer address backend tools outside the advertised schema surface.
- RFC 9728 Protected Resource Metadata URLs are built per spec for resources with path components.
- Binary content survives JSON serialization boundaries in both directions (base64 blob handling).
- Schema/resource discovery follows pagination; discovery transport failures are no longer silently treated as "no resources".
- Health probing was silently disabled with a default config (`HealthProbe.Interval` was never defaulted); probes now have per-tool time budgets, exact state mapping, and propagate state changes to supervisors.
- `Gateway.Start` guards against double-start (previously leaked the first actor system); failed starts no longer leave a live audit stream; `Runtime.ShutdownTimeout` is actually honored by `Stop`.
- Caller-supplied shared `http.Client` is no longer mutated in place (transport double-wrapping and data race).

### Removed

- SSE ingress transport (superseded by the 2026-07-28 stateless spec).
- `IngressConfig.Stateless` and `IngressConfig.SessionIdleTimeout` (the HTTP ingress is stateless-only).

## [v0.1.0] - 2026-03-15

Initial release of portcullis -- a production-ready MCP gateway library for Go, built on the GoAkt actor framework.

### Added

#### Multi-Transport Ingress

- Streamable HTTP handler (`Gateway.Handler`) supporting the MCP 2025-11-25 spec
- Server-Sent Events handler (`Gateway.SSEHandler`) supporting the MCP 2024-11-05 spec
- WebSocket handler (`Gateway.WSHandler`) for full-duplex streaming
- Identity resolution and session management across all transports

#### Multi-Transport Egress

- Stdio executor for child-process tool backends communicating over stdin/stdout
- HTTP executor for remote MCP server connectivity with Streamable HTTP semantics
- Automatic schema discovery and caching from tool backends
- W3C trace-context header propagation on outbound calls

#### Multi-Tenancy and Authorization

- Per-request tenant and client identity resolution via the `IdentityResolver` interface
- Per-tenant quota enforcement with configurable rate limits and concurrency caps
- Two-layer policy evaluation: built-in checks (rate limits, concurrency, tool authorization) and custom `PolicyEvaluator` for context-aware decisions (OPA, ABAC, allowlists)
- Every policy decision recorded in the audit journal

#### Credential Brokering

- Per-tool, per-tenant secret resolution via the `CredentialsProvider` interface
- Multiple providers with ordered evaluation
- Configurable LRU cache with tunable TTL and max entries

#### Circuit Breaking and Resilience

- Per-tool circuit breakers with closed, open, and half-open states
- Configurable failure threshold, open duration, and half-open max requests
- Session executor recovery with transparent failover on transport failures
- Periodic health probing on every tool with automatic state transitions
- Per-tool session concurrency limits and bounded audit mailbox for backpressure

#### Session Management

- One session actor per tool + tenant + client combination for stateful operations
- Session affinity modes: `sticky` (session reuse) and `least_loaded` (load-balanced routing)
- Automatic session passivation after configurable idle timeout

#### Dynamic Tool Management

- Register, update, enable, disable, drain, and remove tools at runtime without restart
- Drain mode to stop accepting new sessions while existing ones complete
- Tool state management: enabled, degraded, unavailable, disabled

#### Streaming

- `InvokeStream()` API with a progress channel for intermediate notifications
- `StreamingResult` with `Collect()` convenience method

#### Cluster Mode

- Multi-node deployment with gossip-based membership via GoAkt clustering
- Distributed actor messaging via GoAkt remoting
- Cluster singleton registrar ensuring exactly one registry instance across the cluster
- Pluggable peer discovery via the `DiscoveryProvider` interface with built-in Kubernetes support
- TLS support for all remoting and cluster communication
- Graceful shutdown with ordered pod termination

#### Observability

- OpenTelemetry metrics: invocation latency (histogram), invocation failures (counter), tool availability state (counter), circuit state transitions (counter), active sessions (up-down counter), session lifecycle events (counters), credential cache results (counter), policy evaluation latency (histogram)
- Distributed tracing with end-to-end spans from ingress to egress and W3C trace propagation
- Pluggable structured logger interface with correlation fields (tenant ID, tool ID, request ID, trace ID)

#### Durable Audit Trail

- Structured `AuditEvent` capture for policy decisions, invocation lifecycle, health transitions, and circuit state changes
- Pluggable `AuditSink` interface with built-in `MemorySink` and `FileSink` implementations
- Bounded mailbox with configurable capacity for backpressure

#### Admin API

- `GetGatewayStatus()` for overall gateway health and tool count
- `GetToolStatus()` for per-tool status, circuit state, sessions, and schemas
- `ListSessions()` for all active sessions across all tools
- `DrainTool()` and `ResetCircuit()` for operational control

#### Error Handling

- Comprehensive error codes: `ErrCodeToolUnavailable`, `ErrCodePolicyDenied`, `ErrCodeTransportFailure`, `ErrCodeTimeout`, `ErrCodeThrottled`, `ErrCodeConcurrencyLimitReached`, `ErrCodeInvalidRequest`, `ErrCodeInternal`
- Rich execution results with status, output map, error detail, duration, and correlation metadata

#### Examples

- `filesystem` -- minimal gateway with a stdio filesystem tool
- `audit-http` -- durable file audit sink with HTTP egress
- `ingress` -- MCP Streamable HTTP ingress with header-based identity
- `admin-policy` -- full admin API and custom policy evaluator
- `quota-assess` -- per-tenant rate limiting and concurrency enforcement
- `full-config` -- complete configuration reference
- `ai-hub` -- production-grade multi-tenant hub with stdio + HTTP egress, policy, credentials, audit, and OpenTelemetry
- `cluster` -- three-node Kubernetes cluster with peer discovery, nginx session affinity, and Jaeger tracing

#### CI/CD

- GitHub Actions pipeline with linting (`golangci-lint`), testing with race detection, and coverage reporting via Codecov

[v0.1.0]: https://github.com/tochemey/portcullis/releases/tag/v0.1.0
