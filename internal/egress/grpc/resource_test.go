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

package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/tochemey/portcullis/mcp"
)

func TestFetchResources(t *testing.T) {
	t.Run("returns empty slices and no error for nil config", func(t *testing.T) {
		resources, templates, err := FetchResources(context.Background(), nil, time.Second)
		assert.NoError(t, err)
		assert.Nil(t, resources)
		assert.Nil(t, templates)
	})

	t.Run("returns empty slices and no error for valid config", func(t *testing.T) {
		cfg := &mcp.GRPCTransportConfig{Target: "localhost:50051"}
		resources, templates, err := FetchResources(context.Background(), cfg, time.Second)
		assert.NoError(t, err)
		assert.Nil(t, resources)
		assert.Nil(t, templates)
	})

	t.Run("returns empty slices regardless of timeout", func(t *testing.T) {
		resources, templates, err := FetchResources(context.Background(), nil, 0)
		assert.NoError(t, err)
		assert.Nil(t, resources)
		assert.Nil(t, templates)
	})
}
