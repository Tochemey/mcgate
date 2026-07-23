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
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tochemey/portcullis/mcp"
)

// allowAllEvaluator is a test implementation of PolicyEvaluator that permits every request.
type allowAllEvaluator struct{}

func (a *allowAllEvaluator) Evaluate(_ context.Context, _ mcp.PolicyInput) *mcp.RuntimeError {
	return nil
}

// denyAllEvaluator is a test implementation of PolicyEvaluator that rejects every request.
type denyAllEvaluator struct {
	reason string
}

func (d *denyAllEvaluator) Evaluate(_ context.Context, _ mcp.PolicyInput) *mcp.RuntimeError {
	return mcp.NewRuntimeError(mcp.ErrCodePolicyDenied, d.reason)
}

// inputCapturingEvaluator captures the PolicyInput it receives for assertion.
type inputCapturingEvaluator struct {
	captured *mcp.PolicyInput
}

func (c *inputCapturingEvaluator) Evaluate(_ context.Context, input mcp.PolicyInput) *mcp.RuntimeError {
	c.captured = &input
	return nil
}

func TestPolicyEvaluatorInterface(t *testing.T) {
	t.Run("allow evaluator returns nil", func(t *testing.T) {
		var ev mcp.PolicyEvaluator = &allowAllEvaluator{}
		result := ev.Evaluate(context.Background(), mcp.PolicyInput{
			TenantID: "tenant-1",
			ToolID:   "tool-1",
		})
		assert.Nil(t, result)
	})

	t.Run("deny evaluator returns RuntimeError with policy denied code", func(t *testing.T) {
		var ev mcp.PolicyEvaluator = &denyAllEvaluator{reason: "not allowed by custom policy"}
		result := ev.Evaluate(context.Background(), mcp.PolicyInput{
			TenantID: "tenant-1",
			ToolID:   "tool-1",
		})
		require.NotNil(t, result)
		assert.Equal(t, mcp.ErrCodePolicyDenied, result.Code)
		assert.Equal(t, "not allowed by custom policy", result.Message)
	})

	t.Run("evaluator receives the input fields", func(t *testing.T) {
		capturing := &inputCapturingEvaluator{}
		var ev mcp.PolicyEvaluator = capturing
		input := mcp.PolicyInput{
			TenantID:                "tenant-a",
			ToolID:                  "weather-tool",
			ActiveSessionCount:      5,
			RequestsInCurrentMinute: 42,
		}
		result := ev.Evaluate(context.Background(), input)
		assert.Nil(t, result)
		require.NotNil(t, capturing.captured)
		assert.Equal(t, mcp.TenantID("tenant-a"), capturing.captured.TenantID)
		assert.Equal(t, mcp.ToolID("weather-tool"), capturing.captured.ToolID)
		assert.Equal(t, 5, capturing.captured.ActiveSessionCount)
		assert.Equal(t, 42, capturing.captured.RequestsInCurrentMinute)
	})
}

func TestPolicyInput(t *testing.T) {
	t.Run("basic fields", func(t *testing.T) {
		in := mcp.PolicyInput{
			TenantID:                "tenant-a",
			ToolID:                  "weather-tool",
			ActiveSessionCount:      5,
			RequestsInCurrentMinute: 42,
		}
		assert.Equal(t, mcp.TenantID("tenant-a"), in.TenantID)
		assert.Equal(t, mcp.ToolID("weather-tool"), in.ToolID)
		assert.Equal(t, 5, in.ActiveSessionCount)
		assert.Equal(t, 42, in.RequestsInCurrentMinute)
	})

	t.Run("scopes field is settable", func(t *testing.T) {
		in := mcp.PolicyInput{
			TenantID: "tenant-a",
			ToolID:   "tool-1",
			Scopes:   []string{"tools:read", "tools:write"},
		}
		assert.Equal(t, []string{"tools:read", "tools:write"}, in.Scopes)
	})

	t.Run("scopes field defaults to nil", func(t *testing.T) {
		in := mcp.PolicyInput{}
		assert.Nil(t, in.Scopes)
	})
}

func TestPolicyEvaluator_ReceivesScopes(t *testing.T) {
	capturing := &inputCapturingEvaluator{}
	var ev mcp.PolicyEvaluator = capturing
	input := mcp.PolicyInput{
		TenantID: "tenant-a",
		ToolID:   "tool-1",
		Scopes:   []string{"tools:read", "admin"},
	}
	result := ev.Evaluate(context.Background(), input)
	assert.Nil(t, result)
	require.NotNil(t, capturing.captured)
	assert.Equal(t, []string{"tools:read", "admin"}, capturing.captured.Scopes)
}
