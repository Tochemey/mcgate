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

	"github.com/tochemey/goakt-mcp/mcp"

	"github.com/tochemey/goakt-mcp/internal/naming"
	"github.com/tochemey/goakt-mcp/internal/runtime"
	actorextension "github.com/tochemey/goakt-mcp/internal/runtime/actor/extension"
	"github.com/tochemey/goakt-mcp/internal/runtime/audit"
	"github.com/tochemey/goakt-mcp/internal/runtime/telemetry"
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
// Spawn: Registrar spawns ToolSupervisor in spawnSupervisor via
// ctx.Spawn(naming.ToolSupervisorName(tool.ID), newToolSupervisor()) as a child of Registrar.
// One supervisor per registered tool; spawned on RegisterTool or BootstrapTools.
//
// Relocation: Follows Registrar. In single-node mode, runs on the local node.
// In cluster mode, Registrar is a cluster singleton, so ToolSupervisor runs on
// whichever node hosts the Registrar singleton. If the Registrar relocates, its
// children (including all supervisors) are recreated on the new node.
//
// The tool definition is resolved from the ToolConfigExtension system extension in
// PostStart, using the tool ID derived from the actor name. Circuit parameters use
// runtime defaults unless overridden by a CircuitConfigExtension system extension.
//
// Sessions are goakt virtual actors (grains), not child actors. The
// supervisor owns a name-keyed map of [sessionRegistration] that lifecycle
// messages ([runtime.SessionActivated] / [runtime.SessionDeactivated])
// keep up to date. The supervisor never spawns sessions directly: it
// activates grain identities via ActorSystem.GrainIdentity and returns the
// identity to the caller.
//
// All fields are unexported to enforce actor immutability rules.
type toolSupervisor struct {
	tool      mcp.Tool
	circuit   *mcp.CircuitBreaker
	journal   *goaktactor.PID
	registrar *goaktactor.PID
	logger    goaktlog.Logger
	self      *goaktactor.PID
	draining  bool
	sessions  map[string]sessionRegistration
}

var _ goaktactor.Actor = (*toolSupervisor)(nil)

// newToolSupervisor creates a ToolSupervisor Actor instance.
// Tool config is resolved from ToolConfigExtension at PostStart.
func newToolSupervisor() *toolSupervisor {
	return &toolSupervisor{}
}

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
		// in the system, so ActorOf reliably finds sibling actors.
		pid, err := ctx.ActorSystem().ActorOf(ctx.Context(), naming.ActorNameJournal)
		if err != nil {
			x.logger.Warnf("actor supervisor failed to resolve journal: %v", err)
			ctx.Err(err)
			return
		}
		x.journal = pid

		// Resolve the registrar so session lifecycle messages can be forwarded
		// to its aggregate session view. Best-effort: the supervisor works
		// without it, the registrar's counts just stay empty.
		if registrar, err := ctx.ActorSystem().ActorOf(ctx.Context(), naming.ActorNameRegistrar); err == nil {
			x.registrar = registrar
		}

		// Resolve tool config from the system-level ToolConfigExtension. The Registrar
		// registers the tool there before spawning the supervisor, so it is always
		// present at PostStart time. The tool ID is derived from the actor name.
		toolID := naming.ToolIDFromSupervisorName(ctx.Self().Name())
		toolExt, ok := ctx.Extension(actorextension.ToolConfigExtensionID).(*actorextension.ToolConfigExtension)
		if !ok || toolExt == nil {
			x.logger.Warnf("actor supervisor:%s tool config extension not found", toolID)
			ctx.Err(mcp.NewRuntimeError(mcp.ErrCodeInternal, "tool config extension not found"))
			return
		}
		tool, found := toolExt.Get(toolID)
		if !found {
			x.logger.Warnf("actor supervisor:%s tool not registered in extension", toolID)
			ctx.Err(mcp.NewRuntimeError(mcp.ErrCodeInternal, "tool config not found"))
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
	case *runtime.GetOrCreateSession:
		x.handleGetOrCreateSession(ctx, msg)
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

	if msg.ToolID != x.tool.ID {
		ctx.Response(&runtime.CanAcceptWorkResult{Accept: false, Reason: canAcceptReasonToolMismatch, SessionCount: sessionCount})
		return
	}

	if x.tool.State == mcp.ToolStateDisabled {
		ctx.Response(&runtime.CanAcceptWorkResult{Accept: false, Reason: canAcceptReasonToolDisabled, SessionCount: sessionCount})
		return
	}

	if x.draining {
		ctx.Response(&runtime.CanAcceptWorkResult{Accept: false, Reason: canAcceptReasonToolDraining, SessionCount: sessionCount})
		return
	}

	if x.tool.MaxSessionsPerTool > 0 && sessionCount >= x.tool.MaxSessionsPerTool {
		ctx.Response(&runtime.CanAcceptWorkResult{Accept: false, Reason: canAcceptReasonBackpressureCap, SessionCount: sessionCount})
		return
	}

	if msg.Probe {
		state, transition := x.circuit.Peek()
		x.emitTransition(transition)
		switch state {
		case mcp.CircuitOpen:
			ctx.Response(&runtime.CanAcceptWorkResult{Accept: false, Reason: canAcceptReasonCircuitOpen, SessionCount: sessionCount})
		case mcp.CircuitHalfOpen:
			ctx.Response(&runtime.CanAcceptWorkResult{Accept: false, Reason: canAcceptReasonCircuitHalfOpen, SessionCount: sessionCount})
		default:
			ctx.Response(&runtime.CanAcceptWorkResult{Accept: true, SessionCount: sessionCount})
		}
		return
	}

	outcome, transition := x.circuit.Acquire()
	x.emitTransition(transition)

	switch outcome {
	case mcp.CircuitAcquireRejectedOpen:
		ctx.Response(&runtime.CanAcceptWorkResult{Accept: false, Reason: canAcceptReasonCircuitOpen, SessionCount: sessionCount})
		return
	case mcp.CircuitAcquireRejectedHalfOpenLimit:
		ctx.Response(&runtime.CanAcceptWorkResult{Accept: false, Reason: canAcceptReasonHalfOpenLimit, SessionCount: sessionCount})
		return
	}

	ctx.Response(&runtime.CanAcceptWorkResult{Accept: true, SessionCount: sessionCount, CircuitGeneration: x.circuit.Generation()})
}

// handleGetOrCreateSession activates (or resolves) the session grain for
// the given tenant+client+tool triple and returns the grain identity. The
// SessionDependency passed through [goaktactor.WithGrainDependencies]
// carries only the identity, tool, and credentials; the grain itself
// builds the executor via the ExecutorFactoryExtension on first activation.
//
// Deliberately NOT pre-creating the executor here prevents a resource leak:
// goakt's grain engine always invokes the factory options on every
// GrainIdentity call, but only runs OnActivate on the first activation of
// a given name. Any executor attached to a repeat call's dependencies
// would be ignored by the already-active grain and never closed.
func (x *toolSupervisor) handleGetOrCreateSession(ctx *goaktactor.ReceiveContext, msg *runtime.GetOrCreateSession) {
	if msg.ToolID != x.tool.ID {
		ctx.Response(&runtime.GetOrCreateSessionResult{Err: mcp.NewRuntimeError(mcp.ErrCodeInvalidRequest, "tool ID mismatch")})
		return
	}

	sessDep := actorextension.NewSessionDependency(msg.TenantID, msg.ClientID, msg.ToolID, x.tool, msg.Credentials)
	name := naming.SessionName(msg.TenantID, msg.ClientID, msg.ToolID)

	identity, err := goaktactor.GrainOf[*sessionGrain](
		ctx.Context(),
		ctx.ActorSystem(),
		name,
		goaktactor.WithGrainDependencies(sessDep),
		goaktactor.WithGrainDeactivateAfter(sessionIdleTimeout(x.tool)),
	)
	if err != nil {
		x.logger.Warnf("actor supervisor:%s activate session grain: %v", x.tool.ID, err)
		ctx.Response(&runtime.GetOrCreateSessionResult{Err: mcp.WrapRuntimeError(mcp.ErrCodeInternal, "failed to activate session grain", err)})
		return
	}

	ctx.Response(&runtime.GetOrCreateSessionResult{Session: identity, Found: true})
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
// current without Ask fan-outs. Fire-and-forget: a missed forward degrades
// a quota count, never correctness of routing.
func (x *toolSupervisor) forwardToRegistrar(msg any) {
	if x.registrar == nil || !x.registrar.IsRunning() {
		return
	}
	_ = goaktactor.Tell(context.Background(), x.registrar, msg)
}

// handleRefreshToolConfig reloads the tool definition from the ToolConfigExtension.
// Called when the Registrar updates, enables, or disables the tool.
func (x *toolSupervisor) handleRefreshToolConfig(ctx *goaktactor.ReceiveContext, msg *runtime.RefreshToolConfig) {
	toolExt, ok := ctx.Extension(actorextension.ToolConfigExtensionID).(*actorextension.ToolConfigExtension)
	if !ok || toolExt == nil {
		return
	}
	tool, found := toolExt.Get(msg.ToolID)
	if !found {
		return
	}
	x.tool = tool
	if tool.State == mcp.ToolStateEnabled {
		x.draining = false
	}
	x.logger.Infof("actor supervisor:%s refreshed tool config state=%s draining=%v", msg.ToolID, tool.State, x.draining)
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
