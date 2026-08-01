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

// Package main runs the mcgate ingress example.
//
// This example demonstrates the MCP Streamable HTTP ingress layer:
//  1. Implementing [mcp.IdentityResolver] to extract tenant+client from HTTP headers.
//  2. Starting the gateway with a filesystem tool.
//  3. Mounting [Gateway.Handler] on a standard net/http server.
//  4. Connecting an MCP client via [sdkmcp.StreamableClientTransport].
//  5. Listing tools and calling one through the HTTP ingress.
//
// The identity resolver reads X-Tenant-ID and X-Client-ID from each request.
// The example client injects those headers via a custom http.RoundTripper.
//
// Prerequisites: Node.js and npx.
//
// Run from repo root:  go run ./examples/ingress
// Run from anywhere:   go run github.com/tochemey/mcgate/examples/ingress
//
// See examples/ingress/README.md for environment variables and details.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/tochemey/mcgate"
	"github.com/tochemey/mcgate/mcp"
)

// headerResolver extracts tenant and client identity from HTTP request headers.
//
// X-Tenant-ID identifies the tenant; X-Client-ID identifies the calling client.
// Both must be present; missing X-Tenant-ID rejects the session with HTTP 400.
type headerResolver struct{}

func (r *headerResolver) ResolveIdentity(req *http.Request) (mcp.TenantID, mcp.ClientID, error) {
	tenant := mcp.TenantID(req.Header.Get("X-Tenant-ID"))
	if tenant.IsZero() {
		return "", "", errors.New("missing X-Tenant-ID header")
	}
	client := mcp.ClientID(req.Header.Get("X-Client-ID"))
	return tenant, client, nil
}

// injectHeadersTransport wraps http.DefaultTransport and adds identity headers
// to every outgoing request so the server's headerResolver can authenticate the
// MCP client.
type injectHeadersTransport struct {
	tenantID string
	clientID string
	base     http.RoundTripper
}

func (t *injectHeadersTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("X-Tenant-ID", t.tenantID)
	clone.Header.Set("X-Client-ID", t.clientID)
	return t.base.RoundTrip(clone)
}

func main() {
	// --- 1. Read configuration from environment ---
	root := os.Getenv("MCP_FS_ROOT")
	if root == "" {
		root = "."
	}
	addr := os.Getenv("MCP_ADDR")
	if addr == "" {
		addr = "127.0.0.1:0" // pick a random free port
	}
	tenantID := os.Getenv("MCP_TENANT_ID")
	if tenantID == "" {
		tenantID = "ingress-tenant"
	}
	clientID := os.Getenv("MCP_CLIENT_ID")
	if clientID == "" {
		clientID = "ingress-client"
	}

	// --- 2. Build and start the gateway ---
	cfg := mcp.Config{
		Tenants: []mcp.TenantConfig{
			{ID: mcp.TenantID(tenantID)},
		},
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

	gw, err := mcgate.New(cfg)
	if err != nil {
		log.Fatalf("create gateway: %v", err)
	}

	ctx := context.Background()
	if err := gw.Start(ctx); err != nil {
		log.Fatalf("start gateway: %v", err)
	}
	// Allow foundational actors to complete their PostStart before serving traffic.
	time.Sleep(300 * time.Millisecond)
	defer func() {
		if err := gw.Stop(ctx); err != nil {
			log.Printf("stop gateway: %v", err)
		}
	}()

	// --- 3. Build the ingress handler ---
	h, err := gw.Handler(mcp.IngressConfig{
		IdentityResolver: &headerResolver{},
	})
	if err != nil {
		log.Fatalf("create handler: %v", err)
	}

	// --- 4. Start the HTTP server ---
	mux := http.NewServeMux()
	mux.Handle("/mcp", h)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	serverAddr := "http://" + ln.Addr().String() + "/mcp"

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("http server: %v", err)
		}
	}()
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	fmt.Println("=== mcgate ingress example ===")
	fmt.Printf("MCP endpoint:  %s\n", serverAddr)
	fmt.Printf("Filesystem root: %s\n", root)
	fmt.Printf("Tenant: %s  Client: %s\n\n", tenantID, clientID)

	// --- 5. Connect an MCP client via Streamable HTTP ---
	// The custom transport injects the identity headers that headerResolver needs.
	mcpClient := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "ingress-example-client", Version: "0.1"}, nil)
	transport := &sdkmcp.StreamableClientTransport{
		Endpoint: serverAddr,
		HTTPClient: &http.Client{
			Transport: &injectHeadersTransport{
				tenantID: tenantID,
				clientID: clientID,
				base:     http.DefaultTransport,
			},
		},
		DisableStandaloneSSE: true,
	}

	session, err := mcpClient.Connect(ctx, transport, nil)
	if err != nil {
		log.Fatalf("mcp connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	// --- 6. List tools available in this session ---
	listResult, err := session.ListTools(ctx, &sdkmcp.ListToolsParams{})
	if err != nil {
		log.Fatalf("list tools: %v", err)
	}
	fmt.Printf("Tools visible in session: %d\n", len(listResult.Tools))
	for _, t := range listResult.Tools {
		fmt.Printf("  - %s\n", t.Name)
	}
	fmt.Println()

	// --- 7. Call the filesystem tool through the HTTP ingress ---
	fmt.Println("--- Calling filesystem tool via HTTP ingress ---")
	callResult, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "filesystem",
		Arguments: map[string]any{
			"name":      "list_directory",
			"arguments": map[string]any{"path": root},
		},
	})
	if err != nil {
		log.Fatalf("call tool: %v", err)
	}

	if callResult.IsError {
		fmt.Println("Tool returned an error:")
		for _, c := range callResult.Content {
			if txt, ok := c.(*sdkmcp.TextContent); ok {
				fmt.Printf("  %s\n", txt.Text)
			}
		}
	} else {
		fmt.Printf("Tool call succeeded. Content items: %d\n", len(callResult.Content))
		for _, c := range callResult.Content {
			if txt, ok := c.(*sdkmcp.TextContent); ok {
				fmt.Printf("  %s\n", txt.Text)
			}
		}
	}
}
