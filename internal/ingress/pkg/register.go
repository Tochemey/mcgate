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

package pkg

import (
	"context"
	"encoding/json"
	"net/http"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/tochemey/portcullis/mcp"
)

// OpenObjectSchema is the fallback JSON Schema that permits any JSON object.
// Used when no backend schema is available for a tool.
var OpenObjectSchema = json.RawMessage(`{"type":"object"}`)

// BuildGetServer returns the per-request server factory consumed by SDK
// handler constructors ([sdkmcp.NewStreamableHTTPHandler]).
//
// The ingress is stateless (MCP 2026-07-28), so the returned function runs on
// every request. Returning nil causes the SDK to send HTTP 400 Bad Request to
// the client, which is the correct response when identity resolution fails
// (malformed credentials, unknown tenant, etc.).
//
// Because construction is per-request, the tenant-visible tool list is served
// from a short-TTL cache instead of asking the registrar every time; only the
// in-memory SDK server assembly happens per request. If the tool list cannot
// be resolved, the request is rejected (returns nil) so the client cannot
// invoke tools against an incomplete server instance.
func BuildGetServer(gw Invoker, resolver mcp.IdentityResolver) func(*http.Request) *sdkmcp.Server {
	cache := newToolListCache(mcp.DefaultToolCacheTTL)
	return func(r *http.Request) *sdkmcp.Server {
		tenantID, clientID, err := resolver.ResolveIdentity(r)
		if err != nil {
			// nil causes the SDK to return 400 Bad Request.
			return nil
		}

		tools, err := cache.get(r.Context(), gw, tenantID)
		if err != nil {
			// Reject the request; the client should retry.
			return nil
		}

		srv := sdkmcp.NewServer(
			&sdkmcp.Implementation{Name: "portcullis"},
			nil,
		)

		for _, t := range tools {
			RegisterTool(srv, gw, t, tenantID, clientID)
			RegisterResources(srv, gw, t, tenantID, clientID)
		}

		return srv
	}
}

// RegisterTool adds tool capabilities to srv with handler closures that capture
// the resolved session identity.
//
// When the tool has cached backend schemas (from tools/list at registration),
// each schema is registered as a separate SDK tool with its actual name,
// description, and input schema. When no schemas are available, a single
// pass-through registration with [OpenObjectSchema] is used as a fallback.
//
// The handler is deliberately kept allocation-free on the hot path: the
// tenantID and clientID values are captured by value in the closure.
func RegisterTool(srv *sdkmcp.Server, gw Invoker, tool mcp.Tool, tenantID mcp.TenantID, clientID mcp.ClientID) {
	toolID := tool.ID
	if len(tool.Schemas) == 0 {
		sdkTool := &sdkmcp.Tool{
			Name:        string(tool.ID),
			InputSchema: OpenObjectSchema,
		}
		srv.AddTool(sdkTool, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
			return DispatchToolCall(ctx, gw, req, toolID, tenantID, clientID)
		})
		return
	}

	// The advertised schema names bound what the nested {"name": ...} call
	// shape may address: a tool registered with explicit per-sub-tool
	// schemas must not let clients redirect invocations to backend tools
	// outside that advertised surface.
	allowedNames := make(map[string]struct{}, len(tool.Schemas))
	for _, schema := range tool.Schemas {
		allowedNames[schema.Name] = struct{}{}
	}

	for _, schema := range tool.Schemas {
		inputSchema := schema.InputSchema
		if inputSchema == nil {
			inputSchema = OpenObjectSchema
		}
		sdkTool := &sdkmcp.Tool{
			Name:        schema.Name,
			Description: schema.Description,
			InputSchema: inputSchema,
		}
		srv.AddTool(sdkTool, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
			return DispatchToolCallRestricted(ctx, gw, req, toolID, tenantID, clientID, allowedNames)
		})
	}
}

// RegisterResources adds resource and resource template capabilities to srv
// with handler closures that capture the resolved session identity.
//
// Each resource discovered from the backend is registered on the per-session
// SDK server so that MCP clients can call resources/list and resources/read.
// The SDK server automatically advertises the resources capability when
// resources are added.
func RegisterResources(srv *sdkmcp.Server, gw Invoker, tool mcp.Tool, tenantID mcp.TenantID, clientID mcp.ClientID) {
	toolID := tool.ID

	for _, r := range tool.Resources {
		res := &sdkmcp.Resource{
			URI:         r.URI,
			Name:        r.Name,
			Description: r.Description,
			MIMEType:    r.MIMEType,
		}
		srv.AddResource(res, func(ctx context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
			return DispatchResourceRead(ctx, gw, req, toolID, tenantID, clientID)
		})
	}

	for _, t := range tool.ResourceTemplates {
		tmpl := &sdkmcp.ResourceTemplate{
			URITemplate: t.URITemplate,
			Name:        t.Name,
			Description: t.Description,
			MIMEType:    t.MIMEType,
		}
		srv.AddResourceTemplate(tmpl, func(ctx context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
			return DispatchResourceRead(ctx, gw, req, toolID, tenantID, clientID)
		})
	}
}
