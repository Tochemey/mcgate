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

package policy

import (
	"github.com/tochemey/mcgate/mcp"
)

// Evaluator performs policy evaluation for tool invocations.
//
// Evaluation order: authorization first, then quotas and rate limits.
// The first denial or throttle short-circuits and returns immediately.
type Evaluator struct {
	config mcp.Config
}

// NewEvaluator creates an Evaluator with the given configuration.
func NewEvaluator(config mcp.Config) *Evaluator {
	return &Evaluator{config: config}
}

// Evaluate runs the full policy check for the given input.
//
// Returns Result with DecisionAllow when all checks pass. Returns Result with
// DecisionDeny or DecisionThrottle and a non-nil Err when a check fails.
func (e *Evaluator) Evaluate(in *Input) Result {
	if in == nil || in.Invocation == nil || in.Tool.ID.IsZero() {
		return Result{
			Decision: DecisionDeny,
			Reason:   "invalid policy input",
			Err:      mcp.NewRuntimeError(mcp.ErrCodeInvalidRequest, "invalid policy input"),
		}
	}

	if r := e.checkAuthorization(in); !r.Allowed() {
		return r
	}

	if r := e.checkQuotas(in); !r.Allowed() {
		return r
	}

	return Result{Decision: DecisionAllow}
}

// checkAuthorization enforces the tool's authorization policy.
//
// For AuthorizationPolicyTenantAllowlist: when tenants are configured, the
// tenant must be in the list. When no tenants are configured, all tenants are
// allowed (no restriction).
func (e *Evaluator) checkAuthorization(in *Input) Result {
	if in.Tool.AuthorizationPolicy != mcp.AuthorizationPolicyTenantAllowlist {
		return Result{Decision: DecisionAllow}
	}

	if len(e.config.Tenants) == 0 {
		return Result{Decision: DecisionAllow}
	}

	for _, tc := range e.config.Tenants {
		if tc.ID == in.TenantID {
			return Result{Decision: DecisionAllow}
		}
	}

	return Result{
		Decision: DecisionDeny,
		Reason:   "tenant not in allowlist",
		Err:      mcp.NewRuntimeError(mcp.ErrCodePolicyDenied, "tenant not permitted for this tool"),
	}
}

// checkQuotas enforces tenant-level quotas: RequestsPerMinute and ConcurrentSessions.
func (e *Evaluator) checkQuotas(in *Input) Result {
	if in.TenantConfig == nil {
		return Result{Decision: DecisionAllow}
	}

	q := in.TenantConfig.Quotas

	if q.ConcurrentSessions > 0 && in.ActiveSessionCount >= q.ConcurrentSessions {
		return Result{
			Decision: DecisionThrottle,
			Reason:   "concurrent session limit reached",
			Err:      mcp.NewRuntimeError(mcp.ErrCodeConcurrencyLimitReached, "tenant concurrent session limit reached"),
		}
	}

	// Throttle when current count already equals limit (allow N, throttle N+1).
	if q.RequestsPerMinute > 0 && in.RequestsInCurrentMinute >= q.RequestsPerMinute {
		return Result{
			Decision: DecisionThrottle,
			Reason:   "requests per minute limit exceeded",
			Err:      mcp.NewRuntimeError(mcp.ErrCodeRateLimited, "tenant rate limit exceeded"),
		}
	}

	return Result{Decision: DecisionAllow}
}
