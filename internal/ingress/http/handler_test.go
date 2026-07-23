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

package http_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ingresshttp "github.com/tochemey/portcullis/internal/ingress/http"
	"github.com/tochemey/portcullis/internal/ingress/pkg"
	"github.com/tochemey/portcullis/mcp"
)

// --- test doubles ------------------------------------------------------------

// fixedResolver always returns the configured identity.
type fixedResolver struct {
	tenantID mcp.TenantID
	clientID mcp.ClientID
}

func (r *fixedResolver) ResolveIdentity(_ *http.Request) (mcp.TenantID, mcp.ClientID, error) {
	return r.tenantID, r.clientID, nil
}

// errResolver always returns an error, causing session rejection.
type errResolver struct{}

func (r *errResolver) ResolveIdentity(_ *http.Request) (mcp.TenantID, mcp.ClientID, error) {
	return "", "", errors.New("unauthorized")
}

// fakeInvoker implements [pkg.Invoker] for tests.
type fakeInvoker struct {
	tools  []mcp.Tool
	result *mcp.ExecutionResult
	err    error

	listErr error
}

func (f *fakeInvoker) Invoke(_ context.Context, _ *mcp.Invocation) (*mcp.ExecutionResult, error) {
	return f.result, f.err
}

func (f *fakeInvoker) ListTools(_ context.Context) ([]mcp.Tool, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.tools, nil
}

// --- helpers -----------------------------------------------------------------

// newTestServer spins up an httptest.Server hosting the ingress handler and
// returns both the server and a connected MCP client session. It registers
// t.Cleanup to close both.
func newTestSession(
	t *testing.T,
	gw pkg.Invoker,
	cfg mcp.IngressConfig,
) *sdkmcp.ClientSession {
	t.Helper()

	h, err := ingresshttp.New(gw, cfg)
	require.NoError(t, err)

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client"}, nil)
	transport := &sdkmcp.StreamableClientTransport{
		Endpoint:             srv.URL,
		DisableStandaloneSSE: true,
	}

	ctx := context.Background()
	session, err := client.Connect(ctx, transport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	return session
}

// --- tests -------------------------------------------------------------------

func TestNew_NilIdentityResolver(t *testing.T) {
	_, err := ingresshttp.New(&fakeInvoker{}, mcp.IngressConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "IdentityResolver")
}

func TestHandler_ToolCallSuccess(t *testing.T) {
	gw := &fakeInvoker{
		tools: []mcp.Tool{{ID: "echo"}},
		result: &mcp.ExecutionResult{
			Status: mcp.ExecutionStatusSuccess,
			Output: map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": "pong"},
				},
			},
		},
	}

	cfg := mcp.IngressConfig{
		IdentityResolver: &fixedResolver{tenantID: "acme", clientID: "c1"},
	}

	session := newTestSession(t, gw, cfg)

	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "echo",
		Arguments: map[string]any{"msg": "ping"},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.IsError)
	require.Len(t, result.Content, 1)
	txt, ok := result.Content[0].(*sdkmcp.TextContent)
	require.True(t, ok)
	assert.Equal(t, "pong", txt.Text)
}

func TestHandler_ToolCallError(t *testing.T) {
	gw := &fakeInvoker{
		tools: []mcp.Tool{{ID: "fail-tool"}},
		result: &mcp.ExecutionResult{
			Status: mcp.ExecutionStatusFailure,
			Err:    mcp.NewRuntimeError(mcp.ErrCodeInternal, "backend error"),
		},
	}

	cfg := mcp.IngressConfig{
		IdentityResolver: &fixedResolver{tenantID: "acme", clientID: "c2"},
	}

	session := newTestSession(t, gw, cfg)

	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "fail-tool",
	})
	require.NoError(t, err) // protocol-level must not error; tool error is in result
	require.NotNil(t, result)
	assert.True(t, result.IsError)
}

func TestHandler_IdentityResolutionFailure(t *testing.T) {
	// When IdentityResolver returns an error, getServer returns nil,
	// which causes the SDK to reply with 400 Bad Request.
	gw := &fakeInvoker{tools: []mcp.Tool{{ID: "any"}}}
	cfg := mcp.IngressConfig{
		IdentityResolver: &errResolver{},
	}

	h, err := ingresshttp.New(gw, cfg)
	require.NoError(t, err)

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "bad-client"}, nil)
	transport := &sdkmcp.StreamableClientTransport{
		Endpoint:             srv.URL,
		DisableStandaloneSSE: true,
	}

	_, err = client.Connect(context.Background(), transport, nil)
	// Connection must fail since the server returns 400
	require.Error(t, err)
}

func TestHandler_ToolWithSchemasRegistersPerSchema(t *testing.T) {
	gw := &fakeInvoker{
		tools: []mcp.Tool{{
			ID: "multi-tool",
			Schemas: []mcp.ToolSchema{
				{Name: "read_file", Description: "Read a file", InputSchema: []byte(`{"type":"object","properties":{"path":{"type":"string"}}}`)},
				{Name: "write_file", Description: "Write a file", InputSchema: []byte(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}}}`)},
			},
		}},
		result: &mcp.ExecutionResult{
			Status: mcp.ExecutionStatusSuccess,
			Output: map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": "ok"},
				},
			},
		},
	}

	cfg := mcp.IngressConfig{
		IdentityResolver: &fixedResolver{tenantID: "acme", clientID: "c1"},
	}

	session := newTestSession(t, gw, cfg)

	// List tools should return both schema-derived tools
	toolsResult, err := session.ListTools(context.Background(), &sdkmcp.ListToolsParams{})
	require.NoError(t, err)
	require.Len(t, toolsResult.Tools, 2)

	names := make(map[string]bool)
	for _, tool := range toolsResult.Tools {
		names[tool.Name] = true
	}
	assert.True(t, names["read_file"])
	assert.True(t, names["write_file"])

	// Call one of the schema-derived tools
	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "read_file",
		Arguments: map[string]any{"path": "/tmp/test"},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.IsError)
}

func TestHandler_ToolWithoutSchemasFallsBackToOpenObject(t *testing.T) {
	gw := &fakeInvoker{
		tools: []mcp.Tool{{ID: "simple-tool"}},
		result: &mcp.ExecutionResult{
			Status: mcp.ExecutionStatusSuccess,
			Output: map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": "ok"},
				},
			},
		},
	}

	cfg := mcp.IngressConfig{
		IdentityResolver: &fixedResolver{tenantID: "acme", clientID: "c1"},
	}

	session := newTestSession(t, gw, cfg)

	// List tools should return the single tool with its ID as name
	toolsResult, err := session.ListTools(context.Background(), &sdkmcp.ListToolsParams{})
	require.NoError(t, err)
	require.Len(t, toolsResult.Tools, 1)
	assert.Equal(t, "simple-tool", toolsResult.Tools[0].Name)
}

func TestHandler_ListToolsFailure(t *testing.T) {
	// When ListTools fails, getServer returns nil → SDK replies 400.
	gw := &fakeInvoker{listErr: errors.New("registry unavailable")}
	cfg := mcp.IngressConfig{
		IdentityResolver: &fixedResolver{tenantID: "acme", clientID: "c3"},
	}

	h, err := ingresshttp.New(gw, cfg)
	require.NoError(t, err)

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "list-err-client"}, nil)
	transport := &sdkmcp.StreamableClientTransport{
		Endpoint:             srv.URL,
		DisableStandaloneSSE: true,
	}

	_, err = client.Connect(context.Background(), transport, nil)
	require.Error(t, err)
}

// --- Enterprise Auth tests --------------------------------------------------

func validEnterpriseAuthConfig() *mcp.EnterpriseAuthConfig {
	return &mcp.EnterpriseAuthConfig{
		TokenVerifier: auth.TokenVerifier(func(_ context.Context, _ string, _ *http.Request) (*auth.TokenInfo, error) {
			return &auth.TokenInfo{
				UserID:     "user-42",
				Scopes:     []string{"tools:read"},
				Expiration: time.Now().Add(time.Hour),
			}, nil
		}),
		ResourceMetadata: &oauthex.ProtectedResourceMetadata{
			Resource:             "https://mcp.example.com/",
			AuthorizationServers: []string{"https://auth.example.com/"},
		},
	}
}

func TestNew_EnterpriseAuth_NilTokenVerifier(t *testing.T) {
	_, err := ingresshttp.New(&fakeInvoker{}, mcp.IngressConfig{
		EnterpriseAuth: &mcp.EnterpriseAuthConfig{
			ResourceMetadata: &oauthex.ProtectedResourceMetadata{
				Resource: "https://mcp.example.com/",
			},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TokenVerifier must not be nil")
}

func TestNew_EnterpriseAuth_NilResourceMetadata(t *testing.T) {
	_, err := ingresshttp.New(&fakeInvoker{}, mcp.IngressConfig{
		EnterpriseAuth: &mcp.EnterpriseAuthConfig{
			TokenVerifier: auth.TokenVerifier(func(_ context.Context, _ string, _ *http.Request) (*auth.TokenInfo, error) {
				return &auth.TokenInfo{Expiration: time.Now().Add(time.Hour)}, nil
			}),
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ResourceMetadata must not be nil")
}

func TestNew_EnterpriseAuth_AutoInstallsIdentityResolver(t *testing.T) {
	// When EnterpriseAuth is set and IdentityResolver is nil, New should
	// succeed because ResolveAuthDefaults installs a TokenIdentityResolver.
	gw := &fakeInvoker{tools: []mcp.Tool{{ID: "echo"}}}
	_, err := ingresshttp.New(gw, mcp.IngressConfig{
		EnterpriseAuth: validEnterpriseAuthConfig(),
	})
	require.NoError(t, err)
}

func TestHandler_EnterpriseAuth_RejectsRequestWithoutToken(t *testing.T) {
	gw := &fakeInvoker{tools: []mcp.Tool{{ID: "echo"}}}
	h, err := ingresshttp.New(gw, mcp.IngressConfig{
		EnterpriseAuth: validEnterpriseAuthConfig(),
	})
	require.NoError(t, err)

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	// Request without Authorization header should get 401.
	resp, err := http.Post(srv.URL, "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("WWW-Authenticate"), "Bearer")
}

func TestHandler_EnterpriseAuth_RejectsInvalidToken(t *testing.T) {
	eaCfg := &mcp.EnterpriseAuthConfig{
		TokenVerifier: auth.TokenVerifier(func(_ context.Context, _ string, _ *http.Request) (*auth.TokenInfo, error) {
			return nil, auth.ErrInvalidToken
		}),
		ResourceMetadata: &oauthex.ProtectedResourceMetadata{
			Resource:             "https://mcp.example.com/",
			AuthorizationServers: []string{"https://auth.example.com/"},
		},
	}

	gw := &fakeInvoker{tools: []mcp.Tool{{ID: "echo"}}}
	h, err := ingresshttp.New(gw, mcp.IngressConfig{
		EnterpriseAuth: eaCfg,
	})
	require.NoError(t, err)

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	req, _ := http.NewRequest(http.MethodPost, srv.URL, nil)
	req.Header.Set("Authorization", "Bearer bad-token")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestHandler_EnterpriseAuth_AcceptsValidToken(t *testing.T) {
	gw := &fakeInvoker{
		tools: []mcp.Tool{{ID: "echo"}},
		result: &mcp.ExecutionResult{
			Status: mcp.ExecutionStatusSuccess,
			Output: map[string]any{
				"content": []map[string]any{{"type": "text", "text": "pong"}},
			},
		},
	}

	h, err := ingresshttp.New(gw, mcp.IngressConfig{
		EnterpriseAuth: validEnterpriseAuthConfig(),
	})
	require.NoError(t, err)

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	// Use the SDK client with a custom transport that injects the token.
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "auth-client"}, nil)
	transport := &sdkmcp.StreamableClientTransport{
		Endpoint:             srv.URL,
		DisableStandaloneSSE: true,
		HTTPClient: &http.Client{
			Transport: &bearerTokenTransport{token: "valid-token"},
		},
	}

	session, err := client.Connect(context.Background(), transport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "echo",
		Arguments: map[string]any{"msg": "ping"},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.IsError)
}

func TestHandler_EnterpriseAuth_RequiredScopeEnforcement(t *testing.T) {
	eaCfg := &mcp.EnterpriseAuthConfig{
		TokenVerifier: auth.TokenVerifier(func(_ context.Context, _ string, _ *http.Request) (*auth.TokenInfo, error) {
			return &auth.TokenInfo{
				UserID:     "user-1",
				Scopes:     []string{"tools:read"}, // only read, not write
				Expiration: time.Now().Add(time.Hour),
			}, nil
		}),
		ResourceMetadata: &oauthex.ProtectedResourceMetadata{
			Resource: "https://mcp.example.com/",
		},
		RequiredScopes: []string{"tools:read", "tools:write"}, // requires write too
	}

	gw := &fakeInvoker{tools: []mcp.Tool{{ID: "echo"}}}
	h, err := ingresshttp.New(gw, mcp.IngressConfig{
		EnterpriseAuth: eaCfg,
	})
	require.NoError(t, err)

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	req, _ := http.NewRequest(http.MethodPost, srv.URL, nil)
	req.Header.Set("Authorization", "Bearer some-token")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	// Missing required scope should result in 403 Forbidden.
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

// bearerTokenTransport injects an Authorization header into every request.
type bearerTokenTransport struct {
	token string
}

func (t *bearerTokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(req)
}

// TestHandler_Legacy20251125Client verifies the stateless (MCP 2026-07-28)
// handler still serves clients speaking the previous protocol revision: a raw
// 2025-11-25 initialize succeeds and negotiates that version, and a
// subsequent tools/call with no Mcp-Session-Id header round-trips through the
// gateway via the transport's temporary per-request session.
func TestHandler_Legacy20251125Client(t *testing.T) {
	gw := &fakeInvoker{
		tools: []mcp.Tool{{ID: "echo"}},
		result: &mcp.ExecutionResult{
			Status: mcp.ExecutionStatusSuccess,
			Output: map[string]any{
				"content": []map[string]any{{"type": "text", "text": "legacy ok"}},
			},
		},
	}

	h, err := ingresshttp.New(gw, mcp.IngressConfig{
		IdentityResolver: &fixedResolver{tenantID: "acme", clientID: "c1"},
	})
	require.NoError(t, err)

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	post := func(body string) (int, string) {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		resp, err := srv.Client().Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return resp.StatusCode, string(data)
	}

	// Legacy handshake: initialize at 2025-11-25.
	status, body := post(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"legacy-client","version":"1.0"}}}`)
	require.Equal(t, http.StatusOK, status)
	assert.Contains(t, body, `"2025-11-25"`, "server must negotiate the requested legacy protocol version")

	// Tool call with NO session header: the stateless transport must serve it
	// through a temporary session.
	status, body = post(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"echo","arguments":{}}}`)
	require.Equal(t, http.StatusOK, status)
	assert.Contains(t, body, "legacy ok")
	assert.NotContains(t, body, `"isError":true`)
}
