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

func TestCompositeExecutorFactory_Create(t *testing.T) {
	factory := NewCompositeExecutorFactory(time.Second, nil)
	require.NotNil(t, factory)

	t.Run("unknown transport returns nil executor", func(t *testing.T) {
		tool := mcp.Tool{
			ID:        "unknown-tool",
			Transport: "mqtt",
		}
		exec, err := factory.Create(context.Background(), tool, nil)
		require.NoError(t, err)
		assert.Nil(t, exec)
	})

	t.Run("grpc tool without config returns nil", func(t *testing.T) {
		tool := mcp.Tool{
			ID:        "grpc-no-cfg",
			Transport: mcp.TransportGRPC,
			GRPC:      nil,
		}
		exec, err := factory.Create(context.Background(), tool, nil)
		require.NoError(t, err)
		assert.Nil(t, exec)
	})

	t.Run("grpc tool with invalid descriptor returns error", func(t *testing.T) {
		tool := mcp.Tool{
			ID:        "grpc-bad",
			Transport: mcp.TransportGRPC,
			GRPC: &mcp.GRPCTransportConfig{
				Target:        "127.0.0.1:1",
				Service:       "pkg.Svc",
				DescriptorSet: "/nonexistent/file.binpb",
			},
		}
		exec, err := factory.Create(context.Background(), tool, nil)
		assert.Nil(t, exec)
		require.Error(t, err)
		var rErr *mcp.RuntimeError
		require.ErrorAs(t, err, &rErr)
		assert.Equal(t, mcp.ErrCodeTransportFailure, rErr.Code)
	})

	t.Run("empty transport returns nil executor", func(t *testing.T) {
		tool := mcp.Tool{ID: "empty-tool"}
		exec, err := factory.Create(context.Background(), tool, nil)
		require.NoError(t, err)
		assert.Nil(t, exec)
	})

	t.Run("stdio tool without config returns nil", func(t *testing.T) {
		tool := mcp.Tool{
			ID:        "stdio-no-cfg",
			Transport: mcp.TransportStdio,
			Stdio:     nil,
		}
		exec, err := factory.Create(context.Background(), tool, nil)
		require.NoError(t, err)
		assert.Nil(t, exec)
	})

	t.Run("http tool without config returns nil", func(t *testing.T) {
		tool := mcp.Tool{
			ID:        "http-no-cfg",
			Transport: mcp.TransportHTTP,
			HTTP:      nil,
		}
		exec, err := factory.Create(context.Background(), tool, nil)
		require.NoError(t, err)
		assert.Nil(t, exec)
	})

	t.Run("stdio tool with invalid command returns error", func(t *testing.T) {
		tool := mcp.Tool{
			ID:        "stdio-bad",
			Transport: mcp.TransportStdio,
			Stdio:     &mcp.StdioTransportConfig{Command: "/nonexistent/binary/xyz"},
		}
		exec, err := factory.Create(context.Background(), tool, nil)
		assert.Nil(t, exec)
		require.Error(t, err)
		var rErr *mcp.RuntimeError
		require.ErrorAs(t, err, &rErr)
		assert.Equal(t, mcp.ErrCodeTransportFailure, rErr.Code)
	})

	t.Run("http tool with unreachable URL returns error", func(t *testing.T) {
		tool := mcp.Tool{
			ID:        "http-bad",
			Transport: mcp.TransportHTTP,
			HTTP:      &mcp.HTTPTransportConfig{URL: "http://127.0.0.1:1/unreachable"},
		}
		exec, err := factory.Create(context.Background(), tool, nil)
		assert.Nil(t, exec)
		require.Error(t, err)
		var rErr *mcp.RuntimeError
		require.ErrorAs(t, err, &rErr)
		assert.Equal(t, mcp.ErrCodeTransportFailure, rErr.Code)
	})
}

func TestNewCompositeExecutorFactory_DefaultTimeout(t *testing.T) {
	factory := NewCompositeExecutorFactory(0, nil)
	require.NotNil(t, factory)
}

func TestCompositeExecutorFactory_ImplementsInterface(t *testing.T) {
	var _ mcp.ExecutorFactory = (*CompositeExecutorFactory)(nil)
}
