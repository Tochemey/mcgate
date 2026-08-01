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

// Package main runs the mcgate gRPC ingress example.
//
// This example demonstrates the MCP gRPC ingress layer:
//  1. Implementing [mcp.GRPCIdentityResolver] to extract tenant+client from gRPC metadata.
//  2. Starting the gateway with a filesystem tool.
//  3. Registering [Gateway.RegisterGRPCService] on a standard gRPC server.
//  4. Connecting a gRPC client and listing tools, then calling one.
//
// The identity resolver reads "x-tenant-id" and "x-client-id" from gRPC
// metadata. The example client injects those values via outgoing metadata.
//
// Prerequisites: Node.js and npx.
//
// Run from repo root:  go run ./examples/ingress-grpc
// Run from anywhere:   go run github.com/tochemey/mcgate/examples/ingress-grpc
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	"github.com/tochemey/mcgate"
	pb "github.com/tochemey/mcgate/internal/ingress/grpc/pb"
	"github.com/tochemey/mcgate/mcp"
)

// metadataKeyTenantID is the gRPC metadata key for the tenant identifier.
const metadataKeyTenantID = "x-tenant-id"

// metadataKeyClientID is the gRPC metadata key for the client identifier.
const metadataKeyClientID = "x-client-id"

// metadataResolver extracts tenant and client identity from gRPC metadata.
type metadataResolver struct{}

func (r *metadataResolver) ResolveGRPCIdentity(ctx context.Context) (mcp.TenantID, mcp.ClientID, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", "", errors.New("missing gRPC metadata")
	}

	tenantVals := md.Get(metadataKeyTenantID)
	if len(tenantVals) == 0 || tenantVals[0] == "" {
		return "", "", fmt.Errorf("missing %s metadata", metadataKeyTenantID)
	}

	var clientID mcp.ClientID
	clientVals := md.Get(metadataKeyClientID)
	if len(clientVals) > 0 {
		clientID = mcp.ClientID(clientVals[0])
	}

	return mcp.TenantID(tenantVals[0]), clientID, nil
}

func main() {
	// --- 1. Read configuration from environment ---
	root := os.Getenv("MCP_FS_ROOT")
	if root == "" {
		root = "."
	}
	addr := os.Getenv("MCP_GRPC_ADDR")
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	tenantID := os.Getenv("MCP_TENANT_ID")
	if tenantID == "" {
		tenantID = "grpc-tenant"
	}
	clientID := os.Getenv("MCP_CLIENT_ID")
	if clientID == "" {
		clientID = "grpc-client"
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

	// --- 3. Create and start the gRPC server ---
	srv := grpc.NewServer()
	if err := gw.RegisterGRPCService(srv, mcp.GRPCIngressConfig{
		IdentityResolver: &metadataResolver{},
	}); err != nil {
		log.Fatalf("register grpc service: %v", err)
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	serverAddr := ln.Addr().String()

	go func() {
		if err := srv.Serve(ln); err != nil {
			log.Printf("grpc server: %v", err)
		}
	}()
	defer srv.GracefulStop()

	fmt.Println("=== mcgate gRPC ingress example ===")
	fmt.Printf("gRPC endpoint:   %s\n", serverAddr)
	fmt.Printf("Filesystem root: %s\n", root)
	fmt.Printf("Tenant: %s  Client: %s\n\n", tenantID, clientID)

	// --- 4. Connect a gRPC client ---
	conn, err := grpc.NewClient(
		serverAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatalf("grpc dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	client := pb.NewMCPToolServiceClient(conn)

	// Attach identity metadata to all outgoing calls.
	md := metadata.Pairs(metadataKeyTenantID, tenantID, metadataKeyClientID, clientID)
	callCtx := metadata.NewOutgoingContext(ctx, md)

	// --- 5. List tools ---
	listResp, err := client.ListTools(callCtx, &pb.ListToolsRequest{})
	if err != nil {
		log.Fatalf("list tools: %v", err)
	}
	fmt.Printf("Tools registered: %d\n", len(listResp.GetTools()))
	for _, t := range listResp.GetTools() {
		fmt.Printf("  - %s (schemas: %d)\n", t.GetId(), len(t.GetSchemas()))
		for _, s := range t.GetSchemas() {
			fmt.Printf("      %s: %s\n", s.GetName(), s.GetDescription())
		}
	}
	fmt.Println()

	// --- 6. Call a tool via gRPC ---
	fmt.Println("--- Calling filesystem tool via gRPC ingress ---")
	args, _ := json.Marshal(map[string]any{"path": root})
	callResp, err := client.CallTool(callCtx, &pb.CallToolRequest{
		ToolName:  "list_directory",
		Arguments: args,
	})
	if err != nil {
		log.Fatalf("call tool: %v", err)
	}

	if callResp.GetIsError() {
		fmt.Println("Tool returned an error:")
		for _, c := range callResp.GetContent() {
			fmt.Printf("  %s\n", c.GetText())
		}
	} else {
		fmt.Printf("Tool call succeeded. Content items: %d\n", len(callResp.GetContent()))
		for _, c := range callResp.GetContent() {
			fmt.Printf("  %s\n", c.GetText())
		}
	}

	// --- 7. Demonstrate streaming (CallToolStream) ---
	fmt.Println("\n--- Calling filesystem tool via gRPC streaming ---")
	stream, err := client.CallToolStream(callCtx, &pb.CallToolStreamRequest{
		ToolName:  "list_directory",
		Arguments: args,
	})
	if err != nil {
		log.Fatalf("call tool stream: %v", err)
	}

	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("stream recv: %v", err)
		}

		switch p := msg.GetPayload().(type) {
		case *pb.CallToolStreamResponse_Progress:
			fmt.Printf("  [progress] %s (%.0f/%.0f)\n", p.Progress.GetMessage(), p.Progress.GetProgress(), p.Progress.GetTotal())
		case *pb.CallToolStreamResponse_Result:
			if p.Result.GetIsError() {
				fmt.Println("  [result] error:")
			} else {
				fmt.Printf("  [result] success, content items: %d\n", len(p.Result.GetContent()))
			}
			for _, c := range p.Result.GetContent() {
				fmt.Printf("    %s\n", c.GetText())
			}
		}
	}
}
