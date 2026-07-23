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

// Package main runs the portcullis AI Hub cluster example.
//
// This example is the cluster-mode counterpart to examples/ai-hub. It runs the
// same full feature set — multi-tenancy, pluggable policy, credential broker,
// durable audit, OpenTelemetry — but as a persistent long-running server
// deployed across a three-node GoAkt actor cluster on Kubernetes.
//
// # Scenario
//
// A platform team runs an AI tool hub that exposes MCP tools to two categories
// of tenant:
//
//   - platform-admin — internal operators with unrestricted access and high
//     rate limits.
//
//   - partner-api — external API partners restricted to read-only tools by a
//     custom PolicyEvaluator. Any tool whose ID does not start with "read-" is
//     denied.
//
// # Features Covered
//
//   - Cluster mode: three gateway pods form a GoAkt actor cluster via
//     Kubernetes discovery (see discovery.go). Tool registry and session actors
//     are distributed across all nodes.
//   - Egress: optional HTTP tool (MCP_TOOL_URL) and optional stdio tool
//     (MCP_FS_ROOT).
//   - Ingress: MCP Streamable HTTP on /mcp with header-based identity
//     resolution (X-Tenant-ID / X-Client-ID).
//   - Pluggable policy: partnerPolicyEvaluator denies non-read-only tools for
//     the partner-api tenant.
//   - Credential broker: envAPIKeyProvider injects per-tool API keys from
//     environment variables (MCP_APIKEY_<TOOL_ID_UPPER>).
//   - Audit: durable FileSink writes NDJSON events to MCP_AUDIT_DIR.
//   - Telemetry: metrics always enabled; tracing enabled when
//     OTEL_EXPORTER_OTLP_ENDPOINT is set.
//   - Admin snapshot: gateway status and registered tools logged at startup.
//   - Health probe: /health HTTP endpoint for Kubernetes liveness/readiness.
//   - Graceful shutdown: gateway drains cleanly on SIGINT/SIGTERM.
//
// # Quick Start (Kubernetes via Kind)
//
//	make cluster-create   # Create Kind cluster (one-time)
//	make image            # Build and load Docker image into Kind
//	make cluster-up       # Deploy gateway cluster, tool server, and nginx
//	make port-forward     # Expose nginx on localhost:8080
//	make test             # Send test tool invocations
//	make cluster-down     # Tear down deployments
//
// # Local Single-Node Run (cluster disabled by default)
//
//	MCP_TOOL_URL=http://localhost:3001/mcp go run ./examples/cluster
//
// See examples/cluster/README.md for full documentation.
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/tochemey/portcullis"
	"github.com/tochemey/portcullis/internal/runtime/audit"
	"github.com/tochemey/portcullis/mcp"
)

// =============================================================================
// Policy
// =============================================================================

// partnerPolicyEvaluator restricts the partner-api tenant to tools whose ID
// starts with "read-". All other tool calls are denied with ErrCodePolicyDenied.
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
// form MCP_APIKEY_<TOOL_ID_UPPER>. Returns nil when the variable is not set.
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

// headerIdentityResolver extracts tenant and client identity from HTTP headers.
// X-Tenant-ID is required; a missing header returns an error so that unknown
// callers are rejected rather than silently attributed to a default tenant.
type headerIdentityResolver struct{}

func (r *headerIdentityResolver) ResolveIdentity(req *http.Request) (mcp.TenantID, mcp.ClientID, error) {
	tenant := mcp.TenantID(req.Header.Get("X-Tenant-ID"))
	if tenant.IsZero() {
		return "", "", errors.New("missing X-Tenant-ID header")
	}
	return tenant, mcp.ClientID(req.Header.Get("X-Client-ID")), nil
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

func getenvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

// =============================================================================
// main / run
// =============================================================================

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	// ── Configuration from environment ──────────────────────────────────────
	httpAddr := getenv("HTTP_ADDR", ":8080")
	clusterEnabled := getenv("CLUSTER_ENABLED", "false") == "true"
	namespace := getenv("NAMESPACE", "default")
	discoveryPortName := getenv("DISCOVERY_PORT_NAME", "discovery-port")
	peersPortName := getenv("PEERS_PORT_NAME", "peers-port")
	remotingPortName := getenv("REMOTING_PORT_NAME", "remoting-port")
	discoveryPort := getenvInt("DISCOVERY_PORT", 3322)
	peersPort := getenvInt("PEERS_PORT", 3320)
	remotingPort := getenvInt("REMOTING_PORT", 3321)
	fsRoot := os.Getenv("MCP_FS_ROOT")
	httpToolURL := os.Getenv("MCP_TOOL_URL")
	auditDir := getenv("MCP_AUDIT_DIR", filepath.Join(os.TempDir(), "portcullis-cluster"))
	otlpEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	debug := getenv("DEBUG", "false") == "true"

	// Pod labels used by KubernetesDiscovery to filter peers via label selector.
	// Must match the labels set on the StatefulSet pod template in k8s/k8s.yaml.
	podLabels := map[string]string{
		"app.kubernetes.io/name":      getenv("POD_LABEL_NAME", "gateway"),
		"app.kubernetes.io/component": getenv("POD_LABEL_COMPONENT", "MCPGateway"),
		"app.kubernetes.io/part-of":   getenv("POD_LABEL_PART_OF", "portcullis-cluster"),
	}

	fmt.Println("╔══════════════════════════════════════════════════╗")
	fmt.Println("║  portcullis  ·  AI Hub Cluster Example (k8s)      ║")
	fmt.Println("╚══════════════════════════════════════════════════╝")
	fmt.Printf("  Cluster enabled  : %v\n", clusterEnabled)
	if clusterEnabled {
		fmt.Printf("  Namespace        : %s\n", namespace)
		fmt.Printf("  Pod labels       : name=%s component=%s\n",
			podLabels["app.kubernetes.io/name"], podLabels["app.kubernetes.io/component"])
		fmt.Printf("  Discovery port   : %d (%s)\n", discoveryPort, discoveryPortName)
		fmt.Printf("  Peers port       : %d (%s)\n", peersPort, peersPortName)
		fmt.Printf("  Remoting port    : %d (%s)\n", remotingPort, remotingPortName)
	}
	fmt.Printf("  HTTP addr        : %s\n", httpAddr)
	fmt.Printf("  Debug            : %v\n", debug)
	fmt.Printf("  Audit dir        : %s\n", auditDir)
	if fsRoot != "" {
		fmt.Printf("  Filesystem root  : %s\n", fsRoot)
	}
	if httpToolURL != "" {
		fmt.Printf("  HTTP tool URL    : %s\n", httpToolURL)
	}
	if otlpEndpoint != "" {
		fmt.Printf("  OTLP endpoint    : %s\n", otlpEndpoint)
	}

	// ── Audit sink ───────────────────────────────────────────────────────────
	fileSink, err := audit.NewFileSink(auditDir)
	if err != nil {
		return fmt.Errorf("create audit sink: %w", err)
	}
	defer func() {
		if err := fileSink.Close(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "close audit sink: %v\n", err)
		}
	}()

	// ── Tools ────────────────────────────────────────────────────────────────
	var tools []mcp.Tool
	if fsRoot != "" {
		tools = append(tools, mcp.Tool{
			ID:        "filesystem",
			Transport: mcp.TransportStdio,
			Stdio: &mcp.StdioTransportConfig{
				Command: "npx",
				Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", fsRoot},
			},
			State: mcp.ToolStateEnabled,
		})
	}
	if httpToolURL != "" {
		tools = append(tools, mcp.Tool{
			ID:        "everything",
			Transport: mcp.TransportHTTP,
			HTTP:      &mcp.HTTPTransportConfig{URL: httpToolURL},
			State:     mcp.ToolStateEnabled,
		})
	}

	// ── Cluster config ───────────────────────────────────────────────────────
	var clusterCfg mcp.ClusterConfig
	if clusterEnabled {
		clusterCfg = mcp.ClusterConfig{
			Enabled: true,
			DiscoveryProvider: NewKubernetesDiscovery(
				namespace,
				discoveryPortName,
				peersPortName,
				remotingPortName,
				podLabels,
			),
			DiscoveryPort: discoveryPort,
			PeersPort:     peersPort,
			RemotingPort:  remotingPort,
		}
	}

	// ── Gateway config ────────────────────────────────────────────────────────
	cfg := mcp.Config{
		Runtime: mcp.RuntimeConfig{
			SessionIdleTimeout:  5 * time.Minute,
			RequestTimeout:      30 * time.Second,
			StartupTimeout:      15 * time.Second,
			HealthProbeInterval: 30 * time.Second,
			ShutdownTimeout:     30 * time.Second,
		},
		Cluster:   clusterCfg,
		Telemetry: mcp.TelemetryConfig{OTLPEndpoint: otlpEndpoint},
		Audit:     mcp.AuditConfig{Sink: fileSink},
		Credentials: mcp.CredentialsConfig{
			Providers: []mcp.CredentialsProvider{&envAPIKeyProvider{}},
			CacheTTL:  5 * time.Minute,
		},
		Tenants: []mcp.TenantConfig{
			{
				ID: "platform-admin",
				Quotas: mcp.TenantQuotaConfig{
					RequestsPerMinute:  120,
					ConcurrentSessions: 20,
				},
			},
			{
				ID: "partner-api",
				Quotas: mcp.TenantQuotaConfig{
					RequestsPerMinute:  30,
					ConcurrentSessions: 5,
				},
				Evaluator: &partnerPolicyEvaluator{},
			},
		},
		HealthProbe: mcp.HealthProbeConfig{Interval: 30 * time.Second},
		Tools:       tools,
	}

	opts := []portcullis.Option{portcullis.WithMetrics()}
	if debug {
		opts = append(opts, portcullis.WithDebug())
	}
	if otlpEndpoint != "" {
		opts = append(opts, portcullis.WithTracing())
	}

	gw, err := portcullis.New(cfg, opts...)
	if err != nil {
		return fmt.Errorf("create gateway: %w", err)
	}
	if err := gw.Start(ctx); err != nil {
		return fmt.Errorf("start gateway: %w", err)
	}
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if err := gw.Stop(shutCtx); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "stop gateway: %v\n", err)
		}
	}()

	// ── Admin startup snapshot ───────────────────────────────────────────────
	logStartupSnapshot(ctx, gw)

	// ── HTTP ingress ─────────────────────────────────────────────────────────
	ingressHandler, err := gw.Handler(mcp.IngressConfig{
		IdentityResolver: &headerIdentityResolver{},
	})
	if err != nil {
		return fmt.Errorf("create ingress handler: %w", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/mcp", ingressHandler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	ln, err := net.Listen("tcp", httpAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", httpAddr, err)
	}

	httpSrv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	srvErr := make(chan error, 1)
	go func() {
		fmt.Printf("MCP endpoint : http://%s/mcp\n", ln.Addr())
		fmt.Printf("Health check : http://%s/health\n", ln.Addr())
		if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			srvErr <- err
		}
	}()

	select {
	case <-ctx.Done():
		fmt.Println("Shutting down...")
	case err := <-srvErr:
		return fmt.Errorf("http server: %w", err)
	}

	shutCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return httpSrv.Shutdown(shutCtx)
}

// logStartupSnapshot prints a brief admin status summary at startup so that
// pod logs immediately show which tools and tenants are active.
func logStartupSnapshot(ctx context.Context, gw *portcullis.Gateway) {
	gwStatus, err := gw.GetGatewayStatus(ctx)
	if err != nil {
		fmt.Printf("admin snapshot: GetGatewayStatus: %v\n", err)
		return
	}
	fmt.Printf("Gateway: running=%-5v  tools=%d  sessions=%d\n",
		gwStatus.Running, gwStatus.ToolCount, gwStatus.SessionCount)

	registeredTools, err := gw.ListTools(ctx)
	if err != nil {
		fmt.Printf("admin snapshot: ListTools: %v\n", err)
		return
	}
	for _, t := range registeredTools {
		status, err := gw.GetToolStatus(ctx, t.ID)
		if err != nil {
			fmt.Printf("  tool=%-14s  (status unavailable: %v)\n", t.ID, err)
			continue
		}
		fmt.Printf("  tool=%-14s  transport=%-5s  state=%-8s  circuit=%-9s  draining=%v\n",
			t.ID, t.Transport, status.State, status.Circuit, status.Draining)
	}
}
