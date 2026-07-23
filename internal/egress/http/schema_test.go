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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tochemey/goakt-mcp/mcp"
)

// startPaginatedMCPHTTPServer starts an in-process MCP HTTP server whose list
// endpoints return one item per page, forcing clients to follow pagination
// cursors. It registers three tools, two resources, and two resource templates.
func startPaginatedMCPHTTPServer(t *testing.T) (url string, cleanup func()) {
	t.Helper()
	tool := func(ctx context.Context, req *sdkmcp.CallToolRequest, args map[string]any) (*sdkmcp.CallToolResult, any, error) {
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "ok"}},
		}, nil, nil
	}
	readResource := func(ctx context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
		return &sdkmcp.ReadResourceResult{
			Contents: []*sdkmcp.ResourceContents{{URI: req.Params.URI, Text: "ok"}},
		}, nil
	}

	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test-paginated", Version: "v0.1.0"}, &sdkmcp.ServerOptions{PageSize: 1})
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "alpha", Description: "alpha"}, tool)
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "bravo", Description: "bravo"}, tool)
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "charlie", Description: "charlie"}, tool)
	server.AddResource(&sdkmcp.Resource{URI: "file:///one.txt", Name: "one"}, readResource)
	server.AddResource(&sdkmcp.Resource{URI: "file:///two.txt", Name: "two"}, readResource)
	server.AddResourceTemplate(&sdkmcp.ResourceTemplate{URITemplate: "file:///{a}", Name: "a"}, readResource)
	server.AddResourceTemplate(&sdkmcp.ResourceTemplate{URITemplate: "dir:///{b}", Name: "b"}, readResource)

	handler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server }, nil)
	srv := httptest.NewServer(handler)
	return srv.URL, srv.Close
}

func TestFetchSchemas_Paginated(t *testing.T) {
	url, cleanup := startPaginatedMCPHTTPServer(t)
	defer cleanup()

	cfg := &mcp.HTTPTransportConfig{URL: url}
	schemas, err := FetchSchemas(context.Background(), cfg, nil, 5*time.Second)
	require.NoError(t, err)

	require.Len(t, schemas, 3, "all pages must be collected")
	names := make([]string, 0, len(schemas))
	for _, s := range schemas {
		names = append(names, s.Name)
	}
	assert.ElementsMatch(t, []string{"alpha", "bravo", "charlie"}, names)
}

func TestFetchSchemas_DoesNotMutateFallbackClient(t *testing.T) {
	url, cleanup := startMCPHTTPServer(t)
	defer cleanup()

	shared := &http.Client{}
	cfg := &mcp.HTTPTransportConfig{URL: url}
	_, err := FetchSchemas(context.Background(), cfg, shared, 5*time.Second)
	require.NoError(t, err)

	assert.Nil(t, shared.Transport, "shared client transport must not be mutated")
}
