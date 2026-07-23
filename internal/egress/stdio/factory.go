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

package stdio

import (
	"context"
	"time"

	"github.com/tochemey/goakt-mcp/mcp"
)

// StdioExecutorFactory creates ToolExecutor instances for stdio tools.
type StdioExecutorFactory struct {
	startupTimeout time.Duration
}

// NewStdioExecutorFactory creates a factory with the given startup timeout.
// When zero, mcp.DefaultStartupTimeout is used.
func NewStdioExecutorFactory(startupTimeout time.Duration) *StdioExecutorFactory {
	if startupTimeout <= 0 {
		startupTimeout = mcp.DefaultStartupTimeout
	}
	return &StdioExecutorFactory{startupTimeout: startupTimeout}
}

// Create returns a StdioExecutor for the tool when it uses stdio transport,
// or nil when the tool uses a different transport. Resolved credentials are
// merged into the child process environment.
func (f *StdioExecutorFactory) Create(ctx context.Context, tool mcp.Tool, creds map[string]string) (mcp.ToolExecutor, error) {
	if !tool.IsStdio() || tool.Stdio == nil {
		return nil, nil
	}
	return NewStdioExecutor(tool.Stdio, f.startupTimeout, creds)
}
