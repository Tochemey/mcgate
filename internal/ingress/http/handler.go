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
	"errors"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/auth"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/tochemey/goakt-mcp/internal/ingress/pkg"
	"github.com/tochemey/goakt-mcp/mcp"
)

// New returns an [http.Handler] that serves MCP over Streamable HTTP and
// routes each tool call through gw.
//
// The handler is stateless (MCP 2026-07-28): every request is self-contained
// and identity (tenantID + clientID) is resolved per request via
// cfg.IdentityResolver. On resolution failure, getServer returns nil which
// causes the SDK to reply with HTTP 400 Bad Request. Clients on the previous
// protocol revision are served through the same handler via a temporary
// per-request session.
//
// New validates that cfg.IdentityResolver is non-nil and returns an error
// if it is not. The gateway does not need to be started at the time New is
// called; tool registration happens lazily per request.
func New(gw pkg.Invoker, cfg mcp.IngressConfig) (http.Handler, error) {
	if err := pkg.ResolveAuthDefaults(&cfg); err != nil {
		return nil, err
	}

	if cfg.IdentityResolver == nil {
		return nil, errors.New("ingress: IdentityResolver must not be nil")
	}

	getServer := pkg.BuildGetServer(gw, cfg.IdentityResolver)

	opts := &sdkmcp.StreamableHTTPOptions{
		Stateless: true,
	}

	var handler http.Handler = sdkmcp.NewStreamableHTTPHandler(getServer, opts)

	if cfg.EnterpriseAuth != nil {
		handler = auth.RequireBearerToken(cfg.EnterpriseAuth.TokenVerifier, &auth.RequireBearerTokenOptions{
			ResourceMetadataURL: pkg.ResourceMetadataURL(cfg.EnterpriseAuth.ResourceMetadata),
			Scopes:              cfg.EnterpriseAuth.RequiredScopes,
		})(handler)
	}

	return handler, nil
}
