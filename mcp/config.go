// MIT License
//
// Copyright (c) 2026 GoAkt Team
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.
//

package mcp

import (
	"net/http"
	"time"
)

// Default configuration values applied when fields are not explicitly set.
const (
	DefaultSessionIdleTimeout  = 5 * time.Minute
	DefaultRequestTimeout      = 30 * time.Second
	DefaultStartupTimeout      = 10 * time.Second
	DefaultHealthProbeInterval = 30 * time.Second
	DefaultHealthProbeTimeout  = 10 * time.Second
	DefaultShutdownTimeout     = 30 * time.Second
	DefaultMaxCacheEntries     = 1000
	DefaultAuditMailboxSize    = 1024
	DefaultRouterPoolSize      = 8
)

// Config is the root configuration for the mcgate gateway.
type Config struct {
	// LogLevel sets the gateway-wide logging verbosity.
	// Accepted values: "debug", "info", "warning", "error", "fatal", "panic".
	// When empty, the default (info) is used.
	LogLevel string

	// Runtime configures core runtime tuning parameters.
	Runtime RuntimeConfig

	// Cluster configures multi-node operation. When Cluster.Enabled is false
	// the gateway runs in single-node mode with no distributed coordination.
	Cluster ClusterConfig

	// Telemetry configures OpenTelemetry export.
	Telemetry TelemetryConfig

	// Audit configures the durable audit sink.
	Audit AuditConfig

	// Credentials configures secret provider backends.
	Credentials CredentialsConfig

	// Tenants holds per-tenant quota and policy configuration.
	Tenants []TenantConfig

	// HealthProbe configures health probe settings.
	HealthProbe HealthProbeConfig

	// Tools holds the tool definitions to register at startup.
	Tools []Tool
}

// RuntimeConfig holds core runtime tuning parameters.
type RuntimeConfig struct {
	// SessionIdleTimeout is how long the router waits before passivating an
	// idle tool session. Zero means use DefaultSessionIdleTimeout.
	SessionIdleTimeout time.Duration
	// RequestTimeout is the maximum wall-clock time allowed for a single tool
	// invocation, including policy evaluation and egress round-trip.
	// Zero means use DefaultRequestTimeout.
	RequestTimeout time.Duration
	// StartupTimeout is the maximum time the gateway waits for a backend MCP
	// server process to become ready after it is spawned.
	// Zero means use DefaultStartupTimeout.
	StartupTimeout time.Duration
	// HealthProbeInterval is how often the health actor probes tool supervisors.
	// Zero means use DefaultHealthProbeInterval. This value is also the fallback
	// for HealthProbe.Interval when that field is zero.
	HealthProbeInterval time.Duration
	// ShutdownTimeout is the maximum time Stop waits for the actor system to
	// drain cleanly before returning an error. Stop bounds its shutdown by both
	// this timeout and the caller's context, whichever expires first.
	// Zero means use DefaultShutdownTimeout.
	ShutdownTimeout time.Duration
	// RouterPoolSize is the number of router actors spawned per node. Each
	// router executes one invocation at a time (its message handler blocks
	// for the duration of the backend call), so the pool size bounds the
	// number of concurrently executing invocations per node. Invocations are
	// distributed across the pool round-robin.
	// Zero means use DefaultRouterPoolSize.
	RouterPoolSize int
}

// ClusterConfig holds multi-node operation settings.
//
// When Enabled is true a DiscoveryProvider must be set so that the cluster
// subsystem can locate peer nodes. The provider is a user-supplied
// implementation of the DiscoveryProvider interface — the gateway does not
// embed any built-in discovery backends.
type ClusterConfig struct {
	// Enabled activates multi-node cluster mode. When false the gateway runs
	// in single-node mode with no distributed coordination.
	Enabled bool
	// DiscoveryProvider is the peer discovery implementation.
	// Required when Enabled is true.
	DiscoveryProvider DiscoveryProvider
	// RegistrarRole is the cluster role that determines which nodes are
	// eligible to host the registrar singleton. When empty, all nodes in the
	// cluster are eligible.
	RegistrarRole string
	// DiscoveryPort is the TCP port the DiscoveryProvider advertises so that
	// peers can locate this node. Zero means use the default (15000).
	DiscoveryPort int
	// PeersPort is the TCP port used for memberlist (gossip) communication
	// between cluster nodes. Zero means use the default (15000).
	PeersPort int
	// RemotingPort is the TCP port used for GoAkt remoting (actor message
	// passing) between cluster nodes. Zero means use the default (15001).
	RemotingPort int
	// TLS configures TLS for remoting and cluster communication.
	// When set, both the remoting server and client use TLS; cluster memberlist
	// and remoting traffic are encrypted. All nodes must share the same root CA.
	TLS *RemotingTLSConfig
}

// RemotingTLSConfig holds TLS settings for GoAkt remoting and cluster.
//
// Server identity: CertFile and KeyFile are required when TLS is enabled.
// Client verification: CACertFile is used to verify remote servers; omit only
// when InsecureSkipVerify is true (dev/testing only).
// Mutual TLS: set ClientCAFile so the server validates client certs; set
// ClientCertFile and ClientKeyFile so the client presents a cert to remotes.
type RemotingTLSConfig struct {
	// CertFile and KeyFile are the server certificate and private key.
	CertFile string
	KeyFile  string
	// ClientCAFile, when non-empty, enables mTLS: server requires client certs
	// signed by this CA.
	ClientCAFile string
	// CACertFile is the CA used to verify remote server certificates.
	CACertFile string
	// ClientCertFile and ClientKeyFile, when both set, present a client cert
	// to remote nodes (mTLS).
	ClientCertFile string
	ClientKeyFile  string
	// InsecureSkipVerify skips server cert verification. Use only for dev/testing.
	InsecureSkipVerify bool
}

// TelemetryConfig holds OpenTelemetry export settings.
//
// The gateway records metrics and traces through the global OpenTelemetry
// providers (otel.GetMeterProvider / otel.GetTracerProvider). Configuring
// exporters — OTLP or otherwise — is the application's responsibility via the
// OpenTelemetry SDK.
type TelemetryConfig struct {
	// OTLPEndpoint is reserved for future use and is currently not consumed by
	// the gateway. Configure the global OpenTelemetry SDK to export telemetry.
	OTLPEndpoint string
}

// AuditOverflowPolicy selects how the gateway behaves when audit events are
// produced faster than the audit sink can persist them.
type AuditOverflowPolicy string

const (
	// AuditOverflowBlock applies backpressure: when the journal's bounded
	// mailbox is full, producing actors block until space is available. No
	// audit event is ever lost, but a wedged sink (e.g. a stalled
	// filesystem) eventually stalls the actors producing audit events.
	// This is the default.
	AuditOverflowBlock AuditOverflowPolicy = "block"
	// AuditOverflowDropNewest favors availability: sink writes happen on a
	// dedicated writer with a bounded queue, and when the queue is full new
	// audit events are dropped (and counted in a warning log) instead of
	// blocking the request path. Use this when gateway availability matters
	// more than audit completeness.
	AuditOverflowDropNewest AuditOverflowPolicy = "drop_newest"
)

// AuditConfig holds audit sink settings.
type AuditConfig struct {
	// Sink is the audit sink to use.
	Sink AuditSink
	// MailboxSize is the maximum number of audit events that can be queued in the
	// journal actor's mailbox. When the mailbox is full, senders block until space
	// is available, providing backpressure. Zero means use DefaultAuditMailboxSize.
	// With AuditOverflowDropNewest, the same size bounds the writer queue.
	MailboxSize int
	// OverflowPolicy selects the behavior when the sink cannot keep up.
	// Empty means AuditOverflowBlock.
	OverflowPolicy AuditOverflowPolicy
}

// CredentialsConfig holds configuration for secret provider backends.
type CredentialsConfig struct {
	// Providers holds the list of credentials providers.
	Providers []CredentialsProvider
	// CacheTTL is the time to live for credentials cache entries. Zero means
	// credentials are not cached and fetched on every invocation.
	CacheTTL time.Duration
	// MaxCacheEntries is the maximum number of entries in the credential cache.
	// When the cache exceeds this limit, expired entries are evicted first,
	// then the least-recently-accessed entry is removed (LRU). Zero means use
	// DefaultMaxCacheEntries.
	MaxCacheEntries int
}

// TenantConfig defines per-tenant settings including quota limits and optional
// custom policy evaluation.
type TenantConfig struct {
	// ID is the identifier for the tenant.
	ID TenantID
	// Quotas is the usage quota limits for the tenant.
	Quotas TenantQuotaConfig
	// Evaluator is an optional custom policy evaluator for this tenant.
	// When set, it is called after all built-in authorization and quota checks
	// pass. Returning a non-nil *RuntimeError from Evaluate denies the invocation.
	// When nil, only the built-in checks apply.
	Evaluator PolicyEvaluator
}

// TenantQuotaConfig defines the usage quota limits for a single tenant.
type TenantQuotaConfig struct {
	// RequestsPerMinute is the maximum number of requests per minute for the tenant.
	RequestsPerMinute int
	// ConcurrentSessions is the maximum number of concurrent sessions for the tenant.
	ConcurrentSessions int
}

// HealthProbeConfig holds health probe settings.
type HealthProbeConfig struct {
	// Interval is the interval between health probes.
	// When zero, Runtime.HealthProbeInterval is used (which itself defaults to
	// DefaultHealthProbeInterval).
	Interval time.Duration
	// Timeout is the maximum duration for a single probe cycle.
	// When zero, DefaultHealthProbeTimeout is used.
	Timeout time.Duration
}

// WSConfig holds WebSocket-specific configuration for [Gateway.WSHandler].
// All zero values use built-in defaults.
type WSConfig struct {
	// ReadBufferSize specifies the I/O buffer size in bytes for reading
	// WebSocket frames. Zero uses the default (4096).
	ReadBufferSize int

	// WriteBufferSize specifies the I/O buffer size in bytes for writing
	// WebSocket frames. Zero uses the default (4096).
	WriteBufferSize int

	// PingInterval is how often the server sends WebSocket ping frames to
	// keep the connection alive. Zero uses the default (30s).
	PingInterval time.Duration

	// MaxMessageSize is the maximum size in bytes of a single incoming
	// WebSocket message. Messages larger than the limit cause the connection
	// to be closed. Zero uses the default (4 MiB); negative disables the
	// limit entirely.
	MaxMessageSize int64

	// CheckOrigin is an optional function that returns true if the request
	// origin is acceptable. When nil, requests whose Origin header is present
	// and does not match the request Host are rejected (same-origin policy).
	// Supply a custom function to allow trusted cross-origin browsers clients.
	CheckOrigin func(r *http.Request) bool
}
