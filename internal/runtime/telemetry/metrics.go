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

package telemetry

import (
	"context"
	"sync/atomic"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/tochemey/portcullis/mcp"
)

const instrumentationName = "github.com/tochemey/portcullis/telemetry"

// Metric attribute keys. Stable names used across every Record* function so
// operators see a consistent dimension vocabulary on scraped metrics.
const (
	attrKeyReason   = "reason"
	attrKeyToolID   = "tool_id"
	attrKeyTenantID = "tenant_id"
)

// Credential cache result attribute values surfaced on the
// portcullis.credential.cache counter.
const (
	credentialCacheHit  = "hit"
	credentialCacheMiss = "miss"
)

// Metrics holds OpenTelemetry instruments for portcullis runtime observability.
// Created at gateway level via RegisterMetrics and used by actors via the
// Record* functions. Mirrors the GoAkt pattern of registering instruments at
// actor system creation.
type Metrics struct {
	toolAvailability  metric.Int64Counter
	invocationLatency metric.Float64Histogram
	invocationFailure metric.Int64Counter
	circuitState      metric.Int64Counter

	// Session lifecycle metrics.
	sessionActive         metric.Int64UpDownCounter
	sessionCreated        metric.Int64Counter
	sessionDestroyed      metric.Int64Counter
	sessionPassivated     metric.Int64Counter
	credentialCacheResult metric.Int64Counter
	policyEvalLatency     metric.Float64Histogram

	// Actor system health metrics.
	deadLetter metric.Int64Counter
}

var metricsPtr atomic.Pointer[Metrics]

// RegisterMetrics creates OpenTelemetry instruments from the given Meter and
// registers them as the default metrics for the process. Call this at gateway
// Start when WithMetrics() is enabled. Returns an error if any instrument
// cannot be created.
//
// Pass nil to use otel.GetMeterProvider().Meter(instrumentationName).
func RegisterMetrics(meter metric.Meter) (*Metrics, error) {
	if meter == nil {
		meter = otel.GetMeterProvider().Meter(instrumentationName)
	}

	metrics := &Metrics{}
	var err error

	if metrics.toolAvailability, err = meter.Int64Counter(
		"portcullis.tool.availability",
		metric.WithDescription("Tool availability state transitions"),
	); err != nil {
		return nil, err
	}

	if metrics.invocationLatency, err = meter.Float64Histogram(
		"portcullis.invocation.latency",
		metric.WithDescription("Tool invocation latency in milliseconds"),
		metric.WithUnit("ms"),
	); err != nil {
		return nil, err
	}

	if metrics.invocationFailure, err = meter.Int64Counter(
		"portcullis.invocation.failure",
		metric.WithDescription("Failed tool invocations"),
	); err != nil {
		return nil, err
	}

	if metrics.circuitState, err = meter.Int64Counter(
		"portcullis.circuit.state",
		metric.WithDescription("Circuit breaker state transitions"),
	); err != nil {
		return nil, err
	}

	if metrics.sessionActive, err = meter.Int64UpDownCounter(
		"portcullis.session.active",
		metric.WithDescription("Active session count (per tool, per tenant)"),
	); err != nil {
		return nil, err
	}

	if metrics.sessionCreated, err = meter.Int64Counter(
		"portcullis.session.created",
		metric.WithDescription("Total sessions created"),
	); err != nil {
		return nil, err
	}

	if metrics.sessionDestroyed, err = meter.Int64Counter(
		"portcullis.session.destroyed",
		metric.WithDescription("Total sessions destroyed"),
	); err != nil {
		return nil, err
	}

	if metrics.sessionPassivated, err = meter.Int64Counter(
		"portcullis.session.passivated",
		metric.WithDescription("Sessions stopped due to idle passivation"),
	); err != nil {
		return nil, err
	}

	if metrics.credentialCacheResult, err = meter.Int64Counter(
		"portcullis.credential.cache",
		metric.WithDescription("Credential cache lookups by result (hit or miss)"),
	); err != nil {
		return nil, err
	}

	if metrics.policyEvalLatency, err = meter.Float64Histogram(
		"portcullis.policy.evaluation.latency",
		metric.WithDescription("Policy evaluation latency in milliseconds"),
		metric.WithUnit("ms"),
	); err != nil {
		return nil, err
	}

	if metrics.deadLetter, err = meter.Int64Counter(
		"portcullis.actor.dead_letter",
		metric.WithDescription("Messages delivered to the actor system dead-letter stream, tagged by reason"),
	); err != nil {
		return nil, err
	}

	metricsPtr.Store(metrics)
	return metrics, nil
}

// UnregisterMetrics clears the default metrics. Use in tests to reset state.
func UnregisterMetrics() {
	metricsPtr.Store(nil)
}

// RecordToolAvailability records a tool availability state transition.
// No-op when metrics are not registered.
func RecordToolAvailability(ctx context.Context, toolID mcp.ToolID, available bool) {
	m := metricsPtr.Load()
	if m == nil {
		return
	}
	m.toolAvailability.Add(ctx, 1, metric.WithAttributes(
		attribute.String(attrKeyToolID, string(toolID)),
		attribute.Bool("available", available),
	))
}

// RecordInvocationLatency records the latency of a successful tool invocation.
// No-op when metrics are not registered.
func RecordInvocationLatency(ctx context.Context, toolID mcp.ToolID, tenantID mcp.TenantID, latencyMs float64) {
	m := metricsPtr.Load()
	if m == nil {
		return
	}
	m.invocationLatency.Record(ctx, latencyMs, metric.WithAttributes(
		attribute.String(attrKeyToolID, string(toolID)),
		attribute.String(attrKeyTenantID, string(tenantID)),
	))
}

// RecordInvocationFailure records a failed tool invocation.
// No-op when metrics are not registered.
func RecordInvocationFailure(ctx context.Context, toolID mcp.ToolID, tenantID mcp.TenantID, reason string) {
	m := metricsPtr.Load()
	if m == nil {
		return
	}
	m.invocationFailure.Add(ctx, 1, metric.WithAttributes(
		attribute.String(attrKeyToolID, string(toolID)),
		attribute.String(attrKeyTenantID, string(tenantID)),
		attribute.String(attrKeyReason, reason),
	))
}

// RecordCircuitState records a circuit breaker state transition.
// No-op when metrics are not registered.
func RecordCircuitState(ctx context.Context, toolID mcp.ToolID, state string) {
	m := metricsPtr.Load()
	if m == nil {
		return
	}
	m.circuitState.Add(ctx, 1, metric.WithAttributes(
		attribute.String(attrKeyToolID, string(toolID)),
		attribute.String("state", state),
	))
}

// RecordSessionCreated records a session creation event and increments the
// active session gauge. No-op when metrics are not registered.
func RecordSessionCreated(ctx context.Context, toolID mcp.ToolID, tenantID mcp.TenantID) {
	m := metricsPtr.Load()
	if m == nil {
		return
	}
	attrs := metric.WithAttributes(
		attribute.String(attrKeyToolID, string(toolID)),
		attribute.String(attrKeyTenantID, string(tenantID)),
	)
	m.sessionCreated.Add(ctx, 1, attrs)
	m.sessionActive.Add(ctx, 1, attrs)
}

// RecordSessionDestroyed records a session destruction event and decrements the
// active session gauge. No-op when metrics are not registered.
func RecordSessionDestroyed(ctx context.Context, toolID mcp.ToolID, tenantID mcp.TenantID) {
	m := metricsPtr.Load()
	if m == nil {
		return
	}
	attrs := metric.WithAttributes(
		attribute.String(attrKeyToolID, string(toolID)),
		attribute.String(attrKeyTenantID, string(tenantID)),
	)
	m.sessionDestroyed.Add(ctx, 1, attrs)
	m.sessionActive.Add(ctx, -1, attrs)
}

// RecordSessionPassivated records a session idle passivation event.
// Keyed by tool_id only; tenant_id is not available from the GoAkt
// ActorPassivated event stream where this is recorded.
// No-op when metrics are not registered.
func RecordSessionPassivated(ctx context.Context, toolID mcp.ToolID) {
	m := metricsPtr.Load()
	if m == nil {
		return
	}
	m.sessionPassivated.Add(ctx, 1, metric.WithAttributes(
		attribute.String(attrKeyToolID, string(toolID)),
	))
}

// RecordCredentialCacheResult records a credential cache lookup as a hit or miss.
// No-op when metrics are not registered.
func RecordCredentialCacheResult(ctx context.Context, toolID mcp.ToolID, tenantID mcp.TenantID, hit bool) {
	m := metricsPtr.Load()
	if m == nil {
		return
	}
	result := credentialCacheMiss
	if hit {
		result = credentialCacheHit
	}
	m.credentialCacheResult.Add(ctx, 1, metric.WithAttributes(
		attribute.String(attrKeyToolID, string(toolID)),
		attribute.String(attrKeyTenantID, string(tenantID)),
		attribute.String("result", result),
	))
}

// RecordPolicyEvaluationLatency records the latency of a policy evaluation.
// No-op when metrics are not registered.
func RecordPolicyEvaluationLatency(ctx context.Context, tenantID mcp.TenantID, decision string, latencyMs float64) {
	m := metricsPtr.Load()
	if m == nil {
		return
	}
	m.policyEvalLatency.Record(ctx, latencyMs, metric.WithAttributes(
		attribute.String(attrKeyTenantID, string(tenantID)),
		attribute.String("decision", decision),
	))
}

// RecordDeadLetter records a message that could not be delivered to its
// target actor. The reason is whatever GoAkt reported on the Deadletter
// event. No-op when metrics are not registered.
func RecordDeadLetter(ctx context.Context, reason string) {
	m := metricsPtr.Load()
	if m == nil {
		return
	}
	m.deadLetter.Add(ctx, 1, metric.WithAttributes(
		attribute.String(attrKeyReason, reason),
	))
}
