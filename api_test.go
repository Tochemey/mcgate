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

package goaktmcp

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/tochemey/goakt-mcp/mcp"
)

func TestFilterToolsForTenant(t *testing.T) {
	open := mcp.Tool{ID: "open-tool"}
	guarded := mcp.Tool{ID: "guarded-tool", AuthorizationPolicy: mcp.AuthorizationPolicyTenantAllowlist}
	tools := []mcp.Tool{open, guarded}

	t.Run("no tenants configured means no restriction", func(t *testing.T) {
		visible := filterToolsForTenant(tools, nil, "anyone")
		assert.Len(t, visible, 2)
	})

	t.Run("configured tenant sees everything", func(t *testing.T) {
		tenants := []mcp.TenantConfig{{ID: "acme"}}
		visible := filterToolsForTenant(tools, tenants, "acme")
		assert.Len(t, visible, 2)
	})

	t.Run("unknown tenant does not see allowlist-guarded tools", func(t *testing.T) {
		tenants := []mcp.TenantConfig{{ID: "acme"}}
		visible := filterToolsForTenant(tools, tenants, "intruder")
		assert.Len(t, visible, 1)
		assert.Equal(t, mcp.ToolID("open-tool"), visible[0].ID)
	})
}
