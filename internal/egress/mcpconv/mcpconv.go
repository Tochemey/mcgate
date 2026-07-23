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

// Package mcpconv provides shared MCP-to-runtime conversion helpers used by
// both the stdio and HTTP egress adapters.
package mcpconv

import (
	"encoding/base64"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/tochemey/goakt-mcp/mcp"
)

// fallbackToolErrorMessage is returned when a CallToolResult reports an
// error but has no text content to extract a human-readable message from.
const fallbackToolErrorMessage = "tool error"

// ParamsFromInvocation extracts the tool name and arguments from an Invocation.
// Params is expected to contain the standard MCP tools/call keys
// (mcp.ParamKeyName and mcp.ParamKeyArguments). When the name key is absent,
// the ToolID is used as a fallback so single-operation tools don't require
// the caller to duplicate the tool name in both places.
func ParamsFromInvocation(inv *mcp.Invocation) (string, any) {
	if inv == nil || inv.Params == nil {
		return "", nil
	}

	name, _ := inv.Params[mcp.ParamKeyName].(string)
	if name == "" {
		name = string(inv.ToolID)
	}
	return name, inv.Params[mcp.ParamKeyArguments]
}

// CallResultToOutput converts an MCP CallToolResult into the runtime output map.
// Returns nil when res is nil.
func CallResultToOutput(res *sdkmcp.CallToolResult) map[string]any {
	if res == nil {
		return nil
	}

	out := make(map[string]any, 2)
	if len(res.Content) > 0 {
		out[mcp.OutputKeyContent] = contentToSlice(res.Content)
	}
	if res.StructuredContent != nil {
		out[mcp.OutputKeyStructuredContent] = res.StructuredContent
	}
	return out
}

// ContentErrorText extracts a human-readable error message from a CallToolResult.
// Falls back to fallbackToolErrorMessage when no text content is present.
func ContentErrorText(res *sdkmcp.CallToolResult) string {
	if res == nil || len(res.Content) == 0 {
		return fallbackToolErrorMessage
	}
	if t, ok := res.Content[0].(*sdkmcp.TextContent); ok {
		return t.Text
	}
	return fallbackToolErrorMessage
}

// IsMethodNotFound reports whether err is a JSON-RPC "method not found" error
// (code -32601). MCP backends return it for optional capabilities they do not
// implement (e.g. resources/list on a server without resources), which is
// distinct from a transport failure or timeout.
func IsMethodNotFound(err error) bool {
	var jErr *jsonrpc.Error
	return errors.As(err, &jErr) && jErr.Code == jsonrpc.CodeMethodNotFound
}

// ResourceParamsFromInvocation extracts the resource URI from an Invocation.
// Params is expected to contain mcp.ParamKeyURI per the resources/read convention.
func ResourceParamsFromInvocation(inv *mcp.Invocation) string {
	if inv == nil || inv.Params == nil {
		return ""
	}

	uri, _ := inv.Params[mcp.ParamKeyURI].(string)
	return uri
}

// ReadResourceResultToOutput converts an MCP ReadResourceResult into the runtime
// output map. Returns nil when res is nil.
func ReadResourceResultToOutput(res *sdkmcp.ReadResourceResult) map[string]any {
	if res == nil {
		return nil
	}

	out := make(map[string]any, 1)
	if len(res.Contents) > 0 {
		contents := make([]map[string]any, 0, len(res.Contents))
		for _, c := range res.Contents {
			if c == nil {
				continue
			}

			item := make(map[string]any, 4)
			item[mcp.ContentKeyURI] = c.URI
			if c.MIMEType != "" {
				item[mcp.ContentKeyMIMEType] = c.MIMEType
			}
			if c.Text != "" {
				item[mcp.ContentKeyText] = c.Text
			}
			if len(c.Blob) > 0 {
				item[mcp.ContentKeyBlob] = c.Blob
			}
			contents = append(contents, item)
		}
		out[mcp.OutputKeyContents] = contents
	}
	return out
}

func contentToSlice(c []sdkmcp.Content) []map[string]any {
	s := make([]map[string]any, 0, len(c))
	for _, item := range c {
		switch t := item.(type) {
		case *sdkmcp.TextContent:
			s = append(s, map[string]any{
				mcp.ContentKeyType: mcp.ContentTypeText,
				mcp.ContentKeyText: t.Text,
			})
		case *sdkmcp.ImageContent:
			s = append(s, map[string]any{
				mcp.ContentKeyType:     mcp.ContentTypeImage,
				mcp.ContentKeyData:     base64.StdEncoding.EncodeToString(t.Data),
				mcp.ContentKeyMIMEType: t.MIMEType,
			})
		case *sdkmcp.AudioContent:
			s = append(s, map[string]any{
				mcp.ContentKeyType:     mcp.ContentTypeAudio,
				mcp.ContentKeyData:     base64.StdEncoding.EncodeToString(t.Data),
				mcp.ContentKeyMIMEType: t.MIMEType,
			})
		case *sdkmcp.EmbeddedResource:
			m := map[string]any{mcp.ContentKeyType: mcp.ContentTypeResource}
			if t.Resource != nil {
				m[mcp.ContentKeyURI] = t.Resource.URI
				if t.Resource.MIMEType != "" {
					m[mcp.ContentKeyMIMEType] = t.Resource.MIMEType
				}
				if t.Resource.Text != "" {
					m[mcp.ContentKeyText] = t.Resource.Text
				}
				if len(t.Resource.Blob) > 0 {
					m[mcp.ContentKeyBlob] = t.Resource.Blob
				}
			}
			s = append(s, m)
		case *sdkmcp.ResourceLink:
			m := map[string]any{
				mcp.ContentKeyType: mcp.ContentTypeResourceLink,
				mcp.ContentKeyURI:  t.URI,
				mcp.ContentKeyName: t.Name,
			}
			if t.MIMEType != "" {
				m[mcp.ContentKeyMIMEType] = t.MIMEType
			}
			s = append(s, m)
		default:
			s = append(s, map[string]any{mcp.ContentKeyType: mcp.ContentTypeUnknown})
		}
	}
	return s
}
