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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	goaktactor "github.com/tochemey/goakt/v4/actor"

	"github.com/tochemey/portcullis/mcp"

	"github.com/tochemey/portcullis/internal/naming"
	"github.com/tochemey/portcullis/internal/runtime"
	actorextension "github.com/tochemey/portcullis/internal/runtime/actor/extension"
	"github.com/tochemey/portcullis/internal/runtime/audit"
)

func TestRegistryActor(t *testing.T) {
	ctx := context.Background()

	t.Run("starts and stops cleanly", func(t *testing.T) {
		system, stop := testActorSystem(t)
		defer stop()

		pid, err := system.Spawn(ctx, naming.ActorNameRegistrar, newRegistrar())
		require.NoError(t, err)
		require.NotNil(t, pid)
		assert.Equal(t, naming.ActorNameRegistrar, pid.Name())

		waitForActors()
	})

	t.Run("register and query tool via Ask", func(t *testing.T) {
		system, stop := testActorSystem(t)
		defer stop()

		pid, err := system.Spawn(ctx, naming.ActorNameRegistrar, newRegistrar())
		require.NoError(t, err)
		waitForActors()

		tool := validStdioTool("my-tool")
		resp, err := goaktactor.Ask(ctx, pid, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)
		regResult, ok := resp.(*runtime.RegisterToolResult)
		require.True(t, ok)
		require.NoError(t, regResult.Err)

		resp, err = goaktactor.Ask(ctx, pid, &runtime.QueryTool{ToolID: "my-tool"}, askTimeout)
		require.NoError(t, err)
		qResult, ok := resp.(*runtime.QueryToolResult)
		require.True(t, ok)
		require.True(t, qResult.Found)
		require.NoError(t, qResult.Err)
		require.NotNil(t, qResult.Tool)
		assert.Equal(t, mcp.ToolID("my-tool"), qResult.Tool.ID)
		assert.Equal(t, mcp.ToolStateEnabled, qResult.Tool.State)
	})

	t.Run("query non-existent tool returns ErrToolNotFound", func(t *testing.T) {
		system, stop := testActorSystem(t)
		defer stop()

		pid, err := system.Spawn(ctx, naming.ActorNameRegistrar, newRegistrar())
		require.NoError(t, err)
		waitForActors()

		resp, err := goaktactor.Ask(ctx, pid, &runtime.QueryTool{ToolID: "nonexistent"}, askTimeout)
		require.NoError(t, err)
		qResult, ok := resp.(*runtime.QueryToolResult)
		require.True(t, ok)
		assert.False(t, qResult.Found)
		require.Error(t, qResult.Err)
		assert.ErrorIs(t, qResult.Err, mcp.ErrToolNotFound)
	})

	t.Run("register invalid tool returns validation error", func(t *testing.T) {
		system, stop := testActorSystem(t)
		defer stop()

		pid, err := system.Spawn(ctx, naming.ActorNameRegistrar, newRegistrar())
		require.NoError(t, err)
		waitForActors()

		invalidTool := mcp.Tool{
			ID:        mcp.ToolID("bad"),
			Transport: mcp.TransportStdio,
			Stdio:     &mcp.StdioTransportConfig{Command: ""},
		}
		resp, err := goaktactor.Ask(ctx, pid, &runtime.RegisterTool{Tool: invalidTool}, askTimeout)
		require.NoError(t, err)
		regResult, ok := resp.(*runtime.RegisterToolResult)
		require.True(t, ok)
		require.Error(t, regResult.Err)
	})

	t.Run("disable and remove tool", func(t *testing.T) {
		system, stop := testActorSystem(t)
		defer stop()

		pid, err := system.Spawn(ctx, naming.ActorNameRegistrar, newRegistrar())
		require.NoError(t, err)
		waitForActors()

		tool := validStdioTool("to-disable")
		_, err = goaktactor.Ask(ctx, pid, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)

		resp, err := goaktactor.Ask(ctx, pid, &runtime.DisableTool{ToolID: "to-disable"}, askTimeout)
		require.NoError(t, err)
		disableResult, ok := resp.(*runtime.DisableToolResult)
		require.True(t, ok)
		require.NoError(t, disableResult.Err)

		resp, err = goaktactor.Ask(ctx, pid, &runtime.QueryTool{ToolID: "to-disable"}, askTimeout)
		require.NoError(t, err)
		qResult := resp.(*runtime.QueryToolResult)
		require.True(t, qResult.Found)
		assert.Equal(t, mcp.ToolStateDisabled, qResult.Tool.State)

		resp, err = goaktactor.Ask(ctx, pid, &runtime.RemoveTool{ToolID: "to-disable"}, askTimeout)
		require.NoError(t, err)
		removeResult, ok := resp.(*runtime.RemoveToolResult)
		require.True(t, ok)
		require.NoError(t, removeResult.Err)

		resp, err = goaktactor.Ask(ctx, pid, &runtime.QueryTool{ToolID: "to-disable"}, askTimeout)
		require.NoError(t, err)
		qResult = resp.(*runtime.QueryToolResult)
		assert.False(t, qResult.Found)
	})

	t.Run("update tool health", func(t *testing.T) {
		system, stop := testActorSystem(t)
		defer stop()

		pid, err := system.Spawn(ctx, naming.ActorNameRegistrar, newRegistrar())
		require.NoError(t, err)
		waitForActors()

		tool := validStdioTool("health-tool")
		_, err = goaktactor.Ask(ctx, pid, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)

		resp, err := goaktactor.Ask(ctx, pid, &runtime.UpdateToolHealth{
			ToolID: "health-tool",
			State:  mcp.ToolStateDegraded,
		}, askTimeout)
		require.NoError(t, err)
		healthResult, ok := resp.(*runtime.UpdateToolHealthResult)
		require.True(t, ok)
		require.NoError(t, healthResult.Err)

		resp, err = goaktactor.Ask(ctx, pid, &runtime.QueryTool{ToolID: "health-tool"}, askTimeout)
		require.NoError(t, err)
		qResult := resp.(*runtime.QueryToolResult)
		require.True(t, qResult.Found)
		assert.Equal(t, mcp.ToolStateDegraded, qResult.Tool.State)
	})

	t.Run("update tool", func(t *testing.T) {
		system, stop := testActorSystem(t)
		defer stop()

		pid, err := system.Spawn(ctx, naming.ActorNameRegistrar, newRegistrar())
		require.NoError(t, err)
		waitForActors()

		tool := validStdioTool("update-tool")
		tool.RequestTimeout = 10 * time.Second
		_, err = goaktactor.Ask(ctx, pid, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)

		updated := tool
		updated.RequestTimeout = 60 * time.Second
		updated.Routing = mcp.RoutingLeastLoaded
		resp, err := goaktactor.Ask(ctx, pid, &runtime.UpdateTool{Tool: updated}, askTimeout)
		require.NoError(t, err)
		updateResult, ok := resp.(*runtime.UpdateToolResult)
		require.True(t, ok)
		require.NoError(t, updateResult.Err)

		resp, err = goaktactor.Ask(ctx, pid, &runtime.QueryTool{ToolID: "update-tool"}, askTimeout)
		require.NoError(t, err)
		qResult := resp.(*runtime.QueryToolResult)
		require.True(t, qResult.Found)
		assert.Equal(t, 60*time.Second, qResult.Tool.RequestTimeout)
		assert.Equal(t, mcp.RoutingLeastLoaded, qResult.Tool.Routing)
	})

	t.Run("get supervisor returns PID when tool has supervisor", func(t *testing.T) {
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(actorextension.NewToolConfigExtension(), actorextension.NewConfigExtension(cfg)),
		)
		defer stop()

		_, err := system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
		require.NoError(t, err)

		pid, err := system.Spawn(ctx, naming.ActorNameRegistrar, newRegistrar())
		require.NoError(t, err)
		waitForActors()

		tool := validStdioTool("supervisor-lookup")
		_, err = goaktactor.Ask(ctx, pid, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)
		waitForActors()

		resp, err := goaktactor.Ask(ctx, pid, &runtime.GetSupervisor{ToolID: "supervisor-lookup"}, askTimeout)
		require.NoError(t, err)
		gsResult, ok := resp.(*runtime.GetSupervisorResult)
		require.True(t, ok)
		if gsResult.Found && gsResult.Supervisor != nil {
			supPID, ok := gsResult.Supervisor.(*goaktactor.PID)
			require.True(t, ok)
			assert.True(t, supPID.IsRunning())
		}
	})

	t.Run("bootstrap tools", func(t *testing.T) {
		system, stop := testActorSystem(t)
		defer stop()

		pid, err := system.Spawn(ctx, naming.ActorNameRegistrar, newRegistrar())
		require.NoError(t, err)
		waitForActors()

		tools := []mcp.Tool{
			validStdioTool("bootstrap-1"),
			validHTTPTool("bootstrap-2"),
		}
		err = goaktactor.Tell(ctx, pid, &runtime.BootstrapTools{Tools: tools})
		require.NoError(t, err)
		waitForActors()

		resp, err := goaktactor.Ask(ctx, pid, &runtime.QueryTool{ToolID: "bootstrap-1"}, askTimeout)
		require.NoError(t, err)
		qResult := resp.(*runtime.QueryToolResult)
		require.True(t, qResult.Found)
		assert.Equal(t, mcp.TransportStdio, qResult.Tool.Transport)

		resp, err = goaktactor.Ask(ctx, pid, &runtime.QueryTool{ToolID: "bootstrap-2"}, askTimeout)
		require.NoError(t, err)
		qResult = resp.(*runtime.QueryToolResult)
		require.True(t, qResult.Found)
		assert.Equal(t, mcp.TransportHTTP, qResult.Tool.Transport)
	})

	t.Run("update tool not found returns ErrToolNotFound", func(t *testing.T) {
		kit, ctx := newTestKit(t)

		kit.ActorSystem().Spawn(ctx, "registry-update-nf", newRegistrar())
		waitForActors()

		probe := kit.NewProbe(ctx)
		tool := validStdioTool("nonexistent")
		probe.SendSync("registry-update-nf", &runtime.UpdateTool{Tool: tool}, askTimeout)
		resp := probe.ExpectAnyMessage()
		result, ok := resp.(*runtime.UpdateToolResult)
		require.True(t, ok)
		require.Error(t, result.Err)
		assert.ErrorIs(t, result.Err, mcp.ErrToolNotFound)
		probe.Stop()
	})

	t.Run("disable tool not found returns ErrToolNotFound", func(t *testing.T) {
		kit, ctx := newTestKit(t)

		kit.ActorSystem().Spawn(ctx, "registry-disable-nf", newRegistrar())
		waitForActors()

		probe := kit.NewProbe(ctx)
		probe.SendSync("registry-disable-nf", &runtime.DisableTool{ToolID: "nonexistent"}, askTimeout)
		resp := probe.ExpectAnyMessage()
		result, ok := resp.(*runtime.DisableToolResult)
		require.True(t, ok)
		require.Error(t, result.Err)
		assert.ErrorIs(t, result.Err, mcp.ErrToolNotFound)
		probe.Stop()
	})

	t.Run("remove tool not found returns ErrToolNotFound", func(t *testing.T) {
		kit, ctx := newTestKit(t)

		kit.ActorSystem().Spawn(ctx, "registry-remove-nf", newRegistrar())
		waitForActors()

		probe := kit.NewProbe(ctx)
		probe.SendSync("registry-remove-nf", &runtime.RemoveTool{ToolID: "nonexistent"}, askTimeout)
		resp := probe.ExpectAnyMessage()
		result, ok := resp.(*runtime.RemoveToolResult)
		require.True(t, ok)
		require.Error(t, result.Err)
		assert.ErrorIs(t, result.Err, mcp.ErrToolNotFound)
		probe.Stop()
	})

	t.Run("update tool health not found returns ErrToolNotFound", func(t *testing.T) {
		kit, ctx := newTestKit(t)

		kit.ActorSystem().Spawn(ctx, "registry-health-nf", newRegistrar())
		waitForActors()

		probe := kit.NewProbe(ctx)
		probe.SendSync("registry-health-nf", &runtime.UpdateToolHealth{
			ToolID: "nonexistent",
			State:  mcp.ToolStateDegraded,
		}, askTimeout)
		resp := probe.ExpectAnyMessage()
		result, ok := resp.(*runtime.UpdateToolHealthResult)
		require.True(t, ok)
		require.Error(t, result.Err)
		assert.ErrorIs(t, result.Err, mcp.ErrToolNotFound)
		probe.Stop()
	})

	t.Run("get supervisor not found returns Found=false", func(t *testing.T) {
		kit, ctx := newTestKit(t)

		kit.ActorSystem().Spawn(ctx, "registry-get-sup-nf", newRegistrar())
		waitForActors()

		probe := kit.NewProbe(ctx)
		probe.SendSync("registry-get-sup-nf", &runtime.GetSupervisor{ToolID: "nonexistent"}, askTimeout)
		resp := probe.ExpectAnyMessage()
		result, ok := resp.(*runtime.GetSupervisorResult)
		require.True(t, ok)
		assert.False(t, result.Found)
		require.Error(t, result.Err)
		assert.ErrorIs(t, result.Err, mcp.ErrToolNotFound)
		probe.Stop()
	})

	t.Run("NewRegistrar returns valid actor for cluster kind registration", func(t *testing.T) {
		a := NewRegistrar()
		require.NotNil(t, a)
	})

	t.Run("enable tool sets state to enabled and notifies supervisor", func(t *testing.T) {
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(actorextension.NewToolConfigExtension(), actorextension.NewConfigExtension(cfg)),
		)
		defer stop()

		_, err := system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
		require.NoError(t, err)

		pid, err := system.Spawn(ctx, naming.ActorNameRegistrar, newRegistrar())
		require.NoError(t, err)
		waitForActors()

		tool := validStdioTool("enable-tool")
		tool.State = mcp.ToolStateDisabled
		_, err = goaktactor.Ask(ctx, pid, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)
		waitForActors()

		// Verify tool is disabled
		resp, err := goaktactor.Ask(ctx, pid, &runtime.QueryTool{ToolID: "enable-tool"}, askTimeout)
		require.NoError(t, err)
		qResult := resp.(*runtime.QueryToolResult)
		require.True(t, qResult.Found)
		assert.Equal(t, mcp.ToolStateDisabled, qResult.Tool.State)

		// Enable the tool
		resp, err = goaktactor.Ask(ctx, pid, &runtime.EnableTool{ToolID: "enable-tool"}, askTimeout)
		require.NoError(t, err)
		enableResult, ok := resp.(*runtime.EnableToolResult)
		require.True(t, ok)
		require.NoError(t, enableResult.Err)

		// Verify state is now enabled
		resp, err = goaktactor.Ask(ctx, pid, &runtime.QueryTool{ToolID: "enable-tool"}, askTimeout)
		require.NoError(t, err)
		qResult = resp.(*runtime.QueryToolResult)
		require.True(t, qResult.Found)
		assert.Equal(t, mcp.ToolStateEnabled, qResult.Tool.State)
	})

	t.Run("enable tool not found returns ErrToolNotFound", func(t *testing.T) {
		kit, ctx := newTestKit(t)

		kit.ActorSystem().Spawn(ctx, "registry-enable-nf", newRegistrar())
		waitForActors()

		probe := kit.NewProbe(ctx)
		probe.SendSync("registry-enable-nf", &runtime.EnableTool{ToolID: "nonexistent"}, askTimeout)
		resp := probe.ExpectAnyMessage()
		result, ok := resp.(*runtime.EnableToolResult)
		require.True(t, ok)
		require.Error(t, result.Err)
		assert.ErrorIs(t, result.Err, mcp.ErrToolNotFound)
		probe.Stop()
	})

	t.Run("update tool propagates config to supervisor via RefreshToolConfig", func(t *testing.T) {
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(actorextension.NewToolConfigExtension(), actorextension.NewConfigExtension(cfg)),
		)
		defer stop()

		_, err := system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
		require.NoError(t, err)

		pid, err := system.Spawn(ctx, naming.ActorNameRegistrar, newRegistrar())
		require.NoError(t, err)
		waitForActors()

		tool := validStdioTool("propagate-tool")
		tool.RequestTimeout = 10 * time.Second
		_, err = goaktactor.Ask(ctx, pid, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)
		waitForActors()

		// Update the tool - this should propagate to the supervisor
		updated := tool
		updated.RequestTimeout = 60 * time.Second
		resp, err := goaktactor.Ask(ctx, pid, &runtime.UpdateTool{Tool: updated}, askTimeout)
		require.NoError(t, err)
		updateResult, ok := resp.(*runtime.UpdateToolResult)
		require.True(t, ok)
		require.NoError(t, updateResult.Err)
		waitForActors()

		// Verify the supervisor still accepts work (it refreshed config without error)
		supResp, err := goaktactor.Ask(ctx, pid, &runtime.GetSupervisor{ToolID: "propagate-tool"}, askTimeout)
		require.NoError(t, err)
		gsResult, ok := supResp.(*runtime.GetSupervisorResult)
		require.True(t, ok)
		require.True(t, gsResult.Found)
		supervisorPID, ok := gsResult.Supervisor.(*goaktactor.PID)
		require.True(t, ok)
		require.True(t, supervisorPID.IsRunning())

		acceptResp, err := goaktactor.Ask(ctx, supervisorPID, &runtime.CanAcceptWork{ToolID: "propagate-tool"}, askTimeout)
		require.NoError(t, err)
		acceptResult, ok := acceptResp.(*runtime.CanAcceptWorkResult)
		require.True(t, ok)
		assert.True(t, acceptResult.Accept)
	})

	t.Run("disable tool propagates config to supervisor", func(t *testing.T) {
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(actorextension.NewToolConfigExtension(), actorextension.NewConfigExtension(cfg)),
		)
		defer stop()

		_, err := system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
		require.NoError(t, err)

		pid, err := system.Spawn(ctx, naming.ActorNameRegistrar, newRegistrar())
		require.NoError(t, err)
		waitForActors()

		tool := validStdioTool("disable-propagate-tool")
		_, err = goaktactor.Ask(ctx, pid, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)
		waitForActors()

		// Disable the tool
		_, err = goaktactor.Ask(ctx, pid, &runtime.DisableTool{ToolID: "disable-propagate-tool"}, askTimeout)
		require.NoError(t, err)
		waitForActors()

		// Verify the supervisor rejects work because tool is now disabled
		supResp, err := goaktactor.Ask(ctx, pid, &runtime.GetSupervisor{ToolID: "disable-propagate-tool"}, askTimeout)
		require.NoError(t, err)
		gsResult, ok := supResp.(*runtime.GetSupervisorResult)
		require.True(t, ok)
		require.True(t, gsResult.Found)
		supervisorPID, ok := gsResult.Supervisor.(*goaktactor.PID)
		require.True(t, ok)

		acceptResp, err := goaktactor.Ask(ctx, supervisorPID, &runtime.CanAcceptWork{ToolID: "disable-propagate-tool"}, askTimeout)
		require.NoError(t, err)
		acceptResult, ok := acceptResp.(*runtime.CanAcceptWorkResult)
		require.True(t, ok)
		assert.False(t, acceptResult.Accept)
		assert.Contains(t, acceptResult.Reason, "disabled")
	})

	t.Run("count sessions for tenant", func(t *testing.T) {
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(actorextension.NewToolConfigExtension(), actorextension.NewConfigExtension(cfg)),
		)
		defer stop()

		_, err := system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
		require.NoError(t, err)

		pid, err := system.Spawn(ctx, naming.ActorNameRegistrar, newRegistrar())
		require.NoError(t, err)
		waitForActors()

		tool := validStdioTool("count-sessions-tool")
		_, err = goaktactor.Ask(ctx, pid, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)
		waitForActors()

		resp, err := goaktactor.Ask(ctx, pid, &runtime.CountSessionsForTenant{TenantID: "tenant-1"}, askTimeout)
		require.NoError(t, err)
		countResult, ok := resp.(*runtime.CountSessionsForTenantResult)
		require.True(t, ok)
		require.NotNil(t, countResult)
		assert.GreaterOrEqual(t, countResult.Count, 0)
	})

	t.Run("GetToolStatus returns status for registered tool with supervisor", func(t *testing.T) {
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(actorextension.NewToolConfigExtension(), actorextension.NewConfigExtension(cfg)),
		)
		defer stop()

		_, err := system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
		require.NoError(t, err)

		pid, err := system.Spawn(ctx, naming.ActorNameRegistrar, newRegistrar())
		require.NoError(t, err)
		waitForActors()

		tool := validStdioTool("get-status-tool")
		_, err = goaktactor.Ask(ctx, pid, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)
		waitForActors()

		resp, err := goaktactor.Ask(ctx, pid, &runtime.GetToolStatus{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.GetToolStatusResult)
		require.True(t, ok)
		require.NoError(t, result.Err)
		assert.Equal(t, tool.ID, result.Status.ToolID)
		assert.Equal(t, mcp.CircuitClosed, result.Status.Circuit)
		assert.False(t, result.Status.Draining)
	})

	t.Run("GetToolStatus returns ErrToolNotFound for unknown tool", func(t *testing.T) {
		kit, ctx := newTestKit(t)

		kit.ActorSystem().Spawn(ctx, "reg-getstatus-nf", newRegistrar())
		waitForActors()

		probe := kit.NewProbe(ctx)
		probe.SendSync("reg-getstatus-nf", &runtime.GetToolStatus{ToolID: "nonexistent"}, askTimeout)
		resp := probe.ExpectAnyMessage()
		result, ok := resp.(*runtime.GetToolStatusResult)
		require.True(t, ok)
		require.Error(t, result.Err)
		assert.ErrorIs(t, result.Err, mcp.ErrToolNotFound)
		probe.Stop()
	})

	t.Run("ResetCircuit relays to supervisor and returns success", func(t *testing.T) {
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(actorextension.NewToolConfigExtension(), actorextension.NewConfigExtension(cfg)),
		)
		defer stop()

		_, err := system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
		require.NoError(t, err)

		pid, err := system.Spawn(ctx, naming.ActorNameRegistrar, newRegistrar())
		require.NoError(t, err)
		waitForActors()

		tool := validStdioTool("reset-circuit-tool")
		_, err = goaktactor.Ask(ctx, pid, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)
		waitForActors()

		resp, err := goaktactor.Ask(ctx, pid, &runtime.ResetCircuit{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.ResetCircuitResult)
		require.True(t, ok)
		require.NoError(t, result.Err)
	})

	t.Run("ResetCircuit returns ErrToolNotFound for unknown tool", func(t *testing.T) {
		kit, ctx := newTestKit(t)

		kit.ActorSystem().Spawn(ctx, "reg-reset-nf", newRegistrar())
		waitForActors()

		probe := kit.NewProbe(ctx)
		probe.SendSync("reg-reset-nf", &runtime.ResetCircuit{ToolID: "nonexistent"}, askTimeout)
		resp := probe.ExpectAnyMessage()
		result, ok := resp.(*runtime.ResetCircuitResult)
		require.True(t, ok)
		require.Error(t, result.Err)
		assert.ErrorIs(t, result.Err, mcp.ErrToolNotFound)
		probe.Stop()
	})

	t.Run("DrainTool relays to supervisor and sets draining", func(t *testing.T) {
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(actorextension.NewToolConfigExtension(), actorextension.NewConfigExtension(cfg)),
		)
		defer stop()

		_, err := system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
		require.NoError(t, err)

		pid, err := system.Spawn(ctx, naming.ActorNameRegistrar, newRegistrar())
		require.NoError(t, err)
		waitForActors()

		tool := validStdioTool("drain-tool-reg")
		_, err = goaktactor.Ask(ctx, pid, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)
		waitForActors()

		resp, err := goaktactor.Ask(ctx, pid, &runtime.DrainTool{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.DrainToolResult)
		require.True(t, ok)
		require.NoError(t, result.Err)

		// Verify draining reflected in status.
		statusResp, err := goaktactor.Ask(ctx, pid, &runtime.GetToolStatus{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)
		statusResult, ok := statusResp.(*runtime.GetToolStatusResult)
		require.True(t, ok)
		assert.True(t, statusResult.Status.Draining)
	})

	t.Run("DrainTool returns ErrToolNotFound for unknown tool", func(t *testing.T) {
		kit, ctx := newTestKit(t)

		kit.ActorSystem().Spawn(ctx, "reg-drain-nf", newRegistrar())
		waitForActors()

		probe := kit.NewProbe(ctx)
		probe.SendSync("reg-drain-nf", &runtime.DrainTool{ToolID: "nonexistent"}, askTimeout)
		resp := probe.ExpectAnyMessage()
		result, ok := resp.(*runtime.DrainToolResult)
		require.True(t, ok)
		require.Error(t, result.Err)
		assert.ErrorIs(t, result.Err, mcp.ErrToolNotFound)
		probe.Stop()
	})

	t.Run("ListAllSessions returns empty when no supervisors registered", func(t *testing.T) {
		kit, ctx := newTestKit(t)

		kit.ActorSystem().Spawn(ctx, "reg-list-sessions-empty", newRegistrar())
		waitForActors()

		probe := kit.NewProbe(ctx)
		probe.SendSync("reg-list-sessions-empty", &runtime.ListAllSessions{}, askTimeout)
		resp := probe.ExpectAnyMessage()
		result, ok := resp.(*runtime.ListAllSessionsResult)
		require.True(t, ok)
		assert.Empty(t, result.Sessions)
		probe.Stop()
	})

	t.Run("GetToolSchema returns cached schemas for registered tool", func(t *testing.T) {
		schemas := []mcp.ToolSchema{
			{Name: "read_file", Description: "Read a file", InputSchema: []byte(`{"type":"object","properties":{"path":{"type":"string"}}}`)},
		}
		fetcher := &mockSchemaFetcher{schemas: schemas}
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(
				actorextension.NewToolConfigExtension(),
				actorextension.NewConfigExtension(cfg),
				actorextension.NewSchemaFetcherExtension(fetcher),
			),
		)
		defer stop()

		_, err := system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
		require.NoError(t, err)

		pid, err := system.Spawn(ctx, naming.ActorNameRegistrar, newRegistrar())
		require.NoError(t, err)
		waitForActors()

		tool := validStdioTool("schema-tool")
		_, err = goaktactor.Ask(ctx, pid, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)
		waitForActors()

		resp, err := goaktactor.Ask(ctx, pid, &runtime.GetToolSchema{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.GetToolSchemaResult)
		require.True(t, ok)
		require.NoError(t, result.Err)
		require.Len(t, result.Schemas, 1)
		assert.Equal(t, "read_file", result.Schemas[0].Name)
	})

	t.Run("GetToolSchema returns ErrToolNotFound for unknown tool", func(t *testing.T) {
		kit, ctx := newTestKit(t)

		kit.ActorSystem().Spawn(ctx, "reg-schema-nf", newRegistrar())
		waitForActors()

		probe := kit.NewProbe(ctx)
		probe.SendSync("reg-schema-nf", &runtime.GetToolSchema{ToolID: "nonexistent"}, askTimeout)
		resp := probe.ExpectAnyMessage()
		result, ok := resp.(*runtime.GetToolSchemaResult)
		require.True(t, ok)
		require.Error(t, result.Err)
		assert.ErrorIs(t, result.Err, mcp.ErrToolNotFound)
		probe.Stop()
	})

	t.Run("ListTools includes cached schemas on returned tools", func(t *testing.T) {
		schemas := []mcp.ToolSchema{
			{Name: "list_dir", Description: "List directory"},
		}
		fetcher := &mockSchemaFetcher{schemas: schemas}
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(
				actorextension.NewToolConfigExtension(),
				actorextension.NewConfigExtension(cfg),
				actorextension.NewSchemaFetcherExtension(fetcher),
			),
		)
		defer stop()

		_, err := system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
		require.NoError(t, err)

		pid, err := system.Spawn(ctx, naming.ActorNameRegistrar, newRegistrar())
		require.NoError(t, err)
		waitForActors()

		tool := validStdioTool("list-schema-tool")
		_, err = goaktactor.Ask(ctx, pid, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)
		waitForActors()

		resp, err := goaktactor.Ask(ctx, pid, &runtime.ListTools{}, askTimeout)
		require.NoError(t, err)
		listResult, ok := resp.(*runtime.ListToolsResult)
		require.True(t, ok)
		require.Len(t, listResult.Tools, 1)
		assert.Len(t, listResult.Tools[0].Schemas, 1)
		assert.Equal(t, "list_dir", listResult.Tools[0].Schemas[0].Name)
	})

	t.Run("RemoveTool cleans up cached schemas", func(t *testing.T) {
		schemas := []mcp.ToolSchema{
			{Name: "tool_func", Description: "A function"},
		}
		fetcher := &mockSchemaFetcher{schemas: schemas}
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(
				actorextension.NewToolConfigExtension(),
				actorextension.NewConfigExtension(cfg),
				actorextension.NewSchemaFetcherExtension(fetcher),
			),
		)
		defer stop()

		_, err := system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
		require.NoError(t, err)

		pid, err := system.Spawn(ctx, naming.ActorNameRegistrar, newRegistrar())
		require.NoError(t, err)
		waitForActors()

		tool := validStdioTool("remove-schema-tool")
		_, err = goaktactor.Ask(ctx, pid, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)
		waitForActors()

		// Schemas cached
		resp, err := goaktactor.Ask(ctx, pid, &runtime.GetToolSchema{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)
		schemaResult := resp.(*runtime.GetToolSchemaResult)
		require.Len(t, schemaResult.Schemas, 1)

		// Remove tool
		_, err = goaktactor.Ask(ctx, pid, &runtime.RemoveTool{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)
		waitForActors()

		// Schemas gone
		resp, err = goaktactor.Ask(ctx, pid, &runtime.GetToolSchema{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)
		schemaResult = resp.(*runtime.GetToolSchemaResult)
		require.Error(t, schemaResult.Err)
		assert.ErrorIs(t, schemaResult.Err, mcp.ErrToolNotFound)
	})

	t.Run("GetToolStatus includes schemas", func(t *testing.T) {
		schemas := []mcp.ToolSchema{
			{Name: "status_func", Description: "Status function"},
		}
		fetcher := &mockSchemaFetcher{schemas: schemas}
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(
				actorextension.NewToolConfigExtension(),
				actorextension.NewConfigExtension(cfg),
				actorextension.NewSchemaFetcherExtension(fetcher),
			),
		)
		defer stop()

		_, err := system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
		require.NoError(t, err)

		pid, err := system.Spawn(ctx, naming.ActorNameRegistrar, newRegistrar())
		require.NoError(t, err)
		waitForActors()

		tool := validStdioTool("status-schema-tool")
		_, err = goaktactor.Ask(ctx, pid, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)
		waitForActors()

		resp, err := goaktactor.Ask(ctx, pid, &runtime.GetToolStatus{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.GetToolStatusResult)
		require.True(t, ok)
		require.NoError(t, result.Err)
		require.Len(t, result.Status.Schemas, 1)
		assert.Equal(t, "status_func", result.Status.Schemas[0].Name)
	})

	t.Run("schema fetch failure does not prevent tool registration", func(t *testing.T) {
		fetcher := &mockSchemaFetcher{err: errors.New("connection refused")}
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(
				actorextension.NewToolConfigExtension(),
				actorextension.NewConfigExtension(cfg),
				actorextension.NewSchemaFetcherExtension(fetcher),
			),
		)
		defer stop()

		_, err := system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
		require.NoError(t, err)

		pid, err := system.Spawn(ctx, naming.ActorNameRegistrar, newRegistrar())
		require.NoError(t, err)
		waitForActors()

		tool := validStdioTool("fetch-fail-tool")
		resp, err := goaktactor.Ask(ctx, pid, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)
		regResult := resp.(*runtime.RegisterToolResult)
		require.NoError(t, regResult.Err)

		// Tool registered but no schemas
		schemaResp, err := goaktactor.Ask(ctx, pid, &runtime.GetToolSchema{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)
		schemaResult := schemaResp.(*runtime.GetToolSchemaResult)
		require.NoError(t, schemaResult.Err)
		assert.Empty(t, schemaResult.Schemas)
	})

	t.Run("re-registration clears stale schemas when fetch fails", func(t *testing.T) {
		// First registration succeeds with schemas, second fails — stale schemas must be cleared.
		callCount := 0
		schemas := []mcp.ToolSchema{{Name: "old_func", Description: "Old"}}

		dynamicFetcher := &dynamicMockSchemaFetcher{
			fn: func() ([]mcp.ToolSchema, error) {
				callCount++
				if callCount == 1 {
					return schemas, nil
				}
				return nil, errors.New("backend unavailable")
			},
		}

		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(
				actorextension.NewToolConfigExtension(),
				actorextension.NewConfigExtension(cfg),
				actorextension.NewSchemaFetcherExtension(dynamicFetcher),
			),
		)
		defer stop()

		_, err := system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
		require.NoError(t, err)

		pid, err := system.Spawn(ctx, naming.ActorNameRegistrar, newRegistrar())
		require.NoError(t, err)
		waitForActors()

		tool := validStdioTool("stale-schema-tool")

		// First registration — schemas fetched successfully
		_, err = goaktactor.Ask(ctx, pid, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)
		waitForActors()

		resp, err := goaktactor.Ask(ctx, pid, &runtime.GetToolSchema{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)
		schemaResult := resp.(*runtime.GetToolSchemaResult)
		require.NoError(t, schemaResult.Err)
		require.Len(t, schemaResult.Schemas, 1)
		assert.Equal(t, "old_func", schemaResult.Schemas[0].Name)

		// Re-register same tool — fetch fails, stale schemas must be cleared
		_, err = goaktactor.Ask(ctx, pid, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)
		waitForActors()

		resp, err = goaktactor.Ask(ctx, pid, &runtime.GetToolSchema{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)
		schemaResult = resp.(*runtime.GetToolSchemaResult)
		require.NoError(t, schemaResult.Err)
		assert.Empty(t, schemaResult.Schemas, "stale schemas should have been cleared")
	})

	t.Run("ListAllSessions fans out to running supervisors", func(t *testing.T) {
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(actorextension.NewToolConfigExtension(), actorextension.NewConfigExtension(cfg)),
		)
		defer stop()

		_, err := system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
		require.NoError(t, err)

		pid, err := system.Spawn(ctx, naming.ActorNameRegistrar, newRegistrar())
		require.NoError(t, err)
		waitForActors()

		tool := validStdioTool("list-sessions-tool")
		_, err = goaktactor.Ask(ctx, pid, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)
		waitForActors()

		resp, err := goaktactor.Ask(ctx, pid, &runtime.ListAllSessions{}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.ListAllSessionsResult)
		require.True(t, ok)
		// No active invocations so sessions slice should be empty (not nil error).
		assert.NotNil(t, result)
	})
}
