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
	"context"

	goaktactor "github.com/tochemey/goakt/v4/actor"
	goaktlog "github.com/tochemey/goakt/v4/log"
	"github.com/tochemey/goakt/v4/supervisor"
	"github.com/tochemey/portcullis/internal/naming"
	"github.com/tochemey/portcullis/internal/runtime"
	actorextension "github.com/tochemey/portcullis/internal/runtime/actor/extension"
	"github.com/tochemey/portcullis/mcp"
)

// toolMetadataFetched carries the result of the asynchronous schema/resource
// fetch that registration starts via PipeTo. The backend I/O runs off the
// registrar's Receive goroutine; this message brings the result back so the
// catalog write happens on the actor goroutine, keeping the singleton
// responsive during registration and bootstrap (see startToolMetadataFetch).
//
// It is delivered only registrar→self on the registrar's own node, so it is
// never serialized across nodes. generation is the per-tool fetch token
// captured when the fetch started; a stale result (superseded by a newer
// registration, or a removed tool) is dropped by handleToolMetadataFetched.
type toolMetadataFetched struct {
	toolID      mcp.ToolID
	generation  uint64
	schemas     []mcp.ToolSchema
	resources   []mcp.ResourceSchema
	templates   []mcp.ResourceTemplateSchema
	schemaErr   error
	resourceErr error
}

// registrar is the Registry actor.
//
// Registrar is the canonical source of truth for tool metadata inside the runtime.
// It owns which tools exist, their current state, and the catalog data needed to
// serve discovery and admin queries. In clustered deployments, Registrar
// operates as a cluster singleton and holds metadata only: execution state
// lives with the cluster-placed tool supervisors and session grains.
//
// For each registered tool, Registrar spawns a ToolSupervisor via SpawnOn so
// the supervisor can be placed on any node (round-robin in cluster mode).
// Supervisors are addressed exclusively by their deterministic name
// (naming.ToolSupervisorName); no PID map is kept because supervisors
// relocate on node loss and a cached PID would go stale.
//
// Spawn: GatewayManager spawns Registrar in spawnFoundationalActors.
//   - Single-node: ctx.Spawn(ActorNameRegistrar, NewRegistrar()) as a child of GatewayManager.
//   - Cluster mode: system.SpawnSingleton(ctx, ActorNameRegistrar, NewRegistrar(), opts...)
//     when cluster.IsClusterConfigured(cfg) is true. NewRegistrar() is registered
//     as a cluster kind in cluster.BuildOptions.
//
// State layout:
//   - catalog holds the serializable tool entries (Tool + fetched metadata).
//     A future CRDT-backed implementation can replace the catalog without
//     changing the actor's message protocol.
//   - sessions is the registrar's aggregate view of active session grains,
//     maintained from SessionActivated/SessionDeactivated lifecycle messages
//     forwarded by the supervisors. Count and enumeration queries answer
//     from this map instead of fanning Ask calls out to every supervisor,
//     which kept the singleton registrar blocked inside Receive.
type registrar struct {
	catalog  *toolCatalog
	sessions map[mcp.ToolID]map[string]sessionRegistration
	logger   goaktlog.Logger
	// fetchGen is a per-tool, monotonically increasing token bumped every time a
	// schema/resource fetch is started. The result handler applies a fetch only
	// when its token is still current, so a slow in-flight fetch cannot clobber
	// a newer registration. Kept monotonic (never reset on remove) so a fetch
	// older than a remove+re-register cannot alias a fresh token.
	fetchGen map[mcp.ToolID]uint64
}

var _ goaktactor.Actor = (*registrar)(nil)

// NewRegistrar creates a new Registrar instance. Exported for cluster kind
// registration (cluster.BuildOptions) and for direct spawning by
// GatewayManager and tests.
func NewRegistrar() goaktactor.Actor { return &registrar{} }

// PreStart initializes Registrar before message processing begins.
func (x *registrar) PreStart(ctx *goaktactor.Context) error {
	x.logger = ctx.Logger()
	x.catalog = newToolCatalog()
	x.sessions = make(map[mcp.ToolID]map[string]sessionRegistration)
	x.fetchGen = make(map[mcp.ToolID]uint64)
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
	case *runtime.ListTools:
		x.handleListTools(ctx)
	case *runtime.CountSessionsForTenant:
		x.handleCountSessionsForTenant(ctx, msg)
	case *runtime.ListAllSessions:
		x.handleListAllSessions(ctx)
	case *runtime.SessionActivated:
		x.handleSessionActivated(msg)
	case *runtime.SessionDeactivated:
		x.handleSessionDeactivated(msg)
	case *runtime.GetToolSchema:
		x.handleGetToolSchema(ctx, msg)
	case *toolMetadataFetched:
		x.handleToolMetadataFetched(msg)
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
	x.ensureSupervisor(ctx, tool)
	x.fetchToolMetadata(ctx, tool)

	x.logger.Infof("actor=%s registered tool=%s (metadata fetch in progress)", naming.ActorNameRegistrar, tool.ID)
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
	x.notifySupervisor(ctx, updated)
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
	x.notifySupervisor(ctx, existing)
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
// tool availability. The supervisor is notified like the enable/disable paths
// do — otherwise the supervisor keeps serving the stale state and the
// registrar and supervisor views diverge.
func (x *registrar) handleUpdateToolHealth(ctx *goaktactor.ReceiveContext, msg *runtime.UpdateToolHealth) {
	existing, ok := x.catalog.Get(msg.ToolID)
	if !ok {
		x.respondIfAsk(ctx, &runtime.UpdateToolHealthResult{Err: mcp.ErrToolNotFound})
		return
	}
	existing.State = msg.State
	x.catalog.UpdateTool(existing)
	x.notifySupervisor(ctx, existing)
	x.logger.Debugf("actor=%s updated health tool=%s state=%s", naming.ActorNameRegistrar, msg.ToolID, msg.State)
	x.respondIfAsk(ctx, &runtime.UpdateToolHealthResult{})
}

// handleBootstrapTools bulk-registers tools from static configuration during
// startup. Invalid tools are logged and skipped. In cluster mode, every
// joining node's GatewayManager sends its static config here, so an already
// running supervisor is refreshed rather than respawned — killing it would
// discard warm circuit state and the session count for no reason.
func (x *registrar) handleBootstrapTools(ctx *goaktactor.ReceiveContext, msg *runtime.BootstrapTools) {
	for _, tool := range msg.Tools {
		if err := mcp.ValidateTool(tool); err != nil {
			x.logger.Warnf("actor=%s bootstrap skip tool=%s: %v", naming.ActorNameRegistrar, tool.ID, err)
			continue
		}

		x.catalog.Put(tool)
		x.ensureSupervisor(ctx, tool)
		x.fetchToolMetadata(ctx, tool)

		x.logger.Infof("actor=%s bootstrap registered tool=%s (metadata fetch in progress)", naming.ActorNameRegistrar, tool.ID)
	}
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

// ensureSupervisor makes sure a ToolSupervisor exists for the tool and holds
// its current definition: an already running supervisor is refreshed in place
// (preserving its circuit state and session count), otherwise one is spawned.
// The tool must already be in the catalog so the new supervisor's PostStart
// QueryTool finds it.
func (x *registrar) ensureSupervisor(ctx *goaktactor.ReceiveContext, tool mcp.Tool) {
	name := naming.ToolSupervisorName(tool.ID)
	if exists, err := ctx.ActorSystem().ActorExists(ctx.Context(), name); err == nil && exists {
		x.notifySupervisor(ctx, tool)
		return
	}
	x.spawnSupervisor(ctx, tool)
}

// spawnSupervisor creates a ToolSupervisorActor for the given tool via
// SpawnOn: in cluster mode the supervisor is placed round-robin across the
// nodes and relocated by goakt when its host node leaves; in single-node mode
// SpawnOn degrades to a plain local spawn. The supervisor pulls its tool
// config from this registrar in PostStart, so the catalog entry must be
// written before this call. Uses a supervisor strategy that resumes (does not
// suspend) on failure, so the tool supervisor remains available for
// subsequent admission checks.
func (x *registrar) spawnSupervisor(ctx *goaktactor.ReceiveContext, tool mcp.Tool) {
	name := naming.ToolSupervisorName(tool.ID)
	strategy := supervisor.NewSupervisor(supervisor.WithAnyErrorDirective(supervisor.ResumeDirective))

	_, err := ctx.ActorSystem().SpawnOn(ctx.Context(), name, NewToolSupervisor(),
		goaktactor.WithSupervisor(strategy),
		goaktactor.WithLongLived(),
		goaktactor.WithPlacement(goaktactor.RoundRobin))
	if err != nil {
		x.logger.Warnf("actor=%s spawn supervisor for tool=%s failed: %v", naming.ActorNameRegistrar, tool.ID, err)
	}
}

// stopSupervisorIfExists stops the supervisor for the given tool wherever it
// runs, by name. No-op when no supervisor exists.
func (x *registrar) stopSupervisorIfExists(ctx *goaktactor.ReceiveContext, toolID mcp.ToolID) {
	name := naming.ToolSupervisorName(toolID)
	if err := ctx.ActorSystem().Kill(ctx.Context(), name); err != nil {
		x.logger.Debugf("actor=%s stop supervisor for tool=%s: %v", naming.ActorNameRegistrar, toolID, err)
	}
}

// notifySupervisor sends the updated tool definition to the tool's supervisor
// via RefreshToolConfig. The supervisor is resolved by name so the message
// reaches it wherever it runs in the cluster; delivery is fire-and-forget
// because a supervisor that is gone (or relocating) will pull the current
// config from this registrar in its next PostStart anyway.
func (x *registrar) notifySupervisor(ctx *goaktactor.ReceiveContext, tool mcp.Tool) {
	pid, err := ctx.ActorSystem().ActorOf(ctx.Context(), naming.ToolSupervisorName(tool.ID))
	if err != nil || pid == nil {
		return
	}
	_ = goaktactor.Tell(ctx.Context(), pid, &runtime.RefreshToolConfig{Tool: tool})
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
	x.notifySupervisor(ctx, existing)
	x.logger.Infof("actor=%s enabled tool=%s", naming.ActorNameRegistrar, msg.ToolID)
	x.respondIfAsk(ctx, &runtime.EnableToolResult{})
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

// fetchToolMetadata clears any stale cached metadata and kicks off the
// backend schema/resource fetch OFF the registrar's Receive goroutine via
// PipeTo. Contacting the backend MCP server is blocking I/O bounded by the
// startup timeout; running it inline would stall the singleton (and, during a
// multi-tool bootstrap, queue every freshly spawned supervisor's PostStart
// QueryTool behind it). The fetchers are resolved here — Extension access is
// only valid on the actor goroutine — and captured by the task, which performs
// only the I/O and returns a toolMetadataFetched message; handleToolMetadataFetched
// writes the cache back on the actor goroutine. The task must not touch actor
// state, so it carries the tool by value and the fetch generation as a token.
//
// Trade-off: the schema/resource cache is eventually consistent — a discovery
// query in the brief fetch window sees the cleared (empty) cache. Schema fetching
// is best-effort anyway (failures leave an empty cache), so this only widens an
// existing tolerance; tool config, supervisor placement, and routing stay
// synchronous.
func (x *registrar) fetchToolMetadata(ctx *goaktactor.ReceiveContext, tool mcp.Tool) {
	// Clear stale metadata synchronously so a discovery query during the fetch
	// window never returns data from a previous registration.
	x.catalog.SetSchemas(tool.ID, nil)
	x.catalog.SetResources(tool.ID, nil, nil)

	x.fetchGen[tool.ID]++
	gen := x.fetchGen[tool.ID]

	schemaFetcher := x.resolveSchemaFetcher(ctx)
	resourceFetcher := x.resolveResourceFetcher(ctx)
	if schemaFetcher == nil && resourceFetcher == nil {
		return
	}

	ctx.PipeTo(ctx.Self(), func() (any, error) {
		result := &toolMetadataFetched{toolID: tool.ID, generation: gen}
		// TODO: use a context that wraps the actor's context with a timeout, so a slow fetch can be canceled when the actor is stopped.
		// The current background context means a slow fetch can linger after the actor is gone,
		// but it is harmless because the result is dropped on arrival (generation mismatch).
		fetchCtx := context.Background()
		if schemaFetcher != nil {
			result.schemas, result.schemaErr = schemaFetcher.FetchSchemas(fetchCtx, tool)
		}

		if resourceFetcher != nil {
			result.resources, result.templates, result.resourceErr = resourceFetcher.FetchResources(fetchCtx, tool)
		}

		return result, nil
	})
}

// handleToolMetadataFetched caches the result of an asynchronous fetch on the
// actor goroutine. A result is dropped when the tool is gone or a newer fetch
// has superseded it (generation mismatch), so a slow fetch never resurrects a
// removed tool or overwrites fresher metadata. Fetch errors leave the (already
// cleared) cache empty, matching the runtime's "operate without schemas" rule.
func (x *registrar) handleToolMetadataFetched(msg *toolMetadataFetched) {
	if !x.catalog.Has(msg.toolID) || x.fetchGen[msg.toolID] != msg.generation {
		return
	}

	if msg.schemaErr != nil {
		x.logger.Warnf("actor=%s schema fetch failed for tool=%s: %v", naming.ActorNameRegistrar, msg.toolID, msg.schemaErr)
	} else {
		x.catalog.SetSchemas(msg.toolID, msg.schemas)
	}

	if msg.resourceErr != nil {
		x.logger.Warnf("actor=%s resource fetch failed for tool=%s: %v", naming.ActorNameRegistrar, msg.toolID, msg.resourceErr)
	} else {
		x.catalog.SetResources(msg.toolID, msg.resources, msg.templates)
	}

	x.logger.Infof("actor=%s cached metadata tool=%s schemas=%d resources=%d templates=%d",
		naming.ActorNameRegistrar, msg.toolID,
		len(x.catalog.Schemas(msg.toolID)),
		len(x.catalog.Resources(msg.toolID)),
		len(x.catalog.ResourceTemplates(msg.toolID)))
}

// resolveSchemaFetcher returns the configured SchemaFetcher, or nil when the
// extension is not registered. Must be called on the actor goroutine.
func (x *registrar) resolveSchemaFetcher(ctx *goaktactor.ReceiveContext) mcp.SchemaFetcher {
	ext, ok := ctx.Extension(actorextension.SchemaFetcherExtensionID).(*actorextension.SchemaFetcherExtension)
	if !ok || ext == nil {
		return nil
	}
	return ext.Fetcher()
}

// resolveResourceFetcher returns the configured ResourceFetcher, or nil when
// the extension is not registered. Must be called on the actor goroutine.
func (x *registrar) resolveResourceFetcher(ctx *goaktactor.ReceiveContext) mcp.ResourceFetcher {
	ext, ok := ctx.Extension(actorextension.ResourceFetcherExtensionID).(*actorextension.ResourceFetcherExtension)
	if !ok || ext == nil {
		return nil
	}
	return ext.Fetcher()
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
