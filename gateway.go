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

package mcgate

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	goaktactor "github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/eventstream"
	goaktlog "github.com/tochemey/goakt/v4/log"
	"github.com/tochemey/goakt/v4/remote"
	gtls "github.com/tochemey/goakt/v4/tls"

	"google.golang.org/grpc"

	"github.com/tochemey/mcgate/internal/discovery"
	"github.com/tochemey/mcgate/internal/egress"
	ingressgrpc "github.com/tochemey/mcgate/internal/ingress/grpc"
	pb "github.com/tochemey/mcgate/internal/ingress/grpc/pb"
	ingresshttp "github.com/tochemey/mcgate/internal/ingress/http"
	ingressws "github.com/tochemey/mcgate/internal/ingress/ws"
	"github.com/tochemey/mcgate/internal/naming"
	"github.com/tochemey/mcgate/internal/runtime"
	"github.com/tochemey/mcgate/internal/runtime/actor"
	actorextension "github.com/tochemey/mcgate/internal/runtime/actor/extension"
	"github.com/tochemey/mcgate/internal/runtime/cluster"
	"github.com/tochemey/mcgate/internal/runtime/config"
	"github.com/tochemey/mcgate/internal/runtime/policy"
	"github.com/tochemey/mcgate/internal/runtime/telemetry"
	"github.com/tochemey/mcgate/internal/security"
	"github.com/tochemey/mcgate/mcp"
)

const gatewayActorSystemName = "mcgate"

// Actor system event consumer tuning. The poll interval trades off event
// latency against CPU wake-ups; one second is fast enough for passivation and
// dead-letter observability without spinning.
const eventConsumerPollInterval = time.Second

// deadLetterUnknown is the sentinel used in log and metric tags when GoAkt
// reports a dead-letter event without a reason or an addressable actor path.
// Keeping the sentinel consistent between the reason field and the path
// formatter lets operators alert on a single tag value.
const deadLetterUnknown = "unknown"

// Gateway is the top-level handle for the mcgate gateway.
//
// Gateway owns the GoAkt actor system and orchestrates the full lifecycle of all
// runtime actors. It exposes a programmatic API for tool invocations, listing,
// and dynamic tool management.
//
// Create a Gateway with New, start it with Start, and stop it with Stop.
type Gateway struct {
	config  mcp.Config
	logger  goaktlog.Logger
	metrics bool
	tracing bool

	mu       sync.RWMutex
	system   goaktactor.ActorSystem
	draining bool

	// starting guards against concurrent or repeated Start calls. It is set
	// under mu at the top of Start and cleared when Start publishes the system
	// (success) or tears down partial state (failure).
	starting bool

	// eventSub receives actor system events (e.g. ActorPassivated) for metrics.
	eventSub    eventstream.Subscriber
	eventStopCh chan struct{}

	// auditStream is the per-Start event stream onto which the JournalActor
	// publishes every AuditEvent. External subscribers obtain a Subscriber via
	// Gateway.SubscribeAudit and poll it through eventstream.Subscriber.Iterator.
	// Created in actorSystemOptions (once per Start) and torn down in Stop.
	auditStream eventstream.Stream

	// managerName is the actor name used for GatewayManager. In single-node mode it
	// is always naming.ActorNameGatewayManager. In cluster mode it is suffixed with the
	// pod hostname so that each node can spawn its own local GatewayManager without
	// conflicting with GoAkt's cluster-wide actor name uniqueness check.
	managerName string

	// routerCounter distributes invocations across the router pool
	// round-robin (see resolveRouter). Atomic so concurrent Invoke calls
	// need no lock.
	routerCounter atomic.Uint64

	// this is only set for testing and is used to inject a pre-started actor system, so Start doesn't create a new one
	testSystem goaktactor.ActorSystem
}

// New creates a new Gateway with the provided configuration and options.
//
// New validates the configuration and applies defaults for any zero-valued
// runtime settings. Call Start to make the gateway operational.
func New(cfg mcp.Config, opts ...Option) (*Gateway, error) {
	config.ApplyDefaults(&cfg)

	gw := &Gateway{
		config: cfg,
		logger: goaktlog.DiscardLogger,
	}

	// Config-level logger is applied first so that explicit options take precedence.
	if cfg.LogLevel != "" {
		gw.logger = config.NewLogger(cfg.LogLevel)
	}

	for _, opt := range opts {
		opt(gw)
	}

	if err := validateTenants(gw.config.Tenants); err != nil {
		return nil, err
	}

	return gw, nil
}

// Start creates and starts the GoAkt actor system, then spawns GatewayManager
// as the runtime composition root.
//
// When Cluster.Enabled is true, Start validates that a DiscoveryProvider is
// configured. If not, Start returns an error.
//
// Start returns an error when the gateway is already started or another Start
// is in flight; call Stop before starting again.
func (g *Gateway) Start(ctx context.Context) error {
	g.mu.Lock()
	if g.system != nil {
		g.mu.Unlock()
		return mcp.NewRuntimeError(mcp.ErrCodeInternal, "gateway already started")
	}
	if g.starting {
		g.mu.Unlock()
		return mcp.NewRuntimeError(mcp.ErrCodeInternal, "gateway start already in progress")
	}
	g.starting = true
	g.mu.Unlock()

	if err := g.validateClusterConfig(); err != nil {
		g.failStart()
		return err
	}

	if g.testSystem != nil {
		g.mu.Lock()
		g.system = g.testSystem
		g.managerName = naming.ActorNameGatewayManager
		g.starting = false
		g.mu.Unlock()
		return nil
	}

	if g.metrics {
		if _, err := telemetry.RegisterMetrics(nil); err != nil {
			g.failStart()
			return mcp.WrapRuntimeError(mcp.ErrCodeInternal, "failed to register metrics", err)
		}
	}

	if g.tracing {
		telemetry.RegisterTracer()
	}

	var tlsInfo *gtls.Info
	if g.config.Cluster.Enabled && g.config.Cluster.TLS != nil {
		var err error
		tlsInfo, err = security.BuildRemotingTLSInfo(g.config.Cluster.TLS)
		if err != nil {
			g.failStart()
			return mcp.WrapRuntimeError(mcp.ErrCodeInternal, "cluster TLS config", err)
		}
	}

	system, err := goaktactor.NewActorSystem(gatewayActorSystemName, g.actorSystemOptions(tlsInfo)...)
	if err != nil {
		g.failStart()
		return mcp.WrapRuntimeError(mcp.ErrCodeInternal, "failed to create actor system", err)
	}

	if err := system.Start(ctx); err != nil {
		g.failStart()
		return mcp.WrapRuntimeError(mcp.ErrCodeInternal, "failed to start actor system", err)
	}

	managerName := g.localManagerName()
	// Restart on fault: GatewayManager is a stateless composition root; a
	// restart respawns the foundational actors. Without an explicit strategy
	// a panic would Stop it (goakt's default) and silently behead the runtime.
	if _, err := system.Spawn(ctx, managerName, actor.NewGatewayManager(),
		goaktactor.WithSupervisor(actor.RestartStrategy()),
		goaktactor.WithLongLived()); err != nil {
		_ = system.Stop(ctx)
		g.failStart()
		return mcp.WrapRuntimeError(mcp.ErrCodeInternal, "failed to spawn GatewayManager", err)
	}

	g.mu.Lock()
	g.system = system
	g.managerName = managerName
	g.starting = false
	g.mu.Unlock()

	if g.metrics {
		g.startEventConsumer(system)
	}

	return nil
}

// failStart releases the in-flight start guard and tears down any state a
// failed Start may have left behind. In particular actorSystemOptions publishes
// g.auditStream before the actor system can fail to create or start; closing
// and clearing it here keeps SubscribeAudit honest on a never-started gateway
// and prevents a retried Start from stranding prior subscribers on an orphaned
// stream.
func (g *Gateway) failStart() {
	g.mu.Lock()
	if g.auditStream != nil {
		g.auditStream.Close()
		g.auditStream = nil
	}
	g.starting = false
	g.mu.Unlock()
}

// Stop gracefully shuts down the actor system.
//
// Stop blocks until shutdown has completed, the provided context is cancelled,
// or Runtime.ShutdownTimeout elapses, whichever comes first.
// Calling Stop on a Gateway that was never started or already stopped is a no-op.
func (g *Gateway) Stop(ctx context.Context) error {
	g.mu.Lock()
	if g.system == nil {
		g.mu.Unlock()
		return nil
	}

	g.draining = true
	system := g.system
	g.mu.Unlock()

	// Bound the shutdown by Runtime.ShutdownTimeout in addition to the caller's
	// context so the documented contract holds even with a background context.
	if timeout := g.config.Runtime.ShutdownTimeout; timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	g.stopEventConsumer(system)

	if err := system.Stop(ctx); err != nil {
		g.mu.Lock()
		g.draining = false
		g.mu.Unlock()
		return mcp.WrapRuntimeError(mcp.ErrCodeInternal, "failed to stop actor system", err)
	}

	g.mu.Lock()
	if g.auditStream != nil {
		g.auditStream.Close()
		g.auditStream = nil
	}
	g.system = nil
	g.draining = false
	g.mu.Unlock()
	return nil
}

// SubscribeAudit returns a new eventstream.Subscriber that receives every
// mcp.AuditEvent written by the Journal actor after Gateway.Start.
//
// Messages delivered on the returned Subscriber carry *mcp.AuditEvent as the
// message payload, on the topic extension.AuditStreamTopic. Consume messages
// by polling Subscriber.Iterator() periodically; the iterator drains messages
// buffered at the time of invocation.
//
// SubscribeAudit returns an error when the gateway has not been started. The
// returned subscriber becomes inert when the gateway is stopped. Callers must
// pass the subscriber to UnsubscribeAudit when finished to release resources.
func (g *Gateway) SubscribeAudit() (eventstream.Subscriber, error) {
	g.mu.RLock()
	stream := g.auditStream
	g.mu.RUnlock()

	if stream == nil {
		return nil, mcp.NewRuntimeError(mcp.ErrCodeInternal, "gateway not started")
	}

	sub := stream.AddSubscriber()
	stream.Subscribe(sub, actorextension.AuditStreamTopic)
	return sub, nil
}

// UnsubscribeAudit removes a subscriber previously returned by SubscribeAudit
// and releases its buffered messages. A nil subscriber is a no-op. Unsubscribing
// from a Gateway that has already been stopped is also a no-op.
//
// RemoveSubscriber drains the subscriber from every topic it is currently
// attached to and then flips it inactive, so a single call covers both the
// topic cleanup and the shutdown signal.
func (g *Gateway) UnsubscribeAudit(sub eventstream.Subscriber) {
	if sub == nil {
		return
	}

	g.mu.RLock()
	stream := g.auditStream
	g.mu.RUnlock()

	if stream == nil {
		return
	}

	stream.RemoveSubscriber(sub)
}

// System returns the underlying GoAkt actor system.
//
// Returns nil if Start has not been called or has not yet succeeded.
// Intended for advanced use cases and testing.
func (g *Gateway) System() goaktactor.ActorSystem {
	g.mu.RLock()
	s := g.system
	g.mu.RUnlock()
	return s
}

// requireRunning returns the actor system if the gateway is running and not
// draining. The returned system is safe to use for the duration of a single
// API call. Callers must not cache the returned system across calls.
func (g *Gateway) requireRunning() (goaktactor.ActorSystem, error) {
	g.mu.RLock()
	system := g.system
	draining := g.draining
	g.mu.RUnlock()
	if draining {
		return nil, mcp.NewRuntimeError(mcp.ErrCodeInternal, "gateway is shutting down")
	}
	if system == nil {
		return nil, mcp.NewRuntimeError(mcp.ErrCodeInternal, "gateway not started")
	}
	return system, nil
}

func (g *Gateway) validateClusterConfig() error {
	if g.config.Cluster.Enabled && discovery.IsNilDiscoveryProvider(g.config.Cluster.DiscoveryProvider) {
		return mcp.NewRuntimeError(mcp.ErrCodeInvalidRequest,
			"cluster is enabled but no DiscoveryProvider is configured")
	}
	return nil
}

// validateTenants checks that tenant IDs are non-empty and unique.
func validateTenants(tenants []mcp.TenantConfig) error {
	seen := make(map[mcp.TenantID]struct{}, len(tenants))
	for _, tenant := range tenants {
		if tenant.ID.IsZero() {
			return mcp.NewRuntimeError(mcp.ErrCodeInvalidRequest, "tenant ID must not be empty")
		}

		if _, dup := seen[tenant.ID]; dup {
			return mcp.NewRuntimeError(mcp.ErrCodeInvalidRequest, "duplicate tenant ID: "+string(tenant.ID))
		}
		seen[tenant.ID] = struct{}{}
	}
	return nil
}

// Handler returns an [http.Handler] that serves MCP Streamable HTTP sessions,
// routing each tool call through this gateway.
//
// Handler validates that cfg.IdentityResolver is set and delegates to
// [ingresshttp.New]. The gateway does not need to be running at the time
// Handler is called; tool discovery happens lazily per session via ListTools.
func (g *Gateway) Handler(cfg mcp.IngressConfig) (http.Handler, error) {
	return ingresshttp.New(g, cfg)
}

// WSHandler returns an [http.Handler] that upgrades HTTP connections to
// WebSocket and serves MCP sessions over the WebSocket transport.
//
// WSHandler validates that cfg.IdentityResolver is set and delegates to
// [ingressws.New]. The wsCfg parameter is optional; nil uses defaults.
// The gateway does not need to be running at the time WSHandler is called;
// tool discovery happens lazily per session.
func (g *Gateway) WSHandler(cfg mcp.IngressConfig, wsCfg *mcp.WSConfig) (http.Handler, error) {
	return ingressws.New(g, cfg, wsCfg)
}

// RegisterGRPCService registers the MCPToolService gRPC service on the
// provided [grpc.Server], routing each tool call through this gateway.
//
// Unlike the HTTP handler factories ([Handler], [WSHandler])
// which return an [http.Handler], gRPC services register directly on a
// [grpc.Server]. This method validates the config and registers the service.
//
// When cfg.EnterpriseAuth is set and cfg.IdentityResolver is nil, a
// token-based identity resolver is installed automatically (same pattern
// as the HTTP ingress).
//
// The gateway does not need to be running at the time RegisterGRPCService is
// called; tool discovery happens lazily per request via ListTools.
func (g *Gateway) RegisterGRPCService(srv *grpc.Server, cfg mcp.GRPCIngressConfig) error {
	svc, err := ingressgrpc.NewServer(g, cfg)
	if err != nil {
		return err
	}
	pb.RegisterMCPToolServiceServer(srv, svc)
	return nil
}

// GRPCAuthInterceptors returns gRPC unary and stream server interceptors that
// enforce Bearer token authentication per the MCP enterprise-managed
// authorization extension.
//
// Install the returned interceptors on the [grpc.Server] before calling
// [Gateway.RegisterGRPCService]:
//
//	unary, stream, err := mcgate.GRPCAuthInterceptors(enterpriseAuthCfg)
//	srv := grpc.NewServer(
//	    grpc.ChainUnaryInterceptor(unary),
//	    grpc.ChainStreamInterceptor(stream),
//	)
//	gw.RegisterGRPCService(srv, cfg)
func GRPCAuthInterceptors(ea *mcp.EnterpriseAuthConfig) (grpc.UnaryServerInterceptor, grpc.StreamServerInterceptor, error) {
	return ingressgrpc.AuthInterceptors(ea)
}

// startEventConsumer subscribes to the actor system event stream and starts a
// background goroutine that polls for ActorPassivated and Deadletter events.
// Passivation is the only reliable signal for idle-session reclamation (GoAkt
// publishes ActorPassivated exclusively when the passivation strategy fires,
// not on explicit stops); dead letters surface undelivered messages that
// would otherwise be invisible.
func (g *Gateway) startEventConsumer(system goaktactor.ActorSystem) {
	sub, err := system.Subscribe()
	if err != nil {
		g.logger.Warnf("failed to subscribe to event stream for passivation metrics: %v", err)
		return
	}

	stopCh := make(chan struct{})

	g.mu.Lock()
	g.eventSub = sub
	g.eventStopCh = stopCh
	g.mu.Unlock()

	go g.consumeSystemEvents(sub, stopCh)
}

// stopEventConsumer signals the background event consumer to stop and
// unsubscribes from the actor system event stream.
func (g *Gateway) stopEventConsumer(system goaktactor.ActorSystem) {
	g.mu.Lock()
	sub := g.eventSub
	stopCh := g.eventStopCh
	g.eventSub = nil
	g.eventStopCh = nil
	g.mu.Unlock()

	if stopCh != nil {
		close(stopCh)
	}
	if sub != nil {
		_ = system.Unsubscribe(sub)
	}
}

// consumeSystemEvents periodically drains the event stream subscriber and
// dispatches each known lifecycle event to its recorder. Unknown payload
// types are ignored so future GoAkt events do not break this loop.
func (g *Gateway) consumeSystemEvents(sub eventstream.Subscriber, stopCh <-chan struct{}) {
	ticker := time.NewTicker(eventConsumerPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			for msg := range sub.Iterator() {
				switch ev := msg.Payload().(type) {
				case *goaktactor.ActorPassivated:
					g.handlePassivationEvent(ev)
				case *goaktactor.Deadletter:
					g.handleDeadLetterEvent(ev)
				}
			}
		}
	}
}

// handlePassivationEvent records a session passivation metric for the tool
// implied by the passivated actor's path. Non-session actors are ignored.
func (g *Gateway) handlePassivationEvent(ev *goaktactor.ActorPassivated) {
	toolID := toolIDFromPath(ev.ActorPath())
	if toolID.IsZero() {
		return
	}

	telemetry.RecordSessionPassivated(context.Background(), toolID)
}

// handleDeadLetterEvent logs the undelivered message and increments the
// dead-letter counter. The reason string is the one GoAkt attached to the
// event (e.g. "actor stopped", "mailbox full"). Sender and receiver paths
// are logged so operators can trace which actor caused the drop.
func (g *Gateway) handleDeadLetterEvent(ev *goaktactor.Deadletter) {
	reason := ev.Reason()
	if reason == "" {
		reason = deadLetterUnknown
	}

	g.logger.Warnf("dead letter: sender=%s receiver=%s reason=%s",
		formatDeadLetterPath(ev.Sender()),
		formatDeadLetterPath(ev.Receiver()),
		reason,
	)

	telemetry.RecordDeadLetter(context.Background(), reason)
}

// formatDeadLetterPath renders an actor Path for dead-letter logs. A nil Path
// is reported as "unknown" instead of the empty string so log readers can
// tell the difference between a real un-named sender and a missing field.
func formatDeadLetterPath(p goaktactor.Path) string {
	if p == nil {
		return deadLetterUnknown
	}
	return p.Name()
}

// toolIDFromPath extracts the tool ID from a passivated session's
// Path. Session actors are named "session-..." and are children of a supervisor
// actor named "supervisor-{toolID}". Returns a zero ToolID when the path does
// not match a session actor.
func toolIDFromPath(path goaktactor.Path) mcp.ToolID {
	if path == nil {
		return ""
	}
	if !strings.HasPrefix(path.Name(), "session-") {
		return ""
	}
	parent := path.Parent()
	if parent == nil {
		return ""
	}

	supervisorName := parent.Name()
	if !strings.HasPrefix(supervisorName, "supervisor-") {
		return ""
	}
	return naming.ToolIDFromSupervisorName(supervisorName)
}

// localManagerName returns the actor name to use for GatewayManager on this node.
//
// In single-node mode the name is always naming.ActorNameGatewayManager.
// In cluster mode GoAkt's system.Spawn checks actor name uniqueness across the
// entire cluster DMap, so a fixed name would prevent every node after the first
// from spawning its own GatewayManager. Suffixing with the pod hostname plus
// the remoting port makes the name cluster-globally unique while remaining
// stable across pod restarts — the port matters when several nodes share a
// host (multi-process deployments, in-process cluster tests).
func (g *Gateway) localManagerName() string {
	if g.config.Cluster.Enabled {
		if hostname, err := os.Hostname(); err == nil && hostname != "" {
			return naming.ActorNameGatewayManager + "-" + hostname + "-" + strconv.Itoa(g.config.Cluster.RemotingPort)
		}
	}
	return naming.ActorNameGatewayManager
}

func (g *Gateway) remoteOptions() []remote.Option {
	opts := []remote.Option{
		remote.WithSerializables(
			(*runtime.CanAcceptWork)(nil),
			(*runtime.CanAcceptWorkResult)(nil),
			(*runtime.ReportFailure)(nil),
			(*runtime.ReportSuccess)(nil),

			(*runtime.RouteInvocation)(nil),
			(*runtime.RouteResult)(nil),

			(*runtime.RegisterTool)(nil),
			(*runtime.RegisterToolResult)(nil),
			(*runtime.UpdateTool)(nil),
			(*runtime.UpdateToolResult)(nil),
			(*runtime.DisableTool)(nil),
			(*runtime.DisableToolResult)(nil),
			(*runtime.RemoveTool)(nil),
			(*runtime.RemoveToolResult)(nil),
			(*runtime.QueryTool)(nil),
			(*runtime.QueryToolResult)(nil),
			(*runtime.UpdateToolHealth)(nil),
			(*runtime.UpdateToolHealthResult)(nil),
			(*runtime.BootstrapTools)(nil),
			(*runtime.ListTools)(nil),
			(*runtime.ListToolsResult)(nil),
			(*runtime.CountSessionsForTenant)(nil),
			(*runtime.CountSessionsForTenantResult)(nil),

			(*runtime.SessionInvoke)(nil),
			(*runtime.SessionInvokeResult)(nil),
			(*runtime.SessionInvokeStream)(nil),
			(*runtime.SessionInvokeStreamResult)(nil),

			// Session lifecycle and circuit-admission signals travel
			// grain→supervisor and supervisor→registrar, both of which can
			// cross node boundaries under cluster-wide execution placement.
			(*runtime.SessionActivated)(nil),
			(*runtime.SessionDeactivated)(nil),
			(*runtime.ReleaseWork)(nil),

			(*runtime.RecordAuditEvent)(nil),

			(*policy.EvaluateRequest)(nil),
			(*policy.EvaluateResult)(nil),

			(*runtime.ResolveRequest)(nil),
			(*runtime.ResolveResult)(nil),
			(*runtime.EnableTool)(nil),
			(*runtime.EnableToolResult)(nil),
			(*runtime.RefreshToolConfig)(nil),

			(*runtime.GetToolStatus)(nil),
			(*runtime.GetToolStatusResult)(nil),
			(*runtime.ResetCircuit)(nil),
			(*runtime.ResetCircuitResult)(nil),
			(*runtime.DrainTool)(nil),
			(*runtime.DrainToolResult)(nil),
			(*runtime.ListAllSessions)(nil),
			(*runtime.ListAllSessionsResult)(nil),
			(*runtime.GetToolSchema)(nil),
			(*runtime.GetToolSchemaResult)(nil),
		),
	}
	if g.tracing {
		opts = append(opts, remote.WithContextPropagator(telemetry.NewOTelContextPropagator()))
	}
	return opts
}

func (g *Gateway) actorSystemOptions(tlsInfo *gtls.Info) []goaktactor.Option {
	execFactory := egress.NewCompositeExecutorFactory(g.config.Runtime.StartupTimeout, nil)
	schemaFetcher := egress.NewCompositeSchemaFetcher(g.config.Runtime.StartupTimeout, nil)
	resourceFetcher := egress.NewCompositeResourceFetcher(g.config.Runtime.StartupTimeout, nil)

	auditStream := eventstream.New()

	g.mu.Lock()
	g.auditStream = auditStream
	g.mu.Unlock()

	opts := []goaktactor.Option{
		goaktactor.WithLogger(g.logger),
		goaktactor.WithActorInitMaxRetries(3),
		goaktactor.WithExtensions(
			actorextension.NewExecutorFactoryExtension(execFactory),
			actorextension.NewSchemaFetcherExtension(schemaFetcher),
			actorextension.NewResourceFetcherExtension(resourceFetcher),
			actorextension.NewConfigExtension(g.config),
			actorextension.NewAuditStreamExtension(auditStream),
		),
	}

	remoteOpts := g.remoteOptions()
	if tlsInfo != nil {
		// TLS rides on the remote config: remoting is only enabled in
		// cluster mode, which is also the only mode that produces a non-nil
		// tlsInfo (see Start).
		remoteOpts = append(remoteOpts, remote.WithTLS(tlsInfo))
	}

	// Registrar and ToolSupervisor are registered as cluster kinds: the
	// registrar so the singleton can start on any node, the supervisor so
	// SpawnOn placement and relocation can instantiate it on remote nodes.
	if clusterOpts := cluster.BuildOptions(g.config, remoteOpts, actor.NewRegistrar(), actor.NewToolSupervisor()); len(clusterOpts) > 0 {
		opts = append(opts, clusterOpts...)
	}
	return opts
}
