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

	"github.com/tochemey/goakt/v4/supervisor"
)

// Supervision strategies for the runtime actors.
//
// GoAkt's default directive for a panicking actor is Stop, which silently
// removes the actor from the runtime. Every spawn in this package therefore
// attaches an explicit strategy, chosen by what a fault costs:
//
//   - Resume (keep state, skip the faulty message) for actors whose
//     in-memory state is authoritative and cannot be rebuilt from
//     configuration: the registrar (tool catalog, aggregate session view),
//     tool supervisors (circuit state, session accounting), the policy actor
//     (rate-limit windows), and the credential broker (secret cache).
//     Restarting any of these would erase state that request correctness or
//     quota enforcement depends on.
//
//   - Restart (reinitialize, rerun PreStart/PostStart) for actors whose
//     state is derived and cheap to rebuild: the gateway manager (respawns
//     its children), routers (re-resolve dependencies by name), the health
//     checker (re-resolves and reschedules probes), the journaler (recreates
//     the sink, respawns its writer), and the journal writer (holds only a
//     sink reference). The retry budget stops a deterministic fault from
//     restart-looping: one more fault after actorMaxRestarts consecutive
//     restarts within actorRestartWindow stops the actor.
const (
	actorMaxRestarts   = 5
	actorRestartWindow = time.Minute
)

// resumeStrategy returns a supervisor that keeps the actor's state and skips
// the faulty message on any error.
func resumeStrategy() *supervisor.Supervisor {
	return supervisor.NewSupervisor(
		supervisor.WithAnyErrorDirective(supervisor.ResumeDirective),
	)
}

// restartStrategy returns a supervisor that reinitializes the actor on any
// error, bounded by the package restart budget.
func restartStrategy() *supervisor.Supervisor {
	return supervisor.NewSupervisor(
		supervisor.WithAnyErrorDirective(supervisor.RestartDirective),
		supervisor.WithRetry(actorMaxRestarts, actorRestartWindow),
	)
}

// RestartStrategy returns the bounded-restart supervision strategy for
// callers outside this package (the Gateway spawning GatewayManager).
func RestartStrategy() *supervisor.Supervisor { return restartStrategy() }
