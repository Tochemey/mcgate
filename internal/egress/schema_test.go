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

package egress

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tochemey/mcgate/mcp"
)

func TestNewCompositeSchemaFetcher(t *testing.T) {
	t.Run("uses provided timeout", func(t *testing.T) {
		f := NewCompositeSchemaFetcher(5*time.Second, nil)
		require.NotNil(t, f)
		assert.Equal(t, 5*time.Second, f.startupTimeout)
	})

	t.Run("defaults timeout when zero", func(t *testing.T) {
		f := NewCompositeSchemaFetcher(0, nil)
		require.NotNil(t, f)
		assert.Equal(t, mcp.DefaultStartupTimeout, f.startupTimeout)
	})

	t.Run("defaults timeout when negative", func(t *testing.T) {
		f := NewCompositeSchemaFetcher(-1, nil)
		require.NotNil(t, f)
		assert.Equal(t, mcp.DefaultStartupTimeout, f.startupTimeout)
	})
}

func TestCompositeSchemaFetcher_FetchSchemas(t *testing.T) {
	f := NewCompositeSchemaFetcher(time.Second, nil)

	t.Run("unsupported transport returns error", func(t *testing.T) {
		tool := mcp.Tool{
			ID:        "unknown-tool",
			Transport: "mqtt",
		}
		schemas, err := f.FetchSchemas(context.Background(), tool)
		require.Error(t, err)
		assert.Nil(t, schemas)
		assert.Contains(t, err.Error(), "unsupported transport type")
		assert.Contains(t, err.Error(), "mqtt")
	})

	t.Run("grpc tool with nil config returns error", func(t *testing.T) {
		tool := mcp.Tool{
			ID:        "grpc-nil",
			Transport: mcp.TransportGRPC,
			GRPC:      nil,
		}
		schemas, err := f.FetchSchemas(context.Background(), tool)
		require.Error(t, err)
		assert.Nil(t, schemas)
	})

	t.Run("empty transport returns error", func(t *testing.T) {
		tool := mcp.Tool{ID: "empty-transport"}
		schemas, err := f.FetchSchemas(context.Background(), tool)
		require.Error(t, err)
		assert.Nil(t, schemas)
		assert.Contains(t, err.Error(), "unsupported transport type")
	})

	t.Run("stdio tool with invalid command returns error", func(t *testing.T) {
		tool := mcp.Tool{
			ID:        "stdio-bad",
			Transport: mcp.TransportStdio,
			Stdio:     &mcp.StdioTransportConfig{Command: "/nonexistent/binary/xyz"},
		}
		schemas, err := f.FetchSchemas(context.Background(), tool)
		require.Error(t, err)
		assert.Nil(t, schemas)
	})

	t.Run("http tool with unreachable URL returns error", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		tool := mcp.Tool{
			ID:        "http-bad",
			Transport: mcp.TransportHTTP,
			HTTP:      &mcp.HTTPTransportConfig{URL: "http://127.0.0.1:1/unreachable"},
		}
		schemas, err := f.FetchSchemas(ctx, tool)
		require.Error(t, err)
		assert.Nil(t, schemas)
	})

	t.Run("implements SchemaFetcher interface", func(t *testing.T) {
		var _ mcp.SchemaFetcher = (*CompositeSchemaFetcher)(nil)
	})
}
