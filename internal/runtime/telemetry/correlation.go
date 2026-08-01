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

import "github.com/tochemey/mcgate/mcp"

// CorrelationFields holds the standard correlation identifiers for structured
// logging and audit. Log entries should include tenant_id, client_id,
// request_id, and trace_id when available.
type CorrelationFields struct {
	TenantID  mcp.TenantID
	ClientID  mcp.ClientID
	RequestID mcp.RequestID
	TraceID   mcp.TraceID
	ToolID    mcp.ToolID
}

// LogFields returns a map suitable for structured logging with correlation
// keys. Keys follow common conventions (tenant_id, client_id, etc.).
func (c *CorrelationFields) LogFields() map[string]string {
	m := make(map[string]string, 5)
	if !c.TenantID.IsZero() {
		m["tenant_id"] = string(c.TenantID)
	}
	if !c.ClientID.IsZero() {
		m["client_id"] = string(c.ClientID)
	}
	if !c.RequestID.IsZero() {
		m["request_id"] = string(c.RequestID)
	}
	if !c.TraceID.IsZero() {
		m["trace_id"] = string(c.TraceID)
	}
	if !c.ToolID.IsZero() {
		m["tool_id"] = string(c.ToolID)
	}
	return m
}

// LogKeyValues returns key-value pairs for use with
// goaktlog.Logger.With(keyValues...). Order is deterministic.
// Avoids the intermediate map allocation of LogFields.
func (c *CorrelationFields) LogKeyValues() []any {
	kvs := make([]any, 0, 10)
	if !c.TenantID.IsZero() {
		kvs = append(kvs, "tenant_id", string(c.TenantID))
	}
	if !c.ClientID.IsZero() {
		kvs = append(kvs, "client_id", string(c.ClientID))
	}
	if !c.RequestID.IsZero() {
		kvs = append(kvs, "request_id", string(c.RequestID))
	}
	if !c.TraceID.IsZero() {
		kvs = append(kvs, "trace_id", string(c.TraceID))
	}
	if !c.ToolID.IsZero() {
		kvs = append(kvs, "tool_id", string(c.ToolID))
	}
	if len(kvs) == 0 {
		return nil
	}
	return kvs
}
