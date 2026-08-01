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

	"github.com/tochemey/mcgate/mcp"
)

func TestTenantID(t *testing.T) {
	var zero mcp.TenantID
	assert.True(t, zero.IsZero())
	assert.Equal(t, "", zero.String())

	id := mcp.TenantID("acme")
	assert.False(t, id.IsZero())
	assert.Equal(t, "acme", id.String())
}

func TestClientID(t *testing.T) {
	var zero mcp.ClientID
	assert.True(t, zero.IsZero())
	id := mcp.ClientID("client-1")
	assert.False(t, id.IsZero())
	assert.Equal(t, "client-1", id.String())
}

func TestToolID(t *testing.T) {
	var zero mcp.ToolID
	assert.True(t, zero.IsZero())
	id := mcp.ToolID("my-tool")
	assert.False(t, id.IsZero())
	assert.Equal(t, "my-tool", id.String())
}

func TestSessionID(t *testing.T) {
	var zero mcp.SessionID
	assert.True(t, zero.IsZero())
	id := mcp.SessionID("sess-123")
	assert.False(t, id.IsZero())
	assert.Equal(t, "sess-123", id.String())
}

func TestRequestID(t *testing.T) {
	var zero mcp.RequestID
	assert.True(t, zero.IsZero())
	id := mcp.RequestID("req-1")
	assert.False(t, id.IsZero())
	assert.Equal(t, "req-1", id.String())
}

func TestTraceID(t *testing.T) {
	var zero mcp.TraceID
	assert.True(t, zero.IsZero())
	id := mcp.TraceID("trace-abc")
	assert.False(t, id.IsZero())
	assert.Equal(t, "trace-abc", id.String())
}
