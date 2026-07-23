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

package grpc

import (
	"context"
	"errors"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/modelcontextprotocol/go-sdk/auth"

	"github.com/tochemey/portcullis/mcp"
)

// metadataKeyAuthorization is the gRPC metadata key for the Authorization header.
const metadataKeyAuthorization = "authorization"

// bearerPrefix is the prefix for Bearer token values in the Authorization header.
const bearerPrefix = "bearer "

// authenticatedStream wraps a [grpc.ServerStream] with an enriched context
// that contains the validated [auth.TokenInfo]. This is needed because
// stream interceptors cannot replace the context on the stream directly.
type authenticatedStream struct {
	grpc.ServerStream
	ctx context.Context
}

// resolveGRPCAuthDefaults validates [mcp.EnterpriseAuthConfig] when present on
// cfg and fills in defaults. When EnterpriseAuth is set and IdentityResolver is
// nil, a [mcp.NewGRPCTokenIdentityResolver] is installed automatically using
// the configured IdentityMapper (or [mcp.DefaultIdentityMapper] when nil).
//
// Unlike the HTTP counterpart, ResourceMetadata is not required for gRPC
// because RFC 9728 Protected Resource Metadata is an HTTP-only discovery
// mechanism.
//
// Returns an error when EnterpriseAuth is set but TokenVerifier is nil.
func resolveGRPCAuthDefaults(cfg *mcp.GRPCIngressConfig) error {
	ea := cfg.EnterpriseAuth
	if ea == nil {
		return nil
	}

	if ea.TokenVerifier == nil {
		return errors.New("enterprise auth: TokenVerifier must not be nil")
	}

	// When enterprise auth is active and no explicit IdentityResolver is
	// provided, install one that reads identity from the validated token.
	if cfg.IdentityResolver == nil {
		cfg.IdentityResolver = mcp.NewGRPCTokenIdentityResolver(ea.IdentityMapper)
	}

	return nil
}

// AuthInterceptors returns gRPC unary and stream server interceptors that
// enforce Bearer token authentication per the MCP enterprise-managed
// authorization extension.
//
// The interceptors extract the Bearer token from the "authorization" gRPC
// metadata key, validate it using the configured [auth.TokenVerifier], check
// required scopes, and store the validated [auth.TokenInfo] in the context
// via [mcp.GRPCContextWithTokenInfo].
//
// Install the returned interceptors on the [grpc.Server] before registering
// the MCPToolService via [Gateway.RegisterGRPCService]:
//
//	unary, stream, err := ingressgrpc.AuthInterceptors(enterpriseAuthCfg)
//	srv := grpc.NewServer(
//	    grpc.ChainUnaryInterceptor(unary),
//	    grpc.ChainStreamInterceptor(stream),
//	)
//	gw.RegisterGRPCService(srv, cfg)
//
// Returns an error when ea is nil or TokenVerifier is nil.
func AuthInterceptors(ea *mcp.EnterpriseAuthConfig) (grpc.UnaryServerInterceptor, grpc.StreamServerInterceptor, error) {
	if ea == nil {
		return nil, nil, errors.New("enterprise auth config must not be nil")
	}
	if ea.TokenVerifier == nil {
		return nil, nil, errors.New("enterprise auth: TokenVerifier must not be nil")
	}

	unary := func(
		ctx context.Context,
		req any,
		_ *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		enriched, err := authenticateGRPC(ctx, ea)
		if err != nil {
			return nil, err
		}
		return handler(enriched, req)
	}

	stream := func(
		srv any,
		ss grpc.ServerStream,
		_ *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		enriched, err := authenticateGRPC(ss.Context(), ea)
		if err != nil {
			return err
		}
		return handler(srv, &authenticatedStream{ServerStream: ss, ctx: enriched})
	}

	return unary, stream, nil
}

// authenticateGRPC validates the Bearer token from gRPC metadata and returns
// an enriched context containing the validated [auth.TokenInfo].
//
// Returns gRPC status Unauthenticated when the token is missing or invalid,
// and PermissionDenied when required scopes are not present.
func authenticateGRPC(ctx context.Context, ea *mcp.EnterpriseAuthConfig) (context.Context, error) {
	token, err := extractBearerToken(ctx)
	if err != nil {
		return ctx, status.Errorf(codes.Unauthenticated, "%v", err)
	}

	// TokenVerifier accepts *http.Request as the third parameter for
	// HTTP-specific validation. For gRPC we pass nil — most verifiers only
	// examine the token string and context.
	info, err := ea.TokenVerifier(ctx, token, nil)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidToken) {
			return ctx, status.Errorf(codes.Unauthenticated, "invalid token: %v", err)
		}
		return ctx, status.Errorf(codes.Unauthenticated, "token verification failed: %v", err)
	}

	// Enforce required scopes when configured.
	if len(ea.RequiredScopes) > 0 {
		if err := checkScopes(info, ea.RequiredScopes); err != nil {
			return ctx, status.Errorf(codes.PermissionDenied, "%v", err)
		}
	}

	return mcp.GRPCContextWithTokenInfo(ctx, info), nil
}

// extractBearerToken extracts the Bearer token from the "authorization" gRPC
// metadata key. Returns an error when the metadata is missing, the
// authorization key is absent, or the value is not a valid Bearer token.
func extractBearerToken(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", errors.New("missing gRPC metadata")
	}

	values := md.Get(metadataKeyAuthorization)
	if len(values) == 0 {
		return "", errors.New("missing authorization metadata")
	}

	authValue := values[0]
	if len(authValue) <= len(bearerPrefix) || !strings.EqualFold(authValue[:len(bearerPrefix)], bearerPrefix) {
		return "", errors.New("authorization metadata must use Bearer scheme")
	}

	return authValue[len(bearerPrefix):], nil
}

// checkScopes verifies that the token carries all required scopes. Returns an
// error listing the first missing scope when the check fails.
func checkScopes(info *auth.TokenInfo, required []string) error {
	if info == nil {
		return errors.New("insufficient scope: no token info")
	}

	granted := make(map[string]struct{}, len(info.Scopes))
	for _, s := range info.Scopes {
		granted[s] = struct{}{}
	}

	for _, req := range required {
		if _, ok := granted[req]; !ok {
			return errors.New("insufficient scope: missing " + req)
		}
	}
	return nil
}

// Context returns the enriched context with the validated token info.
func (s *authenticatedStream) Context() context.Context {
	return s.ctx
}
