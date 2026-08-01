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

func TestNewCompositeResourceFetcher(t *testing.T) {
	t.Run("zero timeout defaults to DefaultStartupTimeout", func(t *testing.T) {
		f := NewCompositeResourceFetcher(0, nil)
		require.NotNil(t, f)
		assert.Equal(t, mcp.DefaultStartupTimeout, f.startupTimeout)
	})

	t.Run("negative timeout defaults to DefaultStartupTimeout", func(t *testing.T) {
		f := NewCompositeResourceFetcher(-1*time.Second, nil)
		require.NotNil(t, f)
		assert.Equal(t, mcp.DefaultStartupTimeout, f.startupTimeout)
	})

	t.Run("positive timeout is preserved", func(t *testing.T) {
		f := NewCompositeResourceFetcher(3*time.Second, nil)
		require.NotNil(t, f)
		assert.Equal(t, 3*time.Second, f.startupTimeout)
	})
}

func TestCompositeResourceFetcher_FetchResources(t *testing.T) {
	t.Run("unsupported transport returns error", func(t *testing.T) {
		f := NewCompositeResourceFetcher(time.Second, nil)
		tool := mcp.Tool{
			ID:        "bad-transport",
			Transport: "unknown",
		}
		resources, templates, err := f.FetchResources(context.Background(), tool)
		assert.Nil(t, resources)
		assert.Nil(t, templates)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported transport type")
	})

	t.Run("stdio transport with nil config returns error", func(t *testing.T) {
		f := NewCompositeResourceFetcher(500*time.Millisecond, nil)
		tool := mcp.Tool{
			ID:        "stdio-nil",
			Transport: mcp.TransportStdio,
			Stdio:     nil,
		}
		_, _, err := f.FetchResources(context.Background(), tool)
		require.Error(t, err)
	})

	t.Run("http transport with nil config returns error", func(t *testing.T) {
		f := NewCompositeResourceFetcher(500*time.Millisecond, nil)
		tool := mcp.Tool{
			ID:        "http-nil",
			Transport: mcp.TransportHTTP,
			HTTP:      nil,
		}
		_, _, err := f.FetchResources(context.Background(), tool)
		require.Error(t, err)
	})

	t.Run("stdio transport with non-existent command returns error", func(t *testing.T) {
		f := NewCompositeResourceFetcher(500*time.Millisecond, nil)
		tool := mcp.Tool{
			ID:        "stdio-bad",
			Transport: mcp.TransportStdio,
			Stdio:     &mcp.StdioTransportConfig{Command: "/nonexistent/binary/xyz"},
		}
		_, _, err := f.FetchResources(context.Background(), tool)
		require.Error(t, err)
	})

	t.Run("http transport with unreachable endpoint returns error", func(t *testing.T) {
		f := NewCompositeResourceFetcher(500*time.Millisecond, nil)
		tool := mcp.Tool{
			ID:        "http-bad",
			Transport: mcp.TransportHTTP,
			HTTP:      &mcp.HTTPTransportConfig{URL: "http://127.0.0.1:1/unreachable"},
		}
		_, _, err := f.FetchResources(context.Background(), tool)
		require.Error(t, err)
	})
}
