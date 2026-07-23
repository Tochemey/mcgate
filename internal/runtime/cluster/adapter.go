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
	"time"

	"github.com/tochemey/goakt/v4/discovery"

	"github.com/tochemey/portcullis/mcp"
)

// discoveryAdapter wraps an mcp.DiscoveryProvider to satisfy GoAkt's
// discovery.Provider interface. The adapter owns a background-derived context
// that is cancelled on Close, keeping the discovery lifecycle independent of
// any caller-scoped context.
type discoveryAdapter struct {
	provider mcp.DiscoveryProvider
	ctx      context.Context
	cancel   context.CancelFunc
}

// Compile-time interface check.
var _ discovery.Provider = (*discoveryAdapter)(nil)

// newDiscoveryAdapter creates an adapter that bridges the simplified
// mcp.DiscoveryProvider to GoAkt's discovery.Provider. The adapter owns its
// own background-derived context so the discovery lifecycle is independent of
// any caller-scoped context (e.g. a startup context with a deadline). The
// adapter context is cancelled when Close is called.
func newDiscoveryAdapter(provider mcp.DiscoveryProvider) *discoveryAdapter {
	ctx, cancel := context.WithCancel(context.Background())
	return &discoveryAdapter{
		provider: provider,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// ID returns the provider name.
func (a *discoveryAdapter) ID() string {
	return a.provider.ID()
}

// Initialize delegates to the provider's Start method.
// GoAkt calls Initialize once during actor system startup.
func (a *discoveryAdapter) Initialize() error {
	return a.provider.Start(a.ctx)
}

// Register is a no-op. The simplified DiscoveryProvider interface merges
// registration into Start.
func (a *discoveryAdapter) Register() error {
	return nil
}

// Deregister is a no-op. The simplified DiscoveryProvider interface merges
// deregistration into Stop.
func (a *discoveryAdapter) Deregister() error {
	return nil
}

// DiscoverPeers delegates to the provider's DiscoverPeers method,
// passing the adapter's context for cancellation and timeout support.
func (a *discoveryAdapter) DiscoverPeers() ([]string, error) {
	return a.provider.DiscoverPeers(a.ctx)
}

// Close cancels the adapter context (aborting any in-flight DiscoverPeers
// calls) and delegates to the provider's Stop method with a bounded timeout
// so a stuck provider cannot block actor system shutdown indefinitely.
func (a *discoveryAdapter) Close() error {
	a.cancel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return a.provider.Stop(ctx)
}
