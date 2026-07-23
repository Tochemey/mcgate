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

// Package main runs the portcullis filesystem example.
//
// This example demonstrates the core gateway workflow:
//  1. Configure the gateway with one MCP tool: the stdio-based filesystem server.
//  2. Start the gateway (actor system + tool bootstrap).
//  3. List registered tools.
//  4. Invoke the list_directory tool and print the result.
//
// The MCP filesystem server is launched as a child process (npx @modelcontextprotocol/server-filesystem).
// The gateway supervises it, manages sessions per tenant+client, and routes invocations over stdin/stdout.
//
// Prerequisites: Node.js and npx.
//
// Run from repo root:  go run ./examples/filesystem
// Run from anywhere:   go run github.com/tochemey/portcullis/examples/filesystem
//
// See examples/filesystem/README.md for a full walkthrough and environment variables.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/tochemey/portcullis"
	"github.com/tochemey/portcullis/mcp"
)

func main() {
	// --- 1. Choose the filesystem root for the MCP server ---
	// The server will only allow operations under this path. Default: current directory.
	// On macOS, set MCP_FS_ROOT=/private/tmp to list /tmp (since /tmp -> /private/tmp).
	root := os.Getenv("MCP_FS_ROOT")
	if root == "" {
		root = "."
	}

	// --- 2. Build gateway config with one stdio tool ---
	// The "filesystem" tool runs as a child process: npx runs the official MCP filesystem server.
	// The gateway will start this process on Start(), supervise it, and route invocations to it.
	cfg := mcp.Config{
		LogLevel: "info",
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
	}

	gw, err := portcullis.New(cfg)
	if err != nil {
		log.Fatalf("create gateway: %v", err)
	}

	ctx := context.Background()
	if err := gw.Start(ctx); err != nil {
		log.Fatalf("start gateway: %v", err)
	}
	// Foundational actors (registrar, router, tool supervisor) spawn asynchronously after Start.
	// A short delay ensures they are ready before we call ListTools and Invoke.
	time.Sleep(200 * time.Millisecond)
	defer func() {
		if err := gw.Stop(ctx); err != nil {
			log.Printf("stop gateway: %v", err)
		}
	}()

	fmt.Println("=== portcullis filesystem example ===")
	fmt.Printf("Filesystem root: %s\n\n", root)

	// --- 3. List tools registered by the gateway ---
	// After bootstrap, the gateway has registered the "filesystem" tool; ListTools returns it.
	tools, err := gw.ListTools(ctx)
	if err != nil {
		log.Fatalf("list tools: %v", err)
	}
	fmt.Printf("Registered tools: %d\n", len(tools))
	for _, t := range tools {
		fmt.Printf("  - %s (%s)\n", t.ID, t.Transport)
	}
	fmt.Println()

	// --- 4. Invoke the list_directory tool ---
	// Invocation specifies: which tool, the MCP method (tools/call), and params in MCP shape
	// (name = tool call name, arguments = object for that tool). Correlation identifies tenant/client/request.
	result, err := gw.Invoke(ctx, &mcp.Invocation{
		ToolID: "filesystem",
		Method: "tools/call",
		Params: map[string]any{
			"name":      "list_directory",
			"arguments": map[string]any{"path": root},
		},
		Correlation: mcp.CorrelationMeta{
			TenantID:  "example-tenant",
			ClientID:  "example-client",
			RequestID: "req-1",
		},
	})
	if err != nil {
		log.Fatalf("invoke list_directory: %v", err)
	}

	// --- 5. Print result ---
	fmt.Printf("Invocation status: %s (duration: %v)\n", result.Status, result.Duration)
	if result.Err != nil {
		fmt.Printf("Error: %s\n", result.Err.Error())
	}
	if len(result.Output) > 0 {
		fmt.Println("\nOutput:")
		pretty, _ := json.MarshalIndent(result.Output, "  ", "  ")
		fmt.Println(string(pretty))
	}
}
