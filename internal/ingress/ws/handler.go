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
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/modelcontextprotocol/go-sdk/auth"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/tochemey/goakt-mcp/internal/ingress/pkg"
	"github.com/tochemey/goakt-mcp/mcp"
)

const (
	defaultReadBufferSize  = 4096
	defaultWriteBufferSize = 4096
	defaultPingInterval    = 30 * time.Second
	defaultMaxMessageSize  = 4 << 20 // 4 MiB, matches the gRPC default recv limit
)

// wsHandler serves MCP sessions over WebSocket by upgrading each incoming
// HTTP request, creating an SDK server session on top of the upgraded
// connection, and driving keepalive pings on a fixed interval.
type wsHandler struct {
	getServer      func(*http.Request) *sdkmcp.Server
	upgrader       *websocket.Upgrader
	pingInterval   time.Duration
	maxMessageSize int64
}

func readBufferSize(c *mcp.WSConfig) int {
	if c != nil && c.ReadBufferSize > 0 {
		return c.ReadBufferSize
	}
	return defaultReadBufferSize
}

func writeBufferSize(c *mcp.WSConfig) int {
	if c != nil && c.WriteBufferSize > 0 {
		return c.WriteBufferSize
	}
	return defaultWriteBufferSize
}

func pingInterval(c *mcp.WSConfig) time.Duration {
	if c != nil && c.PingInterval > 0 {
		return c.PingInterval
	}
	return defaultPingInterval
}

func maxMessageSize(c *mcp.WSConfig) int64 {
	if c != nil && c.MaxMessageSize != 0 {
		if c.MaxMessageSize < 0 {
			return 0 // negative disables the limit
		}
		return c.MaxMessageSize
	}
	return defaultMaxMessageSize
}

// checkOrigin returns the configured origin check, or nil to use
// gorilla/websocket's default same-origin policy: reject when an Origin
// header is present and does not match the request Host. Cross-origin
// browser access must be opted into explicitly via WSConfig.CheckOrigin,
// otherwise any website could open a WebSocket to the gateway with the
// victim's ambient credentials (cross-site WebSocket hijacking).
func checkOrigin(c *mcp.WSConfig) func(r *http.Request) bool {
	if c != nil && c.CheckOrigin != nil {
		return c.CheckOrigin
	}
	return nil
}

// New returns an [http.Handler] that upgrades HTTP connections to WebSocket
// and serves MCP sessions over the WebSocket transport.
//
// Identity (tenantID + clientID) is resolved once during the HTTP upgrade
// request via cfg.IdentityResolver. Each WebSocket connection creates a new
// MCP session.
//
// New validates that cfg.IdentityResolver is non-nil and returns an error
// if it is not.
func New(gw pkg.Invoker, cfg mcp.IngressConfig, wsCfg *mcp.WSConfig) (http.Handler, error) {
	if err := pkg.ResolveAuthDefaults(&cfg); err != nil {
		return nil, err
	}

	if cfg.IdentityResolver == nil {
		return nil, errors.New("ingress/ws: IdentityResolver must not be nil")
	}

	getServer := pkg.BuildGetServer(gw, cfg.IdentityResolver)

	upgrader := &websocket.Upgrader{
		ReadBufferSize:  readBufferSize(wsCfg),
		WriteBufferSize: writeBufferSize(wsCfg),
		CheckOrigin:     checkOrigin(wsCfg),
	}

	var handler http.Handler = &wsHandler{
		getServer:      getServer,
		upgrader:       upgrader,
		pingInterval:   pingInterval(wsCfg),
		maxMessageSize: maxMessageSize(wsCfg),
	}

	if cfg.EnterpriseAuth != nil {
		handler = auth.RequireBearerToken(cfg.EnterpriseAuth.TokenVerifier, &auth.RequireBearerTokenOptions{
			ResourceMetadataURL: pkg.ResourceMetadataURL(cfg.EnterpriseAuth.ResourceMetadata),
			Scopes:              cfg.EnterpriseAuth.RequiredScopes,
		})(handler)
	}

	return handler, nil
}

func (h *wsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	srv := h.getServer(r)
	if srv == nil {
		http.Error(w, "no server available", http.StatusBadRequest)
		return
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade writes the error response itself.
		return
	}

	if h.maxMessageSize > 0 {
		conn.SetReadLimit(h.maxMessageSize)
	}

	sessionID := newSessionID()
	wc := newWSConn(conn, sessionID)

	ss, err := srv.Connect(r.Context(), &wsTransport{conn: wc}, nil)
	if err != nil {
		_ = wc.Close()
		return
	}
	defer ss.Close()

	// Start ping ticker to keep the connection alive.
	if h.pingInterval > 0 {
		go h.pingLoop(conn, wc.done)
	}

	// Block until the connection closes.
	select {
	case <-r.Context().Done():
	case <-wc.done:
	}
}

// pingLoop sends periodic ping frames until the done channel is closed.
func (h *wsHandler) pingLoop(conn *websocket.Conn, done <-chan struct{}) {
	ticker := time.NewTicker(h.pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			_ = conn.WriteControl(
				websocket.PingMessage,
				nil,
				time.Now().Add(5*time.Second),
			)
		}
	}
}

func newSessionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", b)
}
