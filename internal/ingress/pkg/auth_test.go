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

package pkg

import (
	"context"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tochemey/portcullis/mcp"
)

// --- ResolveAuthDefaults ---

func TestResolveAuthDefaults(t *testing.T) {
	validVerifier := auth.TokenVerifier(func(_ context.Context, _ string, _ *http.Request) (*auth.TokenInfo, error) {
		return &auth.TokenInfo{}, nil
	})
	validMetadata := &oauthex.ProtectedResourceMetadata{
		Resource:             "https://mcp.example.com/",
		AuthorizationServers: []string{"https://auth.example.com/"},
	}

	t.Run("nil EnterpriseAuth is a no-op", func(t *testing.T) {
		cfg := mcp.IngressConfig{}
		err := ResolveAuthDefaults(&cfg)
		require.NoError(t, err)
		assert.Nil(t, cfg.EnterpriseAuth)
		assert.Nil(t, cfg.IdentityResolver)
	})

	t.Run("nil TokenVerifier returns error", func(t *testing.T) {
		cfg := mcp.IngressConfig{
			EnterpriseAuth: &mcp.EnterpriseAuthConfig{
				ResourceMetadata: validMetadata,
			},
		}
		err := ResolveAuthDefaults(&cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "TokenVerifier must not be nil")
	})

	t.Run("nil ResourceMetadata returns error", func(t *testing.T) {
		cfg := mcp.IngressConfig{
			EnterpriseAuth: &mcp.EnterpriseAuthConfig{
				TokenVerifier: validVerifier,
			},
		}
		err := ResolveAuthDefaults(&cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ResourceMetadata must not be nil")
	})

	t.Run("empty Resource returns error", func(t *testing.T) {
		cfg := mcp.IngressConfig{
			EnterpriseAuth: &mcp.EnterpriseAuthConfig{
				TokenVerifier:    validVerifier,
				ResourceMetadata: &oauthex.ProtectedResourceMetadata{},
			},
		}
		err := ResolveAuthDefaults(&cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Resource must not be empty")
	})

	t.Run("valid config with nil IdentityResolver installs TokenIdentityResolver", func(t *testing.T) {
		cfg := mcp.IngressConfig{
			EnterpriseAuth: &mcp.EnterpriseAuthConfig{
				TokenVerifier:    validVerifier,
				ResourceMetadata: validMetadata,
			},
		}
		err := ResolveAuthDefaults(&cfg)
		require.NoError(t, err)
		assert.NotNil(t, cfg.IdentityResolver)
	})

	t.Run("valid config preserves explicit IdentityResolver", func(t *testing.T) {
		explicit := &staticIdentityResolver{tenantID: "custom", clientID: "c1"}
		cfg := mcp.IngressConfig{
			IdentityResolver: explicit,
			EnterpriseAuth: &mcp.EnterpriseAuthConfig{
				TokenVerifier:    validVerifier,
				ResourceMetadata: validMetadata,
			},
		}
		err := ResolveAuthDefaults(&cfg)
		require.NoError(t, err)
		assert.Equal(t, explicit, cfg.IdentityResolver)
	})

	t.Run("valid config with custom IdentityMapper installs resolver using it", func(t *testing.T) {
		mapper := mcp.IdentityMapperFunc(func(info *auth.TokenInfo) (mcp.TenantID, mcp.ClientID, error) {
			return "custom-tenant", "custom-client", nil
		})
		cfg := mcp.IngressConfig{
			EnterpriseAuth: &mcp.EnterpriseAuthConfig{
				TokenVerifier:    validVerifier,
				ResourceMetadata: validMetadata,
				IdentityMapper:   mapper,
			},
		}
		err := ResolveAuthDefaults(&cfg)
		require.NoError(t, err)
		assert.NotNil(t, cfg.IdentityResolver)
	})
}

// --- ResourceMetadataURL ---

func TestResourceMetadataURL(t *testing.T) {
	t.Run("nil metadata returns empty string", func(t *testing.T) {
		assert.Empty(t, ResourceMetadataURL(nil))
	})

	t.Run("empty resource returns empty string", func(t *testing.T) {
		assert.Empty(t, ResourceMetadataURL(&oauthex.ProtectedResourceMetadata{}))
	})

	t.Run("resource with trailing slash", func(t *testing.T) {
		url := ResourceMetadataURL(&oauthex.ProtectedResourceMetadata{
			Resource: "https://mcp.example.com/",
		})
		assert.Equal(t, "https://mcp.example.com/.well-known/oauth-protected-resource", url)
	})

	t.Run("resource without trailing slash", func(t *testing.T) {
		url := ResourceMetadataURL(&oauthex.ProtectedResourceMetadata{
			Resource: "https://mcp.example.com",
		})
		assert.Equal(t, "https://mcp.example.com/.well-known/oauth-protected-resource", url)
	})

	t.Run("resource with path inserts well-known between host and path", func(t *testing.T) {
		// RFC 9728 §3.1: the well-known path component goes between the
		// origin and the resource's path, not appended after it.
		url := ResourceMetadataURL(&oauthex.ProtectedResourceMetadata{
			Resource: "https://example.com/mcp/v1",
		})
		assert.Equal(t, "https://example.com/.well-known/oauth-protected-resource/mcp/v1", url)
	})

	t.Run("resource with path and trailing slash", func(t *testing.T) {
		url := ResourceMetadataURL(&oauthex.ProtectedResourceMetadata{
			Resource: "https://example.com/mcp/",
		})
		assert.Equal(t, "https://example.com/.well-known/oauth-protected-resource/mcp", url)
	})
}

// staticIdentityResolver is a test helper.
type staticIdentityResolver struct {
	tenantID mcp.TenantID
	clientID mcp.ClientID
}

func (r *staticIdentityResolver) ResolveIdentity(_ *http.Request) (mcp.TenantID, mcp.ClientID, error) {
	return r.tenantID, r.clientID, nil
}
