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
	"strconv"
	"time"

	goaktactor "github.com/tochemey/goakt/v4/actor"
	goaktlog "github.com/tochemey/goakt/v4/log"
	"github.com/tochemey/mcgate/internal/naming"
	"github.com/tochemey/mcgate/internal/runtime"
	actorextension "github.com/tochemey/mcgate/internal/runtime/actor/extension"
	"github.com/tochemey/mcgate/internal/runtime/audit"
	"github.com/tochemey/mcgate/internal/runtime/telemetry"
	"github.com/tochemey/mcgate/mcp"
)

// Reason strings returned on CanAcceptWorkResult when the supervisor rejects
// work. Dashboards and tests match on these values, so the strings are kept
// stable here rather than inlined at each call site.
const (
	canAcceptReasonToolMismatch    = "tool ID mismatch"
	canAcceptReasonToolDisabled    = "tool is disabled"
	canAcceptReasonToolDraining    = "tool is draining"
	canAcceptReasonCircuitOpen     = "circuit is open"
	canAcceptReasonCircuitHalfOpen = "circuit is half-open"
	canAcceptReasonHalfOpenLimit   = "half-open probe limit reached"
	canAcceptReasonBackpressureCap = "tool session limit reached (backpressure)"
)

// sessionRegistration is the per-tenant+client tuple the supervisor tracks
// for every active session grain. The map of these tuples (keyed by the
// grain name) is the supervisor's authoritative view of local session
// count and identity used by backpressure checks and admin queries.
type sessionRegistration struct {
	TenantID mcp.TenantID
	ClientID mcp.ClientID
}

// toolSupervisor is the ToolSupervisor Actor.
//
// There is one supervisor per registered tool. The supervisor owns the tool's
// circuit breaker state and per-tool session count, and decides whether
// work should proceed or fail fast based on circuit state and
// administrative disable status.
//
// Spawn: the Registrar's supervisorManager child spawns ToolSupervisor via
// ActorSystem.SpawnOn(naming.ToolSupervisorName(tool.ID), ...). One supervisor
// per registered tool; spawned on RegisterTool or BootstrapTools. In cluster
// mode, SpawnOn places the supervisor on any node (round-robin), so execution
// spreads across the cluster instead of piling onto the registrar's node.
//
// Relocation: Yes. Supervisors are relocatable cluster actors; when their host
// node leaves the cluster, goakt recreates them on a survivor. Relocation runs
// the lifecycle hooks from scratch, so the circuit breaker, drain flag, and
// session count restart from their zero values — an accepted v1 trade-off: a
// freshly closed circuit re-learns backend health from live traffic, and the
// session count is repopulated as grains re-activate and report in.
//
// The tool definition is pulled from the Registrar (QueryTool) in PostStart,
// using the tool ID derived from the actor name. The registrar is the
// cluster-wide source of truth, so a relocated supervisor always resumes with
// the current config rather than a spawn-time snapshot. Circuit parameters use
// runtime defaults unless overridden by a CircuitConfigExtension system extension.
//
// Sessions are goakt virtual actors (grains), not child actors. The
// supervisor owns a name-keyed map of [sessionRegistration] that lifecycle
// messages ([runtime.SessionActivated] / [runtime.SessionDeactivated])
// keep up to date. The supervisor never activates sessions: routers activate
// grain identities directly via the grain engine.
//
// All fields are unexported to enforce actor immutability rules.
type toolSupervisor struct {
	tool     mcp.Tool
	circuit  *mcp.CircuitBreaker
	journal  *goaktactor.PID
	logger   goaktlog.Logger
	self     *goaktactor.PID
	draining bool
	sessions map[string]sessionRegistration
}

var _ goaktactor.Actor = (*toolSupervisor)(nil)

// NewToolSupervisor creates a ToolSupervisor Actor instance. Tool config is
// resolved from the Registrar at PostStart. Exported for cluster kind
// registration: remote placement (SpawnOn) and relocation both recreate the
// actor from its registered kind on the target node, so every node must know
// how to construct one.
func NewToolSupervisor() goaktactor.Actor { return &toolSupervisor{} }

// PreStart initializes the logger, circuit breaker, and session map. Tool
// config is resolved later in PostStart once the actor is fully registered
// in the actor system; a CircuitConfigExtension override replaces the
// breaker there.
func (x *toolSupervisor) PreStart(ctx *goaktactor.Context) error {
	x.logger = ctx.Logger()
	x.circuit = mcp.NewCircuitBreaker(mcp.CircuitConfig{})
	x.sessions = make(map[string]sessionRegistration)
	return nil
}

// Receive handles messages delivered to ToolSupervisor.
func (x *toolSupervisor) Receive(ctx *goaktactor.ReceiveContext) {
	switch msg := ctx.Message().(type) {
	case *goaktactor.PostStart:
		x.self = ctx.Self()

		// Resolve the journal by name. PostStart runs once the actor is registered
		// in the system, so ActorOf reliably finds sibling actors. Every node runs
		// its own journal, so this resolves locally wherever the supervisor lands.
		pid, err := ctx.ActorSystem().ActorOf(ctx.Context(), naming.ActorNameJournal)
		if err != nil {
			x.logger.Warnf("actor supervisor failed to resolve journal: %v", err)
			ctx.Err(err)
			return
		}
		x.journal = pid

		// Pull the tool config from the Registrar, the cluster-wide source of
		// truth. The registrar writes the tool into its catalog before spawning
		// the supervisor, so the entry exists by the time PostStart runs — and a
		// relocated supervisor (whose host node died) resumes with the current
		// config instead of a spawn-time snapshot. The tool ID is derived from
		// the actor name.
		toolID := naming.ToolIDFromSupervisorName(ctx.Self().Name())
		tool, err := x.fetchToolFromRegistrar(ctx, toolID)
		if err != nil {
			x.logger.Warnf("actor supervisor:%s tool config resolution failed: %v", toolID, err)
			ctx.Err(err)
			return
		}

		x.tool = tool

		// Optionally override circuit config from the system-level extension.
		// Used in tests to reduce OpenDuration without per-actor injection.
		if circuitExt, ok := ctx.Extension(actorextension.CircuitConfigExtensionID).(*actorextension.CircuitConfigExtension); ok && circuitExt != nil {
			x.circuit = mcp.NewCircuitBreaker(circuitExt.Config())
		}

		x.logger.Infof("actor supervisor:%s started circuit=closed", x.tool.ID)
	case *runtime.CanAcceptWork:
		x.handleCanAcceptWork(ctx, msg)
	case *runtime.ReportFailure:
		x.handleReportFailure(ctx, msg)
	case *runtime.ReportSuccess:
		x.handleReportSuccess(ctx, msg)
	case *runtime.ReleaseWork:
		x.handleReleaseWork(msg)
	case *runtime.SessionActivated:
		x.handleSessionActivated(msg)
	case *runtime.SessionDeactivated:
		x.handleSessionDeactivated(msg)
	case *runtime.RefreshToolConfig:
		x.handleRefreshToolConfig(ctx, msg)
	case *runtime.GetToolStatus:
		x.handleGetToolStatus(ctx, msg)
	case *runtime.ResetCircuit:
		x.handleResetCircuit(ctx, msg)
	case *runtime.DrainTool:
		x.handleDrainTool(ctx, msg)
	default:
		ctx.Unhandled()
	}
}

// PostStop performs cleanup after ToolSupervisor has stopped.
// It derives the tool ID from the actor name instead of reading x.tool,
// because PostStop runs in a different goroutine than Receive.
func (x *toolSupervisor) PostStop(ctx *goaktactor.Context) error {
	toolID := naming.ToolIDFromSupervisorName(ctx.ActorName())
	if toolID.IsZero() {
		x.logger.Infof("actor supervisor stopped before tool resolved")
	} else {
		x.logger.Infof("actor supervisor:%s stopped", toolID)
	}
	return nil
}

// handleCanAcceptWork checks whether this supervisor can accept new work by
// evaluating tool disable status, drain flag, per-tool backpressure
// (MaxSessionsPerTool), circuit state, and half-open probe limits. Responds
// with CanAcceptWorkResult indicating accept/reject, the reason, the current
// session count, and the circuit generation at admission time. SessionCount
// is always populated so the caller can use it for further decisions (e.g.
// backpressure) without a separate round-trip.
//
// Probe requests never reserve a half-open probe slot: health checks report
// availability, they do not commit to executing a backend call, and a
// reserved slot with no forthcoming outcome would wedge the circuit in
// half-open until a manual reset. For the same reason the backpressure check
// runs before Acquire, so a backpressure rejection cannot strand a slot.
func (x *toolSupervisor) handleCanAcceptWork(ctx *goaktactor.ReceiveContext, msg *runtime.CanAcceptWork) {
	sessionCount := len(x.sessions)

	// A mismatch is the one response that omits Tool: this supervisor does not
	// own the requested tool, so it has no authoritative definition to return.
	if msg.ToolID != x.tool.ID {
		ctx.Response(&runtime.CanAcceptWorkResult{Accept: false, Reason: canAcceptReasonToolMismatch, SessionCount: sessionCount})
		return
	}

	// Every path below carries the authoritative tool config so the router can
	// classify a rejection and drive the rest of the pipeline without asking
	// the registrar.
	if x.tool.State == mcp.ToolStateDisabled {
		ctx.Response(&runtime.CanAcceptWorkResult{Accept: false, Reason: canAcceptReasonToolDisabled, SessionCount: sessionCount, Tool: x.tool})
		return
	}

	if x.draining {
		ctx.Response(&runtime.CanAcceptWorkResult{Accept: false, Reason: canAcceptReasonToolDraining, SessionCount: sessionCount, Tool: x.tool})
		return
	}

	if x.tool.MaxSessionsPerTool > 0 && sessionCount >= x.tool.MaxSessionsPerTool {
		ctx.Response(&runtime.CanAcceptWorkResult{Accept: false, Reason: canAcceptReasonBackpressureCap, SessionCount: sessionCount, Tool: x.tool})
		return
	}

	if msg.Probe {
		state, transition := x.circuit.Peek()
		x.emitTransition(transition)
		switch state {
		case mcp.CircuitOpen:
			ctx.Response(&runtime.CanAcceptWorkResult{Accept: false, Reason: canAcceptReasonCircuitOpen, SessionCount: sessionCount, Tool: x.tool})
		case mcp.CircuitHalfOpen:
			ctx.Response(&runtime.CanAcceptWorkResult{Accept: false, Reason: canAcceptReasonCircuitHalfOpen, SessionCount: sessionCount, Tool: x.tool})
		default:
			ctx.Response(&runtime.CanAcceptWorkResult{Accept: true, SessionCount: sessionCount, Tool: x.tool})
		}
		return
	}

	outcome, transition := x.circuit.Acquire()
	x.emitTransition(transition)

	switch outcome {
	case mcp.CircuitAcquireRejectedOpen:
		ctx.Response(&runtime.CanAcceptWorkResult{Accept: false, Reason: canAcceptReasonCircuitOpen, SessionCount: sessionCount, Tool: x.tool})
		return
	case mcp.CircuitAcquireRejectedHalfOpenLimit:
		ctx.Response(&runtime.CanAcceptWorkResult{Accept: false, Reason: canAcceptReasonHalfOpenLimit, SessionCount: sessionCount, Tool: x.tool})
		return
	}

	ctx.Response(&runtime.CanAcceptWorkResult{Accept: true, SessionCount: sessionCount, CircuitGeneration: x.circuit.Generation(), Tool: x.tool})
}

// fetchToolFromRegistrar resolves the Registrar by name and asks it for the
// authoritative tool definition. Called from PostStart on fresh spawn and on
// relocation. The Ask is bounded by the default request timeout; a registrar
// admin operation concurrently Asking this supervisor waits at most that long
// (both sides time out rather than deadlock).
func (x *toolSupervisor) fetchToolFromRegistrar(ctx *goaktactor.ReceiveContext, toolID mcp.ToolID) (mcp.Tool, error) {
	registrar, err := ctx.ActorSystem().ActorOf(ctx.Context(), naming.ActorNameRegistrar)
	if err != nil {
		return mcp.Tool{}, mcp.WrapRuntimeError(mcp.ErrCodeInternal, "registrar not resolvable", err)
	}

	resp, err := goaktactor.Ask(ctx.Context(), registrar, &runtime.QueryTool{ToolID: toolID}, mcp.DefaultRequestTimeout)
	if err != nil {
		return mcp.Tool{}, mcp.WrapRuntimeError(mcp.ErrCodeInternal, "tool config query failed", err)
	}

	result, ok := resp.(*runtime.QueryToolResult)
	if !ok || !result.Found || result.Tool == nil {
		return mcp.Tool{}, mcp.NewRuntimeError(mcp.ErrCodeInternal, "tool config not found in registrar")
	}
	return *result.Tool, nil
}

// sessionIdleTimeout returns the tool's configured idle timeout or the default
// when unset. This controls how long a session can remain idle before passivation.
func sessionIdleTimeout(tool mcp.Tool) time.Duration {
	if tool.IdleTimeout > 0 {
		return tool.IdleTimeout
	}
	return mcp.DefaultSessionIdleTimeout
}

// handleReportFailure records a failure on the circuit breaker. A returned
// transition (Closed→Open threshold exceeded, or HalfOpen→Open probe failed)
// is emitted as an audit and telemetry event. Outcomes carrying a stale
// CircuitGeneration (admitted before the circuit last transitioned) are
// discarded by the breaker.
func (x *toolSupervisor) handleReportFailure(_ *goaktactor.ReceiveContext, msg *runtime.ReportFailure) {
	if msg.ToolID != x.tool.ID {
		return
	}

	transition := x.circuit.OnFailureAt(msg.CircuitGeneration)
	x.logger.Debugf("actor supervisor:%s failure count=%d circuit=%s", x.tool.ID, x.circuit.FailureCount(), x.circuit.State())
	x.emitTransition(transition)
}

// handleReportSuccess records a success on the circuit breaker. A returned
// transition (HalfOpen→Closed probe success) is emitted as an audit and
// telemetry event. Outcomes carrying a stale CircuitGeneration (admitted
// before the circuit last transitioned) are discarded by the breaker.
func (x *toolSupervisor) handleReportSuccess(_ *goaktactor.ReceiveContext, msg *runtime.ReportSuccess) {
	if msg.ToolID != x.tool.ID {
		return
	}

	transition := x.circuit.OnSuccessAt(msg.CircuitGeneration)
	x.emitTransition(transition)
}

// handleReleaseWork returns a half-open probe slot reserved at admission
// when the invocation never reached the backend (a post-admission pipeline
// stage failed). Without this, an aborted invocation would leave the slot
// reserved forever and wedge the circuit in half-open.
func (x *toolSupervisor) handleReleaseWork(msg *runtime.ReleaseWork) {
	if msg.ToolID != x.tool.ID {
		return
	}
	x.circuit.Release()
}

// handleSessionActivated records a session grain's activation so the
// supervisor's per-tool session count reflects reality for backpressure
// and admin reporting, and forwards the message to the registrar's
// aggregate session view. Messages for other tools are ignored defensively.
func (x *toolSupervisor) handleSessionActivated(msg *runtime.SessionActivated) {
	if msg.ToolID != x.tool.ID {
		return
	}

	name := naming.SessionName(msg.TenantID, msg.ClientID, msg.ToolID)
	x.sessions[name] = sessionRegistration{TenantID: msg.TenantID, ClientID: msg.ClientID}
	x.forwardToRegistrar(msg)
}

// handleSessionDeactivated drops a session from the supervisor's map when
// the grain engine passivates or deactivates it, and forwards the message
// to the registrar's aggregate session view.
func (x *toolSupervisor) handleSessionDeactivated(msg *runtime.SessionDeactivated) {
	if msg.ToolID != x.tool.ID {
		return
	}

	name := naming.SessionName(msg.TenantID, msg.ClientID, msg.ToolID)
	delete(x.sessions, name)
	x.forwardToRegistrar(msg)
}

// forwardToRegistrar relays a session lifecycle message to the registrar so
// its aggregate session view (tenant counts, session enumeration) stays
// current without Ask fan-outs. The registrar is re-resolved by name on every
// call because it is a cluster singleton that can relocate; caching its PID
// would leak a stale reference. Fire-and-forget: a missed forward degrades
// a quota count, never correctness of routing.
func (x *toolSupervisor) forwardToRegistrar(msg any) {
	if x.self == nil {
		return
	}

	registrar, err := x.self.ActorSystem().ActorOf(context.Background(), naming.ActorNameRegistrar)
	if err != nil || registrar == nil {
		return
	}
	_ = goaktactor.Tell(context.Background(), registrar, msg)
}

// handleRefreshToolConfig installs the updated tool definition carried on the
// message. Sent by the Registrar after an update, enable, disable, or health
// transition; the definition rides inline because the registrar and this
// supervisor may live on different nodes.
func (x *toolSupervisor) handleRefreshToolConfig(_ *goaktactor.ReceiveContext, msg *runtime.RefreshToolConfig) {
	if msg.Tool.ID != x.tool.ID {
		return
	}
	x.tool = msg.Tool
	if msg.Tool.State == mcp.ToolStateEnabled {
		x.draining = false
	}
	x.logger.Infof("actor supervisor:%s refreshed tool config state=%s draining=%v", msg.Tool.ID, msg.Tool.State, x.draining)
}

// handleGetToolStatus returns the current operational status of the tool:
// state, circuit breaker state, active session count, and drain flag. Calling
// Peek applies any pending Open→HalfOpen transition so the reported circuit
// state is time-accurate.
func (x *toolSupervisor) handleGetToolStatus(ctx *goaktactor.ReceiveContext, msg *runtime.GetToolStatus) {
	if msg.ToolID != x.tool.ID {
		ctx.Response(&runtime.GetToolStatusResult{Err: mcp.NewRuntimeError(mcp.ErrCodeInvalidRequest, "tool ID mismatch")})
		return
	}
	state, transition := x.circuit.Peek()
	x.emitTransition(transition)
	ctx.Response(&runtime.GetToolStatusResult{
		Status: mcp.ToolStatus{
			ToolID:       x.tool.ID,
			State:        x.tool.State,
			Circuit:      state,
			SessionCount: len(x.sessions),
			Draining:     x.draining,
		},
	})
}

// handleResetCircuit manually resets the circuit breaker to closed state,
// clearing the failure counter and half-open probe count.
func (x *toolSupervisor) handleResetCircuit(ctx *goaktactor.ReceiveContext, msg *runtime.ResetCircuit) {
	if msg.ToolID != x.tool.ID {
		ctx.Response(&runtime.ResetCircuitResult{Err: mcp.NewRuntimeError(mcp.ErrCodeInvalidRequest, "tool ID mismatch")})
		return
	}
	transition := x.circuit.Reset()
	if transition != nil {
		x.logger.Infof("actor supervisor:%s circuit reset to closed (was %s)", x.tool.ID, transition.From)
	}
	x.emitTransition(transition)
	ctx.Response(&runtime.ResetCircuitResult{})
}

// handleDrainTool sets the draining flag so new session requests are rejected.
// Existing sessions continue until they passivate or complete.
func (x *toolSupervisor) handleDrainTool(ctx *goaktactor.ReceiveContext, msg *runtime.DrainTool) {
	if msg.ToolID != x.tool.ID {
		ctx.Response(&runtime.DrainToolResult{Err: mcp.NewRuntimeError(mcp.ErrCodeInvalidRequest, "tool ID mismatch")})
		return
	}
	x.draining = true
	x.logger.Infof("actor supervisor:%s draining: no new sessions accepted", x.tool.ID)
	ctx.Response(&runtime.DrainToolResult{})
}

// emitTransition records a circuit state change to the audit journal and the
// OTel CircuitState metric. A nil transition is a no-op, so callers do not
// need to gate their own calls.
func (x *toolSupervisor) emitTransition(transition *mcp.CircuitTransition) {
	if transition == nil {
		return
	}
	x.logger.Infof("actor supervisor:%s circuit %s→%s (%s)", x.tool.ID, transition.From, transition.To, transition.Reason)

	telemetry.RecordCircuitState(context.Background(), x.tool.ID, string(transition.To))

	if x.journal == nil || !x.journal.IsRunning() {
		return
	}
	meta := circuitTransitionMetadata(transition)
	ev := audit.CircuitStateChangeAuditEvent(string(x.tool.ID), string(transition.To), meta)
	_ = goaktactor.Tell(context.Background(), x.journal, &runtime.RecordAuditEvent{Event: ev})
}

// circuitTransitionMetadata renders a CircuitTransition into the string map
// that AuditEvent.Metadata expects. MetadataKeyReason carries the transition
// reason; MetadataKeyFailureCount is included for threshold-exceeded
// transitions so operators can see how many failures tripped the breaker.
func circuitTransitionMetadata(transition *mcp.CircuitTransition) map[string]string {
	meta := map[string]string{audit.MetadataKeyReason: string(transition.Reason)}
	if transition.Reason == mcp.CircuitReasonThresholdExceeded {
		meta[audit.MetadataKeyFailureCount] = strconv.Itoa(transition.FailureCount)
	}
	return meta
}
