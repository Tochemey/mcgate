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

// Package main runs the mcgate tenant quota assessment example.
//
// This program efficiently assesses tenant quota enforcement in a real-world
// scenario: it configures a tenant with tight limits (RequestsPerMinute=5,
// ConcurrentSessions=2), then runs concurrent and sequential invocations to
// verify that the gateway correctly throttles excess requests.
//
// Prerequisites: Node.js and npx.
//
// Run: make -C examples/quota-assess run
// Or:  go run ./examples/quota-assess
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tochemey/mcgate"
	"github.com/tochemey/mcgate/mcp"
)

const (
	tenantID        = "quota-tenant"
	rpmLimit        = 5
	concurrentLimit = 2
)

func main() {
	root := os.Getenv("MCP_FS_ROOT")
	if root == "" {
		root = "."
	}

	config := mcp.Config{
		Tools: []mcp.Tool{
			{
				ID:        "filesystem",
				Transport: mcp.TransportStdio,
				Stdio: &mcp.StdioTransportConfig{
					Command: "npx",
					Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", root},
				},
				State: mcp.ToolStateEnabled,
			},
		},
		Tenants: []mcp.TenantConfig{
			{
				ID: tenantID,
				Quotas: mcp.TenantQuotaConfig{
					RequestsPerMinute:  rpmLimit,
					ConcurrentSessions: concurrentLimit,
				},
			},
		},
	}

	gw, err := mcgate.New(config)
	if err != nil {
		log.Fatalf("create gateway: %v", err)
	}

	ctx := context.Background()
	if err := gw.Start(ctx); err != nil {
		log.Fatalf("start gateway: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	defer func() {
		if err := gw.Stop(ctx); err != nil {
			log.Printf("stop gateway: %v", err)
		}
	}()

	fmt.Println("=== Tenant Quota Assessment ===")
	fmt.Printf("Tenant: %s | RPM limit: %d | Concurrent sessions: %d\n\n", tenantID, rpmLimit, concurrentLimit)

	// Phase 1: Concurrent sessions — fire 4 concurrent invocations with different clients.
	// Expect: 2 allowed (create 2 sessions), 2 throttled (CONCURRENCY_LIMIT_REACHED).
	fmt.Println("--- Phase 1: Concurrent sessions ---")
	assessConcurrentSessions(ctx, gw)

	// Phase 2: Requests per minute — fire 7 sequential invocations (same client).
	// Expect: 5 allowed, 2 throttled (RATE_LIMITED).
	fmt.Println("\n--- Phase 2: Requests per minute ---")
	assessRequestsPerMinute(ctx, gw)

	fmt.Println("\n=== Assessment complete ===")
}

func assessConcurrentSessions(ctx context.Context, gw *mcgate.Gateway) {
	var allowed, throttled atomic.Int32
	var wg sync.WaitGroup

	// Fire concurrentLimit+2 invocations with distinct client IDs to exceed session limit.
	for i := 0; i < concurrentLimit+2; i++ {
		wg.Add(1)
		clientID := mcp.ClientID(fmt.Sprintf("client-%d", i+1))
		go func() {
			defer wg.Done()
			result, err := gw.Invoke(ctx, &mcp.Invocation{
				ToolID: "filesystem",
				Method: "tools/call",
				Params: map[string]any{
					"name":      "list_directory",
					"arguments": map[string]any{"path": "."},
				},
				Correlation: mcp.CorrelationMeta{
					TenantID:  tenantID,
					ClientID:  clientID,
					RequestID: mcp.RequestID(fmt.Sprintf("concurrent-req-%d", time.Now().UnixNano())),
				},
			})
			if err != nil {
				var rErr *mcp.RuntimeError
				if errors.As(err, &rErr) && rErr.Code == mcp.ErrCodeConcurrencyLimitReached {
					throttled.Add(1)
					return
				}
				throttled.Add(1) // other errors count as throttled for assessment
				return
			}
			if result != nil && result.Err != nil && result.Err.Code == mcp.ErrCodeConcurrencyLimitReached {
				throttled.Add(1)
				return
			}
			allowed.Add(1)
		}()
	}

	wg.Wait()
	a, t := allowed.Load(), throttled.Load()
	fmt.Printf("  Allowed: %d | Throttled (concurrency): %d\n", a, t)
	if a == int32(concurrentLimit) && t >= 1 {
		fmt.Printf("  ✓ ConcurrentSessions=%d enforced correctly\n", concurrentLimit)
	} else {
		fmt.Printf("  ⚠ Expected ~%d allowed, ~%d throttled; got %d allowed, %d throttled\n",
			concurrentLimit, 2, a, t)
	}
}

func assessRequestsPerMinute(ctx context.Context, gw *mcgate.Gateway) {
	// Use a single client so we reuse the same session; only RPM should throttle.
	clientID := mcp.ClientID("rpm-client")
	var allowed, throttled int

	for i := 0; i < rpmLimit+2; i++ {
		result, err := gw.Invoke(ctx, &mcp.Invocation{
			ToolID: "filesystem",
			Method: "tools/call",
			Params: map[string]any{
				"name":      "list_directory",
				"arguments": map[string]any{"path": "."},
			},
			Correlation: mcp.CorrelationMeta{
				TenantID:  tenantID,
				ClientID:  clientID,
				RequestID: mcp.RequestID(fmt.Sprintf("rpm-req-%d", i+1)),
			},
		})
		if err != nil {
			var rErr *mcp.RuntimeError
			if errors.As(err, &rErr) && rErr.Code == mcp.ErrCodeRateLimited {
				throttled++
				continue
			}
			throttled++ // other errors
			continue
		}
		if result != nil && result.Err != nil && result.Err.Code == mcp.ErrCodeRateLimited {
			throttled++
			continue
		}
		allowed++
	}

	fmt.Printf("  Allowed: %d | Throttled (rate limit): %d\n", allowed, throttled)
	if allowed == rpmLimit && throttled >= 1 {
		fmt.Printf("  ✓ RequestsPerMinute=%d enforced correctly\n", rpmLimit)
	} else {
		fmt.Printf("  ⚠ Expected %d allowed, ≥1 throttled; got %d allowed, %d throttled\n",
			rpmLimit, allowed, throttled)
	}
}
