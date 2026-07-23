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
	goaktlog "github.com/tochemey/goakt/v4/log"
	"github.com/tochemey/goakt/v4/testkit"
	noopmetric "go.opentelemetry.io/otel/metric/noop"

	"github.com/tochemey/goakt-mcp/mcp"

	"github.com/tochemey/goakt-mcp/internal/naming"
	"github.com/tochemey/goakt-mcp/internal/runtime"
	actorextension "github.com/tochemey/goakt-mcp/internal/runtime/actor/extension"
	"github.com/tochemey/goakt-mcp/internal/runtime/audit"
	"github.com/tochemey/goakt-mcp/internal/runtime/telemetry"
)

func TestRouterActor(t *testing.T) {
	ctx := context.Background()

	t.Run("successful route and execute", func(t *testing.T) {
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(actorextension.NewToolConfigExtension(), actorextension.NewConfigExtension(cfg)),
		)
		defer stop()

		spawnFoundationalActorsForTest(ctx, system)

		registryPID, err := system.ActorOf(ctx, naming.ActorNameRegistrar)
		require.NoError(t, err)
		tool := validStdioTool("route-tool")
		_, err = goaktactor.Ask(ctx, registryPID, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)
		waitForActors()

		routerPID, err := system.ActorOf(ctx, naming.RouterName(0))
		require.NoError(t, err)

		inv := sessionInvocation("route-tool", "tenant1", "client1")
		resp, err := goaktactor.Ask(ctx, routerPID, &runtime.RouteInvocation{Invocation: inv}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.RouteResult)
		require.True(t, ok)
		require.NoError(t, result.Err)
		require.NotNil(t, result.Result)
		assert.Equal(t, mcp.ExecutionStatusSuccess, result.Result.Status)
		assert.Equal(t, "tenant1", string(result.Result.Correlation.TenantID))
		assert.Equal(t, "client1", string(result.Result.Correlation.ClientID))
	})

	t.Run("tool not found", func(t *testing.T) {
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t, goaktactor.WithExtensions(actorextension.NewConfigExtension(cfg)))
		defer stop()

		spawnFoundationalActorsForTest(ctx, system)

		routerPID, err := system.ActorOf(ctx, naming.RouterName(0))
		require.NoError(t, err)

		inv := sessionInvocation("nonexistent-tool", "default", "default")
		resp, err := goaktactor.Ask(ctx, routerPID, &runtime.RouteInvocation{Invocation: inv}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.RouteResult)
		require.True(t, ok)
		require.Error(t, result.Err)
		assert.ErrorIs(t, result.Err, mcp.ErrToolNotFound)
	})

	t.Run("circuit open rejects work", func(t *testing.T) {
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(actorextension.NewToolConfigExtension(), actorextension.NewConfigExtension(cfg)),
		)
		defer stop()

		spawnFoundationalActorsForTest(ctx, system)

		registryPID, err := system.ActorOf(ctx, naming.ActorNameRegistrar)
		require.NoError(t, err)
		tool := validStdioTool("circuit-tool")
		_, err = goaktactor.Ask(ctx, registryPID, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)
		waitForActors()

		// Open the circuit by reporting failures
		resp, err := goaktactor.Ask(ctx, registryPID, &runtime.GetSupervisor{ToolID: "circuit-tool"}, askTimeout)
		require.NoError(t, err)
		gsResult, ok := resp.(*runtime.GetSupervisorResult)
		require.True(t, ok)
		require.True(t, gsResult.Found)
		supervisorPID, ok := gsResult.Supervisor.(*goaktactor.PID)
		require.True(t, ok)

		for i := 0; i < mcp.DefaultCircuitFailureThreshold; i++ {
			_ = goaktactor.Tell(ctx, supervisorPID, &runtime.ReportFailure{ToolID: tool.ID})
		}
		waitForActors()

		routerPID, err := system.ActorOf(ctx, naming.RouterName(0))
		require.NoError(t, err)

		inv := sessionInvocation("circuit-tool", "default", "default")
		resp, err = goaktactor.Ask(ctx, routerPID, &runtime.RouteInvocation{Invocation: inv}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.RouteResult)
		require.True(t, ok)
		require.Error(t, result.Err)
		var rErr *mcp.RuntimeError
		require.True(t, assert.ErrorAs(t, result.Err, &rErr))
		assert.Equal(t, mcp.ErrCodeToolUnavailable, rErr.Code)
	})

	t.Run("tool disabled", func(t *testing.T) {
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(actorextension.NewToolConfigExtension(), actorextension.NewConfigExtension(cfg)),
		)
		defer stop()

		spawnFoundationalActorsForTest(ctx, system)

		registryPID, err := system.ActorOf(ctx, naming.ActorNameRegistrar)
		require.NoError(t, err)
		tool := validStdioTool("disabled-tool")
		tool.State = mcp.ToolStateDisabled
		_, err = goaktactor.Ask(ctx, registryPID, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)
		waitForActors()

		routerPID, err := system.ActorOf(ctx, naming.RouterName(0))
		require.NoError(t, err)

		inv := sessionInvocation("disabled-tool", "default", "default")
		resp, err := goaktactor.Ask(ctx, routerPID, &runtime.RouteInvocation{Invocation: inv}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.RouteResult)
		require.True(t, ok)
		require.Error(t, result.Err)
		var rErr *mcp.RuntimeError
		require.True(t, assert.ErrorAs(t, result.Err, &rErr))
		assert.Equal(t, mcp.ErrCodeToolDisabled, rErr.Code)
	})

	t.Run("invalid invocation nil", func(t *testing.T) {
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t, goaktactor.WithExtensions(actorextension.NewConfigExtension(cfg)))
		defer stop()

		spawnFoundationalActorsForTest(ctx, system)

		routerPID, err := system.ActorOf(ctx, naming.RouterName(0))
		require.NoError(t, err)

		resp, err := goaktactor.Ask(ctx, routerPID, &runtime.RouteInvocation{Invocation: nil}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.RouteResult)
		require.True(t, ok)
		require.Error(t, result.Err)
		var rErr *mcp.RuntimeError
		require.True(t, assert.ErrorAs(t, result.Err, &rErr))
		assert.Equal(t, mcp.ErrCodeInvalidRequest, rErr.Code)
	})

	t.Run("invalid invocation missing tool ID", func(t *testing.T) {
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t, goaktactor.WithExtensions(actorextension.NewConfigExtension(cfg)))
		defer stop()

		spawnFoundationalActorsForTest(ctx, system)

		routerPID, err := system.ActorOf(ctx, naming.RouterName(0))
		require.NoError(t, err)

		inv := sessionInvocation("", "default", "default")
		resp, err := goaktactor.Ask(ctx, routerPID, &runtime.RouteInvocation{Invocation: inv}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.RouteResult)
		require.True(t, ok)
		require.Error(t, result.Err)
		var rErr *mcp.RuntimeError
		require.True(t, assert.ErrorAs(t, result.Err, &rErr))
		assert.Equal(t, mcp.ErrCodeInvalidRequest, rErr.Code)
	})

	t.Run("policy denies tenant not in allowlist", func(t *testing.T) {
		cfg := testConfigWithTenants("allowed-tenant")
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(actorextension.NewToolConfigExtension(), actorextension.NewConfigExtension(cfg)),
		)
		defer stop()

		spawnFoundationalActorsForTest(ctx, system)

		registryPID, err := system.ActorOf(ctx, naming.ActorNameRegistrar)
		require.NoError(t, err)
		tool := validStdioTool("policy-tool")
		tool.AuthorizationPolicy = mcp.AuthorizationPolicyTenantAllowlist
		_, err = goaktactor.Ask(ctx, registryPID, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)
		waitForActors()

		routerPID, err := system.ActorOf(ctx, naming.RouterName(0))
		require.NoError(t, err)

		inv := sessionInvocation("policy-tool", "denied-tenant", "client-1")
		resp, err := goaktactor.Ask(ctx, routerPID, &runtime.RouteInvocation{Invocation: inv}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.RouteResult)
		require.True(t, ok)
		require.Error(t, result.Err)
		var rErr *mcp.RuntimeError
		require.True(t, assert.ErrorAs(t, result.Err, &rErr))
		assert.Equal(t, mcp.ErrCodePolicyDenied, rErr.Code)
	})

	t.Run("successful route with journal records audit event", func(t *testing.T) {
		sink := audit.NewMemorySink()
		cfg := testConfig()
		cfg.Audit.Sink = sink
		kit, ctx := newTestKit(t,
			testkit.WithExtensions(actorextension.NewToolConfigExtension(), actorextension.NewConfigExtension(cfg)),
		)

		spawnFoundationalActorsForTest(ctx, kit.ActorSystem())

		_, err := kit.ActorSystem().ActorOf(ctx, naming.ActorNameRegistrar)
		require.NoError(t, err)
		tool := validStdioTool("route-journal-tool")
		probe := kit.NewProbe(ctx)
		probe.SendSync(naming.ActorNameRegistrar, &runtime.RegisterTool{Tool: tool}, askTimeout)
		probe.ExpectAnyMessage()
		waitForActors()

		_, err = kit.ActorSystem().ActorOf(ctx, naming.RouterName(0))
		require.NoError(t, err)

		inv := sessionInvocation("route-journal-tool", "tenant1", "client1")
		probe.SendSync(naming.RouterName(0), &runtime.RouteInvocation{Invocation: inv}, askTimeout)
		resp := probe.ExpectAnyMessage()
		result, ok := resp.(*runtime.RouteResult)
		require.True(t, ok)
		require.NoError(t, result.Err)
		require.NotNil(t, result.Result)
		assert.Equal(t, mcp.ExecutionStatusSuccess, result.Result.Status)

		waitForActors()
		events := sink.Events()
		require.NotEmpty(t, events)
		assert.Equal(t, mcp.AuditEventTypeInvocationComplete, events[len(events)-1].Type)
		probe.Stop()
	})

	t.Run("route with CredentialPolicyRequired resolves credentials", func(t *testing.T) {
		cfg := testConfig()
		cfg.Credentials.Providers = []mcp.CredentialsProvider{&mockCredentialProvider{creds: map[string]string{"api_key": "secret123"}}}
		cfg.Credentials.CacheTTL = time.Minute
		cfg.Audit.Sink = audit.NewMemorySink()
		kit, ctx := newTestKit(t,
			testkit.WithExtensions(actorextension.NewToolConfigExtension(), actorextension.NewConfigExtension(cfg)),
		)

		spawnFoundationalActorsForTest(ctx, kit.ActorSystem())

		_, err := kit.ActorSystem().ActorOf(ctx, naming.ActorNameRegistrar)
		require.NoError(t, err)
		tool := validStdioTool("cred-tool")
		tool.CredentialPolicy = mcp.CredentialPolicyRequired
		probe := kit.NewProbe(ctx)
		probe.SendSync(naming.ActorNameRegistrar, &runtime.RegisterTool{Tool: tool}, askTimeout)
		probe.ExpectAnyMessage()
		waitForActors()

		_, err = kit.ActorSystem().ActorOf(ctx, naming.RouterName(0))
		require.NoError(t, err)

		inv := sessionInvocation("cred-tool", "tenant1", "client1")
		probe.SendSync(naming.RouterName(0), &runtime.RouteInvocation{Invocation: inv}, askTimeout)
		resp := probe.ExpectAnyMessage()
		result, ok := resp.(*runtime.RouteResult)
		require.True(t, ok)
		require.NoError(t, result.Err)
		require.NotNil(t, result.Result)
		probe.Stop()
	})

	t.Run("route with CredentialPolicyRequired and unavailable credentials fails", func(t *testing.T) {
		cfg := testConfig()
		cfg.Credentials.Providers = []mcp.CredentialsProvider{&mockCredentialProvider{creds: nil}}
		cfg.Credentials.CacheTTL = time.Minute
		cfg.Audit.Sink = audit.NewMemorySink()
		kit, ctx := newTestKit(t,
			testkit.WithExtensions(actorextension.NewToolConfigExtension(), actorextension.NewConfigExtension(cfg)),
		)

		spawnFoundationalActorsForTest(ctx, kit.ActorSystem())

		_, err := kit.ActorSystem().ActorOf(ctx, naming.ActorNameRegistrar)
		require.NoError(t, err)
		tool := validStdioTool("cred-req-tool")
		tool.CredentialPolicy = mcp.CredentialPolicyRequired
		probe := kit.NewProbe(ctx)
		probe.SendSync(naming.ActorNameRegistrar, &runtime.RegisterTool{Tool: tool}, askTimeout)
		probe.ExpectAnyMessage()
		waitForActors()

		_, err = kit.ActorSystem().ActorOf(ctx, naming.RouterName(0))
		require.NoError(t, err)

		inv := sessionInvocation("cred-req-tool", "tenant1", "client1")
		probe.SendSync(naming.RouterName(0), &runtime.RouteInvocation{Invocation: inv}, askTimeout)
		resp := probe.ExpectAnyMessage()
		result, ok := resp.(*runtime.RouteResult)
		require.True(t, ok)
		require.Error(t, result.Err)
		var rErr *mcp.RuntimeError
		require.True(t, assert.ErrorAs(t, result.Err, &rErr))
		assert.Equal(t, mcp.ErrCodeCredentialUnavailable, rErr.Code)
		probe.Stop()
	})

	t.Run("policy rate limit produces throttle outcome", func(t *testing.T) {
		cfg := testConfig()
		cfg.Tenants = []mcp.TenantConfig{{
			ID: "rate-tenant",
			Quotas: mcp.TenantQuotaConfig{
				RequestsPerMinute: 2,
			},
		}}
		cfg.Audit.Sink = audit.NewMemorySink()
		kit, ctx := newTestKit(t,
			testkit.WithExtensions(actorextension.NewToolConfigExtension(), actorextension.NewConfigExtension(cfg)),
		)

		spawnFoundationalActorsForTest(ctx, kit.ActorSystem())

		_, err := kit.ActorSystem().ActorOf(ctx, naming.ActorNameRegistrar)
		require.NoError(t, err)
		tool := validStdioTool("rate-tool")
		probe := kit.NewProbe(ctx)
		probe.SendSync(naming.ActorNameRegistrar, &runtime.RegisterTool{Tool: tool}, askTimeout)
		probe.ExpectAnyMessage()
		waitForActors()

		_, err = kit.ActorSystem().ActorOf(ctx, naming.RouterName(0))
		require.NoError(t, err)

		inv := sessionInvocation("rate-tool", "rate-tenant", "client1")
		probe.SendSync(naming.RouterName(0), &runtime.RouteInvocation{Invocation: inv}, askTimeout)
		resp := probe.ExpectAnyMessage()
		result, ok := resp.(*runtime.RouteResult)
		require.True(t, ok)
		require.NoError(t, result.Err)

		inv2 := sessionInvocation("rate-tool", "rate-tenant", "client1")
		inv2.Correlation.RequestID = "req-2"
		probe.SendSync(naming.RouterName(0), &runtime.RouteInvocation{Invocation: inv2}, askTimeout)
		resp2 := probe.ExpectAnyMessage()
		result2, ok := resp2.(*runtime.RouteResult)
		require.True(t, ok)
		require.NoError(t, result2.Err)

		// Third request exceeds limit (allow N, throttle N+1)
		inv3 := sessionInvocation("rate-tool", "rate-tenant", "client1")
		inv3.Correlation.RequestID = "req-3"
		probe.SendSync(naming.RouterName(0), &runtime.RouteInvocation{Invocation: inv3}, askTimeout)
		resp3 := probe.ExpectAnyMessage()
		result3, ok := resp3.(*runtime.RouteResult)
		require.True(t, ok)
		require.Error(t, result3.Err)
		var rErr *mcp.RuntimeError
		require.True(t, assert.ErrorAs(t, result3.Err, &rErr))
		assert.Equal(t, mcp.ErrCodeRateLimited, rErr.Code)
		probe.Stop()
	})

	t.Run("records InvocationLatency metric on success when metrics are registered", func(t *testing.T) {
		meter := noopmetric.NewMeterProvider().Meter("test")
		_, err := telemetry.RegisterMetrics(meter)
		require.NoError(t, err)
		t.Cleanup(telemetry.UnregisterMetrics)

		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t, goaktactor.WithExtensions(actorextension.NewToolConfigExtension(), actorextension.NewConfigExtension(cfg)))
		defer stop()

		spawnFoundationalActorsForTest(ctx, system)

		registryPID, err := system.ActorOf(ctx, naming.ActorNameRegistrar)
		require.NoError(t, err)
		tool := validStdioTool("metrics-route-tool")
		_, err = goaktactor.Ask(ctx, registryPID, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)
		waitForActors()

		routerPID, err := system.ActorOf(ctx, naming.RouterName(0))
		require.NoError(t, err)

		inv := sessionInvocation("metrics-route-tool", "tenant1", "client1")
		resp, err := goaktactor.Ask(ctx, routerPID, &runtime.RouteInvocation{Invocation: inv}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.RouteResult)
		require.True(t, ok)
		require.NoError(t, result.Err)
	})

	t.Run("route with empty tenant and client defaults to 'default'", func(t *testing.T) {
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(actorextension.NewToolConfigExtension(), actorextension.NewConfigExtension(cfg)),
		)
		defer stop()

		spawnFoundationalActorsForTest(ctx, system)

		registryPID, err := system.ActorOf(ctx, naming.ActorNameRegistrar)
		require.NoError(t, err)
		tool := validStdioTool("default-tenant-tool")
		_, err = goaktactor.Ask(ctx, registryPID, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)
		waitForActors()

		routerPID, err := system.ActorOf(ctx, naming.RouterName(0))
		require.NoError(t, err)

		inv := &mcp.Invocation{
			Correlation: mcp.CorrelationMeta{RequestID: "req-1"},
			ToolID:      "default-tenant-tool",
			Method:      "tools/call",
			Params:      map[string]any{},
		}
		resp, err := goaktactor.Ask(ctx, routerPID, &runtime.RouteInvocation{Invocation: inv}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.RouteResult)
		require.True(t, ok)
		require.NoError(t, result.Err)
		require.NotNil(t, result.Result)
		assert.Equal(t, mcp.ExecutionStatusSuccess, result.Result.Status)
	})

	t.Run("draining tool rejects invocation", func(t *testing.T) {
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(actorextension.NewToolConfigExtension(), actorextension.NewConfigExtension(cfg)),
		)
		defer stop()

		spawnFoundationalActorsForTest(ctx, system)

		registryPID, err := system.ActorOf(ctx, naming.ActorNameRegistrar)
		require.NoError(t, err)
		tool := validStdioTool("drain-route-tool")
		_, err = goaktactor.Ask(ctx, registryPID, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)
		waitForActors()

		// Drain the tool via the registrar
		_, err = goaktactor.Ask(ctx, registryPID, &runtime.DrainTool{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)
		waitForActors()

		routerPID, err := system.ActorOf(ctx, naming.RouterName(0))
		require.NoError(t, err)

		inv := sessionInvocation("drain-route-tool", "default", "default")
		resp, err := goaktactor.Ask(ctx, routerPID, &runtime.RouteInvocation{Invocation: inv}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.RouteResult)
		require.True(t, ok)
		require.Error(t, result.Err)
		var rErr *mcp.RuntimeError
		require.True(t, assert.ErrorAs(t, result.Err, &rErr))
		assert.Equal(t, mcp.ErrCodeToolUnavailable, rErr.Code)
	})

	t.Run("records policy evaluation latency on deny when metrics are registered", func(t *testing.T) {
		meter := noopmetric.NewMeterProvider().Meter("test")
		_, err := telemetry.RegisterMetrics(meter)
		require.NoError(t, err)
		t.Cleanup(telemetry.UnregisterMetrics)

		cfg := testConfigWithTenants("allowed-tenant")
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(actorextension.NewToolConfigExtension(), actorextension.NewConfigExtension(cfg)),
		)
		defer stop()

		spawnFoundationalActorsForTest(ctx, system)

		registryPID, err := system.ActorOf(ctx, naming.ActorNameRegistrar)
		require.NoError(t, err)
		tool := validStdioTool("policy-metric-tool")
		tool.AuthorizationPolicy = mcp.AuthorizationPolicyTenantAllowlist
		_, err = goaktactor.Ask(ctx, registryPID, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)
		waitForActors()

		routerPID, err := system.ActorOf(ctx, naming.RouterName(0))
		require.NoError(t, err)

		inv := sessionInvocation("policy-metric-tool", "denied-tenant", "client-1")
		resp, err := goaktactor.Ask(ctx, routerPID, &runtime.RouteInvocation{Invocation: inv}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.RouteResult)
		require.True(t, ok)
		require.Error(t, result.Err)
		var rErr *mcp.RuntimeError
		require.True(t, assert.ErrorAs(t, result.Err, &rErr))
		assert.Equal(t, mcp.ErrCodePolicyDenied, rErr.Code)
	})

	t.Run("records policy evaluation latency with throttle decision when rate limited", func(t *testing.T) {
		meter := noopmetric.NewMeterProvider().Meter("test")
		_, err := telemetry.RegisterMetrics(meter)
		require.NoError(t, err)
		t.Cleanup(telemetry.UnregisterMetrics)

		cfg := testConfig()
		cfg.Tenants = []mcp.TenantConfig{{
			ID:     "throttle-tenant",
			Quotas: mcp.TenantQuotaConfig{RequestsPerMinute: 1},
		}}
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(actorextension.NewToolConfigExtension(), actorextension.NewConfigExtension(cfg)),
		)
		defer stop()

		spawnFoundationalActorsForTest(ctx, system)

		registryPID, err := system.ActorOf(ctx, naming.ActorNameRegistrar)
		require.NoError(t, err)
		tool := validStdioTool("throttle-metric-tool")
		_, err = goaktactor.Ask(ctx, registryPID, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)
		waitForActors()

		routerPID, err := system.ActorOf(ctx, naming.RouterName(0))
		require.NoError(t, err)

		// First request succeeds (within limit)
		inv1 := sessionInvocation("throttle-metric-tool", "throttle-tenant", "client-1")
		resp1, err := goaktactor.Ask(ctx, routerPID, &runtime.RouteInvocation{Invocation: inv1}, askTimeout)
		require.NoError(t, err)
		result1, ok := resp1.(*runtime.RouteResult)
		require.True(t, ok)
		require.NoError(t, result1.Err)

		// Second request should be throttled (exceeds 1 RPM)
		inv2 := sessionInvocation("throttle-metric-tool", "throttle-tenant", "client-1")
		inv2.Correlation.RequestID = "req-throttle"
		resp2, err := goaktactor.Ask(ctx, routerPID, &runtime.RouteInvocation{Invocation: inv2}, askTimeout)
		require.NoError(t, err)
		result2, ok := resp2.(*runtime.RouteResult)
		require.True(t, ok)
		require.Error(t, result2.Err)
		var rErr *mcp.RuntimeError
		require.True(t, assert.ErrorAs(t, result2.Err, &rErr))
		assert.Equal(t, mcp.ErrCodeRateLimited, rErr.Code)
	})

	t.Run("records InvocationFailure metric on tool-not-found when metrics are registered", func(t *testing.T) {
		meter := noopmetric.NewMeterProvider().Meter("test")
		_, err := telemetry.RegisterMetrics(meter)
		require.NoError(t, err)
		t.Cleanup(telemetry.UnregisterMetrics)

		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t, goaktactor.WithExtensions(actorextension.NewToolConfigExtension(), actorextension.NewConfigExtension(cfg)))
		defer stop()

		spawnFoundationalActorsForTest(ctx, system)

		routerPID, err := system.ActorOf(ctx, naming.RouterName(0))
		require.NoError(t, err)

		inv := sessionInvocation("nonexistent-tool", "tenant1", "client1")
		resp, err := goaktactor.Ask(ctx, routerPID, &runtime.RouteInvocation{Invocation: inv}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.RouteResult)
		require.True(t, ok)
		require.Error(t, result.Err)
	})

	// ===== Streaming tests =====

	t.Run("stream: successful route and execute", func(t *testing.T) {
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(actorextension.NewToolConfigExtension(), actorextension.NewConfigExtension(cfg)),
		)
		defer stop()

		spawnFoundationalActorsForTest(ctx, system)

		registryPID, err := system.ActorOf(ctx, naming.ActorNameRegistrar)
		require.NoError(t, err)
		tool := validStdioTool("stream-route-tool")
		_, err = goaktactor.Ask(ctx, registryPID, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)
		waitForActors()

		routerPID, err := system.ActorOf(ctx, naming.RouterName(0))
		require.NoError(t, err)

		inv := sessionInvocation("stream-route-tool", "tenant1", "client1")
		resp, err := goaktactor.Ask(ctx, routerPID, &runtime.RouteInvokeStream{Invocation: inv}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.RouteStreamResult)
		require.True(t, ok)
		require.NoError(t, result.Err)
		// stdio executors don't implement ToolStreamExecutor, so falls back to sync
		require.NotNil(t, result.Result)
		assert.Equal(t, mcp.ExecutionStatusSuccess, result.Result.Status)
	})

	t.Run("stream: tool not found", func(t *testing.T) {
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t, goaktactor.WithExtensions(actorextension.NewConfigExtension(cfg)))
		defer stop()

		spawnFoundationalActorsForTest(ctx, system)

		routerPID, err := system.ActorOf(ctx, naming.RouterName(0))
		require.NoError(t, err)

		inv := sessionInvocation("nonexistent-stream-tool", "default", "default")
		resp, err := goaktactor.Ask(ctx, routerPID, &runtime.RouteInvokeStream{Invocation: inv}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.RouteStreamResult)
		require.True(t, ok)
		require.Error(t, result.Err)
		assert.ErrorIs(t, result.Err, mcp.ErrToolNotFound)
	})

	t.Run("stream: invalid invocation nil", func(t *testing.T) {
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t, goaktactor.WithExtensions(actorextension.NewConfigExtension(cfg)))
		defer stop()

		spawnFoundationalActorsForTest(ctx, system)

		routerPID, err := system.ActorOf(ctx, naming.RouterName(0))
		require.NoError(t, err)

		resp, err := goaktactor.Ask(ctx, routerPID, &runtime.RouteInvokeStream{Invocation: nil}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.RouteStreamResult)
		require.True(t, ok)
		require.Error(t, result.Err)
		var rErr *mcp.RuntimeError
		require.True(t, assert.ErrorAs(t, result.Err, &rErr))
		assert.Equal(t, mcp.ErrCodeInvalidRequest, rErr.Code)
	})

	t.Run("stream: invalid invocation missing tool ID", func(t *testing.T) {
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t, goaktactor.WithExtensions(actorextension.NewConfigExtension(cfg)))
		defer stop()

		spawnFoundationalActorsForTest(ctx, system)

		routerPID, err := system.ActorOf(ctx, naming.RouterName(0))
		require.NoError(t, err)

		inv := sessionInvocation("", "default", "default")
		resp, err := goaktactor.Ask(ctx, routerPID, &runtime.RouteInvokeStream{Invocation: inv}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.RouteStreamResult)
		require.True(t, ok)
		require.Error(t, result.Err)
		var rErr *mcp.RuntimeError
		require.True(t, assert.ErrorAs(t, result.Err, &rErr))
		assert.Equal(t, mcp.ErrCodeInvalidRequest, rErr.Code)
	})

	t.Run("stream: policy denies tenant not in allowlist", func(t *testing.T) {
		cfg := testConfigWithTenants("allowed-tenant")
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(actorextension.NewToolConfigExtension(), actorextension.NewConfigExtension(cfg)),
		)
		defer stop()

		spawnFoundationalActorsForTest(ctx, system)

		registryPID, err := system.ActorOf(ctx, naming.ActorNameRegistrar)
		require.NoError(t, err)
		tool := validStdioTool("stream-policy-tool")
		tool.AuthorizationPolicy = mcp.AuthorizationPolicyTenantAllowlist
		_, err = goaktactor.Ask(ctx, registryPID, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)
		waitForActors()

		routerPID, err := system.ActorOf(ctx, naming.RouterName(0))
		require.NoError(t, err)

		inv := sessionInvocation("stream-policy-tool", "denied-tenant", "client-1")
		resp, err := goaktactor.Ask(ctx, routerPID, &runtime.RouteInvokeStream{Invocation: inv}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.RouteStreamResult)
		require.True(t, ok)
		require.Error(t, result.Err)
		var rErr *mcp.RuntimeError
		require.True(t, assert.ErrorAs(t, result.Err, &rErr))
		assert.Equal(t, mcp.ErrCodePolicyDenied, rErr.Code)
	})

	t.Run("stream: circuit open rejects work", func(t *testing.T) {
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(actorextension.NewToolConfigExtension(), actorextension.NewConfigExtension(cfg)),
		)
		defer stop()

		spawnFoundationalActorsForTest(ctx, system)

		registryPID, err := system.ActorOf(ctx, naming.ActorNameRegistrar)
		require.NoError(t, err)
		tool := validStdioTool("stream-circuit-tool")
		_, err = goaktactor.Ask(ctx, registryPID, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)
		waitForActors()

		// Open the circuit by reporting failures
		resp, err := goaktactor.Ask(ctx, registryPID, &runtime.GetSupervisor{ToolID: "stream-circuit-tool"}, askTimeout)
		require.NoError(t, err)
		gsResult, ok := resp.(*runtime.GetSupervisorResult)
		require.True(t, ok)
		require.True(t, gsResult.Found)
		supervisorPID, ok := gsResult.Supervisor.(*goaktactor.PID)
		require.True(t, ok)

		for i := 0; i < mcp.DefaultCircuitFailureThreshold; i++ {
			_ = goaktactor.Tell(ctx, supervisorPID, &runtime.ReportFailure{ToolID: tool.ID})
		}
		waitForActors()

		routerPID, err := system.ActorOf(ctx, naming.RouterName(0))
		require.NoError(t, err)

		inv := sessionInvocation("stream-circuit-tool", "default", "default")
		resp, err = goaktactor.Ask(ctx, routerPID, &runtime.RouteInvokeStream{Invocation: inv}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.RouteStreamResult)
		require.True(t, ok)
		require.Error(t, result.Err)
		var rErr *mcp.RuntimeError
		require.True(t, assert.ErrorAs(t, result.Err, &rErr))
		assert.Equal(t, mcp.ErrCodeToolUnavailable, rErr.Code)
	})

	t.Run("stream: disabled tool", func(t *testing.T) {
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(actorextension.NewToolConfigExtension(), actorextension.NewConfigExtension(cfg)),
		)
		defer stop()

		spawnFoundationalActorsForTest(ctx, system)

		registryPID, err := system.ActorOf(ctx, naming.ActorNameRegistrar)
		require.NoError(t, err)
		tool := validStdioTool("stream-disabled-tool")
		tool.State = mcp.ToolStateDisabled
		_, err = goaktactor.Ask(ctx, registryPID, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)
		waitForActors()

		routerPID, err := system.ActorOf(ctx, naming.RouterName(0))
		require.NoError(t, err)

		inv := sessionInvocation("stream-disabled-tool", "default", "default")
		resp, err := goaktactor.Ask(ctx, routerPID, &runtime.RouteInvokeStream{Invocation: inv}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.RouteStreamResult)
		require.True(t, ok)
		require.Error(t, result.Err)
		var rErr *mcp.RuntimeError
		require.True(t, assert.ErrorAs(t, result.Err, &rErr))
		assert.Equal(t, mcp.ErrCodeToolDisabled, rErr.Code)
	})

	t.Run("stream: credential required and resolved", func(t *testing.T) {
		cfg := testConfig()
		cfg.Credentials.Providers = []mcp.CredentialsProvider{&mockCredentialProvider{creds: map[string]string{"api_key": "secret123"}}}
		cfg.Credentials.CacheTTL = time.Minute
		cfg.Audit.Sink = audit.NewMemorySink()
		kit, ctx := newTestKit(t,
			testkit.WithExtensions(actorextension.NewToolConfigExtension(), actorextension.NewConfigExtension(cfg)),
		)

		spawnFoundationalActorsForTest(ctx, kit.ActorSystem())

		_, err := kit.ActorSystem().ActorOf(ctx, naming.ActorNameRegistrar)
		require.NoError(t, err)
		tool := validStdioTool("stream-cred-tool")
		tool.CredentialPolicy = mcp.CredentialPolicyRequired
		probe := kit.NewProbe(ctx)
		probe.SendSync(naming.ActorNameRegistrar, &runtime.RegisterTool{Tool: tool}, askTimeout)
		probe.ExpectAnyMessage()
		waitForActors()

		_, err = kit.ActorSystem().ActorOf(ctx, naming.RouterName(0))
		require.NoError(t, err)

		inv := sessionInvocation("stream-cred-tool", "tenant1", "client1")
		probe.SendSync(naming.RouterName(0), &runtime.RouteInvokeStream{Invocation: inv}, askTimeout)
		resp := probe.ExpectAnyMessage()
		result, ok := resp.(*runtime.RouteStreamResult)
		require.True(t, ok)
		require.NoError(t, result.Err)
		require.NotNil(t, result.Result)
		probe.Stop()
	})

	t.Run("stream: credential required and unavailable", func(t *testing.T) {
		cfg := testConfig()
		cfg.Credentials.Providers = []mcp.CredentialsProvider{&mockCredentialProvider{creds: nil}}
		cfg.Credentials.CacheTTL = time.Minute
		cfg.Audit.Sink = audit.NewMemorySink()
		kit, ctx := newTestKit(t,
			testkit.WithExtensions(actorextension.NewToolConfigExtension(), actorextension.NewConfigExtension(cfg)),
		)

		spawnFoundationalActorsForTest(ctx, kit.ActorSystem())

		_, err := kit.ActorSystem().ActorOf(ctx, naming.ActorNameRegistrar)
		require.NoError(t, err)
		tool := validStdioTool("stream-cred-req-tool")
		tool.CredentialPolicy = mcp.CredentialPolicyRequired
		probe := kit.NewProbe(ctx)
		probe.SendSync(naming.ActorNameRegistrar, &runtime.RegisterTool{Tool: tool}, askTimeout)
		probe.ExpectAnyMessage()
		waitForActors()

		_, err = kit.ActorSystem().ActorOf(ctx, naming.RouterName(0))
		require.NoError(t, err)

		inv := sessionInvocation("stream-cred-req-tool", "tenant1", "client1")
		probe.SendSync(naming.RouterName(0), &runtime.RouteInvokeStream{Invocation: inv}, askTimeout)
		resp := probe.ExpectAnyMessage()
		result, ok := resp.(*runtime.RouteStreamResult)
		require.True(t, ok)
		require.Error(t, result.Err)
		var rErr *mcp.RuntimeError
		require.True(t, assert.ErrorAs(t, result.Err, &rErr))
		assert.Equal(t, mcp.ErrCodeCredentialUnavailable, rErr.Code)
		probe.Stop()
	})

	t.Run("stream: records audit event on success", func(t *testing.T) {
		sink := audit.NewMemorySink()
		cfg := testConfig()
		cfg.Audit.Sink = sink
		kit, ctx := newTestKit(t,
			testkit.WithExtensions(actorextension.NewToolConfigExtension(), actorextension.NewConfigExtension(cfg)),
		)

		spawnFoundationalActorsForTest(ctx, kit.ActorSystem())

		_, err := kit.ActorSystem().ActorOf(ctx, naming.ActorNameRegistrar)
		require.NoError(t, err)
		tool := validStdioTool("stream-audit-tool")
		probe := kit.NewProbe(ctx)
		probe.SendSync(naming.ActorNameRegistrar, &runtime.RegisterTool{Tool: tool}, askTimeout)
		probe.ExpectAnyMessage()
		waitForActors()

		_, err = kit.ActorSystem().ActorOf(ctx, naming.RouterName(0))
		require.NoError(t, err)

		inv := sessionInvocation("stream-audit-tool", "tenant1", "client1")
		probe.SendSync(naming.RouterName(0), &runtime.RouteInvokeStream{Invocation: inv}, askTimeout)
		resp := probe.ExpectAnyMessage()
		result, ok := resp.(*runtime.RouteStreamResult)
		require.True(t, ok)
		require.NoError(t, result.Err)
		require.NotNil(t, result.Result)

		waitForActors()
		events := sink.Events()
		require.NotEmpty(t, events)
		assert.Equal(t, mcp.AuditEventTypeInvocationComplete, events[len(events)-1].Type)
		probe.Stop()
	})

	t.Run("stream: records InvocationLatency metric on success", func(t *testing.T) {
		meter := noopmetric.NewMeterProvider().Meter("test")
		_, err := telemetry.RegisterMetrics(meter)
		require.NoError(t, err)
		t.Cleanup(telemetry.UnregisterMetrics)

		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t, goaktactor.WithExtensions(actorextension.NewToolConfigExtension(), actorextension.NewConfigExtension(cfg)))
		defer stop()

		spawnFoundationalActorsForTest(ctx, system)

		registryPID, err := system.ActorOf(ctx, naming.ActorNameRegistrar)
		require.NoError(t, err)
		tool := validStdioTool("stream-metrics-tool")
		_, err = goaktactor.Ask(ctx, registryPID, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)
		waitForActors()

		routerPID, err := system.ActorOf(ctx, naming.RouterName(0))
		require.NoError(t, err)

		inv := sessionInvocation("stream-metrics-tool", "tenant1", "client1")
		resp, err := goaktactor.Ask(ctx, routerPID, &runtime.RouteInvokeStream{Invocation: inv}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.RouteStreamResult)
		require.True(t, ok)
		require.NoError(t, result.Err)
	})

	t.Run("stream: default tenant and client", func(t *testing.T) {
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(actorextension.NewToolConfigExtension(), actorextension.NewConfigExtension(cfg)),
		)
		defer stop()

		spawnFoundationalActorsForTest(ctx, system)

		registryPID, err := system.ActorOf(ctx, naming.ActorNameRegistrar)
		require.NoError(t, err)
		tool := validStdioTool("stream-default-tool")
		_, err = goaktactor.Ask(ctx, registryPID, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)
		waitForActors()

		routerPID, err := system.ActorOf(ctx, naming.RouterName(0))
		require.NoError(t, err)

		inv := &mcp.Invocation{
			Correlation: mcp.CorrelationMeta{RequestID: "req-1"},
			ToolID:      "stream-default-tool",
			Method:      "tools/call",
			Params:      map[string]any{},
		}
		resp, err := goaktactor.Ask(ctx, routerPID, &runtime.RouteInvokeStream{Invocation: inv}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.RouteStreamResult)
		require.True(t, ok)
		require.NoError(t, result.Err)
		require.NotNil(t, result.Result)
		assert.Equal(t, mcp.ExecutionStatusSuccess, result.Result.Status)
	})

	t.Run("stream: draining tool rejects invocation", func(t *testing.T) {
		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(actorextension.NewToolConfigExtension(), actorextension.NewConfigExtension(cfg)),
		)
		defer stop()

		spawnFoundationalActorsForTest(ctx, system)

		registryPID, err := system.ActorOf(ctx, naming.ActorNameRegistrar)
		require.NoError(t, err)
		tool := validStdioTool("stream-drain-tool")
		_, err = goaktactor.Ask(ctx, registryPID, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)
		waitForActors()

		// Drain the tool
		_, err = goaktactor.Ask(ctx, registryPID, &runtime.DrainTool{ToolID: tool.ID}, askTimeout)
		require.NoError(t, err)
		waitForActors()

		routerPID, err := system.ActorOf(ctx, naming.RouterName(0))
		require.NoError(t, err)

		inv := sessionInvocation("stream-drain-tool", "default", "default")
		resp, err := goaktactor.Ask(ctx, routerPID, &runtime.RouteInvokeStream{Invocation: inv}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.RouteStreamResult)
		require.True(t, ok)
		require.Error(t, result.Err)
		var rErr *mcp.RuntimeError
		require.True(t, assert.ErrorAs(t, result.Err, &rErr))
		assert.Equal(t, mcp.ErrCodeToolUnavailable, rErr.Code)
	})
}

func TestOutcomeFromError(t *testing.T) {
	t.Run("nil error returns empty string", func(t *testing.T) {
		assert.Equal(t, "", outcomeFromError(nil))
	})

	t.Run("QuotaExceeded returns throttle", func(t *testing.T) {
		err := mcp.NewRuntimeError(mcp.ErrCodeQuotaExceeded, "quota exceeded")
		assert.Equal(t, "throttle", outcomeFromError(err))
	})

	t.Run("ConcurrencyLimitReached returns throttle", func(t *testing.T) {
		err := mcp.NewRuntimeError(mcp.ErrCodeConcurrencyLimitReached, "too many sessions")
		assert.Equal(t, "throttle", outcomeFromError(err))
	})

	t.Run("PolicyDenied returns deny", func(t *testing.T) {
		err := mcp.NewRuntimeError(mcp.ErrCodePolicyDenied, "denied")
		assert.Equal(t, "deny", outcomeFromError(err))
	})

	t.Run("non-RuntimeError returns error", func(t *testing.T) {
		err := errors.New("some error")
		assert.Equal(t, "error", outcomeFromError(err))
	})
}

func TestInvocationEventNil(t *testing.T) {
	result := invocationEvent(nil, mcp.AuditEventTypeInvocationComplete, "success", "", "")
	assert.Nil(t, result)
}

func TestRouterLogAuditError(t *testing.T) {
	t.Run("logs warning when error is non-nil", func(t *testing.T) {
		r := &router{
			logger: goaktlog.DiscardLogger,
		}
		// Should not panic; exercises the err != nil branch
		r.logAuditError(errors.New("audit write failed"))
	})

	t.Run("no-op when error is nil", func(t *testing.T) {
		r := &router{
			logger: goaktlog.DiscardLogger,
		}
		r.logAuditError(nil)
	})
}

func TestRouterRecordAuditEventNilJournaler(t *testing.T) {
	r := &router{
		logger: goaktlog.DiscardLogger,
	}
	// journaler is nil, should be no-op and return nil
	err := r.recordAuditEvent(context.Background(), &mcp.AuditEvent{})
	assert.NoError(t, err)
}

func TestRouterResolveCredentialsBrokerNil(t *testing.T) {
	r := &router{
		logger: goaktlog.DiscardLogger,
	}
	tool := validStdioTool("test-tool")
	tool.CredentialPolicy = mcp.CredentialPolicyRequired

	inv := sessionInvocation("test-tool", "t", "c")
	_, err := r.resolveCredentials(context.Background(), inv, tool, "t")
	require.Error(t, err)
	var rErr *mcp.RuntimeError
	require.True(t, assert.ErrorAs(t, err, &rErr))
	assert.Equal(t, mcp.ErrCodeCredentialUnavailable, rErr.Code)
}

func TestRouterResolveCredentialsNotRequired(t *testing.T) {
	r := &router{
		logger: goaktlog.DiscardLogger,
	}
	tool := validStdioTool("test-tool")
	// CredentialPolicy defaults to not-required

	inv := sessionInvocation("test-tool", "t", "c")
	result, err := r.resolveCredentials(context.Background(), inv, tool, "t")
	require.NoError(t, err)
	assert.Equal(t, inv, result)
}

func TestRouterEvaluatePolicyNilPolicyMaker(t *testing.T) {
	r := &router{
		logger: goaktlog.DiscardLogger,
	}
	// policyMaker is nil, should return nil (policy is optional)
	inv := sessionInvocation("test-tool", "t", "c")
	tool := validStdioTool("test-tool")
	err := r.evaluatePolicy(context.Background(), inv, tool, "t", "c")
	assert.NoError(t, err)
}

func TestRouterPreStartDefaultRequestTimeout(t *testing.T) {
	// Verify that the requestTimeout default is applied
	cfg := testConfig()
	cfg.Runtime.RequestTimeout = 0
	cfg.Audit.Sink = audit.NewMemorySink()
	system, stop := testActorSystem(t, goaktactor.WithExtensions(
		actorextension.NewToolConfigExtension(),
		actorextension.NewConfigExtension(cfg),
	))
	defer stop()

	ctx := context.Background()
	spawnFoundationalActorsForTest(ctx, system)

	// Just verify the router starts successfully with zero timeout config
	routerPID, err := system.ActorOf(ctx, naming.RouterName(0))
	require.NoError(t, err)
	require.True(t, routerPID.IsRunning())
}

func TestRouterTracingEnabled(t *testing.T) {
	t.Run("successful route with tracing enabled", func(t *testing.T) {
		// Enable tracing flag via RegisterTracer (uses global noop provider by default)
		telemetry.RegisterTracer()
		t.Cleanup(telemetry.UnregisterTracer)

		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(actorextension.NewToolConfigExtension(), actorextension.NewConfigExtension(cfg)),
		)
		defer stop()

		ctx := context.Background()
		spawnFoundationalActorsForTest(ctx, system)

		registryPID, err := system.ActorOf(ctx, naming.ActorNameRegistrar)
		require.NoError(t, err)
		tool := validStdioTool("trace-route-tool")
		_, err = goaktactor.Ask(ctx, registryPID, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)
		waitForActors()

		routerPID, err := system.ActorOf(ctx, naming.RouterName(0))
		require.NoError(t, err)

		inv := sessionInvocation("trace-route-tool", "tenant1", "client1")
		resp, err := goaktactor.Ask(ctx, routerPID, &runtime.RouteInvocation{Invocation: inv}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.RouteResult)
		require.True(t, ok)
		require.NoError(t, result.Err)
		require.NotNil(t, result.Result)
		assert.Equal(t, mcp.ExecutionStatusSuccess, result.Result.Status)
	})

	t.Run("tool not found with tracing enabled", func(t *testing.T) {
		telemetry.RegisterTracer()
		t.Cleanup(telemetry.UnregisterTracer)

		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t, goaktactor.WithExtensions(actorextension.NewConfigExtension(cfg)))
		defer stop()

		ctx := context.Background()
		spawnFoundationalActorsForTest(ctx, system)

		routerPID, err := system.ActorOf(ctx, naming.RouterName(0))
		require.NoError(t, err)

		inv := sessionInvocation("nonexistent-trace-tool", "default", "default")
		resp, err := goaktactor.Ask(ctx, routerPID, &runtime.RouteInvocation{Invocation: inv}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.RouteResult)
		require.True(t, ok)
		require.Error(t, result.Err)
	})

	t.Run("stream successful route with tracing enabled", func(t *testing.T) {
		telemetry.RegisterTracer()
		t.Cleanup(telemetry.UnregisterTracer)

		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(actorextension.NewToolConfigExtension(), actorextension.NewConfigExtension(cfg)),
		)
		defer stop()

		ctx := context.Background()
		spawnFoundationalActorsForTest(ctx, system)

		registryPID, err := system.ActorOf(ctx, naming.ActorNameRegistrar)
		require.NoError(t, err)
		tool := validStdioTool("trace-stream-tool")
		_, err = goaktactor.Ask(ctx, registryPID, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)
		waitForActors()

		routerPID, err := system.ActorOf(ctx, naming.RouterName(0))
		require.NoError(t, err)

		inv := sessionInvocation("trace-stream-tool", "tenant1", "client1")
		resp, err := goaktactor.Ask(ctx, routerPID, &runtime.RouteInvokeStream{Invocation: inv}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.RouteStreamResult)
		require.True(t, ok)
		require.NoError(t, result.Err)
		require.NotNil(t, result.Result)
	})

	t.Run("stream tool not found with tracing enabled", func(t *testing.T) {
		telemetry.RegisterTracer()
		t.Cleanup(telemetry.UnregisterTracer)

		cfg := testConfig()
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t, goaktactor.WithExtensions(actorextension.NewConfigExtension(cfg)))
		defer stop()

		ctx := context.Background()
		spawnFoundationalActorsForTest(ctx, system)

		routerPID, err := system.ActorOf(ctx, naming.RouterName(0))
		require.NoError(t, err)

		inv := sessionInvocation("nonexistent-trace-stream", "default", "default")
		resp, err := goaktactor.Ask(ctx, routerPID, &runtime.RouteInvokeStream{Invocation: inv}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.RouteStreamResult)
		require.True(t, ok)
		require.Error(t, result.Err)
	})
}

func TestRouterTracingWithPolicyDeny(t *testing.T) {
	t.Run("route invocation policy deny with tracing covers span error path", func(t *testing.T) {
		telemetry.RegisterTracer()
		t.Cleanup(telemetry.UnregisterTracer)

		cfg := testConfigWithTenants("allowed-tenant")
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(actorextension.NewToolConfigExtension(), actorextension.NewConfigExtension(cfg)),
		)
		defer stop()

		ctx := context.Background()
		spawnFoundationalActorsForTest(ctx, system)

		registryPID, err := system.ActorOf(ctx, naming.ActorNameRegistrar)
		require.NoError(t, err)
		tool := validStdioTool("trace-policy-tool")
		tool.AuthorizationPolicy = mcp.AuthorizationPolicyTenantAllowlist
		_, err = goaktactor.Ask(ctx, registryPID, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)
		waitForActors()

		routerPID, err := system.ActorOf(ctx, naming.RouterName(0))
		require.NoError(t, err)

		inv := sessionInvocation("trace-policy-tool", "denied-tenant", "client-1")
		resp, err := goaktactor.Ask(ctx, routerPID, &runtime.RouteInvocation{Invocation: inv}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.RouteResult)
		require.True(t, ok)
		require.Error(t, result.Err)
	})

	t.Run("stream policy deny with tracing covers span error path", func(t *testing.T) {
		telemetry.RegisterTracer()
		t.Cleanup(telemetry.UnregisterTracer)

		cfg := testConfigWithTenants("allowed-tenant")
		cfg.Audit.Sink = audit.NewMemorySink()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(actorextension.NewToolConfigExtension(), actorextension.NewConfigExtension(cfg)),
		)
		defer stop()

		ctx := context.Background()
		spawnFoundationalActorsForTest(ctx, system)

		registryPID, err := system.ActorOf(ctx, naming.ActorNameRegistrar)
		require.NoError(t, err)
		tool := validStdioTool("trace-stream-policy-tool")
		tool.AuthorizationPolicy = mcp.AuthorizationPolicyTenantAllowlist
		_, err = goaktactor.Ask(ctx, registryPID, &runtime.RegisterTool{Tool: tool}, askTimeout)
		require.NoError(t, err)
		waitForActors()

		routerPID, err := system.ActorOf(ctx, naming.RouterName(0))
		require.NoError(t, err)

		inv := sessionInvocation("trace-stream-policy-tool", "denied-tenant", "client-1")
		resp, err := goaktactor.Ask(ctx, routerPID, &runtime.RouteInvokeStream{Invocation: inv}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.RouteStreamResult)
		require.True(t, ok)
		require.Error(t, result.Err)
	})
}

func TestRouterReceiveUnhandledMessage(t *testing.T) {
	cfg := testConfig()
	cfg.Audit.Sink = audit.NewMemorySink()
	system, stop := testActorSystem(t, goaktactor.WithExtensions(
		actorextension.NewToolConfigExtension(),
		actorextension.NewConfigExtension(cfg),
	))
	defer stop()

	ctx := context.Background()
	spawnFoundationalActorsForTest(ctx, system)

	routerPID, err := system.ActorOf(ctx, naming.RouterName(0))
	require.NoError(t, err)

	// Send an unrecognized message type to trigger the default/Unhandled branch
	err = goaktactor.Tell(ctx, routerPID, &runtime.RegisterTool{Tool: validStdioTool("unhandled")})
	require.NoError(t, err)
	waitForActors()
	// Router should still be running after unhandled message
	require.True(t, routerPID.IsRunning())
}
