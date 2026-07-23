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

package egress

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/tochemey/portcullis/mcp"

	egressgrpc "github.com/tochemey/portcullis/internal/egress/grpc"
	egresshttp "github.com/tochemey/portcullis/internal/egress/http"
	"github.com/tochemey/portcullis/internal/egress/stdio"
)

// CompositeResourceFetcher fetches resource metadata from backend MCP servers
// by delegating to the appropriate transport-specific fetcher based on the
// tool's transport type.
type CompositeResourceFetcher struct {
	startupTimeout time.Duration
	httpClient     *http.Client
}

// NewCompositeResourceFetcher creates a resource fetcher with the given timeouts.
func NewCompositeResourceFetcher(startupTimeout time.Duration, httpClient *http.Client) *CompositeResourceFetcher {
	if startupTimeout <= 0 {
		startupTimeout = mcp.DefaultStartupTimeout
	}
	return &CompositeResourceFetcher{
		startupTimeout: startupTimeout,
		httpClient:     httpClient,
	}
}

// FetchResources connects to the backend MCP server for the given tool, calls
// resources/list and resources/templates/list, and returns the discovered
// resource metadata. The connection is closed before returning.
func (f *CompositeResourceFetcher) FetchResources(ctx context.Context, tool mcp.Tool) ([]mcp.ResourceSchema, []mcp.ResourceTemplateSchema, error) {
	switch tool.Transport {
	case mcp.TransportStdio:
		return stdio.FetchResources(ctx, tool.Stdio, f.startupTimeout)
	case mcp.TransportHTTP:
		return egresshttp.FetchResources(ctx, tool.HTTP, f.httpClient, f.startupTimeout)
	case mcp.TransportGRPC:
		return egressgrpc.FetchResources(ctx, tool.GRPC, f.startupTimeout)
	default:
		return nil, nil, fmt.Errorf("unsupported transport type %q for resource fetch", tool.Transport)
	}
}
