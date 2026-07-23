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

package config

import (
	"fmt"

	"github.com/tochemey/portcullis/internal/discovery"
	"github.com/tochemey/portcullis/mcp"
)

// Validate checks cfg for critical errors. Returns nil when valid.
func Validate(cfg *mcp.Config) error {
	if err := validateCluster(&cfg.Cluster); err != nil {
		return err
	}

	if err := validateTenants(cfg.Tenants); err != nil {
		return err
	}

	if err := validateTools(cfg.Tools); err != nil {
		return err
	}
	return nil
}

// validateCluster ensures a discovery provider is set when clustering is on.
func validateCluster(c *mcp.ClusterConfig) error {
	if c.Enabled && discovery.IsNilDiscoveryProvider(c.DiscoveryProvider) {
		return fmt.Errorf("cluster.discovery_provider is required when cluster is enabled")
	}
	return nil
}

// validateTenants verifies that every tenant has a non-empty, unique ID.
func validateTenants(tenants []mcp.TenantConfig) error {
	seen := make(map[mcp.TenantID]bool, len(tenants))
	for i, t := range tenants {
		if t.ID == "" {
			return fmt.Errorf("tenants[%d]: id is required", i)
		}
		if seen[t.ID] {
			return fmt.Errorf("tenants: duplicate tenant id %q", t.ID)
		}
		seen[t.ID] = true
	}
	return nil
}

// validateTools verifies that every tool has a non-empty, unique ID and passes
// transport-specific validation.
func validateTools(tools []mcp.Tool) error {
	seen := make(map[mcp.ToolID]bool, len(tools))
	for i, t := range tools {
		if t.ID == "" {
			return fmt.Errorf("tools[%d]: id is required", i)
		}
		if seen[t.ID] {
			return fmt.Errorf("tools: duplicate tool id %q", t.ID)
		}
		seen[t.ID] = true
		if err := mcp.ValidateTool(t); err != nil {
			return fmt.Errorf("tools[%d] %q: %w", i, t.ID, err)
		}
	}
	return nil
}
