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

package mcp_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tochemey/portcullis/mcp"
)

func TestCircuitState_CanAccept(t *testing.T) {
	assert.True(t, mcp.CircuitClosed.CanAccept())
	assert.True(t, mcp.CircuitHalfOpen.CanAccept())
	assert.False(t, mcp.CircuitOpen.CanAccept())
}

func TestCircuitState_IsOpen(t *testing.T) {
	assert.True(t, mcp.CircuitOpen.IsOpen())
	assert.False(t, mcp.CircuitClosed.IsOpen())
	assert.False(t, mcp.CircuitHalfOpen.IsOpen())
}

func TestCircuitDefaults(t *testing.T) {
	assert.Equal(t, 5, mcp.DefaultCircuitFailureThreshold)
	assert.Equal(t, 30*time.Second, mcp.DefaultCircuitOpenDuration)
	assert.Equal(t, 1, mcp.DefaultCircuitHalfOpenMaxRequests)
}

func TestCircuitBreaker_DefaultsWhenZeroConfig(t *testing.T) {
	breaker := mcp.NewCircuitBreaker(mcp.CircuitConfig{})

	cfg := breaker.Config()
	assert.Equal(t, mcp.DefaultCircuitFailureThreshold, cfg.FailureThreshold)
	assert.Equal(t, mcp.DefaultCircuitOpenDuration, cfg.OpenDuration)
	assert.Equal(t, mcp.DefaultCircuitHalfOpenMaxRequests, cfg.HalfOpenMaxRequests)
	assert.Equal(t, mcp.CircuitClosed, breaker.State())
}

func TestCircuitBreaker_ClosedStateAcceptsWork(t *testing.T) {
	breaker := mcp.NewCircuitBreaker(mcp.CircuitConfig{})

	outcome, transition := breaker.Acquire()

	assert.Equal(t, mcp.CircuitAcquireAccepted, outcome)
	assert.Nil(t, transition)
	assert.Equal(t, mcp.CircuitClosed, breaker.State())
}

func TestCircuitBreaker_OpensAfterFailureThreshold(t *testing.T) {
	breaker := mcp.NewCircuitBreaker(mcp.CircuitConfig{FailureThreshold: 3})

	assert.Nil(t, breaker.OnFailure())
	assert.Nil(t, breaker.OnFailure())
	transition := breaker.OnFailure()

	require.NotNil(t, transition)
	assert.Equal(t, mcp.CircuitClosed, transition.From)
	assert.Equal(t, mcp.CircuitOpen, transition.To)
	assert.Equal(t, mcp.CircuitReasonThresholdExceeded, transition.Reason)
	assert.Equal(t, 3, transition.FailureCount)
	assert.Equal(t, mcp.CircuitOpen, breaker.State())
}

func TestCircuitBreaker_OpenRejectsAcquire(t *testing.T) {
	breaker := mcp.NewCircuitBreaker(mcp.CircuitConfig{FailureThreshold: 1, OpenDuration: time.Hour})
	breaker.OnFailure()

	outcome, _ := breaker.Acquire()

	assert.Equal(t, mcp.CircuitAcquireRejectedOpen, outcome)
}

func TestCircuitBreaker_SuccessResetsFailureCountInClosed(t *testing.T) {
	breaker := mcp.NewCircuitBreaker(mcp.CircuitConfig{FailureThreshold: 3})

	breaker.OnFailure()
	breaker.OnFailure()
	assert.Equal(t, 2, breaker.FailureCount())

	assert.Nil(t, breaker.OnSuccess())
	assert.Equal(t, 0, breaker.FailureCount())
	assert.Equal(t, mcp.CircuitClosed, breaker.State())
}

func TestCircuitBreaker_AutoTransitionsToHalfOpenAfterOpenDuration(t *testing.T) {
	now := time.Unix(0, 0)
	clock := func() time.Time { return now }
	breaker := mcp.NewCircuitBreakerWithClock(mcp.CircuitConfig{
		FailureThreshold: 1,
		OpenDuration:     50 * time.Millisecond,
	}, clock)

	breaker.OnFailure()
	require.Equal(t, mcp.CircuitOpen, breaker.State())

	now = now.Add(49 * time.Millisecond)
	outcome, transition := breaker.Acquire()
	assert.Equal(t, mcp.CircuitAcquireRejectedOpen, outcome)
	assert.Nil(t, transition)

	now = now.Add(10 * time.Millisecond)
	outcome, transition = breaker.Acquire()

	assert.Equal(t, mcp.CircuitAcquireAccepted, outcome)
	require.NotNil(t, transition)
	assert.Equal(t, mcp.CircuitOpen, transition.From)
	assert.Equal(t, mcp.CircuitHalfOpen, transition.To)
	assert.Equal(t, mcp.CircuitReasonOpenDurationElapsed, transition.Reason)
}

func TestCircuitBreaker_HalfOpenEnforcesProbeLimit(t *testing.T) {
	now := time.Unix(0, 0)
	clock := func() time.Time { return now }
	breaker := mcp.NewCircuitBreakerWithClock(mcp.CircuitConfig{
		FailureThreshold:    1,
		OpenDuration:        10 * time.Millisecond,
		HalfOpenMaxRequests: 2,
	}, clock)

	breaker.OnFailure()
	now = now.Add(20 * time.Millisecond)

	first, _ := breaker.Acquire()
	second, _ := breaker.Acquire()
	third, _ := breaker.Acquire()

	assert.Equal(t, mcp.CircuitAcquireAccepted, first)
	assert.Equal(t, mcp.CircuitAcquireAccepted, second)
	assert.Equal(t, mcp.CircuitAcquireRejectedHalfOpenLimit, third)
}

func TestCircuitBreaker_HalfOpenSuccessClosesCircuit(t *testing.T) {
	now := time.Unix(0, 0)
	clock := func() time.Time { return now }
	breaker := mcp.NewCircuitBreakerWithClock(mcp.CircuitConfig{
		FailureThreshold: 1,
		OpenDuration:     10 * time.Millisecond,
	}, clock)

	breaker.OnFailure()
	now = now.Add(20 * time.Millisecond)
	breaker.Acquire()

	transition := breaker.OnSuccess()

	require.NotNil(t, transition)
	assert.Equal(t, mcp.CircuitHalfOpen, transition.From)
	assert.Equal(t, mcp.CircuitClosed, transition.To)
	assert.Equal(t, mcp.CircuitReasonHalfOpenProbeSucceeded, transition.Reason)
	assert.Equal(t, mcp.CircuitClosed, breaker.State())
	assert.Zero(t, breaker.FailureCount())
}

func TestCircuitBreaker_HalfOpenFailureReopensCircuit(t *testing.T) {
	now := time.Unix(0, 0)
	clock := func() time.Time { return now }
	breaker := mcp.NewCircuitBreakerWithClock(mcp.CircuitConfig{
		FailureThreshold: 1,
		OpenDuration:     10 * time.Millisecond,
	}, clock)

	breaker.OnFailure()
	now = now.Add(20 * time.Millisecond)
	breaker.Acquire()

	transition := breaker.OnFailure()

	require.NotNil(t, transition)
	assert.Equal(t, mcp.CircuitHalfOpen, transition.From)
	assert.Equal(t, mcp.CircuitOpen, transition.To)
	assert.Equal(t, mcp.CircuitReasonHalfOpenProbeFailed, transition.Reason)
	assert.Equal(t, mcp.CircuitOpen, breaker.State())
}

func TestCircuitBreaker_ResetFromOpen(t *testing.T) {
	breaker := mcp.NewCircuitBreaker(mcp.CircuitConfig{FailureThreshold: 1, OpenDuration: time.Hour})
	breaker.OnFailure()
	require.Equal(t, mcp.CircuitOpen, breaker.State())

	transition := breaker.Reset()

	require.NotNil(t, transition)
	assert.Equal(t, mcp.CircuitOpen, transition.From)
	assert.Equal(t, mcp.CircuitClosed, transition.To)
	assert.Equal(t, mcp.CircuitReasonManualReset, transition.Reason)
	assert.Equal(t, mcp.CircuitClosed, breaker.State())
	assert.Zero(t, breaker.FailureCount())
}

func TestCircuitBreaker_ResetFromClosedIsNoop(t *testing.T) {
	breaker := mcp.NewCircuitBreaker(mcp.CircuitConfig{FailureThreshold: 3})
	breaker.OnFailure()
	breaker.OnFailure()

	transition := breaker.Reset()

	assert.Nil(t, transition)
	assert.Equal(t, mcp.CircuitClosed, breaker.State())
	assert.Zero(t, breaker.FailureCount())
}

func TestCircuitBreaker_PeekAppliesTimedTransition(t *testing.T) {
	now := time.Unix(0, 0)
	clock := func() time.Time { return now }
	breaker := mcp.NewCircuitBreakerWithClock(mcp.CircuitConfig{
		FailureThreshold: 1,
		OpenDuration:     10 * time.Millisecond,
	}, clock)

	breaker.OnFailure()
	now = now.Add(20 * time.Millisecond)

	state, transition := breaker.Peek()

	assert.Equal(t, mcp.CircuitHalfOpen, state)
	require.NotNil(t, transition)
	assert.Equal(t, mcp.CircuitReasonOpenDurationElapsed, transition.Reason)
}

func TestCircuitBreaker_PeekWithoutTransitionReturnsNilTransition(t *testing.T) {
	breaker := mcp.NewCircuitBreaker(mcp.CircuitConfig{})

	state, transition := breaker.Peek()

	assert.Equal(t, mcp.CircuitClosed, state)
	assert.Nil(t, transition)
}

func TestCircuitBreaker_NilClockFallsBackToTimeNow(t *testing.T) {
	breaker := mcp.NewCircuitBreakerWithClock(mcp.CircuitConfig{}, nil)

	assert.Equal(t, mcp.CircuitClosed, breaker.State())
	outcome, _ := breaker.Acquire()
	assert.Equal(t, mcp.CircuitAcquireAccepted, outcome)
}

func TestCircuitBreaker_ReleaseReturnsHalfOpenSlot(t *testing.T) {
	now := time.Unix(0, 0)
	clock := func() time.Time { return now }
	breaker := mcp.NewCircuitBreakerWithClock(mcp.CircuitConfig{
		FailureThreshold: 1,
		OpenDuration:     10 * time.Millisecond,
	}, clock)

	breaker.OnFailure()
	now = now.Add(20 * time.Millisecond)

	// Reserve the single half-open probe slot.
	outcome, _ := breaker.Acquire()
	require.Equal(t, mcp.CircuitAcquireAccepted, outcome)
	outcome, _ = breaker.Acquire()
	require.Equal(t, mcp.CircuitAcquireRejectedHalfOpenLimit, outcome)

	// Releasing the slot makes it available again instead of wedging the
	// breaker in half-open forever.
	breaker.Release()
	outcome, _ = breaker.Acquire()
	assert.Equal(t, mcp.CircuitAcquireAccepted, outcome)
}

func TestCircuitBreaker_ReleaseIsSafeOutsideHalfOpen(t *testing.T) {
	breaker := mcp.NewCircuitBreaker(mcp.CircuitConfig{})

	// No-op in Closed with no reservation.
	breaker.Release()
	outcome, _ := breaker.Acquire()
	assert.Equal(t, mcp.CircuitAcquireAccepted, outcome)
}

func TestCircuitBreaker_StaleOutcomeIsDiscarded(t *testing.T) {
	now := time.Unix(0, 0)
	clock := func() time.Time { return now }
	breaker := mcp.NewCircuitBreakerWithClock(mcp.CircuitConfig{
		FailureThreshold: 1,
		OpenDuration:     10 * time.Millisecond,
	}, clock)

	// A slow request is admitted while Closed.
	staleGen := breaker.Generation()
	outcome, _ := breaker.Acquire()
	require.Equal(t, mcp.CircuitAcquireAccepted, outcome)

	// The circuit trips and later half-opens.
	breaker.OnFailure()
	now = now.Add(20 * time.Millisecond)
	state, _ := breaker.Peek()
	require.Equal(t, mcp.CircuitHalfOpen, state)

	// The slow request's late failure report must NOT re-open the circuit:
	// it says nothing about the current half-open probe.
	transition := breaker.OnFailureAt(staleGen)
	assert.Nil(t, transition)
	assert.Equal(t, mcp.CircuitHalfOpen, breaker.State())

	// Nor may a stale success close it without a real probe.
	transition = breaker.OnSuccessAt(staleGen)
	assert.Nil(t, transition)
	assert.Equal(t, mcp.CircuitHalfOpen, breaker.State())
}

func TestCircuitBreaker_CurrentGenerationOutcomeIsApplied(t *testing.T) {
	now := time.Unix(0, 0)
	clock := func() time.Time { return now }
	breaker := mcp.NewCircuitBreakerWithClock(mcp.CircuitConfig{
		FailureThreshold: 1,
		OpenDuration:     10 * time.Millisecond,
	}, clock)

	breaker.OnFailure()
	now = now.Add(20 * time.Millisecond)

	outcome, _ := breaker.Acquire()
	require.Equal(t, mcp.CircuitAcquireAccepted, outcome)
	gen := breaker.Generation()

	transition := breaker.OnSuccessAt(gen)
	require.NotNil(t, transition)
	assert.Equal(t, mcp.CircuitClosed, breaker.State())
}

func TestCircuitBreaker_ZeroGenerationBehavesUncorrelated(t *testing.T) {
	breaker := mcp.NewCircuitBreaker(mcp.CircuitConfig{FailureThreshold: 1})

	transition := breaker.OnFailureAt(0)
	require.NotNil(t, transition)
	assert.Equal(t, mcp.CircuitOpen, breaker.State())
}
