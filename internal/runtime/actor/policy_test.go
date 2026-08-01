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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	goaktactor "github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/testkit"

	"github.com/tochemey/mcgate/mcp"

	"github.com/tochemey/mcgate/internal/naming"
	actorextension "github.com/tochemey/mcgate/internal/runtime/actor/extension"
	"github.com/tochemey/mcgate/internal/runtime/policy"
)

func TestPolicyActor(t *testing.T) {
	ctx := context.Background()

	t.Run("spawns and evaluates allow", func(t *testing.T) {
		cfg := testConfig()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(actorextension.NewConfigExtension(cfg)),
		)
		defer stop()

		pid, err := system.Spawn(ctx, naming.ActorNamePolicy, newPolicyMaker())
		require.NoError(t, err)
		waitForActors()

		in := &policy.Input{
			Invocation: sessionInvocation("tool-1", "default", "client-1"),
			Tool:       validStdioTool("tool-1"),
			TenantID:   "default",
			ClientID:   "client-1",
		}
		resp, err := goaktactor.Ask(ctx, pid, &policy.EvaluateRequest{Input: in}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*policy.EvaluateResult)
		require.True(t, ok)
		assert.True(t, result.Result.Allowed())
	})

	t.Run("denies when tenant not in allowlist", func(t *testing.T) {
		cfg := testConfigWithTenants("allowed-tenant")
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(actorextension.NewConfigExtension(cfg)),
		)
		defer stop()

		pid, err := system.Spawn(ctx, naming.ActorNamePolicy, newPolicyMaker())
		require.NoError(t, err)
		waitForActors()

		tool := validStdioTool("tool-1")
		tool.AuthorizationPolicy = mcp.AuthorizationPolicyTenantAllowlist
		in := &policy.Input{
			Invocation: sessionInvocation("tool-1", "denied-tenant", "client-1"),
			Tool:       tool,
			TenantID:   "denied-tenant",
			ClientID:   "client-1",
		}
		resp, err := goaktactor.Ask(ctx, pid, &policy.EvaluateRequest{Input: in}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*policy.EvaluateResult)
		require.True(t, ok)
		assert.False(t, result.Result.Allowed())
		assert.Equal(t, policy.DecisionDeny, result.Result.Decision)
		require.Error(t, result.Result.Err)
		var rErr *mcp.RuntimeError
		require.True(t, assert.ErrorAs(t, result.Result.Err, &rErr))
		assert.Equal(t, mcp.ErrCodePolicyDenied, rErr.Code)
	})

	t.Run("allows when tenant in allowlist", func(t *testing.T) {
		cfg := testConfigWithTenants("allowed-tenant")
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(actorextension.NewConfigExtension(cfg)),
		)
		defer stop()

		pid, err := system.Spawn(ctx, naming.ActorNamePolicy, newPolicyMaker())
		require.NoError(t, err)
		waitForActors()

		tool := validStdioTool("tool-1")
		tool.AuthorizationPolicy = mcp.AuthorizationPolicyTenantAllowlist
		in := &policy.Input{
			Invocation: sessionInvocation("tool-1", "allowed-tenant", "client-1"),
			Tool:       tool,
			TenantID:   "allowed-tenant",
			ClientID:   "client-1",
		}
		resp, err := goaktactor.Ask(ctx, pid, &policy.EvaluateRequest{Input: in}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*policy.EvaluateResult)
		require.True(t, ok)
		assert.True(t, result.Result.Allowed())
	})

	t.Run("throttles when RequestsPerMinute limit is reached", func(t *testing.T) {
		cfg := testConfig()
		cfg.Tenants = []mcp.TenantConfig{
			{ID: "rate-tenant", Quotas: mcp.TenantQuotaConfig{RequestsPerMinute: 2}},
		}
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(actorextension.NewConfigExtension(cfg)),
		)
		defer stop()

		pid, err := system.Spawn(ctx, naming.ActorNamePolicy, newPolicyMaker())
		require.NoError(t, err)
		waitForActors()

		in := func() *policy.Input {
			return &policy.Input{
				Invocation: sessionInvocation("tool-1", "rate-tenant", "client-1"),
				Tool:       validStdioTool("tool-1"),
				TenantID:   "rate-tenant",
				ClientID:   "client-1",
			}
		}

		// first two requests must be allowed
		for i := range 2 {
			resp, err := goaktactor.Ask(ctx, pid, &policy.EvaluateRequest{Input: in()}, askTimeout)
			require.NoError(t, err)
			result, ok := resp.(*policy.EvaluateResult)
			require.True(t, ok, "request %d", i)
			assert.True(t, result.Result.Allowed(), "request %d should be allowed", i)
		}

		// third request must be throttled
		resp, err := goaktactor.Ask(ctx, pid, &policy.EvaluateRequest{Input: in()}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*policy.EvaluateResult)
		require.True(t, ok)
		assert.False(t, result.Result.Allowed())
		assert.Equal(t, policy.DecisionThrottle, result.Result.Decision)
		var rErr *mcp.RuntimeError
		require.True(t, assert.ErrorAs(t, result.Result.Err, &rErr))
		assert.Equal(t, mcp.ErrCodeRateLimited, rErr.Code)
	})

	t.Run("denies nil input", func(t *testing.T) {
		cfg := testConfig()
		system, stop := testActorSystem(t,
			goaktactor.WithExtensions(actorextension.NewConfigExtension(cfg)),
		)
		defer stop()

		pid, err := system.Spawn(ctx, naming.ActorNamePolicy, newPolicyMaker())
		require.NoError(t, err)
		waitForActors()

		resp, err := goaktactor.Ask(ctx, pid, &policy.EvaluateRequest{Input: nil}, askTimeout)
		require.NoError(t, err)
		result, ok := resp.(*policy.EvaluateResult)
		require.True(t, ok)
		assert.False(t, result.Result.Allowed())
		assert.Equal(t, policy.DecisionDeny, result.Result.Decision)
	})

	t.Run("unhandles unknown message", func(t *testing.T) {
		cfg := testConfig()
		kit, ctx := newTestKit(t, testkit.WithExtensions(actorextension.NewConfigExtension(cfg)))

		pid, err := kit.ActorSystem().Spawn(ctx, "policy-unknown", newPolicyMaker())
		require.NoError(t, err)
		require.NoError(t, pid.Tell(ctx, pid, "unknown"))
		waitForActors()
	})
}
