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

// Command envserver is a minimal MCP stdio server used by the stdio egress
// tests. Its "env" tool echoes the value of an environment variable, letting
// tests assert that resolved credentials reach the child process environment.
// List endpoints return one item per page (PageSize 1) so pagination handling
// can be exercised end to end.
package main

import (
	"context"
	"log"
	"os"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	stub := func(ctx context.Context, req *sdkmcp.CallToolRequest, args map[string]any) (*sdkmcp.CallToolResult, any, error) {
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "ok"}},
		}, nil, nil
	}
	envTool := func(ctx context.Context, req *sdkmcp.CallToolRequest, args map[string]any) (*sdkmcp.CallToolResult, any, error) {
		name, _ := args["name"].(string)
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: os.Getenv(name)}},
		}, nil, nil
	}
	readResource := func(ctx context.Context, req *sdkmcp.ReadResourceRequest) (*sdkmcp.ReadResourceResult, error) {
		return &sdkmcp.ReadResourceResult{
			Contents: []*sdkmcp.ResourceContents{{URI: req.Params.URI, Text: "ok"}},
		}, nil
	}

	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "envserver", Version: "v0.1.0"}, &sdkmcp.ServerOptions{PageSize: 1})
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "env", Description: "returns the value of the named environment variable"}, envTool)
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "alpha", Description: "alpha"}, stub)
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "bravo", Description: "bravo"}, stub)
	server.AddResource(&sdkmcp.Resource{URI: "file:///one.txt", Name: "one"}, readResource)
	server.AddResource(&sdkmcp.Resource{URI: "file:///two.txt", Name: "two"}, readResource)
	server.AddResourceTemplate(&sdkmcp.ResourceTemplate{URITemplate: "file:///{a}", Name: "a"}, readResource)
	server.AddResourceTemplate(&sdkmcp.ResourceTemplate{URITemplate: "dir:///{b}", Name: "b"}, readResource)

	if err := server.Run(context.Background(), &sdkmcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
