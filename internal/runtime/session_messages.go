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

package runtime

import "github.com/tochemey/goakt-mcp/mcp"

// Session command and response types for SessionActor and ToolSupervisorActor.
//
// These messages define the contract for session lifecycle, invocation routing,
// and passivation. Session creation is owned by the supervisor; sessions are
// children of the tool supervisor.

// GetOrCreateSession is a request to resolve or activate a session grain for
// the given tenant, client, and tool combination.
//
// The supervisor returns the session grain identity (activating the grain on
// demand). Credentials are passed when the grain is first activated so its
// executor can be set up with backend authentication.
// Must be used with Ask. Response is GetOrCreateSessionResult.
type GetOrCreateSession struct {
	TenantID    mcp.TenantID
	ClientID    mcp.ClientID
	ToolID      mcp.ToolID
	Credentials map[string]string
}

// GetOrCreateSessionResult is the response to GetOrCreateSession.
//
// When Found is true, Session holds the *goaktactor.GrainIdentity of the
// session grain. The field is typed as `any` so this package does not pull
// in the goakt import; callers type-assert before using AskGrain / TellGrain.
type GetOrCreateSessionResult struct {
	Session any
	Found   bool
	Err     error
}

// SessionActivated is sent by a session grain to its ToolSupervisor at the
// end of OnActivate. The supervisor increments its per-tool session count
// used for backpressure decisions. Delivered via Tell so the grain's
// activation path is not blocked on the supervisor's mailbox.
type SessionActivated struct {
	ToolID   mcp.ToolID
	TenantID mcp.TenantID
	ClientID mcp.ClientID
}

// SessionDeactivated is sent by a session grain to its ToolSupervisor when
// the grain is about to be removed from memory (idle passivation, node
// shutdown, explicit deactivation). The supervisor decrements its per-tool
// session count.
type SessionDeactivated struct {
	ToolID   mcp.ToolID
	TenantID mcp.TenantID
	ClientID mcp.ClientID
}

// SessionInvoke is a request to execute an invocation through the session.
//
// The session serializes invocations through its mailbox and tracks in-flight
// work. Must be used with Ask. Response is SessionInvokeResult.
//
// CircuitGeneration is the circuit-breaker admission generation from
// CanAcceptWorkResult. The grain echoes it on the ReportSuccess/ReportFailure
// it sends to the supervisor so stale outcomes are discarded; zero means
// uncorrelated.
type SessionInvoke struct {
	Invocation        *mcp.Invocation
	CircuitGeneration uint64
}

// SessionInvokeResult is the response to SessionInvoke.
type SessionInvokeResult struct {
	Result *mcp.ExecutionResult
	Err    error
}

// SessionInvokeStream is a request to execute an invocation with streaming
// progress support. The session checks if the executor implements
// ToolStreamExecutor and returns a StreamingResult if so.
// Must be used with Ask. Response is SessionInvokeStreamResult.
//
// CircuitGeneration carries the circuit-breaker admission generation; see
// SessionInvoke.
type SessionInvokeStream struct {
	Invocation        *mcp.Invocation
	CircuitGeneration uint64
}

// SessionInvokeStreamResult is the response to SessionInvokeStream.
type SessionInvokeStreamResult struct {
	StreamResult *mcp.StreamingResult
	Result       *mcp.ExecutionResult
	Err          error
}
