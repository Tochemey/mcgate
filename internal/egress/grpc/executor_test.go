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

	egressgrpc "github.com/tochemey/portcullis/internal/egress/grpc"
	"github.com/tochemey/portcullis/internal/egress/grpc/testdata"
	"github.com/tochemey/portcullis/mcp"
)

func TestNewGRPCExecutor(t *testing.T) {
	t.Run("nil config returns error", func(t *testing.T) {
		exec, err := egressgrpc.NewGRPCExecutor(nil, 5*time.Second, nil)
		require.Error(t, err)
		assert.Nil(t, exec)
		var rErr *mcp.RuntimeError
		require.ErrorAs(t, err, &rErr)
		assert.Equal(t, mcp.ErrCodeInvalidRequest, rErr.Code)
	})

	t.Run("empty target returns error", func(t *testing.T) {
		cfg := &mcp.GRPCTransportConfig{
			Target:        "",
			Service:       "testpkg.TestService",
			DescriptorSet: testDescriptorSetPath(t),
		}
		exec, err := egressgrpc.NewGRPCExecutor(cfg, 5*time.Second, nil)
		require.Error(t, err)
		assert.Nil(t, exec)
	})

	t.Run("invalid descriptor set returns error", func(t *testing.T) {
		addr, cleanup, err := testdata.StartTestServer(false)
		require.NoError(t, err)
		defer cleanup()

		cfg := &mcp.GRPCTransportConfig{
			Target:        addr,
			Service:       "testpkg.TestService",
			DescriptorSet: "/nonexistent/file.binpb",
		}
		exec, err := egressgrpc.NewGRPCExecutor(cfg, 5*time.Second, nil)
		require.Error(t, err)
		assert.Nil(t, exec)
	})

	t.Run("creates executor with descriptor set and specific method", func(t *testing.T) {
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
		exec, err := egressgrpc.NewGRPCExecutor(cfg, 5*time.Second, nil)
		require.NoError(t, err)
		require.NotNil(t, exec)
		require.NoError(t, exec.Close())
	})

	t.Run("creates executor with descriptor set and all methods", func(t *testing.T) {
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
		exec, err := egressgrpc.NewGRPCExecutor(cfg, 5*time.Second, nil)
		require.NoError(t, err)
		require.NotNil(t, exec)
		require.NoError(t, exec.Close())
	})

	t.Run("creates executor with reflection", func(t *testing.T) {
		addr, cleanup, err := testdata.StartTestServer(true)
		require.NoError(t, err)
		defer cleanup()

		cfg := &mcp.GRPCTransportConfig{
			Target:     addr,
			Service:    "testpkg.TestService",
			Method:     "Echo",
			Reflection: true,
		}
		exec, err := egressgrpc.NewGRPCExecutor(cfg, 5*time.Second, nil)
		require.NoError(t, err)
		require.NotNil(t, exec)
		require.NoError(t, exec.Close())
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
		exec, err := egressgrpc.NewGRPCExecutor(cfg, 5*time.Second, nil)
		require.Error(t, err)
		assert.Nil(t, exec)
	})
}

func TestGRPCExecutor_Execute(t *testing.T) {
	addr, cleanup, err := testdata.StartTestServer(false)
	require.NoError(t, err)
	defer cleanup()

	absPath, err := filepath.Abs(testDescriptorSetPath(t))
	require.NoError(t, err)

	t.Run("unary call with specific method", func(t *testing.T) {
		cfg := &mcp.GRPCTransportConfig{
			Target:        addr,
			Service:       "testpkg.TestService",
			Method:        "Echo",
			DescriptorSet: absPath,
		}
		exec, err := egressgrpc.NewGRPCExecutor(cfg, 5*time.Second, nil)
		require.NoError(t, err)
		defer exec.Close()

		inv := &mcp.Invocation{
			ToolID: "test-tool",
			Params: map[string]any{
				"name": "Echo",
				"arguments": map[string]any{
					"message": "hello",
					"count":   1,
				},
			},
		}

		result, err := exec.Execute(context.Background(), inv)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, mcp.ExecutionStatusSuccess, result.Status)
		assert.NotNil(t, result.Output)
		assert.Contains(t, result.Output, "content")
	})

	t.Run("unary call with all methods mode", func(t *testing.T) {
		cfg := &mcp.GRPCTransportConfig{
			Target:        addr,
			Service:       "testpkg.TestService",
			DescriptorSet: absPath,
		}
		exec, err := egressgrpc.NewGRPCExecutor(cfg, 5*time.Second, nil)
		require.NoError(t, err)
		defer exec.Close()

		inv := &mcp.Invocation{
			ToolID: "test-tool",
			Params: map[string]any{
				"name": "Echo",
				"arguments": map[string]any{
					"message": "world",
					"count":   2,
				},
			},
		}

		result, err := exec.Execute(context.Background(), inv)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, mcp.ExecutionStatusSuccess, result.Status)
	})

	t.Run("unary call with unknown method returns failure", func(t *testing.T) {
		cfg := &mcp.GRPCTransportConfig{
			Target:        addr,
			Service:       "testpkg.TestService",
			DescriptorSet: absPath,
		}
		exec, err := egressgrpc.NewGRPCExecutor(cfg, 5*time.Second, nil)
		require.NoError(t, err)
		defer exec.Close()

		inv := &mcp.Invocation{
			ToolID: "test-tool",
			Params: map[string]any{
				"name": "NonExistent",
			},
		}

		result, err := exec.Execute(context.Background(), inv)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, mcp.ExecutionStatusFailure, result.Status)
		assert.NotNil(t, result.Err)
	})

	t.Run("unary call with nil arguments succeeds", func(t *testing.T) {
		cfg := &mcp.GRPCTransportConfig{
			Target:        addr,
			Service:       "testpkg.TestService",
			Method:        "Echo",
			DescriptorSet: absPath,
		}
		exec, err := egressgrpc.NewGRPCExecutor(cfg, 5*time.Second, nil)
		require.NoError(t, err)
		defer exec.Close()

		inv := &mcp.Invocation{
			ToolID: "test-tool",
			Params: map[string]any{
				"name": "Echo",
			},
		}

		result, err := exec.Execute(context.Background(), inv)
		require.NoError(t, err)
		assert.Equal(t, mcp.ExecutionStatusSuccess, result.Status)
	})

	t.Run("unary call with metadata", func(t *testing.T) {
		cfg := &mcp.GRPCTransportConfig{
			Target:        addr,
			Service:       "testpkg.TestService",
			Method:        "Echo",
			DescriptorSet: absPath,
			Metadata: map[string]string{
				"x-api-key": "test-key",
			},
		}
		exec, err := egressgrpc.NewGRPCExecutor(cfg, 5*time.Second, nil)
		require.NoError(t, err)
		defer exec.Close()

		inv := &mcp.Invocation{
			ToolID: "test-tool",
			Params: map[string]any{
				"name": "Echo",
				"arguments": map[string]any{
					"message": "with-metadata",
				},
			},
		}

		result, err := exec.Execute(context.Background(), inv)
		require.NoError(t, err)
		assert.Equal(t, mcp.ExecutionStatusSuccess, result.Status)
	})

	t.Run("timeout returns timeout status", func(t *testing.T) {
		cfg := &mcp.GRPCTransportConfig{
			Target:        addr,
			Service:       "testpkg.TestService",
			Method:        "Echo",
			DescriptorSet: absPath,
		}
		exec, err := egressgrpc.NewGRPCExecutor(cfg, 5*time.Second, nil)
		require.NoError(t, err)
		defer exec.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // cancel immediately

		inv := &mcp.Invocation{
			ToolID: "test-tool",
			Params: map[string]any{
				"name": "Echo",
				"arguments": map[string]any{
					"message": "timeout-test",
				},
			},
		}

		result, err := exec.Execute(ctx, inv)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, mcp.ExecutionStatusTimeout, result.Status)
	})
}

func TestGRPCExecutor_ExecuteStream(t *testing.T) {
	addr, cleanup, err := testdata.StartTestServer(false)
	require.NoError(t, err)
	defer cleanup()

	absPath, err := filepath.Abs(testDescriptorSetPath(t))
	require.NoError(t, err)

	t.Run("server streaming call", func(t *testing.T) {
		cfg := &mcp.GRPCTransportConfig{
			Target:        addr,
			Service:       "testpkg.TestService",
			DescriptorSet: absPath,
		}
		exec, err := egressgrpc.NewGRPCExecutor(cfg, 5*time.Second, nil)
		require.NoError(t, err)
		defer exec.Close()

		inv := &mcp.Invocation{
			ToolID: "test-tool",
			Params: map[string]any{
				"name": "StreamEcho",
				"arguments": map[string]any{
					"message": "streaming",
					"count":   3,
				},
			},
		}

		streamResult, err := exec.ExecuteStream(context.Background(), inv)
		require.NoError(t, err)
		require.NotNil(t, streamResult)

		var progressEvents []mcp.ProgressEvent
		for evt := range streamResult.Progress {
			progressEvents = append(progressEvents, evt)
		}

		assert.Len(t, progressEvents, 3)
		for _, evt := range progressEvents {
			assert.NotEmpty(t, evt.Message)
		}

		finalResult := <-streamResult.Final
		require.NotNil(t, finalResult)
		assert.Equal(t, mcp.ExecutionStatusSuccess, finalResult.Status)
		assert.NotNil(t, finalResult.Output)
	})

	t.Run("stream with unknown method returns failure", func(t *testing.T) {
		cfg := &mcp.GRPCTransportConfig{
			Target:        addr,
			Service:       "testpkg.TestService",
			DescriptorSet: absPath,
		}
		exec, err := egressgrpc.NewGRPCExecutor(cfg, 5*time.Second, nil)
		require.NoError(t, err)
		defer exec.Close()

		inv := &mcp.Invocation{
			ToolID: "test-tool",
			Params: map[string]any{
				"name": "NonExistent",
			},
		}

		streamResult, err := exec.ExecuteStream(context.Background(), inv)
		require.NoError(t, err)

		// Drain progress
		for range streamResult.Progress { //nolint:revive
		}

		finalResult := <-streamResult.Final
		require.NotNil(t, finalResult)
		assert.Equal(t, mcp.ExecutionStatusFailure, finalResult.Status)
	})
}

func TestGRPCExecutor_ExecuteStream_timeout(t *testing.T) {
	addr, cleanup, err := testdata.StartTestServer(false)
	require.NoError(t, err)
	defer cleanup()

	absPath, err := filepath.Abs(testDescriptorSetPath(t))
	require.NoError(t, err)

	t.Run("stream with cancelled context returns timeout", func(t *testing.T) {
		cfg := &mcp.GRPCTransportConfig{
			Target:        addr,
			Service:       "testpkg.TestService",
			DescriptorSet: absPath,
		}
		exec, err := egressgrpc.NewGRPCExecutor(cfg, 5*time.Second, nil)
		require.NoError(t, err)
		defer exec.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // cancel immediately

		inv := &mcp.Invocation{
			ToolID: "test-tool",
			Params: map[string]any{
				"name": "StreamEcho",
				"arguments": map[string]any{
					"message": "timeout",
					"count":   100,
				},
			},
		}

		streamResult, err := exec.ExecuteStream(ctx, inv)
		require.NoError(t, err)

		// Drain progress
		for range streamResult.Progress { //nolint:revive
		}

		finalResult := <-streamResult.Final
		require.NotNil(t, finalResult)
		// Either timeout or failure is acceptable when context is cancelled
		assert.Contains(t, []mcp.ExecutionStatus{mcp.ExecutionStatusTimeout, mcp.ExecutionStatusFailure}, finalResult.Status)
	})
}

func TestGRPCExecutor_Execute_nilParams(t *testing.T) {
	addr, cleanup, err := testdata.StartTestServer(false)
	require.NoError(t, err)
	defer cleanup()

	absPath, err := filepath.Abs(testDescriptorSetPath(t))
	require.NoError(t, err)

	t.Run("nil params returns failure with empty method", func(t *testing.T) {
		cfg := &mcp.GRPCTransportConfig{
			Target:        addr,
			Service:       "testpkg.TestService",
			DescriptorSet: absPath,
		}
		exec, err := egressgrpc.NewGRPCExecutor(cfg, 5*time.Second, nil)
		require.NoError(t, err)
		defer exec.Close()

		inv := &mcp.Invocation{
			ToolID: "Echo",
			Params: nil,
		}

		result, err := exec.Execute(context.Background(), inv)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, mcp.ExecutionStatusFailure, result.Status)
	})

	t.Run("empty name in params uses ToolID", func(t *testing.T) {
		cfg := &mcp.GRPCTransportConfig{
			Target:        addr,
			Service:       "testpkg.TestService",
			DescriptorSet: absPath,
		}
		exec, err := egressgrpc.NewGRPCExecutor(cfg, 5*time.Second, nil)
		require.NoError(t, err)
		defer exec.Close()

		inv := &mcp.Invocation{
			ToolID: "Echo",
			Params: map[string]any{
				"arguments": map[string]any{
					"message": "no-name-field",
				},
			},
		}

		result, err := exec.Execute(context.Background(), inv)
		require.NoError(t, err)
		assert.Equal(t, mcp.ExecutionStatusSuccess, result.Status)
	})
}

func TestGRPCExecutor_Execute_TLS(t *testing.T) {
	t.Run("invalid TLS config returns error", func(t *testing.T) {
		cfg := &mcp.GRPCTransportConfig{
			Target:        "localhost:50051",
			Service:       "testpkg.TestService",
			DescriptorSet: testDescriptorSetPath(t),
			TLS: &mcp.TLSClientConfig{
				// InsecureSkipVerify and CACertFile are mutually exclusive
				InsecureSkipVerify: true,
				CACertFile:         "/nonexistent/ca.pem",
			},
		}
		exec, err := egressgrpc.NewGRPCExecutor(cfg, 5*time.Second, nil)
		require.Error(t, err)
		assert.Nil(t, exec)
	})

	t.Run("valid TLS insecure skip verify creates dial options", func(t *testing.T) {
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
			TLS: &mcp.TLSClientConfig{
				InsecureSkipVerify: true,
			},
		}
		exec, err := egressgrpc.NewGRPCExecutor(cfg, 5*time.Second, nil)
		require.NoError(t, err)
		require.NotNil(t, exec)
		require.NoError(t, exec.Close())
	})
}

func TestGRPCExecutor_Execute_afterClose(t *testing.T) {
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
	exec, err := egressgrpc.NewGRPCExecutor(cfg, 5*time.Second, nil)
	require.NoError(t, err)
	require.NoError(t, exec.Close())

	inv := &mcp.Invocation{
		ToolID: "test-tool",
		Params: map[string]any{
			"name": "Echo",
			"arguments": map[string]any{
				"message": "after-close",
			},
		},
	}

	result, err := exec.Execute(context.Background(), inv)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, mcp.ExecutionStatusFailure, result.Status)
}

func TestGRPCExecutor_ExecuteStream_afterClose(t *testing.T) {
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
	exec, err := egressgrpc.NewGRPCExecutor(cfg, 5*time.Second, nil)
	require.NoError(t, err)
	require.NoError(t, exec.Close())

	inv := &mcp.Invocation{
		ToolID: "test-tool",
		Params: map[string]any{
			"name": "StreamEcho",
			"arguments": map[string]any{
				"message": "after-close",
				"count":   1,
			},
		},
	}

	streamResult, err := exec.ExecuteStream(context.Background(), inv)
	require.NoError(t, err)
	for range streamResult.Progress { //nolint:revive // intentional channel drain
	}
	finalResult := <-streamResult.Final
	require.NotNil(t, finalResult)
	assert.Equal(t, mcp.ExecutionStatusFailure, finalResult.Status)
}

func TestGRPCExecutor_ExecuteStream_nilParams(t *testing.T) {
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
	exec, err := egressgrpc.NewGRPCExecutor(cfg, 5*time.Second, nil)
	require.NoError(t, err)
	defer exec.Close()

	inv := &mcp.Invocation{
		ToolID: "Echo",
		Params: nil,
	}

	streamResult, err := exec.ExecuteStream(context.Background(), inv)
	require.NoError(t, err)
	for range streamResult.Progress { //nolint:revive // intentional channel drain
	}
	finalResult := <-streamResult.Final
	require.NotNil(t, finalResult)
	assert.Equal(t, mcp.ExecutionStatusFailure, finalResult.Status)
}

func TestGRPCExecutor_ExecuteStream_withMetadata(t *testing.T) {
	addr, cleanup, err := testdata.StartTestServer(false)
	require.NoError(t, err)
	defer cleanup()

	absPath, err := filepath.Abs(testDescriptorSetPath(t))
	require.NoError(t, err)

	cfg := &mcp.GRPCTransportConfig{
		Target:        addr,
		Service:       "testpkg.TestService",
		DescriptorSet: absPath,
		Metadata: map[string]string{
			"x-api-key": "test-key",
		},
	}
	exec, err := egressgrpc.NewGRPCExecutor(cfg, 5*time.Second, nil)
	require.NoError(t, err)
	defer exec.Close()

	inv := &mcp.Invocation{
		ToolID: "test-tool",
		Params: map[string]any{
			"name": "StreamEcho",
			"arguments": map[string]any{
				"message": "with-metadata",
				"count":   2,
			},
		},
	}

	streamResult, err := exec.ExecuteStream(context.Background(), inv)
	require.NoError(t, err)
	var events []mcp.ProgressEvent
	for evt := range streamResult.Progress {
		events = append(events, evt)
	}
	assert.Len(t, events, 2)
	finalResult := <-streamResult.Final
	require.NotNil(t, finalResult)
	assert.Equal(t, mcp.ExecutionStatusSuccess, finalResult.Status)
}

func TestGRPCExecutor_Execute_invalidProtoArgs(t *testing.T) {
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
	exec, err := egressgrpc.NewGRPCExecutor(cfg, 5*time.Second, nil)
	require.NoError(t, err)
	defer exec.Close()

	// Pass an argument with a key that doesn't exist in the proto schema
	inv := &mcp.Invocation{
		ToolID: "test-tool",
		Params: map[string]any{
			"name": "Echo",
			"arguments": map[string]any{
				"nonexistent_field": "value",
			},
		},
	}

	result, err := exec.Execute(context.Background(), inv)
	require.NoError(t, err)
	require.NotNil(t, result)
	// protojson should reject unknown fields
	assert.Equal(t, mcp.ExecutionStatusFailure, result.Status)
}

func TestGRPCExecutor_ExecuteStream_invalidProtoArgs(t *testing.T) {
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
	exec, err := egressgrpc.NewGRPCExecutor(cfg, 5*time.Second, nil)
	require.NoError(t, err)
	defer exec.Close()

	inv := &mcp.Invocation{
		ToolID: "test-tool",
		Params: map[string]any{
			"name": "StreamEcho",
			"arguments": map[string]any{
				"nonexistent_field": "value",
			},
		},
	}

	streamResult, err := exec.ExecuteStream(context.Background(), inv)
	require.NoError(t, err)
	for range streamResult.Progress { //nolint:revive // intentional channel drain
	}
	finalResult := <-streamResult.Final
	require.NotNil(t, finalResult)
	assert.Equal(t, mcp.ExecutionStatusFailure, finalResult.Status)
}

func TestGRPCExecutor_NewWithReflection_AllMethods(t *testing.T) {
	addr, cleanup, err := testdata.StartTestServer(true)
	require.NoError(t, err)
	defer cleanup()

	cfg := &mcp.GRPCTransportConfig{
		Target:     addr,
		Service:    "testpkg.TestService",
		Reflection: true,
	}
	exec, err := egressgrpc.NewGRPCExecutor(cfg, 5*time.Second, nil)
	require.NoError(t, err)
	defer exec.Close()

	// Execute with all-methods mode via reflection
	inv := &mcp.Invocation{
		ToolID: "test-tool",
		Params: map[string]any{
			"name": "Echo",
			"arguments": map[string]any{
				"message": "reflection-all",
				"count":   1,
			},
		},
	}

	result, err := exec.Execute(context.Background(), inv)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, mcp.ExecutionStatusSuccess, result.Status)
}

func TestGRPCExecutor_Close(t *testing.T) {
	addr, cleanup, err := testdata.StartTestServer(false)
	require.NoError(t, err)
	defer cleanup()

	absPath, err := filepath.Abs(testDescriptorSetPath(t))
	require.NoError(t, err)

	t.Run("close is idempotent", func(t *testing.T) {
		cfg := &mcp.GRPCTransportConfig{
			Target:        addr,
			Service:       "testpkg.TestService",
			Method:        "Echo",
			DescriptorSet: absPath,
		}
		exec, err := egressgrpc.NewGRPCExecutor(cfg, 5*time.Second, nil)
		require.NoError(t, err)

		require.NoError(t, exec.Close())
		require.NoError(t, exec.Close()) // second call should not panic
	})
}

func TestGRPCExecutor_Execute_CredentialsAttachedAsMetadata(t *testing.T) {
	addr, recorder, cleanup, err := testdata.StartTestServerWithMetadata(false)
	require.NoError(t, err)
	defer cleanup()

	absPath, err := filepath.Abs(testDescriptorSetPath(t))
	require.NoError(t, err)

	// x-shared exists in both cfg.Metadata and creds: the credential must win.
	cfg := &mcp.GRPCTransportConfig{
		Target:        addr,
		Service:       "testpkg.TestService",
		Method:        "Echo",
		DescriptorSet: absPath,
		Metadata:      map[string]string{"x-static": "cfg-value", "x-shared": "cfg-value"},
	}
	creds := map[string]string{"x-token": "s3cret", "x-shared": "cred-value"}
	exec, err := egressgrpc.NewGRPCExecutor(cfg, 5*time.Second, creds)
	require.NoError(t, err)
	defer exec.Close()

	inv := &mcp.Invocation{
		ToolID: "test-tool",
		Params: map[string]any{
			"name":      "Echo",
			"arguments": map[string]any{"message": "hello"},
		},
	}
	result, err := exec.Execute(context.Background(), inv)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, mcp.ExecutionStatusSuccess, result.Status)

	md := recorder.Last()
	require.NotNil(t, md)
	assert.Equal(t, []string{"s3cret"}, md.Get("x-token"), "credential must reach the backend as metadata")
	assert.Equal(t, []string{"cred-value"}, md.Get("x-shared"), "credential must override cfg.Metadata")
	assert.Equal(t, []string{"cfg-value"}, md.Get("x-static"), "cfg.Metadata entries must still be applied")
}

func TestGRPCExecutor_ExecuteStream_AbandonedConsumer(t *testing.T) {
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
	exec, err := egressgrpc.NewGRPCExecutor(cfg, 5*time.Second, nil)
	require.NoError(t, err)
	defer exec.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	inv := &mcp.Invocation{
		ToolID: "test-tool",
		Params: map[string]any{
			"name": "StreamEcho",
			"arguments": map[string]any{
				"message": "abandoned",
				"count":   50,
			},
		},
	}

	streamResult, err := exec.ExecuteStream(ctx, inv)
	require.NoError(t, err)

	// Simulate a consumer that gives up: never read Progress. Give the stream
	// goroutine time to block on its first progress send, then cancel the
	// context. The goroutine must abort instead of blocking forever.
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case finalResult := <-streamResult.Final:
		require.NotNil(t, finalResult)
		assert.Contains(t, []mcp.ExecutionStatus{mcp.ExecutionStatusTimeout, mcp.ExecutionStatusFailure}, finalResult.Status)
	case <-time.After(3 * time.Second):
		t.Fatal("stream goroutine leaked: no final result after context cancellation")
	}

	// Both channels must be closed once the goroutine exits.
	select {
	case _, ok := <-streamResult.Final:
		assert.False(t, ok, "final channel must be closed")
	case <-time.After(3 * time.Second):
		t.Fatal("final channel was not closed")
	}
	select {
	case _, ok := <-streamResult.Progress:
		assert.False(t, ok, "progress channel must be closed")
	case <-time.After(3 * time.Second):
		t.Fatal("progress channel was not closed")
	}
}
