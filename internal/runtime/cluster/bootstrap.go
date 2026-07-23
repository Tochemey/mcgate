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

// Package cluster provides cluster bootstrap and configuration for multi-node
// runtime deployment. When Cluster.Enabled is true, the runtime uses GoAkt
// clustering with the user-supplied DiscoveryProvider.
package cluster

import (
	"time"

	goaktactor "github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/remote"

	"github.com/tochemey/portcullis/internal/discovery"
	"github.com/tochemey/portcullis/mcp"
)

const (
	defaultDiscoveryPort = 15000
	defaultPeersPort     = 15000
	defaultRemotingPort  = 15001
)

// IsClusterConfigured returns true when cluster mode is enabled and a
// DiscoveryProvider is set. Use this to decide whether to use SpawnSingleton
// (cluster) vs Spawn (single-node) for actors like the registry.
func IsClusterConfigured(config mcp.Config) bool {
	return config.Cluster.Enabled && !discovery.IsNilDiscoveryProvider(config.Cluster.DiscoveryProvider)
}

// BuildOptions returns actor system options for cluster mode when enabled.
//
// When cfg.Cluster.Enabled is false, returns nil (single-node, no cluster
// options). When cluster is enabled but no DiscoveryProvider is set, returns
// a slice containing only WithRemote (remoting without discovery).
// remoteOpts are forwarded to [remote.NewConfig] (e.g., serializer
// registrations via [remote.WithSerializables]). kinds are the actor instances
// to register for cluster (e.g., RegistryActor).
func BuildOptions(config mcp.Config, remoteOpts []remote.Option, kinds ...goaktactor.Actor) []goaktactor.Option {
	if !config.Cluster.Enabled {
		return nil
	}

	clConfig := config.Cluster

	discoveryPort := clConfig.DiscoveryPort
	if discoveryPort <= 0 {
		discoveryPort = defaultDiscoveryPort
	}

	peersPort := clConfig.PeersPort
	if peersPort <= 0 {
		peersPort = defaultPeersPort
	}

	remotingPort := clConfig.RemotingPort
	if remotingPort <= 0 {
		remotingPort = defaultRemotingPort
	}

	var opts []goaktactor.Option

	remoteCfg := remote.NewConfig("0.0.0.0", remotingPort, remoteOpts...)
	opts = append(opts, goaktactor.WithRemote(remoteCfg))

	if discovery.IsNilDiscoveryProvider(clConfig.DiscoveryProvider) {
		return opts
	}

	discoveryProvider := newDiscoveryAdapter(clConfig.DiscoveryProvider)

	clusterConfig := goaktactor.NewClusterConfig().
		WithDiscovery(discoveryProvider).
		WithPeersPort(peersPort).
		WithDiscoveryPort(discoveryPort).
		WithBootstrapTimeout(10 * time.Second).
		WithClusterBalancerInterval(time.Second).
		WithPartitionCount(20).
		WithMinimumPeersQuorum(1).
		WithReplicaCount(1)

	if len(kinds) > 0 {
		clusterConfig = clusterConfig.WithKinds(kinds...)
	}

	if clConfig.RegistrarRole != "" {
		clusterConfig = clusterConfig.WithRoles(clConfig.RegistrarRole)
	}
	opts = append(opts, goaktactor.WithCluster(clusterConfig))

	return opts
}
