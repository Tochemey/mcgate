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

package pkg

import (
	"context"
	"sync"
	"time"

	"github.com/tochemey/goakt-mcp/mcp"
)

// toolListCache caches the tenant-visible tool list for a short TTL so the
// stateless ingress does not pay a registrar round-trip on every request.
// Registry changes become visible after at most one TTL; the gRPC ingress
// makes the same tradeoff with its tool-name cache.
type toolListCache struct {
	ttl time.Duration

	mu      sync.RWMutex
	entries map[mcp.TenantID]toolListEntry
}

type toolListEntry struct {
	tools   []mcp.Tool
	expires time.Time
}

// newToolListCache creates a cache with the given TTL. A non-positive TTL
// disables caching: every get resolves through the gateway.
func newToolListCache(ttl time.Duration) *toolListCache {
	return &toolListCache{
		ttl:     ttl,
		entries: make(map[mcp.TenantID]toolListEntry),
	}
}

// get returns the tenant-visible tool list, serving from cache when fresh and
// resolving through the gateway (tenant-scoped when supported) otherwise.
// Concurrent misses for the same tenant may each resolve once; the last
// writer wins, which is harmless for an idempotent read.
func (c *toolListCache) get(ctx context.Context, gw Invoker, tenantID mcp.TenantID) ([]mcp.Tool, error) {
	if c.ttl > 0 {
		c.mu.RLock()
		entry, ok := c.entries[tenantID]
		c.mu.RUnlock()
		if ok && time.Now().Before(entry.expires) {
			return entry.tools, nil
		}
	}

	tools, err := ListToolsFor(ctx, gw, tenantID)
	if err != nil {
		return nil, err
	}

	if c.ttl > 0 {
		c.mu.Lock()
		c.entries[tenantID] = toolListEntry{tools: tools, expires: time.Now().Add(c.ttl)}
		c.mu.Unlock()
	}
	return tools, nil
}
