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
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
)

// TransportType defines the underlying MCP transport protocol used by a tool.
type TransportType string

const (
	TransportStdio TransportType = "stdio"
	TransportHTTP  TransportType = "http"
	TransportGRPC  TransportType = "grpc"
)

// RoutingMode defines how the runtime selects an execution target for a tool.
type RoutingMode string

const (
	RoutingSticky      RoutingMode = "sticky"
	RoutingLeastLoaded RoutingMode = "least_loaded"
)

// ToolState describes the current operational availability of a registered tool.
type ToolState string

const (
	ToolStateEnabled     ToolState = "enabled"
	ToolStateDisabled    ToolState = "disabled"
	ToolStateDegraded    ToolState = "degraded"
	ToolStateUnavailable ToolState = "unavailable"
)

// CredentialPolicy defines the credential requirement for a tool.
type CredentialPolicy string

const (
	CredentialPolicyOptional CredentialPolicy = "optional"
	CredentialPolicyRequired CredentialPolicy = "required"
)

// AuthorizationPolicy defines the authorization model for a tool.
type AuthorizationPolicy string

const (
	AuthorizationPolicyTenantAllowlist AuthorizationPolicy = "tenant_allowlist"
)

// StdioTransportConfig holds the execution parameters for tools launched as local
// child processes and communicated with over stdin/stdout.
type StdioTransportConfig struct {
	// Command is the executable path or name to launch (e.g. "npx", "python3").
	Command string
	// Args are the command-line arguments passed to the child process.
	Args []string
	// Env holds additional environment variables set on the child process.
	// These are merged with the parent process environment; duplicates override.
	Env map[string]string
	// IsolateEnv, when true, stops the child process from inheriting the
	// gateway's environment. The child receives only a minimal base (PATH,
	// HOME, TMPDIR, USER, LOGNAME, SHELL, LANG, TERM from the parent), plus
	// Env and any resolved credentials. Recommended when the gateway process
	// environment holds secrets (other tools' API keys, cloud credentials)
	// that the tool binary must not see. False preserves full inheritance,
	// which PATH-dependent launchers like npx typically rely on anyway via
	// the retained base variables.
	IsolateEnv bool
	// WorkingDirectory is the working directory for the child process.
	// When empty, the parent process's working directory is used.
	WorkingDirectory string
}

// HTTPTransportConfig holds the connection parameters for tools reached over HTTP.
type HTTPTransportConfig struct {
	// URL is the MCP server endpoint (e.g. "https://mcp.example.com/sse").
	// Required for HTTP transport tools.
	URL string
	// TLS holds optional TLS settings for the outbound connection. When nil,
	// the default system TLS configuration is used.
	TLS *TLSClientConfig

	// OAuthHandler handles OAuth authorization for outbound connections to this
	// tool's MCP server. When set, the executor attaches it to the SDK's
	// StreamableClientTransport so that token acquisition and refresh happen
	// transparently on every request.
	//
	// For enterprise-managed authorization, use EnterpriseHandler from the
	// MCP Go SDK's extauth package:
	// https://github.com/modelcontextprotocol/go-sdk/tree/main/auth/extauth
	//
	// EnterpriseHandler implements the full 3-step flow defined in the MCP
	// enterprise-managed authorization extension:
	//
	//  1. OIDC login — user authenticates with the enterprise IdP.
	//  2. Token exchange (RFC 8693, https://www.rfc-editor.org/rfc/rfc8693) —
	//     ID Token is exchanged for an ID-JAG at the IdP.
	//  3. JWT bearer grant (RFC 7523, https://www.rfc-editor.org/rfc/rfc7523) —
	//     ID-JAG is exchanged for an access token at the tool's authorization
	//     server.
	//
	// See https://modelcontextprotocol.io/extensions/auth/enterprise-managed-authorization
	//
	// Optional; when nil, no OAuth authorization is applied to outbound requests.
	OAuthHandler auth.OAuthHandler
}

// TLSClientConfig holds TLS settings for outbound HTTP connections to MCP servers.
type TLSClientConfig struct {
	// CACertFile is the path to a PEM-encoded CA certificate bundle used to
	// verify the server's certificate. When empty, the system CA pool is used.
	CACertFile string
	// ClientCertFile is the path to a PEM-encoded client certificate for mTLS.
	// Must be paired with ClientKeyFile.
	ClientCertFile string
	// ClientKeyFile is the path to a PEM-encoded private key for mTLS.
	// Must be paired with ClientCertFile.
	ClientKeyFile string
	// InsecureSkipVerify disables server certificate verification. Use only
	// for development and testing; never in production.
	InsecureSkipVerify bool
}

// GRPCTransportConfig holds the connection parameters for tools reached over gRPC.
type GRPCTransportConfig struct {
	// Target is the gRPC dial target (e.g. "payments.internal:50051").
	// Required for gRPC transport tools.
	Target string
	// Service is the fully-qualified protobuf service name
	// (e.g. "payments.v1.PaymentService"). Used to scope descriptor resolution
	// and to build the full method path for invocations.
	Service string
	// Method is the specific RPC method name (e.g. "Charge"). Combined with
	// Service to form the full gRPC method path "/payments.v1.PaymentService/Charge".
	// When empty, all methods in the service are exposed as tool schemas and the
	// invocation's tool name is used as the method at call time.
	Method string
	// DescriptorSet is the path to a binary-encoded FileDescriptorSet (.binpb)
	// containing the protobuf definitions for the target service. Generated via:
	//
	//   protoc --descriptor_set_out=service.binpb --include_imports service.proto
	//   buf build -o service.binpb
	//
	// Mutually exclusive with Reflection; exactly one must be set.
	DescriptorSet string
	// Reflection enables gRPC server reflection for descriptor discovery instead
	// of a local descriptor set file. Intended for development and staging
	// environments where the backend has reflection enabled.
	//
	// Mutually exclusive with DescriptorSet; exactly one must be set.
	Reflection bool
	// TLS holds optional TLS settings for the outbound gRPC connection. When nil,
	// plaintext (insecure) transport credentials are used.
	TLS *TLSClientConfig
	// Metadata holds static gRPC metadata key-value pairs attached to every
	// outbound RPC. Useful for API keys, routing headers, and similar concerns.
	Metadata map[string]string
}

// Tool represents a registered MCP capability with its full execution metadata.
type Tool struct {
	// ID is the unique identifier for this tool within the gateway registry.
	ID ToolID
	// Transport selects the underlying MCP transport protocol ("stdio" or "http").
	Transport TransportType
	// Stdio holds configuration for stdio-based tools. Required when Transport
	// is [TransportStdio]; ignored otherwise.
	Stdio *StdioTransportConfig
	// HTTP holds configuration for HTTP-based tools. Required when Transport
	// is [TransportHTTP]; ignored otherwise.
	HTTP *HTTPTransportConfig
	// GRPC holds configuration for gRPC-based tools. Required when Transport
	// is [TransportGRPC]; ignored otherwise.
	GRPC *GRPCTransportConfig
	// StartupTimeout is the maximum time to wait for the backend process or
	// connection to become ready. When zero, [DefaultStartupTimeout] is used.
	StartupTimeout time.Duration
	// RequestTimeout is the maximum time for a single tool invocation. When
	// zero, [DefaultRequestTimeout] is used.
	RequestTimeout time.Duration
	// IdleTimeout is how long a session may remain idle before passivation.
	// When zero, [DefaultSessionIdleTimeout] is used.
	IdleTimeout time.Duration
	// Routing selects how the runtime picks an execution target: "sticky"
	// reuses sessions per tenant+client+tool, "least_loaded" picks the
	// session with fewest in-flight requests. Default: "sticky".
	Routing RoutingMode
	// CredentialPolicy defines whether the tool requires resolved credentials
	// ("required") or can proceed without them ("optional"). Default: "optional".
	CredentialPolicy CredentialPolicy
	// AuthorizationPolicy defines the authorization model for this tool.
	// "tenant_allowlist" restricts access to tenants listed in the gateway
	// configuration. Default: no restriction.
	AuthorizationPolicy AuthorizationPolicy
	// State is the initial operational state of the tool. Tools in "disabled"
	// or "unavailable" state reject invocations until re-enabled.
	State ToolState

	// MaxSessionsPerTool limits the number of concurrent sessions for this tool.
	// When > 0, the router rejects new work (backpressure) when the supervisor's
	// session count reaches this limit. Zero means no limit.
	MaxSessionsPerTool int

	// Schemas holds the MCP tool schemas discovered from the backend server via
	// tools/list. Populated by the runtime at registration time; ignored on input.
	Schemas []ToolSchema

	// Resources holds MCP resource metadata discovered from the backend server
	// via resources/list. Populated by the runtime at registration time; ignored on input.
	Resources []ResourceSchema
	// ResourceTemplates holds MCP resource template metadata discovered from the
	// backend server via resources/templates/list. Populated by the runtime at
	// registration time; ignored on input.
	ResourceTemplates []ResourceTemplateSchema
}

// IsStdio reports whether the tool uses the stdio transport.
func (t Tool) IsStdio() bool { return t.Transport == TransportStdio }

// IsHTTP reports whether the tool uses the HTTP transport.
func (t Tool) IsHTTP() bool { return t.Transport == TransportHTTP }

// IsGRPC reports whether the tool uses the gRPC transport.
func (t Tool) IsGRPC() bool { return t.Transport == TransportGRPC }

// IsAvailable reports whether the tool can accept new requests.
func (t Tool) IsAvailable() bool {
	return t.State == ToolStateEnabled || t.State == ToolStateDegraded
}
