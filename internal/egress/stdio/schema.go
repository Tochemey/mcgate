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
	"os/exec"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/tochemey/goakt-mcp/internal/egress/schemaconv"
	"github.com/tochemey/goakt-mcp/mcp"
)

// FetchSchemas connects to the stdio backend, calls tools/list, and returns the
// discovered tool schemas. The connection is closed before returning.
func FetchSchemas(ctx context.Context, cfg *mcp.StdioTransportConfig, startupTimeout time.Duration) ([]mcp.ToolSchema, error) {
	if cfg == nil || cfg.Command == "" {
		return nil, mcp.NewRuntimeError(mcp.ErrCodeInvalidRequest, "stdio config required")
	}

	cmd := exec.Command(cfg.Command, cfg.Args...) //nolint:gosec // command and args are from admin-controlled tool configuration, not user input
	if cfg.WorkingDirectory != "" {
		cmd.Dir = cfg.WorkingDirectory
	}
	cmd.Env = envSlice(cfg.Env, cfg.IsolateEnv)

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "goakt-mcp-schema", Version: mcp.Version()}, nil)
	transport := &sdkmcp.CommandTransport{Command: cmd}

	// Use a single deadline for the entire operation (connect + list tools)
	// so schema discovery cannot hang indefinitely.
	fetchCtx := ctx
	if startupTimeout > 0 {
		var cancel context.CancelFunc
		fetchCtx, cancel = context.WithTimeout(fetchCtx, startupTimeout)
		defer cancel()
	}

	sess, err := client.Connect(fetchCtx, transport, nil)
	if err != nil {
		return nil, mcp.WrapRuntimeError(mcp.ErrCodeTransportFailure, "stdio schema connect failed", err)
	}
	defer sess.Close()

	// Collect all tools, following pagination cursors so tools beyond the
	// first page are not dropped.
	var tools []*sdkmcp.Tool
	for tool, err := range sess.Tools(fetchCtx, nil) {
		if err != nil {
			return nil, mcp.WrapRuntimeError(mcp.ErrCodeTransportFailure, "stdio list tools failed", err)
		}
		tools = append(tools, tool)
	}

	return schemaconv.SDKToolsToSchemas(tools), nil
}
