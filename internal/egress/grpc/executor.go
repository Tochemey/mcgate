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
	"io"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/tochemey/mcgate/internal/security"
	"github.com/tochemey/mcgate/mcp"
)

// GRPCExecutor executes tool invocations over gRPC using dynamic protobuf
// messages. Each executor owns a single gRPC client connection and is bound
// to one service endpoint. It implements both [mcp.ToolExecutor] and
// [mcp.ToolStreamExecutor].
type GRPCExecutor struct {
	conn    *grpc.ClientConn
	service string
	method  string
	md      metadata.MD

	// serviceDesc is the full service descriptor, used to resolve per-invocation
	// methods when method is empty (all RPCs exposed).
	serviceDesc protoreflect.ServiceDescriptor

	// inputDesc and outputDesc are set when a specific method is configured.
	// When method is empty, these are nil and descriptors are resolved per-call
	// from serviceDesc.
	inputDesc  protoreflect.MessageDescriptor
	outputDesc protoreflect.MessageDescriptor

	closed   sync.Once
	closeErr error
}

// Compile-time check that GRPCExecutor satisfies both executor interfaces.
var (
	_ mcp.ToolExecutor       = (*GRPCExecutor)(nil)
	_ mcp.ToolStreamExecutor = (*GRPCExecutor)(nil)
)

// NewGRPCExecutor creates an executor by dialing the configured gRPC endpoint
// and resolving proto descriptors for the target service.
//
// When cfg.DescriptorSet is set, descriptors are loaded from the local file.
// When cfg.Reflection is true, descriptors are fetched via gRPC server
// reflection from the live backend. Exactly one of these must be configured
// (enforced by [mcp.ValidateTool]).
//
// When cfg.Method is set, only that single RPC is resolved. When empty, the
// full service descriptor is stored and methods are resolved per-invocation.
//
// creds holds credentials resolved by the credential broker. Each entry is
// attached as outgoing gRPC metadata on every call, merged with cfg.Metadata;
// credentials win on key conflicts.
func NewGRPCExecutor(cfg *mcp.GRPCTransportConfig, startupTimeout time.Duration, creds map[string]string) (*GRPCExecutor, error) {
	if cfg == nil {
		return nil, mcp.NewRuntimeError(mcp.ErrCodeInvalidRequest, "grpc config required")
	}
	if cfg.Target == "" {
		return nil, mcp.NewRuntimeError(mcp.ErrCodeInvalidRequest, "grpc target required")
	}

	dialOpts, err := buildDialOptions(cfg)
	if err != nil {
		return nil, err
	}

	conn, err := grpc.NewClient(cfg.Target, dialOpts...)
	if err != nil {
		return nil, mcp.WrapRuntimeError(mcp.ErrCodeTransportFailure, "grpc dial failed", err)
	}

	ctx := context.Background()
	if startupTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, startupTimeout)
		defer cancel()
	}

	fds, err := loadDescriptors(ctx, conn, cfg)
	if err != nil {
		conn.Close() //nolint:errcheck
		return nil, err
	}

	exec := &GRPCExecutor{
		conn:    conn,
		service: cfg.Service,
		method:  cfg.Method,
		md:      buildMetadata(cfg.Metadata, creds),
	}

	if cfg.Method != "" {
		exec.inputDesc, exec.outputDesc, err = ResolveMethod(fds, cfg.Service, cfg.Method)
		if err != nil {
			conn.Close() //nolint:errcheck
			return nil, mcp.WrapRuntimeError(mcp.ErrCodeTransportFailure, "resolve grpc method failed", err)
		}
	} else {
		exec.serviceDesc, err = ResolveService(fds, cfg.Service)
		if err != nil {
			conn.Close() //nolint:errcheck
			return nil, mcp.WrapRuntimeError(mcp.ErrCodeTransportFailure, "resolve grpc service failed", err)
		}
	}

	return exec, nil
}

// Execute runs a unary gRPC invocation and returns the result.
//
// The method to invoke is determined by cfg.Method if set, or extracted from
// the invocation's tool name (inv.Params["name"]). The invocation's arguments
// are marshaled into a dynamic protobuf message and sent over the wire.
func (e *GRPCExecutor) Execute(ctx context.Context, inv *mcp.Invocation) (*mcp.ExecutionResult, error) {
	if e.conn == nil {
		return &mcp.ExecutionResult{
			Status:      mcp.ExecutionStatusFailure,
			Err:         mcp.NewRuntimeError(mcp.ErrCodeTransportFailure, "connection not established"),
			Correlation: inv.Correlation,
		}, nil
	}

	methodName, args := paramsFromInvocation(inv)
	inputDesc, outputDesc, err := e.resolveDescriptors(methodName)
	if err != nil {
		return &mcp.ExecutionResult{
			Status:      mcp.ExecutionStatusFailure,
			Err:         mcp.WrapRuntimeError(mcp.ErrCodeTransportFailure, "resolve method descriptors", err),
			Correlation: inv.Correlation,
		}, nil
	}

	reqMsg, err := buildRequest(inputDesc, args)
	if err != nil {
		return &mcp.ExecutionResult{
			Status:      mcp.ExecutionStatusFailure,
			Err:         mcp.WrapRuntimeError(mcp.ErrCodeInvalidRequest, "build grpc request", err),
			Correlation: inv.Correlation,
		}, nil
	}

	respMsg := dynamicpb.NewMessage(outputDesc)
	fullMethod := fmt.Sprintf("/%s/%s", e.service, methodName)

	callCtx := e.attachMetadata(ctx)
	if err := e.conn.Invoke(callCtx, fullMethod, reqMsg, respMsg); err != nil {
		if ctx.Err() != nil {
			return &mcp.ExecutionResult{
				Status:      mcp.ExecutionStatusTimeout,
				Err:         mcp.WrapRuntimeError(mcp.ErrCodeInvocationTimeout, "invocation timed out", err),
				Correlation: inv.Correlation,
			}, nil
		}
		return &mcp.ExecutionResult{
			Status:      mcp.ExecutionStatusFailure,
			Err:         mcp.WrapRuntimeError(mcp.ErrCodeTransportFailure, "grpc call failed", err),
			Correlation: inv.Correlation,
		}, nil
	}

	output, err := responseToOutput(respMsg)
	if err != nil {
		return &mcp.ExecutionResult{
			Status:      mcp.ExecutionStatusFailure,
			Err:         mcp.WrapRuntimeError(mcp.ErrCodeInternal, "marshal grpc response", err),
			Correlation: inv.Correlation,
		}, nil
	}

	return &mcp.ExecutionResult{
		Status:      mcp.ExecutionStatusSuccess,
		Output:      output,
		Correlation: inv.Correlation,
	}, nil
}

// ExecuteStream runs a server-streaming gRPC invocation and returns a
// StreamingResult. Each streamed response is delivered as a ProgressEvent;
// the final response becomes the ExecutionResult.
func (e *GRPCExecutor) ExecuteStream(ctx context.Context, inv *mcp.Invocation) (*mcp.StreamingResult, error) {
	progressCh := make(chan mcp.ProgressEvent)
	finalCh := make(chan *mcp.ExecutionResult, 1)

	go func() {
		defer close(finalCh)
		defer close(progressCh)

		if e.conn == nil {
			finalCh <- &mcp.ExecutionResult{
				Status:      mcp.ExecutionStatusFailure,
				Err:         mcp.NewRuntimeError(mcp.ErrCodeTransportFailure, "connection not established"),
				Correlation: inv.Correlation,
			}
			return
		}

		methodName, args := paramsFromInvocation(inv)
		inputDesc, outputDesc, err := e.resolveDescriptors(methodName)
		if err != nil {
			finalCh <- &mcp.ExecutionResult{
				Status:      mcp.ExecutionStatusFailure,
				Err:         mcp.WrapRuntimeError(mcp.ErrCodeTransportFailure, "resolve method descriptors", err),
				Correlation: inv.Correlation,
			}
			return
		}

		reqMsg, err := buildRequest(inputDesc, args)
		if err != nil {
			finalCh <- &mcp.ExecutionResult{
				Status:      mcp.ExecutionStatusFailure,
				Err:         mcp.WrapRuntimeError(mcp.ErrCodeInvalidRequest, "build grpc request", err),
				Correlation: inv.Correlation,
			}
			return
		}

		fullMethod := fmt.Sprintf("/%s/%s", e.service, methodName)
		callCtx := e.attachMetadata(ctx)

		streamDesc := &grpc.StreamDesc{ServerStreams: true}
		stream, err := e.conn.NewStream(callCtx, streamDesc, fullMethod)
		if err != nil {
			finalCh <- &mcp.ExecutionResult{
				Status:      mcp.ExecutionStatusFailure,
				Err:         mcp.WrapRuntimeError(mcp.ErrCodeTransportFailure, "open grpc stream", err),
				Correlation: inv.Correlation,
			}
			return
		}

		if err := stream.SendMsg(reqMsg); err != nil {
			finalCh <- &mcp.ExecutionResult{
				Status:      mcp.ExecutionStatusFailure,
				Err:         mcp.WrapRuntimeError(mcp.ErrCodeTransportFailure, "send grpc request", err),
				Correlation: inv.Correlation,
			}
			return
		}
		if err := stream.CloseSend(); err != nil {
			finalCh <- &mcp.ExecutionResult{
				Status:      mcp.ExecutionStatusFailure,
				Err:         mcp.WrapRuntimeError(mcp.ErrCodeTransportFailure, "close send", err),
				Correlation: inv.Correlation,
			}
			return
		}

		lastOutput := textContentOutput("")
		for {
			respMsg := dynamicpb.NewMessage(outputDesc)
			if err := stream.RecvMsg(respMsg); err != nil {
				if err == io.EOF {
					break
				}
				if ctx.Err() != nil {
					finalCh <- &mcp.ExecutionResult{
						Status:      mcp.ExecutionStatusTimeout,
						Err:         mcp.WrapRuntimeError(mcp.ErrCodeInvocationTimeout, "stream timed out", err),
						Correlation: inv.Correlation,
					}
					return
				}
				finalCh <- &mcp.ExecutionResult{
					Status:      mcp.ExecutionStatusFailure,
					Err:         mcp.WrapRuntimeError(mcp.ErrCodeTransportFailure, "stream recv failed", err),
					Correlation: inv.Correlation,
				}
				return
			}

			jsonBytes, err := protojson.Marshal(respMsg)
			if err != nil {
				finalCh <- &mcp.ExecutionResult{
					Status:      mcp.ExecutionStatusFailure,
					Err:         mcp.WrapRuntimeError(mcp.ErrCodeInternal, "marshal stream response", err),
					Correlation: inv.Correlation,
				}
				return
			}

			lastOutput = textContentOutput(string(jsonBytes))
			// Never block forever on the progress channel: the consumer may
			// stop reading (e.g. the session grain gave up and cancelled the
			// context). Abort the stream when the context is done.
			select {
			case progressCh <- mcp.ProgressEvent{Message: string(jsonBytes)}:
			case <-ctx.Done():
				finalCh <- &mcp.ExecutionResult{
					Status:      mcp.ExecutionStatusTimeout,
					Err:         mcp.WrapRuntimeError(mcp.ErrCodeInvocationTimeout, "stream cancelled", ctx.Err()),
					Correlation: inv.Correlation,
				}
				return
			}
		}

		finalCh <- &mcp.ExecutionResult{
			Status:      mcp.ExecutionStatusSuccess,
			Output:      lastOutput,
			Correlation: inv.Correlation,
		}
	}()

	return &mcp.StreamingResult{
		Progress: progressCh,
		Final:    finalCh,
	}, nil
}

// Close releases the gRPC connection. Close is safe to call multiple times;
// only the first call performs cleanup.
func (e *GRPCExecutor) Close() error {
	e.closed.Do(func() {
		if e.conn != nil {
			e.closeErr = e.conn.Close()
		}
	})
	return e.closeErr
}

// resolveDescriptors returns the input and output message descriptors for the
// given method name. When a specific method is configured on the executor, it
// returns the cached descriptors. Otherwise, it looks up the method dynamically
// from the service descriptor.
func (e *GRPCExecutor) resolveDescriptors(methodName string) (protoreflect.MessageDescriptor, protoreflect.MessageDescriptor, error) {
	if e.inputDesc != nil && e.outputDesc != nil {
		return e.inputDesc, e.outputDesc, nil
	}
	if e.serviceDesc == nil {
		return nil, nil, fmt.Errorf("no service descriptor available")
	}
	md := e.serviceDesc.Methods().ByName(protoreflect.Name(methodName))
	if md == nil {
		return nil, nil, fmt.Errorf("method %q not found in service %q", methodName, e.service)
	}
	return md.Input(), md.Output(), nil
}

// attachMetadata returns a context with the executor's static metadata attached
// as outgoing gRPC metadata.
func (e *GRPCExecutor) attachMetadata(ctx context.Context) context.Context {
	if len(e.md) > 0 {
		return metadata.NewOutgoingContext(ctx, e.md)
	}
	return ctx
}

// buildDialOptions constructs the gRPC dial options from the transport config.
func buildDialOptions(cfg *mcp.GRPCTransportConfig) ([]grpc.DialOption, error) {
	var opts []grpc.DialOption
	if cfg.TLS != nil {
		tlsCfg, err := security.BuildClientTLSConfig(cfg.TLS)
		if err != nil {
			return nil, mcp.WrapRuntimeError(mcp.ErrCodeTransportFailure, "build grpc TLS config", err)
		}
		if tlsCfg != nil {
			opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)))
		} else {
			opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
		}
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}
	return opts, nil
}

// loadDescriptors loads the protobuf file descriptor set from either a local
// file or via gRPC server reflection, depending on the transport config.
func loadDescriptors(ctx context.Context, conn *grpc.ClientConn, cfg *mcp.GRPCTransportConfig) (*descriptorpb.FileDescriptorSet, error) {
	if cfg.DescriptorSet != "" {
		fds, err := LoadDescriptorSet(cfg.DescriptorSet)
		if err != nil {
			return nil, mcp.WrapRuntimeError(mcp.ErrCodeTransportFailure, "load descriptor set", err)
		}
		return fds, nil
	}
	fds, err := FetchDescriptorSetViaReflection(ctx, conn, cfg.Service)
	if err != nil {
		return nil, mcp.WrapRuntimeError(mcp.ErrCodeTransportFailure, "fetch descriptors via reflection", err)
	}
	return fds, nil
}

// buildMetadata converts the static config metadata and the resolved
// credentials into gRPC metadata. Credentials are applied last so they win
// over config entries with the same key.
func buildMetadata(m, creds map[string]string) metadata.MD {
	if len(m) == 0 && len(creds) == 0 {
		return nil
	}
	md := make(metadata.MD, len(m)+len(creds))
	for k, v := range m {
		md.Set(k, v)
	}
	for k, v := range creds {
		md.Set(k, v)
	}
	return md
}

// paramsFromInvocation extracts the method name and arguments from an
// invocation. The method name falls back to the ToolID when not present
// in the params.
func paramsFromInvocation(inv *mcp.Invocation) (string, any) {
	if inv == nil || inv.Params == nil {
		return "", nil
	}
	name, _ := inv.Params["name"].(string)
	if name == "" {
		name = string(inv.ToolID)
	}
	return name, inv.Params["arguments"]
}

// buildRequest creates a dynamic protobuf message populated from the
// invocation arguments. Arguments are expected to be a map[string]any
// (from JSON unmarshaling) and are converted to protobuf via protojson.
func buildRequest(inputDesc protoreflect.MessageDescriptor, args any) (*dynamicpb.Message, error) {
	msg := dynamicpb.NewMessage(inputDesc)
	if args == nil {
		return msg, nil
	}
	jsonBytes, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("marshal arguments to JSON: %w", err)
	}
	if err := protojson.Unmarshal(jsonBytes, msg); err != nil {
		return nil, fmt.Errorf("unmarshal JSON to protobuf: %w", err)
	}
	return msg, nil
}

// responseToOutput converts a dynamic protobuf response message into the
// runtime output map format. The output shape matches what
// mcpconv.CallResultToOutput produces for HTTP/stdio executors:
//
//	{"content": [{"type": "text", "text": "<json>"}]}
func responseToOutput(msg *dynamicpb.Message) (map[string]any, error) {
	jsonBytes, err := protojson.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("marshal response to JSON: %w", err)
	}
	return textContentOutput(string(jsonBytes)), nil
}

// textContentOutput builds the standard MCP content output map with a single
// text content entry.
func textContentOutput(text string) map[string]any {
	return map[string]any{
		"content": []map[string]any{
			{
				"type": "text",
				"text": text,
			},
		},
	}
}
