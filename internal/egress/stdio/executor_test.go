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
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tochemey/goakt-mcp/mcp"
)

// newTestStdioExecutor creates a StdioExecutor backed by in-memory transports
// connected to the given SDK server. This avoids spawning a real child process.
func newTestStdioExecutor(t *testing.T, server *sdkmcp.Server) *StdioExecutor {
	t.Helper()
	serverT, clientT := sdkmcp.NewInMemoryTransports()
	_, err := server.Connect(context.Background(), serverT, nil)
	require.NoError(t, err)

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-stdio", Version: "v0.1.0"}, nil)
	sess, err := client.Connect(context.Background(), clientT, nil)
	require.NoError(t, err)

	return &StdioExecutor{client: client, sess: sess}
}

func TestNewStdioExecutor_Validation(t *testing.T) {
	t.Run("nil config returns error", func(t *testing.T) {
		exec, err := NewStdioExecutor(nil, time.Second, nil)
		assert.Nil(t, exec)
		require.Error(t, err)
		var rErr *mcp.RuntimeError
		require.ErrorAs(t, err, &rErr)
		assert.Equal(t, mcp.ErrCodeInvalidRequest, rErr.Code)
	})

	t.Run("empty command returns error", func(t *testing.T) {
		cfg := &mcp.StdioTransportConfig{Command: ""}
		exec, err := NewStdioExecutor(cfg, time.Second, nil)
		assert.Nil(t, exec)
		require.Error(t, err)
		var rErr *mcp.RuntimeError
		require.ErrorAs(t, err, &rErr)
		assert.Equal(t, mcp.ErrCodeInvalidRequest, rErr.Code)
	})

	t.Run("non-existent command returns transport failure", func(t *testing.T) {
		cfg := &mcp.StdioTransportConfig{Command: "/nonexistent/binary/xyz"}
		exec, err := NewStdioExecutor(cfg, 500*time.Millisecond, nil)
		assert.Nil(t, exec)
		require.Error(t, err)
		var rErr *mcp.RuntimeError
		require.ErrorAs(t, err, &rErr)
		assert.Equal(t, mcp.ErrCodeTransportFailure, rErr.Code)
	})
}

func TestStdioExecutor_Execute_NilSession(t *testing.T) {
	e := &StdioExecutor{}
	inv := &mcp.Invocation{
		ToolID: "test",
		Correlation: mcp.CorrelationMeta{
			RequestID: "req-1",
		},
	}
	result, err := e.Execute(context.Background(), inv)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, mcp.ExecutionStatusFailure, result.Status)
	assert.Equal(t, mcp.ErrCodeTransportFailure, result.Err.Code)
}

func TestStdioExecutor_Close_Idempotent(t *testing.T) {
	e := &StdioExecutor{}
	require.NoError(t, e.Close())
	require.NoError(t, e.Close())
}

func TestStdioExecutor_ReadResource_NilSession(t *testing.T) {
	e := &StdioExecutor{}
	inv := &mcp.Invocation{
		ToolID: "test",
		Method: "resources/read",
		Params: map[string]any{"uri": "file:///a.txt"},
		Correlation: mcp.CorrelationMeta{
			RequestID: "req-res-1",
		},
	}
	result, err := e.ReadResource(context.Background(), inv)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, mcp.ExecutionStatusFailure, result.Status)
	assert.Equal(t, mcp.ErrCodeTransportFailure, result.Err.Code)
}

func TestStdioExecutor_ReadResource_EmptyURI(t *testing.T) {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test", Version: "v0.1.0"}, nil)
	exec := newTestStdioExecutor(t, server)
	defer exec.Close()

	inv := &mcp.Invocation{
		ToolID: "test",
		Method: "resources/read",
		Params: map[string]any{},
		Correlation: mcp.CorrelationMeta{
			RequestID: "req-empty-uri",
		},
	}
	result, err := exec.ReadResource(context.Background(), inv)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, mcp.ExecutionStatusFailure, result.Status)
	assert.Equal(t, mcp.ErrCodeInvalidRequest, result.Err.Code)
	assert.Contains(t, result.Err.Message, "resource URI is required")
}

func TestStdioExecutor_Execute_Success(t *testing.T) {
	echoTool := func(ctx context.Context, req *sdkmcp.CallToolRequest, args map[string]any) (*sdkmcp.CallToolResult, any, error) {
		msg := "ok"
		if v, ok := args["message"].(string); ok && v != "" {
			msg = v
		}
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: msg}},
		}, nil, nil
	}
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test", Version: "v0.1.0"}, nil)
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "echo", Description: "echo"}, echoTool)

	exec := newTestStdioExecutor(t, server)
	defer exec.Close()

	inv := &mcp.Invocation{
		ToolID: "echo",
		Method: "tools/call",
		Params: map[string]any{
			"name":      "echo",
			"arguments": map[string]any{"message": "hello"},
		},
		Correlation: mcp.CorrelationMeta{RequestID: "req-1"},
	}
	result, err := exec.Execute(context.Background(), inv)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, mcp.ExecutionStatusSuccess, result.Status)
	assert.Nil(t, result.Err)
	require.NotNil(t, result.Output)
	content, ok := result.Output["content"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, content, 1)
	assert.Equal(t, "hello", content[0]["text"])
}

func TestStdioExecutor_Execute_IsError(t *testing.T) {
	errorTool := func(ctx context.Context, req *sdkmcp.CallToolRequest, args map[string]any) (*sdkmcp.CallToolResult, any, error) {
		return &sdkmcp.CallToolResult{
			IsError: true,
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "tool error"}},
		}, nil, nil
	}
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test", Version: "v0.1.0"}, nil)
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "error_tool", Description: "returns error"}, errorTool)

	exec := newTestStdioExecutor(t, server)
	defer exec.Close()

	inv := &mcp.Invocation{
		ToolID:      "error_tool",
		Method:      "tools/call",
		Params:      map[string]any{"name": "error_tool", "arguments": map[string]any{}},
		Correlation: mcp.CorrelationMeta{RequestID: "req-err"},
	}
	result, err := exec.Execute(context.Background(), inv)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, mcp.ExecutionStatusFailure, result.Status)
	require.NotNil(t, result.Err)
	assert.Equal(t, mcp.ErrCodeInternal, result.Err.Code)
	assert.Contains(t, result.Err.Message, "tool error")
}

func TestStdioExecutor_Execute_Timeout(t *testing.T) {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test", Version: "v0.1.0"}, nil)
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "echo", Description: "echo"},
		func(ctx context.Context, req *sdkmcp.CallToolRequest, args map[string]any) (*sdkmcp.CallToolResult, any, error) {
			return &sdkmcp.CallToolResult{
				Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "ok"}},
			}, nil, nil
		})

	exec := newTestStdioExecutor(t, server)
	defer exec.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately to simulate timeout

	inv := &mcp.Invocation{
		ToolID:      "echo",
		Method:      "tools/call",
		Params:      map[string]any{"name": "echo", "arguments": map[string]any{}},
		Correlation: mcp.CorrelationMeta{RequestID: "req-timeout"},
	}
	result, err := exec.Execute(ctx, inv)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, mcp.ExecutionStatusTimeout, result.Status)
	require.NotNil(t, result.Err)
	assert.Equal(t, mcp.ErrCodeInvocationTimeout, result.Err.Code)
}

func TestStdioExecutor_ReadResource_Success(t *testing.T) {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test", Version: "v0.1.0"}, nil)
	server.AddResource(
		&sdkmcp.Resource{URI: "file:///readme.md", Name: "readme", MIMEType: "text/markdown"},
		func(ctx context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
			return &sdkmcp.ReadResourceResult{
				Contents: []*sdkmcp.ResourceContents{
					{URI: "file:///readme.md", MIMEType: "text/markdown", Text: "# Hello"},
				},
			}, nil
		},
	)

	exec := newTestStdioExecutor(t, server)
	defer exec.Close()

	inv := &mcp.Invocation{
		ToolID: "test",
		Method: "resources/read",
		Params: map[string]any{"uri": "file:///readme.md"},
		Correlation: mcp.CorrelationMeta{
			RequestID: "req-res-success",
		},
	}
	result, err := exec.ReadResource(context.Background(), inv)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, mcp.ExecutionStatusSuccess, result.Status)
	assert.Nil(t, result.Err)
	require.NotNil(t, result.Output)
	contents, ok := result.Output["contents"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, contents, 1)
	assert.Equal(t, "file:///readme.md", contents[0]["uri"])
	assert.Equal(t, "# Hello", contents[0]["text"])
}

func TestStdioExecutor_ReadResource_Timeout(t *testing.T) {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test", Version: "v0.1.0"}, nil)
	server.AddResource(
		&sdkmcp.Resource{URI: "file:///a.txt", Name: "a"},
		func(ctx context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
			return &sdkmcp.ReadResourceResult{
				Contents: []*sdkmcp.ResourceContents{{URI: "file:///a.txt", Text: "ok"}},
			}, nil
		},
	)

	exec := newTestStdioExecutor(t, server)
	defer exec.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	inv := &mcp.Invocation{
		ToolID: "test",
		Method: "resources/read",
		Params: map[string]any{"uri": "file:///a.txt"},
		Correlation: mcp.CorrelationMeta{
			RequestID: "req-res-timeout",
		},
	}
	result, err := exec.ReadResource(ctx, inv)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, mcp.ExecutionStatusTimeout, result.Status)
	require.NotNil(t, result.Err)
	assert.Equal(t, mcp.ErrCodeInvocationTimeout, result.Err.Code)
}

func TestStdioExecutor_Close_WithSession(t *testing.T) {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test", Version: "v0.1.0"}, nil)
	exec := newTestStdioExecutor(t, server)

	// Execute a close to terminate the session
	require.NoError(t, exec.Close())
	// Second close is idempotent
	require.NoError(t, exec.Close())
}

func TestEnvSlice(t *testing.T) {
	t.Run("nil extra returns base", func(t *testing.T) {
		result := envSlice(nil, false)
		assert.NotEmpty(t, result)
	})

	t.Run("empty extra returns base", func(t *testing.T) {
		result := envSlice(map[string]string{}, false)
		assert.NotEmpty(t, result)
	})

	t.Run("overrides existing variable", func(t *testing.T) {
		extra := map[string]string{"PATH": "/custom/path"}
		result := envSlice(extra, false)
		found := false
		for _, e := range result {
			if e == "PATH=/custom/path" {
				found = true
				break
			}
		}
		assert.True(t, found, "PATH should be overridden")
	})

	t.Run("adds new variable", func(t *testing.T) {
		extra := map[string]string{"MY_CUSTOM_VAR_XYZ_TEST": "value123"}
		result := envSlice(extra, false)
		found := false
		for _, e := range result {
			if e == "MY_CUSTOM_VAR_XYZ_TEST=value123" {
				found = true
				break
			}
		}
		assert.True(t, found, "new variable should be added")
	})
}

func TestNewStdioExecutor_CredentialsInjectedIntoEnv(t *testing.T) {
	bin := buildEnvServer(t)

	// SHARED_KEY exists in both cfg.Env and creds: the credential must win.
	cfg := &mcp.StdioTransportConfig{
		Command: bin,
		Env:     map[string]string{"FROM_CFG": "cfg-value", "SHARED_KEY": "cfg-value"},
	}
	creds := map[string]string{"CRED_TOKEN": "s3cret", "SHARED_KEY": "cred-value"}

	exec, err := NewStdioExecutor(cfg, 15*time.Second, creds)
	require.NoError(t, err)
	defer exec.Close()

	getenv := func(name string) string {
		t.Helper()
		inv := &mcp.Invocation{
			ToolID:      "env",
			Method:      "tools/call",
			Params:      map[string]any{"name": "env", "arguments": map[string]any{"name": name}},
			Correlation: mcp.CorrelationMeta{RequestID: mcp.RequestID("req-env-" + name)},
		}
		result, err := exec.Execute(context.Background(), inv)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, mcp.ExecutionStatusSuccess, result.Status)
		content, ok := result.Output["content"].([]map[string]any)
		require.True(t, ok)
		require.Len(t, content, 1)
		text, _ := content[0]["text"].(string)
		return text
	}

	assert.Equal(t, "s3cret", getenv("CRED_TOKEN"), "credential must reach the child environment")
	assert.Equal(t, "cred-value", getenv("SHARED_KEY"), "credential must override cfg.Env")
	assert.Equal(t, "cfg-value", getenv("FROM_CFG"), "cfg.Env entries must still be applied")
}

func TestMergeEnv(t *testing.T) {
	t.Run("nil creds returns cfg env unchanged", func(t *testing.T) {
		cfgEnv := map[string]string{"A": "1"}
		assert.Equal(t, cfgEnv, mergeEnv(cfgEnv, nil))
	})

	t.Run("nil cfg env returns creds", func(t *testing.T) {
		creds := map[string]string{"TOKEN": "x"}
		assert.Equal(t, creds, mergeEnv(nil, creds))
	})

	t.Run("creds override cfg env on conflict", func(t *testing.T) {
		cfgEnv := map[string]string{"A": "cfg", "B": "cfg"}
		creds := map[string]string{"B": "cred", "C": "cred"}
		merged := mergeEnv(cfgEnv, creds)
		assert.Equal(t, map[string]string{"A": "cfg", "B": "cred", "C": "cred"}, merged)
	})

	t.Run("does not mutate inputs", func(t *testing.T) {
		cfgEnv := map[string]string{"A": "cfg"}
		creds := map[string]string{"A": "cred"}
		_ = mergeEnv(cfgEnv, creds)
		assert.Equal(t, "cfg", cfgEnv["A"])
	})
}

func TestEnvSlice_Isolate(t *testing.T) {
	t.Setenv("GOAKT_MCP_SECRET_TEST", "leak-me")
	t.Setenv("PATH", "/isolated/bin")

	result := envSlice(map[string]string{"TOOL_KEY": "v1"}, true)

	for _, e := range result {
		assert.NotEqual(t, "GOAKT_MCP_SECRET_TEST=leak-me", e, "isolated env must not inherit arbitrary parent variables")
	}
	assert.Contains(t, result, "PATH=/isolated/bin", "minimal base keeps PATH")
	assert.Contains(t, result, "TOOL_KEY=v1", "configured/credential entries are applied")
}
