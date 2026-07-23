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
	"errors"

	goaktactor "github.com/tochemey/goakt/v4/actor"
	goaktlog "github.com/tochemey/goakt/v4/log"

	"github.com/tochemey/goakt-mcp/mcp"

	"github.com/tochemey/goakt-mcp/internal/naming"
	"github.com/tochemey/goakt-mcp/internal/runtime"
	"github.com/tochemey/goakt-mcp/internal/runtime/actor/extension"
	"github.com/tochemey/goakt-mcp/internal/runtime/audit"
	"github.com/tochemey/goakt-mcp/internal/runtime/cluster"
)

// GatewayManager is the runtime composition root inside the GoAkt actor system.
// It is responsible for spawning and supervising the foundational runtime actors:
// RegistryActor, HealthActor, JournalActor, PolicyActor, CredentialBrokerActor,
// and RouterActor. It is always the first actor spawned after the actor system
// starts and the last to stop during shutdown.
//
// Spawn: Gateway.Start calls system.Spawn(ctx, ActorNameGatewayManager, NewGatewayManager(cfg)).
// GatewayManager is a top-level actor with no parent.
//
// Relocation: No. GatewayManager runs on the local node and does not relocate in cluster mode.
//
// All fields are unexported to enforce actor immutability rules. State is
// initialized in PreStart or on receipt of PostStart.
type GatewayManager struct {
	config mcp.Config
	logger goaktlog.Logger
}

var _ goaktactor.Actor = (*GatewayManager)(nil)

// NewGatewayManager creates a GatewayManager actor primed with the provided configuration.
// The logger is captured from the actor context on PostStart, not injected here.
func NewGatewayManager() *GatewayManager {
	return &GatewayManager{}
}

// PreStart initializes the GatewayManager before message processing begins.
// At this stage the actor context is available but the actor system has not yet
// delivered the PostStart signal. Heavy initialization belongs in the PostStart handler.
//
// The session grain kind is registered here so GrainIdentity lookups from the
// supervisor can activate session grains. RegisterGrainKind is idempotent, so
// repeating the call across restarts is safe.
func (x *GatewayManager) PreStart(ctx *goaktactor.Context) error {
	x.logger = ctx.Logger()
	// this call should never panic since ConfigExtension is registered on the system at startup
	x.config = ctx.Extension(extension.ConfigExtensionID).(*extension.ConfigExtension).Config()

	if err := ctx.ActorSystem().RegisterGrainKind(ctx.Context(), &sessionGrain{}); err != nil {
		return err
	}
	return nil
}

// Receive handles messages delivered to GatewayManager.
//
// On PostStart: spawns RegistryActor, HealthActor, and JournalActor as supervised
// children. These actors are always present for the lifetime of the runtime.
// Unknown messages are marked as unhandled so the runtime can dead-letter them.
func (x *GatewayManager) Receive(ctx *goaktactor.ReceiveContext) {
	switch ctx.Message().(type) {
	case *goaktactor.PostStart:
		x.spawnFoundationalActors(ctx)
	default:
		ctx.Unhandled()
	}
}

// PostStop performs cleanup after GatewayManager has stopped.
func (x *GatewayManager) PostStop(ctx *goaktactor.Context) error {
	ctx.Logger().Infof("actor=%s stopped", naming.ActorNameGatewayManager)
	return nil
}

// spawnFoundationalActors spawns RegistryActor, HealthActor, and JournalActor as
// children of GatewayManager. They are spawned in dependency order so that the
// journal is running before BootstrapTools triggers ToolSupervisorActor spawns.
// Each supervisor resolves the journal by name from the actor system in PreStart.
//
// If any child cannot be spawned, the error is recorded on the ReceiveContext and
// the GatewayManager will surface it to the supervision layer.
func (x *GatewayManager) spawnFoundationalActors(ctx *goaktactor.ReceiveContext) {
	x.logger.Infof("actor=%s spawning foundational actors", naming.ActorNameGatewayManager)

	// spawn the registrar
	registrar := x.spawnRegistrar(ctx)

	// spawn the journal actor with a bounded mailbox to cap audit event backlog.
	// When the mailbox is full, senders block until space is available, providing
	// backpressure instead of unbounded memory growth.
	auditMailboxSize := x.config.Audit.MailboxSize
	if auditMailboxSize == 0 {
		auditMailboxSize = mcp.DefaultAuditMailboxSize
	}

	journaler := ctx.Spawn(naming.ActorNameJournal, newJournaler(),
		goaktactor.WithMailbox(goaktactor.NewBoundedMailbox(auditMailboxSize)),
		goaktactor.WithLongLived())

	// spawn the health actor with journal for health transition audit events
	healthChecker := ctx.Spawn(naming.ActorNameHealth, newHealthChecker(), goaktactor.WithLongLived())

	// spawn the policy maker
	policyMaker := ctx.Spawn(naming.ActorNamePolicy, newPolicyMaker(), goaktactor.WithLongLived())

	// spawn the credential broker actor
	credentialBroker := x.spawnCredentialBroker(ctx)

	registrarLive := registrar != nil && registrar.IsRunning()
	policyMakerLive := policyMaker != nil && policyMaker.IsRunning()
	credentialBrokerLive := credentialBroker != nil && credentialBroker.IsRunning()
	journalerLive := journaler != nil && journaler.IsRunning()
	healthCheckerLive := healthChecker != nil && healthChecker.IsRunning()

	shouldSpawnRouter := registrarLive && policyMakerLive && credentialBrokerLive && journalerLive && healthCheckerLive

	// Spawn the router pool. Each router executes one invocation at a time
	// inside its message handler, so a single router would serialize the
	// whole gateway behind the slowest backend call. Invocations are
	// distributed across the pool round-robin by the Gateway facade.
	//
	// Deliberately NOT goakt's SpawnRouter: invocation routing is
	// request-response (the Gateway Asks RouteInvocation and waits for
	// RouteResult), while goakt routers forward Broadcast messages to
	// routees via Tell, so a routee's Response can never reach the original
	// Ask caller. Its only reply-collecting strategies (scatter-gather,
	// tail-chopping) ask multiple routees, which would execute the same
	// tool invocation more than once. Plain Spawn also gives each pool
	// member a deterministic name the Gateway can address directly.
	if shouldSpawnRouter {
		poolSize := x.config.Runtime.RouterPoolSize
		if poolSize <= 0 {
			poolSize = mcp.DefaultRouterPoolSize
		}
		for i := range poolSize {
			ctx.Spawn(naming.RouterName(i), newRouterActor(), goaktactor.WithLongLived())
		}
	} else {
		err := errors.New("router actor cannot be spawned because one or more dependencies are not running")
		x.logger.Errorf("actor=%s cannot spawn router actor: %v", naming.ActorNameGatewayManager, err)
		ctx.Err(err)
		return
	}

	// bootstrap the tools after journal is running so supervisors can resolve it
	if len(x.config.Tools) > 0 {
		ctx.Tell(registrar, &runtime.BootstrapTools{Tools: x.config.Tools})
	}

	x.logger.Infof("actor=%s foundational actors spawned", naming.ActorNameGatewayManager)
}

// spawnCredentialBroker spawns CredentialBrokerActor when providers are configured.
// Returns nil when no providers are configured.
func (x *GatewayManager) spawnCredentialBroker(ctx *goaktactor.ReceiveContext) *goaktactor.PID {
	return ctx.Spawn(naming.ActorNameCredentialBroker, newCredentialBroker(), goaktactor.WithLongLived())
}

// spawnRegistrar spawns the Registry Actor. When cluster is configured (enabled with
// a user-supplied DiscoveryProvider), uses SpawnSingleton so the registry is a cluster singleton.
func (x *GatewayManager) spawnRegistrar(ctx *goaktactor.ReceiveContext) *goaktactor.PID {
	if cluster.IsClusterConfigured(x.config) {
		sys := ctx.Self().ActorSystem()

		var opts []goaktactor.ClusterSingletonOption
		if x.config.Cluster.RegistrarRole != "" {
			opts = append(opts, goaktactor.WithSingletonRole(x.config.Cluster.RegistrarRole))
		}

		// Same-kind singleton conflicts are resolved inside goakt's
		// SpawnSingleton (idempotent success returning the existing PID),
		// so any error here is a genuine failure — e.g. the name is bound
		// to a different actor kind.
		pid, err := sys.SpawnSingleton(ctx.Context(), naming.ActorNameRegistrar, newRegistrar(), opts...)
		if err != nil {
			x.logger.Warnf("actor=%s spawn singleton registry failed: %v", naming.ActorNameGatewayManager, err)
			return nil
		}
		return pid
	}
	return ctx.Spawn(naming.ActorNameRegistrar, newRegistrar(), goaktactor.WithLongLived())
}

// createAuditSink creates an audit sink from config.
func createAuditSink(config mcp.AuditConfig) mcp.AuditSink {
	if config.Sink != nil {
		switch config.Sink.(type) {
		case *audit.MemorySink:
			return config.Sink
		case *audit.FileSink:
			return config.Sink
		default:
			return config.Sink
		}
	}
	return audit.NewMemorySink()
}

// hasConcurrencyQuotas returns true when any tenant has ConcurrentSessions > 0.
// Used to skip the expensive fan-out CountSessionsForTenant on the hot path.
func hasConcurrencyQuotas(config mcp.Config) bool {
	for i := range config.Tenants {
		if config.Tenants[i].Quotas.ConcurrentSessions > 0 {
			return true
		}
	}
	return false
}
