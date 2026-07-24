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
	"errors"
	"time"

	goaktactor "github.com/tochemey/goakt/v4/actor"
	goaktlog "github.com/tochemey/goakt/v4/log"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/tochemey/portcullis/mcp"

	"github.com/tochemey/portcullis/internal/naming"
	"github.com/tochemey/portcullis/internal/runtime"
	"github.com/tochemey/portcullis/internal/runtime/actor/extension"
	"github.com/tochemey/portcullis/internal/runtime/policy"
	"github.com/tochemey/portcullis/internal/runtime/telemetry"
)

// defaultCheckAcceptWorkReason is returned as the rejection reason when the
// supervisor cannot accept work but did not populate an explicit reason
// string. Kept as a stable constant so audit records and runtime errors do
// not drift between code paths that construct the same failure.
const defaultCheckAcceptWorkReason = "circuit open or tool unavailable"

// router is the RouterActor.
//
// RouterActor is the runtime entry point for tool invocations. It performs the
// routing path: tool lookup, supervisor availability check, session resolution,
// and execution. Routing decisions are deterministic and tenant-aware.
//
// Spawn: GatewayManager spawns a pool of routers in spawnFoundationalActors
// via ctx.Spawn(naming.RouterName(i), newRouterActor()) as children of
// GatewayManager; the Gateway facade distributes invocations across the pool
// round-robin. Dependencies are resolved by name at PostStart.
//
// Relocation: No. RouterActor runs on the local node as a child of GatewayManager
// and does not relocate in cluster mode.
//
// RoutingSticky and RoutingLeastLoaded currently share the same session
// resolution (a deterministic grain identity per tenant+client+tool);
// health-aware exclusions are applied via CanAcceptWork. True least-loaded
// selection would require choosing between multiple candidate sessions per
// tool, which the single-grain-per-triple model does not have.
//
// All fields are unexported to enforce actor immutability rules.
type router struct {
	registrar            *goaktactor.PID
	policyMaker          *goaktactor.PID
	credentialBroker     *goaktactor.PID
	journaler            *goaktactor.PID
	hasConcurrencyQuotas bool
	requestTimeout       time.Duration
	logger               goaktlog.Logger
	// nodeID is this node's "host:port" identity, resolved once at PostStart.
	// It is stamped on streaming requests so the session grain can detect a
	// cross-node caller; the value is immutable for the actor system's
	// lifetime, so it is cached here rather than recomputed per request.
	nodeID string
}

var _ goaktactor.Actor = (*router)(nil)

// newRouterActor creates a RouterActor.
func newRouterActor() *router {
	return &router{}
}

// PreStart validates the registrar and initializes the logger.
func (x *router) PreStart(ctx *goaktactor.Context) error {
	x.logger = ctx.Logger()
	config := ctx.Extension(extension.ConfigExtensionID).(*extension.ConfigExtension).Config()
	x.hasConcurrencyQuotas = hasConcurrencyQuotas(config)
	x.requestTimeout = config.Runtime.RequestTimeout
	if x.requestTimeout == 0 {
		x.requestTimeout = mcp.DefaultRequestTimeout
	}
	x.logger.Infof("actor=%s started", naming.ActorNameRouter)
	return nil
}

// Receive handles messages delivered to RouterActor.
func (x *router) Receive(ctx *goaktactor.ReceiveContext) {
	switch msg := ctx.Message().(type) {
	case *goaktactor.PostStart:
		x.logger.Debugf("actor=%s post-start", naming.ActorNameRouter)
		x.resolveActors(ctx)
	case *runtime.RouteInvocation:
		x.handleRouteInvocation(ctx, msg)
	case *runtime.RouteInvokeStream:
		x.handleRouteInvokeStream(ctx, msg)
	default:
		ctx.Unhandled()
	}
}

// PostStop performs cleanup after RouterActor has stopped.
func (x *router) PostStop(ctx *goaktactor.Context) error {
	x.logger.Infof("actor=%s stopped", naming.ActorNameRouter)
	return nil
}

// resolveActors resolves the journal and registrar actors.
// It is called in PreStart to ensure the actors are available when the health checker starts.
func (x *router) resolveActors(ctx *goaktactor.ReceiveContext) {
	actorSystem := ctx.ActorSystem()
	goCtx := ctx.Context()

	// resolve the journal actor
	journaler, err := actorSystem.ActorOf(goCtx, naming.ActorNameJournal)
	if err != nil {
		ctx.Err(err)
		return
	}

	// resolve the registrar actor
	registrar, err := actorSystem.ActorOf(goCtx, naming.ActorNameRegistrar)
	if err != nil {
		ctx.Err(err)
		return
	}

	// resolve the policy maker actor
	policyMaker, err := actorSystem.ActorOf(goCtx, naming.ActorNamePolicy)
	if err != nil {
		ctx.Err(err)
		return
	}

	// resolve the credential broker actor
	credentialBroker, err := actorSystem.ActorOf(goCtx, naming.ActorNameCredentialBroker)
	if err != nil {
		ctx.Err(err)
		return
	}

	// let us set the various required actors
	x.journaler = journaler
	x.registrar = registrar
	x.policyMaker = policyMaker
	x.credentialBroker = credentialBroker
	x.nodeID = localNodeID(actorSystem)
}

// handleRouteInvocation orchestrates the synchronous routing chain. The
// pre-execution pipeline (validate → lookup → policy → supervisor → accept
// → credentials → session) is shared with handleRouteInvokeStream via
// runPreExecutionPipeline; only the terminal execution step differs.
func (x *router) handleRouteInvocation(ctx *goaktactor.ReceiveContext, msg *runtime.RouteInvocation) {
	if err := x.validateInvocation(msg); err != nil {
		if msg.Invocation != nil {
			auditCtx, auditCancel := context.WithTimeout(ctx.Context(), x.requestTimeout)
			x.emitValidationFailure(auditCtx, msg.Invocation, err)
			auditCancel()
		}
		ctx.Response(&runtime.RouteResult{Err: err})
		return
	}

	inv := msg.Invocation
	tenantID, clientID := x.resolveIdentity(inv)

	log := x.loggerWithCorrelation(inv)
	log.Debugf("actor=%s routing tool=%s", naming.ActorNameRouter, inv.ToolID)
	start := time.Now()

	goCtx, cancel := context.WithTimeout(ctx.Context(), x.requestTimeout)
	defer cancel()

	// routeErr tracks the terminal error for span recording. The deferred
	// span closure always captures the actual failure regardless of which
	// pipeline stage errored out.
	var routeErr error
	if telemetry.TracingEnabled() {
		var span trace.Span
		goCtx, span = x.startRouteSpan(goCtx, "portcullis.route.invoke", inv, tenantID, clientID)
		defer func() {
			if routeErr != nil {
				span.SetStatus(codes.Error, routeErr.Error())
				span.RecordError(routeErr)
			}
			span.End()
		}()
	}

	rctx, failure := x.runPreExecutionPipeline(goCtx, ctx.ActorSystem(), inv, tenantID, clientID)
	if failure != nil {
		routeErr = failure.Err
		x.emitRouteFailure(goCtx, inv, tenantID, failure)
		ctx.Response(&runtime.RouteResult{Err: failure.Err})
		return
	}

	result, err := x.executeInvocation(goCtx, ctx.ActorSystem(), rctx)
	if err != nil {
		routeErr = err
		x.emitExecutionFailure(goCtx, inv, tenantID, err)
		ctx.Response(&runtime.RouteResult{Err: err})
		return
	}

	x.emitInvocationComplete(goCtx, inv, tenantID, string(result.Status), float64(time.Since(start).Milliseconds()))
	ctx.Response(&runtime.RouteResult{Result: result})
}

// handleRouteInvokeStream orchestrates the streaming variant of the routing
// chain. Pre-execution stages are shared with handleRouteInvocation via
// runPreExecutionPipeline; only the terminal executeInvocationStream step
// differs so progress events flow back to the caller.
func (x *router) handleRouteInvokeStream(ctx *goaktactor.ReceiveContext, msg *runtime.RouteInvokeStream) {
	if err := x.validateInvokeStream(msg); err != nil {
		if msg.Invocation != nil {
			auditCtx, auditCancel := context.WithTimeout(ctx.Context(), x.requestTimeout)
			x.emitValidationFailure(auditCtx, msg.Invocation, err)
			auditCancel()
		}
		ctx.Response(&runtime.RouteStreamResult{Err: err})
		return
	}

	inv := msg.Invocation
	tenantID, clientID := x.resolveIdentity(inv)

	log := x.loggerWithCorrelation(inv)
	log.Debugf("actor=%s routing-stream tool=%s", naming.ActorNameRouter, inv.ToolID)
	start := time.Now()

	goCtx, cancel := context.WithTimeout(ctx.Context(), x.requestTimeout)
	defer cancel()

	var routeErr error
	if telemetry.TracingEnabled() {
		var span trace.Span
		goCtx, span = x.startRouteSpan(goCtx, "portcullis.route.invoke_stream", inv, tenantID, clientID)
		defer func() {
			if routeErr != nil {
				span.SetStatus(codes.Error, routeErr.Error())
				span.RecordError(routeErr)
			}
			span.End()
		}()
	}

	rctx, failure := x.runPreExecutionPipeline(goCtx, ctx.ActorSystem(), inv, tenantID, clientID)
	if failure != nil {
		routeErr = failure.Err
		x.emitRouteFailure(goCtx, inv, tenantID, failure)
		ctx.Response(&runtime.RouteStreamResult{Err: failure.Err})
		return
	}

	streamResult, result, err := x.executeInvocationStream(goCtx, ctx.ActorSystem(), rctx)
	if err != nil {
		routeErr = err
		x.emitExecutionFailure(goCtx, inv, tenantID, err)
		ctx.Response(&runtime.RouteStreamResult{Err: err})
		return
	}

	x.emitInvocationComplete(goCtx, inv, tenantID, routeOutcomeInvocationStreaming, float64(time.Since(start).Milliseconds()))
	ctx.Response(&runtime.RouteStreamResult{StreamResult: streamResult, Result: result})
}

// validateInvokeStream rejects nil invocations and invocations without a tool ID.
func (x *router) validateInvokeStream(msg *runtime.RouteInvokeStream) error {
	if msg.Invocation == nil {
		return mcp.NewRuntimeError(mcp.ErrCodeInvalidRequest, "invocation is required")
	}

	if msg.Invocation.ToolID.IsZero() {
		return mcp.NewRuntimeError(mcp.ErrCodeInvalidRequest, "tool ID is required")
	}
	return nil
}

// executeInvocationStream forwards the invocation to the session grain via
// SessionInvokeStream. If the grain returns a StreamingResult, it is passed
// through. Otherwise the synchronous Result is returned.
//
// Transport-level failures release the circuit admission on the supervisor;
// see executeInvocation.
func (x *router) executeInvocationStream(goCtx context.Context, actorSystem goaktactor.ActorSystem, rctx *routeContext) (*mcp.StreamingResult, *mcp.ExecutionResult, error) {
	tool := rctx.Tool
	execTimeout := tool.RequestTimeout
	if execTimeout == 0 {
		execTimeout = mcp.DefaultRequestTimeout
	}

	// CallerNode lets the grain detect a cross-node request and fall back to
	// synchronous execution: a StreamingResult's channels cannot cross the wire.
	msg := &runtime.SessionInvokeStream{
		Invocation:        rctx.Invocation,
		CircuitGeneration: rctx.CircuitGeneration,
		CallerNode:        x.nodeID,
	}
	resp, err := actorSystem.AskGrain(goCtx, rctx.Session, msg, execTimeout)
	if err != nil {
		x.releaseAcceptedWork(goCtx, rctx.Supervisor, rctx.Invocation.ToolID)
		return nil, nil, mcp.WrapRuntimeError(mcp.ErrCodeInternal, "stream invocation failed", err)
	}

	result, ok := resp.(*runtime.SessionInvokeStreamResult)
	if !ok {
		x.releaseAcceptedWork(goCtx, rctx.Supervisor, rctx.Invocation.ToolID)
		return nil, nil, mcp.NewRuntimeError(mcp.ErrCodeInternal, "invalid session stream response")
	}

	if result.Err != nil {
		var rErr *mcp.RuntimeError
		if errors.As(result.Err, &rErr) {
			return nil, nil, result.Err
		}
		return nil, nil, mcp.WrapRuntimeError(mcp.ErrCodeInternal, "session stream error", result.Err)
	}

	return result.StreamResult, result.Result, nil
}

// validateInvocation rejects nil invocations and invocations without a tool ID.
func (x *router) validateInvocation(msg *runtime.RouteInvocation) error {
	if msg.Invocation == nil {
		return mcp.NewRuntimeError(mcp.ErrCodeInvalidRequest, "invocation is required")
	}

	if msg.Invocation.ToolID.IsZero() {
		return mcp.NewRuntimeError(mcp.ErrCodeInvalidRequest, "tool ID is required")
	}
	return nil
}

// resolveCredentials asks the CredentialBrokerActor for credentials when the
// tool has CredentialPolicyRequired. Returns the invocation unchanged when
// credentials are optional, or a copy with credentials attached on success.
func (x *router) resolveCredentials(goCtx context.Context, inv *mcp.Invocation, tool mcp.Tool, tenantID mcp.TenantID) (*mcp.Invocation, error) {
	if tool.CredentialPolicy != mcp.CredentialPolicyRequired {
		return inv, nil
	}

	if x.credentialBroker == nil || !x.credentialBroker.IsRunning() {
		return nil, mcp.NewRuntimeError(mcp.ErrCodeCredentialUnavailable, "credential broker not available")
	}

	resp, err := goaktactor.Ask(goCtx, x.credentialBroker, &runtime.ResolveRequest{
		TenantID: tenantID,
		ToolID:   inv.ToolID,
	}, x.requestTimeout)
	if err != nil {
		return nil, mcp.WrapRuntimeError(mcp.ErrCodeInternal, "credential resolution failed", err)
	}
	result, ok := resp.(*runtime.ResolveResult)
	if !ok || !result.Resolved() {
		if result != nil && result.Err != nil {
			return nil, result.Err
		}
		return nil, mcp.NewRuntimeError(mcp.ErrCodeCredentialUnavailable, "credentials not resolved")
	}

	invCopy := *inv
	invCopy.Credentials = result.Credentials.Values
	return &invCopy, nil
}

// evaluatePolicy asks the PolicyActor for an authorization decision. Returns nil
// when no PolicyActor is available (policy is optional) or when the request is
// allowed. Returns a RuntimeError with the appropriate denial code on reject.
// ActiveSessionCount is resolved from the registrar for ConcurrentSessions quota.
func (x *router) evaluatePolicy(goCtx context.Context, inv *mcp.Invocation, tool mcp.Tool, tenantID mcp.TenantID, clientID mcp.ClientID) error {
	if x.policyMaker == nil || !x.policyMaker.IsRunning() {
		return nil
	}

	activeSessions := 0
	if x.hasConcurrencyQuotas && x.registrar != nil && x.registrar.IsRunning() {
		if resp, err := goaktactor.Ask(goCtx, x.registrar, &runtime.CountSessionsForTenant{TenantID: tenantID}, x.requestTimeout); err == nil {
			if result, ok := resp.(*runtime.CountSessionsForTenantResult); ok {
				activeSessions = result.Count
			}
		}
	}

	in := &policy.Input{
		Invocation:         inv,
		Tool:               tool,
		TenantID:           tenantID,
		ClientID:           clientID,
		ActiveSessionCount: activeSessions,
		Scopes:             inv.Scopes,
	}

	policyStart := time.Now()
	resp, err := goaktactor.Ask(goCtx, x.policyMaker, &policy.EvaluateRequest{Input: in}, x.requestTimeout)
	policyLatencyMs := float64(time.Since(policyStart).Microseconds()) / 1000.0
	if err != nil {
		telemetry.RecordPolicyEvaluationLatency(goCtx, tenantID, "error", policyLatencyMs)
		return mcp.WrapRuntimeError(mcp.ErrCodeInternal, "policy evaluation failed", err)
	}

	result, ok := resp.(*policy.EvaluateResult)
	if !ok {
		telemetry.RecordPolicyEvaluationLatency(goCtx, tenantID, "error", policyLatencyMs)
		return mcp.NewRuntimeError(mcp.ErrCodeInternal, "invalid policy response type")
	}
	if !result.Result.Allowed() {
		decision := string(result.Result.Decision)
		if decision == "" {
			decision = "error"
		}
		telemetry.RecordPolicyEvaluationLatency(goCtx, tenantID, decision, policyLatencyMs)
		if result.Result.Err != nil {
			return result.Result.Err
		}
		return mcp.NewRuntimeError(mcp.ErrCodePolicyDenied, "policy evaluation failed")
	}
	telemetry.RecordPolicyEvaluationLatency(goCtx, tenantID, string(policy.DecisionAllow), policyLatencyMs)
	return nil
}

// lookupTool queries the RegistryActor for the tool definition. Returns
// ErrToolNotFound when the tool is not registered.
func (x *router) lookupTool(goCtx context.Context, toolID mcp.ToolID) (mcp.Tool, error) {
	qResp, err := goaktactor.Ask(goCtx, x.registrar, &runtime.QueryTool{ToolID: toolID}, x.requestTimeout)
	if err != nil {
		return mcp.Tool{}, mcp.WrapRuntimeError(mcp.ErrCodeInternal, "registry query failed", err)
	}

	qResult, ok := qResp.(*runtime.QueryToolResult)
	if !ok || !qResult.Found || qResult.Tool == nil {
		return mcp.Tool{}, mcp.ErrToolNotFound
	}

	return *qResult.Tool, nil
}

// lookupSupervisor resolves the tool's supervisor by its deterministic name.
// ActorOf is location-transparent: a supervisor placed (or relocated) on
// another node comes back as a remote PID and all messaging routes through
// remoting. Resolution is re-done per invocation so relocation never leaves
// the router holding a stale reference.
func (x *router) lookupSupervisor(goCtx context.Context, actorSystem goaktactor.ActorSystem, toolID mcp.ToolID) (*goaktactor.PID, error) {
	supervisor, err := actorSystem.ActorOf(goCtx, naming.ToolSupervisorName(toolID))
	if err != nil || supervisor == nil {
		return nil, mcp.NewRuntimeError(mcp.ErrCodeToolUnavailable, "supervisor not available")
	}
	return supervisor, nil
}

// checkAcceptWork asks the ToolSupervisorActor whether it can accept new work.
// The supervisor evaluates circuit state, tool availability, and backpressure
// (MaxSessionsPerTool) in a single Ask, and returns its authoritative tool
// definition on the result. On acceptance it returns that Tool plus the circuit
// admission generation; on rejection it returns the Tool (when the supervisor
// supplied it) and a typed RuntimeError (ToolDisabled, ToolUnavailable, or
// ConcurrencyLimitReached), classified from the returned Tool.
//
// Returning the Tool here is what keeps the singleton registrar off the hot
// path: routing gets admission and config in one round-trip to the per-tool
// supervisor (distributed), never a per-request QueryTool to the singleton.
//
// Acceptance may reserve a half-open circuit probe slot on the supervisor.
// Callers must guarantee a terminal signal: the session grain reports the
// execution outcome, and every post-admission failure path that prevents the
// backend call must send ReleaseWork (see releaseAcceptedWork).
func (x *router) checkAcceptWork(goCtx context.Context, supervisorPID *goaktactor.PID, toolID mcp.ToolID) (mcp.Tool, uint64, error) {
	acceptResp, err := goaktactor.Ask(goCtx, supervisorPID, &runtime.CanAcceptWork{ToolID: toolID}, x.requestTimeout)
	if err != nil {
		return mcp.Tool{}, 0, mcp.WrapRuntimeError(mcp.ErrCodeInternal, "availability check failed", err)
	}

	acceptResult, ok := acceptResp.(*runtime.CanAcceptWorkResult)
	if !ok || !acceptResult.Accept {
		reason := defaultCheckAcceptWorkReason
		var tool mcp.Tool
		if acceptResult != nil {
			tool = acceptResult.Tool
			if acceptResult.Reason != "" {
				reason = acceptResult.Reason
			}
		}
		switch {
		case tool.State == mcp.ToolStateDisabled:
			return tool, 0, mcp.NewRuntimeError(mcp.ErrCodeToolDisabled, reason)
		case tool.MaxSessionsPerTool > 0 && acceptResult != nil && acceptResult.SessionCount >= tool.MaxSessionsPerTool:
			return tool, 0, mcp.NewRuntimeError(mcp.ErrCodeConcurrencyLimitReached, reason)
		default:
			return tool, 0, mcp.NewRuntimeError(mcp.ErrCodeToolUnavailable, reason)
		}
	}
	return acceptResult.Tool, acceptResult.CircuitGeneration, nil
}

// releaseAcceptedWork returns the admission reserved by checkAcceptWork when
// a later pipeline stage failed before the invocation reached the session
// grain. Without this, a failure between admission and execution would leave
// a half-open circuit probe slot reserved forever, wedging the tool until a
// manual circuit reset. Delivered via Tell; errors are swallowed because the
// supervisor may have stopped, in which case its circuit state dies with it.
func (x *router) releaseAcceptedWork(goCtx context.Context, supervisorPID *goaktactor.PID, toolID mcp.ToolID) {
	if supervisorPID == nil || !supervisorPID.IsRunning() {
		return
	}
	_ = goaktactor.Tell(goCtx, supervisorPID, &runtime.ReleaseWork{ToolID: toolID})
}

// resolveSession activates (or resolves) the session grain for the given
// tenant+client+tool triple directly through the grain engine and returns the
// grain identity. The grain engine owns cluster-wide placement and the
// single-activation guarantee, so no supervisor round-trip is needed — a
// grain identity is location-transparent and AskGrain routes to whichever
// node hosts (or re-activates) the grain.
//
// The SessionDependency passed through [goaktactor.WithGrainDependencies]
// carries only the identity, tool, and credentials; the grain itself builds
// the executor via the ExecutorFactoryExtension on first activation.
// Deliberately NOT pre-creating the executor here prevents a resource leak:
// goakt's grain engine always invokes the factory options on every
// GrainOf call, but only runs OnActivate on the first activation of a given
// name. Any executor attached to a repeat call's dependencies would be
// ignored by the already-active grain and never closed.
func (x *router) resolveSession(goCtx context.Context, actorSystem goaktactor.ActorSystem, tenantID mcp.TenantID, clientID mcp.ClientID, tool mcp.Tool, creds map[string]string) (*goaktactor.GrainIdentity, error) {
	sessDep := extension.NewSessionDependency(tenantID, clientID, tool.ID, tool, creds)
	name := naming.SessionName(tenantID, clientID, tool.ID)

	identity, err := goaktactor.GrainOf[*sessionGrain](
		goCtx,
		actorSystem,
		name,
		goaktactor.WithGrainDependencies(sessDep),
		goaktactor.WithGrainDeactivateAfter(sessionIdleTimeout(tool)),
	)
	if err != nil {
		return nil, mcp.WrapRuntimeError(mcp.ErrCodeInternal, "failed to activate session grain", err)
	}
	return identity, nil
}

// executeInvocation forwards the invocation to the session grain via
// AskGrain and waits for the execution result. Uses the tool's
// RequestTimeout or the default.
//
// A transport-level failure (AskGrain error or malformed response) releases
// the circuit admission on the supervisor: the grain may never have received
// the message, so no outcome report can be relied upon to free a reserved
// half-open probe slot. Grain-reported errors are not released — the grain
// has already reported the outcome to the supervisor itself.
func (x *router) executeInvocation(goCtx context.Context, actorSystem goaktactor.ActorSystem, rctx *routeContext) (*mcp.ExecutionResult, error) {
	tool := rctx.Tool
	execTimeout := tool.RequestTimeout
	if execTimeout == 0 {
		execTimeout = mcp.DefaultRequestTimeout
	}

	msg := &runtime.SessionInvoke{Invocation: rctx.Invocation, CircuitGeneration: rctx.CircuitGeneration}
	sessInvResp, err := actorSystem.AskGrain(goCtx, rctx.Session, msg, execTimeout)
	if err != nil {
		x.releaseAcceptedWork(goCtx, rctx.Supervisor, rctx.Invocation.ToolID)
		return nil, mcp.WrapRuntimeError(mcp.ErrCodeInternal, "invocation failed", err)
	}

	sessInvResult, ok := sessInvResp.(*runtime.SessionInvokeResult)
	if !ok {
		x.releaseAcceptedWork(goCtx, rctx.Supervisor, rctx.Invocation.ToolID)
		return nil, mcp.NewRuntimeError(mcp.ErrCodeInternal, "invalid session response")
	}

	if sessInvResult.Err != nil {
		var rErr *mcp.RuntimeError
		if errors.As(sessInvResult.Err, &rErr) {
			return nil, sessInvResult.Err
		}
		return nil, mcp.WrapRuntimeError(mcp.ErrCodeInternal, "session error", sessInvResult.Err)
	}
	return sessInvResult.Result, nil
}

// recordAuditEvent sends an audit event to the JournalActor via Tell (fire-and-forget).
// No-op when the journal is not available.
func (x *router) recordAuditEvent(ctx context.Context, event *mcp.AuditEvent) error {
	if x.journaler == nil || !x.journaler.IsRunning() {
		return nil
	}
	return goaktactor.Tell(ctx, x.journaler, &runtime.RecordAuditEvent{Event: event})
}

// logAuditError logs an audit event recording error at warn level.
// No-op when err is nil.
func (x *router) logAuditError(err error) {
	if err != nil {
		x.logger.Warnf("actor=%s audit event failed: %v", naming.ActorNameRouter, err)
	}
}

// invocationEvent constructs an audit Event from invocation context. Returns nil
// when the invocation itself is nil (defensive guard for early-validation failures).
func invocationEvent(inv *mcp.Invocation, evType mcp.AuditEventType, outcome, errorCode, message string) *mcp.AuditEvent {
	if inv == nil {
		return nil
	}
	return &mcp.AuditEvent{
		Type:      evType,
		Timestamp: time.Now(),
		TenantID:  string(inv.Correlation.TenantID),
		ClientID:  string(inv.Correlation.ClientID),
		ToolID:    string(inv.ToolID),
		RequestID: string(inv.Correlation.RequestID),
		TraceID:   string(inv.Correlation.TraceID),
		Outcome:   outcome,
		ErrorCode: errorCode,
		Message:   message,
	}
}

// outcomeFromError maps a RuntimeError to an audit outcome string. Policy denials
// produce "deny", rate/quota/concurrency limits produce "throttle", and all other
// errors produce "error".
func outcomeFromError(err error) string {
	if err == nil {
		return ""
	}
	var re *mcp.RuntimeError
	if errors.As(err, &re) {
		switch re.Code {
		case mcp.ErrCodePolicyDenied:
			return "deny"
		case mcp.ErrCodeRateLimited, mcp.ErrCodeQuotaExceeded, mcp.ErrCodeConcurrencyLimitReached:
			return "throttle"
		}
	}
	return "error"
}
