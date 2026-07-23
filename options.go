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

package portcullis

import (
	"os"

	goaktlog "github.com/tochemey/goakt/v4/log"
)

// Option configures a Gateway.
type Option func(*Gateway)

// WithLogger plugs in a custom logger. The gateway wraps it internally to
// satisfy the underlying engine's logging interface. Passing nil (or a
// typed-nil pointer such as (*MyLogger)(nil)) is a no-op: the logger is
// left unchanged, so a LogLevel set in mcp.Config is preserved.
//
// If the Logger also implements LeveledLogger, the adapter uses its level
// for engine-side log gating. Otherwise the adapter defaults to InfoLevel.
func WithLogger(logger Logger) Option {
	return func(g *Gateway) {
		if isNilLogger(logger) {
			return
		}
		g.logger = newLoggerAdapter(logger)
	}
}

// WithDebug enables verbose debug logging from the underlying engine to
// os.Stdout using the built-in structured logger. This is useful for
// diagnosing actor lifecycle, message routing, and cluster events.
func WithDebug() Option {
	return func(g *Gateway) {
		g.logger = goaktlog.NewSlog(goaktlog.DebugLevel, os.Stdout)
	}
}

// WithMetrics enables OpenTelemetry metrics for the gateway. When set, the
// gateway registers portcullis instruments (tool availability, invocation
// latency, failures, circuit state) at Start. Metrics are exported only when
// the OpenTelemetry SDK is initialized and a MeterProvider/exporter is
// configured before Start, similar to GoAkt's WithMetrics().
func WithMetrics() Option {
	return func(g *Gateway) {
		g.metrics = true
	}
}

// WithTracing enables OpenTelemetry tracing for the gateway. When set, the
// gateway registers a Tracer at Start and instruments egress HTTP calls with
// otelhttp for automatic W3C trace-context propagation and span creation.
// Traces are exported only when the OpenTelemetry SDK is initialized and a
// TracerProvider/exporter is configured before Start, similar to GoAkt's
// WithTracing().
func WithTracing() Option {
	return func(g *Gateway) {
		g.tracing = true
	}
}
