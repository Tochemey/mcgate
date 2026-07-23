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

package actor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	goaktactor "github.com/tochemey/goakt/v4/actor"
	goaktlog "github.com/tochemey/goakt/v4/log"
	goaktsupervisor "github.com/tochemey/goakt/v4/supervisor"
	"github.com/tochemey/goakt/v4/testkit"

	"github.com/tochemey/goakt-mcp/mcp"

	"github.com/tochemey/goakt-mcp/internal/naming"
	actorextension "github.com/tochemey/goakt-mcp/internal/runtime/actor/extension"
	"github.com/tochemey/goakt-mcp/internal/runtime/audit"
)

// askTimeout is the default timeout for Ask calls in tests.
const askTimeout = 5 * time.Second

// validStdioTool returns a valid stdio tool for use in tests.
func validStdioTool(id mcp.ToolID) mcp.Tool {
	return mcp.Tool{
		ID:        id,
		Transport: mcp.TransportStdio,
		Stdio:     &mcp.StdioTransportConfig{Command: "npx"},
		State:     mcp.ToolStateEnabled,
	}
}

// validHTTPTool returns a valid HTTP tool for use in tests.
func validHTTPTool(id mcp.ToolID) mcp.Tool {
	return mcp.Tool{
		ID:        id,
		Transport: mcp.TransportHTTP,
		HTTP:      &mcp.HTTPTransportConfig{URL: "http://localhost:8080"},
		State:     mcp.ToolStateEnabled,
	}
}

// testActorSystem creates and starts a minimal GoAkt actor system for use in tests.
// The returned stop function must be called to clean up the system after the test.
// Uses the discard logger to suppress all log output during tests.
func testActorSystem(t *testing.T, extras ...goaktactor.Option) (goaktactor.ActorSystem, func()) {
	t.Helper()
	ctx := context.Background()
	opts := []goaktactor.Option{
		goaktactor.WithLogger(goaktlog.DiscardLogger),
	}
	opts = append(opts, extras...)
	system, err := goaktactor.NewActorSystem("test-goakt-mcp", opts...)
	require.NoError(t, err)
	require.NoError(t, system.Start(ctx))

	stop := func() {
		require.NoError(t, system.Stop(ctx))
	}
	return system, stop
}

// testActorSystemWithTools creates and starts an actor system pre-loaded with a
// ToolConfigExtension containing the given tools and ConfigExtension for journal/health.
// Convenience wrapper over testActorSystem for supervisor tests.
func testActorSystemWithTools(t *testing.T, tools ...mcp.Tool) (goaktactor.ActorSystem, func()) {
	t.Helper()
	toolCfgExt := actorextension.NewToolConfigExtension()
	for _, tool := range tools {
		toolCfgExt.Register(tool)
	}
	cfg := testConfig()
	cfg.Audit.Sink = audit.NewMemorySink()
	return testActorSystem(t, goaktactor.WithExtensions(toolCfgExt, actorextension.NewConfigExtension(cfg)))
}

// testConfig returns a minimal Config suitable for use in tests.
func testConfig() mcp.Config {
	return mcp.Config{
		Runtime: mcp.RuntimeConfig{
			SessionIdleTimeout: mcp.DefaultSessionIdleTimeout,
			RequestTimeout:     mcp.DefaultRequestTimeout,
			StartupTimeout:     mcp.DefaultStartupTimeout,
		},
		Credentials: mcp.CredentialsConfig{
			Providers: []mcp.CredentialsProvider{&mockCredentialProvider{creds: map[string]string{"api-key": "test-secret"}}},
			CacheTTL:  DefaultCredentialTTL,
		},
	}
}

// testConfigWithTenants returns a Config with tenant allowlist for policy tests.
func testConfigWithTenants(tenantIDs ...mcp.TenantID) mcp.Config {
	cfg := testConfig()
	for _, id := range tenantIDs {
		cfg.Tenants = append(cfg.Tenants, mcp.TenantConfig{ID: id, Quotas: mcp.TenantQuotaConfig{}})
	}
	return cfg
}

// waitForActors pauses briefly to allow asynchronous PostStart messages to be
// delivered and processed by newly spawned actors. This is necessary in tests
// that assert on actor state that is set during PostStart handling.
func waitForActors() {
	time.Sleep(100 * time.Millisecond)
}

// spawnFoundationalActorsForTest spawns Registrar, Journal, Policy, CredentialBroker,
// and Router with their canonical names. The system must have ConfigExtension and
// optionally ToolConfigExtension registered. Use for router/registrar tests that
// need the full actor graph.
func spawnFoundationalActorsForTest(ctx context.Context, system goaktactor.ActorSystem) {
	_ = system.RegisterGrainKind(ctx, &sessionGrain{})
	system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
	waitForActors()
	system.Spawn(ctx, naming.ActorNameRegistrar, newRegistrar())
	waitForActors()
	system.Spawn(ctx, naming.ActorNamePolicy, newPolicyMaker())
	waitForActors()
	system.Spawn(ctx, naming.ActorNameCredentialBroker, newCredentialBroker())
	waitForActors()
	system.Spawn(ctx, naming.RouterName(0), newRouterActor())
	waitForActors()
}

// sessionInvocation returns a minimal Invocation for session tests.
func sessionInvocation(toolID mcp.ToolID, tenantID, clientID string) *mcp.Invocation {
	return &mcp.Invocation{
		Correlation: mcp.CorrelationMeta{
			TenantID:  mcp.TenantID(tenantID),
			ClientID:  mcp.ClientID(clientID),
			RequestID: "req-1",
		},
		ToolID: toolID,
		Method: "tools/call",
		Params: map[string]any{},
	}
}

// newTestKit creates a testkit for use in tests. The returned kit is cleaned up via t.Cleanup.
// Optional testkit options (e.g. testkit.WithExtensions) can be passed to configure the system.
func newTestKit(t *testing.T, opts ...testkit.Option) (*testkit.TestKit, context.Context) {
	t.Helper()
	ctx := context.Background()
	kit := testkit.New(ctx, t, opts...)
	t.Cleanup(func() {
		kit.Shutdown(ctx)
	})
	return kit, ctx
}

// mockSchemaFetcher is a test SchemaFetcher that returns configured schemas or error.
type mockSchemaFetcher struct {
	schemas []mcp.ToolSchema
	err     error
}

func (m *mockSchemaFetcher) FetchSchemas(_ context.Context, _ mcp.Tool) ([]mcp.ToolSchema, error) {
	return m.schemas, m.err
}

// mockExecutor is a test executor that returns configured results or errors.
type mockExecutor struct {
	result *mcp.ExecutionResult
	err    error
}

func (m *mockExecutor) Execute(_ context.Context, inv *mcp.Invocation) (*mcp.ExecutionResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.result != nil {
		return m.result, nil
	}
	return &mcp.ExecutionResult{
		Status:      mcp.ExecutionStatusSuccess,
		Output:      map[string]any{},
		Correlation: inv.Correlation,
	}, nil
}

func (m *mockExecutor) Close() error {
	return nil
}

// mockExecutorFactory creates executors for testing executor recovery.
type mockExecutorFactory struct {
	executor mcp.ToolExecutor
	err      error
}

func (m *mockExecutorFactory) Create(_ context.Context, _ mcp.Tool, _ map[string]string) (mcp.ToolExecutor, error) {
	return m.executor, m.err
}

// mockStreamExecutor implements both ToolExecutor and ToolStreamExecutor for tests.
// streamErr is used by ExecuteStream; the embedded mockExecutor.err is used by Execute.
type mockStreamExecutor struct {
	mockExecutor
	streamResult *mcp.StreamingResult
	streamErr    error
}

func (m *mockStreamExecutor) ExecuteStream(_ context.Context, _ *mcp.Invocation) (*mcp.StreamingResult, error) {
	if m.streamErr != nil {
		return nil, m.streamErr
	}
	return m.streamResult, nil
}

// mockResourceExecutor implements both ToolExecutor and ResourceExecutor for tests.
type mockResourceExecutor struct {
	mockExecutor
	readResult *mcp.ExecutionResult
	readErr    error
}

func (m *mockResourceExecutor) ReadResource(_ context.Context, inv *mcp.Invocation) (*mcp.ExecutionResult, error) {
	if m.readErr != nil {
		return nil, m.readErr
	}
	if m.readResult != nil {
		return m.readResult, nil
	}
	return &mcp.ExecutionResult{
		Status: mcp.ExecutionStatusSuccess,
		Output: map[string]any{
			"contents": []map[string]any{
				{"uri": "file:///test", "text": "resource content"},
			},
		},
		Correlation: inv.Correlation,
	}, nil
}

// failingAuditSink is a test sink that returns an error on Write.
type failingAuditSink struct{}

func (f *failingAuditSink) Write(_ *mcp.AuditEvent) error {
	return errors.New("write failed")
}

func (f *failingAuditSink) Close() error {
	return nil
}

// mockCredentialProvider is a test credential provider for use in tests.
type mockCredentialProvider struct {
	creds map[string]string
	err   error
}

var _ mcp.CredentialsProvider = (*mockCredentialProvider)(nil)

func (m *mockCredentialProvider) ID() string {
	return "mock"
}

func (m *mockCredentialProvider) ResolveCredentials(_ context.Context, _ mcp.TenantID, _ mcp.ToolID) (*mcp.Credentials, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &mcp.Credentials{Values: m.creds}, nil
}

// testDenyEvaluator is a PolicyEvaluator that always denies with a fixed reason.
type testDenyEvaluator struct {
	reason string
}

func (d *testDenyEvaluator) Evaluate(_ context.Context, _ mcp.PolicyInput) *mcp.RuntimeError {
	return mcp.NewRuntimeError(mcp.ErrCodePolicyDenied, d.reason)
}

// testAllowEvaluator is a PolicyEvaluator that always allows.
type testAllowEvaluator struct{}

func (a *testAllowEvaluator) Evaluate(_ context.Context, _ mcp.PolicyInput) *mcp.RuntimeError {
	return nil
}

// dynamicMockSchemaFetcher is a test SchemaFetcher that calls a function on each invocation,
// allowing per-call behavior (e.g. succeed first, fail second).
type dynamicMockSchemaFetcher struct {
	fn func() ([]mcp.ToolSchema, error)
}

func (m *dynamicMockSchemaFetcher) FetchSchemas(_ context.Context, _ mcp.Tool) ([]mcp.ToolSchema, error) {
	return m.fn()
}

func spawnTestSupervisor(t *testing.T, tool mcp.Tool) (goaktactor.ActorSystem, *goaktactor.PID, func()) {
	t.Helper()
	ctx := context.Background()
	system, stop := testActorSystemWithTools(t, tool)
	require.NoError(t, system.RegisterGrainKind(ctx, &sessionGrain{}))
	_, err := system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
	require.NoError(t, err)
	name := naming.ToolSupervisorName(tool.ID)
	sup := goaktsupervisor.NewSupervisor(goaktsupervisor.WithAnyErrorDirective(goaktsupervisor.ResumeDirective))
	pid, err := system.Spawn(ctx, name, newToolSupervisor(), goaktactor.WithSupervisor(sup))
	require.NoError(t, err)
	waitForActors()
	return system, pid, stop
}
