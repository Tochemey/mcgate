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

package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/tochemey/portcullis/mcp"
)

func TestApplyDefaults(t *testing.T) {
	t.Run("fills zero values", func(t *testing.T) {
		cfg := mcp.Config{}
		ApplyDefaults(&cfg)
		assert.Equal(t, mcp.DefaultSessionIdleTimeout, cfg.Runtime.SessionIdleTimeout)
		assert.Equal(t, mcp.DefaultRequestTimeout, cfg.Runtime.RequestTimeout)
		assert.Equal(t, mcp.DefaultStartupTimeout, cfg.Runtime.StartupTimeout)
		assert.Equal(t, mcp.DefaultHealthProbeInterval, cfg.Runtime.HealthProbeInterval)
		assert.Equal(t, mcp.DefaultShutdownTimeout, cfg.Runtime.ShutdownTimeout)
		assert.Equal(t, mcp.DefaultRouterPoolSize, cfg.Runtime.RouterPoolSize)
		assert.NotNil(t, cfg.Tools)
		assert.NotNil(t, cfg.Tenants)
	})

	t.Run("HealthProbe.Interval falls back to Runtime.HealthProbeInterval", func(t *testing.T) {
		cfg := mcp.Config{}
		ApplyDefaults(&cfg)
		assert.Equal(t, mcp.DefaultHealthProbeInterval, cfg.HealthProbe.Interval)

		cfg = mcp.Config{
			Runtime: mcp.RuntimeConfig{HealthProbeInterval: 2 * time.Minute},
		}
		ApplyDefaults(&cfg)
		assert.Equal(t, 2*time.Minute, cfg.HealthProbe.Interval)
	})

	t.Run("preserves explicit HealthProbe.Interval", func(t *testing.T) {
		cfg := mcp.Config{
			Runtime:     mcp.RuntimeConfig{HealthProbeInterval: 2 * time.Minute},
			HealthProbe: mcp.HealthProbeConfig{Interval: 7 * time.Second},
		}
		ApplyDefaults(&cfg)
		assert.Equal(t, 7*time.Second, cfg.HealthProbe.Interval)
	})

	t.Run("fills HealthProbe.Timeout and Credentials.MaxCacheEntries defaults", func(t *testing.T) {
		cfg := mcp.Config{}
		ApplyDefaults(&cfg)
		assert.Equal(t, mcp.DefaultHealthProbeTimeout, cfg.HealthProbe.Timeout)
		assert.Equal(t, mcp.DefaultMaxCacheEntries, cfg.Credentials.MaxCacheEntries)
	})

	t.Run("preserves explicit HealthProbe.Timeout and MaxCacheEntries", func(t *testing.T) {
		cfg := mcp.Config{
			HealthProbe: mcp.HealthProbeConfig{Timeout: 5 * time.Second},
			Credentials: mcp.CredentialsConfig{MaxCacheEntries: 500},
		}
		ApplyDefaults(&cfg)
		assert.Equal(t, 5*time.Second, cfg.HealthProbe.Timeout)
		assert.Equal(t, 500, cfg.Credentials.MaxCacheEntries)
	})

	t.Run("fills Audit.MailboxSize default", func(t *testing.T) {
		cfg := mcp.Config{}
		ApplyDefaults(&cfg)
		assert.Equal(t, mcp.DefaultAuditMailboxSize, cfg.Audit.MailboxSize)
	})

	t.Run("preserves explicit Audit.MailboxSize", func(t *testing.T) {
		cfg := mcp.Config{Audit: mcp.AuditConfig{MailboxSize: 256}}
		ApplyDefaults(&cfg)
		assert.Equal(t, 256, cfg.Audit.MailboxSize)
	})

	t.Run("negative values are treated as zero and replaced with defaults", func(t *testing.T) {
		cfg := mcp.Config{
			Runtime: mcp.RuntimeConfig{
				SessionIdleTimeout:  -1 * time.Minute,
				RequestTimeout:      -1 * time.Second,
				StartupTimeout:      -1 * time.Second,
				HealthProbeInterval: -1 * time.Minute,
				ShutdownTimeout:     -1 * time.Second,
			},
			HealthProbe: mcp.HealthProbeConfig{Interval: -10 * time.Second, Timeout: -5 * time.Second},
			Credentials: mcp.CredentialsConfig{MaxCacheEntries: -100},
			Audit:       mcp.AuditConfig{MailboxSize: -512},
		}
		ApplyDefaults(&cfg)
		assert.Equal(t, mcp.DefaultSessionIdleTimeout, cfg.Runtime.SessionIdleTimeout)
		assert.Equal(t, mcp.DefaultRequestTimeout, cfg.Runtime.RequestTimeout)
		assert.Equal(t, mcp.DefaultStartupTimeout, cfg.Runtime.StartupTimeout)
		assert.Equal(t, mcp.DefaultHealthProbeInterval, cfg.Runtime.HealthProbeInterval)
		assert.Equal(t, mcp.DefaultShutdownTimeout, cfg.Runtime.ShutdownTimeout)
		assert.Equal(t, mcp.DefaultRouterPoolSize, cfg.Runtime.RouterPoolSize)
		assert.Equal(t, mcp.DefaultHealthProbeInterval, cfg.HealthProbe.Interval)
		assert.Equal(t, mcp.DefaultHealthProbeTimeout, cfg.HealthProbe.Timeout)
		assert.Equal(t, mcp.DefaultMaxCacheEntries, cfg.Credentials.MaxCacheEntries)
		assert.Equal(t, mcp.DefaultAuditMailboxSize, cfg.Audit.MailboxSize)
	})

	t.Run("preserves explicit values", func(t *testing.T) {
		cfg := mcp.Config{
			Runtime: mcp.RuntimeConfig{
				SessionIdleTimeout:  10 * time.Minute,
				RequestTimeout:      60 * time.Second,
				StartupTimeout:      20 * time.Second,
				HealthProbeInterval: 1 * time.Minute,
				ShutdownTimeout:     45 * time.Second,
			},
			Tools: []mcp.Tool{
				{ID: "t", Transport: mcp.TransportStdio, Stdio: &mcp.StdioTransportConfig{Command: "npx"}, State: mcp.ToolStateEnabled},
			},
			Tenants: []mcp.TenantConfig{{ID: "x"}},
		}
		ApplyDefaults(&cfg)
		assert.Equal(t, 10*time.Minute, cfg.Runtime.SessionIdleTimeout)
		assert.Equal(t, 60*time.Second, cfg.Runtime.RequestTimeout)
		assert.Equal(t, 20*time.Second, cfg.Runtime.StartupTimeout)
		assert.Equal(t, 1*time.Minute, cfg.Runtime.HealthProbeInterval)
		assert.Equal(t, 45*time.Second, cfg.Runtime.ShutdownTimeout)
		assert.Len(t, cfg.Tools, 1)
		assert.Len(t, cfg.Tenants, 1)
	})
}
