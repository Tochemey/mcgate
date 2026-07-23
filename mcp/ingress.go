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
	"context"
	"net/http"
	"time"
)

// DefaultToolCacheTTL is the default time-to-live for the gRPC ingress tool
// name cache. The cache avoids a ListTools actor Ask on every CallTool and
// CallToolStream request.
const DefaultToolCacheTTL = 5 * time.Second

// IdentityResolver extracts the tenant and client identity from an incoming
// HTTP request. Implementations may read from JWT claims, API key headers,
// mTLS certificates, or any other source appropriate for the deployment.
//
// ResolveIdentity is called once per new MCP session. A non-nil error rejects
// the session; the gateway returns HTTP 400 Bad Request to the client.
// Empty identity values are accepted; no default identity is injected.
type IdentityResolver interface {
	ResolveIdentity(r *http.Request) (tenantID TenantID, clientID ClientID, err error)
}

// IngressConfig configures the MCP HTTP ingress handler returned by
// [Gateway.Handler]. Users mount the resulting [http.Handler] inside their
// own HTTP server or router framework.
//
// The HTTP ingress is stateless, per the MCP 2026-07-28 specification: every
// request is self-contained, carries no Mcp-Session-Id, and can land on any
// gateway node behind a plain load balancer. Clients speaking the previous
// protocol revision are served through the same path (the transport creates a
// temporary per-request session for them). All tool/session state lives on
// the egress side, owned by the actor runtime.
type IngressConfig struct {
	// IdentityResolver extracts caller identity from each incoming request.
	// Required; [Gateway.Handler] returns an error when this field is nil.
	IdentityResolver IdentityResolver

	// EnterpriseAuth configures the MCP enterprise-managed authorization
	// extension (io.modelcontextprotocol/enterprise-managed-authorization).
	// See https://modelcontextprotocol.io/extensions/auth/enterprise-managed-authorization
	//
	// When non-nil, the ingress handler wraps the MCP transport with Bearer
	// token authentication middleware per RFC 9728
	// (https://www.rfc-editor.org/rfc/rfc9728). Every incoming request
	// must carry a valid Authorization: Bearer <token> header. Requests
	// without a valid token receive HTTP 401 Unauthorized with a
	// WWW-Authenticate header pointing to the Protected Resource Metadata.
	//
	// When EnterpriseAuth is set and IdentityResolver is nil, the handler
	// automatically uses [NewTokenIdentityResolver] with the configured
	// [IdentityMapper] (or [DefaultIdentityMapper] when IdentityMapper is nil).
	//
	// Optional; when nil, no Bearer token enforcement is applied.
	EnterpriseAuth *EnterpriseAuthConfig
}

// GRPCIdentityResolver extracts the tenant and client identity from the
// incoming gRPC request context. Implementations typically read identity from
// gRPC metadata via [metadata.FromIncomingContext].
//
// ResolveGRPCIdentity is called once per gRPC request. A non-nil error rejects
// the request; the handler returns gRPC status Unauthenticated to the caller.
type GRPCIdentityResolver interface {
	ResolveGRPCIdentity(ctx context.Context) (tenantID TenantID, clientID ClientID, err error)
}

// GRPCIngressConfig configures the MCP gRPC ingress service registered via
// [Gateway.RegisterGRPCService]. Callers register the resulting service on
// their own [grpc.Server].
type GRPCIngressConfig struct {
	// IdentityResolver extracts caller identity from each incoming gRPC
	// request context. Required unless EnterpriseAuth is set (in which case
	// a token-based resolver is installed automatically).
	// [Gateway.RegisterGRPCService] returns an error when both this field
	// and EnterpriseAuth are nil.
	IdentityResolver GRPCIdentityResolver

	// EnterpriseAuth configures Bearer token authentication on the gRPC
	// ingress. When set, every incoming RPC must carry a valid Bearer token
	// in the "authorization" gRPC metadata key. Requests without a valid
	// token receive gRPC status Unauthenticated; requests with insufficient
	// scopes receive PermissionDenied.
	//
	// When EnterpriseAuth is set and IdentityResolver is nil, the handler
	// automatically uses [NewGRPCTokenIdentityResolver] with the configured
	// [IdentityMapper] (or [DefaultIdentityMapper] when IdentityMapper is nil).
	//
	// Note: ResourceMetadata is not required for gRPC (it is HTTP-specific
	// for RFC 9728 discovery). Only TokenVerifier is required.
	//
	// Note: on the gRPC path the TokenVerifier is invoked with a nil
	// *http.Request (there is none). Verifiers that inspect the request
	// (e.g. DPoP proofs or TLS bindings) must nil-check it or they will
	// panic on every gRPC request.
	//
	// Optional; when nil, no Bearer token enforcement is applied.
	EnterpriseAuth *EnterpriseAuthConfig

	// ToolCacheTTL controls how long resolved tool-name-to-ToolID mappings
	// are cached before the handler re-fetches the tool list from the
	// registrar. Zero uses [DefaultToolCacheTTL]. Negative values disable
	// caching entirely.
	ToolCacheTTL time.Duration
}
