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

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

// EnterpriseAuthConfig configures the MCP enterprise-managed authorization
// extension (io.modelcontextprotocol/enterprise-managed-authorization).
//
// When set on [IngressConfig], the gateway acts as an MCP Resource Server:
// incoming requests must carry a valid Bearer token, and the gateway publishes
// Protected Resource Metadata per RFC 9728
// (https://www.rfc-editor.org/rfc/rfc9728) so that MCP clients can discover
// the authorization server.
//
// The enterprise-managed authorization flow works as follows:
//
//  1. The MCP client authenticates with the enterprise IdP via OpenID Connect
//     or SAML and obtains an identity assertion (ID Token or SAML assertion).
//  2. The client exchanges the identity assertion for an ID-JAG (Identity
//     Assertion JWT Authorization Grant) at the IdP token endpoint using
//     RFC 8693 token exchange (https://www.rfc-editor.org/rfc/rfc8693).
//  3. The client presents the ID-JAG to the MCP Authorization Server and
//     receives an access token via the RFC 7523 JWT bearer grant
//     (https://www.rfc-editor.org/rfc/rfc7523).
//  4. The client sends the access token as a Bearer token on every MCP
//     request to this gateway.
//
// This configuration covers step 4: validating the Bearer token and mapping
// it to the gateway's tenant and client identity model.
//
// See https://modelcontextprotocol.io/extensions/auth/enterprise-managed-authorization
type EnterpriseAuthConfig struct {
	// TokenVerifier validates the Bearer token on each incoming MCP request.
	// The function receives the raw token string and the HTTP request, and
	// returns the validated token claims or an error.
	//
	// Implementations should verify the token signature, audience, issuer,
	// expiration, and any other claims required by the deployment. When
	// verification fails, the error must wrap [auth.ErrInvalidToken] so the
	// middleware returns HTTP 401 Unauthorized with a proper WWW-Authenticate
	// header.
	//
	// Required; [Gateway.Handler], [Gateway.WSHandler],
	// [Gateway.RegisterGRPCService], and [GRPCAuthInterceptors] return an error
	// when this field is nil.
	TokenVerifier auth.TokenVerifier

	// ResourceMetadata is the OAuth 2.0 Protected Resource Metadata published
	// at /.well-known/oauth-protected-resource per RFC 9728.
	//
	// At a minimum, set the Resource field (the gateway's resource identifier)
	// and the AuthorizationServers field (the issuer URLs of authorization
	// servers that can issue tokens accepted by this gateway). MCP clients
	// use this metadata to discover where to obtain access tokens.
	//
	// Required; [Gateway.Handler] and [Gateway.WSHandler]
	// return an error when this field is nil.
	ResourceMetadata *oauthex.ProtectedResourceMetadata

	// RequiredScopes lists OAuth scopes that every incoming request must carry.
	// The middleware rejects requests whose tokens lack any of these scopes
	// with HTTP 403 Forbidden.
	//
	// Optional; when empty, no scope enforcement is applied at the middleware
	// level. Per-tool scope enforcement can be implemented via [PolicyEvaluator].
	RequiredScopes []string

	// IdentityMapper maps validated token claims to the gateway's tenant and
	// client identity model. It is called once per new MCP session after the
	// Bearer token has been validated.
	//
	// When nil, [DefaultIdentityMapper] is used: UserID maps to ClientID, and
	// TenantID defaults to "default". Deployments that derive tenant identity
	// from token claims (e.g. an "org_id" custom claim) must provide a custom
	// mapper.
	IdentityMapper IdentityMapper

	// TokenExchanger optionally performs RFC 8693 token exchange so the gateway
	// can mint downstream tokens on behalf of the authenticated user. The
	// typical deployment uses this for MCP Enterprise Managed Authorization
	// (SEP-990): when the gateway calls a backend MCP server that requires a
	// different audience than the ingress token, custom
	// [CredentialsProvider] or [PolicyEvaluator] implementations call
	// TokenExchanger.ExchangeToken with the user's subject token and receive
	// an ID-JAG (or other requested token type) scoped to the backend.
	//
	// Optional; leave nil when the gateway does not need to mint downstream
	// tokens. When set, the exchanger is exposed to runtime extensions via
	// [Gateway] but is not invoked automatically on every request; calling
	// code decides when an exchange is warranted.
	TokenExchanger TokenExchanger
}

// IdentityMapper maps validated bearer token claims to the gateway's tenant
// and client identity types.
//
// Implementations extract tenant and client identifiers from the token's
// claims (scopes, UserID, Extra fields) so the gateway can route, audit,
// and enforce policy using the standard [TenantID] and [ClientID] types.
//
// A non-nil error rejects the MCP session; the gateway returns HTTP 400
// Bad Request to the client.
//
// Implementations must be safe for concurrent use from multiple goroutines.
type IdentityMapper interface {
	MapIdentity(info *auth.TokenInfo) (tenantID TenantID, clientID ClientID, err error)
}

// IdentityMapperFunc is an adapter to allow the use of ordinary functions as
// [IdentityMapper]. If f is a function with the appropriate signature,
// IdentityMapperFunc(f) is an IdentityMapper that calls f.
type IdentityMapperFunc func(info *auth.TokenInfo) (TenantID, ClientID, error)

// tokenIdentityResolver implements [IdentityResolver] by reading
// [auth.TokenInfo] from the request context.
type tokenIdentityResolver struct {
	mapper IdentityMapper
}

// grpcTokenInfoKey is the context key for storing validated [auth.TokenInfo]
// in gRPC handler contexts. The SDK's HTTP middleware uses an unexported key,
// so gRPC needs its own. [NewGRPCTokenIdentityResolver] reads from this key.
type grpcTokenInfoKey struct{}

// grpcTokenIdentityResolver implements [GRPCIdentityResolver] by reading
// [auth.TokenInfo] from the gRPC context.
type grpcTokenIdentityResolver struct {
	mapper IdentityMapper
}

// MapIdentity calls f(info).
func (f IdentityMapperFunc) MapIdentity(info *auth.TokenInfo) (TenantID, ClientID, error) {
	return f(info)
}

// DefaultIdentityMapper returns an [IdentityMapper] that maps [auth.TokenInfo]
// to gateway identity using the following rules:
//
//   - ClientID is set to TokenInfo.UserID. When UserID is empty, ClientID
//     defaults to "default".
//   - TenantID defaults to "default". Deployments that encode tenant identity
//     in token claims should provide a custom [IdentityMapper] instead.
//
// DefaultIdentityMapper never returns an error.
//
// Caution: because empty UserIDs collapse to the "default" client identity,
// two distinct authenticated principals whose verified tokens carry no
// UserID are routed to the same sticky session (sessions are keyed by
// tenant+client+tool) and share its per-session executor state and
// credentials. Deployments whose TokenVerifier can produce TokenInfo without
// a UserID should supply a custom [IdentityMapper] that rejects such tokens
// or derives the identity from another claim.
func DefaultIdentityMapper() IdentityMapper {
	return IdentityMapperFunc(func(info *auth.TokenInfo) (TenantID, ClientID, error) {
		clientID := ClientID("default")
		if info != nil && info.UserID != "" {
			clientID = ClientID(info.UserID)
		}
		return "default", clientID, nil
	})
}

// NewTokenIdentityResolver creates an [IdentityResolver] backed by the
// enterprise-managed authorization extension. It reads the [auth.TokenInfo]
// placed in the request context by the Bearer token middleware and maps it
// to TenantID and ClientID using the provided [IdentityMapper].
//
// When mapper is nil, [DefaultIdentityMapper] is used.
//
// This resolver is intended for use with [EnterpriseAuthConfig]. When the
// request context does not contain a TokenInfo (e.g. the middleware was
// bypassed), the resolver returns an error and the session is rejected.
func NewTokenIdentityResolver(mapper IdentityMapper) IdentityResolver {
	if mapper == nil {
		mapper = DefaultIdentityMapper()
	}
	return &tokenIdentityResolver{mapper: mapper}
}

// ResolveIdentity extracts the validated token claims from the request
// context and delegates to the configured [IdentityMapper].
func (r *tokenIdentityResolver) ResolveIdentity(req *http.Request) (TenantID, ClientID, error) {
	info := auth.TokenInfoFromContext(req.Context())
	if info == nil {
		return "", "", NewRuntimeError(ErrCodeInvalidRequest, "missing bearer token context")
	}
	return r.mapper.MapIdentity(info)
}

// GRPCContextWithTokenInfo returns a new context with the validated token info
// stored for retrieval by [GRPCTokenInfoFromContext].
func GRPCContextWithTokenInfo(ctx context.Context, info *auth.TokenInfo) context.Context {
	return context.WithValue(ctx, grpcTokenInfoKey{}, info)
}

// GRPCTokenInfoFromContext returns the [auth.TokenInfo] stored in the gRPC
// request context by the enterprise auth validation, or nil if none.
func GRPCTokenInfoFromContext(ctx context.Context) *auth.TokenInfo {
	info, _ := ctx.Value(grpcTokenInfoKey{}).(*auth.TokenInfo)
	return info
}

// NewGRPCTokenIdentityResolver creates a [GRPCIdentityResolver] backed by the
// enterprise-managed authorization extension. It reads the [auth.TokenInfo]
// placed in the gRPC context by the handler's enterprise auth validation and
// maps it to TenantID and ClientID using the provided [IdentityMapper].
//
// When mapper is nil, [DefaultIdentityMapper] is used.
//
// This resolver is intended for use with [EnterpriseAuthConfig] on
// [GRPCIngressConfig]. When the context does not contain a TokenInfo (e.g.
// enterprise auth is not configured), the resolver returns an error.
func NewGRPCTokenIdentityResolver(mapper IdentityMapper) GRPCIdentityResolver {
	if mapper == nil {
		mapper = DefaultIdentityMapper()
	}
	return &grpcTokenIdentityResolver{mapper: mapper}
}

// ResolveGRPCIdentity extracts the validated token claims from the gRPC
// context and delegates to the configured [IdentityMapper].
func (r *grpcTokenIdentityResolver) ResolveGRPCIdentity(ctx context.Context) (TenantID, ClientID, error) {
	info := GRPCTokenInfoFromContext(ctx)
	if info == nil {
		return "", "", NewRuntimeError(ErrCodeInvalidRequest, "missing bearer token context")
	}
	return r.mapper.MapIdentity(info)
}

// ProtectedResourceMetadataHandler returns an [http.Handler] that serves
// OAuth 2.0 Protected Resource Metadata per RFC 9728
// (https://www.rfc-editor.org/rfc/rfc9728).
//
// Mount this handler at /.well-known/oauth-protected-resource on the same
// host as the MCP ingress endpoint. MCP clients use this metadata to discover
// the authorization server(s) accepted by the gateway before initiating the
// enterprise-managed authorization flow.
//
// The handler sets Access-Control-Allow-Origin: * because OAuth metadata is
// public, non-sensitive information intended for client discovery
// (RFC 9728 Section 3.1, https://www.rfc-editor.org/rfc/rfc9728#section-3.1).
func ProtectedResourceMetadataHandler(metadata *oauthex.ProtectedResourceMetadata) http.Handler {
	return auth.ProtectedResourceMetadataHandler(metadata)
}
