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

	"github.com/tochemey/portcullis/mcp"
)

func TestHTTPExecutorFactory_NonHTTPTool(t *testing.T) {
	factory := NewHTTPExecutorFactory(nil, time.Second)
	tool := mcp.Tool{
		ID:        "stdio-tool",
		Transport: mcp.TransportStdio,
		Stdio:     &mcp.StdioTransportConfig{Command: "echo"},
	}
	exec, err := factory.Create(context.Background(), tool, nil)
	require.NoError(t, err)
	assert.Nil(t, exec)
}

func TestHTTPExecutorFactory_NilHTTPConfig(t *testing.T) {
	factory := NewHTTPExecutorFactory(nil, time.Second)
	tool := mcp.Tool{
		ID:        "http-tool",
		Transport: mcp.TransportHTTP,
		HTTP:      nil,
	}
	exec, err := factory.Create(context.Background(), tool, nil)
	require.NoError(t, err)
	assert.Nil(t, exec)
}

func TestHTTPExecutorFactory_DefaultTimeout(t *testing.T) {
	factory := NewHTTPExecutorFactory(nil, 0)
	assert.NotNil(t, factory)
}

func TestHTTPExecutorFactory_Create_Success(t *testing.T) {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test", Version: "v0.1.0"}, nil)
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "echo"}, func(ctx context.Context, req *sdkmcp.CallToolRequest, args map[string]any) (*sdkmcp.CallToolResult, any, error) {
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "ok"}}}, nil, nil
	})
	handler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server }, nil)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	factory := NewHTTPExecutorFactory(nil, 5*time.Second)
	tool := mcp.Tool{
		ID:        "http-tool",
		Transport: mcp.TransportHTTP,
		HTTP:      &mcp.HTTPTransportConfig{URL: srv.URL},
	}
	exec, err := factory.Create(context.Background(), tool, nil)
	require.NoError(t, err)
	require.NotNil(t, exec)
	defer exec.Close()
}
