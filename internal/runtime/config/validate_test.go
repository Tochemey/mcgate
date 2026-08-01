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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tochemey/mcgate/mcp"
)

func TestValidate(t *testing.T) {
	validStdioTool := mcp.Tool{
		ID:        "fs",
		Transport: mcp.TransportStdio,
		Stdio:     &mcp.StdioTransportConfig{Command: "echo"},
		State:     mcp.ToolStateEnabled,
	}
	validHTTPTool := mcp.Tool{
		ID:        "remote",
		Transport: mcp.TransportHTTP,
		HTTP:      &mcp.HTTPTransportConfig{URL: "https://example.com"},
		State:     mcp.ToolStateEnabled,
	}

	tests := []struct {
		name    string
		cfg     mcp.Config
		wantErr string
	}{
		{
			name: "valid minimal config",
			cfg: mcp.Config{
				Tools:   []mcp.Tool{validStdioTool},
				Tenants: []mcp.TenantConfig{},
			},
		},
		{
			name: "valid config with tenants and tools",
			cfg: mcp.Config{
				Tenants: []mcp.TenantConfig{{ID: "t1"}},
				Tools:   []mcp.Tool{validStdioTool, validHTTPTool},
			},
		},
		{
			name: "cluster enabled without discovery",
			cfg: mcp.Config{
				Cluster: mcp.ClusterConfig{Enabled: true},
			},
			wantErr: "cluster.discovery_provider is required",
		},
		{
			name: "tenant with empty id",
			cfg: mcp.Config{
				Tenants: []mcp.TenantConfig{{ID: ""}},
			},
			wantErr: "tenants[0]: id is required",
		},
		{
			name: "duplicate tenant ids",
			cfg: mcp.Config{
				Tenants: []mcp.TenantConfig{{ID: "dup"}, {ID: "dup"}},
			},
			wantErr: "duplicate tenant id",
		},
		{
			name: "tool with empty id",
			cfg: mcp.Config{
				Tools: []mcp.Tool{{Transport: mcp.TransportStdio, Stdio: &mcp.StdioTransportConfig{Command: "echo"}}},
			},
			wantErr: "tools[0]: id is required",
		},
		{
			name: "duplicate tool ids",
			cfg: mcp.Config{
				Tools: []mcp.Tool{
					{ID: "dup", Transport: mcp.TransportStdio, Stdio: &mcp.StdioTransportConfig{Command: "echo"}, State: mcp.ToolStateEnabled},
					{ID: "dup", Transport: mcp.TransportStdio, Stdio: &mcp.StdioTransportConfig{Command: "cat"}, State: mcp.ToolStateEnabled},
				},
			},
			wantErr: "duplicate tool id",
		},
		{
			name: "stdio tool missing command",
			cfg: mcp.Config{
				Tools: []mcp.Tool{{ID: "bad", Transport: mcp.TransportStdio}},
			},
			wantErr: "stdio tool must have non-empty command",
		},
		{
			name: "http tool missing url",
			cfg: mcp.Config{
				Tools: []mcp.Tool{{ID: "bad", Transport: mcp.TransportHTTP}},
			},
			wantErr: "http tool must have non-empty URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(&tt.cfg)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
