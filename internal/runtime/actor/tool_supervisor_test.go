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
	"github.com/tochemey/goakt/v4/testkit"
	noopmetric "go.opentelemetry.io/otel/metric/noop"

	"github.com/tochemey/goakt-mcp/mcp"

	"github.com/tochemey/goakt-mcp/internal/naming"
	"github.com/tochemey/goakt-mcp/internal/runtime"
	actorextension "github.com/tochemey/goakt-mcp/internal/runtime/actor/extension"
	"github.com/tochemey/goakt-mcp/internal/runtime/audit"
	"github.com/tochemey/goakt-mcp/internal/runtime/policy"
	"github.com/tochemey/goakt-mcp/internal/runtime/telemetry"
)

func TestToolSupervisorActor(t *testing.T) {
	ctx := context.Background()

	t.Run("resolves tool from extension and accepts work when circuit closed", func(t *testing.T) {
		tool := validStdioTool("supervisor-tool")
		system, stop := testActorSystemWithTools(t, tool)
		defer stop()

		_, err := system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
		require.NoError(t, err)

		name := naming.ToolSupervisorName(tool.ID)
		pid, err := system.Spawn(ctx, name, newToolSupervisor())
		require.NoError(t, err)
		require.NotNil(t, pid)

		waitForActors()

		resp, err := goaktactor.Ask(ctx, pid, &runtime.CanAcceptWork{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.CanAcceptWorkResult)
		require.True(t, ok)
		assert.True(t, result.Accept)
	})

	t.Run("rejects work when circuit opened after failures", func(t *testing.T) {
		tool := validStdioTool("circuit-tool")
		system, stop := testActorSystemWithTools(t, tool)
		defer stop()

		_, err := system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
		require.NoError(t, err)

		name := naming.ToolSupervisorName(tool.ID)
		pid, err := system.Spawn(ctx, name, newToolSupervisor())
		require.NoError(t, err)
		waitForActors()

		for i := 0; i < mcp.DefaultCircuitFailureThreshold; i++ {
			err = goaktactor.Tell(ctx, pid, &runtime.ReportFailure{ToolID: tool.ID})
			require.NoError(t, err)
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
		system, stop := testActorSystemWithTools(t, tool)
		defer stop()

		_, err := system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
		require.NoError(t, err)

		name := naming.ToolSupervisorName(tool.ID)
		pid, err := system.Spawn(ctx, name, newToolSupervisor())
		require.NoError(t, err)
		waitForActors()

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
		toolCfgExt := actorextension.NewToolConfigExtension()
		toolCfgExt.Register(tool)
		circuitCfgExt := actorextension.NewCircuitConfigExtension(circuitCfg)
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		kit, ctx := newTestKit(t,
			testkit.WithExtensions(toolCfgExt, circuitCfgExt, actorextension.NewConfigExtension(cfg)),
		)

		kit.ActorSystem().Spawn(ctx, naming.ActorNameJournal, newJournaler())

		name := naming.ToolSupervisorName(tool.ID)
		kit.ActorSystem().Spawn(ctx, name, newToolSupervisor())
		waitForActors()

		probe := kit.NewProbe(ctx)
		for i := 0; i < mcp.DefaultCircuitFailureThreshold; i++ {
			probe.Send(name, &runtime.ReportFailure{ToolID: tool.ID})
		}
		waitForActors()

		time.Sleep(150 * time.Millisecond)

		probe.SendSync(name, &runtime.CanAcceptWork{ToolID: tool.ID}, askTimeout)
		resp := probe.ExpectAnyMessage()
		result, ok := resp.(*runtime.CanAcceptWorkResult)
		require.True(t, ok)
		assert.True(t, result.Accept)

		probe.Send(name, &runtime.ReportSuccess{ToolID: tool.ID})
		waitForActors()

		probe.SendSync(name, &runtime.CanAcceptWork{ToolID: tool.ID}, askTimeout)
		resp2 := probe.ExpectAnyMessage()
		result2, ok := resp2.(*runtime.CanAcceptWorkResult)
		require.True(t, ok)
		assert.True(t, result2.Accept)
		probe.Stop()
	})

	t.Run("report failure in half-open reopens circuit", func(t *testing.T) {
		tool := validStdioTool("reopen-tool")
		circuitCfg := mcp.CircuitConfig{
			FailureThreshold:    mcp.DefaultCircuitFailureThreshold,
			OpenDuration:        100 * time.Millisecond,
			HalfOpenMaxRequests: mcp.DefaultCircuitHalfOpenMaxRequests,
		}
		toolCfgExt := actorextension.NewToolConfigExtension()
		toolCfgExt.Register(tool)
		circuitCfgExt := actorextension.NewCircuitConfigExtension(circuitCfg)
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		kit, ctx := newTestKit(t,
			testkit.WithExtensions(toolCfgExt, circuitCfgExt, actorextension.NewConfigExtension(cfg)),
		)

		kit.ActorSystem().Spawn(ctx, naming.ActorNameJournal, newJournaler())

		name := naming.ToolSupervisorName(tool.ID)
		kit.ActorSystem().Spawn(ctx, name, newToolSupervisor())
		waitForActors()

		pid, err := kit.ActorSystem().ActorOf(ctx, name)
		require.NoError(t, err)

		for i := 0; i < mcp.DefaultCircuitFailureThreshold; i++ {
			require.NoError(t, pid.Tell(ctx, pid, &runtime.ReportFailure{ToolID: tool.ID}))
		}
		waitForActors()

		time.Sleep(150 * time.Millisecond)

		probe := kit.NewProbe(ctx)
		probe.SendSync(name, &runtime.CanAcceptWork{ToolID: tool.ID}, askTimeout)
		probe.ExpectAnyMessage()

		pid.Tell(ctx, pid, &runtime.ReportFailure{ToolID: tool.ID})
		time.Sleep(30 * time.Millisecond)

		probe.SendSync(name, &runtime.CanAcceptWork{ToolID: tool.ID}, askTimeout)
		resp := probe.ExpectAnyMessage()
		result, ok := resp.(*runtime.CanAcceptWorkResult)
		require.True(t, ok)
		assert.False(t, result.Accept)
		probe.Stop()
	})

	t.Run("report success with wrong tool ID is ignored", func(t *testing.T) {
		tool := validStdioTool("success-mismatch")
		toolCfgExt := actorextension.NewToolConfigExtension()
		toolCfgExt.Register(tool)
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		kit, ctx := newTestKit(t, testkit.WithExtensions(toolCfgExt, actorextension.NewConfigExtension(cfg)))

		kit.ActorSystem().Spawn(ctx, naming.ActorNameJournal, newJournaler())

		name := naming.ToolSupervisorName(tool.ID)
		kit.ActorSystem().Spawn(ctx, name, newToolSupervisor())
		waitForActors()

		pid, _ := kit.ActorSystem().ActorOf(ctx, name)
		require.NoError(t, pid.Tell(ctx, pid, &runtime.ReportSuccess{ToolID: "wrong-tool"}))
		waitForActors()

		probe := kit.NewProbe(ctx)
		probe.SendSync(name, &runtime.CanAcceptWork{ToolID: tool.ID}, askTimeout)
		resp := probe.ExpectAnyMessage()
		result, ok := resp.(*runtime.CanAcceptWorkResult)
		require.True(t, ok)
		assert.True(t, result.Accept)
		probe.Stop()
	})

	t.Run("stops when journal is not running at PostStart", func(t *testing.T) {
		tool := validStdioTool("no-journal-tool")
		system, stop := testActorSystemWithTools(t, tool)
		defer stop()

		// No journal spawned — supervisor must stop itself during PostStart.
		name := naming.ToolSupervisorName(tool.ID)
		pid, err := system.Spawn(ctx, name, newToolSupervisor())
		require.NoError(t, err)
		waitForActors()

		assert.False(t, pid.IsRunning())
	})

	t.Run("stops when tool config extension is absent", func(t *testing.T) {
		// System created WITHOUT ToolConfigExtension — supervisor must stop itself.
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t, goaktactor.WithExtensions(actorextension.NewConfigExtension(cfg)))
		defer stop()

		_, err := system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
		require.NoError(t, err)

		tool := validStdioTool("no-ext-tool")
		name := naming.ToolSupervisorName(tool.ID)
		pid, err := system.Spawn(ctx, name, newToolSupervisor())
		require.NoError(t, err)
		waitForActors()

		assert.False(t, pid.IsRunning())
	})

	t.Run("stops when tool is not registered in extension", func(t *testing.T) {
		// ToolConfigExtension registered but empty (tool never registered).
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(actorextension.NewToolConfigExtension(), actorextension.NewConfigExtension(cfg)),
		)
		defer stop()

		_, err := system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
		require.NoError(t, err)

		tool := validStdioTool("unregistered-tool")
		name := naming.ToolSupervisorName(tool.ID)
		pid, err := system.Spawn(ctx, name, newToolSupervisor())
		require.NoError(t, err)
		waitForActors()

		assert.False(t, pid.IsRunning())
	})

	t.Run("RefreshToolConfig reloads tool definition from extension", func(t *testing.T) {
		tool := validStdioTool("refresh-tool")
		tool.RequestTimeout = 10 * time.Second
		toolCfgExt := actorextension.NewToolConfigExtension()
		toolCfgExt.Register(tool)
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(toolCfgExt, actorextension.NewConfigExtension(cfg)),
		)
		defer stop()

		_, err := system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
		require.NoError(t, err)

		name := naming.ToolSupervisorName(tool.ID)
		pid, err := system.Spawn(ctx, name, newToolSupervisor())
		require.NoError(t, err)
		waitForActors()

		// Verify supervisor accepts work initially
		resp, err := goaktactor.Ask(ctx, pid, &runtime.CanAcceptWork{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.CanAcceptWorkResult)
		require.True(t, ok)
		assert.True(t, result.Accept)

		// Update the tool in the extension to disabled state
		disabledTool := tool
		disabledTool.State = mcp.ToolStateDisabled
		toolCfgExt.Register(disabledTool)

		// Send RefreshToolConfig to the supervisor
		err = goaktactor.Tell(ctx, pid, &runtime.RefreshToolConfig{ToolID: tool.ID})
		require.NoError(t, err)
		waitForActors()

		// Verify supervisor now rejects work because tool is disabled
		resp, err = goaktactor.Ask(ctx, pid, &runtime.CanAcceptWork{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)
		result, ok = resp.(*runtime.CanAcceptWorkResult)
		require.True(t, ok)
		assert.False(t, result.Accept)
		assert.Contains(t, result.Reason, "disabled")
	})

	t.Run("RefreshToolConfig with missing tool is no-op", func(t *testing.T) {
		tool := validStdioTool("refresh-noop-tool")
		toolCfgExt := actorextension.NewToolConfigExtension()
		toolCfgExt.Register(tool)
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(toolCfgExt, actorextension.NewConfigExtension(cfg)),
		)
		defer stop()

		_, err := system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
		require.NoError(t, err)

		name := naming.ToolSupervisorName(tool.ID)
		pid, err := system.Spawn(ctx, name, newToolSupervisor())
		require.NoError(t, err)
		waitForActors()

		// Send RefreshToolConfig with a non-existent tool ID — should be a no-op
		err = goaktactor.Tell(ctx, pid, &runtime.RefreshToolConfig{ToolID: "nonexistent"})
		require.NoError(t, err)
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
		toolCfgExt := actorextension.NewToolConfigExtension()
		toolCfgExt.Register(tool)
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t, goaktactor.WithExtensions(toolCfgExt, actorextension.NewConfigExtension(cfg)))
		defer stop()

		_, err = system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
		require.NoError(t, err)

		name := naming.ToolSupervisorName(tool.ID)
		pid, err := system.Spawn(ctx, name, newToolSupervisor())
		require.NoError(t, err)
		waitForActors()

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
		toolCfgExt := actorextension.NewToolConfigExtension()
		toolCfgExt.Register(tool)
		system, stop := testActorSystem(t, goaktactor.WithExtensions(toolCfgExt, actorextension.NewConfigExtension(cfg)))
		defer stop()

		_, err := system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
		require.NoError(t, err)
		waitForActors()

		name := naming.ToolSupervisorName(tool.ID)
		pid, err := system.Spawn(ctx, name, newToolSupervisor())
		require.NoError(t, err)
		waitForActors()

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
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		toolCfgExt := actorextension.NewToolConfigExtension()
		toolCfgExt.Register(tool)
		system, stop := testActorSystem(t, goaktactor.WithExtensions(toolCfgExt, actorextension.NewConfigExtension(cfg)))
		defer stop()

		_, err := system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
		require.NoError(t, err)
		name := naming.ToolSupervisorName(tool.ID)
		pid, err := system.Spawn(ctx, name, newToolSupervisor())
		require.NoError(t, err)
		waitForActors()

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
		toolCfgExt := actorextension.NewToolConfigExtension()
		toolCfgExt.Register(tool)
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(toolCfgExt, actorextension.NewConfigExtension(cfg)),
		)
		defer stop()

		_, err := system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
		require.NoError(t, err)

		pid, err := system.Spawn(ctx, naming.ToolSupervisorName(tool.ID), newToolSupervisor())
		require.NoError(t, err)
		waitForActors()

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

		// Simulate EnableTool: update extension to enabled state and send RefreshToolConfig.
		enabledTool := tool
		enabledTool.State = mcp.ToolStateEnabled
		toolCfgExt.Register(enabledTool)
		err = goaktactor.Tell(ctx, pid, &runtime.RefreshToolConfig{ToolID: tool.ID})
		require.NoError(t, err)
		waitForActors()

		// Drain must be lifted.
		resp, err = goaktactor.Ask(ctx, pid, &runtime.CanAcceptWork{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)
		canAccept, ok = resp.(*runtime.CanAcceptWorkResult)
		require.True(t, ok)
		assert.True(t, canAccept.Accept)
	})
}

func TestToolSupervisorGetOrCreateSession(t *testing.T) {
	ctx := context.Background()

	t.Run("activates session grain and returns identity", func(t *testing.T) {
		tool := validStdioTool("session-create-tool")
		_, pid, stop := spawnTestSupervisor(t, tool)
		defer stop()

		resp, err := goaktactor.Ask(ctx, pid, &runtime.GetOrCreateSession{
			TenantID: "tenant-1",
			ClientID: "client-1",
			ToolID:   tool.ID,
		}, askTimeout)
		require.NoError(t, err)

		result, ok := resp.(*runtime.GetOrCreateSessionResult)
		require.True(t, ok)
		require.NoError(t, result.Err)
		require.True(t, result.Found)
		require.NotNil(t, result.Session)

		identity, ok := result.Session.(*goaktactor.GrainIdentity)
		require.True(t, ok, "expected *goaktactor.GrainIdentity, got %T", result.Session)
		assert.Equal(t, naming.SessionName("tenant-1", "client-1", tool.ID), identity.Name())
	})

	t.Run("returns same grain identity on repeat call", func(t *testing.T) {
		tool := validStdioTool("session-reuse-tool")
		_, pid, stop := spawnTestSupervisor(t, tool)
		defer stop()

		msg := &runtime.GetOrCreateSession{
			TenantID: "tenant-1",
			ClientID: "client-1",
			ToolID:   tool.ID,
		}

		resp1, err := goaktactor.Ask(ctx, pid, msg, askTimeout)
		require.NoError(t, err)
		result1 := resp1.(*runtime.GetOrCreateSessionResult)
		require.NoError(t, result1.Err)
		require.True(t, result1.Found)
		waitForActors()

		resp2, err := goaktactor.Ask(ctx, pid, msg, askTimeout)
		require.NoError(t, err)
		result2 := resp2.(*runtime.GetOrCreateSessionResult)
		require.True(t, result2.Found)

		id1 := result1.Session.(*goaktactor.GrainIdentity)
		id2 := result2.Session.(*goaktactor.GrainIdentity)
		assert.True(t, id1.Equal(id2), "expected identical grain identity on repeat call")
	})

	t.Run("rejects tool ID mismatch", func(t *testing.T) {
		tool := validStdioTool("session-mismatch-tool")
		_, pid, stop := spawnTestSupervisor(t, tool)
		defer stop()

		resp, err := goaktactor.Ask(ctx, pid, &runtime.GetOrCreateSession{
			TenantID: "tenant-1",
			ClientID: "client-1",
			ToolID:   "wrong-tool",
		}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.GetOrCreateSessionResult)
		require.True(t, ok)
		require.Error(t, result.Err)
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
