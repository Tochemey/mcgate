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

// Package http implements the MCP Streamable HTTP ingress handler for mcgate.
//
// It wraps the MCP go-sdk [sdkmcp.StreamableHTTPHandler] and routes incoming
// tool-call requests through the Gateway routing layer. Caller identity is
// resolved once per MCP session via the configured [mcp.IdentityResolver] and
// captured in per-tool handler closures, so no allocation occurs on the hot
// per-request path.
//
// # Session lifecycle
//
// In stateful mode (default), the SDK assigns each MCP client a unique
// Mcp-Session-Id on first contact. Subsequent requests for that session reuse
// the same server instance and its registered tool handlers, keeping identity
// resolution to once per connection. In stateless mode, the SDK creates a
// temporary session for every HTTP request.
//
// # Tool registration
//
// At session initialization, the handler calls [Invoker.ListTools] to discover
// the current set of registered tools and registers each with a generic
// {"type":"object"} input schema that accepts any JSON object. This is
// correct for a pass-through gateway that forwards arguments verbatim to
// the backend MCP server. Tool changes after session creation take effect
// on the next new session.
package http
