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

package grpc

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/tochemey/goakt-mcp/internal/ingress/grpc/pb"
	"github.com/tochemey/goakt-mcp/internal/ingress/pkg"
	"github.com/tochemey/goakt-mcp/mcp"
)

// mcpMethod is the standard MCP JSON-RPC method name for tool calls.
const mcpMethod = "tools/call"

// invocationParamName is the key in the invocation params map that holds the
// backend tool name.
const invocationParamName = "name"

// invocationParamArguments is the key in the invocation params map that holds
// the backend tool arguments.
const invocationParamArguments = "arguments"

// Compile-time check that server implements the generated gRPC service interface.
var _ pb.MCPToolServiceServer = (*server)(nil)

// server implements the MCPToolService gRPC service. It routes requests through
// the gateway's Invoker/StreamInvoker interface and resolves caller identity via
// the configured GRPCIdentityResolver.
type server struct {
	pb.UnimplementedMCPToolServiceServer
	gw       pkg.StreamInvoker
	resolver mcp.GRPCIdentityResolver
	cache    *toolNameCache // nil when caching is disabled
	// enterpriseAuth, when non-nil, is enforced directly by every RPC handler
	// so the config's promise ("every incoming RPC must carry a valid Bearer
	// token") holds even when the operator did not install AuthInterceptors
	// on the grpc.Server. When the interceptors are installed, the handler
	// check is skipped (the validated TokenInfo is already on the context).
	enterpriseAuth *mcp.EnterpriseAuthConfig
}

// NewServer creates a new MCPToolService gRPC server that routes tool calls
// through gw and resolves caller identity via cfg.IdentityResolver.
//
// When cfg.EnterpriseAuth is set and cfg.IdentityResolver is nil, a
// [mcp.NewGRPCTokenIdentityResolver] is installed automatically. NewServer
// returns an error when both IdentityResolver and EnterpriseAuth are nil.
//
// Tool name resolution results are cached with a configurable TTL
// (cfg.ToolCacheTTL; zero uses [mcp.DefaultToolCacheTTL], negative disables).
func NewServer(gw pkg.StreamInvoker, cfg mcp.GRPCIngressConfig) (pb.MCPToolServiceServer, error) {
	if gw == nil {
		return nil, fmt.Errorf("ingress/grpc: gateway must not be nil")
	}

	if err := resolveGRPCAuthDefaults(&cfg); err != nil {
		return nil, err
	}

	if cfg.IdentityResolver == nil {
		return nil, fmt.Errorf("ingress/grpc: IdentityResolver must not be nil")
	}

	ttl := cfg.ToolCacheTTL
	if ttl == 0 {
		ttl = mcp.DefaultToolCacheTTL
	}

	var cache *toolNameCache
	if ttl > 0 {
		cache = newToolNameCache(ttl)
	}

	return &server{
		gw:             gw,
		resolver:       cfg.IdentityResolver,
		cache:          cache,
		enterpriseAuth: cfg.EnterpriseAuth,
	}, nil
}

// authenticate enforces Bearer token authentication when EnterpriseAuth is
// configured. It is a no-op when EnterpriseAuth is nil or when the request
// context already carries a validated token (the AuthInterceptors path).
// Returns the enriched context on success.
func (s *server) authenticate(ctx context.Context) (context.Context, error) {
	if s.enterpriseAuth == nil {
		return ctx, nil
	}
	if mcp.GRPCTokenInfoFromContext(ctx) != nil {
		return ctx, nil
	}
	return authenticateGRPC(ctx, s.enterpriseAuth)
}

// ListTools returns all tools currently registered in the gateway along with
// their schemas.
//
// The caller must satisfy the same authentication requirements as CallTool:
// EnterpriseAuth (when configured) and identity resolution. Without these
// checks the full tool registry — names, descriptions, and input schemas —
// would be served unauthenticated.
func (s *server) ListTools(ctx context.Context, _ *pb.ListToolsRequest) (*pb.ListToolsResponse, error) {
	ctx, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}

	tenantID, _, err := s.resolver.ResolveGRPCIdentity(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "identity resolution: %v", err)
	}

	tools, err := pkg.ListToolsFor(ctx, s.gw, tenantID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list tools: %v", err)
	}
	return toolsToListToolsResponse(tools), nil
}

// CallTool performs a synchronous tool invocation and returns the result.
//
// The handler resolves the caller identity from gRPC metadata, maps the
// tool_name to a gateway ToolID, builds an Invocation, and forwards it
// through the gateway. Identity resolution failures return
// codes.Unauthenticated; unknown tool names return codes.NotFound.
func (s *server) CallTool(ctx context.Context, req *pb.CallToolRequest) (*pb.CallToolResponse, error) {
	ctx, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}

	inv, err := s.buildInvocation(ctx, req.GetToolName(), req.GetArguments())
	if err != nil {
		return nil, err
	}

	result, gwErr := s.gw.Invoke(ctx, inv)
	if gwErr != nil && result == nil {
		return nil, status.Errorf(codes.Internal, "invoke: %v", gwErr)
	}

	return executionResultToCallToolResponse(result), nil
}

// CallToolStream performs a tool invocation with streaming progress support.
//
// The server sends zero or more CallToolStreamResponse messages containing
// ProgressEvent payloads, followed by exactly one message containing the
// final CallToolResponse.
func (s *server) CallToolStream(req *pb.CallToolStreamRequest, stream pb.MCPToolService_CallToolStreamServer) error {
	ctx, err := s.authenticate(stream.Context())
	if err != nil {
		return err
	}

	inv, err := s.buildInvocation(ctx, req.GetToolName(), req.GetArguments())
	if err != nil {
		return err
	}

	sr, gwErr := s.gw.InvokeStream(ctx, inv)
	if gwErr != nil {
		return status.Errorf(codes.Internal, "invoke stream: %v", gwErr)
	}

	// Deliver progress events as they arrive.
	for pe := range sr.Progress {
		var token string
		if pe.ProgressToken != nil {
			token = fmt.Sprintf("%v", pe.ProgressToken)
		}
		msg := &pb.CallToolStreamResponse{
			Payload: &pb.CallToolStreamResponse_Progress{
				Progress: &pb.ProgressEvent{
					ProgressToken: token,
					Progress:      pe.Progress,
					Total:         pe.Total,
					Message:       pe.Message,
				},
			},
		}
		if err := stream.Send(msg); err != nil {
			return status.Errorf(codes.Internal, "send progress: %v", err)
		}
	}

	// Deliver the final result.
	result := <-sr.Final
	msg := &pb.CallToolStreamResponse{
		Payload: &pb.CallToolStreamResponse_Result{
			Result: executionResultToCallToolResponse(result),
		},
	}
	return stream.Send(msg)
}

// buildInvocation resolves the caller identity and tool ID, then constructs an
// [mcp.Invocation] from the gRPC request parameters.
//
// Returns a gRPC status error on identity resolution failure
// (codes.Unauthenticated) or unknown tool name (codes.NotFound).
func (s *server) buildInvocation(ctx context.Context, toolName string, argsBytes []byte) (*mcp.Invocation, error) {
	if toolName == "" {
		return nil, status.Error(codes.InvalidArgument, "tool_name must not be empty")
	}

	tenantID, clientID, err := s.resolver.ResolveGRPCIdentity(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "identity resolution: %v", err)
	}

	var toolID mcp.ToolID
	if s.cache != nil {
		toolID, err = s.cache.resolve(ctx, s.gw, toolName)
	} else {
		toolID, err = resolveToolID(ctx, s.gw, toolName)
	}
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "%v", err)
	}

	var args map[string]any
	if len(argsBytes) > 0 {
		if err := json.Unmarshal(argsBytes, &args); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid arguments JSON: %v", err)
		}
	}

	inv := &mcp.Invocation{
		ToolID: toolID,
		Method: mcpMethod,
		Params: map[string]any{
			invocationParamName:      toolName,
			invocationParamArguments: args,
		},
		Correlation: mcp.CorrelationMeta{
			TenantID:  tenantID,
			ClientID:  clientID,
			RequestID: pkg.NewRequestID(),
		},
		ReceivedAt: time.Now(),
	}

	// Propagate OAuth scopes from the validated bearer token into the
	// invocation so the policy layer can make scope-aware decisions.
	if info := mcp.GRPCTokenInfoFromContext(ctx); info != nil && len(info.Scopes) > 0 {
		inv.Scopes = info.Scopes
	}

	return inv, nil
}
