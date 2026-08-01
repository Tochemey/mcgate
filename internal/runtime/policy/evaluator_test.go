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

package policy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tochemey/mcgate/mcp"
)

func TestEvaluator(t *testing.T) {
	t.Run("allow when no authorization policy", func(t *testing.T) {
		e := NewEvaluator(mcp.Config{})
		tool := mcp.Tool{
			ID:                  "tool-1",
			Transport:           mcp.TransportStdio,
			Stdio:               &mcp.StdioTransportConfig{Command: "npx"},
			AuthorizationPolicy: "", // no policy
		}
		in := &Input{
			Invocation: &mcp.Invocation{ToolID: "tool-1"},
			Tool:       tool,
			TenantID:   "tenant-1",
			ClientID:   "client-1",
		}
		result := e.Evaluate(in)
		assert.True(t, result.Allowed())
		assert.Equal(t, DecisionAllow, result.Decision)
	})

	t.Run("allow when tenant_allowlist and no tenants configured", func(t *testing.T) {
		e := NewEvaluator(mcp.Config{})
		tool := mcp.Tool{
			ID:                  "tool-1",
			Transport:           mcp.TransportStdio,
			Stdio:               &mcp.StdioTransportConfig{Command: "npx"},
			AuthorizationPolicy: mcp.AuthorizationPolicyTenantAllowlist,
		}
		in := &Input{
			Invocation: &mcp.Invocation{ToolID: "tool-1"},
			Tool:       tool,
			TenantID:   "any-tenant",
			ClientID:   "client-1",
		}
		result := e.Evaluate(in)
		assert.True(t, result.Allowed())
	})

	t.Run("allow when tenant in allowlist", func(t *testing.T) {
		cfg := mcp.Config{
			Tenants: []mcp.TenantConfig{
				{ID: "allowed-tenant", Quotas: mcp.TenantQuotaConfig{}},
			},
		}
		e := NewEvaluator(cfg)
		tool := mcp.Tool{
			ID:                  "tool-1",
			Transport:           mcp.TransportStdio,
			Stdio:               &mcp.StdioTransportConfig{Command: "npx"},
			AuthorizationPolicy: mcp.AuthorizationPolicyTenantAllowlist,
		}
		in := &Input{
			Invocation: &mcp.Invocation{ToolID: "tool-1"},
			Tool:       tool,
			TenantID:   "allowed-tenant",
			ClientID:   "client-1",
		}
		result := e.Evaluate(in)
		assert.True(t, result.Allowed())
	})

	t.Run("deny when tenant not in allowlist", func(t *testing.T) {
		cfg := mcp.Config{
			Tenants: []mcp.TenantConfig{
				{ID: "allowed-tenant", Quotas: mcp.TenantQuotaConfig{}},
			},
		}
		e := NewEvaluator(cfg)
		tool := mcp.Tool{
			ID:                  "tool-1",
			Transport:           mcp.TransportStdio,
			Stdio:               &mcp.StdioTransportConfig{Command: "npx"},
			AuthorizationPolicy: mcp.AuthorizationPolicyTenantAllowlist,
		}
		in := &Input{
			Invocation: &mcp.Invocation{ToolID: "tool-1"},
			Tool:       tool,
			TenantID:   "denied-tenant",
			ClientID:   "client-1",
		}
		result := e.Evaluate(in)
		assert.False(t, result.Allowed())
		assert.Equal(t, DecisionDeny, result.Decision)
		require.Error(t, result.Err)
		var rErr *mcp.RuntimeError
		require.True(t, assert.ErrorAs(t, result.Err, &rErr))
		assert.Equal(t, mcp.ErrCodePolicyDenied, rErr.Code)
	})

	t.Run("throttle when concurrent session limit reached", func(t *testing.T) {
		cfg := mcp.Config{
			Tenants: []mcp.TenantConfig{
				{
					ID: "tenant-1",
					Quotas: mcp.TenantQuotaConfig{
						ConcurrentSessions: 2,
					},
				},
			},
		}
		e := NewEvaluator(cfg)
		tool := mcp.Tool{
			ID:        "tool-1",
			Transport: mcp.TransportStdio,
			Stdio:     &mcp.StdioTransportConfig{Command: "npx"},
		}
		in := &Input{
			Invocation:              &mcp.Invocation{ToolID: "tool-1"},
			Tool:                    tool,
			TenantID:                "tenant-1",
			ClientID:                "client-1",
			TenantConfig:            &cfg.Tenants[0],
			ActiveSessionCount:      2,
			RequestsInCurrentMinute: 0,
		}
		result := e.Evaluate(in)
		assert.False(t, result.Allowed())
		assert.Equal(t, DecisionThrottle, result.Decision)
		require.Error(t, result.Err)
		var rErr *mcp.RuntimeError
		require.True(t, assert.ErrorAs(t, result.Err, &rErr))
		assert.Equal(t, mcp.ErrCodeConcurrencyLimitReached, rErr.Code)
	})

	t.Run("throttle when rate limit exceeded", func(t *testing.T) {
		cfg := mcp.Config{
			Tenants: []mcp.TenantConfig{
				{
					ID: "tenant-1",
					Quotas: mcp.TenantQuotaConfig{
						RequestsPerMinute: 10,
					},
				},
			},
		}
		e := NewEvaluator(cfg)
		tool := mcp.Tool{
			ID:        "tool-1",
			Transport: mcp.TransportStdio,
			Stdio:     &mcp.StdioTransportConfig{Command: "npx"},
		}
		in := &Input{
			Invocation:              &mcp.Invocation{ToolID: "tool-1"},
			Tool:                    tool,
			TenantID:                "tenant-1",
			ClientID:                "client-1",
			TenantConfig:            &cfg.Tenants[0],
			ActiveSessionCount:      0,
			RequestsInCurrentMinute: 10,
		}
		result := e.Evaluate(in)
		assert.False(t, result.Allowed())
		assert.Equal(t, DecisionThrottle, result.Decision)
		require.Error(t, result.Err)
		var rErr *mcp.RuntimeError
		require.True(t, assert.ErrorAs(t, result.Err, &rErr))
		assert.Equal(t, mcp.ErrCodeRateLimited, rErr.Code)
	})

	t.Run("deny when input is nil", func(t *testing.T) {
		e := NewEvaluator(mcp.Config{})
		result := e.Evaluate(nil)
		assert.False(t, result.Allowed())
		assert.Equal(t, DecisionDeny, result.Decision)
	})
}
