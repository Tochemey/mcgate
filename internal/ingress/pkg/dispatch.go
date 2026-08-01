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

package pkg

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/tochemey/mcgate/mcp"
)

// DispatchToolCall translates an SDK CallToolRequest into a gateway Invocation,
// forwards it through gw, and converts the result back to an SDK CallToolResult.
//
// All errors (invalid requests, policy denials, timeouts, execution failures)
// are returned as tool errors (IsError: true) so the LLM can observe and
// self-correct. The returned Go error is always nil.
func DispatchToolCall(
	ctx context.Context,
	gw Invoker,
	req *sdkmcp.CallToolRequest,
	toolID mcp.ToolID,
	tenantID mcp.TenantID,
	clientID mcp.ClientID,
) (*sdkmcp.CallToolResult, error) {
	return DispatchToolCallRestricted(ctx, gw, req, toolID, tenantID, clientID, nil)
}

// DispatchToolCallRestricted is DispatchToolCall with an optional allowlist
// of backend tool names. When allowedNames is non-nil, an invocation whose
// nested {"name": ...} shape addresses a backend tool outside the allowlist
// is rejected as a tool error. Registration paths that advertise explicit
// per-sub-tool schemas pass the advertised names here so a client cannot use
// the nested shape to escape the advertised tool surface (and its input
// schemas) and reach arbitrary backend tools.
func DispatchToolCallRestricted(
	ctx context.Context,
	gw Invoker,
	req *sdkmcp.CallToolRequest,
	toolID mcp.ToolID,
	tenantID mcp.TenantID,
	clientID mcp.ClientID,
	allowedNames map[string]struct{},
) (*sdkmcp.CallToolResult, error) {
	inv, err := requestToInvocation(req, toolID, tenantID, clientID, allowedNames)
	if err != nil {
		r := new(sdkmcp.CallToolResult)
		r.SetError(err)
		return r, nil
	}

	// Propagate OAuth scopes from validated bearer token into the invocation
	// so the policy layer can make scope-aware authorization decisions.
	if info := auth.TokenInfoFromContext(ctx); info != nil && len(info.Scopes) > 0 {
		inv.Scopes = info.Scopes
	}

	result, gwErr := gw.Invoke(ctx, inv)
	return ExecutionResultToCallToolResult(result, gwErr), nil
}

// RequestToInvocation converts a low-level SDK CallToolRequest into the gateway's
// Invocation type. Arguments are unmarshaled from JSON into a map so the egress
// layer can forward them to the backend MCP server via [mcpconv.ParamsFromInvocation].
//
// The toolID parameter is the canonical gateway tool ID used for routing to the
// correct tool supervisor. It is set by the caller (RegisterTool) when capturing
// the closure. req.Params.Name may differ from toolID when schema-derived tools
// are registered: each backend sub-tool (e.g. "read_file", "write_file") gets
// its own SDK registration, but they all route to the same gateway tool.
//
// Backend tool name resolution: when args["name"] is present it is used as the
// backend tool name and args["arguments"] as the backend arguments. This allows
// LLMs and MCP clients to route invocations to the correct sub-tool on the
// backend server via the standard MCP tools/call shape:
//
//	{"name": "<backend-tool>", "arguments": {...}}
//
// If args["name"] is absent (single-operation tools where the SDK tool name
// equals the backend tool name), req.Params.Name is used as a fallback and the
// entire args map is forwarded as arguments.
func RequestToInvocation(req *sdkmcp.CallToolRequest, toolID mcp.ToolID, tenantID mcp.TenantID, clientID mcp.ClientID) (*mcp.Invocation, error) {
	return requestToInvocation(req, toolID, tenantID, clientID, nil)
}

// requestToInvocation is the allowlist-aware core of RequestToInvocation.
// When allowedNames is non-nil, the nested {"name": ...} redirect must land
// on an advertised backend tool name; anything else is rejected so schema-
// restricted tool exposure stays enforceable.
func requestToInvocation(req *sdkmcp.CallToolRequest, toolID mcp.ToolID, tenantID mcp.TenantID, clientID mcp.ClientID, allowedNames map[string]struct{}) (*mcp.Invocation, error) {
	if req == nil || req.Params == nil {
		return nil, fmt.Errorf("invalid tool call request: request and params are required")
	}

	var args map[string]any
	if len(req.Params.Arguments) > 0 {
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return nil, fmt.Errorf("invalid tool arguments: %w", err)
		}
	}

	// Determine the backend tool name and arguments.
	backendName, _ := args[mcp.ParamKeyName].(string)
	var backendArgs any
	if backendName != "" {
		// Nested shape: client specified a backend sub-tool name explicitly.
		if allowedNames != nil {
			if _, ok := allowedNames[backendName]; !ok {
				return nil, fmt.Errorf("unknown backend tool %q: not among the advertised tools", backendName)
			}
		}
		backendArgs = args[mcp.ParamKeyArguments]
	} else {
		// Flat shape: no sub-tool name; use the gateway tool ID as the backend
		// tool name and forward the entire args map as arguments.
		backendName = req.Params.Name
		backendArgs = args
	}

	return &mcp.Invocation{
		ToolID: toolID,
		Method: mcp.MethodToolsCall,
		Params: map[string]any{
			mcp.ParamKeyName:      backendName,
			mcp.ParamKeyArguments: backendArgs,
		},
		Correlation: mcp.CorrelationMeta{
			TenantID:  tenantID,
			ClientID:  clientID,
			RequestID: NewRequestID(),
		},
		ReceivedAt: time.Now(),
	}, nil
}

// ExecutionResultToCallToolResult converts a gateway ExecutionResult (and any
// accompanying gateway error) into an SDK CallToolResult.
//
// Successful results have their output serialized back to SDK content types.
// Non-success statuses (denied, throttled, timeout, failure) are reported as
// tool errors rather than protocol errors, preserving the LLM's ability to
// observe and correct the failure.
func ExecutionResultToCallToolResult(res *mcp.ExecutionResult, gwErr error) *sdkmcp.CallToolResult {
	if gwErr != nil && res == nil {
		r := new(sdkmcp.CallToolResult)
		r.SetError(gwErr)
		return r
	}

	if res == nil {
		r := new(sdkmcp.CallToolResult)
		r.SetError(fmt.Errorf("empty result from gateway"))
		return r
	}

	if res.Status != mcp.ExecutionStatusSuccess {
		errMsg := string(res.Status)
		if res.Err != nil {
			errMsg = res.Err.Error()
		}
		r := new(sdkmcp.CallToolResult)
		r.SetError(fmt.Errorf("%s", errMsg))
		return r
	}

	return OutputToCallToolResult(res.Output)
}

// OutputToCallToolResult converts the gateway's normalized output map back to
// an SDK CallToolResult. Text content items produced by the egress layer are
// reconstructed into typed [sdkmcp.TextContent] entries; all other content
// falls back to a single JSON-serialized text entry.
//
// The full output map is also attached as StructuredContent so consumers with
// schema-aware tooling can access the raw data without parsing text.
func OutputToCallToolResult(output map[string]any) *sdkmcp.CallToolResult {
	if output == nil {
		return &sdkmcp.CallToolResult{}
	}

	result := &sdkmcp.CallToolResult{StructuredContent: output}

	// Attempt to reconstruct typed text content from the "content" key written
	// by mcpconv.CallResultToOutput. Only text items are reconstructed; image,
	// audio, and embedded resource items fall through to JSON serialization.
	if rawContent, ok := output[mcp.OutputKeyContent]; ok {
		// json.Unmarshal into interface{} produces []any for arrays, not
		// []map[string]any, so we must handle both the in-memory path (direct
		// []map[string]any from contentToSlice) and the JSON-decoded path ([]any).
		var items []map[string]any
		switch v := rawContent.(type) {
		case []map[string]any:
			items = v
		case []any:
			items = make([]map[string]any, 0, len(v))
			for _, raw := range v {
				if m, ok := raw.(map[string]any); ok {
					items = append(items, m)
				}
			}
		}
		if len(items) > 0 {
			content := make([]sdkmcp.Content, 0, len(items))
			for _, item := range items {
				if typ, _ := item[mcp.ContentKeyType].(string); typ == mcp.ContentTypeText {
					if text, _ := item[mcp.ContentKeyText].(string); text != "" {
						content = append(content, &sdkmcp.TextContent{Text: text})
					}
				}
			}
			if len(content) > 0 {
				result.Content = content
				return result
			}
		}
	}

	// Fallback: serialize the full output as a single JSON text entry. This
	// preserves all content (image, audio, structured) for clients that can
	// parse JSON in text content.
	data, err := json.Marshal(output)
	if err != nil {
		r := new(sdkmcp.CallToolResult)
		r.SetError(fmt.Errorf("failed to serialize output: %w", err))
		return r
	}
	result.Content = []sdkmcp.Content{&sdkmcp.TextContent{Text: string(data)}}
	return result
}

// DispatchResourceRead translates an SDK ReadResourceRequest into a gateway
// Invocation with Method "resources/read", forwards it through gw, and converts
// the result back to an SDK ReadResourceResult.
func DispatchResourceRead(
	ctx context.Context,
	gw Invoker,
	req *sdkmcp.ReadResourceRequest,
	toolID mcp.ToolID,
	tenantID mcp.TenantID,
	clientID mcp.ClientID,
) (*sdkmcp.ReadResourceResult, error) {
	if req == nil || req.Params == nil {
		return nil, fmt.Errorf("invalid read resource request: request and params are required")
	}

	inv := &mcp.Invocation{
		ToolID: toolID,
		Method: mcp.MethodResourcesRead,
		Params: map[string]any{
			mcp.ParamKeyURI: req.Params.URI,
		},
		Correlation: mcp.CorrelationMeta{
			TenantID:  tenantID,
			ClientID:  clientID,
			RequestID: NewRequestID(),
		},
		ReceivedAt: time.Now(),
	}

	// Propagate OAuth scopes from validated bearer token into the invocation
	// so the policy layer can make scope-aware authorization decisions.
	if info := auth.TokenInfoFromContext(ctx); info != nil && len(info.Scopes) > 0 {
		inv.Scopes = info.Scopes
	}

	result, gwErr := gw.Invoke(ctx, inv)
	return ExecutionResultToReadResourceResult(result, gwErr)
}

// ExecutionResultToReadResourceResult converts a gateway ExecutionResult into
// an SDK ReadResourceResult. Non-success statuses are returned as errors.
//
// When both gwErr and res are non-nil, the result takes precedence (matching
// the behavior of [ExecutionResultToCallToolResult]).
func ExecutionResultToReadResourceResult(res *mcp.ExecutionResult, gwErr error) (*sdkmcp.ReadResourceResult, error) {
	if gwErr != nil && res == nil {
		return nil, gwErr
	}
	if res == nil {
		return nil, fmt.Errorf("empty result from gateway")
	}
	if res.Status != mcp.ExecutionStatusSuccess {
		errMsg := string(res.Status)
		if res.Err != nil {
			errMsg = res.Err.Error()
		}
		return nil, fmt.Errorf("%s", errMsg)
	}

	return OutputToReadResourceResult(res.Output), nil
}

// OutputToReadResourceResult converts the gateway's normalized output map back
// to an SDK ReadResourceResult. The output is expected to contain a "contents"
// key with a slice of resource content maps, each containing "uri" and
// optionally "mimeType", "text", and "blob" fields.
func OutputToReadResourceResult(output map[string]any) *sdkmcp.ReadResourceResult {
	if output == nil {
		return &sdkmcp.ReadResourceResult{}
	}

	result := &sdkmcp.ReadResourceResult{}

	rawContents, ok := output[mcp.OutputKeyContents]
	if !ok {
		data, err := json.Marshal(output)
		if err != nil {
			return result
		}
		result.Contents = []*sdkmcp.ResourceContents{
			{Text: string(data)},
		}
		return result
	}

	var items []map[string]any
	switch v := rawContents.(type) {
	case []map[string]any:
		items = v
	case []any:
		items = make([]map[string]any, 0, len(v))
		for _, raw := range v {
			if m, ok := raw.(map[string]any); ok {
				items = append(items, m)
			}
		}
	}

	if len(items) > 0 {
		contents := make([]*sdkmcp.ResourceContents, 0, len(items))
		for _, item := range items {
			rc := &sdkmcp.ResourceContents{}
			if uri, _ := item[mcp.ContentKeyURI].(string); uri != "" {
				rc.URI = uri
			}
			if mimeType, _ := item[mcp.ContentKeyMIMEType].(string); mimeType != "" {
				rc.MIMEType = mimeType
			}
			if text, _ := item[mcp.ContentKeyText].(string); text != "" {
				rc.Text = text
			}
			// Blob arrives as []byte on the in-memory path, but as a base64
			// string after a JSON round-trip (encoding/json marshals []byte
			// to base64). Handle both so binary resource content survives
			// serialization boundaries instead of being silently dropped.
			switch blob := item[mcp.ContentKeyBlob].(type) {
			case []byte:
				if len(blob) > 0 {
					rc.Blob = blob
				}
			case string:
				if decoded, err := base64.StdEncoding.DecodeString(blob); err == nil && len(decoded) > 0 {
					rc.Blob = decoded
				}
			}
			contents = append(contents, rc)
		}
		result.Contents = contents
	}

	return result
}

// requestIDCounter is the fallback counter used when crypto/rand fails.
var requestIDCounter atomic.Uint64

// NewRequestID generates a cryptographically random 16-hex-character request
// identifier. If crypto/rand fails, it falls back to a time+counter based ID
// packed into 8 bytes to guarantee the same 16-hex-character length.
func NewRequestID() mcp.RequestID {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		ts := uint32(time.Now().UnixNano())
		seq := uint32(requestIDCounter.Add(1))
		b[0] = byte(ts >> 24)
		b[1] = byte(ts >> 16)
		b[2] = byte(ts >> 8)
		b[3] = byte(ts)
		b[4] = byte(seq >> 24)
		b[5] = byte(seq >> 16)
		b[6] = byte(seq >> 8)
		b[7] = byte(seq)
	}
	return mcp.RequestID(fmt.Sprintf("%x", b))
}
