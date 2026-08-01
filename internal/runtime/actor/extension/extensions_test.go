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

package extension

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tochemey/goakt/v4/eventstream"

	"github.com/tochemey/mcgate/mcp"
)

func TestExecutorFactoryExtension(t *testing.T) {
	t.Run("ID returns fixed identifier", func(t *testing.T) {
		ext := NewExecutorFactoryExtension(nil)
		assert.Equal(t, ExecutorFactoryExtensionID, ext.ID())
	})

	t.Run("Factory returns nil when nil factory provided", func(t *testing.T) {
		ext := NewExecutorFactoryExtension(nil)
		assert.Nil(t, ext.Factory())
	})
}

func stdioTool(id mcp.ToolID) mcp.Tool {
	return mcp.Tool{
		ID:        id,
		Transport: mcp.TransportStdio,
		Stdio:     &mcp.StdioTransportConfig{Command: "npx"},
		State:     mcp.ToolStateEnabled,
	}
}

func TestCircuitConfigExtension(t *testing.T) {
	cfg := mcp.CircuitConfig{
		FailureThreshold:    3,
		OpenDuration:        5 * time.Second,
		HalfOpenMaxRequests: 1,
	}

	t.Run("ID returns CircuitConfigExtensionID", func(t *testing.T) {
		ext := NewCircuitConfigExtension(cfg)
		assert.Equal(t, CircuitConfigExtensionID, ext.ID())
	})

	t.Run("Config returns the wrapped config", func(t *testing.T) {
		ext := NewCircuitConfigExtension(cfg)
		assert.Equal(t, cfg, ext.Config())
	})
}

func TestSessionDependency(t *testing.T) {
	tenantID := mcp.TenantID("tenant-1")
	clientID := mcp.ClientID("client-1")
	toolID := mcp.ToolID("tool-1")
	tool := stdioTool(toolID)

	dep := NewSessionDependency(tenantID, clientID, toolID, tool, nil)

	t.Run("ID returns SessionDependencyID", func(t *testing.T) {
		assert.Equal(t, SessionDependencyID, dep.ID())
	})

	t.Run("accessors return correct values", func(t *testing.T) {
		assert.Equal(t, tenantID, dep.TenantID())
		assert.Equal(t, clientID, dep.ClientID())
		assert.Equal(t, toolID, dep.ToolID())
		assert.Equal(t, tool, dep.Tool())
	})

	t.Run("MarshalBinary and UnmarshalBinary round-trip", func(t *testing.T) {
		data, err := dep.MarshalBinary()
		require.NoError(t, err)
		require.NotEmpty(t, data)

		restored := &SessionDependency{}
		require.NoError(t, restored.UnmarshalBinary(data))
		assert.Equal(t, tenantID, restored.TenantID())
		assert.Equal(t, clientID, restored.ClientID())
		assert.Equal(t, toolID, restored.ToolID())
		assert.Equal(t, tool.ID, restored.Tool().ID)
	})

	t.Run("UnmarshalBinary with invalid data returns error", func(t *testing.T) {
		bad := &SessionDependency{}
		err := bad.UnmarshalBinary([]byte("not-valid-gob"))
		require.Error(t, err)
	})
}

func TestConfigExtension(t *testing.T) {
	conf := mcp.Config{}
	ext := NewConfigExtension(conf)
	require.NotNil(t, ext)
	require.Equal(t, ConfigExtensionID, ext.ID())
	require.Equal(t, conf, ext.Config())
}

func TestAuditStreamExtension(t *testing.T) {
	t.Run("ID returns AuditStreamExtensionID", func(t *testing.T) {
		ext := NewAuditStreamExtension(nil)
		assert.Equal(t, AuditStreamExtensionID, ext.ID())
	})

	t.Run("Stream returns the wrapped stream", func(t *testing.T) {
		stream := eventstream.New()
		t.Cleanup(stream.Close)

		ext := NewAuditStreamExtension(stream)

		assert.Same(t, stream, ext.Stream())
	})

	t.Run("Stream returns nil when nil stream provided", func(t *testing.T) {
		ext := NewAuditStreamExtension(nil)

		assert.Nil(t, ext.Stream())
	})

	t.Run("AuditStreamTopic is the documented topic name", func(t *testing.T) {
		assert.Equal(t, "mcp.audit", AuditStreamTopic)
	})
}

func TestSchemaFetcherExtension(t *testing.T) {
	fetcher := &mockSchemaFetcher{}
	ext := NewSchemaFetcherExtension(fetcher)
	require.NotNil(t, ext)
	assert.Equal(t, SchemaFetcherExtensionID, ext.ID())
	assert.Equal(t, fetcher, ext.Fetcher())
}

func TestSessionDependencyCredentials(t *testing.T) {
	creds := map[string]string{"api-key": "secret"}
	dep := NewSessionDependency("t1", "c1", "tool1", stdioTool("tool1"), creds)
	assert.Equal(t, creds, dep.Credentials())

	t.Run("credentials are defensively copied", func(t *testing.T) {
		original := map[string]string{"key": "val"}
		dep := NewSessionDependency("t1", "c1", "tool1", stdioTool("tool1"), original)
		// Mutate the original map after construction
		original["key"] = "mutated"
		assert.Equal(t, "val", dep.Credentials()["key"], "dependency should not be affected by external mutation")
	})

	t.Run("nil credentials remain nil", func(t *testing.T) {
		dep := NewSessionDependency("t1", "c1", "tool1", stdioTool("tool1"), nil)
		assert.Nil(t, dep.Credentials())
	})

	t.Run("credentials survive MarshalBinary/UnmarshalBinary round-trip", func(t *testing.T) {
		creds := map[string]string{"api-key": "secret", "token": "abc123"}
		dep := NewSessionDependency("t1", "c1", "tool1", stdioTool("tool1"), creds)

		data, err := dep.MarshalBinary()
		require.NoError(t, err)

		restored := &SessionDependency{}
		require.NoError(t, restored.UnmarshalBinary(data))
		assert.Equal(t, creds, restored.Credentials())
	})
}

// mockSchemaFetcher is a test SchemaFetcher.
type mockSchemaFetcher struct{}

func (m *mockSchemaFetcher) FetchSchemas(_ context.Context, _ mcp.Tool) ([]mcp.ToolSchema, error) {
	return nil, nil
}
