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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	goaktactor "github.com/tochemey/goakt/v4/actor"
	noopmetric "go.opentelemetry.io/otel/metric/noop"

	"github.com/tochemey/mcgate/mcp"

	"github.com/tochemey/mcgate/internal/naming"
	"github.com/tochemey/mcgate/internal/runtime"
	actorextension "github.com/tochemey/mcgate/internal/runtime/actor/extension"
	"github.com/tochemey/mcgate/internal/runtime/audit"
	"github.com/tochemey/mcgate/internal/runtime/policy"
	"github.com/tochemey/mcgate/internal/runtime/telemetry"
)

func TestToolSupervisorActor(t *testing.T) {
	ctx := context.Background()

	t.Run("resolves tool from registrar and accepts work when circuit closed", func(t *testing.T) {
		tool := validStdioTool("supervisor-tool")
		_, pid, stop := spawnTestSupervisor(t, tool)
		defer stop()

		resp, err := goaktactor.Ask(ctx, pid, &runtime.CanAcceptWork{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.CanAcceptWorkResult)
		require.True(t, ok)
		assert.True(t, result.Accept)
	})

	t.Run("rejects work when circuit opened after failures", func(t *testing.T) {
		tool := validStdioTool("circuit-tool")
		_, pid, stop := spawnTestSupervisor(t, tool)
		defer stop()

		for i := 0; i < mcp.DefaultCircuitFailureThreshold; i++ {
			require.NoError(t, goaktactor.Tell(ctx, pid, &runtime.ReportFailure{ToolID: tool.ID}))
		}
		waitForActors()

		resp, err := goaktactor.Ask(ctx, pid, &runtime.CanAcceptWork{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.CanAcceptWorkResult)
		require.True(t, ok)
		assert.False(t, result.Accept)
		assert.Contains(t, result.Reason, "circuit")
	})

	t.Run("rejects work when tool ID mismatch", func(t *testing.T) {
		tool := validStdioTool("mismatch-tool")
		_, pid, stop := spawnTestSupervisor(t, tool)
		defer stop()

		resp, err := goaktactor.Ask(ctx, pid, &runtime.CanAcceptWork{ToolID: "other-tool"}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.CanAcceptWorkResult)
		require.True(t, ok)
		assert.False(t, result.Accept)
	})

	t.Run("report success closes circuit from half-open", func(t *testing.T) {
		tool := validStdioTool("halfopen-tool")
		circuitCfg := mcp.CircuitConfig{
			FailureThreshold:    mcp.DefaultCircuitFailureThreshold,
			OpenDuration:        100 * time.Millisecond,
			HalfOpenMaxRequests: mcp.DefaultCircuitHalfOpenMaxRequests,
		}
		system, stop := testActorSystemForTools(t,
			goaktactor.WithExtensions(actorextension.NewCircuitConfigExtension(circuitCfg)))
		defer stop()

		spawnToolRuntimeForTest(t, ctx, system, tool)
		pid := supervisorPIDForTest(t, ctx, system, tool.ID)

		for i := 0; i < mcp.DefaultCircuitFailureThreshold; i++ {
			require.NoError(t, goaktactor.Tell(ctx, pid, &runtime.ReportFailure{ToolID: tool.ID}))
		}
		waitForActors()

		time.Sleep(150 * time.Millisecond)

		resp, err := goaktactor.Ask(ctx, pid, &runtime.CanAcceptWork{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.CanAcceptWorkResult)
		require.True(t, ok)
		assert.True(t, result.Accept)

		require.NoError(t, goaktactor.Tell(ctx, pid, &runtime.ReportSuccess{ToolID: tool.ID}))
		waitForActors()

		resp, err = goaktactor.Ask(ctx, pid, &runtime.CanAcceptWork{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)
		result2, ok := resp.(*runtime.CanAcceptWorkResult)
		require.True(t, ok)
		assert.True(t, result2.Accept)
	})

	t.Run("report failure in half-open reopens circuit", func(t *testing.T) {
		tool := validStdioTool("reopen-tool")
		circuitCfg := mcp.CircuitConfig{
			FailureThreshold:    mcp.DefaultCircuitFailureThreshold,
			OpenDuration:        100 * time.Millisecond,
			HalfOpenMaxRequests: mcp.DefaultCircuitHalfOpenMaxRequests,
		}
		system, stop := testActorSystemForTools(t,
			goaktactor.WithExtensions(actorextension.NewCircuitConfigExtension(circuitCfg)))
		defer stop()

		spawnToolRuntimeForTest(t, ctx, system, tool)
		pid := supervisorPIDForTest(t, ctx, system, tool.ID)

		for i := 0; i < mcp.DefaultCircuitFailureThreshold; i++ {
			require.NoError(t, goaktactor.Tell(ctx, pid, &runtime.ReportFailure{ToolID: tool.ID}))
		}
		waitForActors()

		time.Sleep(150 * time.Millisecond)

		// First CanAcceptWork transitions the circuit to half-open and
		// reserves a probe slot.
		_, err := goaktactor.Ask(ctx, pid, &runtime.CanAcceptWork{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)

		require.NoError(t, goaktactor.Tell(ctx, pid, &runtime.ReportFailure{ToolID: tool.ID}))
		time.Sleep(30 * time.Millisecond)

		resp, err := goaktactor.Ask(ctx, pid, &runtime.CanAcceptWork{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.CanAcceptWorkResult)
		require.True(t, ok)
		assert.False(t, result.Accept)
	})

	t.Run("report success with wrong tool ID is ignored", func(t *testing.T) {
		tool := validStdioTool("success-mismatch")
		_, pid, stop := spawnTestSupervisor(t, tool)
		defer stop()

		require.NoError(t, goaktactor.Tell(ctx, pid, &runtime.ReportSuccess{ToolID: "wrong-tool"}))
		waitForActors()

		resp, err := goaktactor.Ask(ctx, pid, &runtime.CanAcceptWork{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.CanAcceptWorkResult)
		require.True(t, ok)
		assert.True(t, result.Accept)
	})

	t.Run("stops when journal is not running at PostStart", func(t *testing.T) {
		tool := validStdioTool("no-journal-tool")
		system, stop := testActorSystemForTools(t)
		defer stop()

		// No journal spawned — supervisor must stop itself during PostStart.
		name := naming.ToolSupervisorName(tool.ID)
		pid, err := system.Spawn(ctx, name, NewToolSupervisor())
		require.NoError(t, err)
		waitForActors()

		assert.False(t, pid.IsRunning())
	})

	t.Run("stops when registrar is not running at PostStart", func(t *testing.T) {
		tool := validStdioTool("no-registrar-tool")
		system, stop := testActorSystemForTools(t)
		defer stop()

		_, err := system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
		require.NoError(t, err)

		// No registrar spawned — the supervisor cannot resolve its tool
		// config and must stop itself during PostStart.
		name := naming.ToolSupervisorName(tool.ID)
		pid, err := system.Spawn(ctx, name, NewToolSupervisor())
		require.NoError(t, err)
		waitForActors()

		assert.False(t, pid.IsRunning())
	})

	t.Run("stops when tool is not registered with the registrar", func(t *testing.T) {
		system, stop := testActorSystemForTools(t)
		defer stop()

		// Registrar running but with an empty catalog.
		spawnToolRuntimeForTest(t, ctx, system)

		tool := validStdioTool("unregistered-tool")
		name := naming.ToolSupervisorName(tool.ID)
		pid, err := system.Spawn(ctx, name, NewToolSupervisor())
		require.NoError(t, err)
		waitForActors()

		assert.False(t, pid.IsRunning())
	})

	t.Run("RefreshToolConfig installs the tool definition carried on the message", func(t *testing.T) {
		tool := validStdioTool("refresh-tool")
		tool.RequestTimeout = 10 * time.Second
		_, pid, stop := spawnTestSupervisor(t, tool)
		defer stop()

		// Verify supervisor accepts work initially
		resp, err := goaktactor.Ask(ctx, pid, &runtime.CanAcceptWork{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.CanAcceptWorkResult)
		require.True(t, ok)
		assert.True(t, result.Accept)

		// Send RefreshToolConfig carrying the disabled definition
		disabledTool := tool
		disabledTool.State = mcp.ToolStateDisabled
		require.NoError(t, goaktactor.Tell(ctx, pid, &runtime.RefreshToolConfig{Tool: disabledTool}))
		waitForActors()

		// Verify supervisor now rejects work because tool is disabled
		resp, err = goaktactor.Ask(ctx, pid, &runtime.CanAcceptWork{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)
		result, ok = resp.(*runtime.CanAcceptWorkResult)
		require.True(t, ok)
		assert.False(t, result.Accept)
		assert.Contains(t, result.Reason, "disabled")
	})

	t.Run("RefreshToolConfig with mismatched tool ID is a no-op", func(t *testing.T) {
		tool := validStdioTool("refresh-noop-tool")
		_, pid, stop := spawnTestSupervisor(t, tool)
		defer stop()

		// A definition for a different tool must be ignored.
		otherTool := validStdioTool("nonexistent")
		otherTool.State = mcp.ToolStateDisabled
		require.NoError(t, goaktactor.Tell(ctx, pid, &runtime.RefreshToolConfig{Tool: otherTool}))
		waitForActors()

		// Supervisor should still accept work with the original tool
		resp, err := goaktactor.Ask(ctx, pid, &runtime.CanAcceptWork{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.CanAcceptWorkResult)
		require.True(t, ok)
		assert.True(t, result.Accept)
	})

	t.Run("circuit open records CircuitState metric when metrics are registered", func(t *testing.T) {
		meter := noopmetric.NewMeterProvider().Meter("test")
		_, err := telemetry.RegisterMetrics(meter)
		require.NoError(t, err)
		t.Cleanup(telemetry.UnregisterMetrics)

		tool := validStdioTool("metrics-circuit-tool")
		_, pid, stop := spawnTestSupervisor(t, tool)
		defer stop()

		for i := 0; i < mcp.DefaultCircuitFailureThreshold; i++ {
			require.NoError(t, goaktactor.Tell(ctx, pid, &runtime.ReportFailure{ToolID: tool.ID}))
		}
		waitForActors()
	})

	t.Run("circuit open emits audit event when journal is running", func(t *testing.T) {
		tool := validStdioTool("circuit-audit-tool")
		sink := audit.NewMemorySink()
		cfg := testConfig()
		cfg.Audit.Sink = sink
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(actorextension.NewConfigExtension(cfg)))
		defer stop()

		spawnToolRuntimeForTest(t, ctx, system, tool)
		pid := supervisorPIDForTest(t, ctx, system, tool.ID)

		for i := 0; i < mcp.DefaultCircuitFailureThreshold; i++ {
			require.NoError(t, goaktactor.Tell(ctx, pid, &runtime.ReportFailure{ToolID: tool.ID}))
		}
		waitForActors()

		events := sink.Events()
		require.NotEmpty(t, events, "expected circuit state change audit event")
		var circuitEvent *mcp.AuditEvent
		for _, e := range events {
			if e.Type == mcp.AuditEventTypeCircuitStateChange {
				circuitEvent = e
				break
			}
		}
		require.NotNil(t, circuitEvent)
		assert.Equal(t, string(tool.ID), circuitEvent.ToolID)
		assert.Equal(t, string(mcp.CircuitOpen), circuitEvent.Outcome)
	})
}

func TestToolSupervisorGetToolStatus(t *testing.T) {
	ctx := context.Background()

	t.Run("returns closed circuit and not draining for fresh supervisor", func(t *testing.T) {
		tool := validStdioTool("admin-status-tool")
		_, pid, stop := spawnTestSupervisor(t, tool)
		defer stop()

		resp, err := goaktactor.Ask(ctx, pid, &runtime.GetToolStatus{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.GetToolStatusResult)
		require.True(t, ok)
		assert.Nil(t, result.Err)
		assert.Equal(t, tool.ID, result.Status.ToolID)
		assert.Equal(t, mcp.ToolStateEnabled, result.Status.State)
		assert.Equal(t, mcp.CircuitClosed, result.Status.Circuit)
		assert.Zero(t, result.Status.SessionCount)
		assert.False(t, result.Status.Draining)
	})

	t.Run("returns error for tool ID mismatch", func(t *testing.T) {
		tool := validStdioTool("admin-mismatch-tool")
		_, pid, stop := spawnTestSupervisor(t, tool)
		defer stop()

		resp, err := goaktactor.Ask(ctx, pid, &runtime.GetToolStatus{ToolID: "other-tool"}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.GetToolStatusResult)
		require.True(t, ok)
		require.Error(t, result.Err)
	})
}

func TestToolSupervisorResetCircuit(t *testing.T) {
	ctx := context.Background()

	t.Run("resets open circuit to closed", func(t *testing.T) {
		tool := validStdioTool("admin-reset-tool")
		_, pid, stop := spawnTestSupervisor(t, tool)
		defer stop()

		// Trip the circuit.
		for i := 0; i < mcp.DefaultCircuitFailureThreshold; i++ {
			require.NoError(t, goaktactor.Tell(ctx, pid, &runtime.ReportFailure{ToolID: tool.ID}))
		}
		waitForActors()

		// Confirm circuit is open.
		resp, err := goaktactor.Ask(ctx, pid, &runtime.CanAcceptWork{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)
		canAccept, ok := resp.(*runtime.CanAcceptWorkResult)
		require.True(t, ok)
		assert.False(t, canAccept.Accept)

		// Reset circuit.
		resp, err = goaktactor.Ask(ctx, pid, &runtime.ResetCircuit{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)
		resetResult, ok := resp.(*runtime.ResetCircuitResult)
		require.True(t, ok)
		assert.Nil(t, resetResult.Err)

		// Circuit must now be closed.
		resp, err = goaktactor.Ask(ctx, pid, &runtime.CanAcceptWork{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)
		canAccept2, ok := resp.(*runtime.CanAcceptWorkResult)
		require.True(t, ok)
		assert.True(t, canAccept2.Accept)
	})

	t.Run("returns error for tool ID mismatch", func(t *testing.T) {
		tool := validStdioTool("admin-reset-mismatch")
		_, pid, stop := spawnTestSupervisor(t, tool)
		defer stop()

		resp, err := goaktactor.Ask(ctx, pid, &runtime.ResetCircuit{ToolID: "other-tool"}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.ResetCircuitResult)
		require.True(t, ok)
		require.Error(t, result.Err)
	})
}

func TestToolSupervisorDrainTool(t *testing.T) {
	ctx := context.Background()

	t.Run("sets draining and rejects new work", func(t *testing.T) {
		tool := validStdioTool("admin-drain-tool")
		_, pid, stop := spawnTestSupervisor(t, tool)
		defer stop()

		resp, err := goaktactor.Ask(ctx, pid, &runtime.DrainTool{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)
		drainResult, ok := resp.(*runtime.DrainToolResult)
		require.True(t, ok)
		assert.Nil(t, drainResult.Err)

		// CanAcceptWork must be rejected with draining reason.
		resp, err = goaktactor.Ask(ctx, pid, &runtime.CanAcceptWork{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)
		canAccept, ok := resp.(*runtime.CanAcceptWorkResult)
		require.True(t, ok)
		assert.False(t, canAccept.Accept)
		assert.Contains(t, canAccept.Reason, "draining")
	})

	t.Run("returns error for tool ID mismatch", func(t *testing.T) {
		tool := validStdioTool("admin-drain-mismatch")
		_, pid, stop := spawnTestSupervisor(t, tool)
		defer stop()

		resp, err := goaktactor.Ask(ctx, pid, &runtime.DrainTool{ToolID: "other-tool"}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.DrainToolResult)
		require.True(t, ok)
		require.Error(t, result.Err)
	})

	t.Run("EnableTool via RefreshToolConfig lifts the drain", func(t *testing.T) {
		tool := validStdioTool("drain-then-enable-tool")
		_, pid, stop := spawnTestSupervisor(t, tool)
		defer stop()

		// Drain the tool.
		resp, err := goaktactor.Ask(ctx, pid, &runtime.DrainTool{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)
		drainResult, ok := resp.(*runtime.DrainToolResult)
		require.True(t, ok)
		assert.Nil(t, drainResult.Err)

		// Confirm it is draining.
		resp, err = goaktactor.Ask(ctx, pid, &runtime.CanAcceptWork{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)
		canAccept, ok := resp.(*runtime.CanAcceptWorkResult)
		require.True(t, ok)
		assert.False(t, canAccept.Accept)

		// Simulate EnableTool: send RefreshToolConfig with the enabled definition.
		enabledTool := tool
		enabledTool.State = mcp.ToolStateEnabled
		require.NoError(t, goaktactor.Tell(ctx, pid, &runtime.RefreshToolConfig{Tool: enabledTool}))
		waitForActors()

		// Drain must be lifted.
		resp, err = goaktactor.Ask(ctx, pid, &runtime.CanAcceptWork{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)
		canAccept, ok = resp.(*runtime.CanAcceptWorkResult)
		require.True(t, ok)
		assert.True(t, canAccept.Accept)
	})
}

func TestToolSupervisorSessionLifecycle(t *testing.T) {
	ctx := context.Background()

	t.Run("session activations and deactivations drive the session count", func(t *testing.T) {
		tool := validStdioTool("session-count-tool")
		_, pid, stop := spawnTestSupervisor(t, tool)
		defer stop()

		require.NoError(t, goaktactor.Tell(ctx, pid, &runtime.SessionActivated{
			ToolID: tool.ID, TenantID: "tenant-1", ClientID: "client-1",
		}))
		require.NoError(t, goaktactor.Tell(ctx, pid, &runtime.SessionActivated{
			ToolID: tool.ID, TenantID: "tenant-1", ClientID: "client-2",
		}))
		waitForActors()

		resp, err := goaktactor.Ask(ctx, pid, &runtime.GetToolStatus{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.GetToolStatusResult)
		require.True(t, ok)
		assert.Equal(t, 2, result.Status.SessionCount)

		require.NoError(t, goaktactor.Tell(ctx, pid, &runtime.SessionDeactivated{
			ToolID: tool.ID, TenantID: "tenant-1", ClientID: "client-1",
		}))
		waitForActors()

		resp, err = goaktactor.Ask(ctx, pid, &runtime.GetToolStatus{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)
		result, ok = resp.(*runtime.GetToolStatusResult)
		require.True(t, ok)
		assert.Equal(t, 1, result.Status.SessionCount)
	})

	t.Run("lifecycle messages for other tools are ignored", func(t *testing.T) {
		tool := validStdioTool("session-ignore-tool")
		_, pid, stop := spawnTestSupervisor(t, tool)
		defer stop()

		require.NoError(t, goaktactor.Tell(ctx, pid, &runtime.SessionActivated{
			ToolID: "other-tool", TenantID: "tenant-1", ClientID: "client-1",
		}))
		waitForActors()

		resp, err := goaktactor.Ask(ctx, pid, &runtime.GetToolStatus{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.GetToolStatusResult)
		require.True(t, ok)
		assert.Zero(t, result.Status.SessionCount)
	})
}

func TestPolicyActorCustomEvaluator(t *testing.T) {
	ctx := context.Background()

	t.Run("custom deny evaluator blocks invocations that pass built-in checks", func(t *testing.T) {
		cfg := testConfig()
		cfg.Tenants = []mcp.TenantConfig{
			{
				ID:        "tenant-1",
				Evaluator: &testDenyEvaluator{reason: "custom rule rejected"},
			},
		}

		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(actorextension.NewConfigExtension(cfg)),
		)
		defer stop()
		pid, err := system.Spawn(ctx, naming.ActorNamePolicy, newPolicyMaker())
		require.NoError(t, err)
		waitForActors()

		tool := validStdioTool("custom-eval-tool")
		in := &policy.Input{
			Invocation: sessionInvocation(tool.ID, "tenant-1", "client-1"),
			Tool:       tool,
			TenantID:   "tenant-1",
			ClientID:   "client-1",
		}
		resp, err := goaktactor.Ask(ctx, pid, &policy.EvaluateRequest{Input: in}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*policy.EvaluateResult)
		require.True(t, ok)
		assert.False(t, result.Result.Allowed())
		assert.Contains(t, result.Result.Reason, "custom rule rejected")
	})

	t.Run("custom allow evaluator passes invocations that pass built-in checks", func(t *testing.T) {
		cfg := testConfig()
		cfg.Tenants = []mcp.TenantConfig{
			{
				ID:        "tenant-2",
				Evaluator: &testAllowEvaluator{},
			},
		}

		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(actorextension.NewConfigExtension(cfg)),
		)
		defer stop()
		pid, err := system.Spawn(ctx, naming.ActorNamePolicy, newPolicyMaker())
		require.NoError(t, err)
		waitForActors()

		tool := validStdioTool("custom-allow-tool")
		in := &policy.Input{
			Invocation: sessionInvocation(tool.ID, "tenant-2", "client-2"),
			Tool:       tool,
			TenantID:   "tenant-2",
			ClientID:   "client-2",
		}
		resp, err := goaktactor.Ask(ctx, pid, &policy.EvaluateRequest{Input: in}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*policy.EvaluateResult)
		require.True(t, ok)
		assert.True(t, result.Result.Allowed())
	})

	t.Run("nil evaluator field is a no-op", func(t *testing.T) {
		cfg := testConfig()
		cfg.Tenants = []mcp.TenantConfig{
			{ID: "tenant-3", Evaluator: nil},
		}

		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(actorextension.NewConfigExtension(cfg)),
		)
		defer stop()
		pid, err := system.Spawn(ctx, naming.ActorNamePolicy, newPolicyMaker())
		require.NoError(t, err)
		waitForActors()

		tool := validStdioTool("nil-eval-tool")
		in := &policy.Input{
			Invocation: sessionInvocation(tool.ID, "tenant-3", "client-3"),
			Tool:       tool,
			TenantID:   "tenant-3",
			ClientID:   "client-3",
		}
		resp, err := goaktactor.Ask(ctx, pid, &policy.EvaluateRequest{Input: in}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*policy.EvaluateResult)
		require.True(t, ok)
		assert.True(t, result.Result.Allowed())
	})
}
