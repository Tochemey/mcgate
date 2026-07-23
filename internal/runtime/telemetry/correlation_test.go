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

package telemetry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tochemey/portcullis/mcp"
)

func TestCorrelationFields_LogFields(t *testing.T) {
	cf := &CorrelationFields{}
	fields := cf.LogFields()
	assert.Empty(t, fields)

	cf = &CorrelationFields{TenantID: mcp.TenantID("t1"), RequestID: mcp.RequestID("r1")}
	fields = cf.LogFields()
	assert.Len(t, fields, 2)
	assert.Equal(t, "t1", fields["tenant_id"])
	assert.Equal(t, "r1", fields["request_id"])

	cf = &CorrelationFields{
		TenantID:  mcp.TenantID("t1"),
		ClientID:  mcp.ClientID("c1"),
		RequestID: mcp.RequestID("r1"),
		TraceID:   mcp.TraceID("tr1"),
		ToolID:    mcp.ToolID("tool1"),
	}
	fields = cf.LogFields()
	assert.Len(t, fields, 5)
	assert.Equal(t, "t1", fields["tenant_id"])
	assert.Equal(t, "c1", fields["client_id"])
	assert.Equal(t, "r1", fields["request_id"])
	assert.Equal(t, "tr1", fields["trace_id"])
	assert.Equal(t, "tool1", fields["tool_id"])
}

func TestCorrelationFields_LogKeyValues(t *testing.T) {
	cf := &CorrelationFields{}
	assert.Nil(t, cf.LogKeyValues())

	cf = &CorrelationFields{TenantID: mcp.TenantID("t1"), TraceID: mcp.TraceID("tr1")}
	kvs := cf.LogKeyValues()
	require.Len(t, kvs, 4)
	assert.Equal(t, "tenant_id", kvs[0])
	assert.Equal(t, "t1", kvs[1])
	assert.Equal(t, "trace_id", kvs[2])
	assert.Equal(t, "tr1", kvs[3])
}
