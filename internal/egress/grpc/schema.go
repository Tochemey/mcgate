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

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/tochemey/mcgate/mcp"
)

// FetchSchemas connects to the gRPC backend to verify reachability, then
// derives tool schemas from the proto descriptors. This mirrors the live-fetch
// pattern used by HTTP (tools/list over HTTP) and stdio (tools/list over
// stdin/stdout) schema fetchers.
//
// For descriptor set mode, the schemas are derived from the local .binpb file.
// For reflection mode, the schemas are derived from descriptors fetched via
// gRPC server reflection on the live connection.
//
// When cfg.Method is set, only that single method is returned as a schema.
// When cfg.Method is empty, all methods in the service are returned.
func FetchSchemas(ctx context.Context, cfg *mcp.GRPCTransportConfig, startupTimeout time.Duration) ([]mcp.ToolSchema, error) {
	if cfg == nil {
		return nil, mcp.NewRuntimeError(mcp.ErrCodeInvalidRequest, "grpc config required")
	}

	dialOpts, err := buildDialOptions(cfg)
	if err != nil {
		return nil, err
	}

	fetchCtx := ctx
	if startupTimeout > 0 {
		var cancel context.CancelFunc
		fetchCtx, cancel = context.WithTimeout(fetchCtx, startupTimeout)
		defer cancel()
	}

	conn, err := grpc.NewClient(cfg.Target, dialOpts...)
	if err != nil {
		return nil, mcp.WrapRuntimeError(mcp.ErrCodeTransportFailure, "grpc schema dial failed", err)
	}
	defer conn.Close() //nolint:errcheck

	// Trigger connectivity check. The connection is lazy by default; force a
	// state transition and wait until it reaches READY or fails.
	conn.Connect()
	for {
		state := conn.GetState()
		if state == connectivity.Ready {
			break
		}
		if state == connectivity.TransientFailure || state == connectivity.Shutdown {
			return nil, mcp.NewRuntimeError(mcp.ErrCodeTransportFailure, fmt.Sprintf("grpc backend unreachable: state=%s", state))
		}
		if !conn.WaitForStateChange(fetchCtx, state) {
			return nil, mcp.WrapRuntimeError(mcp.ErrCodeTransportFailure, "grpc backend unreachable (timeout)", fetchCtx.Err())
		}
	}

	var fds *descriptorpb.FileDescriptorSet
	if cfg.Reflection {
		fds, err = FetchDescriptorSetViaReflection(fetchCtx, conn, cfg.Service)
		if err != nil {
			return nil, mcp.WrapRuntimeError(mcp.ErrCodeTransportFailure, "fetch descriptors via reflection", err)
		}
	} else {
		fds, err = LoadDescriptorSet(cfg.DescriptorSet)
		if err != nil {
			return nil, mcp.WrapRuntimeError(mcp.ErrCodeTransportFailure, "load descriptor set", err)
		}
	}

	sd, err := ResolveService(fds, cfg.Service)
	if err != nil {
		return nil, mcp.WrapRuntimeError(mcp.ErrCodeTransportFailure, "resolve service for schema", err)
	}

	if cfg.Method != "" {
		md := sd.Methods().ByName(protoreflect.Name(cfg.Method))
		if md == nil {
			return nil, mcp.NewRuntimeError(mcp.ErrCodeTransportFailure, fmt.Sprintf("method %q not found in service %q", cfg.Method, cfg.Service))
		}
		schema, err := methodToSchema(md)
		if err != nil {
			return nil, err
		}
		return []mcp.ToolSchema{schema}, nil
	}

	methods := sd.Methods()
	schemas := make([]mcp.ToolSchema, 0, methods.Len())
	for i := range methods.Len() {
		md := methods.Get(i)
		schema, err := methodToSchema(md)
		if err != nil {
			return nil, err
		}
		schemas = append(schemas, schema)
	}
	return schemas, nil
}

// methodToSchema converts a protobuf method descriptor into an MCP ToolSchema.
// The input message's fields are converted to a JSON Schema object.
func methodToSchema(md protoreflect.MethodDescriptor) (mcp.ToolSchema, error) {
	inputSchema, err := messageToJSONSchema(md.Input())
	if err != nil {
		return mcp.ToolSchema{}, fmt.Errorf("generate schema for method %q: %w", md.Name(), err)
	}

	raw, err := json.Marshal(inputSchema)
	if err != nil {
		return mcp.ToolSchema{}, fmt.Errorf("marshal schema for method %q: %w", md.Name(), err)
	}

	return mcp.ToolSchema{
		Name:        string(md.Name()),
		Description: extractDescription(md),
		InputSchema: json.RawMessage(raw),
	}, nil
}

// messageToJSONSchema converts a protobuf message descriptor to a JSON Schema
// representation. Fields are mapped to JSON Schema types based on their proto
// field kind. Self-referencing messages are handled via cycle detection.
func messageToJSONSchema(msgDesc protoreflect.MessageDescriptor) (map[string]any, error) {
	visited := make(map[protoreflect.FullName]bool)
	return messageToJSONSchemaWithVisited(msgDesc, visited), nil
}

// messageToJSONSchemaWithVisited converts a protobuf message descriptor to a
// JSON Schema, tracking visited messages to prevent infinite recursion on
// self-referencing types.
func messageToJSONSchemaWithVisited(msgDesc protoreflect.MessageDescriptor, visited map[protoreflect.FullName]bool) map[string]any {
	if visited[msgDesc.FullName()] {
		return map[string]any{"type": "object"}
	}
	visited[msgDesc.FullName()] = true
	defer delete(visited, msgDesc.FullName())

	properties := make(map[string]any, msgDesc.Fields().Len())
	fields := msgDesc.Fields()

	for i := range fields.Len() {
		fd := fields.Get(i)
		prop := fieldToSchemaProperty(fd, visited)
		properties[string(fd.JSONName())] = prop
	}

	return map[string]any{
		"type":       "object",
		"properties": properties,
	}
}

// fieldToSchemaProperty converts a single protobuf field descriptor into a
// JSON Schema property definition.
func fieldToSchemaProperty(fd protoreflect.FieldDescriptor, visited map[protoreflect.FullName]bool) map[string]any {
	if fd.IsList() {
		items := scalarOrMessageSchema(fd, visited)
		return map[string]any{
			"type":  "array",
			"items": items,
		}
	}
	if fd.IsMap() {
		valDesc := fd.MapValue()
		return map[string]any{
			"type":                 "object",
			"additionalProperties": scalarOrMessageSchema(valDesc, visited),
		}
	}
	return scalarOrMessageSchema(fd, visited)
}

// scalarOrMessageSchema returns the JSON Schema for a field, handling both
// scalar types and nested messages. The visited set prevents infinite recursion
// on self-referencing message types.
func scalarOrMessageSchema(fd protoreflect.FieldDescriptor, visited map[protoreflect.FullName]bool) map[string]any {
	if fd.Kind() == protoreflect.MessageKind || fd.Kind() == protoreflect.GroupKind {
		return messageToJSONSchemaWithVisited(fd.Message(), visited)
	}
	return map[string]any{"type": protoKindToJSONType(fd.Kind())}
}

// protoKindToJSONType maps protobuf field kinds to JSON Schema type strings.
func protoKindToJSONType(kind protoreflect.Kind) string {
	switch kind {
	case protoreflect.BoolKind:
		return "boolean"
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Uint32Kind,
		protoreflect.Fixed32Kind, protoreflect.Sfixed32Kind:
		return "integer"
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Uint64Kind,
		protoreflect.Fixed64Kind, protoreflect.Sfixed64Kind:
		return "string" // JSON cannot represent 64-bit integers precisely
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return "number"
	case protoreflect.StringKind:
		return "string"
	case protoreflect.BytesKind:
		return "string" // base64-encoded
	case protoreflect.EnumKind:
		return "string" // enum values are represented as strings in JSON
	default:
		return "string"
	}
}

// extractDescription returns the leading comments from a method descriptor's
// source location, if available. Returns an empty string when no comments
// are present.
func extractDescription(md protoreflect.MethodDescriptor) string {
	sl := md.ParentFile().SourceLocations().ByDescriptor(md)
	if sl.LeadingComments != "" {
		return sl.LeadingComments
	}
	return ""
}
