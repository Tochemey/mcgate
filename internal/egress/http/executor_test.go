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

package http

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tochemey/goakt-mcp/mcp"
)

func TestNewHTTPExecutor_Validation(t *testing.T) {
	t.Run("nil config returns error", func(t *testing.T) {
		exec, err := NewHTTPExecutor(nil, nil, time.Second, nil)
		assert.Nil(t, exec)
		require.Error(t, err)
		var rErr *mcp.RuntimeError
		require.ErrorAs(t, err, &rErr)
		assert.Equal(t, mcp.ErrCodeInvalidRequest, rErr.Code)
	})

	t.Run("empty URL returns error", func(t *testing.T) {
		cfg := &mcp.HTTPTransportConfig{URL: ""}
		exec, err := NewHTTPExecutor(cfg, nil, time.Second, nil)
		assert.Nil(t, exec)
		require.Error(t, err)
		var rErr *mcp.RuntimeError
		require.ErrorAs(t, err, &rErr)
		assert.Equal(t, mcp.ErrCodeInvalidRequest, rErr.Code)
	})

	t.Run("unreachable endpoint returns transport failure", func(t *testing.T) {
		cfg := &mcp.HTTPTransportConfig{URL: "http://127.0.0.1:1/unreachable"}
		exec, err := NewHTTPExecutor(cfg, nil, 500*time.Millisecond, nil)
		assert.Nil(t, exec)
		require.Error(t, err)
		var rErr *mcp.RuntimeError
		require.ErrorAs(t, err, &rErr)
		assert.Equal(t, mcp.ErrCodeTransportFailure, rErr.Code)
	})
}

func TestHTTPExecutor_Execute_NilSession(t *testing.T) {
	e := &HTTPExecutor{}
	inv := &mcp.Invocation{
		ToolID: "test",
		Correlation: mcp.CorrelationMeta{
			RequestID: "req-1",
		},
	}
	result, err := e.Execute(context.Background(), inv)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, mcp.ExecutionStatusFailure, result.Status)
	assert.Equal(t, mcp.ErrCodeTransportFailure, result.Err.Code)
}

func TestHTTPExecutor_Close_Idempotent(t *testing.T) {
	e := &HTTPExecutor{}
	require.NoError(t, e.Close())
	require.NoError(t, e.Close())
}

// startMCPHTTPServer starts an in-process MCP HTTP server with an echo tool.
// Returns the server URL and a cleanup function.
func startMCPHTTPServer(t *testing.T) (url string, cleanup func()) {
	t.Helper()
	echoTool := func(ctx context.Context, req *sdkmcp.CallToolRequest, args map[string]any) (*sdkmcp.CallToolResult, any, error) {
		msg := "ok"
		if v, ok := args["message"].(string); ok && v != "" {
			msg = v
		}
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: msg}},
		}, nil, nil
	}
	errorTool := func(ctx context.Context, req *sdkmcp.CallToolRequest, args map[string]any) (*sdkmcp.CallToolResult, any, error) {
		return &sdkmcp.CallToolResult{
			IsError: true,
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "tool error"}},
		}, nil, nil
	}
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test", Version: "v0.1.0"}, nil)
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "echo", Description: "echo"}, echoTool)
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "error_tool", Description: "returns error"}, errorTool)
	handler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server }, nil)
	srv := httptest.NewServer(handler)
	return srv.URL, srv.Close
}

func TestHTTPExecutor_Execute_Success(t *testing.T) {
	url, cleanup := startMCPHTTPServer(t)
	defer cleanup()

	cfg := &mcp.HTTPTransportConfig{URL: url}
	exec, err := NewHTTPExecutor(cfg, nil, 5*time.Second, nil)
	require.NoError(t, err)
	defer exec.Close()

	inv := &mcp.Invocation{
		ToolID: "echo",
		Method: "tools/call",
		Params: map[string]any{
			"name":      "echo",
			"arguments": map[string]any{"message": "hello"},
		},
		Correlation: mcp.CorrelationMeta{RequestID: "req-1"},
	}
	result, err := exec.Execute(context.Background(), inv)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, mcp.ExecutionStatusSuccess, result.Status)
	assert.Nil(t, result.Err)
	require.NotNil(t, result.Output)
	content, ok := result.Output["content"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, content, 1)
	assert.Equal(t, "hello", content[0]["text"])
}

func TestHTTPExecutor_Execute_IsError(t *testing.T) {
	url, cleanup := startMCPHTTPServer(t)
	defer cleanup()

	cfg := &mcp.HTTPTransportConfig{URL: url}
	exec, err := NewHTTPExecutor(cfg, nil, 5*time.Second, nil)
	require.NoError(t, err)
	defer exec.Close()

	inv := &mcp.Invocation{
		ToolID:      "error_tool",
		Method:      "tools/call",
		Params:      map[string]any{"name": "error_tool", "arguments": map[string]any{}},
		Correlation: mcp.CorrelationMeta{RequestID: "req-1"},
	}
	result, err := exec.Execute(context.Background(), inv)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, mcp.ExecutionStatusFailure, result.Status)
	require.NotNil(t, result.Err)
	assert.Equal(t, mcp.ErrCodeInternal, result.Err.Code)
	assert.Contains(t, result.Err.Message, "tool error")
}

func TestHTTPExecutor_Execute_Timeout(t *testing.T) {
	url, cleanup := startMCPHTTPServer(t)
	defer cleanup()

	cfg := &mcp.HTTPTransportConfig{URL: url}
	exec, err := NewHTTPExecutor(cfg, nil, 5*time.Second, nil)
	require.NoError(t, err)
	defer exec.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately to simulate timeout

	inv := &mcp.Invocation{
		ToolID:      "echo",
		Method:      "tools/call",
		Params:      map[string]any{"name": "echo", "arguments": map[string]any{}},
		Correlation: mcp.CorrelationMeta{RequestID: "req-1"},
	}
	result, err := exec.Execute(ctx, inv)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, mcp.ExecutionStatusTimeout, result.Status)
	require.NotNil(t, result.Err)
	assert.Equal(t, mcp.ErrCodeInvocationTimeout, result.Err.Code)
}

func TestHTTPExecutor_Close_WithSession(t *testing.T) {
	url, cleanup := startMCPHTTPServer(t)
	defer cleanup()

	cfg := &mcp.HTTPTransportConfig{URL: url}
	exec, err := NewHTTPExecutor(cfg, nil, 5*time.Second, nil)
	require.NoError(t, err)

	// Execute once to ensure session is established
	inv := &mcp.Invocation{
		ToolID:      "echo",
		Method:      "tools/call",
		Params:      map[string]any{"name": "echo", "arguments": map[string]any{}},
		Correlation: mcp.CorrelationMeta{RequestID: "req-1"},
	}
	_, err = exec.Execute(context.Background(), inv)
	require.NoError(t, err)

	require.NoError(t, exec.Close())
	require.NoError(t, exec.Close())
}

// customRoundTripper is a non-*http.Transport RoundTripper used to test the
// fallback path in buildHTTPClient where base is not cloneable.
type customRoundTripper struct{}

func (customRoundTripper) RoundTrip(*http.Request) (*http.Response, error) { return nil, nil }

func writeSelfSignedCert(t *testing.T, certFile, keyFile string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	require.NoError(t, os.WriteFile(certFile, certPEM, 0o600))

	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	require.NoError(t, os.WriteFile(keyFile, keyPEM, 0o600))
}

func TestHTTPExecutor_ExecuteStream_Success(t *testing.T) {
	url, cleanup := startMCPHTTPServer(t)
	defer cleanup()

	cfg := &mcp.HTTPTransportConfig{URL: url}
	exec, err := NewHTTPExecutor(cfg, nil, 5*time.Second, nil)
	require.NoError(t, err)
	defer exec.Close()

	inv := &mcp.Invocation{
		ToolID: "echo",
		Method: "tools/call",
		Params: map[string]any{
			"name":      "echo",
			"arguments": map[string]any{"message": "stream-hello"},
		},
		Correlation: mcp.CorrelationMeta{RequestID: "req-stream-1"},
	}
	sr, err := exec.ExecuteStream(context.Background(), inv)
	require.NoError(t, err)
	require.NotNil(t, sr)

	for range sr.Progress { //nolint:revive // intentional channel drain
	}

	result := <-sr.Final
	require.NotNil(t, result)
	assert.Equal(t, mcp.ExecutionStatusSuccess, result.Status)
	assert.Nil(t, result.Err)
	require.NotNil(t, result.Output)
	content, ok := result.Output["content"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, content, 1)
	assert.Equal(t, "stream-hello", content[0]["text"])
}

func TestHTTPExecutor_ExecuteStream_Collect(t *testing.T) {
	url, cleanup := startMCPHTTPServer(t)
	defer cleanup()

	cfg := &mcp.HTTPTransportConfig{URL: url}
	exec, err := NewHTTPExecutor(cfg, nil, 5*time.Second, nil)
	require.NoError(t, err)
	defer exec.Close()

	inv := &mcp.Invocation{
		ToolID: "echo",
		Method: "tools/call",
		Params: map[string]any{
			"name":      "echo",
			"arguments": map[string]any{"message": "collect-test"},
		},
		Correlation: mcp.CorrelationMeta{RequestID: "req-collect-1"},
	}
	sr, err := exec.ExecuteStream(context.Background(), inv)
	require.NoError(t, err)

	result := sr.Collect()
	require.NotNil(t, result)
	assert.Equal(t, mcp.ExecutionStatusSuccess, result.Status)
}

func TestHTTPExecutor_ExecuteStream_NilSession(t *testing.T) {
	e := &HTTPExecutor{}
	inv := &mcp.Invocation{
		ToolID:      "test",
		Correlation: mcp.CorrelationMeta{RequestID: "req-nil-sess"},
	}
	sr, err := e.ExecuteStream(context.Background(), inv)
	require.NoError(t, err)
	require.NotNil(t, sr)

	result := sr.Collect()
	require.NotNil(t, result)
	assert.Equal(t, mcp.ExecutionStatusFailure, result.Status)
	require.NotNil(t, result.Err)
	assert.Equal(t, mcp.ErrCodeTransportFailure, result.Err.Code)
}

func TestHTTPExecutor_ExecuteStream_Timeout(t *testing.T) {
	url, cleanup := startMCPHTTPServer(t)
	defer cleanup()

	cfg := &mcp.HTTPTransportConfig{URL: url}
	exec, err := NewHTTPExecutor(cfg, nil, 5*time.Second, nil)
	require.NoError(t, err)
	defer exec.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	inv := &mcp.Invocation{
		ToolID:      "echo",
		Method:      "tools/call",
		Params:      map[string]any{"name": "echo", "arguments": map[string]any{}},
		Correlation: mcp.CorrelationMeta{RequestID: "req-stream-timeout"},
	}
	sr, err := exec.ExecuteStream(ctx, inv)
	require.NoError(t, err)

	result := sr.Collect()
	require.NotNil(t, result)
	assert.Equal(t, mcp.ExecutionStatusTimeout, result.Status)
	require.NotNil(t, result.Err)
	assert.Equal(t, mcp.ErrCodeInvocationTimeout, result.Err.Code)
}

func TestHTTPExecutor_ExecuteStream_IsError(t *testing.T) {
	url, cleanup := startMCPHTTPServer(t)
	defer cleanup()

	cfg := &mcp.HTTPTransportConfig{URL: url}
	exec, err := NewHTTPExecutor(cfg, nil, 5*time.Second, nil)
	require.NoError(t, err)
	defer exec.Close()

	inv := &mcp.Invocation{
		ToolID:      "error_tool",
		Method:      "tools/call",
		Params:      map[string]any{"name": "error_tool", "arguments": map[string]any{}},
		Correlation: mcp.CorrelationMeta{RequestID: "req-stream-err"},
	}
	sr, err := exec.ExecuteStream(context.Background(), inv)
	require.NoError(t, err)

	result := sr.Collect()
	require.NotNil(t, result)
	assert.Equal(t, mcp.ExecutionStatusFailure, result.Status)
	require.NotNil(t, result.Err)
	assert.Equal(t, mcp.ErrCodeInternal, result.Err.Code)
	assert.Contains(t, result.Err.Message, "tool error")
}

// startMCPHTTPServerWithResources starts an in-process MCP HTTP server with
// an echo tool and resource support. Returns the server URL and a cleanup function.
func startMCPHTTPServerWithResources(t *testing.T) (url string, cleanup func()) {
	t.Helper()
	echoTool := func(ctx context.Context, req *sdkmcp.CallToolRequest, args map[string]any) (*sdkmcp.CallToolResult, any, error) {
		msg := "ok"
		if v, ok := args["message"].(string); ok && v != "" {
			msg = v
		}
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: msg}},
		}, nil, nil
	}
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test-resource", Version: "v0.1.0"}, nil)
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "echo", Description: "echo"}, echoTool)
	server.AddResource(
		&sdkmcp.Resource{URI: "file:///readme.md", Name: "readme", Description: "The readme", MIMEType: "text/markdown"},
		func(ctx context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
			return &sdkmcp.ReadResourceResult{
				Contents: []*sdkmcp.ResourceContents{
					{URI: "file:///readme.md", MIMEType: "text/markdown", Text: "# Hello World"},
				},
			}, nil
		},
	)
	server.AddResourceTemplate(
		&sdkmcp.ResourceTemplate{URITemplate: "file:///{path}", Name: "file", Description: "A file", MIMEType: "application/octet-stream"},
		func(ctx context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
			return &sdkmcp.ReadResourceResult{
				Contents: []*sdkmcp.ResourceContents{
					{URI: req.Params.URI, MIMEType: "text/plain", Text: "content of " + req.Params.URI},
				},
			}, nil
		},
	)
	handler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server }, nil)
	srv := httptest.NewServer(handler)
	return srv.URL, srv.Close
}

func TestHTTPExecutor_ReadResource_NilSession(t *testing.T) {
	e := &HTTPExecutor{}
	inv := &mcp.Invocation{
		ToolID: "test",
		Method: "resources/read",
		Params: map[string]any{"uri": "file:///a.txt"},
		Correlation: mcp.CorrelationMeta{
			RequestID: "req-res-1",
		},
	}
	result, err := e.ReadResource(context.Background(), inv)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, mcp.ExecutionStatusFailure, result.Status)
	assert.Equal(t, mcp.ErrCodeTransportFailure, result.Err.Code)
}

func TestHTTPExecutor_ReadResource_EmptyURI(t *testing.T) {
	url, cleanup := startMCPHTTPServerWithResources(t)
	defer cleanup()

	cfg := &mcp.HTTPTransportConfig{URL: url}
	exec, err := NewHTTPExecutor(cfg, nil, 5*time.Second, nil)
	require.NoError(t, err)
	defer exec.Close()

	inv := &mcp.Invocation{
		ToolID: "test",
		Method: "resources/read",
		Params: map[string]any{},
		Correlation: mcp.CorrelationMeta{
			RequestID: "req-res-2",
		},
	}
	result, err := exec.ReadResource(context.Background(), inv)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, mcp.ExecutionStatusFailure, result.Status)
	assert.Equal(t, mcp.ErrCodeInvalidRequest, result.Err.Code)
	assert.Contains(t, result.Err.Message, "resource URI is required")
}

func TestHTTPExecutor_ReadResource_Success(t *testing.T) {
	url, cleanup := startMCPHTTPServerWithResources(t)
	defer cleanup()

	cfg := &mcp.HTTPTransportConfig{URL: url}
	exec, err := NewHTTPExecutor(cfg, nil, 5*time.Second, nil)
	require.NoError(t, err)
	defer exec.Close()

	inv := &mcp.Invocation{
		ToolID: "test",
		Method: "resources/read",
		Params: map[string]any{"uri": "file:///readme.md"},
		Correlation: mcp.CorrelationMeta{
			RequestID: "req-res-3",
		},
	}
	result, err := exec.ReadResource(context.Background(), inv)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, mcp.ExecutionStatusSuccess, result.Status)
	assert.Nil(t, result.Err)
	require.NotNil(t, result.Output)
	contents, ok := result.Output["contents"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, contents, 1)
	assert.Equal(t, "file:///readme.md", contents[0]["uri"])
	assert.Equal(t, "# Hello World", contents[0]["text"])
}

func TestHTTPExecutor_ReadResource_Timeout(t *testing.T) {
	url, cleanup := startMCPHTTPServerWithResources(t)
	defer cleanup()

	cfg := &mcp.HTTPTransportConfig{URL: url}
	exec, err := NewHTTPExecutor(cfg, nil, 5*time.Second, nil)
	require.NoError(t, err)
	defer exec.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	inv := &mcp.Invocation{
		ToolID: "test",
		Method: "resources/read",
		Params: map[string]any{"uri": "file:///readme.md"},
		Correlation: mcp.CorrelationMeta{
			RequestID: "req-res-timeout",
		},
	}
	result, err := exec.ReadResource(ctx, inv)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, mcp.ExecutionStatusTimeout, result.Status)
	require.NotNil(t, result.Err)
	assert.Equal(t, mcp.ErrCodeInvocationTimeout, result.Err.Code)
}

func TestBuildHTTPClient(t *testing.T) {
	t.Run("nil TLS uses fallback client", func(t *testing.T) {
		fallback := &http.Client{}
		cfg := &mcp.HTTPTransportConfig{URL: "http://example.com", TLS: nil}
		client, err := buildHTTPClient(cfg, fallback)
		require.NoError(t, err)
		assert.Equal(t, fallback, client)
	})

	t.Run("nil TLS nil fallback returns default client", func(t *testing.T) {
		cfg := &mcp.HTTPTransportConfig{URL: "http://example.com", TLS: nil}
		client, err := buildHTTPClient(cfg, nil)
		require.NoError(t, err)
		require.NotNil(t, client)
	})

	t.Run("TLS with invalid CA returns error", func(t *testing.T) {
		dir := t.TempDir()
		badCA := filepath.Join(dir, "bad-ca.crt")
		require.NoError(t, os.WriteFile(badCA, []byte("not-a-cert"), 0o600))

		cfg := &mcp.HTTPTransportConfig{
			URL: "https://example.com",
			TLS: &mcp.TLSClientConfig{CACertFile: badCA},
		}
		_, err := buildHTTPClient(cfg, nil)
		require.Error(t, err)
	})

	t.Run("TLS insecure skip verify with fallback non-http.Transport", func(t *testing.T) {
		fallback := &http.Client{Transport: customRoundTripper{}}
		cfg := &mcp.HTTPTransportConfig{
			URL: "https://example.com",
			TLS: &mcp.TLSClientConfig{InsecureSkipVerify: true},
		}
		client, err := buildHTTPClient(cfg, fallback)
		require.NoError(t, err)
		require.NotNil(t, client)
	})

	t.Run("TLS insecure skip verify with default transport", func(t *testing.T) {
		cfg := &mcp.HTTPTransportConfig{
			URL: "https://example.com",
			TLS: &mcp.TLSClientConfig{InsecureSkipVerify: true},
		}
		client, err := buildHTTPClient(cfg, nil)
		require.NoError(t, err)
		require.NotNil(t, client)
		transport, ok := client.Transport.(*http.Transport)
		require.True(t, ok)
		require.NotNil(t, transport.TLSClientConfig)
		assert.True(t, transport.TLSClientConfig.InsecureSkipVerify) //nolint:gosec
	})

	t.Run("TLS with valid CA cert and default transport", func(t *testing.T) {
		dir := t.TempDir()
		caFile := filepath.Join(dir, "ca.crt")
		keyFile := filepath.Join(dir, "ca.key")
		writeSelfSignedCert(t, caFile, keyFile)

		cfg := &mcp.HTTPTransportConfig{
			URL: "https://example.com",
			TLS: &mcp.TLSClientConfig{CACertFile: caFile},
		}
		client, err := buildHTTPClient(cfg, nil)
		require.NoError(t, err)
		require.NotNil(t, client)
	})

	t.Run("TLS with valid client cert and fallback http.Transport", func(t *testing.T) {
		dir := t.TempDir()
		certFile := filepath.Join(dir, "client.crt")
		keyFile := filepath.Join(dir, "client.key")
		writeSelfSignedCert(t, certFile, keyFile)

		fallback := &http.Client{Transport: &http.Transport{}}
		cfg := &mcp.HTTPTransportConfig{
			URL: "https://example.com",
			TLS: &mcp.TLSClientConfig{
				ClientCertFile: certFile,
				ClientKeyFile:  keyFile,
			},
		}
		client, err := buildHTTPClient(cfg, fallback)
		require.NoError(t, err)
		require.NotNil(t, client)
	})
}

func TestNewHTTPExecutor_CredentialsSentAsHeaders(t *testing.T) {
	// Wrap the MCP handler so every inbound request records the credential
	// headers it carried.
	echoTool := func(ctx context.Context, req *sdkmcp.CallToolRequest, args map[string]any) (*sdkmcp.CallToolResult, any, error) {
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "ok"}},
		}, nil, nil
	}
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test", Version: "v0.1.0"}, nil)
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "echo", Description: "echo"}, echoTool)
	mcpHandler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server }, nil)

	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("X-Api-Key"))
		mu.Unlock()
		mcpHandler.ServeHTTP(w, r)
	}))
	defer srv.Close()

	creds := map[string]string{"X-Api-Key": "s3cret"}
	cfg := &mcp.HTTPTransportConfig{URL: srv.URL}
	exec, err := NewHTTPExecutor(cfg, nil, 5*time.Second, creds)
	require.NoError(t, err)
	defer exec.Close()

	inv := &mcp.Invocation{
		ToolID:      "echo",
		Method:      "tools/call",
		Params:      map[string]any{"name": "echo", "arguments": map[string]any{}},
		Correlation: mcp.CorrelationMeta{RequestID: "req-creds"},
	}
	result, err := exec.Execute(context.Background(), inv)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, mcp.ExecutionStatusSuccess, result.Status)

	// Every outbound request (initialize, notifications, tools/call) must
	// carry the credential header.
	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, seen)
	for i, v := range seen {
		assert.Equal(t, "s3cret", v, "request %d is missing the credential header", i)
	}
}

func TestNewHTTPExecutor_DoesNotMutateFallbackClient(t *testing.T) {
	url, cleanup := startMCPHTTPServer(t)
	defer cleanup()

	// A shared fallback client must never be mutated: wrapping its transport
	// in place would double-wrap otelhttp on every session and race
	// concurrent readers.
	shared := &http.Client{}
	cfg := &mcp.HTTPTransportConfig{URL: url}
	exec, err := NewHTTPExecutor(cfg, shared, 5*time.Second, map[string]string{"X-Api-Key": "s3cret"})
	require.NoError(t, err)
	defer exec.Close()

	assert.Nil(t, shared.Transport, "shared client transport must not be mutated")
}
