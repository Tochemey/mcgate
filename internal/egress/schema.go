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

	"github.com/tochemey/mcgate/mcp"

	egressgrpc "github.com/tochemey/mcgate/internal/egress/grpc"
	egresshttp "github.com/tochemey/mcgate/internal/egress/http"
	"github.com/tochemey/mcgate/internal/egress/stdio"
)

// CompositeSchemaFetcher fetches tool schemas from backend MCP servers by
// delegating to the appropriate transport-specific fetcher based on the tool's
// transport type.
type CompositeSchemaFetcher struct {
	startupTimeout time.Duration
	httpClient     *http.Client
}

// NewCompositeSchemaFetcher creates a schema fetcher with the given timeouts.
func NewCompositeSchemaFetcher(startupTimeout time.Duration, httpClient *http.Client) *CompositeSchemaFetcher {
	if startupTimeout <= 0 {
		startupTimeout = mcp.DefaultStartupTimeout
	}
	return &CompositeSchemaFetcher{
		startupTimeout: startupTimeout,
		httpClient:     httpClient,
	}
}

// FetchSchemas connects to the backend MCP server for the given tool, calls
// tools/list, and returns the discovered schemas. The connection is closed
// before returning.
func (f *CompositeSchemaFetcher) FetchSchemas(ctx context.Context, tool mcp.Tool) ([]mcp.ToolSchema, error) {
	switch tool.Transport {
	case mcp.TransportStdio:
		return stdio.FetchSchemas(ctx, tool.Stdio, f.startupTimeout)
	case mcp.TransportHTTP:
		return egresshttp.FetchSchemas(ctx, tool.HTTP, f.httpClient, f.startupTimeout)
	case mcp.TransportGRPC:
		return egressgrpc.FetchSchemas(ctx, tool.GRPC, f.startupTimeout)
	default:
		return nil, fmt.Errorf("unsupported transport type %q for schema fetch", tool.Transport)
	}
}
