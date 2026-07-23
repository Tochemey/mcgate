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
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tochemey/portcullis/mcp"
)

// recordingProvider records method calls for verification.
type recordingProvider struct {
	id             string
	peers          []string
	startCalled    atomic.Bool
	stopCalled     atomic.Bool
	discoverCalled atomic.Bool
	startErr       error
	stopErr        error
	discoverErr    error
	startCtx       context.Context
	discoverCtx    context.Context
}

var _ mcp.DiscoveryProvider = (*recordingProvider)(nil)

func (r *recordingProvider) ID() string { return r.id }

func (r *recordingProvider) Start(ctx context.Context) error {
	r.startCalled.Store(true)
	r.startCtx = ctx
	return r.startErr
}

func (r *recordingProvider) DiscoverPeers(ctx context.Context) ([]string, error) {
	r.discoverCalled.Store(true)
	r.discoverCtx = ctx
	return r.peers, r.discoverErr
}

func (r *recordingProvider) Stop(_ context.Context) error {
	r.stopCalled.Store(true)
	return r.stopErr
}

func TestDiscoveryAdapter_ID(t *testing.T) {
	provider := &recordingProvider{id: "test-provider"}
	adapter := newDiscoveryAdapter(provider)
	assert.Equal(t, "test-provider", adapter.ID())
}

func TestDiscoveryAdapter_Initialize(t *testing.T) {
	t.Run("delegates to Start", func(t *testing.T) {
		provider := &recordingProvider{id: "test"}
		adapter := newDiscoveryAdapter(provider)
		err := adapter.Initialize()
		require.NoError(t, err)
		assert.True(t, provider.startCalled.Load())
	})

	t.Run("propagates Start error", func(t *testing.T) {
		provider := &recordingProvider{id: "test", startErr: errors.New("start failed")}
		adapter := newDiscoveryAdapter(provider)
		err := adapter.Initialize()
		require.Error(t, err)
		assert.Equal(t, "start failed", err.Error())
	})
}

func TestDiscoveryAdapter_Register(t *testing.T) {
	provider := &recordingProvider{id: "test"}
	adapter := newDiscoveryAdapter(provider)
	err := adapter.Register()
	require.NoError(t, err)
}

func TestDiscoveryAdapter_Deregister(t *testing.T) {
	provider := &recordingProvider{id: "test"}
	adapter := newDiscoveryAdapter(provider)
	err := adapter.Deregister()
	require.NoError(t, err)
}

func TestDiscoveryAdapter_DiscoverPeers(t *testing.T) {
	t.Run("returns peers from provider", func(t *testing.T) {
		provider := &recordingProvider{id: "test", peers: []string{"host1:8080", "host2:8080"}}
		adapter := newDiscoveryAdapter(provider)
		peers, err := adapter.DiscoverPeers()
		require.NoError(t, err)
		assert.Equal(t, []string{"host1:8080", "host2:8080"}, peers)
		assert.True(t, provider.discoverCalled.Load())
	})

	t.Run("propagates discover error", func(t *testing.T) {
		provider := &recordingProvider{id: "test", discoverErr: errors.New("network error")}
		adapter := newDiscoveryAdapter(provider)
		_, err := adapter.DiscoverPeers()
		require.Error(t, err)
		assert.Equal(t, "network error", err.Error())
	})
}

func TestDiscoveryAdapter_Close(t *testing.T) {
	t.Run("calls Stop and cancels context", func(t *testing.T) {
		provider := &recordingProvider{id: "test"}
		adapter := newDiscoveryAdapter(provider)

		// Initialize first to capture the context
		_ = adapter.Initialize()
		capturedCtx := provider.startCtx

		err := adapter.Close()
		require.NoError(t, err)
		assert.True(t, provider.stopCalled.Load())

		// The adapter's context should be cancelled after Close
		assert.Error(t, capturedCtx.Err(), "adapter context should be cancelled after Close")
	})

	t.Run("propagates Stop error", func(t *testing.T) {
		provider := &recordingProvider{id: "test", stopErr: errors.New("stop failed")}
		adapter := newDiscoveryAdapter(provider)
		err := adapter.Close()
		require.Error(t, err)
		assert.Equal(t, "stop failed", err.Error())
	})
}

func TestDiscoveryAdapter_ContextPropagation(t *testing.T) {
	provider := &recordingProvider{id: "test", peers: []string{"peer:8080"}}
	adapter := newDiscoveryAdapter(provider)

	_ = adapter.Initialize()
	_, _ = adapter.DiscoverPeers()

	// The contexts passed to Start and DiscoverPeers should both be
	// derived from the adapter's internal context.
	require.NotNil(t, provider.startCtx)
	require.NotNil(t, provider.discoverCtx)
	assert.NoError(t, provider.startCtx.Err())
	assert.NoError(t, provider.discoverCtx.Err())

	// After Close, the adapter's context is cancelled.
	_ = adapter.Close()
	assert.Error(t, provider.startCtx.Err())
	assert.Error(t, provider.discoverCtx.Err())
}
