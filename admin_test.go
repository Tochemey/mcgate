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

package portcullis

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tochemey/portcullis/mcp"
)

// newStartedGatewayWithTools starts a gateway with the given tools pre-registered
// and returns a cleanup function. Uses DiscardLogger to suppress test output.
func newStartedGatewayWithTools(t *testing.T, tools ...mcp.Tool) (*Gateway, context.Context, func()) {
	t.Helper()
	ctx := context.Background()
	cfg := testConfig()
	cfg.Tools = tools
	gw, err := New(cfg)
	require.NoError(t, err)
	require.NoError(t, gw.Start(ctx))
	waitForActors()
	return gw, ctx, func() {
		require.NoError(t, gw.Stop(ctx))
	}
}

// adminTool returns a valid stdio tool for admin tests.
func adminTool(id mcp.ToolID) mcp.Tool {
	return mcp.Tool{
		ID:        id,
		Transport: mcp.TransportStdio,
		Stdio:     &mcp.StdioTransportConfig{Command: "npx"},
		State:     mcp.ToolStateEnabled,
	}
}

func TestGetToolStatus(t *testing.T) {
	tool := adminTool("status-tool")
	gw, ctx, stop := newStartedGatewayWithTools(t, tool)
	defer stop()

	t.Run("returns status for registered tool", func(t *testing.T) {
		status, err := gw.GetToolStatus(ctx, tool.ID)
		require.NoError(t, err)
		require.NotNil(t, status)
		assert.Equal(t, tool.ID, status.ToolID)
		assert.Equal(t, mcp.ToolStateEnabled, status.State)
		assert.Equal(t, mcp.CircuitClosed, status.Circuit)
		assert.Zero(t, status.SessionCount)
		assert.False(t, status.Draining)
	})

	t.Run("returns error for unknown tool", func(t *testing.T) {
		_, err := gw.GetToolStatus(ctx, "nonexistent-tool")
		require.Error(t, err)
	})

	t.Run("returns error for empty tool ID", func(t *testing.T) {
		_, err := gw.GetToolStatus(ctx, "")
		require.Error(t, err)
	})

	t.Run("returns error when gateway not started", func(t *testing.T) {
		gw2, err := New(testConfig())
		require.NoError(t, err)
		_, err = gw2.GetToolStatus(ctx, "any-tool")
		require.Error(t, err)
	})
}

func TestResetCircuit(t *testing.T) {
	tool := adminTool("circuit-tool")
	gw, ctx, stop := newStartedGatewayWithTools(t, tool)
	defer stop()

	t.Run("resets circuit on registered tool", func(t *testing.T) {
		err := gw.ResetCircuit(ctx, tool.ID)
		require.NoError(t, err)

		status, err := gw.GetToolStatus(ctx, tool.ID)
		require.NoError(t, err)
		assert.Equal(t, mcp.CircuitClosed, status.Circuit)
	})

	t.Run("returns error for unknown tool", func(t *testing.T) {
		err := gw.ResetCircuit(ctx, "unknown-tool")
		require.Error(t, err)
	})

	t.Run("returns error for empty tool ID", func(t *testing.T) {
		err := gw.ResetCircuit(ctx, "")
		require.Error(t, err)
	})

	t.Run("returns error when gateway not started", func(t *testing.T) {
		gw2, err := New(testConfig())
		require.NoError(t, err)
		err = gw2.ResetCircuit(ctx, tool.ID)
		require.Error(t, err)
	})
}

func TestDrainTool(t *testing.T) {
	tool := adminTool("drain-tool")
	gw, ctx, stop := newStartedGatewayWithTools(t, tool)
	defer stop()

	t.Run("drains registered tool and status reflects draining", func(t *testing.T) {
		err := gw.DrainTool(ctx, tool.ID)
		require.NoError(t, err)

		status, err := gw.GetToolStatus(ctx, tool.ID)
		require.NoError(t, err)
		assert.True(t, status.Draining)
	})

	t.Run("returns error for unknown tool", func(t *testing.T) {
		err := gw.DrainTool(ctx, "unknown-tool")
		require.Error(t, err)
	})

	t.Run("returns error for empty tool ID", func(t *testing.T) {
		err := gw.DrainTool(ctx, "")
		require.Error(t, err)
	})

	t.Run("returns error when gateway not started", func(t *testing.T) {
		gw2, err := New(testConfig())
		require.NoError(t, err)
		err = gw2.DrainTool(ctx, tool.ID)
		require.Error(t, err)
	})
}

func TestGetGatewayStatus(t *testing.T) {
	tool := adminTool("gw-status-tool")
	gw, ctx, stop := newStartedGatewayWithTools(t, tool)
	defer stop()

	t.Run("returns running status with tool count", func(t *testing.T) {
		status, err := gw.GetGatewayStatus(ctx)
		require.NoError(t, err)
		require.NotNil(t, status)
		assert.True(t, status.Running)
		assert.Equal(t, 1, status.ToolCount)
		assert.Zero(t, status.SessionCount)
	})

	t.Run("returns not running for unstarted gateway", func(t *testing.T) {
		gw2, err := New(testConfig())
		require.NoError(t, err)
		status, err := gw2.GetGatewayStatus(ctx)
		require.NoError(t, err)
		require.NotNil(t, status)
		assert.False(t, status.Running)
	})
}

func TestListSessions(t *testing.T) {
	tool := adminTool("list-sessions-tool")
	gw, ctx, stop := newStartedGatewayWithTools(t, tool)
	defer stop()

	t.Run("returns empty slice when no sessions active", func(t *testing.T) {
		sessions, err := gw.ListSessions(ctx)
		require.NoError(t, err)
		assert.Empty(t, sessions)
	})

	t.Run("returns error when gateway not started", func(t *testing.T) {
		gw2, err := New(testConfig())
		require.NoError(t, err)
		_, err = gw2.ListSessions(ctx)
		require.Error(t, err)
	})
}

func TestGetToolSchema(t *testing.T) {
	tool := adminTool("schema-tool")
	gw, ctx, stop := newStartedGatewayWithTools(t, tool)
	defer stop()

	t.Run("returns schemas for registered tool without error", func(t *testing.T) {
		schemas, err := gw.GetToolSchema(ctx, tool.ID)
		require.NoError(t, err)
		// Backend is not running so schemas may be nil; the important check is
		// that the function completes without error for a known tool ID.
		_ = schemas
	})

	t.Run("returns error for unknown tool", func(t *testing.T) {
		_, err := gw.GetToolSchema(ctx, "nonexistent-tool")
		require.Error(t, err)
	})

	t.Run("returns error for empty tool ID", func(t *testing.T) {
		_, err := gw.GetToolSchema(ctx, "")
		require.Error(t, err)
		var rErr *mcp.RuntimeError
		require.ErrorAs(t, err, &rErr)
		assert.Equal(t, mcp.ErrCodeInvalidRequest, rErr.Code)
	})

	t.Run("returns error when gateway not started", func(t *testing.T) {
		gw2, err := New(testConfig())
		require.NoError(t, err)
		_, err = gw2.GetToolSchema(ctx, tool.ID)
		require.Error(t, err)
	})
}
