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

package portcullis

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	goaktnats "github.com/tochemey/goakt/v4/discovery/nats"

	"github.com/tochemey/portcullis/internal/naming"
	"github.com/tochemey/portcullis/mcp"
)

// natsDiscoveryProvider adapts GoAkt's NATS discovery provider to the
// mcp.DiscoveryProvider interface the gateway consumes.
type natsDiscoveryProvider struct {
	inner *goaktnats.Discovery
}

var _ mcp.DiscoveryProvider = (*natsDiscoveryProvider)(nil)

func (p *natsDiscoveryProvider) ID() string { return p.inner.ID() }

func (p *natsDiscoveryProvider) Start(context.Context) error {
	if err := p.inner.Initialize(); err != nil {
		return err
	}
	return p.inner.Register()
}

func (p *natsDiscoveryProvider) DiscoverPeers(context.Context) ([]string, error) {
	return p.inner.DiscoverPeers()
}

func (p *natsDiscoveryProvider) Stop(context.Context) error {
	if err := p.inner.Deregister(); err != nil {
		_ = p.inner.Close()
		return err
	}
	return p.inner.Close()
}

// startEmbeddedNATS runs an in-process NATS server on a random port for peer
// discovery, following the pattern GoAkt's own cluster test suites use.
func startEmbeddedNATS(t *testing.T) string {
	t.Helper()
	srv, err := natsserver.NewServer(&natsserver.Options{
		Host:   "127.0.0.1",
		Port:   -1,
		NoLog:  true,
		NoSigs: true,
	})
	require.NoError(t, err)

	go srv.Start()
	require.True(t, srv.ReadyForConnections(5*time.Second), "embedded NATS server did not start")
	t.Cleanup(srv.Shutdown)

	return fmt.Sprintf("nats://%s", srv.Addr().String())
}

// hostIP returns the machine's first non-loopback IPv4 address. GoAkt binds
// its cluster transports on the detected host address, so the discovery
// provider must advertise the same one — advertising 127.0.0.1 while
// memberlist listens on the LAN address leaves every node in a one-member
// cluster.
func hostIP(t *testing.T) string {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	require.NoError(t, err)

	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() && ipNet.IP.To4() != nil {
			return ipNet.IP.String()
		}
	}

	t.Skip("no non-loopback IPv4 address available for cluster test")
	return ""
}

// freePorts reserves n distinct TCP ports by binding and immediately
// releasing ephemeral listeners.
func freePorts(t *testing.T, n int) []int {
	t.Helper()
	ports := make([]int, 0, n)

	for range n {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		ports = append(ports, ln.Addr().(*net.TCPAddr).Port)
		require.NoError(t, ln.Close())
	}
	return ports
}

// startClusterNode creates and starts one gateway node joined to the NATS
// discovery subject. Each node gets its own remoting and gossip ports so the
// three nodes coexist in a single test process.
func startClusterNode(t *testing.T, ctx context.Context, natsAddr string) *Gateway {
	t.Helper()
	ports := freePorts(t, 3)
	remotingPort, peersPort, discoveryPort := ports[0], ports[1], ports[2]

	provider := &natsDiscoveryProvider{inner: goaktnats.NewDiscovery(&goaktnats.Config{
		NatsServer:    natsAddr,
		NatsSubject:   "portcullis-cluster-it",
		Host:          hostIP(t),
		DiscoveryPort: discoveryPort,
	})}

	cfg := mcp.Config{
		Cluster: mcp.ClusterConfig{
			Enabled:           true,
			DiscoveryProvider: provider,
			DiscoveryPort:     discoveryPort,
			PeersPort:         peersPort,
			RemotingPort:      remotingPort,
		},
	}

	gw, err := New(cfg)
	require.NoError(t, err)
	require.NoError(t, gw.Start(ctx))

	return gw
}

// startEchoBackend runs an in-process MCP HTTP backend exposing an echo tool,
// so cluster nodes have a real executor target.
func startEchoBackend(t *testing.T) string {
	t.Helper()
	echoTool := func(_ context.Context, _ *sdkmcp.CallToolRequest, args map[string]any) (*sdkmcp.CallToolResult, any, error) {
		msg := "ok"
		if v, ok := args["message"].(string); ok && v != "" {
			msg = v
		}
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: msg}},
		}, nil, nil
	}

	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "cluster-it", Version: "v0.1.0"}, nil)
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "echo", Description: "echo"}, echoTool)
	handler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server }, nil)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return srv.URL
}

// echoInvocation builds a tools/call invocation against the echo backend tool.
func echoInvocation(toolID mcp.ToolID, requestID string) *mcp.Invocation {
	return &mcp.Invocation{
		ToolID: toolID,
		Method: mcp.MethodToolsCall,
		Params: map[string]any{
			mcp.ParamKeyName:      "echo",
			mcp.ParamKeyArguments: map[string]any{"message": "hello-cluster"},
		},
		Correlation: mcp.CorrelationMeta{
			TenantID:  "tenant-it",
			ClientID:  "client-it",
			RequestID: mcp.RequestID(requestID),
		},
	}
}

// TestClusterWideExecutionPlacement stands up the recommended three-node
// topology (NATS discovery, replica count 2) and verifies the Workstream B
// exit criteria:
//
//  1. Tool metadata registered on one node is visible cluster-wide.
//  2. A request landing on any node executes a tool whose supervisor and
//     warm executor may live on another node.
//  3. Losing the node that hosts a tool's supervisor relocates the
//     supervisor to a survivor and the tool keeps serving (with a reset
//     circuit, the documented v1 trade-off).
func TestClusterWideExecutionPlacement(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping cluster integration test in -short mode")
	}

	ctx := context.Background()
	natsAddr := startEmbeddedNATS(t)
	backendURL := startEchoBackend(t)

	nodes := []*Gateway{
		startClusterNode(t, ctx, natsAddr),
		startClusterNode(t, ctx, natsAddr),
		startClusterNode(t, ctx, natsAddr),
	}
	stopped := make([]bool, len(nodes))

	t.Cleanup(func() {
		for i, node := range nodes {
			if !stopped[i] {
				_ = node.Stop(context.Background())
			}
		}
	})

	// Register three tools once every node has joined, so the registrar's
	// round-robin SpawnOn actually spreads supervisors across members.
	toolIDs := []mcp.ToolID{"cluster-echo-0", "cluster-echo-1", "cluster-echo-2"}

	for _, id := range toolIDs {
		tool := mcp.Tool{
			ID:        id,
			Transport: mcp.TransportHTTP,
			HTTP:      &mcp.HTTPTransportConfig{URL: backendURL},
			State:     mcp.ToolStateEnabled,
		}
		require.NoError(t, nodes[0].RegisterTool(ctx, tool))
	}

	// 1. Metadata is cluster-wide: every node sees every tool through the
	// singleton registrar.
	for i, node := range nodes {
		require.Eventuallyf(t, func() bool {
			tools, err := node.ListTools(ctx)
			return err == nil && len(tools) == len(toolIDs)
		}, 30*time.Second, 500*time.Millisecond, "node %d never saw the full tool catalog", i)
	}

	// 2. Any node executes any tool, wherever its supervisor lives.
	for i, node := range nodes {
		for _, id := range toolIDs {
			require.Eventuallyf(t, func() bool {
				result, err := node.Invoke(ctx, echoInvocation(id, fmt.Sprintf("req-%d-%s", i, id)))
				return err == nil && result != nil && result.Status == mcp.ExecutionStatusSuccess
			}, 30*time.Second, time.Second, "node %d could not execute tool %s", i, id)
		}
	}

	// Find a tool whose supervisor does NOT live on node 0 (the oldest node,
	// which hosts the registrar singleton), so stopping its host exercises
	// relocation without touching the registrar.
	victimNode, victimTool := -1, mcp.ToolID("")

	for _, id := range toolIDs {
		for i := 1; i < len(nodes); i++ {
			pid, err := nodes[i].system.ActorOf(ctx, naming.ToolSupervisorName(id))
			if err == nil && pid != nil && pid.IsLocal() {
				victimNode, victimTool = i, id
				break
			}
		}

		if victimNode > 0 {
			break
		}
	}
	require.Positive(t, victimNode, "round-robin placement left no supervisor off node 0; cannot exercise relocation")

	// 3. Node loss relocates the supervisor and the tool keeps serving.
	require.NoError(t, nodes[victimNode].Stop(context.Background()))
	stopped[victimNode] = true

	survivor := nodes[0]
	require.Eventuallyf(t, func() bool {
		result, err := survivor.Invoke(ctx, echoInvocation(victimTool, "req-relocated"))
		return err == nil && result != nil && result.Status == mcp.ExecutionStatusSuccess
	}, 60*time.Second, time.Second, "tool %s never recovered after losing its supervisor host", victimTool)

	// The relocated supervisor answers admin queries with a fresh (closed)
	// circuit — the documented state-reset trade-off.
	status, err := survivor.GetToolStatus(ctx, victimTool)
	if err == nil {
		assert.Equal(t, mcp.CircuitClosed, status.Circuit)
	}
}
