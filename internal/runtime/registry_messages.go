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

package runtime

import "github.com/tochemey/portcullis/mcp"

// Registry command and response types.
//
// These messages define the contract between RegistryActor and its callers.
// Commands are sent via Tell or Ask; responses are returned via ctx.Response
// when the command was delivered through Ask.
//
// All mutating commands (RegisterTool, UpdateTool, DisableTool, RemoveTool,
// UpdateToolHealth, BootstrapTools) can be used with Tell for fire-and-forget
// or with Ask when the caller needs to confirm success. QueryTool is
// request-response only and must be used with Ask.

// RegisterTool is a command to add or replace a tool in the registry.
//
// If a tool with the same ID already exists, it is replaced. The tool is
// registered in ToolStateEnabled unless it was previously disabled.
// Use with Tell for fire-and-forget or Ask for RegisterToolResult.
type RegisterTool struct {
	Tool mcp.Tool
}

// RegisterToolResult is the response to an Ask-wrapped RegisterTool.
type RegisterToolResult struct {
	Err error
}

// UpdateTool is a command to update an existing tool's policy and timing metadata.
//
// The tool must already exist. Only updatable fields (timing, routing, policies,
// transport config) are applied; the tool ID cannot be changed. Use with Tell or
// Ask for UpdateToolResult.
type UpdateTool struct {
	Tool mcp.Tool
}

// UpdateToolResult is the response to an Ask-wrapped UpdateTool.
type UpdateToolResult struct {
	Err error
}

// DisableTool is a command to administratively disable a tool.
//
// The tool remains in the registry but its state is set to ToolStateDisabled.
// Requests to disabled tools are rejected without attempting execution.
// Use with Tell or Ask for DisableToolResult.
type DisableTool struct {
	ToolID mcp.ToolID
}

// DisableToolResult is the response to an Ask-wrapped DisableTool.
type DisableToolResult struct {
	Err error
}

// RemoveTool is a command to remove a tool from the registry.
//
// The tool is fully removed. Any supervisor for this tool will be orphaned
// and should be stopped by the runtime. Use with Tell or Ask for RemoveToolResult.
type RemoveTool struct {
	ToolID mcp.ToolID
}

// RemoveToolResult is the response to an Ask-wrapped RemoveTool.
type RemoveToolResult struct {
	Err error
}

// QueryTool is a command to look up a tool by ID.
//
// Must be used with Ask. The response is QueryToolResult.
type QueryTool struct {
	ToolID mcp.ToolID
}

// QueryToolResult is the response to QueryTool.
//
// When Found is true, Tool holds the authoritative tool definition and Err is nil.
// When Found is false, Tool is nil and Err is ErrToolNotFound.
type QueryToolResult struct {
	Tool  *mcp.Tool
	Found bool
	Err   error
}

// UpdateToolHealth is a command to update a tool's operational state.
//
// Used by HealthActor and discovery components to report health transitions
// (e.g., degraded, unavailable). The tool must exist. Typically sent via Tell.
// Use with Ask for UpdateToolHealthResult when confirmation is needed.
type UpdateToolHealth struct {
	ToolID mcp.ToolID
	State  mcp.ToolState
}

// UpdateToolHealthResult is the response to an Ask-wrapped UpdateToolHealth.
type UpdateToolHealthResult struct {
	Err error
}

// BootstrapTools is a command to bulk-register tools from static configuration.
//
// Sent by GatewayManager during startup. Each tool is validated and registered.
// Duplicate IDs replace existing entries. Invalid tools are logged and skipped.
// Typically sent via Tell; no response is expected.
type BootstrapTools struct {
	Tools []mcp.Tool
}

// ListTools is a command to enumerate all registered tools.
//
// Used by HealthActor to discover tools for probing. Must be used with Ask.
// Response is ListToolsResult.
type ListTools struct{}

// ListToolsResult is the response to ListTools.
type ListToolsResult struct {
	Tools []mcp.Tool
}

// CountSessionsForTenant is a command to count active sessions for a tenant
// across all tools. Used by policy evaluation for ConcurrentSessions quota.
// Must be used with Ask. Response is CountSessionsForTenantResult.
type CountSessionsForTenant struct {
	TenantID mcp.TenantID
}

// CountSessionsForTenantResult is the response to CountSessionsForTenant.
type CountSessionsForTenantResult struct {
	Count int
}

// EnableTool is a command to re-enable a previously disabled tool.
//
// The tool must exist. Its state is set to ToolStateEnabled and the
// supervisor is notified of the config change. Use with Tell or Ask
// for EnableToolResult.
type EnableTool struct {
	ToolID mcp.ToolID
}

// EnableToolResult is the response to an Ask-wrapped EnableTool.
type EnableToolResult struct {
	Err error
}
