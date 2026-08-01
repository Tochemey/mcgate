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

package actor

import (
	"github.com/tochemey/mcgate/mcp"
)

// toolCatalogEntry bundles a Tool with its discovered schemas and MCP
// resource metadata. The three collections have the same tool-ID lifecycle
// so holding them together keeps the catalog's invariants simple: when a
// tool goes, its metadata goes.
type toolCatalogEntry struct {
	Tool              mcp.Tool
	Schemas           []mcp.ToolSchema
	Resources         []mcp.ResourceSchema
	ResourceTemplates []mcp.ResourceTemplateSchema
}

// toolCatalog is the registrar's serializable storage layer. It holds every
// registered tool and the fetched metadata attached to it, and exposes a
// narrow API for mutation and lookup.
//
// Non-serializable per-node state — supervisor PIDs, actor references —
// lives on the registrar itself, not here. This separation is intentional:
// it lets a future CRDT-backed implementation slot in behind the same API
// without touching the supervisor lifecycle or the actor's message flow.
//
// toolCatalog is NOT safe for concurrent mutation; it is owned by the
// registrar actor whose single-threaded Receive serializes all access.
type toolCatalog struct {
	entries map[mcp.ToolID]*toolCatalogEntry
}

// newToolCatalog returns an empty catalog ready for registration.
func newToolCatalog() *toolCatalog {
	return &toolCatalog{
		entries: make(map[mcp.ToolID]*toolCatalogEntry),
	}
}

// Has reports whether the catalog contains a tool with the given ID.
func (c *toolCatalog) Has(toolID mcp.ToolID) bool {
	_, ok := c.entries[toolID]
	return ok
}

// Get returns the stored tool by ID. The second return value reports
// whether the tool was present. The returned Tool does NOT include
// schemas / resources; call GetWithMetadata when those are needed.
func (c *toolCatalog) Get(toolID mcp.ToolID) (mcp.Tool, bool) {
	entry, ok := c.entries[toolID]
	if !ok {
		return mcp.Tool{}, false
	}
	return entry.Tool, true
}

// GetWithMetadata returns the tool decorated with its cached schemas and
// resource metadata, suitable for ListTools / GetToolStatus responses.
func (c *toolCatalog) GetWithMetadata(toolID mcp.ToolID) (mcp.Tool, bool) {
	entry, ok := c.entries[toolID]
	if !ok {
		return mcp.Tool{}, false
	}

	tool := entry.Tool
	tool.Schemas = entry.Schemas
	tool.Resources = entry.Resources
	tool.ResourceTemplates = entry.ResourceTemplates
	return tool, true
}

// Put inserts or replaces the tool entry while clearing any previously
// cached schemas / resources so a failed re-registration does not leak
// stale metadata.
func (c *toolCatalog) Put(tool mcp.Tool) {
	c.entries[tool.ID] = &toolCatalogEntry{Tool: tool}
}

// UpdateTool replaces the Tool for an existing entry, preserving the cached
// schemas and resources untouched. Returns false when the entry does not
// exist so callers can surface ErrToolNotFound.
func (c *toolCatalog) UpdateTool(tool mcp.Tool) bool {
	entry, ok := c.entries[tool.ID]
	if !ok {
		return false
	}

	entry.Tool = tool
	return true
}

// Remove deletes the tool and all of its cached metadata. Returns false
// when the tool was not present.
func (c *toolCatalog) Remove(toolID mcp.ToolID) bool {
	if _, ok := c.entries[toolID]; !ok {
		return false
	}

	delete(c.entries, toolID)
	return true
}

// SetSchemas replaces the cached schema list for a tool. No-op when the
// tool is not registered, so callers do not need to gate their own calls.
func (c *toolCatalog) SetSchemas(toolID mcp.ToolID, schemas []mcp.ToolSchema) {
	entry, ok := c.entries[toolID]
	if !ok {
		return
	}

	entry.Schemas = schemas
}

// SetResources replaces the cached MCP resource and resource-template
// lists for a tool. No-op when the tool is not registered.
func (c *toolCatalog) SetResources(toolID mcp.ToolID, resources []mcp.ResourceSchema, templates []mcp.ResourceTemplateSchema) {
	entry, ok := c.entries[toolID]
	if !ok {
		return
	}

	entry.Resources = resources
	entry.ResourceTemplates = templates
}

// Schemas returns the cached schema slice for the tool, or nil when the
// tool is absent or no schemas have been fetched.
func (c *toolCatalog) Schemas(toolID mcp.ToolID) []mcp.ToolSchema {
	entry, ok := c.entries[toolID]
	if !ok {
		return nil
	}

	return entry.Schemas
}

// Resources returns the cached MCP resource slice for the tool, or nil.
func (c *toolCatalog) Resources(toolID mcp.ToolID) []mcp.ResourceSchema {
	entry, ok := c.entries[toolID]
	if !ok {
		return nil
	}

	return entry.Resources
}

// ResourceTemplates returns the cached MCP resource-template slice, or nil.
func (c *toolCatalog) ResourceTemplates(toolID mcp.ToolID) []mcp.ResourceTemplateSchema {
	entry, ok := c.entries[toolID]
	if !ok {
		return nil
	}

	return entry.ResourceTemplates
}

// List returns every tool decorated with its cached metadata. The returned
// slice is a snapshot; subsequent mutations to the catalog do not affect it.
func (c *toolCatalog) List() []mcp.Tool {
	tools := make([]mcp.Tool, 0, len(c.entries))
	for _, entry := range c.entries {
		tool := entry.Tool
		tool.Schemas = entry.Schemas
		tool.Resources = entry.Resources
		tool.ResourceTemplates = entry.ResourceTemplates
		tools = append(tools, tool)
	}
	return tools
}

// Len returns the number of tools in the catalog.
func (c *toolCatalog) Len() int {
	return len(c.entries)
}
