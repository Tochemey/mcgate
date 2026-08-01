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
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	goaktactor "github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/testkit"
	noopmetric "go.opentelemetry.io/otel/metric/noop"

	"github.com/tochemey/mcgate/mcp"

	"github.com/tochemey/mcgate/internal/naming"
	"github.com/tochemey/mcgate/internal/runtime"
	"github.com/tochemey/mcgate/internal/runtime/actor/extension"
	"github.com/tochemey/mcgate/internal/runtime/telemetry"
)

func TestCredentialBrokerActor(t *testing.T) {
	t.Run("resolves credentials from env provider", func(t *testing.T) {
		ctx := t.Context()
		t.Setenv("MCP_CRED_CRED_TOOL_API_KEY", "test-secret")
		config := testConfig()
		system, stop := testActorSystem(t, goaktactor.WithExtensions(extension.NewConfigExtension(config)))
		defer stop()

		pid, err := system.Spawn(ctx, naming.ActorNameCredentialBroker, newCredentialBroker())
		require.NoError(t, err)
		waitForActors()

		resp, err := goaktactor.Ask(ctx, pid, &runtime.ResolveRequest{
			TenantID: "default",
			ToolID:   "cred-tool",
		}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.ResolveResult)
		require.True(t, ok)
		require.True(t, result.Resolved())
		assert.Equal(t, "test-secret", result.Credentials.Values["api-key"])
	})

	t.Run("returns ErrCredentialUnavailable when no credentials", func(t *testing.T) {
		ctx := t.Context()
		cfg := testConfig()
		cfg.Credentials.Providers = []mcp.CredentialsProvider{&mockCredentialProvider{creds: nil}}

		system, stop := testActorSystem(t, goaktactor.WithExtensions(extension.NewConfigExtension(cfg)))
		defer stop()

		pid, err := system.Spawn(ctx, naming.ActorNameCredentialBroker, newCredentialBroker())
		require.NoError(t, err)
		waitForActors()

		resp, err := goaktactor.Ask(ctx, pid, &runtime.ResolveRequest{
			TenantID: "default",
			ToolID:   "no-creds-tool",
		}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*runtime.ResolveResult)
		require.True(t, ok)
		assert.False(t, result.Resolved())
		require.Error(t, result.Err)
		var rErr *mcp.RuntimeError
		require.True(t, assert.ErrorAs(t, result.Err, &rErr))
		assert.Equal(t, mcp.ErrCodeCredentialUnavailable, rErr.Code)
	})

	t.Run("resolves from provider and caches", func(t *testing.T) {
		cfg := testConfig()
		cfg.Credentials.Providers = []mcp.CredentialsProvider{&mockCredentialProvider{creds: map[string]string{"api_key": "secret"}}}
		cfg.Credentials.CacheTTL = time.Minute
		kit, ctx := newTestKit(t, testkit.WithExtensions(extension.NewConfigExtension(cfg)))

		pid, err := kit.ActorSystem().Spawn(ctx, "broker-cache", newCredentialBroker())
		require.NoError(t, err)
		require.NotNil(t, pid)
		waitForActors()

		probe := kit.NewProbe(ctx)
		probe.SendSync("broker-cache", &runtime.ResolveRequest{
			TenantID: "tenant-1",
			ToolID:   "tool-1",
		}, askTimeout)
		resp := probe.ExpectAnyMessage()
		result, ok := resp.(*runtime.ResolveResult)
		require.True(t, ok)
		require.NoError(t, result.Err)
		assert.Equal(t, "secret", result.Credentials.Values["api_key"])

		probe.SendSync("broker-cache", &runtime.ResolveRequest{
			TenantID: "tenant-1",
			ToolID:   "tool-1",
		}, askTimeout)
		resp2 := probe.ExpectAnyMessage()
		result2, ok := resp2.(*runtime.ResolveResult)
		require.True(t, ok)
		require.NoError(t, result2.Err)
		assert.Equal(t, "secret", result2.Credentials.Values["api_key"])
		probe.Stop()
	})

	t.Run("returns ErrCredentialUnavailable when no provider has credentials", func(t *testing.T) {
		cfg := testConfig()
		cfg.Credentials.Providers = []mcp.CredentialsProvider{&mockCredentialProvider{creds: nil}}
		cfg.Credentials.CacheTTL = time.Minute
		kit, ctx := newTestKit(t, testkit.WithExtensions(extension.NewConfigExtension(cfg)))

		kit.ActorSystem().Spawn(ctx, "broker-empty", newCredentialBroker())
		waitForActors()

		probe := kit.NewProbe(ctx)
		probe.SendSync("broker-empty", &runtime.ResolveRequest{
			TenantID: "tenant-1",
			ToolID:   "tool-1",
		}, askTimeout)
		resp := probe.ExpectAnyMessage()
		result, ok := resp.(*runtime.ResolveResult)
		require.True(t, ok)
		require.Error(t, result.Err)
		assert.Equal(t, mcp.ErrCodeCredentialUnavailable, result.Err.(*mcp.RuntimeError).Code)
		probe.Stop()
	})

	t.Run("uses default cache TTL when zero", func(t *testing.T) {
		cfg := testConfig()
		cfg.Credentials.Providers = []mcp.CredentialsProvider{&mockCredentialProvider{creds: map[string]string{"k": "v"}}}
		cfg.Credentials.CacheTTL = 0
		kit, ctx := newTestKit(t, testkit.WithExtensions(extension.NewConfigExtension(cfg)))

		kit.ActorSystem().Spawn(ctx, "broker-default-ttl", newCredentialBroker())
		waitForActors()

		probe := kit.NewProbe(ctx)
		probe.SendSync("broker-default-ttl", &runtime.ResolveRequest{
			TenantID: "t1",
			ToolID:   "tool1",
		}, askTimeout)
		resp := probe.ExpectAnyMessage()
		result, ok := resp.(*runtime.ResolveResult)
		require.True(t, ok)
		require.NoError(t, result.Err)
		assert.Equal(t, "v", result.Credentials.Values["k"])
		probe.Stop()
	})

	t.Run("skips provider that returns error and uses next provider", func(t *testing.T) {
		cfg := testConfig()
		cfg.Credentials.Providers = []mcp.CredentialsProvider{
			&mockCredentialProvider{err: errors.New("provider failed")},
			&mockCredentialProvider{creds: map[string]string{"key": "val"}},
		}
		cfg.Credentials.CacheTTL = time.Minute
		kit, ctx := newTestKit(t, testkit.WithExtensions(extension.NewConfigExtension(cfg)))

		kit.ActorSystem().Spawn(ctx, "broker-fail-then-ok", newCredentialBroker())
		waitForActors()

		probe := kit.NewProbe(ctx)
		probe.SendSync("broker-fail-then-ok", &runtime.ResolveRequest{
			TenantID: "tenant-1",
			ToolID:   "tool-1",
		}, askTimeout)
		resp := probe.ExpectAnyMessage()
		result, ok := resp.(*runtime.ResolveResult)
		require.True(t, ok)
		require.NoError(t, result.Err)
		assert.Equal(t, "val", result.Credentials.Values["key"])
		probe.Stop()
	})

	t.Run("evicts expired entries when cache is full", func(t *testing.T) {
		cfg := testConfig()
		cfg.Credentials.Providers = []mcp.CredentialsProvider{&mockCredentialProvider{creds: map[string]string{"k": "v"}}}
		cfg.Credentials.CacheTTL = 50 * time.Millisecond
		cfg.Credentials.MaxCacheEntries = 2
		kit, ctx := newTestKit(t, testkit.WithExtensions(extension.NewConfigExtension(cfg)))

		pid, err := kit.ActorSystem().Spawn(ctx, "broker-evict-expired", newCredentialBroker())
		require.NoError(t, err)
		require.NotNil(t, pid)
		waitForActors()

		probe := kit.NewProbe(ctx)

		// Fill cache with 2 entries
		probe.SendSync("broker-evict-expired", &runtime.ResolveRequest{TenantID: "t1", ToolID: "tool1"}, askTimeout)
		probe.ExpectAnyMessage()
		probe.SendSync("broker-evict-expired", &runtime.ResolveRequest{TenantID: "t1", ToolID: "tool2"}, askTimeout)
		probe.ExpectAnyMessage()

		// Wait for entries to expire
		time.Sleep(100 * time.Millisecond)

		// Third request should succeed — expired entries are evicted to make room
		probe.SendSync("broker-evict-expired", &runtime.ResolveRequest{TenantID: "t1", ToolID: "tool3"}, askTimeout)
		resp := probe.ExpectAnyMessage()
		result, ok := resp.(*runtime.ResolveResult)
		require.True(t, ok)
		require.NoError(t, result.Err)
		assert.Equal(t, "v", result.Credentials.Values["k"])
		probe.Stop()
	})

	t.Run("evicts LRU entry when cache is full and no entries expired", func(t *testing.T) {
		cfg := testConfig()
		cfg.Credentials.Providers = []mcp.CredentialsProvider{&mockCredentialProvider{creds: map[string]string{"k": "v"}}}
		cfg.Credentials.CacheTTL = time.Hour
		cfg.Credentials.MaxCacheEntries = 2
		kit, ctx := newTestKit(t, testkit.WithExtensions(extension.NewConfigExtension(cfg)))

		pid, err := kit.ActorSystem().Spawn(ctx, "broker-evict-lru", newCredentialBroker())
		require.NoError(t, err)
		require.NotNil(t, pid)
		waitForActors()

		probe := kit.NewProbe(ctx)

		// Fill cache with 2 entries
		probe.SendSync("broker-evict-lru", &runtime.ResolveRequest{TenantID: "t1", ToolID: "tool1"}, askTimeout)
		probe.ExpectAnyMessage()

		time.Sleep(10 * time.Millisecond)

		probe.SendSync("broker-evict-lru", &runtime.ResolveRequest{TenantID: "t1", ToolID: "tool2"}, askTimeout)
		probe.ExpectAnyMessage()

		// Access tool1 again to make it more recent
		probe.SendSync("broker-evict-lru", &runtime.ResolveRequest{TenantID: "t1", ToolID: "tool1"}, askTimeout)
		probe.ExpectAnyMessage()

		// Third entry should evict tool2 (least recently accessed)
		probe.SendSync("broker-evict-lru", &runtime.ResolveRequest{TenantID: "t1", ToolID: "tool3"}, askTimeout)
		resp := probe.ExpectAnyMessage()
		result, ok := resp.(*runtime.ResolveResult)
		require.True(t, ok)
		require.NoError(t, result.Err)
		assert.Equal(t, "v", result.Credentials.Values["k"])
		probe.Stop()
	})

	t.Run("cache updates lastAccess on hit", func(t *testing.T) {
		cfg := testConfig()
		cfg.Credentials.Providers = []mcp.CredentialsProvider{&mockCredentialProvider{creds: map[string]string{"k": "v"}}}
		cfg.Credentials.CacheTTL = time.Hour
		kit, ctx := newTestKit(t, testkit.WithExtensions(extension.NewConfigExtension(cfg)))

		pid, err := kit.ActorSystem().Spawn(ctx, "broker-lastaccess", newCredentialBroker())
		require.NoError(t, err)
		require.NotNil(t, pid)
		waitForActors()

		probe := kit.NewProbe(ctx)

		// First call caches
		probe.SendSync("broker-lastaccess", &runtime.ResolveRequest{TenantID: "t1", ToolID: "tool1"}, askTimeout)
		resp1 := probe.ExpectAnyMessage()
		result1, ok := resp1.(*runtime.ResolveResult)
		require.True(t, ok)
		require.NoError(t, result1.Err)

		time.Sleep(10 * time.Millisecond)

		// Second call should hit cache and return same result
		probe.SendSync("broker-lastaccess", &runtime.ResolveRequest{TenantID: "t1", ToolID: "tool1"}, askTimeout)
		resp2 := probe.ExpectAnyMessage()
		result2, ok := resp2.(*runtime.ResolveResult)
		require.True(t, ok)
		require.NoError(t, result2.Err)
		assert.Equal(t, "v", result2.Credentials.Values["k"])
		probe.Stop()
	})

	t.Run("unhandles unknown message", func(t *testing.T) {
		cfg := testConfig()
		cfg.Credentials.Providers = []mcp.CredentialsProvider{&mockCredentialProvider{creds: map[string]string{"k": "v"}}}
		cfg.Credentials.CacheTTL = time.Minute
		kit, ctx := newTestKit(t, testkit.WithExtensions(extension.NewConfigExtension(cfg)))

		pid, err := kit.ActorSystem().Spawn(ctx, "broker-unknown", newCredentialBroker())
		require.NoError(t, err)
		require.NoError(t, pid.Tell(ctx, pid, "unknown"))
		waitForActors()
	})

	t.Run("records credential cache hit and miss metrics when registered", func(t *testing.T) {
		meter := noopmetric.NewMeterProvider().Meter("test")
		_, err := telemetry.RegisterMetrics(meter)
		require.NoError(t, err)
		t.Cleanup(telemetry.UnregisterMetrics)

		cfg := testConfig()
		cfg.Credentials.Providers = []mcp.CredentialsProvider{&mockCredentialProvider{creds: map[string]string{"k": "v"}}}
		cfg.Credentials.CacheTTL = time.Minute
		kit, ctx := newTestKit(t, testkit.WithExtensions(extension.NewConfigExtension(cfg)))

		_, err = kit.ActorSystem().Spawn(ctx, "broker-metrics", newCredentialBroker())
		require.NoError(t, err)
		waitForActors()

		probe := kit.NewProbe(ctx)

		// First call: cache miss → records miss metric
		probe.SendSync("broker-metrics", &runtime.ResolveRequest{TenantID: "t1", ToolID: "tool1"}, askTimeout)
		resp := probe.ExpectAnyMessage()
		result, ok := resp.(*runtime.ResolveResult)
		require.True(t, ok)
		require.NoError(t, result.Err)

		// Second call: cache hit → records hit metric
		probe.SendSync("broker-metrics", &runtime.ResolveRequest{TenantID: "t1", ToolID: "tool1"}, askTimeout)
		resp2 := probe.ExpectAnyMessage()
		result2, ok := resp2.(*runtime.ResolveResult)
		require.True(t, ok)
		require.NoError(t, result2.Err)
		assert.Equal(t, "v", result2.Credentials.Values["k"])
		probe.Stop()
	})
}
