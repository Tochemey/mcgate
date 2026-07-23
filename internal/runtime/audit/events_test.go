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

package audit_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tochemey/portcullis/internal/runtime/audit"
	"github.com/tochemey/portcullis/mcp"
)

const (
	testToolID        = "tool-1"
	testToolIDClosed  = "tool-2"
	testStateEnabled  = "enabled"
	testStateDegraded = "degraded"
	testStateOpen     = "open"
	testStateClosed   = "closed"
	testReasonValue   = "failure_threshold"
	testCountKey      = "count"
	testCountValue    = "5"
)

func TestHealthTransitionEvent(t *testing.T) {
	ev := audit.HealthTransitionAuditEvent(testToolID, testStateEnabled, testStateDegraded)

	require.NotNil(t, ev)
	assert.Equal(t, mcp.AuditEventTypeHealthTransition, ev.Type)
	assert.Equal(t, testToolID, ev.ToolID)
	assert.Equal(t, testStateDegraded, ev.Outcome)
	assert.NotZero(t, ev.Timestamp)

	require.NotNil(t, ev.Metadata)
	assert.Equal(t, testStateEnabled, ev.Metadata[audit.MetadataKeyFromState])
	assert.Equal(t, testStateDegraded, ev.Metadata[audit.MetadataKeyToState])
}

func TestCircuitStateChangeEvent(t *testing.T) {
	meta := map[string]string{
		audit.MetadataKeyReason: testReasonValue,
		testCountKey:            testCountValue,
	}
	ev := audit.CircuitStateChangeAuditEvent(testToolID, testStateOpen, meta)

	require.NotNil(t, ev)
	assert.Equal(t, mcp.AuditEventTypeCircuitStateChange, ev.Type)
	assert.Equal(t, testToolID, ev.ToolID)
	assert.Equal(t, testStateOpen, ev.Outcome)
	assert.NotZero(t, ev.Timestamp)
	assert.Equal(t, meta, ev.Metadata)

	ev = audit.CircuitStateChangeAuditEvent(testToolIDClosed, testStateClosed, nil)

	require.NotNil(t, ev)
	assert.Nil(t, ev.Metadata)
}
