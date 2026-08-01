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

// Package main runs the mcgate full-config example.
//
// This example demonstrates the majority of the gateway configuration:
//   - Runtime: session idle timeout, request timeout, startup/shutdown timeouts, health probe interval
//   - Telemetry: OpenTelemetry OTLP endpoint (optional)
//   - Audit: durable FileSink for invocation events
//   - Credentials: pluggable credential provider (env-based example)
//   - Tenants: per-tenant quotas (requests per minute, concurrent sessions)
//   - HealthProbe: health probe interval
//   - Gateway options: WithMetrics(), WithTracing()
//   - Tools: mixed stdio and HTTP transports
//
// Prerequisites:
//   - Node.js and npx
//   - For HTTP tool: npx -y @modelcontextprotocol/server-everything streamableHttp
//
// Run from repo root:  go run ./examples/full-config
// Run from anywhere:   go run github.com/tochemey/mcgate/examples/full-config
//
// See examples/full-config/README.md for a full walkthrough.
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

	"github.com/tochemey/mcgate"
	"github.com/tochemey/mcgate/internal/runtime/audit"
	"github.com/tochemey/mcgate/mcp"
)

// envCredProvider is a simple credentials provider that reads from environment variables.
// For demo purposes it returns credentials when MCP_DEMO_API_KEY is set.
type envCredProvider struct{}

func (e *envCredProvider) ID() string { return "env" }

func (e *envCredProvider) ResolveCredentials(_ context.Context, tenantID mcp.TenantID, toolID mcp.ToolID) (*mcp.Credentials, error) {
	apiKey := os.Getenv("MCP_DEMO_API_KEY")
	if apiKey == "" {
		return nil, nil
	}
	return &mcp.Credentials{
		TenantID: tenantID,
		ToolID:   toolID,
		Values:   map[string]string{"api_key": apiKey},
	}, nil
}

func main() {
	// --- 1. Configure paths and URLs from environment ---
	root := os.Getenv("MCP_FS_ROOT")
	if root == "" {
		root = "."
	}
	httpURL := os.Getenv("MCP_HTTP_URL")
	if httpURL == "" {
		httpURL = "http://localhost:3001/mcp"
	}
	auditDir := os.Getenv("MCP_AUDIT_DIR")
	if auditDir == "" {
		auditDir = filepath.Join(os.TempDir(), "mcgate-audit")
	}
	otlpEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")

	// --- 2. Create durable audit sink ---
	fileSink, err := audit.NewFileSink(auditDir)
	if err != nil {
		log.Fatalf("create audit sink: %v", err)
	}

	// --- 3. Build tools: stdio + optional HTTP ---
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

	// --- 4. Build comprehensive config ---
	config := mcp.Config{
		// Runtime: core tuning parameters
		Runtime: mcp.RuntimeConfig{
			SessionIdleTimeout:  5 * time.Minute,
			RequestTimeout:      30 * time.Second,
			StartupTimeout:      10 * time.Second,
			HealthProbeInterval: 30 * time.Second,
			ShutdownTimeout:     30 * time.Second,
		},

		// Cluster: multi-node mode (disabled for this example; structure shown for reference)
		// Cluster: mcp.ClusterConfig{
		// 	Enabled:       true,
		// 	Discovery:     "kubernetes", // or "dnssd"
		// 	SingletonRole: "gateway",
		// 	PeersPort:     7946,
		// 	RemotingPort:  8555,
		// 	Kubernetes: mcp.KubernetesDiscoveryConfig{
		// 		Namespace:         "default",
		// 		DiscoveryPortName: "discovery",
		// 		RemotingPortName:  "remoting",
		// 		PeersPortName:     "peers",
		// 		PodLabels:         map[string]string{"app": "mcgate"},
		// 	},
		// },

		// Telemetry: OpenTelemetry OTLP export
		Telemetry: mcp.TelemetryConfig{
			OTLPEndpoint: otlpEndpoint,
		},

		// Audit: durable sink for invocation events
		Audit: mcp.AuditConfig{Sink: fileSink},

		// Credentials: pluggable providers (env-based example)
		Credentials: mcp.CredentialsConfig{
			Providers: []mcp.CredentialsProvider{&envCredProvider{}},
			CacheTTL:  5 * time.Minute,
		},

		// Tenants: per-tenant quotas for policy and rate limiting
		Tenants: []mcp.TenantConfig{
			{
				ID: "example-tenant",
				Quotas: mcp.TenantQuotaConfig{
					RequestsPerMinute:  60,
					ConcurrentSessions: 10,
				},
			},
		},

		// HealthProbe: health actor probe interval
		HealthProbe: mcp.HealthProbeConfig{
			Interval: 30 * time.Second,
		},

		Tools: tools,
	}

	// --- 5. Create gateway with options (metrics + tracing) ---
	gw, err := mcgate.New(config,
		mcgate.WithMetrics(),
		mcgate.WithTracing(),
	)
	if err != nil {
		log.Fatalf("create gateway: %v", err)
	}

	ctx := context.Background()
	if err := gw.Start(ctx); err != nil {
		log.Fatalf("start gateway: %v", err)
	}

	time.Sleep(200 * time.Millisecond)
	defer func() {
		if err := gw.Stop(ctx); err != nil {
			log.Printf("stop gateway: %v", err)
		}
	}()

	fmt.Println("=== mcgate full-config example ===")
	fmt.Printf("Runtime: session_idle=%v, request=%v, startup=%v\n",
		config.Runtime.SessionIdleTimeout, config.Runtime.RequestTimeout, config.Runtime.StartupTimeout)
	fmt.Printf("Audit: %s/audit.log\n", auditDir)
	fmt.Printf("Tenants: %d (quotas: rpm=%d, concurrent=%d)\n",
		len(config.Tenants),
		config.Tenants[0].Quotas.RequestsPerMinute,
		config.Tenants[0].Quotas.ConcurrentSessions)
	if otlpEndpoint != "" {
		fmt.Printf("Telemetry: OTLP endpoint=%s\n", otlpEndpoint)
	}
	fmt.Printf("Filesystem root: %s\n", root)
	fmt.Printf("HTTP tool URL: %s\n\n", httpURL)

	// --- 6. List tools ---
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
		TenantID:  "example-tenant",
		ClientID:  "full-config-client",
		RequestID: "req-1",
	}

	// --- 7. Invoke filesystem (stdio) ---
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

	// --- 8. Invoke everything (HTTP) if registered ---
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

	// --- 9. Stop and flush audit ---
	if err := gw.Stop(ctx); err != nil {
		log.Printf("stop gateway: %v", err)
	}
	if err := fileSink.Close(); err != nil {
		log.Printf("close audit sink: %v", err)
	}

	fmt.Println("--- Audit log (last 10 events) ---")
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
	if len(lines) > 10 {
		start = len(lines) - 10
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
