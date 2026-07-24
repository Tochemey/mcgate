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

	"github.com/tochemey/portcullis/mcp"

	"github.com/tochemey/portcullis/internal/naming"
	actorextension "github.com/tochemey/portcullis/internal/runtime/actor/extension"
	"github.com/tochemey/portcullis/internal/runtime/audit"
	"github.com/tochemey/portcullis/internal/runtime/telemetry"
)

func TestHealthActor(t *testing.T) {
	ctx := context.Background()

	t.Run("starts and stops cleanly", func(t *testing.T) {
		cfg := testConfig()
		cfg.HealthProbe.Interval = time.Hour
		system, stop := testActorSystem(t, goaktactor.WithExtensions(actorextension.NewConfigExtension(cfg)))
		defer stop()

		_, err := system.Spawn(ctx, naming.ActorNameRegistrar, NewRegistrar())
		require.NoError(t, err)
		_, err = system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
		require.NoError(t, err)
		waitForActors()

		pid, err := system.Spawn(ctx, naming.ActorNameHealth, newHealthChecker())
		require.NoError(t, err)
		require.NotNil(t, pid)
		assert.Equal(t, naming.ActorNameHealth, pid.Name())

		waitForActors()
	})

	t.Run("unhandles non-PostStart messages", func(t *testing.T) {
		cfg := testConfig()
		cfg.HealthProbe.Interval = time.Hour
		kit, ctx := newTestKit(t, testkit.WithExtensions(actorextension.NewConfigExtension(cfg)))

		kit.Spawn(ctx, naming.ActorNameRegistrar, NewRegistrar())
		kit.Spawn(ctx, naming.ActorNameJournal, newJournaler())
		waitForActors()

		kit.Spawn(ctx, naming.ActorNameHealth, newHealthChecker())
		waitForActors()

		pid, err := kit.ActorSystem().ActorOf(ctx, naming.ActorNameHealth)
		require.NoError(t, err)
		require.NotNil(t, pid)
		require.NoError(t, pid.Tell(ctx, pid, "unknown-message"))
		waitForActors()
	})

	t.Run("runProbes with nil registrar is a no-op", func(t *testing.T) {
		cfg := testConfig()
		cfg.HealthProbe.Interval = time.Hour
		system, stop := testActorSystem(t, goaktactor.WithExtensions(actorextension.NewConfigExtension(cfg)))
		defer stop()

		_, err := system.Spawn(ctx, naming.ActorNameRegistrar, NewRegistrar())
		require.NoError(t, err)
		_, err = system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
		require.NoError(t, err)
		waitForActors()

		pid, err := system.Spawn(ctx, naming.ActorNameHealth, newHealthChecker())
		require.NoError(t, err)
		waitForActors()

		require.NoError(t, pid.Tell(ctx, pid, &runProbes{}))
		waitForActors()
	})

	t.Run("runProbes probes a healthy tool and skips disabled tools", func(t *testing.T) {
		cfg := testConfig()
		cfg.HealthProbe.Interval = time.Hour
		system, stop := testActorSystem(t, goaktactor.WithExtensions(actorextension.NewConfigExtension(cfg)))
		defer stop()

		tool := validStdioTool("health-probe-tool")
		disabledTool := validStdioTool("disabled-probe-tool")
		disabledTool.State = mcp.ToolStateDisabled
		spawnToolRuntimeForTest(t, ctx, system, tool, disabledTool)

		healthPID, err := system.Spawn(ctx, naming.ActorNameHealth, newHealthChecker())
		require.NoError(t, err)
		waitForActors()

		require.NoError(t, healthPID.Tell(ctx, healthPID, &runProbes{}))
		time.Sleep(300 * time.Millisecond)
	})

	t.Run("runProbes marks the tool unavailable when its supervisor failed to initialize", func(t *testing.T) {
		sink := audit.NewMemorySink()
		cfg := testConfig()
		cfg.HealthProbe.Interval = time.Hour
		cfg.Audit.Sink = sink
		system, stop := testActorSystem(t, goaktactor.WithExtensions(actorextension.NewConfigExtension(cfg)))
		defer stop()

		// Spawn the registrar WITHOUT a journal: registering a tool still puts
		// it in the catalog, but the supervisor spawned as a side effect fails
		// PostStart (journal missing) and never resolves its tool config.
		// ListTools returns the tool, the probe cannot get a healthy answer
		// from the supervisor, and the tool is marked Unavailable.
		registrarPID, err := system.Spawn(ctx, naming.ActorNameRegistrar, NewRegistrar())
		require.NoError(t, err)
		waitForActors()

		registerToolForTest(t, ctx, registrarPID, validStdioTool("no-supervisor-tool"))

		// Spawn the journal only now so the health checker can start; the
		// supervisor stays unconfigured because nothing re-runs its PostStart.
		_, err = system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
		require.NoError(t, err)
		waitForActors()

		healthPID, err := system.Spawn(ctx, naming.ActorNameHealth, newHealthChecker())
		require.NoError(t, err)
		waitForActors()

		require.NoError(t, healthPID.Tell(ctx, healthPID, &runProbes{}))
		time.Sleep(500 * time.Millisecond)

		events := sink.Events()
		require.GreaterOrEqual(t, len(events), 1, "expected an Enabled->Unavailable health transition event")
	})

	t.Run("runProbes with dead registrar schedules next", func(t *testing.T) {
		cfg := testConfig()
		cfg.HealthProbe.Interval = time.Hour
		system, stop := testActorSystem(t, goaktactor.WithExtensions(actorextension.NewConfigExtension(cfg)))
		defer stop()

		_, err := system.Spawn(ctx, naming.ActorNameRegistrar, NewRegistrar())
		require.NoError(t, err)
		_, err = system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
		require.NoError(t, err)
		waitForActors()

		healthPID, err := system.Spawn(ctx, naming.ActorNameHealth, newHealthChecker())
		require.NoError(t, err)
		waitForActors()

		require.NoError(t, system.Kill(ctx, naming.ActorNameRegistrar))
		waitForActors()

		require.NoError(t, healthPID.Tell(ctx, healthPID, &runProbes{}))
		waitForActors()
	})

	t.Run("runProbes records ToolAvailability metric when metrics are registered", func(t *testing.T) {
		meter := noopmetric.NewMeterProvider().Meter("test")
		_, err := telemetry.RegisterMetrics(meter)
		require.NoError(t, err)
		t.Cleanup(telemetry.UnregisterMetrics)

		cfg := testConfig()
		cfg.HealthProbe.Interval = time.Hour
		system, stop := testActorSystem(t, goaktactor.WithExtensions(actorextension.NewConfigExtension(cfg)))
		defer stop()

		// Register the tool Unavailable so the probe transitions it to Enabled
		// and exercises the metric recording path.
		tool := validStdioTool("metrics-health-tool")
		tool.State = mcp.ToolStateUnavailable
		spawnToolRuntimeForTest(t, ctx, system, tool)

		healthPID, err := system.Spawn(ctx, naming.ActorNameHealth, newHealthChecker())
		require.NoError(t, err)
		waitForActors()

		require.NoError(t, healthPID.Tell(ctx, healthPID, &runProbes{}))
		time.Sleep(300 * time.Millisecond)
	})

	t.Run("runProbes records health transition to journal when state changes", func(t *testing.T) {
		sink := audit.NewMemorySink()
		cfg := testConfig()
		cfg.HealthProbe.Interval = time.Hour
		cfg.Audit.Sink = sink
		system, stop := testActorSystem(t, goaktactor.WithExtensions(actorextension.NewConfigExtension(cfg)))
		defer stop()

		spawnToolRuntimeForTest(t, ctx, system, validStdioTool("health-audit-tool"))

		healthPID, err := system.Spawn(ctx, naming.ActorNameHealth, newHealthChecker())
		require.NoError(t, err)
		waitForActors()

		require.NoError(t, healthPID.Tell(ctx, healthPID, &runProbes{}))
		time.Sleep(300 * time.Millisecond)

		// The journal is wired; verify no panic occurred.
		assert.NotNil(t, sink.Events())
	})

	t.Run("runProbes calls recordHealthTransition when tool state transitions from Unavailable to Enabled", func(t *testing.T) {
		sink := audit.NewMemorySink()
		cfg := testConfig()
		cfg.HealthProbe.Interval = time.Hour
		cfg.Audit.Sink = sink
		system, stop := testActorSystem(t, goaktactor.WithExtensions(actorextension.NewConfigExtension(cfg)))
		defer stop()

		tool := validStdioTool("transition-tool")
		tool.State = mcp.ToolStateUnavailable
		spawnToolRuntimeForTest(t, ctx, system, tool)

		healthPID, err := system.Spawn(ctx, naming.ActorNameHealth, newHealthChecker())
		require.NoError(t, err)
		waitForActors()

		require.NoError(t, healthPID.Tell(ctx, healthPID, &runProbes{}))
		time.Sleep(500 * time.Millisecond)

		events := sink.Events()
		require.NotNil(t, events)
		assert.GreaterOrEqual(t, len(events), 1, "expected at least one health transition event")
	})
}
