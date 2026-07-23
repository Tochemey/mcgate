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

package http

import (
	"context"
	"net/http"
	"sync"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/tochemey/portcullis/internal/egress/mcpconv"
	"github.com/tochemey/portcullis/internal/security"
	"github.com/tochemey/portcullis/mcp"
)

// HTTPExecutor executes MCP tool invocations over HTTP using the streamable transport.
// Each executor owns a single MCP client session and is bound to one tool endpoint.
// It implements both [mcp.ToolExecutor] and [mcp.ToolStreamExecutor].
type HTTPExecutor struct {
	client   *sdkmcp.Client
	sess     *sdkmcp.ClientSession
	closed   sync.Once
	closeErr error
}

// Compile-time check that HTTPExecutor satisfies executor interfaces.
var (
	_ mcp.ToolExecutor       = (*HTTPExecutor)(nil)
	_ mcp.ToolStreamExecutor = (*HTTPExecutor)(nil)
	_ mcp.ResourceExecutor   = (*HTTPExecutor)(nil)
)

// Execute runs the MCP tools/call invocation and returns the result.
// The context should carry a request-scoped deadline. Execute does not
// set its own timeout.
func (e *HTTPExecutor) Execute(ctx context.Context, inv *mcp.Invocation) (*mcp.ExecutionResult, error) {
	if e.sess == nil {
		return &mcp.ExecutionResult{
			Status:      mcp.ExecutionStatusFailure,
			Err:         mcp.NewRuntimeError(mcp.ErrCodeTransportFailure, "session not connected"),
			Correlation: inv.Correlation,
		}, nil
	}

	name, args := mcpconv.ParamsFromInvocation(inv)
	params := &sdkmcp.CallToolParams{Name: name, Arguments: args}

	res, err := e.sess.CallTool(ctx, params)
	if err != nil {
		if ctx.Err() != nil {
			return &mcp.ExecutionResult{
				Status:      mcp.ExecutionStatusTimeout,
				Err:         mcp.WrapRuntimeError(mcp.ErrCodeInvocationTimeout, "invocation timed out", err),
				Correlation: inv.Correlation,
			}, nil
		}
		return &mcp.ExecutionResult{
			Status:      mcp.ExecutionStatusFailure,
			Err:         mcp.WrapRuntimeError(mcp.ErrCodeTransportFailure, "call failed", err),
			Correlation: inv.Correlation,
		}, nil
	}

	output := mcpconv.CallResultToOutput(res)
	status := mcp.ExecutionStatusSuccess
	var rErr *mcp.RuntimeError
	if res.IsError {
		status = mcp.ExecutionStatusFailure
		rErr = mcp.NewRuntimeError(mcp.ErrCodeInternal, mcpconv.ContentErrorText(res))
	}

	return &mcp.ExecutionResult{
		Status:      status,
		Output:      output,
		Err:         rErr,
		Correlation: inv.Correlation,
	}, nil
}

// ReadResource reads an MCP resource from the backend server via resources/read.
// The URI is extracted from the invocation parameters.
func (e *HTTPExecutor) ReadResource(ctx context.Context, inv *mcp.Invocation) (*mcp.ExecutionResult, error) {
	if e.sess == nil {
		return &mcp.ExecutionResult{
			Status:      mcp.ExecutionStatusFailure,
			Err:         mcp.NewRuntimeError(mcp.ErrCodeTransportFailure, "session not connected"),
			Correlation: inv.Correlation,
		}, nil
	}

	uri := mcpconv.ResourceParamsFromInvocation(inv)
	if uri == "" {
		return &mcp.ExecutionResult{
			Status:      mcp.ExecutionStatusFailure,
			Err:         mcp.NewRuntimeError(mcp.ErrCodeInvalidRequest, "resource URI is required"),
			Correlation: inv.Correlation,
		}, nil
	}

	params := &sdkmcp.ReadResourceParams{URI: uri}
	res, err := e.sess.ReadResource(ctx, params)
	if err != nil {
		if ctx.Err() != nil {
			return &mcp.ExecutionResult{
				Status:      mcp.ExecutionStatusTimeout,
				Err:         mcp.WrapRuntimeError(mcp.ErrCodeInvocationTimeout, "resource read timed out", err),
				Correlation: inv.Correlation,
			}, nil
		}
		return &mcp.ExecutionResult{
			Status:      mcp.ExecutionStatusFailure,
			Err:         mcp.WrapRuntimeError(mcp.ErrCodeTransportFailure, "resource read failed", err),
			Correlation: inv.Correlation,
		}, nil
	}

	output := mcpconv.ReadResourceResultToOutput(res)
	return &mcp.ExecutionResult{
		Status:      mcp.ExecutionStatusSuccess,
		Output:      output,
		Correlation: inv.Correlation,
	}, nil
}

// ExecuteStream runs the MCP tools/call invocation and returns a StreamingResult.
//
// The HTTP executor does not forward progress notifications from the backend
// MCP server. The Progress channel is provided for interface compatibility but
// will always be empty and closed immediately before the final result arrives.
// Use the standard Execute method when progress forwarding is not required.
func (e *HTTPExecutor) ExecuteStream(ctx context.Context, inv *mcp.Invocation) (*mcp.StreamingResult, error) {
	progressCh := make(chan mcp.ProgressEvent)
	finalCh := make(chan *mcp.ExecutionResult, 1)

	go func() {
		defer close(finalCh)
		defer close(progressCh)

		// Execute always returns a nil Go error; all failures are expressed in
		// the returned ExecutionResult. The underscore is intentional.
		result, _ := e.Execute(ctx, inv)
		if result == nil {
			result = &mcp.ExecutionResult{
				Status:      mcp.ExecutionStatusFailure,
				Err:         mcp.NewRuntimeError(mcp.ErrCodeInternal, "nil result from execute"),
				Correlation: inv.Correlation,
			}
		}
		finalCh <- result
	}()

	return &mcp.StreamingResult{
		Progress: progressCh,
		Final:    finalCh,
	}, nil
}

// Close releases the HTTP session and connection. Close is safe to call
// multiple times; only the first call performs cleanup.
func (e *HTTPExecutor) Close() error {
	e.closed.Do(func() {
		if e.sess != nil {
			e.closeErr = e.sess.Close()
			e.sess = nil
		}
		e.client = nil
	})
	return e.closeErr
}

// NewHTTPExecutor creates an executor by connecting to the configured HTTP endpoint.
//
// When cfg.TLS is set, a per-tool HTTP client is created with the appropriate
// TLS configuration (custom CA, client certificates, or InsecureSkipVerify).
// When cfg.TLS is nil and fallbackClient is non-nil, fallbackClient is used.
// When both are nil, a default http.Client is used.
//
// The fallbackClient should not set a global Timeout since request-level
// deadlines are managed by the session's context.
//
// creds holds credentials resolved by the credential broker. Each entry is
// sent as an HTTP request header (key = header name, value = header value)
// on every outbound request.
func NewHTTPExecutor(cfg *mcp.HTTPTransportConfig, fallbackClient *http.Client, startupTimeout time.Duration, creds map[string]string) (*HTTPExecutor, error) {
	if cfg == nil || cfg.URL == "" {
		return nil, mcp.NewRuntimeError(mcp.ErrCodeInvalidRequest, "http config required")
	}

	httpClient, err := buildHTTPClient(cfg, fallbackClient)
	if err != nil {
		return nil, err
	}
	httpClient = clientWithWrappedTransport(httpClient, creds)

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "portcullis", Version: mcp.Version()}, nil)
	transport := &sdkmcp.StreamableClientTransport{
		Endpoint:     cfg.URL,
		HTTPClient:   httpClient,
		MaxRetries:   2,
		OAuthHandler: cfg.OAuthHandler,
	}

	ctx := context.Background()
	if startupTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, startupTimeout)
		defer cancel()
	}

	sess, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, mcp.WrapRuntimeError(mcp.ErrCodeTransportFailure, "http connect failed", err)
	}

	return &HTTPExecutor{client: client, sess: sess}, nil
}

// buildHTTPClient returns the http.Client to use for this tool.
// When the tool has TLS configuration, a new client with a cloned transport
// is created so that TLS settings are isolated per tool. Otherwise the
// fallback client (or a default) is returned.
func buildHTTPClient(cfg *mcp.HTTPTransportConfig, fallback *http.Client) (*http.Client, error) {
	if cfg.TLS == nil {
		if fallback != nil {
			return fallback, nil
		}
		return &http.Client{}, nil
	}

	tlsCfg, err := security.BuildClientTLSConfig(cfg.TLS)
	if err != nil {
		return nil, err
	}

	base := http.DefaultTransport
	if fallback != nil && fallback.Transport != nil {
		base = fallback.Transport
	}

	if t, ok := base.(*http.Transport); ok {
		clone := t.Clone()
		clone.TLSClientConfig = tlsCfg
		return &http.Client{Transport: clone}, nil
	}

	return &http.Client{
		Transport: &http.Transport{TLSClientConfig: tlsCfg},
	}, nil
}

// clientWithWrappedTransport returns a shallow copy of httpClient whose
// transport injects the resolved credentials as request headers and is
// instrumented for tracing (see wrapTransport). A copy is returned so the
// caller-supplied (possibly shared) client is never mutated: mutating it in
// place would double-wrap otelhttp on every session and race concurrent
// readers.
func clientWithWrappedTransport(httpClient *http.Client, creds map[string]string) *http.Client {
	base := httpClient.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	clone := *httpClient
	clone.Transport = wrapTransport(withCredentialHeaders(base, creds))
	return &clone
}
