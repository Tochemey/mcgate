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
	"net/http"
	"time"

	"github.com/tochemey/mcgate/mcp"

	egressgrpc "github.com/tochemey/mcgate/internal/egress/grpc"
	egresshttp "github.com/tochemey/mcgate/internal/egress/http"
	"github.com/tochemey/mcgate/internal/egress/stdio"
)

// CompositeExecutorFactory creates ToolExecutor instances by delegating to
// stdio, HTTP, or gRPC factories based on tool transport. Low-allocation:
// reuses shared factory instances.
type CompositeExecutorFactory struct {
	stdio *stdio.StdioExecutorFactory
	http  *egresshttp.HTTPExecutorFactory
	grpc  *egressgrpc.GRPCExecutorFactory
}

// NewCompositeExecutorFactory creates a factory with the given timeouts.
// When zero, config defaults are used.
func NewCompositeExecutorFactory(startupTimeout time.Duration, httpClient *http.Client) *CompositeExecutorFactory {
	if startupTimeout <= 0 {
		startupTimeout = mcp.DefaultStartupTimeout
	}
	return &CompositeExecutorFactory{
		stdio: stdio.NewStdioExecutorFactory(startupTimeout),
		http:  egresshttp.NewHTTPExecutorFactory(httpClient, startupTimeout),
		grpc:  egressgrpc.NewGRPCExecutorFactory(startupTimeout),
	}
}

// Create returns an executor for the tool's transport, or nil when the tool
// has no configured transport.
func (x *CompositeExecutorFactory) Create(ctx context.Context, tool mcp.Tool, creds map[string]string) (mcp.ToolExecutor, error) {
	switch tool.Transport {
	case mcp.TransportStdio:
		return x.stdio.Create(ctx, tool, creds)
	case mcp.TransportHTTP:
		return x.http.Create(ctx, tool, creds)
	case mcp.TransportGRPC:
		return x.grpc.Create(ctx, tool, creds)
	default:
		return nil, nil
	}
}
