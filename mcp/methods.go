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

// MCP JSON-RPC method names. These mirror the protocol names used by the
// modelcontextprotocol/go-sdk (which keeps them unexported) so that portcullis
// can route invocations by Invocation.Method without re-inventing the wire
// strings at every call site.
const (
	// MethodToolsCall is the MCP method for invoking a tool.
	MethodToolsCall = "tools/call"
	// MethodResourcesRead is the MCP method for reading a resource.
	MethodResourcesRead = "resources/read"
)

// Keys used inside Invocation.Params for tool and resource invocations. The
// ingress/egress conversion helpers read and write these keys to translate
// between the gateway's Invocation shape and the SDK request shape.
const (
	// ParamKeyName carries the backend tool name inside a tools/call invocation.
	ParamKeyName = "name"
	// ParamKeyArguments carries the tool argument map inside a tools/call
	// invocation. The value is typically map[string]any.
	ParamKeyArguments = "arguments"
	// ParamKeyURI carries the resource URI inside a resources/read invocation.
	ParamKeyURI = "uri"
)

// Keys used inside ExecutionResult.Output for tool and resource results.
const (
	// OutputKeyContent is the top-level key under which tools/call results
	// expose their content items. The value is typically []any where each
	// element is a content-item map.
	OutputKeyContent = "content"
	// OutputKeyContents is the top-level key under which resources/read
	// results expose their resource contents. The value is typically []any.
	OutputKeyContents = "contents"
	// OutputKeyStructuredContent is the top-level key for the SDK's
	// structured-content payload that clients with schema-aware tooling
	// can consume directly without parsing the textual content list.
	OutputKeyStructuredContent = "structuredContent"
)

// Keys on an individual content item inside tools/call results and on a
// resource content item inside resources/read results.
const (
	// ContentKeyType labels the content-item discriminator ("text", "image",
	// "audio", "resource", "resource_link", "unknown").
	ContentKeyType = "type"
	// ContentKeyText carries the UTF-8 text payload of a text content item.
	ContentKeyText = "text"
	// ContentKeyURI identifies the resource referenced by a resource content item.
	ContentKeyURI = "uri"
	// ContentKeyMIMEType carries the MIME type of an image, audio, or
	// resource content item.
	ContentKeyMIMEType = "mimeType"
	// ContentKeyBlob carries the base64-encoded blob payload of a binary
	// resource content item.
	ContentKeyBlob = "blob"
	// ContentKeyData carries the base64-encoded payload of an image or
	// audio content item.
	ContentKeyData = "data"
	// ContentKeyName carries the logical name of a resource-link content item.
	ContentKeyName = "name"
)

// Discriminator values used under the ContentKeyType field.
const (
	// ContentTypeText labels text content items.
	ContentTypeText = "text"
	// ContentTypeImage labels image content items.
	ContentTypeImage = "image"
	// ContentTypeAudio labels audio content items.
	ContentTypeAudio = "audio"
	// ContentTypeResource labels embedded-resource content items.
	ContentTypeResource = "resource"
	// ContentTypeResourceLink labels resource-link content items.
	ContentTypeResourceLink = "resource_link"
	// ContentTypeUnknown labels unrecognized content items so that
	// round-trips preserve the discriminator without losing data.
	ContentTypeUnknown = "unknown"
)
