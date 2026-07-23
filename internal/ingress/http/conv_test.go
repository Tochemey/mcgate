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
	"encoding/json"
	"errors"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tochemey/portcullis/internal/ingress/pkg"
	"github.com/tochemey/portcullis/mcp"
)

// --- helpers -----------------------------------------------------------------

func makeCallToolRequest(name string, args map[string]any) *sdkmcp.CallToolRequest {
	raw, _ := json.Marshal(args)
	return &sdkmcp.CallToolRequest{
		Params: &sdkmcp.CallToolParamsRaw{
			Name:      name,
			Arguments: raw,
		},
	}
}

// --- requestToInvocation -----------------------------------------------------

func TestRequestToInvocation(t *testing.T) {
	t.Run("flat shape: no nested name uses gateway tool ID as backend name", func(t *testing.T) {
		// Client passes flat arguments with no "name" key.
		// Gateway tool ID is used as the backend tool name; args forwarded as-is.
		req := makeCallToolRequest("my-tool", map[string]any{"key": "val"})
		inv, err := pkg.RequestToInvocation(req, "my-tool", "tenant-1", "client-1")
		require.NoError(t, err)

		assert.Equal(t, mcp.ToolID("my-tool"), inv.ToolID)
		assert.Equal(t, "my-tool", inv.Params["name"])
		assert.Equal(t, "tools/call", inv.Method)
		assert.Equal(t, mcp.TenantID("tenant-1"), inv.Correlation.TenantID)
		assert.Equal(t, mcp.ClientID("client-1"), inv.Correlation.ClientID)
		assert.NotEmpty(t, inv.Correlation.RequestID)
		assert.False(t, inv.ReceivedAt.IsZero())

		params, ok := inv.Params["arguments"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "val", params["key"])
	})

	t.Run("nested shape: args name used as backend tool name", func(t *testing.T) {
		// Client passes {"name": "list_directory", "arguments": {"path": "/tmp"}}
		// Gateway routes to "filesystem" tool; backend call uses "list_directory".
		req := makeCallToolRequest("filesystem", map[string]any{
			"name":      "list_directory",
			"arguments": map[string]any{"path": "/tmp"},
		})
		inv, err := pkg.RequestToInvocation(req, "filesystem", "acme", "agent-1")
		require.NoError(t, err)

		assert.Equal(t, mcp.ToolID("filesystem"), inv.ToolID)
		assert.Equal(t, "list_directory", inv.Params["name"])

		backendArgs, ok := inv.Params["arguments"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "/tmp", backendArgs["path"])
	})

	t.Run("nil arguments produces nil args map", func(t *testing.T) {
		req := &sdkmcp.CallToolRequest{
			Params: &sdkmcp.CallToolParamsRaw{Name: "tool-x"},
		}
		inv, err := pkg.RequestToInvocation(req, "tool-x", "t", "c")
		require.NoError(t, err)
		assert.Equal(t, "tool-x", inv.Params["name"])
		assert.Nil(t, inv.Params["arguments"])
	})

	t.Run("nil request returns error", func(t *testing.T) {
		_, err := pkg.RequestToInvocation(nil, "tool", "t", "c")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "request and params are required")
	})

	t.Run("nil params returns error", func(t *testing.T) {
		_, err := pkg.RequestToInvocation(&sdkmcp.CallToolRequest{}, "tool", "t", "c")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "request and params are required")
	})

	t.Run("invalid JSON arguments returns error", func(t *testing.T) {
		req := &sdkmcp.CallToolRequest{
			Params: &sdkmcp.CallToolParamsRaw{
				Name:      "tool-y",
				Arguments: json.RawMessage(`not-json`),
			},
		}
		_, err := pkg.RequestToInvocation(req, "tool-y", "t", "c")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid tool arguments")
	})
}

// --- executionResultToCallToolResult -----------------------------------------

func TestExecutionResultToCallToolResult(t *testing.T) {
	t.Run("non-nil gateway error with nil result returns tool error", func(t *testing.T) {
		r := pkg.ExecutionResultToCallToolResult(nil, errors.New("boom"))
		require.NotNil(t, r)
		assert.True(t, r.IsError)
	})

	t.Run("nil result with nil error returns empty error", func(t *testing.T) {
		r := pkg.ExecutionResultToCallToolResult(nil, nil)
		require.NotNil(t, r)
		assert.True(t, r.IsError)
	})

	t.Run("non-success status returns tool error", func(t *testing.T) {
		res := &mcp.ExecutionResult{
			Status: mcp.ExecutionStatusDenied,
			Err:    mcp.NewRuntimeError(mcp.ErrCodePolicyDenied, "policy denied"),
		}
		r := pkg.ExecutionResultToCallToolResult(res, nil)
		require.NotNil(t, r)
		assert.True(t, r.IsError)
	})

	t.Run("success status with output returns content", func(t *testing.T) {
		res := &mcp.ExecutionResult{
			Status: mcp.ExecutionStatusSuccess,
			Output: map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": "hello"},
				},
			},
		}
		r := pkg.ExecutionResultToCallToolResult(res, nil)
		require.NotNil(t, r)
		assert.False(t, r.IsError)
		require.Len(t, r.Content, 1)
		txt, ok := r.Content[0].(*sdkmcp.TextContent)
		require.True(t, ok)
		assert.Equal(t, "hello", txt.Text)
	})

	t.Run("gateway error with non-nil result uses result", func(t *testing.T) {
		// gwErr != nil but res != nil — gateway error is ignored; result is used.
		res := &mcp.ExecutionResult{
			Status: mcp.ExecutionStatusSuccess,
			Output: map[string]any{"content": []map[string]any{{"type": "text", "text": "ok"}}},
		}
		r := pkg.ExecutionResultToCallToolResult(res, errors.New("soft-err"))
		require.NotNil(t, r)
		// res != nil, so result wins and error is ignored
		assert.False(t, r.IsError)
	})
}

// --- outputToCallToolResult --------------------------------------------------

func TestOutputToCallToolResult(t *testing.T) {
	t.Run("nil output returns empty result", func(t *testing.T) {
		r := pkg.OutputToCallToolResult(nil)
		require.NotNil(t, r)
		assert.Nil(t, r.Content)
	})

	t.Run("text content items are reconstructed", func(t *testing.T) {
		out := map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "line one"},
				{"type": "text", "text": "line two"},
			},
		}
		r := pkg.OutputToCallToolResult(out)
		require.Len(t, r.Content, 2)
		assert.Equal(t, "line one", r.Content[0].(*sdkmcp.TextContent).Text)
		assert.Equal(t, "line two", r.Content[1].(*sdkmcp.TextContent).Text)
		assert.Equal(t, out, r.StructuredContent)
	})

	t.Run("text item with empty text is skipped", func(t *testing.T) {
		out := map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": ""},
				{"type": "text", "text": "keep"},
			},
		}
		r := pkg.OutputToCallToolResult(out)
		require.Len(t, r.Content, 1)
		assert.Equal(t, "keep", r.Content[0].(*sdkmcp.TextContent).Text)
	})

	t.Run("non-text content falls back to JSON serialization", func(t *testing.T) {
		out := map[string]any{
			"content": []map[string]any{
				{"type": "image", "data": "base64abc"},
			},
		}
		r := pkg.OutputToCallToolResult(out)
		require.Len(t, r.Content, 1)
		txt, ok := r.Content[0].(*sdkmcp.TextContent)
		require.True(t, ok)
		assert.Contains(t, txt.Text, "image")
	})

	t.Run("output without content key falls back to JSON", func(t *testing.T) {
		out := map[string]any{"result": 42}
		r := pkg.OutputToCallToolResult(out)
		require.Len(t, r.Content, 1)
		txt, ok := r.Content[0].(*sdkmcp.TextContent)
		require.True(t, ok)
		assert.Contains(t, txt.Text, "42")
	})

	t.Run("JSON-decoded content ([]any) is reconstructed correctly", func(t *testing.T) {
		// Simulate the output after a JSON round-trip: json.Unmarshal produces
		// []any for arrays, not []map[string]any. Both paths must work.
		raw := map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "before"},
			},
		}
		// Round-trip through JSON to get []any.
		encoded, err := json.Marshal(raw)
		require.NoError(t, err)
		var decoded map[string]any
		require.NoError(t, json.Unmarshal(encoded, &decoded))

		r := pkg.OutputToCallToolResult(decoded)
		require.Len(t, r.Content, 1)
		txt, ok := r.Content[0].(*sdkmcp.TextContent)
		require.True(t, ok)
		assert.Equal(t, "before", txt.Text)
	})
}

// --- dispatchToolCall --------------------------------------------------------

// stubInvoker is a minimal Invoker for testing dispatchToolCall.
type stubInvoker struct {
	result *mcp.ExecutionResult
	err    error
}

func (s *stubInvoker) Invoke(_ context.Context, _ *mcp.Invocation) (*mcp.ExecutionResult, error) {
	return s.result, s.err
}

func (s *stubInvoker) ListTools(_ context.Context) ([]mcp.Tool, error) {
	return nil, nil
}

func TestDispatchToolCall(t *testing.T) {
	t.Run("successful invocation returns text content", func(t *testing.T) {
		gw := &stubInvoker{
			result: &mcp.ExecutionResult{
				Status: mcp.ExecutionStatusSuccess,
				Output: map[string]any{
					"content": []map[string]any{{"type": "text", "text": "done"}},
				},
			},
		}
		req := makeCallToolRequest("tool-a", map[string]any{"x": 1})
		r, err := pkg.DispatchToolCall(context.Background(), gw, req, "tool-a", "t1", "c1")
		require.NoError(t, err)
		require.NotNil(t, r)
		assert.False(t, r.IsError)
	})

	t.Run("invalid JSON arguments returns tool error not Go error", func(t *testing.T) {
		gw := &stubInvoker{}
		req := &sdkmcp.CallToolRequest{
			Params: &sdkmcp.CallToolParamsRaw{
				Name:      "tool-b",
				Arguments: json.RawMessage(`{bad json`),
			},
		}
		r, err := pkg.DispatchToolCall(context.Background(), gw, req, "tool-b", "t1", "c1")
		require.NoError(t, err) // must not propagate as Go error
		require.NotNil(t, r)
		assert.True(t, r.IsError)
	})

	t.Run("gateway invoke error is surfaced as tool error", func(t *testing.T) {
		gw := &stubInvoker{err: errors.New("internal failure")}
		req := makeCallToolRequest("tool-c", nil)
		r, err := pkg.DispatchToolCall(context.Background(), gw, req, "tool-c", "t1", "c1")
		require.NoError(t, err)
		require.NotNil(t, r)
		assert.True(t, r.IsError)
	})
}

// --- newRequestID ------------------------------------------------------------

func TestNewRequestID(t *testing.T) {
	id1 := pkg.NewRequestID()
	id2 := pkg.NewRequestID()
	assert.NotEmpty(t, string(id1))
	assert.NotEmpty(t, string(id2))
	assert.NotEqual(t, id1, id2, "request IDs must be unique")
	assert.Len(t, string(id1), 16, "expected 16 hex characters")
}
