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

	"github.com/stretchr/testify/assert"

	"github.com/tochemey/portcullis/mcp"
)

func TestCorrelationMeta_IsZero(t *testing.T) {
	zero := mcp.CorrelationMeta{}
	assert.True(t, zero.IsZero())

	nonZero := mcp.CorrelationMeta{TenantID: "t1"}
	assert.False(t, nonZero.IsZero())
}

func TestInvocation_Scopes(t *testing.T) {
	t.Run("scopes field defaults to nil", func(t *testing.T) {
		inv := mcp.Invocation{}
		assert.Nil(t, inv.Scopes)
	})

	t.Run("scopes field is settable", func(t *testing.T) {
		inv := mcp.Invocation{
			Scopes: []string{"tools:read", "tools:write"},
		}
		assert.Equal(t, []string{"tools:read", "tools:write"}, inv.Scopes)
	})
}

func TestExecutionResult_StatusHelpers(t *testing.T) {
	cases := []struct {
		status    mcp.ExecutionStatus
		succeeded bool
		failed    bool
		timedOut  bool
		denied    bool
		throttled bool
	}{
		{mcp.ExecutionStatusSuccess, true, false, false, false, false},
		{mcp.ExecutionStatusFailure, false, true, false, false, false},
		{mcp.ExecutionStatusTimeout, false, false, true, false, false},
		{mcp.ExecutionStatusDenied, false, false, false, true, false},
		{mcp.ExecutionStatusThrottled, false, false, false, false, true},
	}
	for _, tc := range cases {
		r := mcp.ExecutionResult{Status: tc.status}
		assert.Equal(t, tc.succeeded, r.Succeeded(), "status=%s succeeded", tc.status)
		assert.Equal(t, tc.failed, r.Failed(), "status=%s failed", tc.status)
		assert.Equal(t, tc.timedOut, r.TimedOut(), "status=%s timedOut", tc.status)
		assert.Equal(t, tc.denied, r.Denied(), "status=%s denied", tc.status)
		assert.Equal(t, tc.throttled, r.Throttled(), "status=%s throttled", tc.status)
	}
}
