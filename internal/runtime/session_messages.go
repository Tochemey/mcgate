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

import "github.com/tochemey/portcullis/mcp"

// Session command and response types for the session grain and ToolSupervisor.
//
// These messages define the contract for session lifecycle, invocation routing,
// and passivation. Sessions are virtual actors (grains) activated cluster-wide
// by name; routers activate them directly via the grain engine and the grain
// reports its lifecycle to the tool supervisor via Tell.

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
//
// CallerNode is the "host:port" identity of the actor system that issued the
// request. A StreamingResult carries raw Go channels, which can never cross a
// node boundary; when the grain is activated on a different node than the
// caller, it falls back to synchronous execution and returns a Result-only
// response (StreamResult nil), which serializes cleanly.
type SessionInvokeStream struct {
	Invocation        *mcp.Invocation
	CircuitGeneration uint64
	CallerNode        string
}

// SessionInvokeStreamResult is the response to SessionInvokeStream.
//
// StreamResult is only ever populated when the caller and the grain share a
// node; cross-node requests receive Result instead (see
// SessionInvokeStream.CallerNode).
type SessionInvokeStreamResult struct {
	StreamResult *mcp.StreamingResult
	Result       *mcp.ExecutionResult
	Err          error
}
