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

package grpc

import (
	"context"
	"time"

	"github.com/tochemey/mcgate/mcp"
)

// GRPCExecutorFactory creates ToolExecutor instances for gRPC tools.
type GRPCExecutorFactory struct {
	startupTimeout time.Duration
}

// NewGRPCExecutorFactory creates a factory with the given startup timeout.
// When startupTimeout is zero, [mcp.DefaultStartupTimeout] is used.
func NewGRPCExecutorFactory(startupTimeout time.Duration) *GRPCExecutorFactory {
	if startupTimeout <= 0 {
		startupTimeout = mcp.DefaultStartupTimeout
	}
	return &GRPCExecutorFactory{startupTimeout: startupTimeout}
}

// Create returns a GRPCExecutor for the tool when it uses gRPC transport,
// or nil when the tool uses a different transport. Resolved credentials are
// attached as outgoing gRPC metadata on every call.
func (x *GRPCExecutorFactory) Create(ctx context.Context, tool mcp.Tool, creds map[string]string) (mcp.ToolExecutor, error) {
	if !tool.IsGRPC() || tool.GRPC == nil {
		return nil, nil
	}
	return NewGRPCExecutor(tool.GRPC, x.startupTimeout, creds)
}
