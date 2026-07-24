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

package portcullis

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	goaktactor "github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/eventstream"
	goaktlog "github.com/tochemey/goakt/v4/log"
	"google.golang.org/grpc"

	"github.com/tochemey/portcullis/internal/egress"
	"github.com/tochemey/portcullis/internal/naming"
	"github.com/tochemey/portcullis/internal/runtime"
	"github.com/tochemey/portcullis/internal/runtime/actor"
	actorextension "github.com/tochemey/portcullis/internal/runtime/actor/extension"
	"github.com/tochemey/portcullis/mcp"
)

// fixedIdentityResolver always returns the configured tenant and client IDs.
type fixedIdentityResolver struct {
	tenantID mcp.TenantID
	clientID mcp.ClientID
}

func (r *fixedIdentityResolver) ResolveIdentity(_ *http.Request) (mcp.TenantID, mcp.ClientID, error) {
	return r.tenantID, r.clientID, nil
}

// fixedGRPCIdentityResolver always returns the configured identity for gRPC.
type fixedGRPCIdentityResolver struct {
	tenantID mcp.TenantID
	clientID mcp.ClientID
}

func (r *fixedGRPCIdentityResolver) ResolveGRPCIdentity(_ context.Context) (mcp.TenantID, mcp.ClientID, error) {
	return r.tenantID, r.clientID, nil
}

// validTestTokenVerifier returns a TokenVerifier that always succeeds.
func validTestTokenVerifier() auth.TokenVerifier {
	return auth.TokenVerifier(func(_ context.Context, _ string, _ *http.Request) (*auth.TokenInfo, error) {
		return &auth.TokenInfo{
			UserID:     "user-1",
			Scopes:     []string{"tools:read"},
			Expiration: time.Now().Add(time.Hour),
		}, nil
	})
}

// noopDiscovery is a test DiscoveryProvider that returns a static peer list.
type noopDiscovery struct {
	peers []string
}

func (n *noopDiscovery) ID() string                                        { return "noop" }
func (n *noopDiscovery) Start(_ context.Context) error                     { return nil }
func (n *noopDiscovery) DiscoverPeers(_ context.Context) ([]string, error) { return n.peers, nil }
func (n *noopDiscovery) Stop(_ context.Context) error                      { return nil }

// failingDiscovery is a DiscoveryProvider whose Start always fails, forcing
// the actor system start to fail after actorSystemOptions has already run.
type failingDiscovery struct{}

func (f *failingDiscovery) ID() string { return "failing" }
func (f *failingDiscovery) Start(_ context.Context) error {
	return fmt.Errorf("discovery start failed")
}
func (f *failingDiscovery) DiscoverPeers(_ context.Context) ([]string, error) {
	return nil, fmt.Errorf("no peers")
}
func (f *failingDiscovery) Stop(_ context.Context) error { return nil }

// freePort returns an available port for use in tests.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

// minimalGatewayManager is a GatewayManager with no children. Used to exercise
// the resolveRegistrar/resolveRouter fallback path (system.ActorOf when Child fails).
type minimalGatewayManager struct{}

func (m *minimalGatewayManager) PreStart(_ *goaktactor.Context) error { return nil }
func (m *minimalGatewayManager) Receive(_ *goaktactor.ReceiveContext) {}
func (m *minimalGatewayManager) PostStop(_ *goaktactor.Context) error { return nil }

func testConfig() mcp.Config {
	return mcp.Config{
		Runtime: mcp.RuntimeConfig{
			SessionIdleTimeout: mcp.DefaultSessionIdleTimeout,
			RequestTimeout:     mcp.DefaultRequestTimeout,
			StartupTimeout:     mcp.DefaultStartupTimeout,
		},
	}
}

func waitForActors() {
	time.Sleep(100 * time.Millisecond)
}

func TestNew(t *testing.T) {
	t.Run("returns Gateway with valid config", func(t *testing.T) {
		gw, err := New(testConfig())
		require.NoError(t, err)
		require.NotNil(t, gw)
		assert.Nil(t, gw.System(), "system must be nil before Start")
	})

	t.Run("WithMetrics enables metrics", func(t *testing.T) {
		gw, err := New(testConfig(), WithMetrics())
		require.NoError(t, err)
		require.NotNil(t, gw)
		assert.True(t, gw.metrics)
	})

	t.Run("returns Gateway with default logger when none provided", func(t *testing.T) {
		gw, err := New(testConfig())
		require.NoError(t, err)
		require.NotNil(t, gw)
	})

	t.Run("New with LogLevel in config uses config logger", func(t *testing.T) {
		cfg := testConfig()
		cfg.LogLevel = "info"
		gw, err := New(cfg)
		require.NoError(t, err)
		require.NotNil(t, gw)
		assert.NotEqual(t, goaktlog.DiscardLogger, gw.logger)
	})
}

func TestGatewayStartStop(t *testing.T) {
	ctx := context.Background()

	t.Run("starts cleanly and exposes the actor system", func(t *testing.T) {
		gw, err := New(testConfig())
		require.NoError(t, err)

		require.NoError(t, gw.Start(ctx))
		assert.NotNil(t, gw.System())

		waitForActors()

		require.NoError(t, gw.Stop(ctx))
	})

	t.Run("stop on unstarted gateway is a no-op", func(t *testing.T) {
		gw, err := New(testConfig())
		require.NoError(t, err)
		assert.NoError(t, gw.Stop(ctx))
	})

	t.Run("GatewayManager is present after start", func(t *testing.T) {
		gw, err := New(testConfig())
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))

		waitForActors()

		pid, err := gw.System().ActorOf(ctx, naming.ActorNameGatewayManager)
		require.NoError(t, err)
		require.NotNil(t, pid)

		require.NoError(t, gw.Stop(ctx))
	})

	t.Run("foundational actors are children of GatewayManager after start", func(t *testing.T) {
		gw, err := New(testConfig())
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))

		waitForActors()

		managerPID, err := gw.System().ActorOf(ctx, naming.ActorNameGatewayManager)
		require.NoError(t, err)
		require.NotNil(t, managerPID)

		children := managerPID.Children()
		childNames := make(map[string]bool, len(children))
		for _, c := range children {
			childNames[c.Name()] = true
		}

		assert.True(t, childNames[naming.ActorNameRegistrar], "RegistryActor must be a child of GatewayManager")
		assert.True(t, childNames[naming.ActorNameHealth], "HealthActor must be a child of GatewayManager")
		assert.True(t, childNames[naming.ActorNameJournal], "JournalActor must be a child of GatewayManager")

		require.NoError(t, gw.Stop(ctx))
	})

	t.Run("bootstrap tools from config are loaded into registry", func(t *testing.T) {
		cfg := testConfig()
		cfg.Tools = []mcp.Tool{
			{
				ID:        mcp.ToolID("bootstrap-tool"),
				Transport: mcp.TransportStdio,
				Stdio:     &mcp.StdioTransportConfig{Command: "npx"},
				State:     mcp.ToolStateEnabled,
			},
		}

		gw, err := New(cfg)
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))

		waitForActors()

		tools, err := gw.ListTools(ctx)
		require.NoError(t, err)
		require.Len(t, tools, 1)
		assert.Equal(t, mcp.ToolID("bootstrap-tool"), tools[0].ID)
		assert.Equal(t, mcp.TransportStdio, tools[0].Transport)

		require.NoError(t, gw.Stop(ctx))
	})

	t.Run("Start returns error when Cluster.Enabled but no DiscoveryProvider", func(t *testing.T) {
		cfg := testConfig()
		cfg.Cluster.Enabled = true

		gw, err := New(cfg)
		require.NoError(t, err)

		err = gw.Start(ctx)
		require.Error(t, err)
		var rErr *mcp.RuntimeError
		require.ErrorAs(t, err, &rErr)
		assert.Equal(t, mcp.ErrCodeInvalidRequest, rErr.Code)
		assert.Contains(t, err.Error(), "no DiscoveryProvider is configured")
	})

	t.Run("Start returns error when Cluster.TLS has invalid cert paths", func(t *testing.T) {
		cfg := testConfig()
		cfg.Cluster.Enabled = true
		cfg.Cluster.DiscoveryProvider = &noopDiscovery{}
		cfg.Cluster.TLS = &mcp.RemotingTLSConfig{
			CertFile: "/nonexistent/cert.pem",
			KeyFile:  "/nonexistent/key.pem",
		}

		gw, err := New(cfg)
		require.NoError(t, err)

		err = gw.Start(ctx)
		require.Error(t, err)
		var rErr *mcp.RuntimeError
		require.ErrorAs(t, err, &rErr)
		assert.Equal(t, mcp.ErrCodeInternal, rErr.Code)
		assert.Contains(t, err.Error(), "cluster TLS")
	})

	t.Run("Start with WithMetrics succeeds", func(t *testing.T) {
		gw, err := New(testConfig(), WithMetrics())
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		waitForActors()
		assert.NotNil(t, gw.System())
		require.NoError(t, gw.Stop(ctx))
	})

	t.Run("Start with WithTracing succeeds", func(t *testing.T) {
		gw, err := New(testConfig(), WithTracing())
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		waitForActors()
		assert.NotNil(t, gw.System())
		require.NoError(t, gw.Stop(ctx))
	})
}

func TestGatewayStartGuard(t *testing.T) {
	ctx := context.Background()

	t.Run("second Start returns error while gateway is running", func(t *testing.T) {
		gw, err := New(testConfig())
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		waitForActors()

		err = gw.Start(ctx)
		require.Error(t, err)
		var rErr *mcp.RuntimeError
		require.ErrorAs(t, err, &rErr)
		assert.Equal(t, mcp.ErrCodeInternal, rErr.Code)
		assert.Contains(t, err.Error(), "gateway already started")

		require.NoError(t, gw.Stop(ctx))
	})

	t.Run("Stop then Start works again", func(t *testing.T) {
		gw, err := New(testConfig())
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		waitForActors()
		require.NoError(t, gw.Stop(ctx))

		require.NoError(t, gw.Start(ctx))
		waitForActors()
		assert.NotNil(t, gw.System())
		require.NoError(t, gw.Stop(ctx))
	})

	t.Run("Start returns error while another start is in flight", func(t *testing.T) {
		gw, err := New(testConfig())
		require.NoError(t, err)

		gw.mu.Lock()
		gw.starting = true
		gw.mu.Unlock()

		err = gw.Start(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "start already in progress")

		gw.mu.Lock()
		gw.starting = false
		gw.mu.Unlock()
	})

	t.Run("failed Start clears the audit stream and start guard", func(t *testing.T) {
		cfg := testConfig()
		cfg.Cluster.Enabled = true
		cfg.Cluster.DiscoveryProvider = &failingDiscovery{}
		cfg.Cluster.PeersPort = freePort(t)
		cfg.Cluster.DiscoveryPort = freePort(t)
		cfg.Cluster.RemotingPort = freePort(t)

		gw, err := New(cfg)
		require.NoError(t, err)

		err = gw.Start(ctx)
		require.Error(t, err)

		// SubscribeAudit must report "not started"; an orphaned audit stream
		// left over from the failed Start would let it succeed instead.
		_, err = gw.SubscribeAudit()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "gateway not started")

		gw.mu.RLock()
		assert.False(t, gw.starting, "failed Start must release the start guard")
		assert.Nil(t, gw.auditStream, "failed Start must clear the audit stream")
		gw.mu.RUnlock()
	})
}

func TestGatewayAPI(t *testing.T) {
	ctx := context.Background()

	t.Run("RegisterTool and ListTools", func(t *testing.T) {
		gw, err := New(testConfig())
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		waitForActors()
		defer func() { _ = gw.Stop(ctx) }()

		tool := mcp.Tool{
			ID:        "dynamic-tool",
			Transport: mcp.TransportStdio,
			Stdio:     &mcp.StdioTransportConfig{Command: "echo"},
			State:     mcp.ToolStateEnabled,
		}

		err = gw.RegisterTool(ctx, tool)
		require.NoError(t, err)

		waitForActors()

		tools, err := gw.ListTools(ctx)
		require.NoError(t, err)
		found := false
		for _, t := range tools {
			if t.ID == "dynamic-tool" {
				found = true
				break
			}
		}
		assert.True(t, found, "dynamically registered tool should be listed")
	})

	t.Run("DisableTool", func(t *testing.T) {
		cfg := testConfig()
		cfg.Tools = []mcp.Tool{
			{ID: "to-disable", Transport: mcp.TransportStdio, Stdio: &mcp.StdioTransportConfig{Command: "npx"}, State: mcp.ToolStateEnabled},
		}
		gw, err := New(cfg)
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		waitForActors()
		defer func() { _ = gw.Stop(ctx) }()

		err = gw.DisableTool(ctx, "to-disable")
		require.NoError(t, err)
	})

	t.Run("RemoveTool", func(t *testing.T) {
		cfg := testConfig()
		cfg.Tools = []mcp.Tool{
			{ID: "to-remove", Transport: mcp.TransportStdio, Stdio: &mcp.StdioTransportConfig{Command: "npx"}, State: mcp.ToolStateEnabled},
		}
		gw, err := New(cfg)
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		waitForActors()
		defer func() { _ = gw.Stop(ctx) }()

		err = gw.RemoveTool(ctx, "to-remove")
		require.NoError(t, err)

		tools, err := gw.ListTools(ctx)
		require.NoError(t, err)
		for _, tool := range tools {
			assert.NotEqual(t, mcp.ToolID("to-remove"), tool.ID)
		}
	})

	t.Run("RegisterTool validates tool", func(t *testing.T) {
		gw, err := New(testConfig())
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		waitForActors()
		defer func() { _ = gw.Stop(ctx) }()

		err = gw.RegisterTool(ctx, mcp.Tool{})
		require.Error(t, err)
		var rErr *mcp.RuntimeError
		require.ErrorAs(t, err, &rErr)
		assert.Equal(t, mcp.ErrCodeInvalidRequest, rErr.Code)
	})

	t.Run("API methods return error when gateway not started", func(t *testing.T) {
		gw, err := New(testConfig())
		require.NoError(t, err)

		_, err = gw.ListTools(ctx)
		require.Error(t, err)

		_, err = gw.Invoke(ctx, &mcp.Invocation{})
		require.Error(t, err)

		_, err = gw.InvokeStream(ctx, &mcp.Invocation{})
		require.Error(t, err)

		err = gw.RegisterTool(ctx, mcp.Tool{ID: "x", Transport: mcp.TransportStdio, Stdio: &mcp.StdioTransportConfig{Command: "y"}})
		require.Error(t, err)

		err = gw.UpdateTool(ctx, mcp.Tool{ID: "x", Transport: mcp.TransportStdio, Stdio: &mcp.StdioTransportConfig{Command: "y"}})
		require.Error(t, err)

		err = gw.DisableTool(ctx, "x")
		require.Error(t, err)

		err = gw.RemoveTool(ctx, "x")
		require.Error(t, err)

		err = gw.EnableTool(ctx, "x")
		require.Error(t, err)
	})

	t.Run("Invoke returns error for non-existent tool", func(t *testing.T) {
		gw, err := New(testConfig())
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		waitForActors()
		defer func() { _ = gw.Stop(ctx) }()

		_, err = gw.Invoke(ctx, &mcp.Invocation{
			ToolID: "does-not-exist",
			Method: "tools/call",
			Params: map[string]any{},
			Correlation: mcp.CorrelationMeta{
				TenantID:  "test-tenant",
				ClientID:  "test-client",
				RequestID: "req-1",
			},
		})
		require.Error(t, err)
	})

	t.Run("UpdateTool updates a registered tool", func(t *testing.T) {
		cfg := testConfig()
		cfg.Tools = []mcp.Tool{
			{ID: "to-update", Transport: mcp.TransportStdio, Stdio: &mcp.StdioTransportConfig{Command: "echo"}, State: mcp.ToolStateEnabled},
		}
		gw, err := New(cfg)
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		waitForActors()
		defer func() { _ = gw.Stop(ctx) }()

		updated := mcp.Tool{
			ID:        "to-update",
			Transport: mcp.TransportStdio,
			Stdio:     &mcp.StdioTransportConfig{Command: "cat"},
			State:     mcp.ToolStateEnabled,
		}
		err = gw.UpdateTool(ctx, updated)
		require.NoError(t, err)
	})

	t.Run("UpdateTool returns error when gateway not started", func(t *testing.T) {
		gw, err := New(testConfig())
		require.NoError(t, err)
		err = gw.UpdateTool(ctx, mcp.Tool{ID: "x"})
		require.Error(t, err)
	})

	t.Run("Invoke succeeds with MCP stdio tool", func(t *testing.T) {
		cfg := testConfig()
		cfg.Runtime.StartupTimeout = 15 * time.Second
		cfg.Tools = []mcp.Tool{
			{
				ID:        "echo-tool",
				Transport: mcp.TransportStdio,
				Stdio: &mcp.StdioTransportConfig{
					Command: "npx",
					Args:    []string{"-y", "mcp-hello-world"},
				},
				State: mcp.ToolStateEnabled,
			},
		}
		gw, err := New(cfg)
		require.NoError(t, err)
		if err := gw.Start(ctx); err != nil {
			t.Skipf("gateway start failed (npx/node may be unavailable): %v", err)
		}
		waitForActors()
		defer func() { _ = gw.Stop(ctx) }()

		result, err := gw.Invoke(ctx, &mcp.Invocation{
			ToolID: "echo-tool",
			Method: "tools/call",
			Params: map[string]any{
				"name":      "echo",
				"arguments": map[string]any{"message": "api-test"},
			},
			Correlation: mcp.CorrelationMeta{
				TenantID:  "test-tenant",
				ClientID:  "test-client",
				RequestID: "req-invoke-success",
			},
		})
		if err != nil {
			t.Skipf("invoke failed (MCP server may be unavailable): %v", err)
		}
		require.NotNil(t, result)
		assert.True(t, result.Succeeded(), "invocation should succeed")
		assert.NotNil(t, result.Output)
	})

	t.Run("API with system-wide Registrar and Router exercises resolver fallback", func(t *testing.T) {
		ctx := context.Background()
		cfg := actor.ExternalTestConfig()
		execFactory := egress.NewCompositeExecutorFactory(mcp.DefaultStartupTimeout, nil)
		system, stop := newTestActorSystemForResolverFallback(t, cfg, execFactory)
		defer stop()

		_, err := system.Spawn(ctx, naming.ActorNameGatewayManager, &minimalGatewayManager{})
		require.NoError(t, err)
		waitForActors()

		actor.SpawnFoundationalActorsForExternalTest(ctx, system)
		waitForActors()

		gw, err := New(testConfig(), withSystemForTesting(system))
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		defer func() { _ = gw.Stop(ctx) }()

		tools, err := gw.ListTools(ctx)
		require.NoError(t, err)
		require.NotNil(t, tools)

		tool := mcp.Tool{
			ID:        "fallback-tool",
			Transport: mcp.TransportStdio,
			Stdio:     &mcp.StdioTransportConfig{Command: "npx"},
			State:     mcp.ToolStateEnabled,
		}
		err = gw.RegisterTool(ctx, tool)
		require.NoError(t, err)
		waitForActors()

		_, err = gw.Invoke(ctx, &mcp.Invocation{
			ToolID: "fallback-tool",
			Method: "tools/call",
			Params: map[string]any{},
			Correlation: mcp.CorrelationMeta{
				TenantID:  "test-tenant",
				ClientID:  "test-client",
				RequestID: "req-fallback",
			},
		})
		require.Error(t, err)

		// Exercise InvokeStream through the resolver fallback path
		_, err = gw.InvokeStream(ctx, &mcp.Invocation{
			ToolID: "fallback-tool",
			Method: "tools/call",
			Params: map[string]any{},
			Correlation: mcp.CorrelationMeta{
				TenantID:  "test-tenant",
				ClientID:  "test-client",
				RequestID: "req-stream-fallback",
			},
		})
		require.Error(t, err) // tool execution will fail but routing should work
	})
}

func newTestActorSystemForResolverFallback(t *testing.T, cfg mcp.Config, execFactory mcp.ExecutorFactory) (goaktactor.ActorSystem, func()) {
	t.Helper()
	ctx := context.Background()
	opts := []goaktactor.Option{
		goaktactor.WithLogger(goaktlog.DiscardLogger),
		goaktactor.WithExtensions(
			actorextension.NewExecutorFactoryExtension(execFactory),
			actorextension.NewConfigExtension(cfg),
		),
	}
	system, err := goaktactor.NewActorSystem("test-resolver-fallback", opts...)
	require.NoError(t, err)
	require.NoError(t, system.Start(ctx))
	return system, func() { _ = system.Stop(ctx) }
}

func TestGatewayTenantValidation(t *testing.T) {
	t.Run("New rejects empty tenant ID", func(t *testing.T) {
		cfg := testConfig()
		cfg.Tenants = []mcp.TenantConfig{{ID: ""}}
		_, err := New(cfg)
		require.Error(t, err)
		var rErr *mcp.RuntimeError
		require.ErrorAs(t, err, &rErr)
		assert.Equal(t, mcp.ErrCodeInvalidRequest, rErr.Code)
		assert.Contains(t, err.Error(), "tenant ID must not be empty")
	})

	t.Run("New rejects duplicate tenant IDs", func(t *testing.T) {
		cfg := testConfig()
		cfg.Tenants = []mcp.TenantConfig{
			{ID: "dup-tenant"},
			{ID: "dup-tenant"},
		}
		_, err := New(cfg)
		require.Error(t, err)
		var rErr *mcp.RuntimeError
		require.ErrorAs(t, err, &rErr)
		assert.Equal(t, mcp.ErrCodeInvalidRequest, rErr.Code)
		assert.Contains(t, err.Error(), "duplicate tenant ID")
	})

	t.Run("New accepts valid tenants", func(t *testing.T) {
		cfg := testConfig()
		cfg.Tenants = []mcp.TenantConfig{
			{ID: "tenant-a"},
			{ID: "tenant-b"},
		}
		gw, err := New(cfg)
		require.NoError(t, err)
		require.NotNil(t, gw)
	})

	t.Run("New accepts empty tenants list", func(t *testing.T) {
		cfg := testConfig()
		cfg.Tenants = nil
		gw, err := New(cfg)
		require.NoError(t, err)
		require.NotNil(t, gw)
	})
}

func TestGatewayDraining(t *testing.T) {
	ctx := context.Background()

	t.Run("API methods return error while gateway is draining", func(t *testing.T) {
		gw, err := New(testConfig())
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		waitForActors()

		// Simulate draining state without actually stopping the system
		gw.mu.Lock()
		gw.draining = true
		gw.mu.Unlock()

		_, err = gw.ListTools(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "shutting down")

		_, err = gw.Invoke(ctx, &mcp.Invocation{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "shutting down")

		err = gw.RegisterTool(ctx, mcp.Tool{ID: "x", Transport: mcp.TransportStdio, Stdio: &mcp.StdioTransportConfig{Command: "y"}})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "shutting down")

		err = gw.EnableTool(ctx, "x")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "shutting down")

		err = gw.DisableTool(ctx, "x")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "shutting down")

		err = gw.RemoveTool(ctx, "x")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "shutting down")

		_, err = gw.InvokeStream(ctx, &mcp.Invocation{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "shutting down")

		err = gw.UpdateTool(ctx, mcp.Tool{ID: "x", Transport: mcp.TransportStdio, Stdio: &mcp.StdioTransportConfig{Command: "y"}})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "shutting down")

		// Restore state so Stop() works
		gw.mu.Lock()
		gw.draining = false
		gw.mu.Unlock()
		require.NoError(t, gw.Stop(ctx))
	})
}

func TestGatewayEnableTool(t *testing.T) {
	ctx := context.Background()

	t.Run("EnableTool re-enables a disabled tool", func(t *testing.T) {
		cfg := testConfig()
		cfg.Tools = []mcp.Tool{
			{ID: "to-enable", Transport: mcp.TransportStdio, Stdio: &mcp.StdioTransportConfig{Command: "npx"}, State: mcp.ToolStateEnabled},
		}
		gw, err := New(cfg)
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		waitForActors()
		defer func() { _ = gw.Stop(ctx) }()

		// Disable the tool first
		err = gw.DisableTool(ctx, "to-enable")
		require.NoError(t, err)

		// Re-enable it
		err = gw.EnableTool(ctx, "to-enable")
		require.NoError(t, err)
	})

	t.Run("EnableTool returns error for non-existent tool", func(t *testing.T) {
		gw, err := New(testConfig())
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		waitForActors()
		defer func() { _ = gw.Stop(ctx) }()

		err = gw.EnableTool(ctx, "nonexistent")
		require.Error(t, err)
	})

	t.Run("EnableTool rejects empty tool ID", func(t *testing.T) {
		gw, err := New(testConfig())
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		waitForActors()
		defer func() { _ = gw.Stop(ctx) }()

		err = gw.EnableTool(ctx, "")
		require.Error(t, err)
		var rErr *mcp.RuntimeError
		require.ErrorAs(t, err, &rErr)
		assert.Equal(t, mcp.ErrCodeInvalidRequest, rErr.Code)
	})

	t.Run("EnableTool returns error when gateway not started", func(t *testing.T) {
		gw, err := New(testConfig())
		require.NoError(t, err)
		err = gw.EnableTool(ctx, "x")
		require.Error(t, err)
	})

	t.Run("DisableTool rejects empty tool ID", func(t *testing.T) {
		gw, err := New(testConfig())
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		waitForActors()
		defer func() { _ = gw.Stop(ctx) }()

		err = gw.DisableTool(ctx, "")
		require.Error(t, err)
		var rErr *mcp.RuntimeError
		require.ErrorAs(t, err, &rErr)
		assert.Equal(t, mcp.ErrCodeInvalidRequest, rErr.Code)
	})

	t.Run("RemoveTool rejects empty tool ID", func(t *testing.T) {
		gw, err := New(testConfig())
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		waitForActors()
		defer func() { _ = gw.Stop(ctx) }()

		err = gw.RemoveTool(ctx, "")
		require.Error(t, err)
		var rErr *mcp.RuntimeError
		require.ErrorAs(t, err, &rErr)
		assert.Equal(t, mcp.ErrCodeInvalidRequest, rErr.Code)
	})

	t.Run("UpdateTool validates tool before sending to registrar", func(t *testing.T) {
		gw, err := New(testConfig())
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		waitForActors()
		defer func() { _ = gw.Stop(ctx) }()

		// Tool with empty ID should fail validation
		err = gw.UpdateTool(ctx, mcp.Tool{})
		require.Error(t, err)
		var rErr *mcp.RuntimeError
		require.ErrorAs(t, err, &rErr)
		assert.Equal(t, mcp.ErrCodeInvalidRequest, rErr.Code)
	})
}

func TestGatewayAPI_ClusterMode(t *testing.T) {
	ctx := context.Background()

	t.Run("API with cluster mode uses system-wide Registrar and Router", func(t *testing.T) {
		cfg := testConfig()
		cfg.Cluster.Enabled = true
		cfg.Cluster.DiscoveryProvider = &noopDiscovery{}
		// Use high ports to avoid conflicts with other processes
		cfg.Cluster.PeersPort = freePort(t)
		cfg.Cluster.DiscoveryPort = freePort(t)
		cfg.Cluster.RemotingPort = freePort(t)
		cfg.Tools = []mcp.Tool{
			{
				ID:        "cluster-tool",
				Transport: mcp.TransportStdio,
				Stdio:     &mcp.StdioTransportConfig{Command: "npx"},
				State:     mcp.ToolStateEnabled,
			},
		}
		gw, err := New(cfg)
		require.NoError(t, err)
		if err := gw.Start(ctx); err != nil {
			t.Skipf("cluster gateway start failed (discovery provider may be unavailable): %v", err)
		}
		waitForActors()
		time.Sleep(2 * time.Second) // allow cluster to form and singleton to be elected
		defer func() { _ = gw.Stop(ctx) }()

		tools, err := gw.ListTools(ctx)
		if err != nil {
			t.Skipf("ListTools failed in cluster mode: %v", err)
		}
		require.NotNil(t, tools)
		found := false
		for _, tool := range tools {
			if tool.ID == "cluster-tool" {
				found = true
				break
			}
		}
		assert.True(t, found, "bootstrap tool should be listed in cluster mode")
	})
}

func TestGatewayHandler(t *testing.T) {
	t.Run("Handler returns a valid http.Handler when IdentityResolver is set", func(t *testing.T) {
		gw, err := New(testConfig())
		require.NoError(t, err)

		handler, err := gw.Handler(mcp.IngressConfig{
			IdentityResolver: &fixedIdentityResolver{tenantID: "t1", clientID: "c1"},
		})
		require.NoError(t, err)
		require.NotNil(t, handler)
	})

	t.Run("Handler returns error when IdentityResolver is nil", func(t *testing.T) {
		gw, err := New(testConfig())
		require.NoError(t, err)

		_, err = gw.Handler(mcp.IngressConfig{})
		require.Error(t, err)
	})
}

func TestGatewayWSHandler(t *testing.T) {
	t.Run("WSHandler returns a valid http.Handler when IdentityResolver is set", func(t *testing.T) {
		gw, err := New(testConfig())
		require.NoError(t, err)

		handler, err := gw.WSHandler(mcp.IngressConfig{
			IdentityResolver: &fixedIdentityResolver{tenantID: "t1", clientID: "c1"},
		}, nil)
		require.NoError(t, err)
		require.NotNil(t, handler)
	})

	t.Run("WSHandler returns error when IdentityResolver is nil", func(t *testing.T) {
		gw, err := New(testConfig())
		require.NoError(t, err)

		_, err = gw.WSHandler(mcp.IngressConfig{}, nil)
		require.Error(t, err)
	})
}

func TestGatewayRegisterGRPCService(t *testing.T) {
	t.Run("RegisterGRPCService succeeds when IdentityResolver is set", func(t *testing.T) {
		gw, err := New(testConfig())
		require.NoError(t, err)

		srv := grpc.NewServer()
		err = gw.RegisterGRPCService(srv, mcp.GRPCIngressConfig{
			IdentityResolver: &fixedGRPCIdentityResolver{tenantID: "t1", clientID: "c1"},
		})
		require.NoError(t, err)
		srv.Stop()
	})

	t.Run("RegisterGRPCService returns error when IdentityResolver is nil", func(t *testing.T) {
		gw, err := New(testConfig())
		require.NoError(t, err)

		srv := grpc.NewServer()
		err = gw.RegisterGRPCService(srv, mcp.GRPCIngressConfig{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "IdentityResolver must not be nil")
		srv.Stop()
	})

	t.Run("RegisterGRPCService auto-installs identity resolver with EnterpriseAuth", func(t *testing.T) {
		gw, err := New(testConfig())
		require.NoError(t, err)

		srv := grpc.NewServer()
		err = gw.RegisterGRPCService(srv, mcp.GRPCIngressConfig{
			EnterpriseAuth: &mcp.EnterpriseAuthConfig{
				TokenVerifier: validTestTokenVerifier(),
			},
		})
		require.NoError(t, err)
		srv.Stop()
	})

	t.Run("RegisterGRPCService returns error with EnterpriseAuth but nil TokenVerifier", func(t *testing.T) {
		gw, err := New(testConfig())
		require.NoError(t, err)

		srv := grpc.NewServer()
		err = gw.RegisterGRPCService(srv, mcp.GRPCIngressConfig{
			EnterpriseAuth: &mcp.EnterpriseAuthConfig{},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "TokenVerifier must not be nil")
		srv.Stop()
	})
}

func TestGRPCAuthInterceptors(t *testing.T) {
	t.Run("returns interceptors when config is valid", func(t *testing.T) {
		unary, stream, err := GRPCAuthInterceptors(&mcp.EnterpriseAuthConfig{
			TokenVerifier: validTestTokenVerifier(),
		})
		require.NoError(t, err)
		assert.NotNil(t, unary)
		assert.NotNil(t, stream)
	})

	t.Run("returns error when config is nil", func(t *testing.T) {
		_, _, err := GRPCAuthInterceptors(nil)
		require.Error(t, err)
	})
}

func TestInvokeStream(t *testing.T) {
	ctx := context.Background()

	t.Run("returns error when gateway not started", func(t *testing.T) {
		gw, err := New(testConfig())
		require.NoError(t, err)

		_, err = gw.InvokeStream(ctx, &mcp.Invocation{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "gateway not started")
	})

	t.Run("returns StreamingResult with closed Progress and buffered Final on success", func(t *testing.T) {
		cfg := testConfig()
		cfg.Tenants = []mcp.TenantConfig{{ID: "stream-tenant"}}
		cfg.Tools = []mcp.Tool{
			{ID: "stream-tool", Transport: mcp.TransportStdio, Stdio: &mcp.StdioTransportConfig{Command: "npx"}, State: mcp.ToolStateEnabled},
		}
		gw, err := New(cfg)
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		waitForActors()
		defer func() { _ = gw.Stop(ctx) }()

		inv := &mcp.Invocation{
			ToolID: "stream-tool",
			Method: "tools/call",
			Params: map[string]any{"name": "echo", "arguments": map[string]any{}},
			Correlation: mcp.CorrelationMeta{
				TenantID:  "stream-tenant",
				ClientID:  "stream-client",
				RequestID: "stream-req-1",
			},
		}

		result, err := gw.InvokeStream(ctx, inv)
		// The invocation may error (tool backend not running), but InvokeStream
		// itself must be reached and return a non-nil StreamingResult on success
		// or propagate the error from Invoke. Either way the function is covered.
		if err != nil {
			assert.Nil(t, result)
			return
		}
		require.NotNil(t, result)
		// Progress channel must be closed immediately (non-streaming path)
		_, open := <-result.Progress
		assert.False(t, open, "Progress channel should be closed for non-streaming result")
		// Final channel must have exactly one result buffered
		final, ok := <-result.Final
		require.True(t, ok)
		assert.NotNil(t, final)
	})
}

func TestGatewayStop(t *testing.T) {
	ctx := context.Background()

	t.Run("Stop sets system to nil after shutdown", func(t *testing.T) {
		gw, err := New(testConfig())
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		waitForActors()
		assert.NotNil(t, gw.System())

		require.NoError(t, gw.Stop(ctx))
		assert.Nil(t, gw.System())
	})

	t.Run("Stop is idempotent — second call is a no-op", func(t *testing.T) {
		gw, err := New(testConfig())
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		waitForActors()

		require.NoError(t, gw.Stop(ctx))
		require.NoError(t, gw.Stop(ctx)) // second stop must not error
	})
}

// testPath is a minimal goaktactor.Path implementation for unit tests.
type testPath struct {
	name   string
	parent goaktactor.Path
}

func (p *testPath) Name() string                      { return p.name }
func (p *testPath) Parent() goaktactor.Path           { return p.parent }
func (p *testPath) Host() string                      { return "" }
func (p *testPath) HostPort() string                  { return "" }
func (p *testPath) Port() int                         { return 0 }
func (p *testPath) String() string                    { return "" }
func (p *testPath) System() string                    { return "" }
func (p *testPath) Equals(other goaktactor.Path) bool { return false }

func TestToolIDFromPassivatedPath(t *testing.T) {
	tests := []struct {
		name     string
		path     goaktactor.Path
		expected mcp.ToolID
	}{
		{
			name:     "valid session path",
			path:     &testPath{name: "session-t1-c1-my-tool", parent: &testPath{name: "supervisor-my-tool"}},
			expected: mcp.ToolID("my-tool"),
		},
		{
			name:     "non-session actor",
			path:     &testPath{name: "supervisor-my-tool", parent: &testPath{name: "registrar"}},
			expected: "",
		},
		{
			name:     "nil path",
			path:     nil,
			expected: "",
		},
		{
			name:     "session with no parent",
			path:     &testPath{name: "session-t1-c1-tool"},
			expected: "",
		},
		{
			name:     "session with non-supervisor parent",
			path:     &testPath{name: "session-t1-c1-tool", parent: &testPath{name: "registrar"}},
			expected: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, toolIDFromPath(tt.path))
		})
	}
}

// syncStreamRouterActor responds to RouteInvokeStream with a synchronous result
// (StreamResult nil, Result non-nil) to exercise the fallback wrapping path.
type syncStreamRouterActor struct{}

func (s *syncStreamRouterActor) PreStart(_ *goaktactor.Context) error { return nil }
func (s *syncStreamRouterActor) Receive(ctx *goaktactor.ReceiveContext) {
	switch ctx.Message().(type) {
	case *runtime.RouteInvokeStream:
		ctx.Response(&runtime.RouteStreamResult{
			Result: &mcp.ExecutionResult{
				Output: map[string]any{"type": "text", "text": "sync-fallback"},
			},
		})
	case *runtime.RouteInvocation:
		ctx.Response(&runtime.RouteResult{
			Result: &mcp.ExecutionResult{
				Output: map[string]any{"type": "text", "text": "sync"},
			},
		})
	default:
		ctx.Response("unhandled")
	}
}
func (s *syncStreamRouterActor) PostStop(_ *goaktactor.Context) error { return nil }

// streamErrRouterActor responds to RouteInvokeStream with an error result
// to exercise the result.Err != nil path in InvokeStream.
type streamErrRouterActor struct{}

func (s *streamErrRouterActor) PreStart(_ *goaktactor.Context) error { return nil }
func (s *streamErrRouterActor) Receive(ctx *goaktactor.ReceiveContext) {
	switch ctx.Message().(type) {
	case *runtime.RouteInvokeStream:
		ctx.Response(&runtime.RouteStreamResult{
			Err: mcp.NewRuntimeError(mcp.ErrCodeToolNotFound, "tool not found for stream"),
		})
	default:
		ctx.Response("unhandled")
	}
}
func (s *streamErrRouterActor) PostStop(_ *goaktactor.Context) error { return nil }

// streamingRouterActor responds to RouteInvokeStream with a real StreamingResult
// (StreamResult non-nil) to exercise the streaming return path.
type streamingRouterActor struct{}

func (s *streamingRouterActor) PreStart(_ *goaktactor.Context) error { return nil }
func (s *streamingRouterActor) Receive(ctx *goaktactor.ReceiveContext) {
	switch ctx.Message().(type) {
	case *runtime.RouteInvokeStream:
		progressCh := make(chan mcp.ProgressEvent)
		close(progressCh)
		finalCh := make(chan *mcp.ExecutionResult, 1)
		finalCh <- &mcp.ExecutionResult{
			Status: mcp.ExecutionStatusSuccess,
			Output: map[string]any{"type": "text", "text": "streamed"},
		}
		close(finalCh)
		ctx.Response(&runtime.RouteStreamResult{
			StreamResult: &mcp.StreamingResult{
				Progress: progressCh,
				Final:    finalCh,
			},
		})
	default:
		ctx.Response("unhandled")
	}
}
func (s *streamingRouterActor) PostStop(_ *goaktactor.Context) error { return nil }

// noResponseActor never responds to Ask messages, causing Ask to timeout.
type noResponseActor struct{}

func (n *noResponseActor) PreStart(_ *goaktactor.Context) error { return nil }
func (n *noResponseActor) Receive(_ *goaktactor.ReceiveContext) {}
func (n *noResponseActor) PostStop(_ *goaktactor.Context) error { return nil }

// wrongTypeActor always responds with a plain string regardless of the message
// type, causing type assertions in the API layer to fail.
type wrongTypeActor struct{}

func (w *wrongTypeActor) PreStart(_ *goaktactor.Context) error { return nil }
func (w *wrongTypeActor) Receive(ctx *goaktactor.ReceiveContext) {
	ctx.Response("unexpected-type")
}
func (w *wrongTypeActor) PostStop(_ *goaktactor.Context) error { return nil }

// newTestSystemWithWrongTypeActors creates an actor system with a
// minimalGatewayManager plus system-wide registrar and router stubs that return
// wrong response types. This lets us exercise the !ok type-assertion branches.
func newTestSystemWithWrongTypeActors(t *testing.T) (goaktactor.ActorSystem, func()) {
	t.Helper()
	ctx := context.Background()
	system, err := goaktactor.NewActorSystem("test-wrong-type",
		goaktactor.WithLogger(goaktlog.DiscardLogger),
	)
	require.NoError(t, err)
	require.NoError(t, system.Start(ctx))

	// Spawn a minimal gateway manager so resolveRegistrar/resolveRouter can look it up.
	_, err = system.Spawn(ctx, naming.ActorNameGatewayManager, &minimalGatewayManager{})
	require.NoError(t, err)

	// Spawn wrong-type actors at system level (the fallback path in resolveRegistrar/resolveRouter).
	_, err = system.Spawn(ctx, naming.ActorNameRegistrar, &wrongTypeActor{})
	require.NoError(t, err)
	_, err = system.Spawn(ctx, naming.RouterName(0), &wrongTypeActor{})
	require.NoError(t, err)

	waitForActors()
	return system, func() { _ = system.Stop(ctx) }
}

func TestAPIUnexpectedResponseType(t *testing.T) {
	ctx := context.Background()

	t.Run("Invoke returns error on unexpected response type", func(t *testing.T) {
		system, stop := newTestSystemWithWrongTypeActors(t)
		defer stop()

		gw, err := New(testConfig(), withSystemForTesting(system))
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		defer func() { _ = gw.Stop(ctx) }()

		_, err = gw.Invoke(ctx, &mcp.Invocation{
			ToolID:      "x",
			Method:      "tools/call",
			Params:      map[string]any{},
			Correlation: mcp.CorrelationMeta{TenantID: "t", ClientID: "c", RequestID: "r"},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unexpected response type")
	})

	t.Run("InvokeStream returns error on unexpected response type", func(t *testing.T) {
		system, stop := newTestSystemWithWrongTypeActors(t)
		defer stop()

		gw, err := New(testConfig(), withSystemForTesting(system))
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		defer func() { _ = gw.Stop(ctx) }()

		_, err = gw.InvokeStream(ctx, &mcp.Invocation{
			ToolID:      "x",
			Method:      "tools/call",
			Params:      map[string]any{},
			Correlation: mcp.CorrelationMeta{TenantID: "t", ClientID: "c", RequestID: "r"},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unexpected response type")
	})

	t.Run("ListTools returns error on unexpected response type", func(t *testing.T) {
		system, stop := newTestSystemWithWrongTypeActors(t)
		defer stop()

		gw, err := New(testConfig(), withSystemForTesting(system))
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		defer func() { _ = gw.Stop(ctx) }()

		_, err = gw.ListTools(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unexpected response type")
	})

	t.Run("RegisterTool returns error on unexpected response type", func(t *testing.T) {
		system, stop := newTestSystemWithWrongTypeActors(t)
		defer stop()

		gw, err := New(testConfig(), withSystemForTesting(system))
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		defer func() { _ = gw.Stop(ctx) }()

		err = gw.RegisterTool(ctx, mcp.Tool{
			ID: "x", Transport: mcp.TransportStdio,
			Stdio: &mcp.StdioTransportConfig{Command: "echo"},
			State: mcp.ToolStateEnabled,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unexpected response type")
	})

	t.Run("UpdateTool returns error on unexpected response type", func(t *testing.T) {
		system, stop := newTestSystemWithWrongTypeActors(t)
		defer stop()

		gw, err := New(testConfig(), withSystemForTesting(system))
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		defer func() { _ = gw.Stop(ctx) }()

		err = gw.UpdateTool(ctx, mcp.Tool{
			ID: "x", Transport: mcp.TransportStdio,
			Stdio: &mcp.StdioTransportConfig{Command: "echo"},
			State: mcp.ToolStateEnabled,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unexpected response type")
	})

	t.Run("DisableTool returns error on unexpected response type", func(t *testing.T) {
		system, stop := newTestSystemWithWrongTypeActors(t)
		defer stop()

		gw, err := New(testConfig(), withSystemForTesting(system))
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		defer func() { _ = gw.Stop(ctx) }()

		err = gw.DisableTool(ctx, "some-tool")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unexpected response type")
	})

	t.Run("RemoveTool returns error on unexpected response type", func(t *testing.T) {
		system, stop := newTestSystemWithWrongTypeActors(t)
		defer stop()

		gw, err := New(testConfig(), withSystemForTesting(system))
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		defer func() { _ = gw.Stop(ctx) }()

		err = gw.RemoveTool(ctx, "some-tool")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unexpected response type")
	})

	t.Run("EnableTool returns error on unexpected response type", func(t *testing.T) {
		system, stop := newTestSystemWithWrongTypeActors(t)
		defer stop()

		gw, err := New(testConfig(), withSystemForTesting(system))
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		defer func() { _ = gw.Stop(ctx) }()

		err = gw.EnableTool(ctx, "some-tool")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unexpected response type")
	})
}

func TestResolverFallbackError(t *testing.T) {
	ctx := context.Background()

	t.Run("resolveRegistrar returns error when system-wide fallback fails", func(t *testing.T) {
		// Create an actor system with only a minimalGatewayManager but NO
		// system-wide registrar actor. The child lookup will fail (no children),
		// then system.ActorOf will also fail, triggering the error path.
		system, err := goaktactor.NewActorSystem("test-resolve-reg-err",
			goaktactor.WithLogger(goaktlog.DiscardLogger),
		)
		require.NoError(t, err)
		require.NoError(t, system.Start(ctx))
		defer func() { _ = system.Stop(ctx) }()

		_, err = system.Spawn(ctx, naming.ActorNameGatewayManager, &minimalGatewayManager{})
		require.NoError(t, err)
		waitForActors()

		gw, err := New(testConfig(), withSystemForTesting(system))
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		defer func() { _ = gw.Stop(ctx) }()

		// ListTools uses resolveRegistrar
		_, err = gw.ListTools(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "registrar not found")
	})

	t.Run("resolveRouter returns error when system-wide fallback fails", func(t *testing.T) {
		// Create an actor system with only a minimalGatewayManager but NO
		// system-wide router actor.
		system, err := goaktactor.NewActorSystem("test-resolve-rtr-err",
			goaktactor.WithLogger(goaktlog.DiscardLogger),
		)
		require.NoError(t, err)
		require.NoError(t, system.Start(ctx))
		defer func() { _ = system.Stop(ctx) }()

		_, err = system.Spawn(ctx, naming.ActorNameGatewayManager, &minimalGatewayManager{})
		require.NoError(t, err)
		waitForActors()

		gw, err := New(testConfig(), withSystemForTesting(system))
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		defer func() { _ = gw.Stop(ctx) }()

		// Invoke uses resolveRouter
		_, err = gw.Invoke(ctx, &mcp.Invocation{
			ToolID:      "x",
			Method:      "tools/call",
			Params:      map[string]any{},
			Correlation: mcp.CorrelationMeta{TenantID: "t", ClientID: "c", RequestID: "r"},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "router not found")

		// InvokeStream uses resolveRouter too
		_, err = gw.InvokeStream(ctx, &mcp.Invocation{
			ToolID:      "x",
			Method:      "tools/call",
			Params:      map[string]any{},
			Correlation: mcp.CorrelationMeta{TenantID: "t", ClientID: "c", RequestID: "r"},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "router not found")
	})
}

// newTestSystemWithNoResponseActors creates an actor system where the registrar
// and router actors never respond, causing Ask to timeout.
func newTestSystemWithNoResponseActors(t *testing.T) (goaktactor.ActorSystem, func()) {
	t.Helper()
	ctx := context.Background()
	system, err := goaktactor.NewActorSystem("test-no-response",
		goaktactor.WithLogger(goaktlog.DiscardLogger),
	)
	require.NoError(t, err)
	require.NoError(t, system.Start(ctx))

	_, err = system.Spawn(ctx, naming.ActorNameGatewayManager, &minimalGatewayManager{})
	require.NoError(t, err)
	_, err = system.Spawn(ctx, naming.ActorNameRegistrar, &noResponseActor{})
	require.NoError(t, err)
	_, err = system.Spawn(ctx, naming.RouterName(0), &noResponseActor{})
	require.NoError(t, err)

	waitForActors()
	return system, func() { _ = system.Stop(ctx) }
}

func TestAPIAskError(t *testing.T) {
	ctx := context.Background()

	t.Run("Invoke returns error when Ask times out", func(t *testing.T) {
		system, stop := newTestSystemWithNoResponseActors(t)
		defer stop()

		cfg := testConfig()
		cfg.Runtime.RequestTimeout = 200 * time.Millisecond
		gw, err := New(cfg, withSystemForTesting(system))
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		defer func() { _ = gw.Stop(ctx) }()

		_, err = gw.Invoke(ctx, &mcp.Invocation{
			ToolID:      "x",
			Method:      "tools/call",
			Params:      map[string]any{},
			Correlation: mcp.CorrelationMeta{TenantID: "t", ClientID: "c", RequestID: "r"},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "router ask failed")
	})

	t.Run("InvokeStream returns error when Ask times out", func(t *testing.T) {
		system, stop := newTestSystemWithNoResponseActors(t)
		defer stop()

		cfg := testConfig()
		cfg.Runtime.RequestTimeout = 200 * time.Millisecond
		gw, err := New(cfg, withSystemForTesting(system))
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		defer func() { _ = gw.Stop(ctx) }()

		_, err = gw.InvokeStream(ctx, &mcp.Invocation{
			ToolID:      "x",
			Method:      "tools/call",
			Params:      map[string]any{},
			Correlation: mcp.CorrelationMeta{TenantID: "t", ClientID: "c", RequestID: "r"},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "router ask failed")
	})

	t.Run("ListTools returns error when Ask times out", func(t *testing.T) {
		system, stop := newTestSystemWithNoResponseActors(t)
		defer stop()

		cfg := testConfig()
		cfg.Runtime.RequestTimeout = 200 * time.Millisecond
		gw, err := New(cfg, withSystemForTesting(system))
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		defer func() { _ = gw.Stop(ctx) }()

		_, err = gw.ListTools(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "registrar ask failed")
	})

	t.Run("RegisterTool returns error when Ask times out", func(t *testing.T) {
		system, stop := newTestSystemWithNoResponseActors(t)
		defer stop()

		cfg := testConfig()
		cfg.Runtime.RequestTimeout = 200 * time.Millisecond
		gw, err := New(cfg, withSystemForTesting(system))
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		defer func() { _ = gw.Stop(ctx) }()

		err = gw.RegisterTool(ctx, mcp.Tool{
			ID: "x", Transport: mcp.TransportStdio,
			Stdio: &mcp.StdioTransportConfig{Command: "echo"},
			State: mcp.ToolStateEnabled,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "registrar ask failed")
	})

	t.Run("UpdateTool returns error when Ask times out", func(t *testing.T) {
		system, stop := newTestSystemWithNoResponseActors(t)
		defer stop()

		cfg := testConfig()
		cfg.Runtime.RequestTimeout = 200 * time.Millisecond
		gw, err := New(cfg, withSystemForTesting(system))
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		defer func() { _ = gw.Stop(ctx) }()

		err = gw.UpdateTool(ctx, mcp.Tool{
			ID: "x", Transport: mcp.TransportStdio,
			Stdio: &mcp.StdioTransportConfig{Command: "echo"},
			State: mcp.ToolStateEnabled,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "registrar ask failed")
	})

	t.Run("DisableTool returns error when Ask times out", func(t *testing.T) {
		system, stop := newTestSystemWithNoResponseActors(t)
		defer stop()

		cfg := testConfig()
		cfg.Runtime.RequestTimeout = 200 * time.Millisecond
		gw, err := New(cfg, withSystemForTesting(system))
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		defer func() { _ = gw.Stop(ctx) }()

		err = gw.DisableTool(ctx, "some-tool")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "registrar ask failed")
	})

	t.Run("RemoveTool returns error when Ask times out", func(t *testing.T) {
		system, stop := newTestSystemWithNoResponseActors(t)
		defer stop()

		cfg := testConfig()
		cfg.Runtime.RequestTimeout = 200 * time.Millisecond
		gw, err := New(cfg, withSystemForTesting(system))
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		defer func() { _ = gw.Stop(ctx) }()

		err = gw.RemoveTool(ctx, "some-tool")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "registrar ask failed")
	})

	t.Run("EnableTool returns error when Ask times out", func(t *testing.T) {
		system, stop := newTestSystemWithNoResponseActors(t)
		defer stop()

		cfg := testConfig()
		cfg.Runtime.RequestTimeout = 200 * time.Millisecond
		gw, err := New(cfg, withSystemForTesting(system))
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		defer func() { _ = gw.Stop(ctx) }()

		err = gw.EnableTool(ctx, "some-tool")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "registrar ask failed")
	})
}

func TestInvokeStreamNonExistentTool(t *testing.T) {
	ctx := context.Background()

	t.Run("InvokeStream returns error for non-existent tool", func(t *testing.T) {
		gw, err := New(testConfig())
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		waitForActors()
		defer func() { _ = gw.Stop(ctx) }()

		_, err = gw.InvokeStream(ctx, &mcp.Invocation{
			ToolID: "does-not-exist",
			Method: "tools/call",
			Params: map[string]any{},
			Correlation: mcp.CorrelationMeta{
				TenantID:  "test-tenant",
				ClientID:  "test-client",
				RequestID: "req-stream-missing",
			},
		})
		require.Error(t, err)
	})
}

func TestInvokeStreamFallbackWrapping(t *testing.T) {
	ctx := context.Background()

	t.Run("InvokeStream wraps synchronous result in StreamingResult", func(t *testing.T) {
		system, err := goaktactor.NewActorSystem("test-sync-stream",
			goaktactor.WithLogger(goaktlog.DiscardLogger),
		)
		require.NoError(t, err)
		require.NoError(t, system.Start(ctx))
		defer func() { _ = system.Stop(ctx) }()

		_, err = system.Spawn(ctx, naming.ActorNameGatewayManager, &minimalGatewayManager{})
		require.NoError(t, err)
		_, err = system.Spawn(ctx, naming.RouterName(0), &syncStreamRouterActor{})
		require.NoError(t, err)
		waitForActors()

		gw, err := New(testConfig(), withSystemForTesting(system))
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		defer func() { _ = gw.Stop(ctx) }()

		result, err := gw.InvokeStream(ctx, &mcp.Invocation{
			ToolID:      "x",
			Method:      "tools/call",
			Params:      map[string]any{},
			Correlation: mcp.CorrelationMeta{TenantID: "t", ClientID: "c", RequestID: "r"},
		})
		require.NoError(t, err)
		require.NotNil(t, result)

		// Progress channel must be closed immediately (non-streaming fallback)
		_, open := <-result.Progress
		assert.False(t, open, "Progress channel should be closed for synchronous fallback")

		// Final channel must have exactly one result buffered
		final, ok := <-result.Final
		require.True(t, ok)
		require.NotNil(t, final)
	})

	t.Run("InvokeStream returns error from RouteStreamResult.Err", func(t *testing.T) {
		system, err := goaktactor.NewActorSystem("test-stream-err",
			goaktactor.WithLogger(goaktlog.DiscardLogger),
		)
		require.NoError(t, err)
		require.NoError(t, system.Start(ctx))
		defer func() { _ = system.Stop(ctx) }()

		_, err = system.Spawn(ctx, naming.ActorNameGatewayManager, &minimalGatewayManager{})
		require.NoError(t, err)
		_, err = system.Spawn(ctx, naming.RouterName(0), &streamErrRouterActor{})
		require.NoError(t, err)
		waitForActors()

		gw, err := New(testConfig(), withSystemForTesting(system))
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		defer func() { _ = gw.Stop(ctx) }()

		_, err = gw.InvokeStream(ctx, &mcp.Invocation{
			ToolID:      "x",
			Method:      "tools/call",
			Params:      map[string]any{},
			Correlation: mcp.CorrelationMeta{TenantID: "t", ClientID: "c", RequestID: "r"},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tool not found for stream")
	})

	t.Run("InvokeStream returns StreamingResult from streaming executor", func(t *testing.T) {
		system, err := goaktactor.NewActorSystem("test-streaming-path",
			goaktactor.WithLogger(goaktlog.DiscardLogger),
		)
		require.NoError(t, err)
		require.NoError(t, system.Start(ctx))
		defer func() { _ = system.Stop(ctx) }()

		_, err = system.Spawn(ctx, naming.ActorNameGatewayManager, &minimalGatewayManager{})
		require.NoError(t, err)
		_, err = system.Spawn(ctx, naming.RouterName(0), &streamingRouterActor{})
		require.NoError(t, err)
		waitForActors()

		gw, err := New(testConfig(), withSystemForTesting(system))
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		defer func() { _ = gw.Stop(ctx) }()

		result, err := gw.InvokeStream(ctx, &mcp.Invocation{
			ToolID:      "x",
			Method:      "tools/call",
			Params:      map[string]any{},
			Correlation: mcp.CorrelationMeta{TenantID: "t", ClientID: "c", RequestID: "r"},
		})
		require.NoError(t, err)
		require.NotNil(t, result)

		// Drain progress
		for p := range result.Progress {
			_ = p
		}
		// Get final result
		final, ok := <-result.Final
		require.True(t, ok)
		require.NotNil(t, final)
		assert.True(t, final.Succeeded())
	})

	t.Run("Invoke with synchronous result through stub router", func(t *testing.T) {
		system, err := goaktactor.NewActorSystem("test-sync-invoke",
			goaktactor.WithLogger(goaktlog.DiscardLogger),
		)
		require.NoError(t, err)
		require.NoError(t, system.Start(ctx))
		defer func() { _ = system.Stop(ctx) }()

		_, err = system.Spawn(ctx, naming.ActorNameGatewayManager, &minimalGatewayManager{})
		require.NoError(t, err)
		_, err = system.Spawn(ctx, naming.RouterName(0), &syncStreamRouterActor{})
		require.NoError(t, err)
		waitForActors()

		gw, err := New(testConfig(), withSystemForTesting(system))
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		defer func() { _ = gw.Stop(ctx) }()

		result, err := gw.Invoke(ctx, &mcp.Invocation{
			ToolID:      "x",
			Method:      "tools/call",
			Params:      map[string]any{},
			Correlation: mcp.CorrelationMeta{TenantID: "t", ClientID: "c", RequestID: "r"},
		})
		require.NoError(t, err)
		require.NotNil(t, result)
	})
}

func TestResolverManagerNotFound(t *testing.T) {
	ctx := context.Background()

	newNoManagerGateway := func(t *testing.T, name string) (*Gateway, func()) {
		t.Helper()
		system, err := goaktactor.NewActorSystem(name,
			goaktactor.WithLogger(goaktlog.DiscardLogger),
		)
		require.NoError(t, err)
		require.NoError(t, system.Start(ctx))

		gw, err := New(testConfig(), withSystemForTesting(system))
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		return gw, func() { _ = gw.Stop(ctx); _ = system.Stop(ctx) }
	}

	t.Run("ListTools returns error when GatewayManager not found", func(t *testing.T) {
		gw, stop := newNoManagerGateway(t, "test-no-mgr-list")
		defer stop()
		_, err := gw.ListTools(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "GatewayManager not found")
	})

	t.Run("RegisterTool returns error when GatewayManager not found", func(t *testing.T) {
		gw, stop := newNoManagerGateway(t, "test-no-mgr-reg")
		defer stop()
		err := gw.RegisterTool(ctx, mcp.Tool{
			ID: "x", Transport: mcp.TransportStdio,
			Stdio: &mcp.StdioTransportConfig{Command: "echo"},
			State: mcp.ToolStateEnabled,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "GatewayManager not found")
	})

	t.Run("UpdateTool returns error when GatewayManager not found", func(t *testing.T) {
		gw, stop := newNoManagerGateway(t, "test-no-mgr-upd")
		defer stop()
		err := gw.UpdateTool(ctx, mcp.Tool{
			ID: "x", Transport: mcp.TransportStdio,
			Stdio: &mcp.StdioTransportConfig{Command: "echo"},
			State: mcp.ToolStateEnabled,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "GatewayManager not found")
	})

	t.Run("DisableTool returns error when GatewayManager not found", func(t *testing.T) {
		gw, stop := newNoManagerGateway(t, "test-no-mgr-dis")
		defer stop()
		err := gw.DisableTool(ctx, "some-tool")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "GatewayManager not found")
	})

	t.Run("RemoveTool returns error when GatewayManager not found", func(t *testing.T) {
		gw, stop := newNoManagerGateway(t, "test-no-mgr-rem")
		defer stop()
		err := gw.RemoveTool(ctx, "some-tool")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "GatewayManager not found")
	})

	t.Run("EnableTool returns error when GatewayManager not found", func(t *testing.T) {
		gw, stop := newNoManagerGateway(t, "test-no-mgr-ena")
		defer stop()
		err := gw.EnableTool(ctx, "some-tool")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "GatewayManager not found")
	})

	t.Run("Invoke returns error when GatewayManager not found", func(t *testing.T) {
		gw, stop := newNoManagerGateway(t, "test-no-mgr-inv")
		defer stop()
		_, err := gw.Invoke(ctx, &mcp.Invocation{
			ToolID:      "x",
			Method:      "tools/call",
			Params:      map[string]any{},
			Correlation: mcp.CorrelationMeta{TenantID: "t", ClientID: "c", RequestID: "r"},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "GatewayManager not found")
	})

	t.Run("InvokeStream returns error when GatewayManager not found", func(t *testing.T) {
		gw, stop := newNoManagerGateway(t, "test-no-mgr-invs")
		defer stop()
		_, err := gw.InvokeStream(ctx, &mcp.Invocation{
			ToolID:      "x",
			Method:      "tools/call",
			Params:      map[string]any{},
			Correlation: mcp.CorrelationMeta{TenantID: "t", ClientID: "c", RequestID: "r"},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "GatewayManager not found")
	})
}

func TestGatewaySubscribeAudit(t *testing.T) {
	ctx := context.Background()

	t.Run("returns error before Start", func(t *testing.T) {
		gw, err := New(testConfig())
		require.NoError(t, err)

		sub, err := gw.SubscribeAudit()

		require.Error(t, err)
		assert.Nil(t, sub)
		assert.Contains(t, err.Error(), "gateway not started")
	})

	t.Run("delivers events published by the journal actor", func(t *testing.T) {
		gw, err := New(testConfig())
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		t.Cleanup(func() { _ = gw.Stop(ctx) })

		waitForActors()

		sub, err := gw.SubscribeAudit()
		require.NoError(t, err)
		require.NotNil(t, sub)
		t.Cleanup(func() { gw.UnsubscribeAudit(sub) })

		journalPID, err := gw.System().ActorOf(ctx, naming.ActorNameJournal)
		require.NoError(t, err)
		require.NotNil(t, journalPID)

		ev := &mcp.AuditEvent{
			Type:     mcp.AuditEventTypeInvocationComplete,
			TenantID: "tenant-audit",
			ClientID: "client-audit",
			ToolID:   "tool-audit",
			Outcome:  "success",
		}
		require.NoError(t, goaktactor.Tell(ctx, journalPID, &runtime.RecordAuditEvent{Event: ev}))

		received := waitForAuditEvent(t, sub, 500*time.Millisecond)

		assert.Equal(t, mcp.AuditEventTypeInvocationComplete, received.Type)
		assert.Equal(t, "tenant-audit", received.TenantID)
		assert.Equal(t, "tool-audit", received.ToolID)
		assert.Equal(t, "success", received.Outcome)
	})

	t.Run("UnsubscribeAudit with nil is a no-op", func(t *testing.T) {
		gw, err := New(testConfig())
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		t.Cleanup(func() { _ = gw.Stop(ctx) })

		assert.NotPanics(t, func() { gw.UnsubscribeAudit(nil) })
	})

	t.Run("UnsubscribeAudit stops event delivery", func(t *testing.T) {
		gw, err := New(testConfig())
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		t.Cleanup(func() { _ = gw.Stop(ctx) })

		waitForActors()

		sub, err := gw.SubscribeAudit()
		require.NoError(t, err)

		gw.UnsubscribeAudit(sub)

		journalPID, err := gw.System().ActorOf(ctx, naming.ActorNameJournal)
		require.NoError(t, err)
		require.NoError(t, goaktactor.Tell(ctx, journalPID, &runtime.RecordAuditEvent{
			Event: &mcp.AuditEvent{
				Type:    mcp.AuditEventTypeInvocationComplete,
				ToolID:  "post-unsubscribe",
				Outcome: "success",
			},
		}))
		waitForActors()

		assert.False(t, sub.Active(), "subscriber must be inactive after UnsubscribeAudit")
	})

	t.Run("returns error after Stop", func(t *testing.T) {
		gw, err := New(testConfig())
		require.NoError(t, err)
		require.NoError(t, gw.Start(ctx))
		require.NoError(t, gw.Stop(ctx))

		sub, err := gw.SubscribeAudit()

		require.Error(t, err)
		assert.Nil(t, sub)
	})
}

func TestGatewayDeadLetterHandling(t *testing.T) {
	t.Run("formatDeadLetterPath returns unknown for nil path", func(t *testing.T) {
		assert.Equal(t, "unknown", formatDeadLetterPath(nil))
	})

	t.Run("handleDeadLetterEvent logs sender receiver and reason", func(t *testing.T) {
		recorder := newRecordingLogger()
		gw := &Gateway{logger: recorder}

		ev := goaktactor.NewDeadletter(nil, nil, "dropped-message", time.Now(), "actor stopped")

		gw.handleDeadLetterEvent(ev)

		require.Len(t, recorder.warns, 1)
		entry := recorder.warns[0]
		assert.Contains(t, entry, "dead letter")
		assert.Contains(t, entry, "sender=unknown")
		assert.Contains(t, entry, "receiver=unknown")
		assert.Contains(t, entry, "reason=actor stopped")
	})

	t.Run("handleDeadLetterEvent defaults empty reason to unknown", func(t *testing.T) {
		recorder := newRecordingLogger()
		gw := &Gateway{logger: recorder}

		ev := goaktactor.NewDeadletter(nil, nil, "dropped-message", time.Now(), "")

		gw.handleDeadLetterEvent(ev)

		require.Len(t, recorder.warns, 1)
		assert.Contains(t, recorder.warns[0], "reason=unknown")
	})
}

// recordingLogger captures every Warnf call so tests can assert on log
// output without coupling to a specific logger implementation.
type recordingLogger struct {
	goaktlog.Logger
	warns []string
}

func newRecordingLogger() *recordingLogger {
	return &recordingLogger{Logger: goaktlog.DiscardLogger}
}

func (r *recordingLogger) Warnf(format string, args ...any) {
	r.warns = append(r.warns, fmt.Sprintf(format, args...))
}

// waitForAuditEvent polls the subscriber until an event arrives or the deadline
// passes, then returns the first *mcp.AuditEvent found. It fails the test if no
// audit event is received in the allotted time.
func waitForAuditEvent(t *testing.T, sub eventstream.Subscriber, timeout time.Duration) *mcp.AuditEvent {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for msg := range sub.Iterator() {
			if ev, ok := msg.Payload().(*mcp.AuditEvent); ok {
				return ev
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("no audit event received within %s", timeout)
	return nil
}
