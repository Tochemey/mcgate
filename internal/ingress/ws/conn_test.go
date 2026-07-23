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

package ws

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tochemey/portcullis/mcp"
)

// echoWSServer creates a test server that upgrades to WebSocket and echoes messages.
func echoWSServer(t *testing.T) (*httptest.Server, func()) {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(_ *http.Request) bool { return true }}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if err := conn.WriteMessage(mt, msg); err != nil {
				return
			}
		}
	})
	srv := httptest.NewServer(handler)
	return srv, srv.Close
}

// dialWSConn dials the test server and returns a wsConn and client-side websocket.Conn.
func dialWSConn(t *testing.T, srv *httptest.Server) (*wsConn, *websocket.Conn) {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	clientConn, _, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)

	// The server side is handled by the echo handler.
	// We use the client-side connection in a wsConn for testing.
	wc := newWSConn(clientConn, "test-session-1")
	return wc, clientConn
}

func TestWSConn_SessionID(t *testing.T) {
	srv, cleanup := echoWSServer(t)
	defer cleanup()

	wc, _ := dialWSConn(t, srv)
	defer wc.Close()

	assert.Equal(t, "test-session-1", wc.SessionID())
}

func TestWSConn_WriteAndRead(t *testing.T) {
	srv, cleanup := echoWSServer(t)
	defer cleanup()

	wc, _ := dialWSConn(t, srv)
	defer wc.Close()

	ctx := context.Background()

	// Create a JSON-RPC request message.
	reqData := []byte(`{"jsonrpc":"2.0","id":1,"method":"ping","params":{}}`)
	msg, err := jsonrpc.DecodeMessage(reqData)
	require.NoError(t, err)

	// Write message through wsConn.
	require.NoError(t, wc.Write(ctx, msg))

	// Read the echoed message back.
	readMsg, err := wc.Read(ctx)
	require.NoError(t, err)
	require.NotNil(t, readMsg)

	// Verify the echoed message matches.
	encoded, err := jsonrpc.EncodeMessage(readMsg)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	assert.Equal(t, "ping", decoded["method"])
}

func TestWSConn_ReadCancelled(t *testing.T) {
	srv, cleanup := echoWSServer(t)
	defer cleanup()

	wc, _ := dialWSConn(t, srv)
	defer wc.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := wc.Read(ctx)
	require.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}

func TestWSConn_WriteCancelled(t *testing.T) {
	srv, cleanup := echoWSServer(t)
	defer cleanup()

	wc, _ := dialWSConn(t, srv)
	defer wc.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	reqData := []byte(`{"jsonrpc":"2.0","id":1,"method":"test","params":{}}`)
	msg, err := jsonrpc.DecodeMessage(reqData)
	require.NoError(t, err)

	err = wc.Write(ctx, msg)
	require.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}

func TestWSConn_Close_Idempotent(t *testing.T) {
	srv, cleanup := echoWSServer(t)
	defer cleanup()

	wc, _ := dialWSConn(t, srv)
	require.NoError(t, wc.Close())
	require.NoError(t, wc.Close()) // second close is a no-op
}

func TestWSConn_ReadAfterClose(t *testing.T) {
	srv, cleanup := echoWSServer(t)
	defer cleanup()

	wc, _ := dialWSConn(t, srv)
	require.NoError(t, wc.Close())

	_, err := wc.Read(context.Background())
	require.Error(t, err)
	assert.Equal(t, io.EOF, err)
}

func TestWSConn_WriteAfterClose(t *testing.T) {
	srv, cleanup := echoWSServer(t)
	defer cleanup()

	wc, _ := dialWSConn(t, srv)
	require.NoError(t, wc.Close())

	reqData := []byte(`{"jsonrpc":"2.0","id":1,"method":"test","params":{}}`)
	msg, err := jsonrpc.DecodeMessage(reqData)
	require.NoError(t, err)

	err = wc.Write(context.Background(), msg)
	require.Error(t, err)
	assert.Equal(t, io.EOF, err)
}

func TestWSTransport_Connect(t *testing.T) {
	srv, cleanup := echoWSServer(t)
	defer cleanup()

	wc, _ := dialWSConn(t, srv)
	defer wc.Close()

	transport := &wsTransport{conn: wc}
	conn, err := transport.Connect(context.Background())
	require.NoError(t, err)
	assert.Equal(t, wc, conn)
}

func TestIsTimeout(t *testing.T) {
	t.Run("timeout error returns true", func(t *testing.T) {
		err := &timeoutErr{timeout: true}
		assert.True(t, isTimeout(err))
	})

	t.Run("non-timeout error returns false", func(t *testing.T) {
		err := &timeoutErr{timeout: false}
		assert.False(t, isTimeout(err))
	})

	t.Run("regular error returns false", func(t *testing.T) {
		assert.False(t, isTimeout(io.EOF))
	})

	t.Run("nil-like error returns false", func(t *testing.T) {
		assert.False(t, isTimeout(io.ErrUnexpectedEOF))
	})
}

// timeoutErr implements the net.Error interface for testing isTimeout.
type timeoutErr struct {
	timeout bool
}

func (e *timeoutErr) Error() string   { return "timeout error" }
func (e *timeoutErr) Timeout() bool   { return e.timeout }
func (e *timeoutErr) Temporary() bool { return false }

func TestNewSessionID(t *testing.T) {
	id1 := newSessionID()
	id2 := newSessionID()
	assert.NotEmpty(t, id1)
	assert.NotEmpty(t, id2)
	assert.NotEqual(t, id1, id2)
	assert.Len(t, id1, 32) // 16 bytes hex-encoded
}

func TestConfig_Methods(t *testing.T) {
	t.Run("nil config returns defaults", func(t *testing.T) {
		var c *mcp.WSConfig
		assert.Equal(t, defaultReadBufferSize, readBufferSize(c))
		assert.Equal(t, defaultWriteBufferSize, writeBufferSize(c))
		assert.Equal(t, defaultPingInterval, pingInterval(c))
		assert.Equal(t, int64(defaultMaxMessageSize), maxMessageSize(c))
		// A nil origin check selects gorilla/websocket's default same-origin
		// policy; returning a permissive func here would allow cross-site
		// WebSocket hijacking by default.
		assert.Nil(t, checkOrigin(c))
	})

	t.Run("zero values return defaults", func(t *testing.T) {
		c := &mcp.WSConfig{}
		assert.Equal(t, defaultReadBufferSize, readBufferSize(c))
		assert.Equal(t, defaultWriteBufferSize, writeBufferSize(c))
		assert.Equal(t, defaultPingInterval, pingInterval(c))
		assert.Equal(t, int64(defaultMaxMessageSize), maxMessageSize(c))
	})

	t.Run("custom values used", func(t *testing.T) {
		c := &mcp.WSConfig{
			ReadBufferSize:  8192,
			WriteBufferSize: 16384,
			PingInterval:    10 * time.Second,
			MaxMessageSize:  1 << 20,
			CheckOrigin:     func(_ *http.Request) bool { return false },
		}
		assert.Equal(t, 8192, readBufferSize(c))
		assert.Equal(t, 16384, writeBufferSize(c))
		assert.Equal(t, 10*time.Second, pingInterval(c))
		assert.Equal(t, int64(1<<20), maxMessageSize(c))
		assert.False(t, checkOrigin(c)(nil))
	})

	t.Run("negative max message size disables the limit", func(t *testing.T) {
		c := &mcp.WSConfig{MaxMessageSize: -1}
		assert.Equal(t, int64(0), maxMessageSize(c))
	})
}
