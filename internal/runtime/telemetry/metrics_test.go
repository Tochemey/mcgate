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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tochemey/mcgate/mcp"
)

func TestRecordFunctions_NoPanicWhenUnregistered(t *testing.T) {
	ctx := context.Background()
	UnregisterMetrics()

	// Record* should be no-ops when metrics are not registered
	assert.NotPanics(t, func() { RecordToolAvailability(ctx, mcp.ToolID("tool1"), true) })
	assert.NotPanics(t, func() { RecordInvocationLatency(ctx, mcp.ToolID("tool1"), mcp.TenantID("tenant1"), 42.5) })
	assert.NotPanics(t, func() { RecordInvocationFailure(ctx, mcp.ToolID("tool1"), mcp.TenantID("tenant1"), "timeout") })
	assert.NotPanics(t, func() { RecordCircuitState(ctx, mcp.ToolID("tool1"), "open") })
	assert.NotPanics(t, func() { RecordSessionCreated(ctx, mcp.ToolID("tool1"), mcp.TenantID("tenant1")) })
	assert.NotPanics(t, func() { RecordSessionDestroyed(ctx, mcp.ToolID("tool1"), mcp.TenantID("tenant1")) })
	assert.NotPanics(t, func() { RecordSessionPassivated(ctx, mcp.ToolID("tool1")) })
	assert.NotPanics(t, func() { RecordCredentialCacheResult(ctx, mcp.ToolID("tool1"), mcp.TenantID("tenant1"), true) })
	assert.NotPanics(t, func() { RecordPolicyEvaluationLatency(ctx, mcp.TenantID("tenant1"), "allow", 1.5) })
	assert.NotPanics(t, func() { RecordDeadLetter(ctx, "mailbox full") })
}

func TestRegisterMetrics(t *testing.T) {
	t.Run("creates instruments from meter", func(t *testing.T) {
		m, err := RegisterMetrics(nil)
		require.NoError(t, err)
		require.NotNil(t, m)
		t.Cleanup(UnregisterMetrics)
	})
}

func TestRecordFunctions_WithRegisteredMetrics(t *testing.T) {
	ctx := context.Background()

	m, err := RegisterMetrics(nil)
	require.NoError(t, err)
	require.NotNil(t, m)
	t.Cleanup(UnregisterMetrics)

	t.Run("RecordToolAvailability does not panic when registered", func(t *testing.T) {
		assert.NotPanics(t, func() {
			RecordToolAvailability(ctx, mcp.ToolID("tool-a"), true)
			RecordToolAvailability(ctx, mcp.ToolID("tool-a"), false)
		})
	})

	t.Run("RecordInvocationLatency does not panic when registered", func(t *testing.T) {
		assert.NotPanics(t, func() {
			RecordInvocationLatency(ctx, mcp.ToolID("tool-a"), mcp.TenantID("tenant-1"), 12.5)
		})
	})

	t.Run("RecordInvocationFailure does not panic when registered", func(t *testing.T) {
		assert.NotPanics(t, func() {
			RecordInvocationFailure(ctx, mcp.ToolID("tool-a"), mcp.TenantID("tenant-1"), "circuit_open")
		})
	})

	t.Run("RecordCircuitState does not panic when registered", func(t *testing.T) {
		assert.NotPanics(t, func() {
			RecordCircuitState(ctx, mcp.ToolID("tool-a"), "open")
			RecordCircuitState(ctx, mcp.ToolID("tool-a"), "closed")
		})
	})

	t.Run("RecordSessionCreated does not panic when registered", func(t *testing.T) {
		assert.NotPanics(t, func() {
			RecordSessionCreated(ctx, mcp.ToolID("tool-a"), mcp.TenantID("tenant-1"))
		})
	})

	t.Run("RecordSessionDestroyed does not panic when registered", func(t *testing.T) {
		assert.NotPanics(t, func() {
			RecordSessionDestroyed(ctx, mcp.ToolID("tool-a"), mcp.TenantID("tenant-1"))
		})
	})

	t.Run("RecordSessionPassivated does not panic when registered", func(t *testing.T) {
		assert.NotPanics(t, func() {
			RecordSessionPassivated(ctx, mcp.ToolID("tool-a"))
		})
	})

	t.Run("RecordCredentialCacheResult does not panic when registered", func(t *testing.T) {
		assert.NotPanics(t, func() {
			RecordCredentialCacheResult(ctx, mcp.ToolID("tool-a"), mcp.TenantID("tenant-1"), true)
			RecordCredentialCacheResult(ctx, mcp.ToolID("tool-a"), mcp.TenantID("tenant-1"), false)
		})
	})

	t.Run("RecordPolicyEvaluationLatency does not panic when registered", func(t *testing.T) {
		assert.NotPanics(t, func() {
			RecordPolicyEvaluationLatency(ctx, mcp.TenantID("tenant-1"), "allow", 2.5)
			RecordPolicyEvaluationLatency(ctx, mcp.TenantID("tenant-1"), "deny", 1.0)
		})
	})

	t.Run("RecordDeadLetter does not panic when registered", func(t *testing.T) {
		assert.NotPanics(t, func() {
			RecordDeadLetter(ctx, "actor stopped")
			RecordDeadLetter(ctx, "mailbox full")
		})
	})
}
