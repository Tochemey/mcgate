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

package actor

import (
	goaktactor "github.com/tochemey/goakt/v4/actor"
	goaktlog "github.com/tochemey/goakt/v4/log"
	"github.com/tochemey/goakt/v4/supervisor"

	"github.com/tochemey/portcullis/mcp"

	"github.com/tochemey/portcullis/internal/naming"
	"github.com/tochemey/portcullis/internal/runtime"
	actorextension "github.com/tochemey/portcullis/internal/runtime/actor/extension"
)

// registrar is the Registry actor.
//
// Registrar is the canonical source of truth for tool metadata inside the runtime.
// It owns which tools exist, their current state, and the routing information needed
// to reach the responsible tool supervisor. In clustered deployments, Registrar
// operates as a cluster singleton.
//
// For each registered tool, Registrar spawns and supervises a ToolSupervisor.
// The supervisor PID is stored for lookup via GetSupervisor.
//
// Spawn: GatewayManager spawns Registrar in spawnFoundationalActors.
//   - Single-node: ctx.Spawn(ActorNameRegistrar, newRegistrar()) as a child of GatewayManager.
//   - Cluster mode: system.SpawnSingleton(ctx, ActorNameRegistrar, newRegistrar(), opts...)
//     when cluster.IsClusterConfigured(cfg) is true. NewRegistrar() is registered
//     as a cluster kind in cluster.BuildOptions.
//
// State layout:
//   - catalog holds the serializable tool entries (Tool + fetched metadata).
//     A future CRDT-backed implementation can replace the catalog without
//     changing the actor's message protocol.
//   - supervisors holds per-node PID references for spawned ToolSupervisors;
//     these are intentionally kept outside the catalog because PIDs are
//     node-local and cannot be replicated.
//   - sessions is the registrar's aggregate view of active session grains,
//     maintained from SessionActivated/SessionDeactivated lifecycle messages
//     forwarded by the supervisors. Count and enumeration queries answer
//     from this map instead of fanning Ask calls out to every supervisor,
//     which kept the singleton registrar blocked inside Receive.
type registrar struct {
	catalog     *toolCatalog
	supervisors map[mcp.ToolID]*goaktactor.PID
	sessions    map[mcp.ToolID]map[string]sessionRegistration
	logger      goaktlog.Logger
}

var _ goaktactor.Actor = (*registrar)(nil)

// newRegistrar creates a new Registrar instance.
func newRegistrar() *registrar {
	return &registrar{}
}

// NewRegistrar returns a Registrar instance for cluster kind registration.
// Used by the cluster bootstrap when Cluster.Enabled is true.
func NewRegistrar() goaktactor.Actor { return newRegistrar() }

// PreStart initializes Registrar before message processing begins.
func (x *registrar) PreStart(ctx *goaktactor.Context) error {
	x.logger = ctx.Logger()
	x.catalog = newToolCatalog()
	x.supervisors = make(map[mcp.ToolID]*goaktactor.PID)
	x.sessions = make(map[mcp.ToolID]map[string]sessionRegistration)
	ctx.Logger().Infof("actor=%s starting", naming.ActorNameRegistrar)
	return nil
}

// Receive handles messages delivered to RegistryActor.
func (x *registrar) Receive(ctx *goaktactor.ReceiveContext) {
	switch msg := ctx.Message().(type) {
	case *goaktactor.PostStart:
		x.logger.Infof("actor=%s started", naming.ActorNameRegistrar)
	case *runtime.RegisterTool:
		x.handleRegisterTool(ctx, msg)
	case *runtime.UpdateTool:
		x.handleUpdateTool(ctx, msg)
	case *runtime.DisableTool:
		x.handleDisableTool(ctx, msg)
	case *runtime.EnableTool:
		x.handleEnableTool(ctx, msg)
	case *runtime.RemoveTool:
		x.handleRemoveTool(ctx, msg)
	case *runtime.QueryTool:
		x.handleQueryTool(ctx, msg)
	case *runtime.UpdateToolHealth:
		x.handleUpdateToolHealth(ctx, msg)
	case *runtime.BootstrapTools:
		x.handleBootstrapTools(ctx, msg)
	case *runtime.GetSupervisor:
		x.handleGetSupervisor(ctx, msg)
	case *runtime.ListTools:
		x.handleListTools(ctx)
	case *runtime.CountSessionsForTenant:
		x.handleCountSessionsForTenant(ctx, msg)
	case *runtime.GetToolStatus:
		x.handleGetToolStatus(ctx, msg)
	case *runtime.ResetCircuit:
		x.handleResetCircuit(ctx, msg)
	case *runtime.DrainTool:
		x.handleDrainTool(ctx, msg)
	case *runtime.ListAllSessions:
		x.handleListAllSessions(ctx)
	case *runtime.SessionActivated:
		x.handleSessionActivated(msg)
	case *runtime.SessionDeactivated:
		x.handleSessionDeactivated(msg)
	case *runtime.GetToolSchema:
		x.handleGetToolSchema(ctx, msg)
	default:
		ctx.Unhandled()
	}
}

// PostStop performs cleanup after Registrar has stopped.
func (x *registrar) PostStop(ctx *goaktactor.Context) error {
	x.logger.Infof("actor=%s stopped", naming.ActorNameRegistrar)
	return nil
}

// handleRegisterTool validates and registers a tool. If a tool with the same ID
// was previously disabled, the disabled state is preserved.
//
// Re-registration keeps a running supervisor alive and refreshes its config
// instead of stopping and respawning it: the tool's session grains are not
// supervisor children, so a fresh supervisor would start with an empty
// session map (breaking MaxSessionsPerTool backpressure and admin listings
// until every pre-existing grain passivates) and a reset circuit breaker.
func (x *registrar) handleRegisterTool(ctx *goaktactor.ReceiveContext, msg *runtime.RegisterTool) {
	if err := mcp.ValidateTool(msg.Tool); err != nil {
		x.respondIfAsk(ctx, &runtime.RegisterToolResult{Err: err})
		return
	}

	tool := msg.Tool
	if existing, ok := x.catalog.Get(tool.ID); ok && existing.State == mcp.ToolStateDisabled {
		tool.State = mcp.ToolStateDisabled
	}

	x.catalog.Put(tool)
	x.fetchAndCacheSchemas(ctx, tool)
	x.fetchAndCacheResources(ctx, tool)

	if existing, ok := x.supervisors[tool.ID]; ok && existing != nil && existing.IsRunning() {
		x.updateToolExtension(ctx, tool)
		x.notifySupervisor(ctx, tool.ID)
	} else {
		x.stopSupervisorIfExists(ctx, tool.ID)
		if pid := x.spawnSupervisor(ctx, tool); pid != nil {
			x.supervisors[tool.ID] = pid
		}
	}

	x.logger.Infof("actor=%s registered tool=%s schemas=%d resources=%d templates=%d",
		naming.ActorNameRegistrar, tool.ID,
		len(x.catalog.Schemas(tool.ID)),
		len(x.catalog.Resources(tool.ID)),
		len(x.catalog.ResourceTemplates(tool.ID)))
	x.respondIfAsk(ctx, &runtime.RegisterToolResult{})
}

// handleUpdateTool applies mutable field updates to an existing tool. Identity
// and transport configuration are preserved from the original registration.
// Returns ErrToolNotFound when the tool does not exist.
func (x *registrar) handleUpdateTool(ctx *goaktactor.ReceiveContext, msg *runtime.UpdateTool) {
	existing, ok := x.catalog.Get(msg.Tool.ID)
	if !ok {
		x.respondIfAsk(ctx, &runtime.UpdateToolResult{Err: mcp.ErrToolNotFound})
		return
	}

	updated := msg.Tool
	updated.ID = existing.ID
	updated.Transport = existing.Transport
	updated.Stdio = existing.Stdio
	updated.HTTP = existing.HTTP

	if err := mcp.ValidateTool(updated); err != nil {
		x.respondIfAsk(ctx, &runtime.UpdateToolResult{Err: err})
		return
	}

	updated.State = existing.State
	x.catalog.UpdateTool(updated)
	x.updateToolExtension(ctx, updated)
	x.notifySupervisor(ctx, updated.ID)
	x.logger.Infof("actor=%s updated tool=%s", naming.ActorNameRegistrar, updated.ID)
	x.respondIfAsk(ctx, &runtime.UpdateToolResult{})
}

// handleDisableTool sets the tool state to ToolStateDisabled. The tool remains
// in the registry but all new requests are rejected. Returns ErrToolNotFound
// when the tool does not exist.
func (x *registrar) handleDisableTool(ctx *goaktactor.ReceiveContext, msg *runtime.DisableTool) {
	existing, ok := x.catalog.Get(msg.ToolID)
	if !ok {
		x.respondIfAsk(ctx, &runtime.DisableToolResult{Err: mcp.ErrToolNotFound})
		return
	}

	existing.State = mcp.ToolStateDisabled
	x.catalog.UpdateTool(existing)
	x.updateToolExtension(ctx, existing)
	x.notifySupervisor(ctx, msg.ToolID)
	x.logger.Infof("actor=%s disabled tool=%s", naming.ActorNameRegistrar, msg.ToolID)
	x.respondIfAsk(ctx, &runtime.DisableToolResult{})
}

// handleRemoveTool removes a tool from the registry and stops its supervisor.
// Returns ErrToolNotFound when the tool does not exist.
func (x *registrar) handleRemoveTool(ctx *goaktactor.ReceiveContext, msg *runtime.RemoveTool) {
	if !x.catalog.Has(msg.ToolID) {
		x.respondIfAsk(ctx, &runtime.RemoveToolResult{Err: mcp.ErrToolNotFound})
		return
	}

	x.stopSupervisorIfExists(ctx, msg.ToolID)
	x.catalog.Remove(msg.ToolID)
	delete(x.supervisors, msg.ToolID)
	delete(x.sessions, msg.ToolID)
	x.logger.Infof("actor=%s removed tool=%s", naming.ActorNameRegistrar, msg.ToolID)
	x.respondIfAsk(ctx, &runtime.RemoveToolResult{})
}

// handleQueryTool looks up a tool by ID and returns the authoritative definition.
// Returns Found=false and ErrToolNotFound when the tool is not registered.
func (x *registrar) handleQueryTool(ctx *goaktactor.ReceiveContext, msg *runtime.QueryTool) {
	tool, ok := x.catalog.Get(msg.ToolID)
	if !ok {
		x.respondIfAsk(ctx, &runtime.QueryToolResult{Found: false, Err: mcp.ErrToolNotFound})
		return
	}
	x.respondIfAsk(ctx, &runtime.QueryToolResult{Tool: &tool, Found: true})
}

// handleUpdateToolHealth transitions a tool's operational state (e.g. enabled,
// degraded, unavailable). Used by health probes and discovery to reflect actual
// tool availability. The ToolConfigExtension and supervisor are updated like
// the enable/disable paths do — otherwise the supervisor keeps serving the
// stale state and the registrar and supervisor views diverge.
func (x *registrar) handleUpdateToolHealth(ctx *goaktactor.ReceiveContext, msg *runtime.UpdateToolHealth) {
	existing, ok := x.catalog.Get(msg.ToolID)
	if !ok {
		x.respondIfAsk(ctx, &runtime.UpdateToolHealthResult{Err: mcp.ErrToolNotFound})
		return
	}
	existing.State = msg.State
	x.catalog.UpdateTool(existing)
	x.updateToolExtension(ctx, existing)
	x.notifySupervisor(ctx, msg.ToolID)
	x.logger.Debugf("actor=%s updated health tool=%s state=%s", naming.ActorNameRegistrar, msg.ToolID, msg.State)
	x.respondIfAsk(ctx, &runtime.UpdateToolHealthResult{})
}

// handleBootstrapTools bulk-registers tools from static configuration during
// startup. Invalid tools are logged and skipped. Each valid tool gets a
// supervisor spawned as a child of the registry.
func (x *registrar) handleBootstrapTools(ctx *goaktactor.ReceiveContext, msg *runtime.BootstrapTools) {
	for _, tool := range msg.Tools {
		if err := mcp.ValidateTool(tool); err != nil {
			x.logger.Warnf("actor=%s bootstrap skip tool=%s: %v", naming.ActorNameRegistrar, tool.ID, err)
			continue
		}

		x.stopSupervisorIfExists(ctx, tool.ID)
		x.catalog.Put(tool)
		x.fetchAndCacheSchemas(ctx, tool)
		x.fetchAndCacheResources(ctx, tool)
		if pid := x.spawnSupervisor(ctx, tool); pid != nil {
			x.supervisors[tool.ID] = pid
		}

		x.logger.Infof("actor=%s bootstrap registered tool=%s schemas=%d resources=%d templates=%d",
			naming.ActorNameRegistrar, tool.ID,
			len(x.catalog.Schemas(tool.ID)),
			len(x.catalog.Resources(tool.ID)),
			len(x.catalog.ResourceTemplates(tool.ID)))
	}
}

// handleGetSupervisor resolves the supervisor PID for the given tool. Returns
// Found=false and ErrToolNotFound when no supervisor exists.
func (x *registrar) handleGetSupervisor(ctx *goaktactor.ReceiveContext, msg *runtime.GetSupervisor) {
	pid, ok := x.supervisors[msg.ToolID]
	if !ok {
		x.respondIfAsk(ctx, &runtime.GetSupervisorResult{Found: false, Err: mcp.ErrToolNotFound})
		return
	}
	x.respondIfAsk(ctx, &runtime.GetSupervisorResult{Supervisor: pid, Found: true})
}

// handleListTools returns all registered tools with their cached schemas attached.
func (x *registrar) handleListTools(ctx *goaktactor.ReceiveContext) {
	x.respondIfAsk(ctx, &runtime.ListToolsResult{Tools: x.catalog.List()})
}

// handleCountSessionsForTenant sums active sessions for the tenant from the
// registrar's aggregate session map. Used by policy evaluation for the
// ConcurrentSessions quota. The map is maintained from lifecycle messages
// forwarded by the supervisors, so the answer is a local read: no Ask
// fan-out, no blocking inside the singleton registrar's Receive.
func (x *registrar) handleCountSessionsForTenant(ctx *goaktactor.ReceiveContext, msg *runtime.CountSessionsForTenant) {
	total := 0
	for _, regs := range x.sessions {
		for _, reg := range regs {
			if reg.TenantID == msg.TenantID {
				total++
			}
		}
	}
	x.respondIfAsk(ctx, &runtime.CountSessionsForTenantResult{Count: total})
}

// handleSessionActivated records a session grain's activation in the
// registrar's aggregate view. Forwarded by the tool supervisors.
func (x *registrar) handleSessionActivated(msg *runtime.SessionActivated) {
	regs, ok := x.sessions[msg.ToolID]
	if !ok {
		regs = make(map[string]sessionRegistration)
		x.sessions[msg.ToolID] = regs
	}
	name := naming.SessionName(msg.TenantID, msg.ClientID, msg.ToolID)
	regs[name] = sessionRegistration{TenantID: msg.TenantID, ClientID: msg.ClientID}
}

// handleSessionDeactivated drops a session grain from the registrar's
// aggregate view. Forwarded by the tool supervisors.
func (x *registrar) handleSessionDeactivated(msg *runtime.SessionDeactivated) {
	regs, ok := x.sessions[msg.ToolID]
	if !ok {
		return
	}
	delete(regs, naming.SessionName(msg.TenantID, msg.ClientID, msg.ToolID))
	if len(regs) == 0 {
		delete(x.sessions, msg.ToolID)
	}
}

// spawnSupervisor creates a ToolSupervisorActor as a child of the registry for
// the given tool. The tool is registered in the ToolConfigExtension system extension
// before spawning so the supervisor can resolve it in PostStart. Returns nil on error.
// Uses a supervisor strategy that resumes (does not suspend) when child sessions fail,
// so the tool supervisor remains available for subsequent GetOrCreateSession requests.
func (x *registrar) spawnSupervisor(ctx *goaktactor.ReceiveContext, tool mcp.Tool) *goaktactor.PID {
	if toolExt, ok := ctx.Extension(actorextension.ToolConfigExtensionID).(*actorextension.ToolConfigExtension); ok && toolExt != nil {
		toolExt.Register(tool)
	}
	name := naming.ToolSupervisorName(tool.ID)
	toolSupervisor := supervisor.NewSupervisor(supervisor.WithAnyErrorDirective(supervisor.ResumeDirective))
	return ctx.Spawn(name, newToolSupervisor(), goaktactor.WithSupervisor(toolSupervisor), goaktactor.WithLongLived())
}

// stopSupervisorIfExists stops the supervisor for the given tool if one is
// currently tracked and removes its config from the ToolConfigExtension.
// No-op when no supervisor exists.
func (x *registrar) stopSupervisorIfExists(ctx *goaktactor.ReceiveContext, toolID mcp.ToolID) {
	if pid, ok := x.supervisors[toolID]; ok {
		ctx.Stop(pid)
		delete(x.supervisors, toolID)
	}
	if toolExt, ok := ctx.Extension(actorextension.ToolConfigExtensionID).(*actorextension.ToolConfigExtension); ok && toolExt != nil {
		toolExt.Remove(toolID)
	}
}

// handleEnableTool sets the tool state to ToolStateEnabled. The tool must exist.
// The supervisor is notified of the config change via RefreshToolConfig.
// Returns ErrToolNotFound when the tool does not exist.
func (x *registrar) handleEnableTool(ctx *goaktactor.ReceiveContext, msg *runtime.EnableTool) {
	existing, ok := x.catalog.Get(msg.ToolID)
	if !ok {
		x.respondIfAsk(ctx, &runtime.EnableToolResult{Err: mcp.ErrToolNotFound})
		return
	}

	existing.State = mcp.ToolStateEnabled
	x.catalog.UpdateTool(existing)
	x.updateToolExtension(ctx, existing)
	x.notifySupervisor(ctx, msg.ToolID)
	x.logger.Infof("actor=%s enabled tool=%s", naming.ActorNameRegistrar, msg.ToolID)
	x.respondIfAsk(ctx, &runtime.EnableToolResult{})
}

// updateToolExtension updates the tool in the ToolConfigExtension system extension.
func (x *registrar) updateToolExtension(ctx *goaktactor.ReceiveContext, tool mcp.Tool) {
	if toolExt, ok := ctx.Extension(actorextension.ToolConfigExtensionID).(*actorextension.ToolConfigExtension); ok && toolExt != nil {
		toolExt.Register(tool)
	}
}

// notifySupervisor sends a RefreshToolConfig message to the tool's supervisor
// so it can reload the updated tool definition from the extension.
func (x *registrar) notifySupervisor(ctx *goaktactor.ReceiveContext, toolID mcp.ToolID) {
	if pid, ok := x.supervisors[toolID]; ok && pid != nil && pid.IsRunning() {
		_ = goaktactor.Tell(ctx.Context(), pid, &runtime.RefreshToolConfig{ToolID: toolID})
	}
}

// handleGetToolStatus relays GetToolStatus to the tool's supervisor and returns
// the result. Returns ErrToolNotFound when no supervisor exists for the tool.
func (x *registrar) handleGetToolStatus(ctx *goaktactor.ReceiveContext, msg *runtime.GetToolStatus) {
	if !x.catalog.Has(msg.ToolID) {
		x.respondIfAsk(ctx, &runtime.GetToolStatusResult{Err: mcp.ErrToolNotFound})
		return
	}
	pid, ok := x.supervisors[msg.ToolID]
	if !ok || pid == nil || !pid.IsRunning() {
		x.respondIfAsk(ctx, &runtime.GetToolStatusResult{Err: mcp.NewRuntimeError(mcp.ErrCodeToolUnavailable, "tool supervisor is not running")})
		return
	}
	resp, err := goaktactor.Ask(ctx.Context(), pid, msg, mcp.DefaultRequestTimeout)
	if err != nil {
		x.respondIfAsk(ctx, &runtime.GetToolStatusResult{Err: mcp.WrapRuntimeError(mcp.ErrCodeInternal, "supervisor ask failed", err)})
		return
	}
	result, ok := resp.(*runtime.GetToolStatusResult)
	if !ok {
		x.respondIfAsk(ctx, &runtime.GetToolStatusResult{Err: mcp.NewRuntimeError(mcp.ErrCodeInternal, "unexpected response type")})
		return
	}
	result.Status.Schemas = x.catalog.Schemas(msg.ToolID)
	x.respondIfAsk(ctx, result)
}

// handleResetCircuit relays ResetCircuit to the tool's supervisor.
// Returns ErrToolNotFound when the tool is not registered, or
// ErrCodeToolUnavailable when the supervisor is not running.
func (x *registrar) handleResetCircuit(ctx *goaktactor.ReceiveContext, msg *runtime.ResetCircuit) {
	if !x.catalog.Has(msg.ToolID) {
		x.respondIfAsk(ctx, &runtime.ResetCircuitResult{Err: mcp.ErrToolNotFound})
		return
	}
	pid, ok := x.supervisors[msg.ToolID]
	if !ok || pid == nil || !pid.IsRunning() {
		x.respondIfAsk(ctx, &runtime.ResetCircuitResult{Err: mcp.NewRuntimeError(mcp.ErrCodeToolUnavailable, "tool supervisor is not running")})
		return
	}
	resp, err := goaktactor.Ask(ctx.Context(), pid, msg, mcp.DefaultRequestTimeout)
	if err != nil {
		x.respondIfAsk(ctx, &runtime.ResetCircuitResult{Err: mcp.WrapRuntimeError(mcp.ErrCodeInternal, "supervisor ask failed", err)})
		return
	}
	result, ok := resp.(*runtime.ResetCircuitResult)
	if !ok {
		x.respondIfAsk(ctx, &runtime.ResetCircuitResult{Err: mcp.NewRuntimeError(mcp.ErrCodeInternal, "unexpected response type")})
		return
	}
	x.respondIfAsk(ctx, result)
}

// handleDrainTool relays DrainTool to the tool's supervisor.
// Returns ErrToolNotFound when the tool is not registered, or
// ErrCodeToolUnavailable when the supervisor is not running.
func (x *registrar) handleDrainTool(ctx *goaktactor.ReceiveContext, msg *runtime.DrainTool) {
	if !x.catalog.Has(msg.ToolID) {
		x.respondIfAsk(ctx, &runtime.DrainToolResult{Err: mcp.ErrToolNotFound})
		return
	}
	pid, ok := x.supervisors[msg.ToolID]
	if !ok || pid == nil || !pid.IsRunning() {
		x.respondIfAsk(ctx, &runtime.DrainToolResult{Err: mcp.NewRuntimeError(mcp.ErrCodeToolUnavailable, "tool supervisor is not running")})
		return
	}
	resp, err := goaktactor.Ask(ctx.Context(), pid, msg, mcp.DefaultRequestTimeout)
	if err != nil {
		x.respondIfAsk(ctx, &runtime.DrainToolResult{Err: mcp.WrapRuntimeError(mcp.ErrCodeInternal, "supervisor ask failed", err)})
		return
	}
	result, ok := resp.(*runtime.DrainToolResult)
	if !ok {
		x.respondIfAsk(ctx, &runtime.DrainToolResult{Err: mcp.NewRuntimeError(mcp.ErrCodeInternal, "unexpected response type")})
		return
	}
	x.respondIfAsk(ctx, result)
}

// handleListAllSessions enumerates active sessions from the registrar's
// aggregate session map, maintained from supervisor-forwarded lifecycle
// messages. A local read: no Ask fan-out, no blocking inside the singleton
// registrar's Receive.
func (x *registrar) handleListAllSessions(ctx *goaktactor.ReceiveContext) {
	var all []mcp.SessionInfo
	for toolID, regs := range x.sessions {
		for name, reg := range regs {
			all = append(all, mcp.SessionInfo{
				Name:     name,
				ToolID:   toolID,
				TenantID: reg.TenantID,
				ClientID: reg.ClientID,
			})
		}
	}
	x.respondIfAsk(ctx, &runtime.ListAllSessionsResult{Sessions: all})
}

// fetchAndCacheSchemas resolves the SchemaFetcherExtension from the actor system,
// fetches tool schemas from the backend MCP server, and stores them in the
// catalog. Stale schemas are cleared before fetching so a failed re-registration
// does not leave outdated schemas in the cache. Fetch failures are logged and
// result in an empty schema cache for the tool, allowing the runtime to
// operate without schemas.
func (x *registrar) fetchAndCacheSchemas(ctx *goaktactor.ReceiveContext, tool mcp.Tool) {
	// Clear any previously cached schemas so a fetch failure does not leave
	// stale data from an earlier registration.
	x.catalog.SetSchemas(tool.ID, nil)

	ext := ctx.Extension(actorextension.SchemaFetcherExtensionID)
	fetcherExt, ok := ext.(*actorextension.SchemaFetcherExtension)
	if !ok || fetcherExt == nil {
		x.logger.Debugf("actor=%s schema fetcher extension not configured, skipping schema fetch for tool=%s", naming.ActorNameRegistrar, tool.ID)
		return
	}

	fetcher := fetcherExt.Fetcher()
	if fetcher == nil {
		x.logger.Debugf("actor=%s schema fetcher is nil, skipping schema fetch for tool=%s", naming.ActorNameRegistrar, tool.ID)
		return
	}

	schemas, err := fetcher.FetchSchemas(ctx.Context(), tool)
	if err != nil {
		x.logger.Warnf("actor=%s schema fetch failed for tool=%s: %v", naming.ActorNameRegistrar, tool.ID, err)
		return
	}

	x.catalog.SetSchemas(tool.ID, schemas)
}

// fetchAndCacheResources resolves the ResourceFetcherExtension from the actor
// system, fetches MCP resource metadata from the backend server, and stores
// it in the catalog. Stale entries are cleared before fetching so a failed
// re-registration does not leave outdated data. Fetch failures are logged
// and result in empty caches for the tool, allowing the runtime to operate
// without resources.
func (x *registrar) fetchAndCacheResources(ctx *goaktactor.ReceiveContext, tool mcp.Tool) {
	x.catalog.SetResources(tool.ID, nil, nil)

	ext := ctx.Extension(actorextension.ResourceFetcherExtensionID)
	fetcherExt, ok := ext.(*actorextension.ResourceFetcherExtension)
	if !ok || fetcherExt == nil {
		x.logger.Debugf("actor=%s resource fetcher extension not configured, skipping resource fetch for tool=%s", naming.ActorNameRegistrar, tool.ID)
		return
	}

	fetcher := fetcherExt.Fetcher()
	if fetcher == nil {
		x.logger.Debugf("actor=%s resource fetcher is nil, skipping resource fetch for tool=%s", naming.ActorNameRegistrar, tool.ID)
		return
	}

	resources, templates, err := fetcher.FetchResources(ctx.Context(), tool)
	if err != nil {
		x.logger.Warnf("actor=%s resource fetch failed for tool=%s: %v", naming.ActorNameRegistrar, tool.ID, err)
		return
	}

	x.catalog.SetResources(tool.ID, resources, templates)
}

// handleGetToolSchema returns the cached schemas for a tool.
// Returns ErrToolNotFound when the tool is not registered.
func (x *registrar) handleGetToolSchema(ctx *goaktactor.ReceiveContext, msg *runtime.GetToolSchema) {
	if !x.catalog.Has(msg.ToolID) {
		x.respondIfAsk(ctx, &runtime.GetToolSchemaResult{Err: mcp.ErrToolNotFound})
		return
	}

	x.respondIfAsk(ctx, &runtime.GetToolSchemaResult{Schemas: x.catalog.Schemas(msg.ToolID)})
}

// respondIfAsk sends the response when the message was delivered via Ask.
// When delivered via Tell, the response channel is nil and this is a no-op.
func (x *registrar) respondIfAsk(ctx *goaktactor.ReceiveContext, resp any) {
	ctx.Response(resp)
}
