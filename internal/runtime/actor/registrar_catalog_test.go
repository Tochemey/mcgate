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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tochemey/portcullis/mcp"
)

const (
	catalogTestToolID  = mcp.ToolID("catalog-tool")
	catalogTestMissing = mcp.ToolID("missing-tool")
)

func catalogTestTool() mcp.Tool {
	return mcp.Tool{
		ID:        catalogTestToolID,
		Transport: mcp.TransportStdio,
		Stdio:     &mcp.StdioTransportConfig{Command: "npx"},
		State:     mcp.ToolStateEnabled,
	}
}

func TestToolCatalog_EmptyAfterConstruction(t *testing.T) {
	catalog := newToolCatalog()

	assert.Zero(t, catalog.Len())
	assert.False(t, catalog.Has(catalogTestToolID))

	_, ok := catalog.Get(catalogTestToolID)
	assert.False(t, ok)
}

func TestToolCatalog_PutAndGet(t *testing.T) {
	catalog := newToolCatalog()
	tool := catalogTestTool()

	catalog.Put(tool)

	assert.True(t, catalog.Has(tool.ID))
	got, ok := catalog.Get(tool.ID)
	require.True(t, ok)
	assert.Equal(t, tool.ID, got.ID)
	assert.Equal(t, tool.Transport, got.Transport)
	assert.Equal(t, 1, catalog.Len())
}

func TestToolCatalog_PutReplacesEntryAndClearsMetadata(t *testing.T) {
	catalog := newToolCatalog()
	tool := catalogTestTool()
	catalog.Put(tool)

	schemas := []mcp.ToolSchema{{Name: "s1"}}
	catalog.SetSchemas(tool.ID, schemas)
	catalog.SetResources(tool.ID, []mcp.ResourceSchema{{URI: "r1"}}, []mcp.ResourceTemplateSchema{{URITemplate: "t1"}})
	require.Len(t, catalog.Schemas(tool.ID), 1)

	replacement := tool
	replacement.State = mcp.ToolStateDisabled
	catalog.Put(replacement)

	assert.Equal(t, mcp.ToolStateDisabled, mustGet(t, catalog, tool.ID).State)
	assert.Nil(t, catalog.Schemas(tool.ID))
	assert.Nil(t, catalog.Resources(tool.ID))
	assert.Nil(t, catalog.ResourceTemplates(tool.ID))
}

func TestToolCatalog_UpdateToolPreservesMetadata(t *testing.T) {
	catalog := newToolCatalog()
	tool := catalogTestTool()
	catalog.Put(tool)
	catalog.SetSchemas(tool.ID, []mcp.ToolSchema{{Name: "keep"}})

	updated := tool
	updated.State = mcp.ToolStateDisabled
	ok := catalog.UpdateTool(updated)

	require.True(t, ok)
	assert.Equal(t, mcp.ToolStateDisabled, mustGet(t, catalog, tool.ID).State)
	assert.Len(t, catalog.Schemas(tool.ID), 1, "UpdateTool must not clear schemas")
}

func TestToolCatalog_UpdateToolReportsMissing(t *testing.T) {
	catalog := newToolCatalog()

	ok := catalog.UpdateTool(catalogTestTool())

	assert.False(t, ok)
}

func TestToolCatalog_RemoveDeletesEntryAndMetadata(t *testing.T) {
	catalog := newToolCatalog()
	tool := catalogTestTool()
	catalog.Put(tool)
	catalog.SetSchemas(tool.ID, []mcp.ToolSchema{{Name: "s1"}})

	removed := catalog.Remove(tool.ID)

	assert.True(t, removed)
	assert.False(t, catalog.Has(tool.ID))
	assert.Nil(t, catalog.Schemas(tool.ID))
	assert.Zero(t, catalog.Len())
}

func TestToolCatalog_RemoveReportsMissing(t *testing.T) {
	catalog := newToolCatalog()

	removed := catalog.Remove(catalogTestMissing)

	assert.False(t, removed)
}

func TestToolCatalog_SetSchemasIsNoopForMissingTool(t *testing.T) {
	catalog := newToolCatalog()

	catalog.SetSchemas(catalogTestMissing, []mcp.ToolSchema{{Name: "s1"}})

	assert.Nil(t, catalog.Schemas(catalogTestMissing))
}

func TestToolCatalog_SetResourcesIsNoopForMissingTool(t *testing.T) {
	catalog := newToolCatalog()

	catalog.SetResources(catalogTestMissing, []mcp.ResourceSchema{{URI: "r"}}, []mcp.ResourceTemplateSchema{{URITemplate: "t"}})

	assert.Nil(t, catalog.Resources(catalogTestMissing))
	assert.Nil(t, catalog.ResourceTemplates(catalogTestMissing))
}

func TestToolCatalog_ListDecoratesWithMetadata(t *testing.T) {
	catalog := newToolCatalog()
	tool := catalogTestTool()
	catalog.Put(tool)
	catalog.SetSchemas(tool.ID, []mcp.ToolSchema{{Name: "s"}})
	catalog.SetResources(tool.ID,
		[]mcp.ResourceSchema{{URI: "r"}},
		[]mcp.ResourceTemplateSchema{{URITemplate: "t"}},
	)

	tools := catalog.List()

	require.Len(t, tools, 1)
	assert.Equal(t, tool.ID, tools[0].ID)
	assert.Len(t, tools[0].Schemas, 1)
	assert.Len(t, tools[0].Resources, 1)
	assert.Len(t, tools[0].ResourceTemplates, 1)
}

func TestToolCatalog_GetWithMetadataIncludesCachedFields(t *testing.T) {
	catalog := newToolCatalog()
	tool := catalogTestTool()
	catalog.Put(tool)
	catalog.SetSchemas(tool.ID, []mcp.ToolSchema{{Name: "s"}})
	catalog.SetResources(tool.ID,
		[]mcp.ResourceSchema{{URI: "r"}},
		[]mcp.ResourceTemplateSchema{{URITemplate: "t"}},
	)

	got, ok := catalog.GetWithMetadata(tool.ID)

	require.True(t, ok)
	assert.Len(t, got.Schemas, 1)
	assert.Len(t, got.Resources, 1)
	assert.Len(t, got.ResourceTemplates, 1)
}

func TestToolCatalog_GetWithMetadataReturnsFalseWhenMissing(t *testing.T) {
	catalog := newToolCatalog()

	_, ok := catalog.GetWithMetadata(catalogTestMissing)

	assert.False(t, ok)
}

// mustGet returns the tool under toolID or fails the test. It keeps the
// positive-path assertions focused on the property under test.
func mustGet(t *testing.T, catalog *toolCatalog, toolID mcp.ToolID) mcp.Tool {
	t.Helper()

	tool, ok := catalog.Get(toolID)
	require.True(t, ok, "expected tool %s in catalog", toolID)
	return tool
}
