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

	goaktactor "github.com/tochemey/goakt/v4/actor"
	goaktlog "github.com/tochemey/goakt/v4/log"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/tochemey/goakt-mcp/mcp"

	"github.com/tochemey/goakt-mcp/internal/runtime/telemetry"
)

// Audit-event outcome strings emitted by the router pipeline. These are
// consumed by dashboards and audit queries, so the values are stable and
// must not drift between call sites.
const (
	routeOutcomeInvalid             = "invalid"
	routeOutcomeError               = "error"
	routeOutcomeUnavailable         = "unavailable"
	routeOutcomeCredentialUnavail   = "credential_unavailable" //nolint:gosec // audit outcome label, not a credential
	routeOutcomeSessionError        = "session_error"
	routeOutcomeExecutionError      = "execution_error"
	routeOutcomeInvocationStreaming = "streaming"
)

// Default tenant and client IDs applied when the incoming invocation does not
// carry explicit identity. Matches the long-standing "default" fallback so
// policy and credential providers keyed on tenant can still match.
const (
	routeDefaultTenantID = "default"
	routeDefaultClientID = "default"
)

// Span attribute keys for router-level tracing. Scoped constants prevent
// drift between the synchronous and streaming route spans.
const (
	spanAttrToolID   = "mcp.tool_id"
	spanAttrTenantID = "mcp.tenant_id"
	spanAttrClientID = "mcp.client_id"
)

// routeContext carries the resolved state produced by the pre-execution
// pipeline. Handlers use it to drive the synchronous or streaming execution
// step without redoing lookup, policy, or credential work.
//
// Session is a grain identity; routers dispatch via ActorSystem.AskGrain
// rather than goaktactor.Ask because session lifecycle is now managed by
// the grain engine.
type routeContext struct {
	Invocation *mcp.Invocation
	Tool       mcp.Tool
	TenantID   mcp.TenantID
	ClientID   mcp.ClientID
	Supervisor *goaktactor.PID
	Session    *goaktactor.GrainIdentity
	// CircuitGeneration is the circuit-breaker admission generation returned
	// by checkAcceptWork. It rides along on SessionInvoke/SessionInvokeStream
	// so the grain's outcome report can be correlated with the admission.
	CircuitGeneration uint64
}

// routeFailure describes the terminal state of a failed pipeline stage. It
// carries everything the caller needs to emit uniform audit and telemetry
// records without duplicating the mapping from stage to event type / outcome
// / error code.
type routeFailure struct {
	Err       error
	Code      mcp.ErrorCode
	Outcome   string
	EventType mcp.AuditEventType
}

// resolveIdentity extracts tenant and client identifiers from an invocation,
// substituting the default sentinel when either is zero.
func (x *router) resolveIdentity(inv *mcp.Invocation) (mcp.TenantID, mcp.ClientID) {
	tenantID := inv.Correlation.TenantID
	if tenantID.IsZero() {
		tenantID = routeDefaultTenantID
	}

	clientID := inv.Correlation.ClientID
	if clientID.IsZero() {
		clientID = routeDefaultClientID
	}

	return tenantID, clientID
}

// runPreExecutionPipeline performs every routing step up to (and including)
// session resolution. On success it returns a routeContext the caller can use
// to dispatch synchronous or streaming execution. On failure it returns the
// typed routeFailure describing the stage that failed.
//
// The pipeline order is intentional: cheaper checks (tool lookup, policy) run
// before potentially expensive ones (credential resolution, session spawn).
func (x *router) runPreExecutionPipeline(goCtx context.Context, inv *mcp.Invocation, tenantID mcp.TenantID, clientID mcp.ClientID) (*routeContext, *routeFailure) {
	tool, err := x.lookupTool(goCtx, inv.ToolID)
	if err != nil {
		return nil, &routeFailure{
			Err:       err,
			Code:      mcp.ErrCodeToolNotFound,
			Outcome:   routeOutcomeError,
			EventType: mcp.AuditEventTypeInvocationFailed,
		}
	}

	if err := x.evaluatePolicy(goCtx, inv, tool, tenantID, clientID); err != nil {
		return nil, &routeFailure{
			Err:       err,
			Code:      errorCodeFrom(err, mcp.ErrCodePolicyDenied),
			Outcome:   outcomeFromError(err),
			EventType: mcp.AuditEventTypePolicyDecision,
		}
	}

	supervisor, err := x.lookupSupervisor(goCtx, inv.ToolID)
	if err != nil {
		return nil, &routeFailure{
			Err:       err,
			Code:      mcp.ErrCodeInternal,
			Outcome:   routeOutcomeError,
			EventType: mcp.AuditEventTypeInvocationFailed,
		}
	}

	generation, err := x.checkAcceptWork(goCtx, supervisor, inv.ToolID, tool)
	if err != nil {
		return nil, &routeFailure{
			Err:       err,
			Code:      errorCodeFrom(err, mcp.ErrCodeToolUnavailable),
			Outcome:   routeOutcomeUnavailable,
			EventType: mcp.AuditEventTypeInvocationFailed,
		}
	}

	invToUse, err := x.resolveCredentials(goCtx, inv, tool, tenantID)
	if err != nil {
		x.releaseAcceptedWork(goCtx, supervisor, inv.ToolID)
		return nil, &routeFailure{
			Err:       err,
			Code:      mcp.ErrCodeCredentialUnavailable,
			Outcome:   routeOutcomeCredentialUnavail,
			EventType: mcp.AuditEventTypeInvocationFailed,
		}
	}

	session, err := x.resolveSession(goCtx, supervisor, tenantID, clientID, inv.ToolID, invToUse.Credentials)
	if err != nil {
		x.releaseAcceptedWork(goCtx, supervisor, inv.ToolID)
		return nil, &routeFailure{
			Err:       err,
			Code:      mcp.ErrCodeInternal,
			Outcome:   routeOutcomeSessionError,
			EventType: mcp.AuditEventTypeInvocationFailed,
		}
	}

	return &routeContext{
		Invocation:        invToUse,
		Tool:              tool,
		TenantID:          tenantID,
		ClientID:          clientID,
		Supervisor:        supervisor,
		Session:           session,
		CircuitGeneration: generation,
	}, nil
}

// emitRouteFailure records a uniform failure trail: invocation-failure
// telemetry counter, audit event for the failing stage, and (upstream of
// this call) the span's Error status. It is safe to call with a nil
// invocation and collapses to a no-op on audit when the journal is not
// running.
func (x *router) emitRouteFailure(goCtx context.Context, inv *mcp.Invocation, tenantID mcp.TenantID, failure *routeFailure) {
	if failure == nil {
		return
	}

	telemetry.RecordInvocationFailure(goCtx, inv.ToolID, tenantID, string(failure.Code))
	x.logAuditError(x.recordAuditEvent(goCtx, invocationEvent(
		inv,
		failure.EventType,
		failure.Outcome,
		string(failure.Code),
		failure.Err.Error(),
	)))
}

// emitValidationFailure records the audit trail for an invocation that
// failed the initial validateInvocation / validateInvokeStream check. Unlike
// emitRouteFailure, it does not bump the invocation-failure telemetry
// counter because validation-only failures never reach the tool.
func (x *router) emitValidationFailure(goCtx context.Context, inv *mcp.Invocation, err error) {
	if inv == nil {
		return
	}

	x.logAuditError(x.recordAuditEvent(goCtx, invocationEvent(
		inv,
		mcp.AuditEventTypeInvocationFailed,
		routeOutcomeInvalid,
		string(mcp.ErrCodeInvalidRequest),
		err.Error(),
	)))
}

// emitExecutionFailure records telemetry and audit for a failure that
// happened inside executeInvocation or executeInvocationStream — after the
// pipeline succeeded. Kept separate from emitRouteFailure because the
// calling handlers already have the concrete error and execution-specific
// outcome constants.
func (x *router) emitExecutionFailure(goCtx context.Context, inv *mcp.Invocation, tenantID mcp.TenantID, err error) {
	code := errorCodeFrom(err, mcp.ErrCodeInternal)
	telemetry.RecordInvocationFailure(goCtx, inv.ToolID, tenantID, string(code))
	x.logAuditError(x.recordAuditEvent(goCtx, invocationEvent(
		inv,
		mcp.AuditEventTypeInvocationFailed,
		routeOutcomeExecutionError,
		string(code),
		err.Error(),
	)))
}

// emitInvocationComplete records the success audit event and invocation
// latency metric once an invocation returns a result.
func (x *router) emitInvocationComplete(goCtx context.Context, inv *mcp.Invocation, tenantID mcp.TenantID, outcome string, latencyMs float64) {
	telemetry.RecordInvocationLatency(goCtx, inv.ToolID, tenantID, latencyMs)
	x.logAuditError(x.recordAuditEvent(goCtx, invocationEvent(
		inv,
		mcp.AuditEventTypeInvocationComplete,
		outcome,
		"",
		"",
	)))
}

// errorCodeFrom extracts the RuntimeError code from err or returns fallback
// when err is not a RuntimeError. Removes the repetitive `errors.As` block
// that used to appear at every error-handling site in the router.
func errorCodeFrom(err error, fallback mcp.ErrorCode) mcp.ErrorCode {
	if err == nil {
		return fallback
	}

	if re, ok := errors.AsType[*mcp.RuntimeError](err); ok {
		return re.Code
	}
	return fallback
}

// loggerWithCorrelation returns a logger that has the invocation's tenant,
// client, request, trace, and tool correlation fields attached as structured
// key/value pairs. Falls back to the plain router logger when no fields are
// present (e.g. for tests with bare invocations).
func (x *router) loggerWithCorrelation(inv *mcp.Invocation) goaktlog.Logger {
	corr := &telemetry.CorrelationFields{
		TenantID:  inv.Correlation.TenantID,
		ClientID:  inv.Correlation.ClientID,
		RequestID: inv.Correlation.RequestID,
		TraceID:   inv.Correlation.TraceID,
		ToolID:    inv.ToolID,
	}

	kvs := corr.LogKeyValues()
	if len(kvs) == 0 {
		return x.logger
	}
	return x.logger.With(kvs...)
}

// startRouteSpan begins an internal span for a routing operation with the
// standard set of identity attributes. Callers must End() the returned span
// themselves; the context carries the span for downstream instrumentation.
func (x *router) startRouteSpan(parent context.Context, name string, inv *mcp.Invocation, tenantID mcp.TenantID, clientID mcp.ClientID) (context.Context, trace.Span) {
	return telemetry.Tracer().Start(parent, name,
		trace.WithAttributes(
			attribute.String(spanAttrToolID, string(inv.ToolID)),
			attribute.String(spanAttrTenantID, string(tenantID)),
			attribute.String(spanAttrClientID, string(clientID)),
		),
		trace.WithSpanKind(trace.SpanKindInternal),
	)
}
