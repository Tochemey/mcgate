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
	"time"

	goaktactor "github.com/tochemey/goakt/v4/actor"
	goaktlog "github.com/tochemey/goakt/v4/log"

	"github.com/tochemey/mcgate/mcp"

	"github.com/tochemey/mcgate/internal/naming"
	"github.com/tochemey/mcgate/internal/runtime/actor/extension"
	"github.com/tochemey/mcgate/internal/runtime/policy"
)

// policyMaker is the policy actor.
//
// policy actor evaluates authorization, quotas, rate limits, and concurrency
// limits before execution. It holds per-tenant usage counters for rate limiting
// and delegates to the policy Evaluator for authorization and quota checks.
//
// Spawn: GatewayManager spawns policy actor in spawnFoundationalActors via
// ctx.Spawn(ActorNamePolicy, newPolicyMaker()) as a child of GatewayManager.
// The runtime config is resolved from the ConfigExtension in PreStart.
//
// Relocation: No. policy actor runs on the local node as a child of GatewayManager
// and does not relocate in cluster mode.
//
// State is protected by the actor mailbox (one message at a time); no mutex
// is needed or allowed inside an actor.
//
// All fields are unexported to enforce actor immutability rules.
type policyMaker struct {
	evaluator     *policy.Evaluator
	config        mcp.Config
	requestCounts map[mcp.TenantID]int
	currentMinute int64
	logger        goaktlog.Logger
}

var _ goaktactor.Actor = (*policyMaker)(nil)

// newPolicyMaker creates a policy actor instance. Configuration and the
// evaluator are resolved from the ConfigExtension during PreStart.
func newPolicyMaker() *policyMaker {
	return &policyMaker{}
}

// PreStart initializes the policy actor before message processing begins.
// The runtime config is resolved from the ConfigExtension and used to build
// the Evaluator. The ConfigExtension is registered on the actor system at
// startup, so the type assertion is safe.
func (x *policyMaker) PreStart(ctx *goaktactor.Context) error {
	x.logger = ctx.Logger()
	x.config = ctx.Extension(extension.ConfigExtensionID).(*extension.ConfigExtension).Config()
	x.evaluator = policy.NewEvaluator(x.config)
	x.requestCounts = make(map[mcp.TenantID]int)
	x.logger.Infof("actor=%s started", naming.ActorNamePolicy)
	return nil
}

// Receive handles messages delivered to policy actor.
func (x *policyMaker) Receive(ctx *goaktactor.ReceiveContext) {
	switch msg := ctx.Message().(type) {
	case *goaktactor.PostStart:
		x.logger.Debugf("actor=%s post-start", naming.ActorNamePolicy)
	case *policy.EvaluateRequest:
		x.handleEvaluate(ctx, msg)
	default:
		ctx.Unhandled()
	}
}

// PostStop performs cleanup after policy actor has stopped.
func (x *policyMaker) PostStop(ctx *goaktactor.Context) error {
	x.logger.Infof("actor=%s stopped", naming.ActorNamePolicy)
	return nil
}

// handleEvaluate processes a policy evaluation request. It tracks per-tenant
// request counts for rate limiting (resetting each calendar minute) and
// delegates authorization and quota checks to the Evaluator.
func (x *policyMaker) handleEvaluate(ctx *goaktactor.ReceiveContext, msg *policy.EvaluateRequest) {
	if msg.Input == nil {
		ctx.Response(&policy.EvaluateResult{
			Result: policy.Result{
				Decision: policy.DecisionDeny,
				Reason:   "missing policy input",
				Err:      mcp.NewRuntimeError(mcp.ErrCodeInvalidRequest, "missing policy input"),
			},
		})
		return
	}

	in := msg.Input

	now := time.Now()
	minute := now.Unix() / 60
	if minute != x.currentMinute {
		clear(x.requestCounts)
		x.currentMinute = minute
	}

	// Snapshot the current count for "allow N, throttle N+1" semantics, then
	// increment unconditionally so denied requests still count toward the rate
	// limit. This prevents a flood of denied calls from bypassing throttling.
	in.RequestsInCurrentMinute = x.requestCounts[in.TenantID]
	x.requestCounts[in.TenantID]++
	in.TenantConfig = x.lookupTenantConfig(in.TenantID)

	result := x.evaluator.Evaluate(in)
	if !result.Allowed() {
		ctx.Response(&policy.EvaluateResult{Result: result})
		return
	}

	// Call the tenant's custom PolicyEvaluator when configured. It runs after
	// all built-in authorization and quota checks have passed.
	if in.TenantConfig != nil && in.TenantConfig.Evaluator != nil {
		policyInput := mcp.PolicyInput{
			TenantID:                in.TenantID,
			ToolID:                  in.Tool.ID,
			ActiveSessionCount:      in.ActiveSessionCount,
			RequestsInCurrentMinute: in.RequestsInCurrentMinute,
			Scopes:                  in.Scopes,
		}
		if runtimeErr := in.TenantConfig.Evaluator.Evaluate(ctx.Context(), policyInput); runtimeErr != nil {
			ctx.Response(&policy.EvaluateResult{
				Result: policy.Result{
					Decision: policy.DecisionDeny,
					Reason:   runtimeErr.Message,
					Err:      runtimeErr,
				},
			})
			return
		}
	}

	ctx.Response(&policy.EvaluateResult{Result: result})
}

// lookupTenantConfig returns the TenantConfig for the given tenant, or nil
// when the tenant is not explicitly configured. Used to resolve quota limits.
func (x *policyMaker) lookupTenantConfig(tenantID mcp.TenantID) *mcp.TenantConfig {
	for i := range x.config.Tenants {
		tc := &x.config.Tenants[i]
		if tc.ID == tenantID {
			return tc
		}
	}
	return nil
}
