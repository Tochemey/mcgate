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
type CanAcceptWorkResult struct {
	Accept            bool
	Reason            string
	SessionCount      int
	CircuitGeneration uint64
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

// SupervisorCountSessionsForTenant is a request to count this supervisor's
// sessions that belong to the given tenant. Session names follow
// SessionName(tenantID, clientID, toolID). Must be used with Ask.
// Response is SupervisorCountSessionsForTenantResult.
type SupervisorCountSessionsForTenant struct {
	TenantID mcp.TenantID
}

// SupervisorCountSessionsForTenantResult is the response.
type SupervisorCountSessionsForTenantResult struct {
	Count int
}

// RefreshToolConfig notifies a ToolSupervisor that its tool configuration
// has changed in the ToolConfigExtension. The supervisor re-reads the
// updated config from the extension. Typically sent via Tell from the
// Registrar after an UpdateTool or EnableTool.
type RefreshToolConfig struct {
	ToolID mcp.ToolID
}
