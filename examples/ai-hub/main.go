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

// Package main runs the portcullis AI Hub example.
//
// This example models a real-world multi-tenant AI tool hub that demonstrates
// the full breadth of the portcullis gateway library in a single runnable
// program.
//
// # Scenario
//
// A platform team runs an AI tool hub that exposes MCP tools to two
// categories of tenant:
//
//   - platform-admin — internal operators with unrestricted access and high
//     rate limits.
//
//   - partner-api — external API partners restricted to read-only tools by a
//     custom PolicyEvaluator.
//
// # Features Covered
//
//   - Egress: stdio tool (filesystem) and optional HTTP tool (everything).
//   - Ingress: Gateway.Handler with a header-based IdentityResolver.
//   - Pluggable policy: partnerPolicyEvaluator denies non-read-only tools.
//   - Credential broker: envAPIKeyProvider injects per-tool API keys from env.
//   - Audit: durable FileSink writes events as NDJSON to disk.
//   - Runtime config: all timeouts, health probe interval.
//   - Telemetry: optional OpenTelemetry metrics and tracing.
//   - Admin API: GetGatewayStatus, GetToolStatus, ListSessions, DrainTool,
//     ResetCircuit.
//   - MCP client: StreamableClientTransport end-to-end through the HTTP
//     ingress.
//
// # Prerequisites
//
//   - Node.js and npx
//   - Optional HTTP tool: npx -y @modelcontextprotocol/server-everything streamableHttp
//
// Run from repo root:  go run ./examples/ai-hub
// Run from anywhere:   go run github.com/tochemey/portcullis/examples/ai-hub
//
// See examples/ai-hub/README.md for environment variables and expected output.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/tochemey/portcullis"
	"github.com/tochemey/portcullis/internal/runtime/audit"
	"github.com/tochemey/portcullis/mcp"
)

// =============================================================================
// Policy
// =============================================================================

// partnerPolicyEvaluator restricts partner-api tenants to tools whose ID
// starts with "read-". Any other tool call is denied with ErrCodePolicyDenied.
// This evaluator is attached to the partner-api TenantConfig and is called
// after all built-in authorization and quota checks pass.
type partnerPolicyEvaluator struct{}

func (e *partnerPolicyEvaluator) Evaluate(_ context.Context, in mcp.PolicyInput) *mcp.RuntimeError {
	if strings.HasPrefix(string(in.ToolID), "read-") {
		return nil
	}
	return mcp.NewRuntimeError(
		mcp.ErrCodePolicyDenied,
		fmt.Sprintf("partner tenant %q may only call read-only tools (prefix \"read-\"); denied tool %q",
			in.TenantID, in.ToolID),
	)
}

// =============================================================================
// Credentials
// =============================================================================

// envAPIKeyProvider reads per-tool API keys from environment variables of the
// form MCP_APIKEY_<TOOL_ID_UPPER>. For example, the key for tool "filesystem"
// is read from MCP_APIKEY_FILESYSTEM. Returns nil credentials when the
// variable is not set (the tool proceeds without injected credentials).
type envAPIKeyProvider struct{}

func (p *envAPIKeyProvider) ID() string { return "env-api-key" }

func (p *envAPIKeyProvider) ResolveCredentials(_ context.Context, _ mcp.TenantID, toolID mcp.ToolID) (*mcp.Credentials, error) {
	envKey := "MCP_APIKEY_" + strings.ToUpper(strings.ReplaceAll(string(toolID), "-", "_"))
	apiKey := os.Getenv(envKey)
	if apiKey == "" {
		return nil, nil
	}
	return &mcp.Credentials{
		ToolID: toolID,
		Values: map[string]string{"api_key": apiKey},
	}, nil
}

// =============================================================================
// Ingress identity
// =============================================================================

// headerIdentityResolver extracts tenant and client identity from HTTP request
// headers. X-Tenant-ID and X-Client-ID must be present; a missing X-Tenant-ID
// rejects the session with HTTP 400.
type headerIdentityResolver struct{}

func (r *headerIdentityResolver) ResolveIdentity(req *http.Request) (mcp.TenantID, mcp.ClientID, error) {
	tenant := mcp.TenantID(req.Header.Get("X-Tenant-ID"))
	if tenant.IsZero() {
		return "", "", errors.New("missing X-Tenant-ID header")
	}
	return tenant, mcp.ClientID(req.Header.Get("X-Client-ID")), nil
}

// headerInjectTransport wraps an http.RoundTripper and injects the given
// identity headers on every outgoing request so the headerIdentityResolver
// can authenticate the MCP client.
type headerInjectTransport struct {
	tenantID string
	clientID string
	base     http.RoundTripper
}

func (t *headerInjectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("X-Tenant-ID", t.tenantID)
	clone.Header.Set("X-Client-ID", t.clientID)
	return t.base.RoundTrip(clone)
}

// =============================================================================
// Helpers
// =============================================================================

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func printSection(title string) {
	fmt.Printf("\n%s\n%s\n", title, strings.Repeat("─", len(title)))
}

// =============================================================================
// main
// =============================================================================

func main() {
	// ── Environment ──────────────────────────────────────────────────────────
	root := getenv("MCP_FS_ROOT", ".")
	httpToolURL := getenv("MCP_HTTP_URL", "http://localhost:3001/mcp")
	auditDir := getenv("MCP_AUDIT_DIR", filepath.Join(os.TempDir(), "portcullis-ai-hub"))
	mcpAddr := getenv("MCP_ADDR", "127.0.0.1:0")
	otlpEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")

	fmt.Println("╔══════════════════════════════════════╗")
	fmt.Println("║   portcullis  ·  AI Tool Hub Example  ║")
	fmt.Println("╚══════════════════════════════════════╝")
	fmt.Printf("  Filesystem root : %s\n", root)
	fmt.Printf("  HTTP tool URL   : %s\n", httpToolURL)
	fmt.Printf("  Audit dir       : %s\n", auditDir)
	if otlpEndpoint != "" {
		fmt.Printf("  OTLP endpoint   : %s\n", otlpEndpoint)
	}

	ctx := context.Background()

	fileSink, err := audit.NewFileSink(auditDir)
	if err != nil {
		log.Fatalf("create audit sink: %v", err)
	}

	gw := startGateway(ctx, fileSink, root, httpToolURL, otlpEndpoint)
	defer func() {
		if err := gw.Stop(ctx); err != nil {
			log.Printf("stop gateway: %v", err)
		}
		if err := fileSink.Close(); err != nil {
			log.Printf("close audit sink: %v", err)
		}
	}()

	printSection("A. Admin API — startup snapshot")
	runAdminStartupSnapshot(ctx, gw)

	serverURL := startIngressServer(ctx, gw, mcpAddr)

	printSection("C. Egress — direct invocation (platform-admin)")
	runEgressInvocations(ctx, gw, root, httpToolURL)

	printSection("D. Ingress — MCP client over HTTP (platform-admin)")
	runIngressClient(ctx, serverURL, root)

	printSection("E. Pluggable Policy — partner-api evaluation")
	runPolicyDemo(ctx, gw, root)

	printSection("F. Admin API — operational control")
	runAdminControl(ctx, gw)

	printSection("G. Audit log (last 15 events)")
	// Stop the gateway so the file sink flushes all events before we read it.
	if err := gw.Stop(ctx); err != nil {
		log.Printf("stop gateway: %v", err)
	}
	if err := fileSink.Close(); err != nil {
		log.Printf("close audit sink: %v", err)
	}
	showAuditLog(auditDir)

	fmt.Println("\n╔══════════════════════════════╗")
	fmt.Println("║   AI Hub example complete    ║")
	fmt.Println("╚══════════════════════════════╝")
}

func startGateway(ctx context.Context, fileSink *audit.FileSink, root, httpToolURL, otlpEndpoint string) *portcullis.Gateway {
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
	if httpToolURL != "" {
		tools = append(tools, mcp.Tool{
			ID:        "everything",
			Transport: mcp.TransportHTTP,
			HTTP:      &mcp.HTTPTransportConfig{URL: httpToolURL},
			State:     mcp.ToolStateEnabled,
		})
	}

	cfg := mcp.Config{
		Runtime: mcp.RuntimeConfig{
			SessionIdleTimeout:  5 * time.Minute,
			RequestTimeout:      30 * time.Second,
			StartupTimeout:      10 * time.Second,
			HealthProbeInterval: 30 * time.Second,
			ShutdownTimeout:     30 * time.Second,
		},
		Telemetry:   mcp.TelemetryConfig{OTLPEndpoint: otlpEndpoint},
		Audit:       mcp.AuditConfig{Sink: fileSink},
		Credentials: mcp.CredentialsConfig{Providers: []mcp.CredentialsProvider{&envAPIKeyProvider{}}, CacheTTL: 5 * time.Minute},
		Tenants: []mcp.TenantConfig{
			{ID: "platform-admin", Quotas: mcp.TenantQuotaConfig{RequestsPerMinute: 120, ConcurrentSessions: 20}},
			{ID: "partner-api", Quotas: mcp.TenantQuotaConfig{RequestsPerMinute: 30, ConcurrentSessions: 5}, Evaluator: &partnerPolicyEvaluator{}},
		},
		HealthProbe: mcp.HealthProbeConfig{Interval: 30 * time.Second},
		Tools:       tools,
	}

	gw, err := portcullis.New(cfg,
		portcullis.WithMetrics(),
		portcullis.WithTracing(),
	)
	if err != nil {
		log.Fatalf("create gateway: %v", err)
	}
	if err := gw.Start(ctx); err != nil {
		log.Fatalf("start gateway: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	return gw
}

func runAdminStartupSnapshot(ctx context.Context, gw *portcullis.Gateway) {
	gwStatus, err := gw.GetGatewayStatus(ctx)
	if err != nil {
		log.Fatalf("GetGatewayStatus: %v", err)
	}
	fmt.Printf("Gateway: running=%-5v  tools=%d  sessions=%d\n",
		gwStatus.Running, gwStatus.ToolCount, gwStatus.SessionCount)

	registeredTools, err := gw.ListTools(ctx)
	if err != nil {
		log.Fatalf("ListTools: %v", err)
	}
	for _, t := range registeredTools {
		status, err := gw.GetToolStatus(ctx, t.ID)
		if err != nil {
			log.Printf("GetToolStatus %s: %v", t.ID, err)
			continue
		}
		fmt.Printf("  tool=%-14s transport=%-5s state=%-8s circuit=%-9s draining=%v\n",
			t.ID, t.Transport, status.State, status.Circuit, status.Draining)
	}

	sessions, err := gw.ListSessions(ctx)
	if err != nil {
		log.Fatalf("ListSessions: %v", err)
	}
	fmt.Printf("Active sessions: %d\n", len(sessions))
}

func startIngressServer(ctx context.Context, gw *portcullis.Gateway, mcpAddr string) string {
	printSection("B. Ingress — MCP Streamable HTTP server")

	h, err := gw.Handler(mcp.IngressConfig{
		IdentityResolver: &headerIdentityResolver{},
	})
	if err != nil {
		log.Fatalf("create handler: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/mcp", h)

	ln, err := net.Listen("tcp", mcpAddr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	serverURL := "http://" + ln.Addr().String() + "/mcp"

	httpSrv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("http server: %v", err)
		}
	}()
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutCtx)
	}()

	fmt.Printf("MCP endpoint: %s\n", serverURL)
	fmt.Println("IdentityResolver: reads X-Tenant-ID and X-Client-ID headers")
	return serverURL
}

func runEgressInvocations(ctx context.Context, gw *portcullis.Gateway, root, httpToolURL string) {
	fsResult, err := gw.Invoke(ctx, &mcp.Invocation{
		ToolID: "filesystem",
		Method: "tools/call",
		Params: map[string]any{"name": "list_directory", "arguments": map[string]any{"path": root}},
		Correlation: mcp.CorrelationMeta{
			TenantID: "platform-admin", ClientID: "admin-cli", RequestID: "hub-req-1",
		},
	})
	if err != nil {
		fmt.Printf("filesystem (direct): error — %v\n", err)
	} else {
		fmt.Printf("filesystem (direct): status=%-8s  duration=%v\n", fsResult.Status, fsResult.Duration)
		if fsResult.Err != nil {
			fmt.Printf("  tool error: %s\n", fsResult.Err.Message)
		}
	}

	if httpToolURL == "" {
		return
	}
	evResult, err := gw.Invoke(ctx, &mcp.Invocation{
		ToolID: "everything",
		Method: "tools/call",
		Params: map[string]any{"name": "get-sum", "arguments": map[string]any{"a": 7, "b": 3}},
		Correlation: mcp.CorrelationMeta{
			TenantID: "platform-admin", ClientID: "admin-cli", RequestID: "hub-req-2",
		},
	})
	if err != nil {
		fmt.Printf("everything  (direct): error — %v (is server-everything running?)\n", err)
		return
	}
	fmt.Printf("everything  (direct): status=%-8s  duration=%v\n", evResult.Status, evResult.Duration)
	if evResult.Err != nil {
		fmt.Printf("  tool error: %s\n", evResult.Err.Message)
	} else if len(evResult.Output) > 0 {
		pretty, _ := json.MarshalIndent(evResult.Output, "    ", "  ")
		fmt.Printf("  output: %s\n", pretty)
	}
}

func runIngressClient(ctx context.Context, serverURL, root string) {
	mcpClient := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "ai-hub-example", Version: "0.1"}, nil)
	transport := &sdkmcp.StreamableClientTransport{
		Endpoint: serverURL,
		HTTPClient: &http.Client{
			Transport: &headerInjectTransport{
				tenantID: "platform-admin",
				clientID: "ingress-client",
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

	listResult, err := session.ListTools(ctx, &sdkmcp.ListToolsParams{})
	if err != nil {
		log.Printf("list tools via ingress: %v", err)
	} else {
		fmt.Printf("Session tools: %d\n", len(listResult.Tools))
		for _, t := range listResult.Tools {
			fmt.Printf("  - %s\n", t.Name)
		}
	}

	callResult, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "filesystem",
		Arguments: map[string]any{"name": "list_directory", "arguments": map[string]any{"path": root}},
	})
	if err != nil {
		fmt.Printf("filesystem (ingress): error — %v\n", err)
	} else if callResult.IsError {
		fmt.Println("filesystem (ingress): tool error")
		for _, c := range callResult.Content {
			if txt, ok := c.(*sdkmcp.TextContent); ok {
				fmt.Printf("  %s\n", txt.Text)
			}
		}
	} else {
		fmt.Printf("filesystem (ingress): succeeded, content items=%d\n", len(callResult.Content))
	}
}

func runPolicyDemo(ctx context.Context, gw *portcullis.Gateway, root string) {
	fmt.Println("Invoking filesystem as partner-api (evaluator should deny: no read- prefix) ...")
	partnerResult, err := gw.Invoke(ctx, &mcp.Invocation{
		ToolID: "filesystem",
		Method: "tools/call",
		Params: map[string]any{"name": "list_directory", "arguments": map[string]any{"path": root}},
		Correlation: mcp.CorrelationMeta{
			TenantID: "partner-api", ClientID: "partner-client", RequestID: "hub-req-3",
		},
	})
	switch {
	case err != nil:
		var rErr *mcp.RuntimeError
		if errors.As(err, &rErr) {
			fmt.Printf("  DENIED (%s): %s\n", rErr.Code, rErr.Message)
		} else {
			fmt.Printf("  error: %v\n", err)
		}
	case partnerResult != nil && partnerResult.Err != nil:
		fmt.Printf("  DENIED (%s): %s\n", partnerResult.Err.Code, partnerResult.Err.Message)
	default:
		fmt.Printf("  ALLOWED (status=%s) — policy evaluator did not deny\n", partnerResult.Status)
	}
}

func runAdminControl(ctx context.Context, gw *portcullis.Gateway) {
	gwStatus, err := gw.GetGatewayStatus(ctx)
	if err != nil {
		log.Fatalf("GetGatewayStatus: %v", err)
	}
	fmt.Printf("Gateway: tools=%d  sessions=%d\n", gwStatus.ToolCount, gwStatus.SessionCount)

	sessions, err := gw.ListSessions(ctx)
	if err != nil {
		log.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) > 0 {
		fmt.Printf("Active sessions (%d):\n", len(sessions))
		for _, s := range sessions {
			fmt.Printf("  name=%-40s  tenant=%-16s  tool=%s\n", s.Name, s.TenantID, s.ToolID)
		}
	} else {
		fmt.Println("Active sessions: 0 (sessions were already closed)")
	}

	fmt.Println("\nDraining tool \"filesystem\" ...")
	if err := gw.DrainTool(ctx, "filesystem"); err != nil {
		log.Printf("DrainTool: %v", err)
	} else if status, _ := gw.GetToolStatus(ctx, "filesystem"); status != nil {
		fmt.Printf("  filesystem: draining=%-5v  circuit=%s\n", status.Draining, status.Circuit)
	}

	fmt.Println("\nResetting circuit for \"filesystem\" ...")
	if err := gw.ResetCircuit(ctx, "filesystem"); err != nil {
		log.Printf("ResetCircuit: %v", err)
	} else if status, _ := gw.GetToolStatus(ctx, "filesystem"); status != nil {
		fmt.Printf("  filesystem: draining=%-5v  circuit=%s\n", status.Draining, status.Circuit)
	}
}

func showAuditLog(auditDir string) {
	auditPath := filepath.Join(auditDir, "audit.log")
	f, err := os.Open(auditPath)
	if err != nil {
		fmt.Printf("  (audit log not available: %v)\n", err)
		return
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	start := 0
	if len(lines) > 15 {
		start = len(lines) - 15
	}
	for i := start; i < len(lines); i++ {
		var ev mcp.AuditEvent
		if err := json.Unmarshal([]byte(lines[i]), &ev); err != nil {
			fmt.Printf("  [parse error] %s\n", lines[i])
			continue
		}
		fmt.Printf("  %-26s  %-28s  tenant=%-16s  tool=%-12s  outcome=%s\n",
			ev.Timestamp.Format(time.RFC3339),
			ev.Type, ev.TenantID, ev.ToolID, ev.Outcome)
	}
}
