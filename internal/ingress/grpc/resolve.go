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

package grpc

import (
	"context"
	"fmt"

	"github.com/tochemey/portcullis/internal/ingress/pkg"
	"github.com/tochemey/portcullis/mcp"
)

// resolveToolID maps a tool_name from a gRPC request to the canonical gateway
// ToolID by consulting the current tool registry via gw.ListTools.
//
// For single-schema tools the tool_name matches the tool ID directly. For
// multi-schema tools each schema name maps to its parent tool ID.
//
// Returns an error when the tool_name cannot be resolved.
func resolveToolID(ctx context.Context, gw pkg.Invoker, toolName string) (mcp.ToolID, error) {
	tools, err := gw.ListTools(ctx)
	if err != nil {
		return "", fmt.Errorf("list tools: %w", err)
	}

	for _, tool := range tools {
		// Single-schema tools: tool_name matches the tool ID directly.
		if string(tool.ID) == toolName {
			return tool.ID, nil
		}
		// Multi-schema tools: each schema name maps to the parent tool ID.
		for _, schema := range tool.Schemas {
			if schema.Name == toolName {
				return tool.ID, nil
			}
		}
	}

	return "", fmt.Errorf("tool %q not found", toolName)
}
