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

package mcp_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tochemey/mcgate/mcp"
)

func TestEnterpriseAuthConfig(t *testing.T) {
	t.Run("zero value has nil fields", func(t *testing.T) {
		var cfg mcp.EnterpriseAuthConfig
		assert.Nil(t, cfg.TokenVerifier)
		assert.Nil(t, cfg.ResourceMetadata)
		assert.Nil(t, cfg.RequiredScopes)
		assert.Nil(t, cfg.IdentityMapper)
	})

	t.Run("all fields are settable", func(t *testing.T) {
		verifier := auth.TokenVerifier(func(_ context.Context, _ string, _ *http.Request) (*auth.TokenInfo, error) {
			return &auth.TokenInfo{}, nil
		})
		metadata := &oauthex.ProtectedResourceMetadata{
			Resource:             "https://mcp.example.com/",
			AuthorizationServers: []string{"https://auth.example.com/"},
			ScopesSupported:      []string{"tools:read", "tools:write"},
		}
		mapper := mcp.IdentityMapperFunc(func(_ *auth.TokenInfo) (mcp.TenantID, mcp.ClientID, error) {
			return "t", "c", nil
		})

		cfg := mcp.EnterpriseAuthConfig{
			TokenVerifier:    verifier,
			ResourceMetadata: metadata,
			RequiredScopes:   []string{"tools:read"},
			IdentityMapper:   mapper,
		}
		assert.NotNil(t, cfg.TokenVerifier)
		assert.NotNil(t, cfg.ResourceMetadata)
		assert.Equal(t, []string{"tools:read"}, cfg.RequiredScopes)
		assert.NotNil(t, cfg.IdentityMapper)
	})
}

func TestIdentityMapper(t *testing.T) {
	t.Run("IdentityMapperFunc implements IdentityMapper", func(t *testing.T) {
		var mapper mcp.IdentityMapper = mcp.IdentityMapperFunc(func(info *auth.TokenInfo) (mcp.TenantID, mcp.ClientID, error) {
			return mcp.TenantID(info.Extra["org"].(string)), mcp.ClientID(info.UserID), nil
		})
		info := &auth.TokenInfo{
			UserID: "user-42",
			Extra:  map[string]any{"org": "acme-corp"},
		}
		tenantID, clientID, err := mapper.MapIdentity(info)
		require.NoError(t, err)
		assert.Equal(t, mcp.TenantID("acme-corp"), tenantID)
		assert.Equal(t, mcp.ClientID("user-42"), clientID)
	})

	t.Run("IdentityMapperFunc can return error", func(t *testing.T) {
		mapper := mcp.IdentityMapperFunc(func(_ *auth.TokenInfo) (mcp.TenantID, mcp.ClientID, error) {
			return "", "", mcp.NewRuntimeError(mcp.ErrCodeInvalidRequest, "missing org claim")
		})
		_, _, err := mapper.MapIdentity(&auth.TokenInfo{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing org claim")
	})
}

func TestDefaultIdentityMapper(t *testing.T) {
	mapper := mcp.DefaultIdentityMapper()

	t.Run("maps UserID to ClientID and defaults TenantID", func(t *testing.T) {
		info := &auth.TokenInfo{UserID: "alice"}
		tenantID, clientID, err := mapper.MapIdentity(info)
		require.NoError(t, err)
		assert.Equal(t, mcp.TenantID("default"), tenantID)
		assert.Equal(t, mcp.ClientID("alice"), clientID)
	})

	t.Run("empty UserID defaults ClientID to default", func(t *testing.T) {
		info := &auth.TokenInfo{}
		tenantID, clientID, err := mapper.MapIdentity(info)
		require.NoError(t, err)
		assert.Equal(t, mcp.TenantID("default"), tenantID)
		assert.Equal(t, mcp.ClientID("default"), clientID)
	})

	t.Run("nil TokenInfo defaults both to default", func(t *testing.T) {
		tenantID, clientID, err := mapper.MapIdentity(nil)
		require.NoError(t, err)
		assert.Equal(t, mcp.TenantID("default"), tenantID)
		assert.Equal(t, mcp.ClientID("default"), clientID)
	})
}

func TestNewTokenIdentityResolver(t *testing.T) {
	t.Run("resolves identity from token info in context", func(t *testing.T) {
		mapper := mcp.IdentityMapperFunc(func(info *auth.TokenInfo) (mcp.TenantID, mcp.ClientID, error) {
			return mcp.TenantID(info.Extra["org"].(string)), mcp.ClientID(info.UserID), nil
		})
		resolver := mcp.NewTokenIdentityResolver(mapper)

		info := &auth.TokenInfo{
			UserID:     "bob",
			Expiration: time.Now().Add(time.Hour),
			Extra:      map[string]any{"org": "widgets-inc"},
		}
		req := requestWithTokenInfo(info)

		tenantID, clientID, err := resolver.ResolveIdentity(req)
		require.NoError(t, err)
		assert.Equal(t, mcp.TenantID("widgets-inc"), tenantID)
		assert.Equal(t, mcp.ClientID("bob"), clientID)
	})

	t.Run("returns error when no token info in context", func(t *testing.T) {
		resolver := mcp.NewTokenIdentityResolver(nil)
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)

		_, _, err := resolver.ResolveIdentity(req)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing bearer token context")
	})

	t.Run("nil mapper uses DefaultIdentityMapper", func(t *testing.T) {
		resolver := mcp.NewTokenIdentityResolver(nil)

		info := &auth.TokenInfo{
			UserID:     "carol",
			Expiration: time.Now().Add(time.Hour),
		}
		req := requestWithTokenInfo(info)

		tenantID, clientID, err := resolver.ResolveIdentity(req)
		require.NoError(t, err)
		assert.Equal(t, mcp.TenantID("default"), tenantID)
		assert.Equal(t, mcp.ClientID("carol"), clientID)
	})

	t.Run("propagates mapper error", func(t *testing.T) {
		mapper := mcp.IdentityMapperFunc(func(_ *auth.TokenInfo) (mcp.TenantID, mcp.ClientID, error) {
			return "", "", mcp.NewRuntimeError(mcp.ErrCodeInvalidRequest, "bad token")
		})
		resolver := mcp.NewTokenIdentityResolver(mapper)

		info := &auth.TokenInfo{Expiration: time.Now().Add(time.Hour)}
		req := requestWithTokenInfo(info)

		_, _, err := resolver.ResolveIdentity(req)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "bad token")
	})
}

func TestProtectedResourceMetadataHandler(t *testing.T) {
	metadata := &oauthex.ProtectedResourceMetadata{
		Resource:             "https://mcp.example.com/",
		AuthorizationServers: []string{"https://auth.example.com/"},
		ScopesSupported:      []string{"tools:read", "tools:write"},
	}

	handler := mcp.ProtectedResourceMetadataHandler(metadata)

	t.Run("GET returns JSON metadata with CORS headers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
		assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))

		body, err := io.ReadAll(rec.Body)
		require.NoError(t, err)

		var got oauthex.ProtectedResourceMetadata
		require.NoError(t, json.Unmarshal(body, &got))
		assert.Equal(t, "https://mcp.example.com/", got.Resource)
		assert.Equal(t, []string{"https://auth.example.com/"}, got.AuthorizationServers)
		assert.Equal(t, []string{"tools:read", "tools:write"}, got.ScopesSupported)
	})

	t.Run("OPTIONS returns no content for CORS preflight", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/.well-known/oauth-protected-resource", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNoContent, rec.Code)
		assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
	})

	t.Run("POST returns method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/.well-known/oauth-protected-resource", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	})
}

// requestWithTokenInfo creates an *http.Request with auth.TokenInfo injected
// into the context using the SDK's RequireBearerToken middleware path.
// This simulates what happens after the middleware has validated a token.
func requestWithTokenInfo(info *auth.TokenInfo) *http.Request {
	// Use the SDK's RequireBearerToken middleware to inject TokenInfo the
	// canonical way. We create a verifier that returns the pre-built info,
	// wrap a no-op handler, and execute a synthetic request with a bearer token.
	verifier := auth.TokenVerifier(func(_ context.Context, _ string, _ *http.Request) (*auth.TokenInfo, error) {
		return info, nil
	})
	middleware := auth.RequireBearerToken(verifier, nil)

	var captured *http.Request
	inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		captured = r
	})

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	middleware(inner).ServeHTTP(rec, req)

	if captured == nil {
		// Fallback: return the original request (middleware rejected it).
		return req
	}
	return captured
}
