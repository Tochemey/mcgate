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

package cluster

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tochemey/mcgate/mcp"
)

// staticDiscovery is a test implementation of mcp.DiscoveryProvider.
type staticDiscovery struct {
	id    string
	peers []string
}

var _ mcp.DiscoveryProvider = (*staticDiscovery)(nil)

func (s *staticDiscovery) ID() string                                        { return s.id }
func (s *staticDiscovery) Start(_ context.Context) error                     { return nil }
func (s *staticDiscovery) DiscoverPeers(_ context.Context) ([]string, error) { return s.peers, nil }
func (s *staticDiscovery) Stop(_ context.Context) error                      { return nil }

func TestIsClusterConfigured(t *testing.T) {
	t.Run("returns false when Cluster.Enabled is false", func(t *testing.T) {
		cfg := mcp.Config{}
		cfg.Cluster.Enabled = false
		cfg.Cluster.DiscoveryProvider = &staticDiscovery{id: "static"}
		assert.False(t, IsClusterConfigured(cfg))
	})

	t.Run("returns false when DiscoveryProvider is nil", func(t *testing.T) {
		cfg := mcp.Config{}
		cfg.Cluster.Enabled = true
		assert.False(t, IsClusterConfigured(cfg))
	})

	t.Run("returns true when enabled with DiscoveryProvider set", func(t *testing.T) {
		cfg := mcp.Config{}
		cfg.Cluster.Enabled = true
		cfg.Cluster.DiscoveryProvider = &staticDiscovery{id: "static"}
		assert.True(t, IsClusterConfigured(cfg))
	})
}

func TestBuildOptions(t *testing.T) {
	t.Run("returns nil when Cluster.Enabled is false", func(t *testing.T) {
		cfg := mcp.Config{}
		cfg.Cluster.Enabled = false
		opts := BuildOptions(cfg, nil)
		assert.Nil(t, opts)
	})

	t.Run("returns only WithRemote when enabled but DiscoveryProvider is nil", func(t *testing.T) {
		cfg := mcp.Config{}
		cfg.Cluster.Enabled = true
		opts := BuildOptions(cfg, nil)
		require.NotNil(t, opts)
		assert.Len(t, opts, 1, "expect only WithRemote")
	})

	t.Run("returns WithRemote and WithCluster when DiscoveryProvider is set", func(t *testing.T) {
		cfg := mcp.Config{}
		cfg.Cluster.Enabled = true
		cfg.Cluster.DiscoveryProvider = &staticDiscovery{id: "test", peers: []string{"peer1:8080"}}
		opts := BuildOptions(cfg, nil)
		require.NotNil(t, opts)
		assert.GreaterOrEqual(t, len(opts), 2, "expect WithRemote and WithCluster")
	})

	t.Run("applies default ports when not specified", func(t *testing.T) {
		cfg := mcp.Config{}
		cfg.Cluster.Enabled = true
		cfg.Cluster.DiscoveryProvider = &staticDiscovery{id: "test"}
		opts := BuildOptions(cfg, nil)
		require.NotNil(t, opts)
		assert.GreaterOrEqual(t, len(opts), 2)
	})

	t.Run("uses custom ports when specified", func(t *testing.T) {
		cfg := mcp.Config{}
		cfg.Cluster.Enabled = true
		cfg.Cluster.DiscoveryProvider = &staticDiscovery{id: "test"}
		cfg.Cluster.PeersPort = 20000
		cfg.Cluster.RemotingPort = 20001
		opts := BuildOptions(cfg, nil)
		require.NotNil(t, opts)
		assert.GreaterOrEqual(t, len(opts), 2)
	})
}
