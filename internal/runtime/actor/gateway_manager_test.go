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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	goaktactor "github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/testkit"

	"github.com/tochemey/goakt-mcp/mcp"

	"github.com/tochemey/goakt-mcp/internal/naming"
	"github.com/tochemey/goakt-mcp/internal/runtime/actor/extension"
	"github.com/tochemey/goakt-mcp/internal/runtime/audit"
)

func TestGatewayManager(t *testing.T) {
	t.Run("spawns as a top-level actor successfully", func(t *testing.T) {
		ctx := t.Context()
		config := testConfig()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(extension.NewConfigExtension(config)))
		defer stop()

		pid, err := system.Spawn(ctx, naming.ActorNameGatewayManager, NewGatewayManager())
		require.NoError(t, err)
		require.NotNil(t, pid)
		assert.Equal(t, naming.ActorNameGatewayManager, pid.Name())
	})

	t.Run("spawns foundational children on PostStart", func(t *testing.T) {
		ctx := t.Context()
		config := testConfig()
		system, stop := testActorSystem(t, goaktactor.WithExtensions(extension.NewConfigExtension(config)))
		defer stop()

		pid, err := system.Spawn(ctx, naming.ActorNameGatewayManager, NewGatewayManager())
		require.NoError(t, err)

		waitForActors()

		children := pid.Children()
		childNames := make(map[string]bool, len(children))
		for _, c := range children {
			childNames[c.Name()] = true
		}

		assert.True(t, childNames[naming.ActorNameRegistrar], "RegistryActor must be spawned")
		assert.True(t, childNames[naming.ActorNameHealth], "HealthActor must be spawned")
		assert.True(t, childNames[naming.ActorNameJournal], "JournalActor must be spawned")
		assert.True(t, childNames[naming.ActorNamePolicy], "PolicyActor must be spawned")
		assert.True(t, childNames[naming.ActorNameCredentialBroker], "CredentialBrokerActor must be spawned")

		poolSize := config.Runtime.RouterPoolSize
		if poolSize <= 0 {
			poolSize = mcp.DefaultRouterPoolSize
		}
		for i := range poolSize {
			assert.True(t, childNames[naming.RouterName(i)], "router %d of the pool must be spawned", i)
		}
		assert.Len(t, children, 5+poolSize, "GatewayManager must spawn the five foundational actors plus the router pool")
	})

	t.Run("unhandles unknown message", func(t *testing.T) {
		config := testConfig()
		kit, ctx := newTestKit(t, testkit.WithExtensions(extension.NewConfigExtension(config)))

		pid, err := kit.ActorSystem().Spawn(ctx, naming.ActorNameGatewayManager, NewGatewayManager())
		require.NoError(t, err)
		waitForActors()
		require.NoError(t, pid.Tell(ctx, pid, "unknown"))
		waitForActors()
	})

	t.Run("createAuditSink returns sink when configured", func(t *testing.T) {
		memSink := audit.NewMemorySink()
		cfg := mcp.AuditConfig{Sink: memSink}
		sink := createAuditSink(cfg)
		require.NotNil(t, sink)
		_ = sink.Close()
	})
}

func TestCreateAuditSink(t *testing.T) {
	t.Run("createAuditSink with FileSink returns the sink", func(t *testing.T) {
		tmpDir := t.TempDir()
		fileSink, err := audit.NewFileSink(tmpDir)
		require.NoError(t, err)
		cfg := mcp.AuditConfig{Sink: fileSink}
		sink := createAuditSink(cfg)
		require.NotNil(t, sink)
		_ = sink.Close()
	})

	t.Run("createAuditSink with nil Sink returns default MemorySink", func(t *testing.T) {
		cfg := mcp.AuditConfig{}
		sink := createAuditSink(cfg)
		require.NotNil(t, sink)
		_ = sink.Close()
	})

	t.Run("createAuditSink with custom sink returns it", func(t *testing.T) {
		custom := &failingAuditSink{}
		cfg := mcp.AuditConfig{Sink: custom}
		sink := createAuditSink(cfg)
		require.NotNil(t, sink)
		assert.Equal(t, custom, sink)
	})
}

func TestHasConcurrencyQuotas(t *testing.T) {
	t.Run("hasConcurrencyQuotas returns true when tenant has concurrent sessions", func(t *testing.T) {
		cfg := mcp.Config{
			Tenants: []mcp.TenantConfig{
				{ID: "t1", Quotas: mcp.TenantQuotaConfig{ConcurrentSessions: 10}},
			},
		}
		assert.True(t, hasConcurrencyQuotas(cfg))
	})

	t.Run("hasConcurrencyQuotas returns false with no tenants", func(t *testing.T) {
		cfg := mcp.Config{}
		assert.False(t, hasConcurrencyQuotas(cfg))
	})

	t.Run("hasConcurrencyQuotas returns false when all quotas are zero", func(t *testing.T) {
		cfg := mcp.Config{
			Tenants: []mcp.TenantConfig{
				{ID: "t1", Quotas: mcp.TenantQuotaConfig{RequestsPerMinute: 100}},
			},
		}
		assert.False(t, hasConcurrencyQuotas(cfg))
	})
}

func TestExternalTestHelpers(t *testing.T) {
	t.Run("ExternalTestConfig returns valid config", func(t *testing.T) {
		cfg := ExternalTestConfig()
		assert.Equal(t, mcp.DefaultSessionIdleTimeout, cfg.Runtime.SessionIdleTimeout)
		assert.Equal(t, mcp.DefaultRequestTimeout, cfg.Runtime.RequestTimeout)
		assert.Equal(t, mcp.DefaultStartupTimeout, cfg.Runtime.StartupTimeout)
		assert.NotNil(t, cfg.Audit.Sink)
	})

	t.Run("SpawnFoundationalActorsForExternalTest spawns all required actors", func(t *testing.T) {
		cfg := ExternalTestConfig()
		kit, ctx := newTestKit(t, testkit.WithExtensions(
			extension.NewToolConfigExtension(),
			extension.NewConfigExtension(cfg),
		))

		SpawnFoundationalActorsForExternalTest(ctx, kit.ActorSystem())

		pid, err := kit.ActorSystem().ActorOf(ctx, naming.ActorNameRegistrar)
		require.NoError(t, err)
		assert.True(t, pid.IsRunning())

		pid, err = kit.ActorSystem().ActorOf(ctx, naming.RouterName(0))
		require.NoError(t, err)
		assert.True(t, pid.IsRunning())
	})
}
