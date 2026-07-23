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
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tochemey/goakt-mcp/mcp"
)

// staticResolver is a test implementation of IdentityResolver that always
// returns fixed tenant and client IDs.
type staticResolver struct {
	tenantID mcp.TenantID
	clientID mcp.ClientID
}

func (r *staticResolver) ResolveIdentity(_ *http.Request) (mcp.TenantID, mcp.ClientID, error) {
	return r.tenantID, r.clientID, nil
}

// failingResolver is a test implementation that always returns an error.
type failingResolver struct{ err error }

func (r *failingResolver) ResolveIdentity(_ *http.Request) (mcp.TenantID, mcp.ClientID, error) {
	return "", "", r.err
}

func TestIngressConfig(t *testing.T) {
	t.Run("IdentityResolver field is settable", func(t *testing.T) {
		resolver := &staticResolver{tenantID: "acme", clientID: "c1"}
		cfg := mcp.IngressConfig{IdentityResolver: resolver}
		require.NotNil(t, cfg.IdentityResolver)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		tenantID, clientID, err := cfg.IdentityResolver.ResolveIdentity(req)
		require.NoError(t, err)
		assert.Equal(t, mcp.TenantID("acme"), tenantID)
		assert.Equal(t, mcp.ClientID("c1"), clientID)
	})
}

func TestIdentityResolver(t *testing.T) {
	t.Run("static resolver returns configured identity", func(t *testing.T) {
		resolver := &staticResolver{tenantID: "tenant-a", clientID: "client-b"}
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		tenantID, clientID, err := resolver.ResolveIdentity(req)
		require.NoError(t, err)
		assert.Equal(t, mcp.TenantID("tenant-a"), tenantID)
		assert.Equal(t, mcp.ClientID("client-b"), clientID)
	})

	t.Run("failing resolver propagates error", func(t *testing.T) {
		want := errors.New("unauthorized")
		resolver := &failingResolver{err: want}
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		_, _, err := resolver.ResolveIdentity(req)
		assert.ErrorIs(t, err, want)
	})
}
