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

package mcpconv

import (
	"errors"
	"fmt"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tochemey/mcgate/mcp"
)

func TestParamsFromInvocation(t *testing.T) {
	t.Run("nil invocation returns empty", func(t *testing.T) {
		name, args := ParamsFromInvocation(nil)
		assert.Empty(t, name)
		assert.Nil(t, args)
	})

	t.Run("nil params returns empty", func(t *testing.T) {
		inv := &mcp.Invocation{ToolID: "my-tool"}
		name, args := ParamsFromInvocation(inv)
		assert.Empty(t, name)
		assert.Nil(t, args)
	})

	t.Run("extracts name and arguments from params", func(t *testing.T) {
		inv := &mcp.Invocation{
			ToolID: "my-tool",
			Params: map[string]any{
				"name":      "greet",
				"arguments": map[string]any{"user": "alice"},
			},
		}
		name, args := ParamsFromInvocation(inv)
		assert.Equal(t, "greet", name)
		argsMap, ok := args.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "alice", argsMap["user"])
	})

	t.Run("falls back to ToolID when name is missing", func(t *testing.T) {
		inv := &mcp.Invocation{
			ToolID: "fallback-tool",
			Params: map[string]any{
				"arguments": map[string]any{"x": 1},
			},
		}
		name, args := ParamsFromInvocation(inv)
		assert.Equal(t, "fallback-tool", name)
		assert.NotNil(t, args)
	})

	t.Run("falls back to ToolID when name is empty string", func(t *testing.T) {
		inv := &mcp.Invocation{
			ToolID: "fallback-tool",
			Params: map[string]any{
				"name":      "",
				"arguments": nil,
			},
		}
		name, _ := ParamsFromInvocation(inv)
		assert.Equal(t, "fallback-tool", name)
	})
}

func TestCallResultToOutput(t *testing.T) {
	t.Run("nil result returns nil", func(t *testing.T) {
		assert.Nil(t, CallResultToOutput(nil))
	})

	t.Run("empty result returns map without content key", func(t *testing.T) {
		res := &sdkmcp.CallToolResult{}
		out := CallResultToOutput(res)
		require.NotNil(t, out)
		_, hasContent := out["content"]
		assert.False(t, hasContent)
	})

	t.Run("text content is mapped", func(t *testing.T) {
		res := &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{
				&sdkmcp.TextContent{Text: "hello"},
			},
		}
		out := CallResultToOutput(res)
		content, ok := out["content"].([]map[string]any)
		require.True(t, ok)
		require.Len(t, content, 1)
		assert.Equal(t, "text", content[0]["type"])
		assert.Equal(t, "hello", content[0]["text"])
	})

	t.Run("image content is mapped with base64 data", func(t *testing.T) {
		res := &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{
				&sdkmcp.ImageContent{Data: []byte{0x89, 0x50}, MIMEType: "image/png"},
			},
		}
		out := CallResultToOutput(res)
		content, ok := out["content"].([]map[string]any)
		require.True(t, ok)
		require.Len(t, content, 1)
		assert.Equal(t, "image", content[0]["type"])
		assert.Equal(t, "image/png", content[0]["mimeType"])
		assert.NotEmpty(t, content[0]["data"])
	})

	t.Run("audio content is mapped", func(t *testing.T) {
		res := &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{
				&sdkmcp.AudioContent{Data: []byte{0x00}, MIMEType: "audio/wav"},
			},
		}
		out := CallResultToOutput(res)
		content, ok := out["content"].([]map[string]any)
		require.True(t, ok)
		require.Len(t, content, 1)
		assert.Equal(t, "audio", content[0]["type"])
		assert.Equal(t, "audio/wav", content[0]["mimeType"])
	})

	t.Run("embedded resource is mapped", func(t *testing.T) {
		res := &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{
				&sdkmcp.EmbeddedResource{
					Resource: &sdkmcp.ResourceContents{
						URI:      "file:///test.txt",
						MIMEType: "text/plain",
						Text:     "file contents",
					},
				},
			},
		}
		out := CallResultToOutput(res)
		content, ok := out["content"].([]map[string]any)
		require.True(t, ok)
		require.Len(t, content, 1)
		assert.Equal(t, "resource", content[0]["type"])
		assert.Equal(t, "file:///test.txt", content[0]["uri"])
		assert.Equal(t, "text/plain", content[0]["mimeType"])
		assert.Equal(t, "file contents", content[0]["text"])
	})

	t.Run("embedded resource preserves blob", func(t *testing.T) {
		blob := []byte{0xDE, 0xAD, 0xBE, 0xEF}
		res := &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{
				&sdkmcp.EmbeddedResource{
					Resource: &sdkmcp.ResourceContents{
						URI:      "file:///test.bin",
						MIMEType: "application/octet-stream",
						Blob:     blob,
					},
				},
			},
		}
		out := CallResultToOutput(res)
		content, ok := out["content"].([]map[string]any)
		require.True(t, ok)
		require.Len(t, content, 1)
		assert.Equal(t, "resource", content[0]["type"])
		assert.Equal(t, "file:///test.bin", content[0]["uri"])
		assert.Equal(t, "application/octet-stream", content[0]["mimeType"])
		assert.Equal(t, blob, content[0]["blob"])
		_, hasText := content[0]["text"]
		assert.False(t, hasText)
	})

	t.Run("resource link is mapped", func(t *testing.T) {
		res := &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{
				&sdkmcp.ResourceLink{
					URI:      "https://example.com",
					Name:     "example",
					MIMEType: "text/html",
				},
			},
		}
		out := CallResultToOutput(res)
		content, ok := out["content"].([]map[string]any)
		require.True(t, ok)
		require.Len(t, content, 1)
		assert.Equal(t, "resource_link", content[0]["type"])
		assert.Equal(t, "https://example.com", content[0]["uri"])
		assert.Equal(t, "example", content[0]["name"])
		assert.Equal(t, "text/html", content[0]["mimeType"])
	})

	t.Run("structured content is included", func(t *testing.T) {
		sc := map[string]any{"key": "val"}
		res := &sdkmcp.CallToolResult{StructuredContent: sc}
		out := CallResultToOutput(res)
		assert.Equal(t, sc, out["structuredContent"])
	})

	t.Run("mixed content types in single result", func(t *testing.T) {
		res := &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{
				&sdkmcp.TextContent{Text: "hello"},
				&sdkmcp.ImageContent{Data: []byte{0x01}, MIMEType: "image/jpeg"},
			},
		}
		out := CallResultToOutput(res)
		content, ok := out["content"].([]map[string]any)
		require.True(t, ok)
		require.Len(t, content, 2)
		assert.Equal(t, "text", content[0]["type"])
		assert.Equal(t, "image", content[1]["type"])
	})
}

func TestResourceParamsFromInvocation(t *testing.T) {
	t.Run("nil invocation returns empty", func(t *testing.T) {
		assert.Empty(t, ResourceParamsFromInvocation(nil))
	})

	t.Run("nil params returns empty", func(t *testing.T) {
		inv := &mcp.Invocation{ToolID: "my-tool"}
		assert.Empty(t, ResourceParamsFromInvocation(inv))
	})

	t.Run("extracts uri from params", func(t *testing.T) {
		inv := &mcp.Invocation{
			ToolID: "my-tool",
			Params: map[string]any{
				"uri": "file:///readme.md",
			},
		}
		assert.Equal(t, "file:///readme.md", ResourceParamsFromInvocation(inv))
	})

	t.Run("returns empty when uri is not a string", func(t *testing.T) {
		inv := &mcp.Invocation{
			ToolID: "my-tool",
			Params: map[string]any{
				"uri": 42,
			},
		}
		assert.Empty(t, ResourceParamsFromInvocation(inv))
	})

	t.Run("returns empty when uri key is absent", func(t *testing.T) {
		inv := &mcp.Invocation{
			ToolID: "my-tool",
			Params: map[string]any{
				"name": "something",
			},
		}
		assert.Empty(t, ResourceParamsFromInvocation(inv))
	})
}

func TestReadResourceResultToOutput(t *testing.T) {
	t.Run("nil result returns nil", func(t *testing.T) {
		assert.Nil(t, ReadResourceResultToOutput(nil))
	})

	t.Run("empty result returns map without contents key", func(t *testing.T) {
		res := &sdkmcp.ReadResourceResult{}
		out := ReadResourceResultToOutput(res)
		require.NotNil(t, out)
		_, hasContents := out["contents"]
		assert.False(t, hasContents)
	})

	t.Run("text resource content is mapped", func(t *testing.T) {
		res := &sdkmcp.ReadResourceResult{
			Contents: []*sdkmcp.ResourceContents{
				{URI: "file:///a.txt", MIMEType: "text/plain", Text: "hello world"},
			},
		}
		out := ReadResourceResultToOutput(res)
		contents, ok := out["contents"].([]map[string]any)
		require.True(t, ok)
		require.Len(t, contents, 1)
		assert.Equal(t, "file:///a.txt", contents[0]["uri"])
		assert.Equal(t, "text/plain", contents[0]["mimeType"])
		assert.Equal(t, "hello world", contents[0]["text"])
	})

	t.Run("blob resource content is mapped", func(t *testing.T) {
		res := &sdkmcp.ReadResourceResult{
			Contents: []*sdkmcp.ResourceContents{
				{URI: "file:///b.bin", MIMEType: "application/octet-stream", Blob: []byte{0x01, 0x02, 0x03}},
			},
		}
		out := ReadResourceResultToOutput(res)
		contents, ok := out["contents"].([]map[string]any)
		require.True(t, ok)
		require.Len(t, contents, 1)
		assert.Equal(t, "file:///b.bin", contents[0]["uri"])
		assert.Equal(t, "application/octet-stream", contents[0]["mimeType"])
		assert.Equal(t, []byte{0x01, 0x02, 0x03}, contents[0]["blob"])
	})

	t.Run("nil content entries are skipped", func(t *testing.T) {
		res := &sdkmcp.ReadResourceResult{
			Contents: []*sdkmcp.ResourceContents{
				nil,
				{URI: "file:///c.txt", Text: "valid"},
				nil,
			},
		}
		out := ReadResourceResultToOutput(res)
		contents, ok := out["contents"].([]map[string]any)
		require.True(t, ok)
		require.Len(t, contents, 1)
		assert.Equal(t, "file:///c.txt", contents[0]["uri"])
	})

	t.Run("empty optional fields are omitted", func(t *testing.T) {
		res := &sdkmcp.ReadResourceResult{
			Contents: []*sdkmcp.ResourceContents{
				{URI: "file:///d.txt"},
			},
		}
		out := ReadResourceResultToOutput(res)
		contents, ok := out["contents"].([]map[string]any)
		require.True(t, ok)
		require.Len(t, contents, 1)
		assert.Equal(t, "file:///d.txt", contents[0]["uri"])
		_, hasMime := contents[0]["mimeType"]
		assert.False(t, hasMime)
		_, hasText := contents[0]["text"]
		assert.False(t, hasText)
		_, hasBlob := contents[0]["blob"]
		assert.False(t, hasBlob)
	})

	t.Run("multiple resource contents are mapped", func(t *testing.T) {
		res := &sdkmcp.ReadResourceResult{
			Contents: []*sdkmcp.ResourceContents{
				{URI: "file:///e.txt", Text: "first"},
				{URI: "file:///f.txt", MIMEType: "text/plain", Text: "second"},
			},
		}
		out := ReadResourceResultToOutput(res)
		contents, ok := out["contents"].([]map[string]any)
		require.True(t, ok)
		require.Len(t, contents, 2)
		assert.Equal(t, "file:///e.txt", contents[0]["uri"])
		assert.Equal(t, "file:///f.txt", contents[1]["uri"])
	})
}

func TestContentErrorText(t *testing.T) {
	t.Run("nil result returns default", func(t *testing.T) {
		assert.Equal(t, "tool error", ContentErrorText(nil))
	})

	t.Run("empty content returns default", func(t *testing.T) {
		res := &sdkmcp.CallToolResult{}
		assert.Equal(t, "tool error", ContentErrorText(res))
	})

	t.Run("text content returns text", func(t *testing.T) {
		res := &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{
				&sdkmcp.TextContent{Text: "something went wrong"},
			},
		}
		assert.Equal(t, "something went wrong", ContentErrorText(res))
	})

	t.Run("non-text first content returns default", func(t *testing.T) {
		res := &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{
				&sdkmcp.ImageContent{Data: []byte{0x01}, MIMEType: "image/png"},
			},
		}
		assert.Equal(t, "tool error", ContentErrorText(res))
	})
}

func TestIsMethodNotFound(t *testing.T) {
	t.Run("nil error", func(t *testing.T) {
		assert.False(t, IsMethodNotFound(nil))
	})

	t.Run("plain error", func(t *testing.T) {
		assert.False(t, IsMethodNotFound(errors.New("boom")))
	})

	t.Run("jsonrpc method not found", func(t *testing.T) {
		err := &jsonrpc.Error{Code: jsonrpc.CodeMethodNotFound, Message: "method not found"}
		assert.True(t, IsMethodNotFound(err))
	})

	t.Run("wrapped jsonrpc method not found", func(t *testing.T) {
		err := fmt.Errorf("calling %q: %w", "resources/list", &jsonrpc.Error{Code: jsonrpc.CodeMethodNotFound, Message: "method not found"})
		assert.True(t, IsMethodNotFound(err))
	})

	t.Run("other jsonrpc code", func(t *testing.T) {
		err := &jsonrpc.Error{Code: jsonrpc.CodeInternalError, Message: "internal"}
		assert.False(t, IsMethodNotFound(err))
	})
}
