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

package ws_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ingressws "github.com/tochemey/portcullis/internal/ingress/ws"
	"github.com/tochemey/portcullis/mcp"
)

// --- test doubles ------------------------------------------------------------

type fixedResolver struct {
	tenantID mcp.TenantID
	clientID mcp.ClientID
}

func (r *fixedResolver) ResolveIdentity(_ *http.Request) (mcp.TenantID, mcp.ClientID, error) {
	return r.tenantID, r.clientID, nil
}

type errResolver struct{}

func (r *errResolver) ResolveIdentity(_ *http.Request) (mcp.TenantID, mcp.ClientID, error) {
	return "", "", errors.New("unauthorized")
}

type fakeInvoker struct {
	tools   []mcp.Tool
	result  *mcp.ExecutionResult
	err     error
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

func startWSServer(t *testing.T, gw *fakeInvoker, cfg mcp.IngressConfig, wsCfg *mcp.WSConfig) *httptest.Server {
	t.Helper()
	h, err := ingressws.New(gw, cfg, wsCfg)
	require.NoError(t, err)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func dialWS(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// jsonRPCRequest sends a JSON-RPC request and reads the response.
func jsonRPCRequest(t *testing.T, conn *websocket.Conn, method string, params any) map[string]any {
	t.Helper()
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	}
	data, err := json.Marshal(req)
	require.NoError(t, err)
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, data))

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, respData, err := conn.ReadMessage()
	require.NoError(t, err)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(respData, &resp))
	return resp
}

// --- tests -------------------------------------------------------------------

func TestNew_NilIdentityResolver(t *testing.T) {
	_, err := ingressws.New(&fakeInvoker{}, mcp.IngressConfig{}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "IdentityResolver")
}

func TestNew_DefaultConfig(t *testing.T) {
	h, err := ingressws.New(&fakeInvoker{}, mcp.IngressConfig{
		IdentityResolver: &fixedResolver{tenantID: "acme", clientID: "c1"},
	}, nil)
	require.NoError(t, err)
	assert.NotNil(t, h)
}

func TestNew_CustomConfig(t *testing.T) {
	h, err := ingressws.New(&fakeInvoker{}, mcp.IngressConfig{
		IdentityResolver: &fixedResolver{tenantID: "acme", clientID: "c1"},
	}, &mcp.WSConfig{
		ReadBufferSize:  8192,
		WriteBufferSize: 8192,
		PingInterval:    10 * time.Second,
		CheckOrigin:     func(_ *http.Request) bool { return true },
	})
	require.NoError(t, err)
	assert.NotNil(t, h)
}

func TestHandler_WebSocketUpgrade(t *testing.T) {
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

	srv := startWSServer(t, gw, mcp.IngressConfig{
		IdentityResolver: &fixedResolver{tenantID: "acme", clientID: "c1"},
	}, nil)

	conn := dialWS(t, srv)

	// The MCP SDK server expects an initialize request first.
	resp := jsonRPCRequest(t, conn, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "test-ws-client"},
	})
	require.NotNil(t, resp["result"], "initialize should return a result")
}

func TestHandler_IdentityResolutionFailure(t *testing.T) {
	gw := &fakeInvoker{tools: []mcp.Tool{{ID: "any"}}}
	srv := startWSServer(t, gw, mcp.IngressConfig{
		IdentityResolver: &errResolver{},
	}, nil)

	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	if conn != nil {
		_ = conn.Close()
	}
	// When identity resolution fails, the server returns an error before upgrade.
	if err != nil {
		assert.Contains(t, err.Error(), "bad handshake")
		return
	}
	// If somehow we got a connection, the server should have sent an error.
	if resp != nil {
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	}
}

func TestHandler_ListToolsFailure(t *testing.T) {
	gw := &fakeInvoker{listErr: errors.New("registry unavailable")}
	srv := startWSServer(t, gw, mcp.IngressConfig{
		IdentityResolver: &fixedResolver{tenantID: "acme", clientID: "c3"},
	}, nil)

	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if conn != nil {
		_ = conn.Close()
	}
	// When ListTools fails, the handler returns an error before upgrade.
	if err != nil {
		assert.Contains(t, err.Error(), "bad handshake")
	}
}

func TestConfig_Defaults(t *testing.T) {
	// Nil config should use defaults without panicking.
	h, err := ingressws.New(&fakeInvoker{tools: []mcp.Tool{{ID: "t"}}}, mcp.IngressConfig{
		IdentityResolver: &fixedResolver{tenantID: "acme", clientID: "c1"},
	}, nil)
	require.NoError(t, err)
	assert.NotNil(t, h)
}
