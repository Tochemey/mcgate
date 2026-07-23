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

package actor

import (
	"context"
	"time"

	goaktactor "github.com/tochemey/goakt/v4/actor"

	"github.com/tochemey/portcullis/mcp"

	"github.com/tochemey/portcullis/internal/naming"
	"github.com/tochemey/portcullis/internal/runtime/audit"
)

// ExternalTestConfig returns a minimal config for use in gateway API tests.
func ExternalTestConfig() mcp.Config {
	cfg := mcp.Config{
		Runtime: mcp.RuntimeConfig{
			SessionIdleTimeout: mcp.DefaultSessionIdleTimeout,
			RequestTimeout:     mcp.DefaultRequestTimeout,
			StartupTimeout:     mcp.DefaultStartupTimeout,
		},
		Credentials: mcp.CredentialsConfig{
			Providers: []mcp.CredentialsProvider{},
			CacheTTL:  DefaultCredentialTTL,
		},
	}
	cfg.Audit.Sink = audit.NewMemorySink()
	return cfg
}

// SpawnFoundationalActorsForExternalTest spawns Journal, Registrar, Policy,
// CredentialBroker, and Router as top-level actors. Use for gateway API tests
// that need the full actor graph with a minimal GatewayManager (no children)
// to exercise the resolveRegistrar/resolveRouter fallback path.
func SpawnFoundationalActorsForExternalTest(ctx context.Context, system goaktactor.ActorSystem) {
	system.Spawn(ctx, naming.ActorNameJournal, newJournaler())
	waitForActorsExternal()
	system.Spawn(ctx, naming.ActorNameRegistrar, newRegistrar())
	waitForActorsExternal()
	system.Spawn(ctx, naming.ActorNamePolicy, newPolicyMaker())
	waitForActorsExternal()
	system.Spawn(ctx, naming.ActorNameCredentialBroker, newCredentialBroker())
	waitForActorsExternal()
	system.Spawn(ctx, naming.RouterName(0), newRouterActor())
	waitForActorsExternal()
}

func waitForActorsExternal() {
	time.Sleep(100 * time.Millisecond)
}
