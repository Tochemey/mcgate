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
	"os"
	"os/exec"
	"sync"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/tochemey/mcgate/mcp"

	"github.com/tochemey/mcgate/internal/egress/mcpconv"
)

// StdioExecutor executes MCP tool invocations over a stdio child process.
//
// It uses the official MCP Go SDK CommandTransport to launch the process and
// communicate via newline-delimited JSON over stdin/stdout. The executor owns
// the process lifecycle; Close terminates the child.
type StdioExecutor struct {
	client   *sdkmcp.Client
	sess     *sdkmcp.ClientSession
	closed   sync.Once
	closeErr error
}

// Compile-time check that StdioExecutor satisfies the ResourceExecutor interface.
var _ mcp.ResourceExecutor = (*StdioExecutor)(nil)

// Execute runs the MCP tools/call invocation and returns the result.
func (e *StdioExecutor) Execute(ctx context.Context, inv *mcp.Invocation) (*mcp.ExecutionResult, error) {
	if e.sess == nil {
		return &mcp.ExecutionResult{
			Status:      mcp.ExecutionStatusFailure,
			Err:         mcp.NewRuntimeError(mcp.ErrCodeTransportFailure, "session not connected"),
			Correlation: inv.Correlation,
		}, nil
	}

	name, args := mcpconv.ParamsFromInvocation(inv)
	params := &sdkmcp.CallToolParams{Name: name, Arguments: args}

	res, err := e.sess.CallTool(ctx, params)
	if err != nil {
		if ctx.Err() != nil {
			return &mcp.ExecutionResult{
				Status:      mcp.ExecutionStatusTimeout,
				Err:         mcp.WrapRuntimeError(mcp.ErrCodeInvocationTimeout, "invocation timed out", err),
				Correlation: inv.Correlation,
			}, nil
		}
		return &mcp.ExecutionResult{
			Status:      mcp.ExecutionStatusFailure,
			Err:         mcp.WrapRuntimeError(mcp.ErrCodeTransportFailure, "call failed", err),
			Correlation: inv.Correlation,
		}, nil
	}

	output := mcpconv.CallResultToOutput(res)
	status := mcp.ExecutionStatusSuccess
	var rErr *mcp.RuntimeError
	if res.IsError {
		status = mcp.ExecutionStatusFailure
		rErr = mcp.NewRuntimeError(mcp.ErrCodeInternal, mcpconv.ContentErrorText(res))
	}

	return &mcp.ExecutionResult{
		Status:      status,
		Output:      output,
		Err:         rErr,
		Correlation: inv.Correlation,
	}, nil
}

// ReadResource reads an MCP resource from the backend server via resources/read.
// The URI is extracted from the invocation parameters.
func (e *StdioExecutor) ReadResource(ctx context.Context, inv *mcp.Invocation) (*mcp.ExecutionResult, error) {
	if e.sess == nil {
		return &mcp.ExecutionResult{
			Status:      mcp.ExecutionStatusFailure,
			Err:         mcp.NewRuntimeError(mcp.ErrCodeTransportFailure, "session not connected"),
			Correlation: inv.Correlation,
		}, nil
	}

	uri := mcpconv.ResourceParamsFromInvocation(inv)
	if uri == "" {
		return &mcp.ExecutionResult{
			Status:      mcp.ExecutionStatusFailure,
			Err:         mcp.NewRuntimeError(mcp.ErrCodeInvalidRequest, "resource URI is required"),
			Correlation: inv.Correlation,
		}, nil
	}

	params := &sdkmcp.ReadResourceParams{URI: uri}
	res, err := e.sess.ReadResource(ctx, params)
	if err != nil {
		if ctx.Err() != nil {
			return &mcp.ExecutionResult{
				Status:      mcp.ExecutionStatusTimeout,
				Err:         mcp.WrapRuntimeError(mcp.ErrCodeInvocationTimeout, "resource read timed out", err),
				Correlation: inv.Correlation,
			}, nil
		}
		return &mcp.ExecutionResult{
			Status:      mcp.ExecutionStatusFailure,
			Err:         mcp.WrapRuntimeError(mcp.ErrCodeTransportFailure, "resource read failed", err),
			Correlation: inv.Correlation,
		}, nil
	}

	output := mcpconv.ReadResourceResultToOutput(res)
	return &mcp.ExecutionResult{
		Status:      mcp.ExecutionStatusSuccess,
		Output:      output,
		Correlation: inv.Correlation,
	}, nil
}

// Close terminates the child process and releases resources.
func (e *StdioExecutor) Close() error {
	e.closed.Do(func() {
		if e.sess != nil {
			e.closeErr = e.sess.Close()
			e.sess = nil
		}
		e.client = nil
	})
	return e.closeErr
}

// NewStdioExecutor creates an executor by launching the configured command and
// connecting the MCP client. Returns an error if the process fails to start or
// connect within startupTimeout.
//
// creds holds credentials resolved by the credential broker. Each entry is
// merged into the child process environment; credentials win over both the
// inherited environment and cfg.Env entries with the same key.
func NewStdioExecutor(cfg *mcp.StdioTransportConfig, startupTimeout time.Duration, creds map[string]string) (*StdioExecutor, error) {
	if cfg == nil || cfg.Command == "" {
		return nil, mcp.NewRuntimeError(mcp.ErrCodeInvalidRequest, "stdio config required")
	}

	cmd := exec.Command(cfg.Command, cfg.Args...) //nolint:gosec // command and args are from admin-controlled tool configuration, not user input
	if cfg.WorkingDirectory != "" {
		cmd.Dir = cfg.WorkingDirectory
	}
	cmd.Env = envSlice(mergeEnv(cfg.Env, creds), cfg.IsolateEnv)

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "mcgate", Version: mcp.Version()}, nil)
	transport := &sdkmcp.CommandTransport{Command: cmd}

	ctx := context.Background()
	if startupTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, startupTimeout)
		defer cancel()
	}

	sess, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, mcp.WrapRuntimeError(mcp.ErrCodeTransportFailure, "stdio connect failed", err)
	}

	return &StdioExecutor{client: client, sess: sess}, nil
}

// mergeEnv overlays creds on top of the configured environment entries.
// Returns cfgEnv unchanged when there are no credentials.
func mergeEnv(cfgEnv, creds map[string]string) map[string]string {
	if len(creds) == 0 {
		return cfgEnv
	}
	merged := make(map[string]string, len(cfgEnv)+len(creds))
	for k, v := range cfgEnv {
		merged[k] = v
	}
	for k, v := range creds {
		merged[k] = v
	}
	return merged
}

// minimalEnvKeys are the parent variables retained when IsolateEnv is set:
// enough for launchers (PATH), temp files, and locale-sensitive tools, while
// withholding everything else the gateway process environment may hold
// (other tools' API keys, cloud credentials).
var minimalEnvKeys = []string{"PATH", "HOME", "TMPDIR", "USER", "LOGNAME", "SHELL", "LANG", "TERM"}

// envSlice builds the child process environment: the inherited parent
// environment (or the minimal base when isolate is set) overlaid with extra.
func envSlice(extra map[string]string, isolate bool) []string {
	base := os.Environ()
	if isolate {
		base = base[:0:0]
		for _, key := range minimalEnvKeys {
			if v, ok := os.LookupEnv(key); ok {
				base = append(base, key+"="+v)
			}
		}
	}
	if len(extra) == 0 {
		return base
	}
	out := make([]string, 0, len(base)+len(extra))
	used := make(map[string]struct{}, len(extra))
	for _, e := range base {
		for i := 0; i < len(e); i++ {
			if e[i] == '=' {
				key := e[:i]
				if v, ok := extra[key]; ok {
					out = append(out, key+"="+v)
					used[key] = struct{}{}
				} else {
					out = append(out, e)
				}
				break
			}
		}
	}
	for k, v := range extra {
		if _, ok := used[k]; !ok {
			out = append(out, k+"="+v)
		}
	}
	return out
}
