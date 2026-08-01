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
	"github.com/stretchr/testify/require"

	"github.com/tochemey/mcgate/mcp"
)

func TestValidateTool(t *testing.T) {
	t.Run("valid stdio tool", func(t *testing.T) {
		tool := mcp.Tool{
			ID:        "fs",
			Transport: mcp.TransportStdio,
			Stdio:     &mcp.StdioTransportConfig{Command: "npx"},
		}
		assert.NoError(t, mcp.ValidateTool(tool))
	})

	t.Run("valid http tool", func(t *testing.T) {
		tool := mcp.Tool{
			ID:        "http-tool",
			Transport: mcp.TransportHTTP,
			HTTP:      &mcp.HTTPTransportConfig{URL: "http://localhost"},
		}
		assert.NoError(t, mcp.ValidateTool(tool))
	})

	t.Run("missing ID", func(t *testing.T) {
		tool := mcp.Tool{Transport: mcp.TransportStdio, Stdio: &mcp.StdioTransportConfig{Command: "x"}}
		err := mcp.ValidateTool(tool)
		require.Error(t, err)
		var rErr *mcp.RuntimeError
		require.ErrorAs(t, err, &rErr)
		assert.Equal(t, mcp.ErrCodeInvalidRequest, rErr.Code)
	})

	t.Run("stdio missing command", func(t *testing.T) {
		tool := mcp.Tool{ID: "t", Transport: mcp.TransportStdio, Stdio: &mcp.StdioTransportConfig{Command: ""}}
		err := mcp.ValidateTool(tool)
		require.Error(t, err)
	})

	t.Run("stdio nil config", func(t *testing.T) {
		tool := mcp.Tool{ID: "t", Transport: mcp.TransportStdio, Stdio: nil}
		err := mcp.ValidateTool(tool)
		require.Error(t, err)
	})

	t.Run("http missing URL", func(t *testing.T) {
		tool := mcp.Tool{ID: "t", Transport: mcp.TransportHTTP, HTTP: &mcp.HTTPTransportConfig{URL: ""}}
		err := mcp.ValidateTool(tool)
		require.Error(t, err)
	})

	t.Run("http nil config", func(t *testing.T) {
		tool := mcp.Tool{ID: "t", Transport: mcp.TransportHTTP, HTTP: nil}
		err := mcp.ValidateTool(tool)
		require.Error(t, err)
	})

	t.Run("valid grpc tool with descriptor set", func(t *testing.T) {
		tool := mcp.Tool{
			ID:        "grpc-tool",
			Transport: mcp.TransportGRPC,
			GRPC: &mcp.GRPCTransportConfig{
				Target:        "localhost:50051",
				Service:       "pkg.MyService",
				DescriptorSet: "/path/to/descriptors.binpb",
			},
		}
		assert.NoError(t, mcp.ValidateTool(tool))
	})

	t.Run("valid grpc tool with reflection", func(t *testing.T) {
		tool := mcp.Tool{
			ID:        "grpc-tool",
			Transport: mcp.TransportGRPC,
			GRPC: &mcp.GRPCTransportConfig{
				Target:     "localhost:50051",
				Service:    "pkg.MyService",
				Reflection: true,
			},
		}
		assert.NoError(t, mcp.ValidateTool(tool))
	})

	t.Run("grpc nil config", func(t *testing.T) {
		tool := mcp.Tool{ID: "t", Transport: mcp.TransportGRPC, GRPC: nil}
		err := mcp.ValidateTool(tool)
		require.Error(t, err)
		var rErr *mcp.RuntimeError
		require.ErrorAs(t, err, &rErr)
		assert.Equal(t, mcp.ErrCodeInvalidRequest, rErr.Code)
	})

	t.Run("grpc empty target", func(t *testing.T) {
		tool := mcp.Tool{
			ID:        "t",
			Transport: mcp.TransportGRPC,
			GRPC:      &mcp.GRPCTransportConfig{Target: "", Service: "svc", DescriptorSet: "/f"},
		}
		err := mcp.ValidateTool(tool)
		require.Error(t, err)
	})

	t.Run("grpc empty service", func(t *testing.T) {
		tool := mcp.Tool{
			ID:        "t",
			Transport: mcp.TransportGRPC,
			GRPC:      &mcp.GRPCTransportConfig{Target: "host:50051", Service: "", DescriptorSet: "/f"},
		}
		err := mcp.ValidateTool(tool)
		require.Error(t, err)
	})

	t.Run("grpc both descriptor set and reflection", func(t *testing.T) {
		tool := mcp.Tool{
			ID:        "t",
			Transport: mcp.TransportGRPC,
			GRPC: &mcp.GRPCTransportConfig{
				Target:        "host:50051",
				Service:       "svc",
				DescriptorSet: "/f",
				Reflection:    true,
			},
		}
		err := mcp.ValidateTool(tool)
		require.Error(t, err)
		var rErr *mcp.RuntimeError
		require.ErrorAs(t, err, &rErr)
		assert.Contains(t, rErr.Message, "not both")
	})

	t.Run("grpc neither descriptor set nor reflection", func(t *testing.T) {
		tool := mcp.Tool{
			ID:        "t",
			Transport: mcp.TransportGRPC,
			GRPC:      &mcp.GRPCTransportConfig{Target: "host:50051", Service: "svc"},
		}
		err := mcp.ValidateTool(tool)
		require.Error(t, err)
		var rErr *mcp.RuntimeError
		require.ErrorAs(t, err, &rErr)
		assert.Contains(t, rErr.Message, "either DescriptorSet or Reflection")
	})

	t.Run("unknown transport", func(t *testing.T) {
		tool := mcp.Tool{ID: "t", Transport: "mqtt"}
		err := mcp.ValidateTool(tool)
		require.Error(t, err)
		var rErr *mcp.RuntimeError
		require.ErrorAs(t, err, &rErr)
		assert.Equal(t, mcp.ErrCodeInvalidRequest, rErr.Code)
	})
}
