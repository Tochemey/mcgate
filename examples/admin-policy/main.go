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

// Package main runs the mcgate admin API and pluggable policy example.
//
// This example demonstrates two features added alongside the core gateway:
//
// # Admin / Operational API
//
// After the gateway is started, the admin methods provide live operational
// visibility and control without interrupting in-flight requests:
//
//   - [Gateway.GetGatewayStatus] — overall health, tool and session counts.
//   - [Gateway.GetToolStatus] — per-tool circuit state, session count, drain flag.
//   - [Gateway.ListSessions] — all active sessions across all tools.
//   - [Gateway.DrainTool] — stop accepting new sessions for a tool.
//   - [Gateway.ResetCircuit] — manually close a tripped circuit breaker.
//
// # Pluggable Policy Engine
//
// Each tenant can attach a [mcp.PolicyEvaluator] that is called after all
// built-in authorization and quota checks pass. Returning a non-nil
// [mcp.RuntimeError] denies the invocation; returning nil allows it.
//
// This example uses a simple role-based evaluator: the "read-only" tenant may
// only call tools whose ID starts with "read-", all other calls are denied.
//
// Prerequisites: Node.js and npx.
//
// Run from repo root:  go run ./examples/admin-policy
// Run from anywhere:   go run github.com/tochemey/mcgate/examples/admin-policy
//
// See examples/admin-policy/README.md for environment variables and details.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/tochemey/mcgate"
	"github.com/tochemey/mcgate/mcp"
)

// readOnlyEvaluator is a [mcp.PolicyEvaluator] that restricts a tenant to
// tools whose ID starts with a given prefix. Any other tool call is denied
// with ErrCodePolicyDenied.
type readOnlyEvaluator struct {
	allowedPrefix string
}

func (e *readOnlyEvaluator) Evaluate(_ context.Context, in mcp.PolicyInput) *mcp.RuntimeError {
	if strings.HasPrefix(string(in.ToolID), e.allowedPrefix) {
		return nil // allow
	}
	return mcp.NewRuntimeError(
		mcp.ErrCodePolicyDenied,
		fmt.Sprintf("tenant %q may only call tools with prefix %q; denied %q",
			in.TenantID, e.allowedPrefix, in.ToolID),
	)
}

func main() {
	root := os.Getenv("MCP_FS_ROOT")
	if root == "" {
		root = "."
	}

	// --- 1. Configure gateway with two tenants ---
	//
	// "ops-tenant" has no custom evaluator; all tools are accessible.
	// "read-only-tenant" has a readOnlyEvaluator that only allows tools
	// with the "read-" prefix. Since our tool is named "filesystem" (no
	// "read-" prefix), all invocations from this tenant will be denied.
	cfg := mcp.Config{
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
				ID: "ops-tenant",
				Quotas: mcp.TenantQuotaConfig{
					RequestsPerMinute:  60,
					ConcurrentSessions: 10,
				},
			},
			{
				ID: "read-only-tenant",
				Quotas: mcp.TenantQuotaConfig{
					RequestsPerMinute:  30,
					ConcurrentSessions: 5,
				},
				Evaluator: &readOnlyEvaluator{allowedPrefix: "read-"},
			},
		},
	}

	gw, err := mcgate.New(cfg)
	if err != nil {
		log.Fatalf("create gateway: %v", err)
	}

	ctx := context.Background()
	if err := gw.Start(ctx); err != nil {
		log.Fatalf("start gateway: %v", err)
	}
	// Allow foundational actors to complete their PostStart.
	time.Sleep(300 * time.Millisecond)
	defer func() {
		if err := gw.Stop(ctx); err != nil {
			log.Printf("stop gateway: %v", err)
		}
	}()

	fmt.Println("=== mcgate admin API + pluggable policy example ===")
	fmt.Println()

	// -------------------------------------------------------------------------
	// Section A: Admin / Operational API
	// -------------------------------------------------------------------------
	fmt.Println("── Admin API ─────────────────────────────────────────────")

	// GetGatewayStatus
	gwStatus, err := gw.GetGatewayStatus(ctx)
	if err != nil {
		log.Fatalf("GetGatewayStatus: %v", err)
	}
	fmt.Printf("Gateway: running=%v  tools=%d  sessions=%d\n",
		gwStatus.Running, gwStatus.ToolCount, gwStatus.SessionCount)

	// GetToolStatus — inspect the "filesystem" tool
	toolStatus, err := gw.GetToolStatus(ctx, "filesystem")
	if err != nil {
		log.Fatalf("GetToolStatus: %v", err)
	}
	fmt.Printf("Tool %q: state=%s  circuit=%s  sessions=%d  draining=%v\n",
		toolStatus.ToolID, toolStatus.State, toolStatus.Circuit,
		toolStatus.SessionCount, toolStatus.Draining)

	// ListSessions — empty at this point
	sessions, err := gw.ListSessions(ctx)
	if err != nil {
		log.Fatalf("ListSessions: %v", err)
	}
	fmt.Printf("Active sessions: %d\n", len(sessions))

	// DrainTool — stop accepting new sessions for "filesystem"
	fmt.Println()
	fmt.Println("Draining tool \"filesystem\" ...")
	if err := gw.DrainTool(ctx, "filesystem"); err != nil {
		log.Fatalf("DrainTool: %v", err)
	}
	afterDrain, err := gw.GetToolStatus(ctx, "filesystem")
	if err != nil {
		log.Fatalf("GetToolStatus after drain: %v", err)
	}
	fmt.Printf("  After drain: draining=%v\n", afterDrain.Draining)

	// ResetCircuit — manually close the circuit breaker
	fmt.Println()
	fmt.Println("Resetting circuit for \"filesystem\" ...")
	if err := gw.ResetCircuit(ctx, "filesystem"); err != nil {
		log.Fatalf("ResetCircuit: %v", err)
	}
	afterReset, err := gw.GetToolStatus(ctx, "filesystem")
	if err != nil {
		log.Fatalf("GetToolStatus after reset: %v", err)
	}
	fmt.Printf("  After reset: circuit=%s\n", afterReset.Circuit)

	// -------------------------------------------------------------------------
	// Section B: Pluggable Policy Engine
	// -------------------------------------------------------------------------
	fmt.Println()
	fmt.Println("── Pluggable Policy Engine ───────────────────────────────")

	// EnableTool lifts the drain: it sets the tool state back to enabled and
	// sends RefreshToolConfig to the supervisor, which resets the draining flag.
	if err := gw.EnableTool(ctx, "filesystem"); err != nil {
		log.Printf("EnableTool: %v (tool may already be enabled)", err)
	}
	time.Sleep(100 * time.Millisecond)

	// Invocation from "ops-tenant": no custom evaluator → should succeed.
	fmt.Println()
	fmt.Println("Invoking as ops-tenant (no custom evaluator) ...")
	opsResult, err := gw.Invoke(ctx, &mcp.Invocation{
		ToolID: "filesystem",
		Method: "tools/call",
		Params: map[string]any{
			"name":      "list_directory",
			"arguments": map[string]any{"path": root},
		},
		Correlation: mcp.CorrelationMeta{
			TenantID:  "ops-tenant",
			ClientID:  "ops-client",
			RequestID: "policy-req-1",
		},
	})
	if err != nil {
		var rErr *mcp.RuntimeError
		if errors.As(err, &rErr) {
			fmt.Printf("  ops-tenant: DENIED by policy — %s: %s\n", rErr.Code, rErr.Message)
		} else {
			fmt.Printf("  ops-tenant: error — %v\n", err)
		}
	} else if opsResult.Err != nil {
		fmt.Printf("  ops-tenant: tool error — %s\n", opsResult.Err.Message)
	} else {
		fmt.Printf("  ops-tenant: ALLOWED (status=%s)\n", opsResult.Status)
	}

	// Invocation from "read-only-tenant": readOnlyEvaluator denies "filesystem"
	// because it doesn't start with "read-".
	fmt.Println()
	fmt.Println("Invoking as read-only-tenant (custom evaluator — should deny) ...")
	readOnlyResult, err := gw.Invoke(ctx, &mcp.Invocation{
		ToolID: "filesystem",
		Method: "tools/call",
		Params: map[string]any{
			"name":      "list_directory",
			"arguments": map[string]any{"path": root},
		},
		Correlation: mcp.CorrelationMeta{
			TenantID:  "read-only-tenant",
			ClientID:  "read-only-client",
			RequestID: "policy-req-2",
		},
	})
	if err != nil {
		var rErr *mcp.RuntimeError
		if errors.As(err, &rErr) {
			fmt.Printf("  read-only-tenant: DENIED by policy — %s: %s\n", rErr.Code, rErr.Message)
		} else {
			fmt.Printf("  read-only-tenant: error — %v\n", err)
		}
	} else if readOnlyResult != nil && readOnlyResult.Err != nil {
		fmt.Printf("  read-only-tenant: DENIED by policy — %s: %s\n",
			readOnlyResult.Err.Code, readOnlyResult.Err.Message)
	} else {
		fmt.Printf("  read-only-tenant: ALLOWED (status=%s)\n", readOnlyResult.Status)
	}

	fmt.Println()
	fmt.Println("=== Example complete ===")
}
