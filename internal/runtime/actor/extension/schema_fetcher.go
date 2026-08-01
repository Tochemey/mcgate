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
	goaktextension "github.com/tochemey/goakt/v4/extension"

	"github.com/tochemey/mcgate/mcp"
)

// SchemaFetcherExtensionID is the fixed identifier for the SchemaFetcher
// extension registered on the actor system.
const SchemaFetcherExtensionID = "schema-fetcher"

// SchemaFetcherExtension wraps a SchemaFetcher for registration as an
// actor system extension. The registrar resolves this extension to fetch
// tool schemas from backend MCP servers at registration time.
type SchemaFetcherExtension struct {
	fetcher mcp.SchemaFetcher
}

var _ goaktextension.Extension = (*SchemaFetcherExtension)(nil)

// NewSchemaFetcherExtension creates an extension wrapping the given fetcher.
func NewSchemaFetcherExtension(fetcher mcp.SchemaFetcher) *SchemaFetcherExtension {
	return &SchemaFetcherExtension{fetcher: fetcher}
}

// ID returns the unique identifier for this extension.
func (e *SchemaFetcherExtension) ID() string {
	return SchemaFetcherExtensionID
}

// Fetcher returns the wrapped SchemaFetcher.
func (e *SchemaFetcherExtension) Fetcher() mcp.SchemaFetcher {
	return e.fetcher
}
