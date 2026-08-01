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

import "github.com/tochemey/mcgate/mcp"

// Admin command and response types.
//
// These messages support operational visibility and control over running tools
// and sessions. They are used by the Gateway admin API methods (GetToolStatus,
// GetGatewayStatus, ListSessions, DrainTool, ResetCircuit).
//
// Commands that target a specific tool are relayed by the Registrar to the
// appropriate ToolSupervisorActor. All admin commands must be used with Ask.

// GetToolStatus is a request to retrieve the current operational status of a tool.
//
// The Registrar relays the request to the tool's ToolSupervisorActor, which
// reports its circuit state, session count, and drain status.
// Must be used with Ask. Response is GetToolStatusResult.
type GetToolStatus struct {
	ToolID mcp.ToolID
}

// GetToolStatusResult is the response to GetToolStatus.
//
// When Err is nil, Status holds the current operational state of the tool.
// When Err is non-nil (e.g. ErrToolNotFound), Status is zero.
type GetToolStatusResult struct {
	Status mcp.ToolStatus
	Err    error
}

// ResetCircuit is a command to manually reset a tool's circuit breaker.
//
// The circuit is transitioned to closed and the failure counter is reset to
// zero regardless of its current state. Use this to manually recover a tool
// whose circuit opened after transient failures that have since resolved.
// Must be used with Ask. Response is ResetCircuitResult.
type ResetCircuit struct {
	ToolID mcp.ToolID
}

// ResetCircuitResult is the response to ResetCircuit.
type ResetCircuitResult struct {
	Err error
}

// DrainTool is a command to administratively drain a tool.
//
// When a tool is draining, the supervisor rejects all new session requests
// with "tool is draining". Existing sessions continue until they passivate
// or complete naturally. Drain is sticky: call RegisterTool or EnableTool to
// re-enable the tool if needed.
// Must be used with Ask. Response is DrainToolResult.
type DrainTool struct {
	ToolID mcp.ToolID
}

// DrainToolResult is the response to DrainTool.
type DrainToolResult struct {
	Err error
}

// ListAllSessions is a request to enumerate all active sessions across all
// tools.
//
// The Registrar answers from its aggregate session view, maintained from the
// SessionActivated/SessionDeactivated lifecycle messages the supervisors
// forward. Must be used with Ask. Response is ListAllSessionsResult.
type ListAllSessions struct{}

// ListAllSessionsResult is the response to ListAllSessions.
type ListAllSessionsResult struct {
	Sessions []mcp.SessionInfo
}

// GetToolSchema is a request to retrieve the cached MCP tool schemas for a tool.
//
// The Registrar returns schemas that were fetched from the backend MCP server
// at registration time. Must be used with Ask. Response is GetToolSchemaResult.
type GetToolSchema struct {
	ToolID mcp.ToolID
}

// GetToolSchemaResult is the response to GetToolSchema.
type GetToolSchemaResult struct {
	Schemas []mcp.ToolSchema
	Err     error
}
