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

package portcullis

import (
	"context"
	"fmt"

	goaktactor "github.com/tochemey/goakt/v4/actor"

	"github.com/tochemey/portcullis/internal/runtime"
	"github.com/tochemey/portcullis/mcp"
)

// GetToolStatus returns the current operational status of a registered tool.
//
// The status includes the tool's state, circuit breaker state, active session
// count, and whether the tool is being drained. Returns ErrToolNotFound when
// no tool with the given ID is registered.
func (g *Gateway) GetToolStatus(ctx context.Context, toolID mcp.ToolID) (*mcp.ToolStatus, error) {
	system, err := g.requireRunning()
	if err != nil {
		return nil, err
	}

	if toolID.IsZero() {
		return nil, mcp.NewRuntimeError(mcp.ErrCodeInvalidRequest, "tool ID is required")
	}

	registrar, err := g.resolveRegistrar(ctx, system)
	if err != nil {
		return nil, err
	}

	resp, err := goaktactor.Ask(ctx, registrar, &runtime.GetToolStatus{ToolID: toolID}, g.config.Runtime.RequestTimeout)
	if err != nil {
		return nil, mcp.WrapRuntimeError(mcp.ErrCodeInternal, "registrar ask failed", err)
	}

	result, ok := resp.(*runtime.GetToolStatusResult)
	if !ok {
		return nil, mcp.NewRuntimeError(mcp.ErrCodeInternal, fmt.Sprintf("unexpected response type %T", resp))
	}
	if result.Err != nil {
		return nil, result.Err
	}
	return &result.Status, nil
}

// GetGatewayStatus returns the current operational state of the gateway,
// including whether it is running, the total tool count, and the total active
// session count across all tools.
func (g *Gateway) GetGatewayStatus(ctx context.Context) (*mcp.GatewayStatus, error) {
	g.mu.RLock()
	running := g.system != nil && !g.draining
	g.mu.RUnlock()

	if !running {
		return &mcp.GatewayStatus{Running: false}, nil
	}

	system, err := g.requireRunning()
	if err != nil {
		return &mcp.GatewayStatus{Running: false}, nil
	}

	registrar, err := g.resolveRegistrar(ctx, system)
	if err != nil {
		return nil, err
	}

	toolsResp, err := goaktactor.Ask(ctx, registrar, &runtime.ListTools{}, g.config.Runtime.RequestTimeout)
	if err != nil {
		return nil, mcp.WrapRuntimeError(mcp.ErrCodeInternal, "registrar ask failed", err)
	}
	toolsResult, ok := toolsResp.(*runtime.ListToolsResult)
	if !ok {
		return nil, mcp.NewRuntimeError(mcp.ErrCodeInternal, fmt.Sprintf("unexpected response type %T", toolsResp))
	}

	sessionsResp, err := goaktactor.Ask(ctx, registrar, &runtime.ListAllSessions{}, g.config.Runtime.RequestTimeout)
	if err != nil {
		return nil, mcp.WrapRuntimeError(mcp.ErrCodeInternal, "registrar ask failed", err)
	}
	sessionsResult, ok := sessionsResp.(*runtime.ListAllSessionsResult)
	if !ok {
		return nil, mcp.NewRuntimeError(mcp.ErrCodeInternal, fmt.Sprintf("unexpected response type %T", sessionsResp))
	}

	return &mcp.GatewayStatus{
		Running:      true,
		ToolCount:    len(toolsResult.Tools),
		SessionCount: len(sessionsResult.Sessions),
	}, nil
}

// ListSessions returns all active sessions across all registered tools.
//
// Each SessionInfo contains the session's actor name, the tool it is bound to,
// and the tenant and client identities.
func (g *Gateway) ListSessions(ctx context.Context) ([]mcp.SessionInfo, error) {
	system, err := g.requireRunning()
	if err != nil {
		return nil, err
	}

	registrar, err := g.resolveRegistrar(ctx, system)
	if err != nil {
		return nil, err
	}

	resp, err := goaktactor.Ask(ctx, registrar, &runtime.ListAllSessions{}, g.config.Runtime.RequestTimeout)
	if err != nil {
		return nil, mcp.WrapRuntimeError(mcp.ErrCodeInternal, "registrar ask failed", err)
	}

	result, ok := resp.(*runtime.ListAllSessionsResult)
	if !ok {
		return nil, mcp.NewRuntimeError(mcp.ErrCodeInternal, fmt.Sprintf("unexpected response type %T", resp))
	}
	return result.Sessions, nil
}

// DrainTool administratively drains a tool. Once draining, the tool's
// supervisor rejects all new session requests. Existing sessions continue
// until they passivate or complete naturally.
//
// To re-enable a drained tool, call RegisterTool or EnableTool with the
// same tool ID.
func (g *Gateway) DrainTool(ctx context.Context, toolID mcp.ToolID) error {
	system, err := g.requireRunning()
	if err != nil {
		return err
	}

	if toolID.IsZero() {
		return mcp.NewRuntimeError(mcp.ErrCodeInvalidRequest, "tool ID is required")
	}

	registrar, err := g.resolveRegistrar(ctx, system)
	if err != nil {
		return err
	}

	resp, err := goaktactor.Ask(ctx, registrar, &runtime.DrainTool{ToolID: toolID}, g.config.Runtime.RequestTimeout)
	if err != nil {
		return mcp.WrapRuntimeError(mcp.ErrCodeInternal, "registrar ask failed", err)
	}

	result, ok := resp.(*runtime.DrainToolResult)
	if !ok {
		return mcp.NewRuntimeError(mcp.ErrCodeInternal, fmt.Sprintf("unexpected response type %T", resp))
	}
	return result.Err
}

// GetToolSchema returns the cached MCP tool schemas for a registered tool.
//
// Schemas are fetched from the backend MCP server at registration time via
// tools/list and cached by the registrar. Returns ErrToolNotFound when no
// tool with the given ID is registered.
func (g *Gateway) GetToolSchema(ctx context.Context, toolID mcp.ToolID) ([]mcp.ToolSchema, error) {
	system, err := g.requireRunning()
	if err != nil {
		return nil, err
	}

	if toolID.IsZero() {
		return nil, mcp.NewRuntimeError(mcp.ErrCodeInvalidRequest, "tool ID is required")
	}

	registrar, err := g.resolveRegistrar(ctx, system)
	if err != nil {
		return nil, err
	}

	resp, err := goaktactor.Ask(ctx, registrar, &runtime.GetToolSchema{ToolID: toolID}, g.config.Runtime.RequestTimeout)
	if err != nil {
		return nil, mcp.WrapRuntimeError(mcp.ErrCodeInternal, "registrar ask failed", err)
	}

	result, ok := resp.(*runtime.GetToolSchemaResult)
	if !ok {
		return nil, mcp.NewRuntimeError(mcp.ErrCodeInternal, fmt.Sprintf("unexpected response type %T", resp))
	}
	if result.Err != nil {
		return nil, result.Err
	}
	return result.Schemas, nil
}

// ResetCircuit manually resets the circuit breaker for a tool to the closed
// state, clearing the failure counter. Use this to manually recover a tool
// whose circuit tripped after transient failures that have since resolved.
func (g *Gateway) ResetCircuit(ctx context.Context, toolID mcp.ToolID) error {
	system, err := g.requireRunning()
	if err != nil {
		return err
	}

	if toolID.IsZero() {
		return mcp.NewRuntimeError(mcp.ErrCodeInvalidRequest, "tool ID is required")
	}

	registrar, err := g.resolveRegistrar(ctx, system)
	if err != nil {
		return err
	}

	resp, err := goaktactor.Ask(ctx, registrar, &runtime.ResetCircuit{ToolID: toolID}, g.config.Runtime.RequestTimeout)
	if err != nil {
		return mcp.WrapRuntimeError(mcp.ErrCodeInternal, "registrar ask failed", err)
	}

	result, ok := resp.(*runtime.ResetCircuitResult)
	if !ok {
		return mcp.NewRuntimeError(mcp.ErrCodeInternal, fmt.Sprintf("unexpected response type %T", resp))
	}
	return result.Err
}
