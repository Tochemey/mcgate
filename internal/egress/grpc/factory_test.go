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
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	egressgrpc "github.com/tochemey/mcgate/internal/egress/grpc"
	"github.com/tochemey/mcgate/internal/egress/grpc/testdata"
	"github.com/tochemey/mcgate/mcp"
)

func TestNewGRPCExecutorFactory(t *testing.T) {
	t.Run("uses provided timeout", func(t *testing.T) {
		f := egressgrpc.NewGRPCExecutorFactory(5 * time.Second)
		require.NotNil(t, f)
	})

	t.Run("defaults timeout when zero", func(t *testing.T) {
		f := egressgrpc.NewGRPCExecutorFactory(0)
		require.NotNil(t, f)
	})

	t.Run("defaults timeout when negative", func(t *testing.T) {
		f := egressgrpc.NewGRPCExecutorFactory(-1)
		require.NotNil(t, f)
	})
}

func TestGRPCExecutorFactory_Create(t *testing.T) {
	factory := egressgrpc.NewGRPCExecutorFactory(5 * time.Second)

	t.Run("non-grpc tool returns nil", func(t *testing.T) {
		tool := mcp.Tool{
			ID:        "http-tool",
			Transport: mcp.TransportHTTP,
			HTTP:      &mcp.HTTPTransportConfig{URL: "http://localhost"},
		}
		exec, err := factory.Create(context.Background(), tool, nil)
		require.NoError(t, err)
		assert.Nil(t, exec)
	})

	t.Run("grpc tool with nil config returns nil", func(t *testing.T) {
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
	})

	t.Run("grpc tool with valid config creates executor", func(t *testing.T) {
		addr, cleanup, err := testdata.StartTestServer(false)
		require.NoError(t, err)
		defer cleanup()

		absPath, err := filepath.Abs(testDescriptorSetPath(t))
		require.NoError(t, err)

		tool := mcp.Tool{
			ID:        "grpc-good",
			Transport: mcp.TransportGRPC,
			GRPC: &mcp.GRPCTransportConfig{
				Target:        addr,
				Service:       "testpkg.TestService",
				Method:        "Echo",
				DescriptorSet: absPath,
			},
		}
		exec, err := factory.Create(context.Background(), tool, nil)
		require.NoError(t, err)
		require.NotNil(t, exec)
		require.NoError(t, exec.Close())
	})
}
