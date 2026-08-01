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

package grpc_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tochemey/mcgate/mcp"

	egressgrpc "github.com/tochemey/mcgate/internal/egress/grpc"
	"github.com/tochemey/mcgate/internal/egress/grpc/testdata"
)

func TestFetchSchemas(t *testing.T) {
	t.Run("nil config returns error", func(t *testing.T) {
		schemas, err := egressgrpc.FetchSchemas(context.Background(), nil, 5*time.Second)
		require.Error(t, err)
		assert.Nil(t, schemas)
	})

	t.Run("unreachable backend returns error", func(t *testing.T) {
		cfg := &mcp.GRPCTransportConfig{
			Target:        "127.0.0.1:1",
			Service:       "testpkg.TestService",
			DescriptorSet: testDescriptorSetPath(t),
		}
		schemas, err := egressgrpc.FetchSchemas(context.Background(), cfg, 2*time.Second)
		require.Error(t, err)
		assert.Nil(t, schemas)
	})

	t.Run("descriptor set mode returns all method schemas", func(t *testing.T) {
		addr, cleanup, err := testdata.StartTestServer(false)
		require.NoError(t, err)
		defer cleanup()

		absPath, err := filepath.Abs(testDescriptorSetPath(t))
		require.NoError(t, err)

		cfg := &mcp.GRPCTransportConfig{
			Target:        addr,
			Service:       "testpkg.TestService",
			DescriptorSet: absPath,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		schemas, err := egressgrpc.FetchSchemas(ctx, cfg, 5*time.Second)
		require.NoError(t, err)
		require.Len(t, schemas, 2)

		names := make([]string, len(schemas))
		for i, s := range schemas {
			names[i] = s.Name
		}
		assert.Contains(t, names, "Echo")
		assert.Contains(t, names, "StreamEcho")

		// Verify the input schema structure for Echo
		for _, s := range schemas {
			if s.Name == "Echo" {
				var schema map[string]any
				require.NoError(t, json.Unmarshal(s.InputSchema, &schema))
				assert.Equal(t, "object", schema["type"])
				props, ok := schema["properties"].(map[string]any)
				require.True(t, ok)
				assert.Contains(t, props, "message")
				assert.Contains(t, props, "count")

				// Verify field types
				msgProp, ok := props["message"].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, "string", msgProp["type"])

				countProp, ok := props["count"].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, "integer", countProp["type"])
			}
		}
	})

	t.Run("descriptor set mode with specific method returns single schema", func(t *testing.T) {
		addr, cleanup, err := testdata.StartTestServer(false)
		require.NoError(t, err)
		defer cleanup()

		absPath, err := filepath.Abs(testDescriptorSetPath(t))
		require.NoError(t, err)

		cfg := &mcp.GRPCTransportConfig{
			Target:        addr,
			Service:       "testpkg.TestService",
			Method:        "Echo",
			DescriptorSet: absPath,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		schemas, err := egressgrpc.FetchSchemas(ctx, cfg, 5*time.Second)
		require.NoError(t, err)
		require.Len(t, schemas, 1)
		assert.Equal(t, "Echo", schemas[0].Name)
	})

	t.Run("reflection mode returns schemas", func(t *testing.T) {
		addr, cleanup, err := testdata.StartTestServer(true)
		require.NoError(t, err)
		defer cleanup()

		cfg := &mcp.GRPCTransportConfig{
			Target:     addr,
			Service:    "testpkg.TestService",
			Reflection: true,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		schemas, err := egressgrpc.FetchSchemas(ctx, cfg, 5*time.Second)
		require.NoError(t, err)
		require.Len(t, schemas, 2)
	})

	t.Run("unknown method returns error", func(t *testing.T) {
		addr, cleanup, err := testdata.StartTestServer(false)
		require.NoError(t, err)
		defer cleanup()

		absPath, err := filepath.Abs(testDescriptorSetPath(t))
		require.NoError(t, err)

		cfg := &mcp.GRPCTransportConfig{
			Target:        addr,
			Service:       "testpkg.TestService",
			Method:        "NonExistent",
			DescriptorSet: absPath,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		schemas, err := egressgrpc.FetchSchemas(ctx, cfg, 5*time.Second)
		require.Error(t, err)
		assert.Nil(t, schemas)
	})

	t.Run("unknown service returns error", func(t *testing.T) {
		addr, cleanup, err := testdata.StartTestServer(false)
		require.NoError(t, err)
		defer cleanup()

		absPath, err := filepath.Abs(testDescriptorSetPath(t))
		require.NoError(t, err)

		cfg := &mcp.GRPCTransportConfig{
			Target:        addr,
			Service:       "nonexistent.Service",
			DescriptorSet: absPath,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		schemas, err := egressgrpc.FetchSchemas(ctx, cfg, 5*time.Second)
		require.Error(t, err)
		assert.Nil(t, schemas)
	})

	t.Run("rich service schema covers all field types", func(t *testing.T) {
		addr, cleanup, err := testdata.StartTestServer(false)
		require.NoError(t, err)
		defer cleanup()

		absPath, err := filepath.Abs(testDescriptorSetPath(t))
		require.NoError(t, err)

		cfg := &mcp.GRPCTransportConfig{
			Target:        addr,
			Service:       "testpkg.RichService",
			Method:        "Process",
			DescriptorSet: absPath,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		schemas, err := egressgrpc.FetchSchemas(ctx, cfg, 5*time.Second)
		require.NoError(t, err)
		require.Len(t, schemas, 1)
		assert.Equal(t, "Process", schemas[0].Name)

		var schema map[string]any
		require.NoError(t, json.Unmarshal(schemas[0].InputSchema, &schema))
		assert.Equal(t, "object", schema["type"])

		props, ok := schema["properties"].(map[string]any)
		require.True(t, ok)

		// string field
		nameProp, ok := props["name"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "string", nameProp["type"])

		// int32 field
		ageProp, ok := props["age"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "integer", ageProp["type"])

		// int64 field (represented as string in JSON)
		bigNumProp, ok := props["bigNumber"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "string", bigNumProp["type"])

		// bool field
		activeProp, ok := props["active"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "boolean", activeProp["type"])

		// double field
		scoreProp, ok := props["score"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "number", scoreProp["type"])

		// float field
		ratioProp, ok := props["ratio"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "number", ratioProp["type"])

		// bytes field (represented as string)
		payloadProp, ok := props["payload"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "string", payloadProp["type"])

		// repeated string field
		tagsProp, ok := props["tags"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "array", tagsProp["type"])
		items, ok := tagsProp["items"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "string", items["type"])

		// map<string,string> field
		labelsProp, ok := props["labels"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "object", labelsProp["type"])
		addlProps, ok := labelsProp["additionalProperties"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "string", addlProps["type"])

		// nested message field
		addrProp, ok := props["address"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "object", addrProp["type"])
		addrProps, ok := addrProp["properties"].(map[string]any)
		require.True(t, ok)
		assert.Contains(t, addrProps, "street")
		assert.Contains(t, addrProps, "city")
		assert.Contains(t, addrProps, "zip")

		// enum field (represented as string)
		statusProp, ok := props["status"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "string", statusProp["type"])
	})

	t.Run("self-referencing message does not infinite loop", func(t *testing.T) {
		addr, cleanup, err := testdata.StartTestServer(false)
		require.NoError(t, err)
		defer cleanup()

		absPath, err := filepath.Abs(testDescriptorSetPath(t))
		require.NoError(t, err)

		cfg := &mcp.GRPCTransportConfig{
			Target:        addr,
			Service:       "testpkg.SelfRefService",
			Method:        "Traverse",
			DescriptorSet: absPath,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		schemas, err := egressgrpc.FetchSchemas(ctx, cfg, 5*time.Second)
		require.NoError(t, err)
		require.Len(t, schemas, 1)

		var schema map[string]any
		require.NoError(t, json.Unmarshal(schemas[0].InputSchema, &schema))
		assert.Equal(t, "object", schema["type"])

		props, ok := schema["properties"].(map[string]any)
		require.True(t, ok)
		assert.Contains(t, props, "value")
		assert.Contains(t, props, "left")
		assert.Contains(t, props, "right")

		// The recursive fields should be truncated to {"type": "object"}
		leftProp, ok := props["left"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "object", leftProp["type"])
	})

	t.Run("zero startup timeout uses context deadline", func(t *testing.T) {
		addr, cleanup, err := testdata.StartTestServer(false)
		require.NoError(t, err)
		defer cleanup()

		absPath, err := filepath.Abs(testDescriptorSetPath(t))
		require.NoError(t, err)

		cfg := &mcp.GRPCTransportConfig{
			Target:        addr,
			Service:       "testpkg.TestService",
			DescriptorSet: absPath,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		schemas, err := egressgrpc.FetchSchemas(ctx, cfg, 0)
		require.NoError(t, err)
		assert.Len(t, schemas, 2)
	})
}
