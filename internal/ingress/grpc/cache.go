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

package grpc

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/tochemey/mcgate/internal/ingress/pkg"
	"github.com/tochemey/mcgate/mcp"
)

// toolNameCache is a short-lived TTL cache that maps tool names to gateway
// ToolIDs. It avoids a ListTools actor Ask on every CallTool and
// CallToolStream request.
//
// The cache is safe for concurrent use from multiple goroutines. Reads use a
// shared read lock; rebuilds acquire an exclusive write lock with
// double-checked locking to prevent thundering-herd refreshes.
type toolNameCache struct {
	mu       sync.RWMutex
	byName   map[string]mcp.ToolID
	expireAt time.Time
	ttl      time.Duration
}

// newToolNameCache creates a cache with the given TTL.
func newToolNameCache(ttl time.Duration) *toolNameCache {
	return &toolNameCache{
		ttl:    ttl,
		byName: make(map[string]mcp.ToolID),
	}
}

// resolve maps a tool name to a ToolID, using the cache when fresh and
// falling back to gw.ListTools when the cache is expired or the name is
// not found in an expired cache.
func (c *toolNameCache) resolve(ctx context.Context, gw pkg.Invoker, toolName string) (mcp.ToolID, error) {
	c.mu.RLock()
	if time.Now().Before(c.expireAt) {
		if id, ok := c.byName[toolName]; ok {
			c.mu.RUnlock()
			return id, nil
		}
		// Cache is fresh but name not found — tool does not exist.
		c.mu.RUnlock()
		return "", fmt.Errorf("tool %q not found", toolName)
	}
	c.mu.RUnlock()

	return c.refreshAndResolve(ctx, gw, toolName)
}

// refreshAndResolve rebuilds the cache from ListTools under a write lock and
// resolves the tool name. Double-checked locking prevents multiple goroutines
// from refreshing concurrently when the cache expires.
func (c *toolNameCache) refreshAndResolve(ctx context.Context, gw pkg.Invoker, toolName string) (mcp.ToolID, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Re-check: another goroutine may have refreshed while we waited for the lock.
	if time.Now().Before(c.expireAt) {
		if id, ok := c.byName[toolName]; ok {
			return id, nil
		}
		return "", fmt.Errorf("tool %q not found", toolName)
	}

	tools, err := gw.ListTools(ctx)
	if err != nil {
		return "", fmt.Errorf("list tools: %w", err)
	}

	c.byName = buildToolNameMap(tools)
	c.expireAt = time.Now().Add(c.ttl)

	if id, ok := c.byName[toolName]; ok {
		return id, nil
	}
	return "", fmt.Errorf("tool %q not found", toolName)
}

// buildToolNameMap constructs the name-to-ToolID index from the tool list.
// Single-schema tools are indexed by their tool ID. Multi-schema tools are
// indexed by each schema name, all mapping to the parent tool ID.
func buildToolNameMap(tools []mcp.Tool) map[string]mcp.ToolID {
	m := make(map[string]mcp.ToolID)
	for _, tool := range tools {
		// Single-schema tools: tool name matches tool ID.
		m[string(tool.ID)] = tool.ID
		// Multi-schema tools: each schema name maps to the parent tool ID.
		for _, schema := range tool.Schemas {
			m[schema.Name] = tool.ID
		}
	}
	return m
}
