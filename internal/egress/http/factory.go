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
	"time"

	"github.com/tochemey/mcgate/mcp"
)

// HTTPExecutorFactory creates ToolExecutor instances for HTTP tools.
type HTTPExecutorFactory struct {
	httpClient     *http.Client
	startupTimeout time.Duration
}

// NewHTTPExecutorFactory creates a factory. When startupTimeout is zero,
// mcp.DefaultStartupTimeout is used.
func NewHTTPExecutorFactory(httpClient *http.Client, startupTimeout time.Duration) *HTTPExecutorFactory {
	if startupTimeout <= 0 {
		startupTimeout = mcp.DefaultStartupTimeout
	}
	return &HTTPExecutorFactory{httpClient: httpClient, startupTimeout: startupTimeout}
}

// Create returns an HTTPExecutor for the tool when it uses HTTP transport,
// or nil when the tool uses a different transport. Resolved credentials are
// sent as HTTP request headers on every outbound request.
func (f *HTTPExecutorFactory) Create(ctx context.Context, tool mcp.Tool, creds map[string]string) (mcp.ToolExecutor, error) {
	if !tool.IsHTTP() || tool.HTTP == nil {
		return nil, nil
	}
	return NewHTTPExecutor(tool.HTTP, f.httpClient, f.startupTimeout, creds)
}
