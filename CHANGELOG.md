# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

Initial release of mcgate — a supervised MCP gateway library for Go, built on the [GoAkt](https://github.com/Tochemey/goakt) actor framework: a stateless protocol edge in front of a supervised, stateful execution core.

### Added

#### Multi-Transport Ingress

- Streamable HTTP handler (`Gateway.Handler`) implementing the MCP 2026-07-28 stateless spec: every request is self-contained and any gateway node can serve it; clients on the previous protocol revision are served through the same path. Built on MCP Go SDK v1.7.0. The tenant-visible tool surface is cached per request cycle so stateless construction stays cheap.
- WebSocket handler (`Gateway.WSHandler`) for full-duplex streaming, with same-origin policy by default and bounded message size (`WSConfig.MaxMessageSize`, default 4 MiB)
- gRPC ingress (`MCPToolService`: `ListTools`, `CallTool`, `CallToolStream`) with metadata-based identity resolution, Bearer-token interceptors, tool-name TTL cache, and server-streaming progress
- Per-request tenant and client identity resolution via the `IdentityResolver` interface across all transports

#### Multi-Transport Egress

- Stdio executor for child-process tool backends communicating over stdin/stdout, with `StdioTransportConfig.IsolateEnv` to stop child processes from inheriting the gateway's full environment
- HTTP executor for remote MCP server connectivity with Streamable HTTP semantics
- gRPC egress: invoke gRPC services as tools via proto descriptor sets or server reflection, with dynamic protobuf construction, JSON Schema derivation, mTLS, and streaming
- Automatic schema and resource discovery from tool backends with pagination support and caching
- W3C trace-context header propagation on outbound calls

#### MCP Resources

- `resources/list`, `resources/templates/list`, and `resources/read` proxied through the full actor pipeline; resource metadata discovered and cached at registration

#### Multi-Tenancy and Authorization

- Enterprise-managed authorization (RFC 9728 + MCP extension) on all ingress transports, with `TokenVerifier`, `IdentityMapper`, scope propagation to policy, and RFC 8693 `TokenExchanger` for minting downstream tokens
- Per-tenant quota enforcement with configurable rate limits and concurrency caps
- Two-layer policy evaluation: built-in checks (rate limits, concurrency, tool authorization) and custom `PolicyEvaluator` for context-aware decisions (OPA, ABAC, allowlists)
- Tenant-scoped tool listing: allowlist-guarded tools are hidden from tenants outside the configured tenant set on every ingress
- Every policy decision recorded in the audit journal

#### Credential Brokering

- Per-tool, per-tenant secret resolution via the `CredentialsProvider` interface, with multiple providers in ordered evaluation and a configurable LRU cache
- Resolved credentials injected into backends as child-process env vars (stdio), request headers (HTTP), and call metadata (gRPC)

#### Circuit Breaking and Resilience

- Per-tool `mcp.CircuitBreaker` state machine (closed, open, half-open) with configurable failure threshold, open duration, and half-open max requests; outcome reporting is generation-correlated so stale results are discarded, and client disconnects never trip a breaker
- Transparent executor recovery with in-request retry on transport failures
- Periodic read-only health probing on every tool with per-tool time budgets and automatic state transitions
- Per-tool session concurrency limits
- Router pool (`Runtime.RouterPoolSize`, default 8): invocations are distributed round-robin so one slow backend call cannot serialize the gateway

#### Session Management

- Sessions as GoAkt grains (virtual actors): on-demand activation, executor construction on activation, and automatic idle passivation via the grain engine
- One session grain per tool + tenant + client combination for stateful operations
- Session affinity modes: `sticky` (session reuse) and `least_loaded` (load-balanced routing)

#### Dynamic Tool Management

- Register, update, enable, disable, drain, and remove tools at runtime without restart; re-registering a tool refreshes the running supervisor in place, preserving session accounting and circuit state
- Drain mode to stop accepting new sessions while existing ones complete
- Tool state management: enabled, degraded, unavailable, disabled

#### Streaming

- `InvokeStream()` API with a progress channel for intermediate notifications; streams are detached from the request handler and bounded by the tool's own timeout
- `StreamingResult` with `Collect()` convenience method

#### Cluster Mode

- Multi-node deployment with gossip-based membership via GoAkt clustering
- Distributed actor messaging via GoAkt remoting
- Cluster singleton registrar ensuring exactly one registry instance across the cluster
- Pluggable peer discovery via the `DiscoveryProvider` interface with built-in Kubernetes support
- TLS support for all remoting and cluster communication
- Graceful shutdown with ordered pod termination

#### Observability

- OpenTelemetry metrics under the `mcgate.*` prefix: invocation latency (histogram), invocation failures (counter), tool availability state (counter), circuit state transitions (counter), active sessions (up-down counter), session lifecycle events (counters), credential cache results (counter), policy evaluation latency (histogram), actor dead letters (counter)
- Distributed tracing with end-to-end spans from ingress to egress and W3C trace propagation
- Pluggable structured logger interface with correlation fields (tenant ID, tool ID, request ID, trace ID)

#### Durable Audit Trail

- Structured `AuditEvent` capture for policy decisions, invocation lifecycle, health transitions, and circuit state changes
- Pluggable `AuditSink` interface with built-in `MemorySink` and `FileSink` implementations
- Audit overflow policy (`Audit.OverflowPolicy`): `block` (default, lossless backpressure) or `drop_newest` (bounded writer queue that never stalls the request path)
- Audit event stream: `Gateway.SubscribeAudit`/`UnsubscribeAudit` for in-process fan-out alongside the `AuditSink`

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
- `ingress-grpc` -- gRPC ingress with Bearer-token interceptors and streaming progress
- `admin-policy` -- full admin API and custom policy evaluator
- `quota-assess` -- per-tenant rate limiting and concurrency enforcement
- `full-config` -- complete configuration reference
- `ai-hub` -- production-grade multi-tenant hub with stdio + HTTP egress, policy, credentials, audit, and OpenTelemetry
- `cluster` -- three-node Kubernetes cluster with peer discovery, nginx session affinity, and Jaeger tracing

#### CI/CD

- GitHub Actions pipeline with linting (`golangci-lint`), testing with race detection, and coverage reporting via Codecov
