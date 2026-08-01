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
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tochemey/mcgate/internal/runtime/telemetry"
)

func TestWrapTransport(t *testing.T) {
	base := http.DefaultTransport

	t.Run("returns base when tracing disabled", func(t *testing.T) {
		telemetry.UnregisterTracer()
		got := wrapTransport(base)
		assert.Same(t, base, got)
	})

	t.Run("returns instrumented transport when tracing enabled", func(t *testing.T) {
		t.Cleanup(telemetry.UnregisterTracer)
		telemetry.RegisterTracer()
		got := wrapTransport(base)
		require.NotNil(t, got)
		assert.NotSame(t, base, got, "wrapTransport should return otel-instrumented transport when tracing is enabled")
	})
}
