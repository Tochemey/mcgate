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

package audit

import (
	"maps"
	"sync"

	"github.com/tochemey/portcullis/mcp"
)

// MemorySink is an in-memory audit sink for testing.
//
// Events are appended to a slice. Safe for concurrent use.
type MemorySink struct {
	mu     sync.Mutex
	events []*mcp.AuditEvent
	closed bool
}

// NewMemorySink creates a MemorySink.
func NewMemorySink() *MemorySink {
	return &MemorySink{events: make([]*mcp.AuditEvent, 0, 16)}
}

// Write appends the event to the in-memory buffer.
func (m *MemorySink) Write(event *mcp.AuditEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	eventCopy := *event
	if event.Metadata != nil {
		metaCopy := make(map[string]string, len(event.Metadata))
		maps.Copy(metaCopy, event.Metadata)
		eventCopy.Metadata = metaCopy
	}
	m.events = append(m.events, &eventCopy)
	return nil
}

// Close marks the sink as closed.
func (m *MemorySink) Close() error {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	return nil
}

// Events returns a copy of the recorded events.
func (m *MemorySink) Events() []*mcp.AuditEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*mcp.AuditEvent, len(m.events))
	for i, e := range m.events {
		eCopy := *e
		if e.Metadata != nil {
			metaCopy := make(map[string]string, len(e.Metadata))
			maps.Copy(metaCopy, e.Metadata)
			eCopy.Metadata = metaCopy
		}
		out[i] = &eCopy
	}
	return out
}
