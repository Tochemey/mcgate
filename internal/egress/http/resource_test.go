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
	"errors"
	gohttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tochemey/portcullis/mcp"
)

func TestFetchResources_Validation(t *testing.T) {
	t.Run("nil config returns error", func(t *testing.T) {
		resources, templates, err := FetchResources(context.Background(), nil, nil, time.Second)
		assert.Nil(t, resources)
		assert.Nil(t, templates)
		require.Error(t, err)
		var rErr *mcp.RuntimeError
		require.ErrorAs(t, err, &rErr)
		assert.Equal(t, mcp.ErrCodeInvalidRequest, rErr.Code)
	})

	t.Run("empty URL returns error", func(t *testing.T) {
		cfg := &mcp.HTTPTransportConfig{URL: ""}
		resources, templates, err := FetchResources(context.Background(), cfg, nil, time.Second)
		assert.Nil(t, resources)
		assert.Nil(t, templates)
		require.Error(t, err)
		var rErr *mcp.RuntimeError
		require.ErrorAs(t, err, &rErr)
		assert.Equal(t, mcp.ErrCodeInvalidRequest, rErr.Code)
	})

	t.Run("unreachable endpoint returns transport failure", func(t *testing.T) {
		cfg := &mcp.HTTPTransportConfig{URL: "http://127.0.0.1:1/unreachable"}
		resources, templates, err := FetchResources(context.Background(), cfg, nil, 500*time.Millisecond)
		assert.Nil(t, resources)
		assert.Nil(t, templates)
		require.Error(t, err)
		var rErr *mcp.RuntimeError
		require.ErrorAs(t, err, &rErr)
		assert.Equal(t, mcp.ErrCodeTransportFailure, rErr.Code)
	})
}

func TestFetchResources_Success(t *testing.T) {
	url, cleanup := startMCPHTTPServerWithResources(t)
	defer cleanup()

	cfg := &mcp.HTTPTransportConfig{URL: url}
	resources, templates, err := FetchResources(context.Background(), cfg, nil, 5*time.Second)
	require.NoError(t, err)

	require.Len(t, resources, 1)
	assert.Equal(t, "file:///readme.md", resources[0].URI)
	assert.Equal(t, "readme", resources[0].Name)
	assert.Equal(t, "The readme", resources[0].Description)
	assert.Equal(t, "text/markdown", resources[0].MIMEType)

	require.Len(t, templates, 1)
	assert.Equal(t, "file:///{path}", templates[0].URITemplate)
	assert.Equal(t, "file", templates[0].Name)
	assert.Equal(t, "A file", templates[0].Description)
	assert.Equal(t, "application/octet-stream", templates[0].MIMEType)
}

func TestFetchResources_NoResourceSupport(t *testing.T) {
	// Server without resources registered
	url, cleanup := startMCPHTTPServer(t)
	defer cleanup()

	cfg := &mcp.HTTPTransportConfig{URL: url}
	resources, templates, err := FetchResources(context.Background(), cfg, nil, 5*time.Second)
	require.NoError(t, err)
	assert.Empty(t, resources)
	assert.Empty(t, templates)
}

func TestFetchResources_Paginated(t *testing.T) {
	url, cleanup := startPaginatedMCPHTTPServer(t)
	defer cleanup()

	cfg := &mcp.HTTPTransportConfig{URL: url}
	resources, templates, err := FetchResources(context.Background(), cfg, nil, 5*time.Second)
	require.NoError(t, err)

	require.Len(t, resources, 2, "all resource pages must be collected")
	uris := []string{resources[0].URI, resources[1].URI}
	assert.ElementsMatch(t, []string{"file:///one.txt", "file:///two.txt"}, uris)

	require.Len(t, templates, 2, "all template pages must be collected")
	tmpls := []string{templates[0].URITemplate, templates[1].URITemplate}
	assert.ElementsMatch(t, []string{"file:///{a}", "dir:///{b}"}, tmpls)
}

func TestFetchResources_ListErrorPropagated(t *testing.T) {
	// A backend that supports resources but fails resources/list must surface
	// a transport failure, not silently return an empty result. Only JSON-RPC
	// "method not found" means "resources unsupported".
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test-broken", Version: "v0.1.0"}, nil)
	server.AddResource(
		&sdkmcp.Resource{URI: "file:///a.txt", Name: "a"},
		func(ctx context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
			return &sdkmcp.ReadResourceResult{
				Contents: []*sdkmcp.ResourceContents{{URI: "file:///a.txt", Text: "ok"}},
			}, nil
		},
	)
	server.AddReceivingMiddleware(func(next sdkmcp.MethodHandler) sdkmcp.MethodHandler {
		return func(ctx context.Context, method string, req sdkmcp.Request) (sdkmcp.Result, error) {
			if method == "resources/list" {
				return nil, errors.New("backend exploded")
			}
			return next(ctx, method, req)
		}
	})
	handler := sdkmcp.NewStreamableHTTPHandler(func(*gohttp.Request) *sdkmcp.Server { return server }, nil)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	cfg := &mcp.HTTPTransportConfig{URL: srv.URL}
	resources, templates, err := FetchResources(context.Background(), cfg, nil, 5*time.Second)
	assert.Nil(t, resources)
	assert.Nil(t, templates)
	require.Error(t, err)
	var rErr *mcp.RuntimeError
	require.ErrorAs(t, err, &rErr)
	assert.Equal(t, mcp.ErrCodeTransportFailure, rErr.Code)
	assert.Contains(t, rErr.Message, "list resources failed")
}
