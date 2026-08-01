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

// ExecutorFactoryExtensionID is the fixed identifier for the ExecutorFactory
// extension registered on the actor system.
const ExecutorFactoryExtensionID = "executor-factory"

// ExecutorFactoryExtension wraps an ExecutorFactory for registration as an
// actor system extension. ToolSupervisor resolves this extension to create
// executors when spawning sessions.
type ExecutorFactoryExtension struct {
	factory mcp.ExecutorFactory
}

var _ goaktextension.Extension = (*ExecutorFactoryExtension)(nil)

// NewExecutorFactoryExtension creates an extension wrapping the given factory.
func NewExecutorFactoryExtension(factory mcp.ExecutorFactory) *ExecutorFactoryExtension {
	return &ExecutorFactoryExtension{factory: factory}
}

// ID returns the unique identifier for this extension.
func (e *ExecutorFactoryExtension) ID() string {
	return ExecutorFactoryExtensionID
}

// Factory returns the wrapped ExecutorFactory.
func (e *ExecutorFactoryExtension) Factory() mcp.ExecutorFactory {
	return e.factory
}
