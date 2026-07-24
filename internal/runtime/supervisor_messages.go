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

// Supervisor command and response types for ToolSupervisorActor.
//
// These messages define the contract between the supervisor and its callers
// (routing, sessions, health probes). Use Ask for request-response, Tell for
// fire-and-forget notifications.

// CanAcceptWork is a request to determine whether the supervisor can accept new work.
//
// The supervisor considers circuit state, tool availability, and administrative
// disable status. Must be used with Ask. Response is CanAcceptWorkResult.
//
// Probe marks the request as a read-only health probe: the supervisor reports
// availability without reserving a half-open circuit probe slot. Callers that
// leave Probe false are admitted for real work and must ensure exactly one
// terminal signal reaches the supervisor afterwards: a ReportSuccess /
// ReportFailure once the backend call completes, or a ReleaseWork when the
// admitted invocation never reaches the backend.
type CanAcceptWork struct {
	ToolID mcp.ToolID
	Probe  bool
}

// CanAcceptWorkResult is the response to CanAcceptWork.
// SessionCount is populated for callers that need backpressure info without
// a separate round-trip. CircuitGeneration is the circuit breaker generation
// at admission time; callers thread it through to ReportSuccess/ReportFailure
// so the supervisor can discard outcomes that arrive after the circuit has
// transitioned to a different state.
//
// Tool carries the supervisor's authoritative tool definition on every
// non-mismatch response so the router gets admission and config in a single
// round-trip and never has to query the singleton registrar on the hot path.
// It is the zero Tool only on a tool-ID mismatch (the supervisor cannot answer
// for a tool it does not own). mcp.Tool already serializes across nodes (it
// rides QueryToolResult and RefreshToolConfig), so this adds no new
// serialization surface even though the supervisor may be remote.
type CanAcceptWorkResult struct {
	Accept            bool
	Reason            string
	SessionCount      int
	CircuitGeneration uint64
	Tool              mcp.Tool
}

// ReportFailure notifies the supervisor that an invocation or session failed.
//
// The supervisor increments its failure count and may open the circuit.
// Typically sent via Tell from sessions or transport adapters.
// CircuitGeneration carries the admission generation from CanAcceptWorkResult;
// zero means uncorrelated (always applied).
type ReportFailure struct {
	ToolID            mcp.ToolID
	CircuitGeneration uint64
}

// ReportSuccess notifies the supervisor that an invocation succeeded.
//
// In closed state, this resets the failure counter. In half-open state,
// this closes the circuit. Typically sent via Tell.
// CircuitGeneration carries the admission generation from CanAcceptWorkResult;
// zero means uncorrelated (always applied).
type ReportSuccess struct {
	ToolID            mcp.ToolID
	CircuitGeneration uint64
}

// ReleaseWork notifies the supervisor that an invocation admitted via
// CanAcceptWork never reached the backend (a later pipeline stage failed),
// so any half-open probe slot reserved at admission must be returned without
// recording an outcome. Sent via Tell; safe to send even when no slot was
// reserved.
type ReleaseWork struct {
	ToolID mcp.ToolID
}

// RefreshToolConfig notifies a ToolSupervisor that its tool configuration has
// changed. Tool carries the updated authoritative definition inline (its ID
// identifies the tool) so the message is self-contained across node
// boundaries: supervisors are cluster-placed and may not share a process with
// the registrar. Typically sent via Tell from the Registrar after an
// UpdateTool, EnableTool, DisableTool, or UpdateToolHealth.
type RefreshToolConfig struct {
	Tool mcp.Tool
}
