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

	"github.com/tochemey/portcullis/mcp"
)

// ResourceFetcherExtensionID is the fixed identifier for the ResourceFetcher
// extension registered on the actor system.
const ResourceFetcherExtensionID = "resource-fetcher"

// ResourceFetcherExtension wraps a ResourceFetcher for registration as an
// actor system extension. The registrar resolves this extension to fetch
// resource metadata from backend MCP servers at registration time.
type ResourceFetcherExtension struct {
	fetcher mcp.ResourceFetcher
}

var _ goaktextension.Extension = (*ResourceFetcherExtension)(nil)

// NewResourceFetcherExtension creates an extension wrapping the given fetcher.
func NewResourceFetcherExtension(fetcher mcp.ResourceFetcher) *ResourceFetcherExtension {
	return &ResourceFetcherExtension{fetcher: fetcher}
}

// ID returns the unique identifier for this extension.
func (e *ResourceFetcherExtension) ID() string {
	return ResourceFetcherExtensionID
}

// Fetcher returns the wrapped ResourceFetcher.
func (e *ResourceFetcherExtension) Fetcher() mcp.ResourceFetcher {
	return e.fetcher
}
