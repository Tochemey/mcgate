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

// Package main runs the portcullis audit + HTTP example.
//
// This example demonstrates:
//  1. Durable audit via FileSink — invocation events are written to audit.log.
//  2. HTTP-based egress — an MCP tool reached over HTTP (server-everything).
//  3. Mixed transports — one stdio tool (filesystem) and one HTTP tool (everything).
//
// Prerequisites:
//   - Node.js and npx
//   - Run the HTTP MCP server in a separate terminal:
//     npx -y @modelcontextprotocol/server-everything streamableHttp
//
// Run from repo root:  go run ./examples/audit-http
// Run from anywhere:   go run github.com/tochemey/portcullis/examples/audit-http
//
// See examples/audit-http/README.md for a full walkthrough.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/tochemey/portcullis"
	"github.com/tochemey/portcullis/internal/runtime/audit"
	"github.com/tochemey/portcullis/mcp"
)

func main() {
	// --- 1. Configure paths and URLs ---
	root := os.Getenv("MCP_FS_ROOT")
	if root == "" {
		root = "."
	}
	httpURL := os.Getenv("MCP_HTTP_URL")
	// server-everything streamableHttp serves at /mcp on port 3001
	if httpURL == "" {
		httpURL = "http://localhost:3001/mcp"
	}
	auditDir := os.Getenv("MCP_AUDIT_DIR")
	if auditDir == "" {
		auditDir = filepath.Join(os.TempDir(), "portcullis-audit")
	}

	// --- 2. Create durable audit sink ---
	// FileSink writes invocation events as NDJSON to audit.log.
	fileSink, err := audit.NewFileSink(auditDir)
	if err != nil {
		log.Fatalf("create audit sink: %v", err)
	}

	// --- 3. Build config: filesystem (stdio) + everything (HTTP) + audit ---
	tools := []mcp.Tool{
		{
			ID:        "filesystem",
			Transport: mcp.TransportStdio,
			Stdio: &mcp.StdioTransportConfig{
				Command: "npx",
				Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", root},
			},
			State: mcp.ToolStateEnabled,
		},
	}
	if httpURL != "" {
		tools = append(tools, mcp.Tool{
			ID:        "everything",
			Transport: mcp.TransportHTTP,
			HTTP:      &mcp.HTTPTransportConfig{URL: httpURL},
			State:     mcp.ToolStateEnabled,
		})
	}

	config := mcp.Config{
		Audit: mcp.AuditConfig{Sink: fileSink},
		Tools: tools,
	}

	gw, err := portcullis.New(config)
	if err != nil {
		log.Fatalf("create gateway: %v", err)
	}

	ctx := context.Background()
	if err := gw.Start(ctx); err != nil {
		log.Fatalf("start gateway: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	fmt.Println("=== portcullis audit + HTTP example ===")
	fmt.Printf("Audit log: %s/audit.log\n", auditDir)
	fmt.Printf("Filesystem root: %s\n", root)
	fmt.Printf("HTTP tool URL: %s\n\n", httpURL)

	// --- 4. List tools ---
	toolsList, err := gw.ListTools(ctx)
	if err != nil {
		log.Fatalf("list tools: %v", err)
	}
	fmt.Printf("Registered tools: %d\n", len(toolsList))
	for _, t := range toolsList {
		fmt.Printf("  - %s (%s)\n", t.ID, t.Transport)
	}
	fmt.Println()

	corr := mcp.CorrelationMeta{
		TenantID:  "audit-http-tenant",
		ClientID:  "audit-http-client",
		RequestID: "req-1",
	}

	// --- 5. Invoke filesystem (stdio) ---
	fmt.Println("--- Invoking filesystem (stdio) ---")
	fsResult, err := gw.Invoke(ctx, &mcp.Invocation{
		ToolID: "filesystem",
		Method: "tools/call",
		Params: map[string]any{
			"name":      "list_directory",
			"arguments": map[string]any{"path": root},
		},
		Correlation: corr,
	})
	if err != nil {
		log.Printf("invoke filesystem: %v", err)
	} else {
		fmt.Printf("Status: %s (duration: %v)\n", fsResult.Status, fsResult.Duration)
		if fsResult.Err != nil {
			fmt.Printf("Error: %s\n", fsResult.Err.Error())
		}
	}
	fmt.Println()

	// --- 6. Invoke everything (HTTP) if registered ---
	if httpURL != "" {
		fmt.Println("--- Invoking everything (HTTP) ---")
		evResult, err := gw.Invoke(ctx, &mcp.Invocation{
			ToolID: "everything",
			Method: "tools/call",
			Params: map[string]any{
				"name":      "get-sum",
				"arguments": map[string]any{"a": 2, "b": 3},
			},
			Correlation: mcp.CorrelationMeta{
				TenantID:  corr.TenantID,
				ClientID:  corr.ClientID,
				RequestID: "req-2",
			},
		})
		if err != nil {
			log.Printf("invoke everything: %v (is the HTTP server running?)", err)
		} else {
			fmt.Printf("Status: %s (duration: %v)\n", evResult.Status, evResult.Duration)
			if evResult.Err != nil {
				fmt.Printf("Error: %s\n", evResult.Err.Error())
			}
			if len(evResult.Output) > 0 {
				pretty, _ := json.MarshalIndent(evResult.Output, "  ", "  ")
				fmt.Printf("Output: %s\n", pretty)
			}
		}
		fmt.Println()
	}

	// --- 7. Stop gateway, flush audit sink, then print audit log ---
	if err := gw.Stop(ctx); err != nil {
		log.Printf("stop gateway: %v", err)
	}
	if err := fileSink.Close(); err != nil {
		log.Printf("close audit sink: %v", err)
	}

	fmt.Println("--- Audit log (last 20 events) ---")
	auditPath := filepath.Clean(filepath.Join(auditDir, "audit.log"))
	f, err := os.Open(auditPath) //nolint:gosec // auditPath is constructed from a trusted config value
	if err != nil {
		log.Printf("open audit log: %v", err)
		return
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		log.Printf("read audit log: %v", err)
		return
	}

	start := 0
	if len(lines) > 20 {
		start = len(lines) - 20
	}
	for i := start; i < len(lines); i++ {
		var ev mcp.AuditEvent
		if err := json.Unmarshal([]byte(lines[i]), &ev); err != nil {
			fmt.Printf("  [parse error] %s\n", lines[i])
			continue
		}
		fmt.Printf("  %s | %s | tenant=%s tool=%s outcome=%s\n",
			ev.Timestamp.Format(time.RFC3339), ev.Type, ev.TenantID, ev.ToolID, ev.Outcome)
	}
}
