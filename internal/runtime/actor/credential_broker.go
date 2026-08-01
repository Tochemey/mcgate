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
	"maps"
	"time"

	goaktactor "github.com/tochemey/goakt/v4/actor"
	goaktlog "github.com/tochemey/goakt/v4/log"

	"github.com/tochemey/mcgate/mcp"

	"github.com/tochemey/mcgate/internal/naming"
	"github.com/tochemey/mcgate/internal/runtime"
	"github.com/tochemey/mcgate/internal/runtime/actor/extension"
	"github.com/tochemey/mcgate/internal/runtime/telemetry"
)

// DefaultCredentialTTL is the default cache TTL for resolved credentials.
const DefaultCredentialTTL = 5 * time.Minute

// credentialCacheEntry holds cached credentials with expiration.
type credentialCacheEntry struct {
	creds      map[string]string
	expiresAt  time.Time
	lastAccess time.Time
}

// credentialBroker is the CredentialBroker.
//
// Resolves credentials just-in-time through configured providers. Uses bounded
// in-memory cache with TTL to avoid repeated provider calls. Does not persist
// secrets longer than the cache TTL.
//
// Spawn: GatewayManager spawns CredentialBroker in spawnCredentialBroker when
// credential providers are configured. Uses ctx.Spawn(ActorNameCredentialBroker,
// newCredentialBroker(providers, ttl)) as a child of GatewayManager. Not spawned
// when buildCredentialProviders returns an empty list.
//
// Relocation: No. CredentialBroker runs on the local node as a child of
// GatewayManager and does not relocate in cluster mode.
//
// State is protected by the actor mailbox (one message at a time); no mutex
// is needed or allowed inside an actor.
type credentialBroker struct {
	providers  []mcp.CredentialsProvider
	cache      map[string]*credentialCacheEntry
	cacheTTL   time.Duration
	maxEntries int
	logger     goaktlog.Logger
}

var _ goaktactor.Actor = (*credentialBroker)(nil)

// newCredentialBroker creates a CredentialBroker with the given providers.
func newCredentialBroker() *credentialBroker {
	return &credentialBroker{}
}

// PreStart initializes the CredentialBroker.
func (x *credentialBroker) PreStart(ctx *goaktactor.Context) error {
	x.logger = ctx.Logger()
	config := ctx.Extension(extension.ConfigExtensionID).(*extension.ConfigExtension).Config()
	x.cacheTTL = config.Credentials.CacheTTL
	x.providers = config.Credentials.Providers
	x.cache = make(map[string]*credentialCacheEntry)
	x.maxEntries = config.Credentials.MaxCacheEntries
	if x.maxEntries == 0 {
		x.maxEntries = mcp.DefaultMaxCacheEntries
	}
	x.logger.Infof("actor=%s started", naming.ActorNameCredentialBroker)
	return nil
}

// Receive handles messages delivered to CredentialBroker.
func (x *credentialBroker) Receive(ctx *goaktactor.ReceiveContext) {
	switch msg := ctx.Message().(type) {
	case *goaktactor.PostStart:
		x.logger.Debugf("actor=%s post-start", naming.ActorNameCredentialBroker)
	case *runtime.ResolveRequest:
		x.handleResolve(ctx, msg)
	default:
		ctx.Unhandled()
	}
}

// PostStop clears the credential cache and releases resources.
// GoAkt guarantees PostStop runs after all message processing has completed,
// so no synchronization is needed.
func (x *credentialBroker) PostStop(ctx *goaktactor.Context) error {
	x.cache = make(map[string]*credentialCacheEntry)
	x.logger.Infof("actor=%s stopped", naming.ActorNameCredentialBroker)
	return nil
}

// handleResolve resolves credentials for the requested tenant and tool.
// It checks the in-memory cache first; on miss or expiry it iterates through
// configured providers in preference order and caches the first successful result.
// Returns a defensive copy of the credential map so callers cannot mutate the cache.
func (x *credentialBroker) handleResolve(ctx *goaktactor.ReceiveContext, msg *runtime.ResolveRequest) {
	cachingEnabled := x.cacheTTL > 0
	cacheKey := string(msg.TenantID) + ":" + string(msg.ToolID)

	if cachingEnabled {
		if entry, ok := x.cache[cacheKey]; ok && time.Now().Before(entry.expiresAt) {
			entry.lastAccess = time.Now()
			telemetry.RecordCredentialCacheResult(ctx.Context(), msg.ToolID, msg.TenantID, true)
			credsCopy := make(map[string]string, len(entry.creds))
			maps.Copy(credsCopy, entry.creds)
			ctx.Response(&runtime.ResolveResult{Credentials: &mcp.Credentials{Values: credsCopy}})
			return
		}
		telemetry.RecordCredentialCacheResult(ctx.Context(), msg.ToolID, msg.TenantID, false)
	}

	goCtx := ctx.Context()
	var resolved map[string]string
	for _, provider := range x.providers {
		creds, err := provider.ResolveCredentials(goCtx, msg.TenantID, msg.ToolID)
		if err != nil {
			x.logger.Warnf("actor=%s provider=%s resolve failed: %v", naming.ActorNameCredentialBroker, provider.ID(), err)
			continue
		}

		if creds != nil && len(creds.Values) > 0 {
			resolved = creds.Values
			break
		}
	}

	if len(resolved) == 0 {
		ctx.Response(&runtime.ResolveResult{
			Err: mcp.NewRuntimeError(mcp.ErrCodeCredentialUnavailable, "no credentials found for tenant and tool"),
		})
		return
	}

	if cachingEnabled {
		x.evictIfNeeded()
		now := time.Now()
		x.cache[cacheKey] = &credentialCacheEntry{
			creds:      resolved,
			expiresAt:  now.Add(x.cacheTTL),
			lastAccess: now,
		}
	}

	credsCopy := make(map[string]string, len(resolved))
	maps.Copy(credsCopy, resolved)
	ctx.Response(&runtime.ResolveResult{Credentials: &mcp.Credentials{
		TenantID: msg.TenantID,
		ToolID:   msg.ToolID,
		Values:   credsCopy,
	}})
}

// evictIfNeeded removes entries when the cache exceeds maxEntries.
// Expired entries are removed first. If still over limit, the entry with
// the oldest lastAccess time is evicted (LRU). Protected by actor mailbox.
func (x *credentialBroker) evictIfNeeded() {
	if x.maxEntries <= 0 || len(x.cache) < x.maxEntries {
		return
	}

	now := time.Now()
	for k, entry := range x.cache {
		if now.After(entry.expiresAt) {
			delete(x.cache, k)
		}
	}

	if len(x.cache) < x.maxEntries {
		return
	}

	var oldestKey string
	var oldestAccess time.Time
	for k, entry := range x.cache {
		if oldestKey == "" || entry.lastAccess.Before(oldestAccess) {
			oldestKey = k
			oldestAccess = entry.lastAccess
		}
	}
	if oldestKey != "" {
		delete(x.cache, oldestKey)
	}
}
