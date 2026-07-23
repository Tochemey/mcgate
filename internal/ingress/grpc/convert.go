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
	"encoding/json"

	pb "github.com/tochemey/goakt-mcp/internal/ingress/grpc/pb"
	"github.com/tochemey/goakt-mcp/mcp"
)

// contentTypeText is the MCP content type identifier for textual content items.
const contentTypeText = "text"

// outputKeyContent is the key in the standard MCP output map that holds the
// content items array. The expected shape is:
//
//	{"content": [{"type": "text", "text": "..."}, ...]}
const outputKeyContent = "content"

// contentFieldType is the key in a content item map that identifies the content
// kind (e.g. "text", "image", "audio").
const contentFieldType = "type"

// contentFieldText is the key in a content item map that holds textual content.
const contentFieldText = "text"

// contentFieldMimeType is the key in a content item map that holds the MIME
// type. Content items are produced by the egress layer, which writes the
// MIME type under [mcp.ContentKeyMIMEType] ("mimeType").
const contentFieldMimeType = mcp.ContentKeyMIMEType

// contentFieldData is the key in a content item map that holds binary data
// encoded as a base64 string.
const contentFieldData = "data"

// toolsToListToolsResponse converts the gateway's tool list into the proto
// ListToolsResponse message. Each tool is mapped to a ToolInfo with its
// schemas preserved.
func toolsToListToolsResponse(tools []mcp.Tool) *pb.ListToolsResponse {
	resp := &pb.ListToolsResponse{
		Tools: make([]*pb.ToolInfo, 0, len(tools)),
	}
	for _, tool := range tools {
		info := &pb.ToolInfo{
			Id:      string(tool.ID),
			Schemas: make([]*pb.ToolSchema, 0, len(tool.Schemas)),
		}
		for _, s := range tool.Schemas {
			info.Schemas = append(info.Schemas, &pb.ToolSchema{
				Name:        s.Name,
				Description: s.Description,
				InputSchema: s.InputSchema,
			})
		}
		resp.Tools = append(resp.Tools, info)
	}
	return resp
}

// executionResultToCallToolResponse converts a gateway ExecutionResult into the
// proto CallToolResponse message.
//
// Successful results have their output content items mapped to proto
// ContentItem messages. The full output map is serialized as JSON into
// structured_content for clients that need the raw data.
//
// Non-success results are returned with is_error set to true and the error
// message as a text content item.
func executionResultToCallToolResponse(result *mcp.ExecutionResult) *pb.CallToolResponse {
	if result == nil {
		return &pb.CallToolResponse{
			IsError: true,
			Content: []*pb.ContentItem{{
				Type: contentTypeText,
				Text: "empty result from gateway",
			}},
		}
	}

	if result.Status != mcp.ExecutionStatusSuccess {
		errMsg := string(result.Status)
		if result.Err != nil {
			errMsg = result.Err.Error()
		}
		return &pb.CallToolResponse{
			IsError: true,
			Content: []*pb.ContentItem{{
				Type: contentTypeText,
				Text: errMsg,
			}},
		}
	}

	resp := &pb.CallToolResponse{}

	// Serialize the full output as structured_content for clients that need
	// the raw data beyond what content items convey.
	if result.Output != nil {
		if data, err := json.Marshal(result.Output); err == nil {
			resp.StructuredContent = data
		}
	}

	// Extract content items from the standard MCP output shape.
	resp.Content = outputToContentItems(result.Output)
	return resp
}

// outputToContentItems extracts the "content" array from the standard MCP
// output map and converts each entry to a proto ContentItem.
//
// The expected output shape is:
//
//	{"content": [{"type": "text", "text": "..."}, ...]}
//
// Non-text items are included with their type, data, and mime_type fields
// preserved.
func outputToContentItems(output map[string]any) []*pb.ContentItem {
	if output == nil {
		return nil
	}

	rawContent, ok := output[outputKeyContent]
	if !ok {
		return nil
	}

	// Handle both in-memory ([]map[string]any) and JSON-decoded ([]any) paths.
	var items []map[string]any
	switch v := rawContent.(type) {
	case []map[string]any:
		items = v
	case []any:
		items = make([]map[string]any, 0, len(v))
		for _, raw := range v {
			if m, ok := raw.(map[string]any); ok {
				items = append(items, m)
			}
		}
	}

	if len(items) == 0 {
		return nil
	}

	result := make([]*pb.ContentItem, 0, len(items))
	for _, item := range items {
		ci := &pb.ContentItem{}
		if typ, ok := item[contentFieldType].(string); ok {
			ci.Type = typ
		}
		if text, ok := item[contentFieldText].(string); ok {
			ci.Text = text
		}
		if mimeType, ok := item[contentFieldMimeType].(string); ok {
			ci.MimeType = mimeType
		}
		// Binary data is stored as a base64 string in the output map and is
		// carried base64-encoded in the proto data field (see ContentItem.data).
		if data, ok := item[contentFieldData].(string); ok {
			ci.Data = []byte(data)
		}
		result = append(result, ci)
	}
	return result
}
