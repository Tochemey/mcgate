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

// Package schemaconv provides shared helpers for converting MCP SDK tool types
// into gateway ToolSchema values.
package schemaconv

import (
	"encoding/json"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/tochemey/portcullis/mcp"
)

// SDKToolsToSchemas converts a slice of SDK Tool pointers into gateway ToolSchema values.
func SDKToolsToSchemas(tools []*sdkmcp.Tool) []mcp.ToolSchema {
	if len(tools) == 0 {
		return nil
	}
	schemas := make([]mcp.ToolSchema, 0, len(tools))
	for _, t := range tools {
		if t == nil {
			continue
		}
		schemas = append(schemas, mcp.ToolSchema{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: MarshalInputSchema(t.InputSchema),
		})
	}
	return schemas
}

// MarshalInputSchema marshals the SDK tool's InputSchema (typed as any) into
// json.RawMessage. Returns nil when v is nil.
func MarshalInputSchema(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	if raw, ok := v.(json.RawMessage); ok {
		return raw
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return data
}
