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
	"sync"
	"time"

	goaktactor "github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/extension"
	goaktlog "github.com/tochemey/goakt/v4/log"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/tochemey/portcullis/mcp"

	"github.com/tochemey/portcullis/internal/naming"
	"github.com/tochemey/portcullis/internal/runtime"
	actorextension "github.com/tochemey/portcullis/internal/runtime/actor/extension"
	"github.com/tochemey/portcullis/internal/runtime/telemetry"
)

// OTel span attribute keys for per-session spans started inside the grain.
// Kept as file-scoped constants so dashboards can pin on stable names.
const (
	sessionSpanAttrToolID   = "mcp.tool_id"
	sessionSpanAttrTenantID = "mcp.tenant_id"
	sessionSpanAttrClientID = "mcp.client_id"
	sessionSpanAttrMethod   = "mcp.method"
)

// streamForwardSendTimeout bounds the time forwardStream will block trying
// to hand a progress event to a consumer. When the consumer disconnects
// and stops draining, an unbounded send would leak the goroutine (and
// keep the grain alive, blocking passivation) for the lifetime of the
// executor's stream. This timeout collapses that failure mode into a
// clean goroutine exit at a small price: bursty streams whose consumer
// pauses for longer than the window lose subsequent events. In practice
// the default is generous enough to tolerate any legitimate consumer
// pause while still bounding the leak.
const streamForwardSendTimeout = 30 * time.Second

// sessionGrain is the virtual-actor replacement for the former SessionActor.
//
// One grain exists per (tenant, client, tool) triple. Grains are activated
// on demand by goakt's grain engine when a caller resolves an identity for
// the first time, and passivated after the configured idle duration without
// a message. Between activation and deactivation, messages are processed
// sequentially, preserving single-threaded state semantics.
//
// Lifecycle reporting: the grain tells its ToolSupervisor
// [runtime.SessionActivated] on first activation and
// [runtime.SessionDeactivated] on passivation so the supervisor can keep a
// backpressure count without relying on parent/child actor topology (grains
// are not actor children).
//
// Invocation flow:
//   - [runtime.SessionInvoke] executes synchronously; on transport failure
//     the grain rebuilds its executor via the ExecutorFactory extension
//     and retries once.
//   - [runtime.SessionInvokeStream] dispatches to a [mcp.ToolStreamExecutor]
//     when available; progress and final events flow back through a
//     [mcp.StreamingResult] whose consumer outlives the grain message.
//
// sessionGrain is only ever touched from OnActivate, OnReceive, and
// OnDeactivate — the grain engine invokes these serially for a single
// grain instance, so grain state needs no mutex. The one exception is
// the streaming-goroutine pathway, but those goroutines operate on a
// locally-captured StreamingResult, not on grain fields; we coordinate
// their lifecycle with OnDeactivate via a WaitGroup rather than locking
// shared state.
//
// Boundary note: the streaming path is the one place this package hands
// raw channels out of the actor system, because the consumer of a
// [mcp.StreamingResult] (an ingress handler or library caller) is not an
// actor and cannot receive messages. Inside the actor system, all
// communication is message passing; channels appear only at this edge.
type sessionGrain struct {
	// streams tracks in-flight forwardStream goroutines. Incremented at
	// stream dispatch, decremented when the goroutine exits. OnDeactivate
	// waits on this WaitGroup so the executor is never closed while a
	// stream is still consuming it.
	streams sync.WaitGroup

	tenantID    mcp.TenantID
	clientID    mcp.ClientID
	toolID      mcp.ToolID
	tool        mcp.Tool
	executor    mcp.ToolExecutor
	credentials map[string]string

	// retired holds executors replaced by tryRecoverExecutor. They cannot be
	// closed at replacement time because an in-flight streaming goroutine may
	// still be reading from them; OnDeactivate closes them after streams.Wait
	// guarantees no stream is active. Only touched from OnReceive and
	// OnDeactivate, which the grain engine serializes.
	retired []mcp.ToolExecutor

	actorSystem goaktactor.ActorSystem
	logger      goaktlog.Logger
}

// Compile-time proof that sessionGrain satisfies the Grain contract.
var _ goaktactor.Grain = (*sessionGrain)(nil)

// OnActivate resolves the session dependency, builds the executor from the
// actor system's ExecutorFactoryExtension, records the session-created
// metric, and notifies the ToolSupervisor via a Tell so the supervisor can
// update its per-tool session count.
//
// Executor construction happens HERE (not in the supervisor) so repeat
// GrainOf calls for an already-active grain do not leak orphaned backend
// handles. The factory is looked up on the actor system's extension
// registry; when no factory is present the grain runs in stub mode
// (invocations succeed with an empty output), which test harnesses use to
// exercise the actor pipeline without real backends.
func (g *sessionGrain) OnActivate(ctx context.Context, props *goaktactor.GrainProps) error {
	g.actorSystem = props.ActorSystem()
	g.logger = g.actorSystem.Logger()

	dep := findSessionDependency(props.Dependencies())
	if dep == nil {
		return mcp.NewRuntimeError(mcp.ErrCodeInternal, "session dependency not found")
	}

	g.tenantID = dep.TenantID()
	g.clientID = dep.ClientID()
	g.toolID = dep.ToolID()
	g.tool = dep.Tool()
	g.credentials = dep.Credentials()

	if g.toolID.IsZero() {
		return mcp.NewRuntimeError(mcp.ErrCodeInternal, "session dependency has empty tool ID")
	}

	executor, err := g.createExecutor(ctx)
	if err != nil {
		return err
	}
	g.executor = executor

	telemetry.RecordSessionCreated(ctx, g.toolID, g.tenantID)
	g.notifySupervisor(ctx, &runtime.SessionActivated{
		ToolID:   g.toolID,
		TenantID: g.tenantID,
		ClientID: g.clientID,
	})

	g.logger.Infof("session grain:%s-%s-%s activated", g.tenantID, g.clientID, g.toolID)
	return nil
}

// createExecutor looks up the ExecutorFactoryExtension on the actor system
// and invokes it with the grain's tool + credentials. Returns (nil, nil)
// when no factory is registered — the grain then runs in stub mode (see
// OnActivate).
func (g *sessionGrain) createExecutor(ctx context.Context) (mcp.ToolExecutor, error) {
	for _, ext := range g.actorSystem.Extensions() {
		ef, ok := ext.(*actorextension.ExecutorFactoryExtension)
		if !ok || ef == nil {
			continue
		}

		factory := ef.Factory()
		if factory == nil {
			return nil, nil
		}

		executor, err := factory.Create(ctx, g.tool, g.credentials)
		if err != nil {
			return nil, mcp.WrapRuntimeError(mcp.ErrCodeInternal, "failed to create executor", err)
		}
		return executor, nil
	}
	return nil, nil
}

// OnReceive dispatches every message delivered to the grain. It mirrors the
// runtime message set the former SessionActor understood, so routers and
// admin probes interact with the grain the same way.
func (g *sessionGrain) OnReceive(gctx *goaktactor.GrainContext) {
	switch msg := gctx.Message().(type) {
	case *runtime.SessionInvoke:
		g.handleSessionInvoke(gctx, msg)
	case *runtime.SessionInvokeStream:
		g.handleSessionInvokeStream(gctx, msg)
	default:
		gctx.Unhandled()
	}
}

// OnDeactivate waits for every in-flight streaming goroutine to finish
// (so the executor is never closed while a stream is still reading from
// it), then closes the executor, records the destroyed metric, and
// notifies the ToolSupervisor via Tell. Tell failures are swallowed
// because supervisors may already be stopping during system shutdown.
func (g *sessionGrain) OnDeactivate(ctx context.Context, _ *goaktactor.GrainProps) error {
	// Block the grain engine's deactivation until every streaming
	// goroutine has drained. The mailbox is already quiesced at this
	// point (no new OnReceive calls), so this wait has a bounded upper
	// bound: the longest in-flight stream plus its consumer drain.
	g.streams.Wait()

	executor := g.executor
	g.executor = nil

	if executor != nil {
		_ = executor.Close()
	}

	// Executors retired by recovery were kept open for in-flight streams;
	// with streams drained they can now be released.
	for _, old := range g.retired {
		if old != nil {
			_ = old.Close()
		}
	}
	g.retired = nil

	telemetry.RecordSessionDestroyed(ctx, g.toolID, g.tenantID)
	g.notifySupervisor(ctx, &runtime.SessionDeactivated{
		ToolID:   g.toolID,
		TenantID: g.tenantID,
		ClientID: g.clientID,
	})

	g.logger.Infof("session grain:%s-%s-%s deactivated", g.tenantID, g.clientID, g.toolID)
	return nil
}

// handleSessionInvoke runs a synchronous tool invocation. Transport-layer
// failures trigger a single-shot executor replacement via the
// ExecutorFactory extension, then one retry of the invocation.
func (g *sessionGrain) handleSessionInvoke(gctx *goaktactor.GrainContext, msg *runtime.SessionInvoke) {
	if msg.Invocation == nil {
		gctx.Response(&runtime.SessionInvokeResult{Err: mcp.NewRuntimeError(mcp.ErrCodeInvalidRequest, "invocation is required")})
		return
	}
	if msg.Invocation.ToolID != g.toolID {
		gctx.Response(&runtime.SessionInvokeResult{Err: mcp.NewRuntimeError(mcp.ErrCodeInvalidRequest, "tool ID mismatch")})
		return
	}

	log := g.invocationLogger(msg.Invocation)
	start := time.Now()

	executor := g.executor
	if executor == nil {
		// Stub mode: no executor configured.
		result := &mcp.ExecutionResult{
			Status:      mcp.ExecutionStatusSuccess,
			Output:      map[string]any{},
			Duration:    time.Since(start),
			Correlation: msg.Invocation.Correlation,
		}
		gctx.Response(&runtime.SessionInvokeResult{Result: result})
		g.reportOutcomeToSupervisor(gctx.Context(), result, msg.CircuitGeneration)
		return
	}

	result := g.executeOnce(gctx, executor, msg.Invocation, log, start)
	gctx.Response(&runtime.SessionInvokeResult{Result: result})
	g.reportOutcomeToSupervisor(gctx.Context(), result, msg.CircuitGeneration)
}

// executeOnce performs one invocation round, attempting executor recovery
// and a single retry on transport failures. It always returns a non-nil
// ExecutionResult so callers do not need a nil-check on the happy path.
func (g *sessionGrain) executeOnce(gctx *goaktactor.GrainContext, executor mcp.ToolExecutor, inv *mcp.Invocation, log goaktlog.Logger, start time.Time) *mcp.ExecutionResult {
	timeout := g.requestTimeout()
	execCtx, cancel := context.WithTimeout(gctx.Context(), timeout)
	defer cancel()

	span, finishSpan := g.startExecuteSpan(execCtx, inv)
	execCtx = span

	result, err := g.dispatch(execCtx, executor, inv)
	duration := time.Since(start)

	if err != nil || isTransportFailure(result) {
		reason := err
		if reason == nil && result != nil && result.Err != nil {
			reason = result.Err
		}
		log.Warnf("session grain:%s-%s-%s execution failed, attempting recovery: %v", g.tenantID, g.clientID, g.toolID, reason)

		if g.tryRecoverExecutor(gctx) {
			log.Infof("session grain:%s-%s-%s executor recovered, retrying", g.tenantID, g.clientID, g.toolID)

			retryCtx, retryCancel := context.WithTimeout(gctx.Context(), timeout)
			defer retryCancel()
			retryStart := time.Now()

			result, err = g.dispatch(retryCtx, g.executor, inv)
			duration = time.Since(retryStart)
		}

		if err != nil {
			log.Warnf("session grain:%s-%s-%s execution failed: %v", g.tenantID, g.clientID, g.toolID, err)
			finishSpan(err)
			return &mcp.ExecutionResult{
				Status:      mcp.ExecutionStatusFailure,
				Err:         mcp.WrapRuntimeError(mcp.ErrCodeInternal, "execution failed", err),
				Duration:    duration,
				Correlation: inv.Correlation,
			}
		}
	}

	if result != nil && result.Duration == 0 {
		result.Duration = duration
	}

	finishSpan(nil)
	return result
}

// dispatch routes the invocation to the executor using the MCP method on
// the invocation. Resources use a distinct entry point because they share
// the ExecutionResult shape but not the call semantics.
func (g *sessionGrain) dispatch(ctx context.Context, executor mcp.ToolExecutor, inv *mcp.Invocation) (*mcp.ExecutionResult, error) {
	if inv.Method == mcp.MethodResourcesRead {
		return g.executeResourceRead(ctx, executor, inv)
	}
	return executor.Execute(ctx, inv)
}

// executeResourceRead delegates to the executor's optional ResourceExecutor
// capability. Returns a typed failure result when the executor does not
// implement resource semantics.
func (g *sessionGrain) executeResourceRead(ctx context.Context, executor mcp.ToolExecutor, inv *mcp.Invocation) (*mcp.ExecutionResult, error) {
	re, ok := executor.(mcp.ResourceExecutor)
	if !ok {
		return &mcp.ExecutionResult{
			Status:      mcp.ExecutionStatusFailure,
			Err:         mcp.NewRuntimeError(mcp.ErrCodeInvalidRequest, "executor does not support resources"),
			Correlation: inv.Correlation,
		}, nil
	}
	return re.ReadResource(ctx, inv)
}

// handleSessionInvokeStream dispatches a streaming invocation. When the
// executor implements [mcp.ToolStreamExecutor], a StreamingResult flows
// back to the caller and a goroutine owns the downstream channel close
// plus supervisor outcome reporting so the grain mailbox is free
// immediately for the next message.
func (g *sessionGrain) handleSessionInvokeStream(gctx *goaktactor.GrainContext, msg *runtime.SessionInvokeStream) {
	if msg.Invocation == nil {
		gctx.Response(&runtime.SessionInvokeStreamResult{Err: mcp.NewRuntimeError(mcp.ErrCodeInvalidRequest, "invocation is required")})
		return
	}
	if msg.Invocation.ToolID != g.toolID {
		gctx.Response(&runtime.SessionInvokeStreamResult{Err: mcp.NewRuntimeError(mcp.ErrCodeInvalidRequest, "tool ID mismatch")})
		return
	}

	executor := g.executor
	streamExec, ok := executor.(mcp.ToolStreamExecutor)
	if executor == nil || !ok {
		g.handleSessionInvokeStreamFallback(gctx, executor, msg)
		return
	}

	timeout := g.requestTimeout()
	// The stream outlives this message: the caller consumes it long after
	// the router's per-message context (the parent of gctx.Context()) has
	// been cancelled by the router handler returning. WithoutCancel detaches
	// the execution context from that cancellation while preserving values
	// (trace context); the stream stays bounded by the tool's own request
	// timeout and by the cancel owned by the forwarding goroutine.
	execCtx, cancel := context.WithTimeout(context.WithoutCancel(gctx.Context()), timeout)

	sr, err := streamExec.ExecuteStream(execCtx, msg.Invocation)
	if err != nil {
		cancel()
		result := &mcp.ExecutionResult{
			Status:      mcp.ExecutionStatusFailure,
			Err:         mcp.WrapRuntimeError(mcp.ErrCodeInternal, "stream execution failed", err),
			Correlation: msg.Invocation.Correlation,
		}
		gctx.Response(&runtime.SessionInvokeStreamResult{Result: result})
		g.reportOutcomeToSupervisor(gctx.Context(), result, msg.CircuitGeneration)
		return
	}

	// Fan out the executor's channels so the caller and the goroutine do
	// not race on the same source. The goroutine is the sole reader of
	// sr.Progress / sr.Final; it forwards events to callerProg / callerFinal
	// and reports the outcome to the supervisor after the stream closes.
	callerProg := make(chan mcp.ProgressEvent)
	callerFinal := make(chan *mcp.ExecutionResult, 1)

	// Add BEFORE starting the goroutine so OnDeactivate cannot squeeze
	// between Go runtime scheduling and the first Add call.
	g.streams.Add(1)
	go g.forwardStream(sr, callerProg, callerFinal, cancel, msg.CircuitGeneration)

	gctx.Response(&runtime.SessionInvokeStreamResult{
		StreamResult: &mcp.StreamingResult{
			Progress: callerProg,
			Final:    callerFinal,
		},
	})
}

// handleSessionInvokeStreamFallback runs the synchronous execute path when
// streaming is unavailable, wrapping the single result in a
// SessionInvokeStreamResult so ingress handlers see a uniform response
// type. The stub path mirrors SessionInvoke's stub handling.
func (g *sessionGrain) handleSessionInvokeStreamFallback(gctx *goaktactor.GrainContext, executor mcp.ToolExecutor, msg *runtime.SessionInvokeStream) {
	start := time.Now()

	if executor == nil {
		result := &mcp.ExecutionResult{
			Status:      mcp.ExecutionStatusSuccess,
			Output:      map[string]any{},
			Duration:    time.Since(start),
			Correlation: msg.Invocation.Correlation,
		}
		g.reportOutcomeToSupervisor(gctx.Context(), result, msg.CircuitGeneration)
		gctx.Response(&runtime.SessionInvokeStreamResult{Result: result})
		return
	}

	timeout := g.requestTimeout()
	execCtx, cancel := context.WithTimeout(gctx.Context(), timeout)
	defer cancel()

	result, err := executor.Execute(execCtx, msg.Invocation)
	if err != nil {
		result = &mcp.ExecutionResult{
			Status:      mcp.ExecutionStatusFailure,
			Err:         mcp.WrapRuntimeError(mcp.ErrCodeInternal, "execution failed", err),
			Duration:    time.Since(start),
			Correlation: msg.Invocation.Correlation,
		}
	}
	if result != nil && result.Duration == 0 {
		result.Duration = time.Since(start)
	}

	g.reportOutcomeToSupervisor(gctx.Context(), result, msg.CircuitGeneration)
	gctx.Response(&runtime.SessionInvokeStreamResult{Result: result})
}

// forwardStream pumps the executor's StreamingResult onto the caller's
// channels and then reports the outcome to the supervisor. Runs on its own
// goroutine so the grain's OnReceive returns immediately after dispatching
// the stream — keeping the mailbox free for the next message.
//
// An abandoned consumer (ingress client disconnect, HTTP request timeout)
// would leak this goroutine if we used a bare blocking send, because the
// upstream executor can keep producing events indefinitely. The bounded
// send below caps that leak at streamForwardSendTimeout per event: if
// the consumer stops draining, we cancel the executor context and exit.
func (g *sessionGrain) forwardStream(source *mcp.StreamingResult, progress chan<- mcp.ProgressEvent, final chan<- *mcp.ExecutionResult, cancel context.CancelFunc, generation uint64) {
	// Done must run before cancel so OnDeactivate's streams.Wait unblocks
	// before any deferred cancellation can race with the grain tearing
	// down the executor.
	defer g.streams.Done()
	// cancel is owned by this goroutine — canceling earlier would abort the
	// executor mid-stream.
	defer cancel()

	// progressForwardLoop returns true when the stream completed cleanly
	// (source.Progress was drained) and false when the consumer was
	// abandoned and we exited early via the timeout path.
	consumed := g.forwardProgress(source.Progress, progress)
	close(progress)

	if !consumed {
		// Consumer went away. Drain Final non-blockingly so we don't
		// block here either, then close.
		var outcome *mcp.ExecutionResult
		select {
		case fin, ok := <-source.Final:
			if ok && fin != nil {
				outcome = fin
				// Best-effort deliver to final; buffered channel so this
				// does not block.
				select {
				case final <- fin:
				default:
				}
			}
		default:
		}
		close(final)
		if outcome != nil {
			// The backend did finish; report its real verdict.
			g.reportOutcomeToSupervisor(context.Background(), outcome, generation)
			return
		}
		// No backend verdict exists: the consumer disconnected, which says
		// nothing about the backend's health. Release the circuit admission
		// instead of reporting a failure — otherwise client disconnects
		// would trip the breaker and deny service for a healthy tool.
		g.releaseWorkToSupervisor(context.Background())
		return
	}

	outcome := <-source.Final
	if outcome != nil {
		final <- outcome
	}
	close(final)

	// Use a fresh background context for the supervisor Tell: the grain
	// activation context that spawned us may have expired by now.
	g.reportOutcomeToSupervisor(context.Background(), outcome, generation)
}

// forwardProgress drains source into dst until source closes. Returns
// true when drained fully, or false when dst stopped accepting events for
// longer than streamForwardSendTimeout (consumer abandonment signal).
func (g *sessionGrain) forwardProgress(source <-chan mcp.ProgressEvent, dst chan<- mcp.ProgressEvent) bool {
	for evt := range source {
		timer := time.NewTimer(streamForwardSendTimeout)
		select {
		case dst <- evt:
			timer.Stop()
		case <-timer.C:
			g.logger.Warnf("session grain:%s-%s-%s progress consumer abandoned after %s; aborting stream",
				g.tenantID, g.clientID, g.toolID, streamForwardSendTimeout)
			return false
		}
	}
	return true
}

// tryRecoverExecutor replaces a failed executor with a fresh one produced
// by the ExecutorFactory extension. Returns true when the new executor is
// installed. The replaced executor is retired and closed at deactivation,
// after in-flight streams have drained.
func (g *sessionGrain) tryRecoverExecutor(gctx *goaktactor.GrainContext) bool {
	ext := gctx.Extension(actorextension.ExecutorFactoryExtensionID)
	ef, ok := ext.(*actorextension.ExecutorFactoryExtension)
	if !ok || ef == nil {
		return false
	}

	factory := ef.Factory()
	if factory == nil {
		g.logger.Warnf("session grain:%s-%s-%s executor recovery skipped: factory is nil", g.tenantID, g.clientID, g.toolID)
		return false
	}

	newExec, err := factory.Create(gctx.Context(), g.tool, g.credentials)
	if err != nil {
		g.logger.Warnf("session grain:%s-%s-%s executor recovery failed: %v", g.tenantID, g.clientID, g.toolID, err)
		return false
	}

	old := g.executor
	g.executor = newExec

	// The old executor is retired, not closed: a streaming goroutine
	// dispatched by an earlier message may still be reading from it, and
	// closing the backend session under an in-flight stream would abort the
	// stream (and mis-report a backend failure to the circuit breaker).
	// OnDeactivate closes retired executors once streams.Wait has drained.
	if old != nil {
		g.retired = append(g.retired, old)
	}
	return true
}

// reportOutcomeToSupervisor sends a ReportSuccess or ReportFailure message
// to the ToolSupervisor so it can advance the per-tool circuit-breaker
// state. Delivered via Tell (fire-and-forget) so the invocation response
// is never blocked on the supervisor's mailbox. The generation is the
// circuit admission generation carried on the SessionInvoke message; the
// supervisor uses it to discard outcomes that arrive after the circuit has
// already transitioned (zero means uncorrelated).
func (g *sessionGrain) reportOutcomeToSupervisor(ctx context.Context, result *mcp.ExecutionResult, generation uint64) {
	pid := g.resolveSupervisor(ctx)
	if pid == nil {
		return
	}

	success := result != nil && result.Status == mcp.ExecutionStatusSuccess && result.Err == nil
	if success {
		_ = goaktactor.Tell(ctx, pid, &runtime.ReportSuccess{ToolID: g.toolID, CircuitGeneration: generation})
		return
	}
	_ = goaktactor.Tell(ctx, pid, &runtime.ReportFailure{ToolID: g.toolID, CircuitGeneration: generation})
}

// releaseWorkToSupervisor returns the circuit admission without recording an
// outcome. Used when no backend verdict exists — e.g. the stream consumer
// disconnected before the backend finished — so a reserved half-open probe
// slot is freed instead of being blamed on (or credited to) the backend.
func (g *sessionGrain) releaseWorkToSupervisor(ctx context.Context) {
	pid := g.resolveSupervisor(ctx)
	if pid == nil {
		return
	}
	_ = goaktactor.Tell(ctx, pid, &runtime.ReleaseWork{ToolID: g.toolID})
}

// notifySupervisor sends a lifecycle message (SessionActivated /
// SessionDeactivated) to the ToolSupervisor via Tell. Errors are swallowed
// because supervisors may have stopped by the time OnDeactivate fires
// during system shutdown.
func (g *sessionGrain) notifySupervisor(ctx context.Context, msg any) {
	pid := g.resolveSupervisor(ctx)
	if pid == nil {
		return
	}
	_ = goaktactor.Tell(ctx, pid, msg)
}

// resolveSupervisor returns the PID of the ToolSupervisor for this grain's
// tool, or nil when unavailable. The lookup is intentionally re-done each
// call: in cluster mode, a supervisor can relocate and caching the PID
// would leak a stale reference.
func (g *sessionGrain) resolveSupervisor(ctx context.Context) *goaktactor.PID {
	if g.actorSystem == nil {
		return nil
	}

	pid, err := g.actorSystem.ActorOf(ctx, naming.ToolSupervisorName(g.toolID))
	if err != nil || pid == nil || !pid.IsRunning() {
		return nil
	}
	return pid
}

// invocationLogger decorates the grain logger with the correlation fields
// from the inbound invocation so log lines tied to a single request can be
// correlated in downstream systems.
func (g *sessionGrain) invocationLogger(inv *mcp.Invocation) goaktlog.Logger {
	corr := &telemetry.CorrelationFields{
		TenantID:  inv.Correlation.TenantID,
		ClientID:  inv.Correlation.ClientID,
		RequestID: inv.Correlation.RequestID,
		TraceID:   inv.Correlation.TraceID,
		ToolID:    inv.ToolID,
	}

	kvs := corr.LogKeyValues()
	if len(kvs) == 0 {
		return g.logger
	}
	return g.logger.With(kvs...)
}

// startExecuteSpan opens an internal OTel span around a single executor
// call. The returned context carries the span and finishSpan must be
// called exactly once with the terminal error (nil on success).
func (g *sessionGrain) startExecuteSpan(parent context.Context, inv *mcp.Invocation) (context.Context, func(err error)) {
	if !telemetry.TracingEnabled() {
		return parent, func(error) {}
	}

	ctx, span := telemetry.Tracer().Start(parent, "portcullis.session.execute",
		trace.WithAttributes(
			attribute.String(sessionSpanAttrToolID, string(g.toolID)),
			attribute.String(sessionSpanAttrTenantID, string(g.tenantID)),
			attribute.String(sessionSpanAttrClientID, string(g.clientID)),
			attribute.String(sessionSpanAttrMethod, inv.Method),
		),
		trace.WithSpanKind(trace.SpanKindInternal),
	)

	return ctx, func(err error) {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err)
		}
		span.End()
	}
}

// requestTimeout returns the per-invocation execution timeout, falling
// back to the package default when the tool does not override it.
func (g *sessionGrain) requestTimeout() time.Duration {
	if g.tool.RequestTimeout > 0 {
		return g.tool.RequestTimeout
	}
	return mcp.DefaultRequestTimeout
}

// findSessionDependency locates the SessionDependency in the GrainProps
// dependency slice. Returns nil when no session dependency was attached at
// activation time.
func findSessionDependency(deps []extension.Dependency) *actorextension.SessionDependency {
	for _, dep := range deps {
		if sd, ok := dep.(*actorextension.SessionDependency); ok {
			return sd
		}
	}
	return nil
}

// isTransportFailure returns true when the execution result indicates a
// transport-level failure (connection drop, process crash). The built-in
// stdio and HTTP executors surface these as result.Err carrying
// ErrCodeTransportFailure and a nil Go error.
func isTransportFailure(result *mcp.ExecutionResult) bool {
	if result == nil || result.Err == nil {
		return false
	}
	return result.Err.Code == mcp.ErrCodeTransportFailure
}
