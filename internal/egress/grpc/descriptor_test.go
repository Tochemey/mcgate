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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	egressgrpc "github.com/tochemey/portcullis/internal/egress/grpc"
	"github.com/tochemey/portcullis/internal/egress/grpc/testdata"
)

func testDescriptorSetPath(t *testing.T) string {
	t.Helper()
	return filepath.Join("testdata", "test_service.binpb")
}

func TestLoadDescriptorSet(t *testing.T) {
	t.Run("loads valid descriptor set", func(t *testing.T) {
		fds, err := egressgrpc.LoadDescriptorSet(testDescriptorSetPath(t))
		require.NoError(t, err)
		require.NotNil(t, fds)
		assert.Greater(t, len(fds.GetFile()), 0)
	})

	t.Run("returns error for missing file", func(t *testing.T) {
		_, err := egressgrpc.LoadDescriptorSet("/nonexistent/path.binpb")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "read descriptor set")
	})

	t.Run("returns error for invalid content", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "bad.binpb")
		require.NoError(t, os.WriteFile(tmpFile, []byte("not a valid protobuf"), 0o600))
		_, err := egressgrpc.LoadDescriptorSet(tmpFile)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unmarshal descriptor set")
	})
}

func TestFetchDescriptorSetViaReflection(t *testing.T) {
	addr, cleanup, err := testdata.StartTestServer(true)
	require.NoError(t, err)
	defer cleanup()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()

	t.Run("fetches descriptors for known service", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		fds, err := egressgrpc.FetchDescriptorSetViaReflection(ctx, conn, "testpkg.TestService")
		require.NoError(t, err)
		require.NotNil(t, fds)
		assert.Greater(t, len(fds.GetFile()), 0)
	})

	t.Run("returns error for unknown service", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_, err := egressgrpc.FetchDescriptorSetViaReflection(ctx, conn, "nonexistent.Service")
		require.Error(t, err)
	})

	t.Run("fetches rich service with all field types", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		fds, err := egressgrpc.FetchDescriptorSetViaReflection(ctx, conn, "testpkg.RichService")
		require.NoError(t, err)
		require.NotNil(t, fds)

		// Verify we can resolve the service and its methods from reflected descriptors.
		sd, err := egressgrpc.ResolveService(fds, "testpkg.RichService")
		require.NoError(t, err)
		assert.Equal(t, 1, sd.Methods().Len())
	})

	t.Run("fetches self-referencing service", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		fds, err := egressgrpc.FetchDescriptorSetViaReflection(ctx, conn, "testpkg.SelfRefService")
		require.NoError(t, err)
		require.NotNil(t, fds)

		input, output, err := egressgrpc.ResolveMethod(fds, "testpkg.SelfRefService", "Traverse")
		require.NoError(t, err)
		assert.Equal(t, "TreeNode", string(input.Name()))
		assert.Equal(t, "TreeNode", string(output.Name()))
	})

	t.Run("returns error on cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := egressgrpc.FetchDescriptorSetViaReflection(ctx, conn, "testpkg.TestService")
		require.Error(t, err)
	})

	t.Run("returns error for non-reflection server", func(t *testing.T) {
		noRefAddr, noRefCleanup, err := testdata.StartTestServer(false)
		require.NoError(t, err)
		defer noRefCleanup()

		noRefConn, err := grpc.NewClient(noRefAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		require.NoError(t, err)
		defer noRefConn.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_, err = egressgrpc.FetchDescriptorSetViaReflection(ctx, noRefConn, "testpkg.TestService")
		require.Error(t, err)
	})
}

func TestResolveService(t *testing.T) {
	fds, err := egressgrpc.LoadDescriptorSet(testDescriptorSetPath(t))
	require.NoError(t, err)

	t.Run("resolves known service", func(t *testing.T) {
		sd, err := egressgrpc.ResolveService(fds, "testpkg.TestService")
		require.NoError(t, err)
		assert.Equal(t, "TestService", string(sd.Name()))
		assert.Equal(t, 2, sd.Methods().Len())
	})

	t.Run("returns error for unknown service", func(t *testing.T) {
		_, err := egressgrpc.ResolveService(fds, "nonexistent.Service")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("returns error for non-service descriptor", func(t *testing.T) {
		_, err := egressgrpc.ResolveService(fds, "testpkg.EchoRequest")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not a service descriptor")
	})

	t.Run("returns error for invalid file descriptor set", func(t *testing.T) {
		// Create a FileDescriptorSet with an invalid file descriptor that
		// references a non-existent dependency, causing protodesc.NewFiles to fail.
		badFDS := &descriptorpb.FileDescriptorSet{
			File: []*descriptorpb.FileDescriptorProto{
				{
					Name:       proto.String("bad.proto"),
					Dependency: []string{"nonexistent.proto"},
				},
			},
		}
		_, err := egressgrpc.ResolveService(badFDS, "some.Service")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "build file registry")
	})
}

func TestResolveMethod(t *testing.T) {
	fds, err := egressgrpc.LoadDescriptorSet(testDescriptorSetPath(t))
	require.NoError(t, err)

	t.Run("resolves known method", func(t *testing.T) {
		input, output, err := egressgrpc.ResolveMethod(fds, "testpkg.TestService", "Echo")
		require.NoError(t, err)
		assert.Equal(t, "EchoRequest", string(input.Name()))
		assert.Equal(t, "EchoResponse", string(output.Name()))
	})

	t.Run("returns error for unknown method", func(t *testing.T) {
		_, _, err := egressgrpc.ResolveMethod(fds, "testpkg.TestService", "NonExistent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "method \"NonExistent\" not found")
	})

	t.Run("returns error for unknown service", func(t *testing.T) {
		_, _, err := egressgrpc.ResolveMethod(fds, "nonexistent.Service", "Echo")
		require.Error(t, err)
	})
}
