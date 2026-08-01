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

	"github.com/tochemey/mcgate/mcp"
)

func TestToolStatus(t *testing.T) {
	t.Run("zero value", func(t *testing.T) {
		var s mcp.ToolStatus
		assert.Empty(t, s.ToolID)
		assert.Empty(t, s.State)
		assert.Empty(t, s.Circuit)
		assert.Zero(t, s.SessionCount)
		assert.False(t, s.Draining)
	})

	t.Run("populated fields", func(t *testing.T) {
		s := mcp.ToolStatus{
			ToolID:       "my-tool",
			State:        mcp.ToolStateEnabled,
			Circuit:      mcp.CircuitClosed,
			SessionCount: 3,
			Draining:     true,
		}
		assert.Equal(t, mcp.ToolID("my-tool"), s.ToolID)
		assert.Equal(t, mcp.ToolStateEnabled, s.State)
		assert.Equal(t, mcp.CircuitClosed, s.Circuit)
		assert.Equal(t, 3, s.SessionCount)
		assert.True(t, s.Draining)
	})

	t.Run("disabled and open circuit", func(t *testing.T) {
		s := mcp.ToolStatus{
			ToolID:  "broken-tool",
			State:   mcp.ToolStateDisabled,
			Circuit: mcp.CircuitOpen,
		}
		assert.Equal(t, mcp.ToolID("broken-tool"), s.ToolID)
		assert.Equal(t, mcp.ToolStateDisabled, s.State)
		assert.Equal(t, mcp.CircuitOpen, s.Circuit)
		assert.False(t, s.Draining)
	})
}

func TestGatewayStatus(t *testing.T) {
	t.Run("not running", func(t *testing.T) {
		s := mcp.GatewayStatus{}
		assert.False(t, s.Running)
		assert.Zero(t, s.ToolCount)
		assert.Zero(t, s.SessionCount)
	})

	t.Run("running with tools and sessions", func(t *testing.T) {
		s := mcp.GatewayStatus{Running: true, ToolCount: 5, SessionCount: 12}
		assert.True(t, s.Running)
		assert.Equal(t, 5, s.ToolCount)
		assert.Equal(t, 12, s.SessionCount)
	})
}

func TestSessionInfo(t *testing.T) {
	s := mcp.SessionInfo{
		Name:     "session-tenant1-client1-tool1",
		ToolID:   "tool1",
		TenantID: "tenant1",
		ClientID: "client1",
	}
	assert.Equal(t, "session-tenant1-client1-tool1", s.Name)
	assert.Equal(t, mcp.ToolID("tool1"), s.ToolID)
	assert.Equal(t, mcp.TenantID("tenant1"), s.TenantID)
	assert.Equal(t, mcp.ClientID("client1"), s.ClientID)
}
