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

package mcp

import (
	"context"
)

// Provider resolves credentials for a tenant and tool.
//
// Implementations should avoid long-lived secret storage. The broker may cache
// results with bounded TTL; providers are not required to implement caching.
type CredentialsProvider interface {
	// ID returns the provider identifier (e.g., "env", "file").
	ID() string

	// ResolveCredentials returns credentials for the given tenant and tool.
	// Returns (nil, nil) when the provider has no credentials for this combination.
	// Returns (nil, err) when resolution fails.
	ResolveCredentials(ctx context.Context, tenantID TenantID, toolID ToolID) (*Credentials, error)
}

// Credentials represents the credentials for a tenant and tool.
type Credentials struct {
	// TenantID is the identifier for the tenant.
	TenantID TenantID
	// ToolID is the identifier for the tool.
	ToolID ToolID
	// Values is the map of credential values. Keys are applied verbatim to
	// the backend transport when the session executor is created:
	//
	//   - stdio: each entry becomes an environment variable on the child
	//     process (overriding inherited and configured variables of the
	//     same name).
	//   - HTTP: each entry is sent as an HTTP request header (key = header
	//     name) on every outbound request.
	//   - gRPC: each entry is attached as outgoing gRPC metadata on every
	//     call (overriding static GRPCTransportConfig.Metadata keys).
	Values map[string]string
}
