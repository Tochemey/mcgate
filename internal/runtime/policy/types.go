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
	"github.com/tochemey/portcullis/mcp"
)

// Decision is the outcome of a policy evaluation.
type Decision string

const (
	// DecisionAllow means the request is permitted to proceed.
	DecisionAllow Decision = "allow"

	// DecisionDeny means the request is rejected by authorization policy.
	DecisionDeny Decision = "deny"

	// DecisionThrottle means the request is rejected due to quota, rate limit,
	// or concurrency limit. The caller may retry later.
	DecisionThrottle Decision = "throttle"
)

// Input holds the context required to evaluate policy for an invocation.
type Input struct {
	// Invocation is the tool invocation request.
	Invocation *mcp.Invocation

	// Tool is the registered tool definition.
	Tool mcp.Tool

	// TenantID is the resolved tenant identity (never zero; defaults to "default").
	TenantID mcp.TenantID

	// ClientID is the resolved client identity (never zero; defaults to "default").
	ClientID mcp.ClientID

	// TenantConfig is the tenant configuration when the tenant is known.
	// Nil when the tenant is not in the static configuration.
	TenantConfig *mcp.TenantConfig

	// ActiveSessionCount is the number of live sessions for this tenant across all tools.
	// Used for ConcurrentSessions quota enforcement. Zero when unknown.
	ActiveSessionCount int

	// RequestsInCurrentMinute is the number of requests from this tenant in the current
	// minute window. Used for RequestsPerMinute rate limiting. Zero when unknown.
	RequestsInCurrentMinute int

	// Scopes holds the OAuth scopes granted by the validated bearer token
	// when the enterprise-managed authorization extension is active. Empty
	// when enterprise auth is not configured or the token carries no scopes.
	Scopes []string
}

// Result holds the outcome of a policy evaluation.
type Result struct {
	// Decision is the policy decision.
	Decision Decision

	// Reason is a human-readable explanation for deny or throttle.
	// Empty when Decision is Allow.
	Reason string

	// Err is the runtime error to return to the caller.
	// Nil when Decision is Allow.
	Err error
}

// Allowed reports whether the policy decision permits the request to proceed.
func (r Result) Allowed() bool {
	return r.Decision == DecisionAllow && r.Err == nil
}
