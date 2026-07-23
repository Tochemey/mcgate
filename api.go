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

	"github.com/tochemey/portcullis/internal/naming"
	"github.com/tochemey/portcullis/internal/runtime"
	"github.com/tochemey/portcullis/mcp"
)

// Invoke sends a tool invocation through the gateway and returns the execution result.
//
// The invocation is routed to the appropriate tool session via the RouterActor.
// Policy evaluation, credential resolution, and session management are handled
// internally by the runtime.
func (g *Gateway) Invoke(ctx context.Context, inv *mcp.Invocation) (*mcp.ExecutionResult, error) {
	system, err := g.requireRunning()
	if err != nil {
		return nil, err
	}

	router, err := g.resolveRouter(ctx, system)
	if err != nil {
		return nil, err
	}

	resp, err := goaktactor.Ask(ctx, router, &runtime.RouteInvocation{Invocation: inv}, g.config.Runtime.RequestTimeout)
	if err != nil {
		return nil, mcp.WrapRuntimeError(mcp.ErrCodeInternal, "router ask failed", err)
	}

	result, ok := resp.(*runtime.RouteResult)
	if !ok {
		return nil, mcp.NewRuntimeError(mcp.ErrCodeInternal, fmt.Sprintf("unexpected response type %T", resp))
	}

	if result.Err != nil {
		return result.Result, result.Err
	}
	return result.Result, nil
}

// InvokeStream sends a tool invocation through the gateway with streaming
// progress support. If the underlying executor supports streaming, the
// returned StreamingResult delivers progress events as they arrive, followed
// by the final ExecutionResult. If the executor does not support streaming,
// the StreamingResult's Progress channel is immediately closed and the final
// result is delivered on the Final channel.
//
// Callers that do not need progress can use StreamingResult.Collect() to
// drain progress and get the final result directly. For non-streaming callers,
// the standard Invoke method is preferred.
func (g *Gateway) InvokeStream(ctx context.Context, inv *mcp.Invocation) (*mcp.StreamingResult, error) {
	system, err := g.requireRunning()
	if err != nil {
		return nil, err
	}

	router, err := g.resolveRouter(ctx, system)
	if err != nil {
		return nil, err
	}

	resp, err := goaktactor.Ask(ctx, router, &runtime.RouteInvokeStream{Invocation: inv}, g.config.Runtime.RequestTimeout)
	if err != nil {
		return nil, mcp.WrapRuntimeError(mcp.ErrCodeInternal, "router ask failed", err)
	}

	result, ok := resp.(*runtime.RouteStreamResult)
	if !ok {
		return nil, mcp.NewRuntimeError(mcp.ErrCodeInternal, fmt.Sprintf("unexpected response type %T", resp))
	}

	if result.Err != nil {
		return nil, result.Err
	}

	// When the executor supports streaming, return the StreamingResult directly.
	if result.StreamResult != nil {
		return result.StreamResult, nil
	}

	// Fallback: the session returned a synchronous result (executor does not
	// implement ToolStreamExecutor). Wrap it in a StreamingResult so callers
	// always get a consistent return type.
	progressCh := make(chan mcp.ProgressEvent)
	close(progressCh)

	finalCh := make(chan *mcp.ExecutionResult, 1)
	finalCh <- result.Result
	close(finalCh)

	return &mcp.StreamingResult{
		Progress: progressCh,
		Final:    finalCh,
	}, nil
}

// ListTools returns all tools currently registered in the gateway.
func (g *Gateway) ListTools(ctx context.Context) ([]mcp.Tool, error) {
	system, err := g.requireRunning()
	if err != nil {
		return nil, err
	}

	registrar, err := g.resolveRegistrar(ctx, system)
	if err != nil {
		return nil, err
	}

	resp, err := goaktactor.Ask(ctx, registrar, &runtime.ListTools{}, g.config.Runtime.RequestTimeout)
	if err != nil {
		return nil, mcp.WrapRuntimeError(mcp.ErrCodeInternal, "registrar ask failed", err)
	}

	result, ok := resp.(*runtime.ListToolsResult)
	if !ok {
		return nil, mcp.NewRuntimeError(mcp.ErrCodeInternal, fmt.Sprintf("unexpected response type %T", resp))
	}
	return result.Tools, nil
}

// ListToolsForTenant returns the registered tools visible to the given tenant.
//
// Visibility mirrors the policy layer's tenant-allowlist authorization
// (internal/runtime/policy): a tool with AuthorizationPolicyTenantAllowlist is
// hidden when tenants are configured and tenantID is not among them. Tools
// without an authorization policy are visible to every tenant. Ingress
// handlers use this (via their TenantScopedLister check) so one tenant cannot
// enumerate another tenant's restricted tool names, descriptions, and input
// schemas through tools/list.
func (g *Gateway) ListToolsForTenant(ctx context.Context, tenantID mcp.TenantID) ([]mcp.Tool, error) {
	tools, err := g.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	return filterToolsForTenant(tools, g.config.Tenants, tenantID), nil
}

// filterToolsForTenant hides allowlist-guarded tools from tenants outside the
// configured tenant set, mirroring policy.checkAuthorization: with no tenants
// configured there is no restriction, and tools without the allowlist policy
// are always visible.
func filterToolsForTenant(tools []mcp.Tool, tenants []mcp.TenantConfig, tenantID mcp.TenantID) []mcp.Tool {
	if len(tenants) == 0 {
		return tools
	}

	tenantKnown := false
	for _, tc := range tenants {
		if tc.ID == tenantID {
			tenantKnown = true
			break
		}
	}
	if tenantKnown {
		return tools
	}

	visible := make([]mcp.Tool, 0, len(tools))
	for _, tool := range tools {
		if tool.AuthorizationPolicy == mcp.AuthorizationPolicyTenantAllowlist {
			continue
		}
		visible = append(visible, tool)
	}
	return visible
}

// RegisterTool adds or replaces a tool in the gateway registry.
//
// The tool is validated before registration. If a tool with the same ID exists,
// it is replaced.
func (g *Gateway) RegisterTool(ctx context.Context, tool mcp.Tool) error {
	system, err := g.requireRunning()
	if err != nil {
		return err
	}

	if err := mcp.ValidateTool(tool); err != nil {
		return err
	}

	registrar, err := g.resolveRegistrar(ctx, system)
	if err != nil {
		return err
	}

	resp, err := goaktactor.Ask(ctx, registrar, &runtime.RegisterTool{Tool: tool}, g.config.Runtime.RequestTimeout)
	if err != nil {
		return mcp.WrapRuntimeError(mcp.ErrCodeInternal, "registrar ask failed", err)
	}

	result, ok := resp.(*runtime.RegisterToolResult)
	if !ok {
		return mcp.NewRuntimeError(mcp.ErrCodeInternal, fmt.Sprintf("unexpected response type %T", resp))
	}
	return result.Err
}

// UpdateTool updates an existing tool's metadata in the registry.
//
// The tool must already exist. Only updatable fields are applied; the tool ID
// cannot be changed.
func (g *Gateway) UpdateTool(ctx context.Context, tool mcp.Tool) error {
	system, err := g.requireRunning()
	if err != nil {
		return err
	}

	if err := mcp.ValidateTool(tool); err != nil {
		return err
	}

	registrar, err := g.resolveRegistrar(ctx, system)
	if err != nil {
		return err
	}

	resp, err := goaktactor.Ask(ctx, registrar, &runtime.UpdateTool{Tool: tool}, g.config.Runtime.RequestTimeout)
	if err != nil {
		return mcp.WrapRuntimeError(mcp.ErrCodeInternal, "registrar ask failed", err)
	}

	result, ok := resp.(*runtime.UpdateToolResult)
	if !ok {
		return mcp.NewRuntimeError(mcp.ErrCodeInternal, fmt.Sprintf("unexpected response type %T", resp))
	}
	return result.Err
}

// DisableTool administratively disables a tool. The tool remains in the registry
// but its state is set to Disabled. Requests to disabled tools are rejected.
func (g *Gateway) DisableTool(ctx context.Context, toolID mcp.ToolID) error {
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

	resp, err := goaktactor.Ask(ctx, registrar, &runtime.DisableTool{ToolID: toolID}, g.config.Runtime.RequestTimeout)
	if err != nil {
		return mcp.WrapRuntimeError(mcp.ErrCodeInternal, "registrar ask failed", err)
	}

	result, ok := resp.(*runtime.DisableToolResult)
	if !ok {
		return mcp.NewRuntimeError(mcp.ErrCodeInternal, fmt.Sprintf("unexpected response type %T", resp))
	}
	return result.Err
}

// RemoveTool removes a tool from the registry entirely.
func (g *Gateway) RemoveTool(ctx context.Context, toolID mcp.ToolID) error {
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

	resp, err := goaktactor.Ask(ctx, registrar, &runtime.RemoveTool{ToolID: toolID}, g.config.Runtime.RequestTimeout)
	if err != nil {
		return mcp.WrapRuntimeError(mcp.ErrCodeInternal, "registrar ask failed", err)
	}

	result, ok := resp.(*runtime.RemoveToolResult)
	if !ok {
		return mcp.NewRuntimeError(mcp.ErrCodeInternal, fmt.Sprintf("unexpected response type %T", resp))
	}
	return result.Err
}

// EnableTool re-enables a previously disabled tool. The tool must exist in the
// registry. Its state is set to Enabled and the supervisor is notified.
func (g *Gateway) EnableTool(ctx context.Context, toolID mcp.ToolID) error {
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

	resp, err := goaktactor.Ask(ctx, registrar, &runtime.EnableTool{ToolID: toolID}, g.config.Runtime.RequestTimeout)
	if err != nil {
		return mcp.WrapRuntimeError(mcp.ErrCodeInternal, "registrar ask failed", err)
	}

	result, ok := resp.(*runtime.EnableToolResult)
	if !ok {
		return mcp.NewRuntimeError(mcp.ErrCodeInternal, fmt.Sprintf("unexpected response type %T", resp))
	}
	return result.Err
}

// resolveRegistrar finds the RegistrarActor PID, trying child lookup first and
// falling back to system-wide lookup for cluster singletons.
func (g *Gateway) resolveRegistrar(ctx context.Context, system goaktactor.ActorSystem) (*goaktactor.PID, error) {
	manager, err := system.ActorOf(ctx, g.managerName)
	if err != nil {
		return nil, mcp.WrapRuntimeError(mcp.ErrCodeInternal, "GatewayManager not found", err)
	}

	pid, err := manager.Child(naming.ActorNameRegistrar)
	if err == nil && pid != nil {
		return pid, nil
	}

	pid, err = system.ActorOf(ctx, naming.ActorNameRegistrar)
	if err != nil {
		return nil, mcp.WrapRuntimeError(mcp.ErrCodeInternal, "registrar not found", err)
	}
	return pid, nil
}

// resolveRouter picks a router from the per-node pool round-robin and returns
// its PID. Distributing invocations across the pool keeps one slow backend
// call from serializing every other invocation behind it: each router
// executes a single invocation at a time inside its message handler.
func (g *Gateway) resolveRouter(ctx context.Context, system goaktactor.ActorSystem) (*goaktactor.PID, error) {
	manager, err := system.ActorOf(ctx, g.managerName)
	if err != nil {
		return nil, mcp.WrapRuntimeError(mcp.ErrCodeInternal, "GatewayManager not found", err)
	}

	poolSize := g.config.Runtime.RouterPoolSize
	if poolSize <= 0 {
		poolSize = mcp.DefaultRouterPoolSize
	}
	name := naming.RouterName(int(g.routerCounter.Add(1) % uint64(poolSize)))

	if pid, err := manager.Child(name); err == nil && pid != nil {
		return pid, nil
	}
	if pid, err := system.ActorOf(ctx, name); err == nil && pid != nil {
		return pid, nil
	}

	// Fall back to the pool's first router when the selected member is
	// unavailable — e.g. it failed to spawn or was stopped.
	fallback := naming.RouterName(0)
	if fallback != name {
		if pid, err := manager.Child(fallback); err == nil && pid != nil {
			return pid, nil
		}
	}
	pid, err := system.ActorOf(ctx, fallback)
	if err != nil {
		return nil, mcp.WrapRuntimeError(mcp.ErrCodeInternal, "router not found", err)
	}
	return pid, nil
}
