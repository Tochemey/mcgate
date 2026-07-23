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

package extension

import (
	"sync"

	"github.com/tochemey/goakt/v4/eventstream"
	goaktextension "github.com/tochemey/goakt/v4/extension"

	"github.com/tochemey/goakt-mcp/mcp"
)

// ToolConfigExtensionID is the fixed identifier for the ToolConfig extension
// registered on the actor system.
const ToolConfigExtensionID = "tool-config"

// ToolConfigExtension is a system-level extension that holds all registered tool
// definitions. It is registered once with the actor system at startup and updated
// by the Registrar whenever tools are added or removed. ToolSupervisorActor
// resolves its own tool definition from this extension in PostStart, deriving the
// tool ID from its actor name via naming.ToolIDFromSupervisorName.
type ToolConfigExtension struct {
	mu    sync.RWMutex
	tools map[mcp.ToolID]mcp.Tool
}

var _ goaktextension.Extension = (*ToolConfigExtension)(nil)

// NewToolConfigExtension creates an empty ToolConfigExtension.
func NewToolConfigExtension() *ToolConfigExtension {
	return &ToolConfigExtension{tools: make(map[mcp.ToolID]mcp.Tool)}
}

// ID returns the unique identifier for this extension.
func (e *ToolConfigExtension) ID() string { return ToolConfigExtensionID }

// Register adds or replaces the tool definition for the given tool.
func (e *ToolConfigExtension) Register(tool mcp.Tool) {
	e.mu.Lock()
	e.tools[tool.ID] = tool
	e.mu.Unlock()
}

// Remove deletes the tool definition for the given tool ID.
func (e *ToolConfigExtension) Remove(toolID mcp.ToolID) {
	e.mu.Lock()
	delete(e.tools, toolID)
	e.mu.Unlock()
}

// Get retrieves the tool definition for the given tool ID.
func (e *ToolConfigExtension) Get(toolID mcp.ToolID) (mcp.Tool, bool) {
	e.mu.RLock()
	tool, ok := e.tools[toolID]
	e.mu.RUnlock()
	return tool, ok
}

// CircuitConfigExtensionID is the fixed identifier for the CircuitConfig extension.
const CircuitConfigExtensionID = "circuit-config"

// CircuitConfigExtension is an optional system-level extension that overrides the
// default circuit breaker configuration for all ToolSupervisorActors in the system.
// When not registered, supervisors use the package-level defaults. Primarily used
// in tests to reduce OpenDuration and speed up circuit state transition assertions.
type CircuitConfigExtension struct {
	cfg mcp.CircuitConfig
}

var _ goaktextension.Extension = (*CircuitConfigExtension)(nil)

// NewCircuitConfigExtension creates an extension wrapping the given circuit config.
func NewCircuitConfigExtension(cfg mcp.CircuitConfig) *CircuitConfigExtension {
	return &CircuitConfigExtension{cfg: cfg}
}

// ID returns the unique identifier for this extension.
func (c *CircuitConfigExtension) ID() string { return CircuitConfigExtensionID }

// Config returns the circuit breaker configuration.
func (c *CircuitConfigExtension) Config() mcp.CircuitConfig { return c.cfg }

// ConfigExtensionID is the fixed identifier for the Config extension.
const ConfigExtensionID = "config"

// ConfigExtension is a system-level extension that holds the runtime configuration.
type ConfigExtension struct {
	config mcp.Config
}

// Enforce that ConfigExtension implements the Extension interface.
var _ goaktextension.Extension = (*ConfigExtension)(nil)

// NewConfigExtension creates a new ConfigExtension.
func NewConfigExtension(config mcp.Config) *ConfigExtension {
	return &ConfigExtension{config: config}
}

// ID returns the unique identifier for this extension.
func (c *ConfigExtension) ID() string { return ConfigExtensionID }

// Config returns the runtime configuration.
func (c *ConfigExtension) Config() mcp.Config { return c.config }

// AuditStreamExtensionID is the fixed identifier for the AuditStream extension.
const AuditStreamExtensionID = "audit-stream"

// AuditStreamTopic is the topic on which audit events are published. Subscribers
// obtained from Gateway.SubscribeAudit receive messages carrying *mcp.AuditEvent
// payloads on this topic.
const AuditStreamTopic = "mcp.audit"

// AuditStreamExtension is a system-level extension that exposes the audit
// event stream to actors that need to publish (Journaler) or external callers
// that subscribe (Gateway.SubscribeAudit). The underlying stream is owned by
// the Gateway and torn down on Gateway.Stop.
//
// When this extension is not registered (or carries a nil stream), audit
// events are written to the configured AuditSink only and no stream
// publishing occurs.
type AuditStreamExtension struct {
	stream eventstream.Stream
}

// Enforce that AuditStreamExtension implements the Extension interface.
var _ goaktextension.Extension = (*AuditStreamExtension)(nil)

// NewAuditStreamExtension creates an AuditStreamExtension backed by the given
// stream. A nil stream is accepted and turns the extension into a no-op.
func NewAuditStreamExtension(stream eventstream.Stream) *AuditStreamExtension {
	return &AuditStreamExtension{stream: stream}
}

// ID returns the unique identifier for this extension.
func (e *AuditStreamExtension) ID() string { return AuditStreamExtensionID }

// Stream returns the underlying event stream, or nil when the extension was
// created with a nil stream.
func (e *AuditStreamExtension) Stream() eventstream.Stream { return e.stream }
