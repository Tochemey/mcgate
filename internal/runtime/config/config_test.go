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
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	goaktlog "github.com/tochemey/goakt/v4/log"

	"github.com/tochemey/mcgate/mcp"
)

func TestDefaultConstants(t *testing.T) {
	assert.Equal(t, 5*time.Minute, mcp.DefaultSessionIdleTimeout)
	assert.Equal(t, 30*time.Second, mcp.DefaultRequestTimeout)
	assert.Equal(t, 10*time.Second, mcp.DefaultStartupTimeout)
}

func TestConfigZeroValue(t *testing.T) {
	var cfg mcp.Config
	assert.Empty(t, cfg.Tools)
	assert.Empty(t, cfg.Tenants)
	assert.Zero(t, cfg.Runtime.RequestTimeout)
}

func TestRuntimeConfig(t *testing.T) {
	cfg := mcp.RuntimeConfig{
		SessionIdleTimeout: mcp.DefaultSessionIdleTimeout,
		RequestTimeout:     mcp.DefaultRequestTimeout,
		StartupTimeout:     mcp.DefaultStartupTimeout,
	}
	assert.Equal(t, 5*time.Minute, cfg.SessionIdleTimeout)
	assert.Equal(t, 30*time.Second, cfg.RequestTimeout)
	assert.Equal(t, 10*time.Second, cfg.StartupTimeout)
}

// testDiscoveryProvider is a minimal DiscoveryProvider for config tests.
type testDiscoveryProvider struct{}

func (t *testDiscoveryProvider) ID() string                                        { return "test" }
func (t *testDiscoveryProvider) Start(_ context.Context) error                     { return nil }
func (t *testDiscoveryProvider) DiscoverPeers(_ context.Context) ([]string, error) { return nil, nil }
func (t *testDiscoveryProvider) Stop(_ context.Context) error                      { return nil }

func TestClusterConfig(t *testing.T) {
	cfg := mcp.ClusterConfig{
		Enabled:           true,
		DiscoveryProvider: &testDiscoveryProvider{},
		RegistrarRole:     "control-plane",
		PeersPort:         15000,
		RemotingPort:      15001,
	}
	assert.True(t, cfg.Enabled)
	assert.NotNil(t, cfg.DiscoveryProvider)
	assert.Equal(t, "test", cfg.DiscoveryProvider.ID())
	assert.Equal(t, "control-plane", cfg.RegistrarRole)
	assert.Equal(t, 15000, cfg.PeersPort)
	assert.Equal(t, 15001, cfg.RemotingPort)
}

func TestTelemetryConfig(t *testing.T) {
	cfg := mcp.TelemetryConfig{OTLPEndpoint: "http://otel-collector:4318"}
	assert.Equal(t, "http://otel-collector:4318", cfg.OTLPEndpoint)
}

func TestTenantConfig(t *testing.T) {
	cfg := mcp.TenantConfig{
		ID: mcp.TenantID("acme-dev"),
		Quotas: mcp.TenantQuotaConfig{
			RequestsPerMinute:  1000,
			ConcurrentSessions: 200,
		},
	}
	assert.Equal(t, mcp.TenantID("acme-dev"), cfg.ID)
	assert.Equal(t, 1000, cfg.Quotas.RequestsPerMinute)
	assert.Equal(t, 200, cfg.Quotas.ConcurrentSessions)
}

func TestParseLogLevel(t *testing.T) {
	cases := []struct {
		input    string
		expected goaktlog.Level
	}{
		{"debug", goaktlog.DebugLevel},
		{"info", goaktlog.InfoLevel},
		{"warning", goaktlog.WarningLevel},
		{"warn", goaktlog.WarningLevel},
		{"error", goaktlog.ErrorLevel},
		{"fatal", goaktlog.FatalLevel},
		{"panic", goaktlog.PanicLevel},
		{"", goaktlog.InvalidLevel},
		{"unknown", goaktlog.InvalidLevel},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := ParseLogLevel(tc.input)
			assert.Equal(t, tc.expected, got)
		})
	}
}

func TestNewLogger(t *testing.T) {
	t.Run("empty level returns default logger", func(t *testing.T) {
		l := NewLogger("")
		require.NotNil(t, l)
	})

	t.Run("invalid level returns default logger", func(t *testing.T) {
		l := NewLogger("nonsense")
		require.NotNil(t, l)
	})

	t.Run("valid level returns slog logger", func(t *testing.T) {
		l := NewLogger("info")
		require.NotNil(t, l)
	})

	t.Run("debug level returns slog logger", func(t *testing.T) {
		l := NewLogger("debug")
		require.NotNil(t, l)
	})
}

func TestFullConfig(t *testing.T) {
	cfg := mcp.Config{
		Runtime: mcp.RuntimeConfig{
			SessionIdleTimeout: mcp.DefaultSessionIdleTimeout,
			RequestTimeout:     mcp.DefaultRequestTimeout,
			StartupTimeout:     mcp.DefaultStartupTimeout,
		},
		Cluster: mcp.ClusterConfig{
			Enabled:           true,
			DiscoveryProvider: &testDiscoveryProvider{},
			RegistrarRole:     "control-plane",
		},
		Telemetry: mcp.TelemetryConfig{OTLPEndpoint: "http://otel-collector:4318"},
		Tenants: []mcp.TenantConfig{
			{ID: "acme-dev", Quotas: mcp.TenantQuotaConfig{RequestsPerMinute: 1000, ConcurrentSessions: 200}},
		},
		Tools: []mcp.Tool{
			{ID: "filesystem", Transport: mcp.TransportStdio, Stdio: &mcp.StdioTransportConfig{Command: "npx"}, State: mcp.ToolStateEnabled},
		},
	}

	assert.True(t, cfg.Cluster.Enabled)
	require.Len(t, cfg.Tenants, 1)
	require.Len(t, cfg.Tools, 1)
	assert.Equal(t, mcp.ToolID("filesystem"), cfg.Tools[0].ID)
}
