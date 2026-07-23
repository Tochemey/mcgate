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
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/stretchr/testify/assert"
	"golang.org/x/oauth2"

	"github.com/tochemey/portcullis/mcp"
)

func TestTool_IsStdio(t *testing.T) {
	tool := mcp.Tool{Transport: mcp.TransportStdio}
	assert.True(t, tool.IsStdio())
	assert.False(t, tool.IsHTTP())
}

func TestTool_IsHTTP(t *testing.T) {
	tool := mcp.Tool{Transport: mcp.TransportHTTP}
	assert.True(t, tool.IsHTTP())
	assert.False(t, tool.IsStdio())
}

func TestTool_IsAvailable(t *testing.T) {
	assert.True(t, mcp.Tool{State: mcp.ToolStateEnabled}.IsAvailable())
	assert.True(t, mcp.Tool{State: mcp.ToolStateDegraded}.IsAvailable())
	assert.False(t, mcp.Tool{State: mcp.ToolStateDisabled}.IsAvailable())
	assert.False(t, mcp.Tool{State: mcp.ToolStateUnavailable}.IsAvailable())
}

func TestHTTPTransportConfig_OAuthHandler(t *testing.T) {
	t.Run("nil by default", func(t *testing.T) {
		cfg := mcp.HTTPTransportConfig{URL: "https://mcp.example.com"}
		assert.Nil(t, cfg.OAuthHandler)
	})

	t.Run("settable with OAuthHandler implementation", func(t *testing.T) {
		handler := &stubOAuthHandler{}
		cfg := mcp.HTTPTransportConfig{
			URL:          "https://mcp.example.com",
			OAuthHandler: handler,
		}
		assert.NotNil(t, cfg.OAuthHandler)
	})
}

// stubOAuthHandler is a minimal auth.OAuthHandler for testing field assignment.
type stubOAuthHandler struct{}

func (s *stubOAuthHandler) TokenSource(_ context.Context) (oauth2.TokenSource, error) {
	return nil, nil
}

func (s *stubOAuthHandler) Authorize(_ context.Context, _ *http.Request, _ *http.Response) error {
	return nil
}

// Compile-time check that stubOAuthHandler satisfies auth.OAuthHandler.
var _ auth.OAuthHandler = (*stubOAuthHandler)(nil)
