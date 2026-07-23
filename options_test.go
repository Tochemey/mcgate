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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	goaktactor "github.com/tochemey/goakt/v4/actor"
	goaktlog "github.com/tochemey/goakt/v4/log"

	"github.com/tochemey/portcullis/mcp"
)

// withSystemForTesting injects a pre-built actor system for testing. When set,
// Start uses this system instead of creating one.
func withSystemForTesting(system goaktactor.ActorSystem) Option {
	return func(g *Gateway) {
		g.testSystem = system
	}
}

// noopLogger is a Logger implementation that discards all output. It is used
// in tests to exercise WithLogger with a concrete Logger value; goaktlog is
// still imported in this file for unrelated assertions.
type noopLogger struct{}

func (noopLogger) Debug(_ string, _ ...any) {}
func (noopLogger) Info(_ string, _ ...any)  {}
func (noopLogger) Warn(_ string, _ ...any)  {}
func (noopLogger) Error(_ string, _ ...any) {}

// noopPtrLogger is a pointer-receiver Logger used to test typed-nil detection.
type noopPtrLogger struct{}

func (*noopPtrLogger) Debug(_ string, _ ...any) {}
func (*noopPtrLogger) Info(_ string, _ ...any)  {}
func (*noopPtrLogger) Warn(_ string, _ ...any)  {}
func (*noopPtrLogger) Error(_ string, _ ...any) {}

// leveledNoopLogger implements both Logger and LeveledLogger.
type leveledNoopLogger struct {
	level string
}

func (*leveledNoopLogger) Debug(_ string, _ ...any) {}
func (*leveledNoopLogger) Info(_ string, _ ...any)  {}
func (*leveledNoopLogger) Warn(_ string, _ ...any)  {}
func (*leveledNoopLogger) Error(_ string, _ ...any) {}
func (l *leveledNoopLogger) Level() string          { return l.level }

func TestWithLogger(t *testing.T) {
	t.Run("nil logger is a no-op and leaves default DiscardLogger", func(t *testing.T) {
		gw, err := New(mcp.Config{}, WithLogger(nil))
		require.NoError(t, err)
		require.NotNil(t, gw)
		assert.Equal(t, goaktlog.DiscardLogger, gw.logger)
	})

	t.Run("typed-nil logger is a no-op and leaves default DiscardLogger", func(t *testing.T) {
		var nl *noopPtrLogger // typed-nil pointer
		gw, err := New(mcp.Config{}, WithLogger(nl))
		require.NoError(t, err)
		require.NotNil(t, gw)
		assert.Equal(t, goaktlog.DiscardLogger, gw.logger)
	})

	t.Run("nil logger does not override a config-level logger", func(t *testing.T) {
		cfg := mcp.Config{}
		cfg.LogLevel = "info"
		gw, err := New(cfg, WithLogger(nil))
		require.NoError(t, err)
		require.NotNil(t, gw)
		assert.NotEqual(t, goaktlog.DiscardLogger, gw.logger, "config-level logger must be preserved when nil is passed")
	})

	t.Run("typed-nil logger does not override a config-level logger", func(t *testing.T) {
		cfg := mcp.Config{}
		cfg.LogLevel = "info"
		var nl *noopPtrLogger
		gw, err := New(cfg, WithLogger(nl))
		require.NoError(t, err)
		require.NotNil(t, gw)
		assert.NotEqual(t, goaktlog.DiscardLogger, gw.logger, "config-level logger must be preserved when typed-nil is passed")
	})

	t.Run("custom logger wraps in adapter", func(t *testing.T) {
		gw, err := New(mcp.Config{}, WithLogger(noopLogger{}))
		require.NoError(t, err)
		require.NotNil(t, gw)
		assert.NotNil(t, gw.logger)
		assert.NotEqual(t, goaktlog.DiscardLogger, gw.logger)
		adapter, ok := gw.logger.(*loggerAdapter)
		assert.True(t, ok)
		assert.Equal(t, goaktlog.InfoLevel, adapter.LogLevel(), "adapter defaults to InfoLevel")
	})

	t.Run("LeveledLogger sets adapter level", func(t *testing.T) {
		gw, err := New(mcp.Config{}, WithLogger(&leveledNoopLogger{level: "error"}))
		require.NoError(t, err)
		require.NotNil(t, gw)
		adapter, ok := gw.logger.(*loggerAdapter)
		require.True(t, ok)
		assert.Equal(t, goaktlog.ErrorLevel, adapter.LogLevel())
	})
}

func TestWithDebug(t *testing.T) {
	gw, err := New(mcp.Config{}, WithDebug())
	require.NoError(t, err)
	require.NotNil(t, gw)
	assert.NotNil(t, gw.logger)
	assert.NotEqual(t, goaktlog.DiscardLogger, gw.logger)
}

func TestWithMetrics(t *testing.T) {
	gw, err := New(mcp.Config{}, WithMetrics())
	require.NoError(t, err)
	require.NotNil(t, gw)
	assert.True(t, gw.metrics)
}

func TestWithTracing(t *testing.T) {
	gw, err := New(mcp.Config{}, WithTracing())
	require.NoError(t, err)
	require.NotNil(t, gw)
	assert.True(t, gw.tracing)
}

func TestOptions_Combined(t *testing.T) {
	gw, err := New(mcp.Config{}, WithLogger(nil), WithMetrics(), WithTracing())
	require.NoError(t, err)
	require.NotNil(t, gw)
	assert.Equal(t, goaktlog.DiscardLogger, gw.logger)
	assert.True(t, gw.metrics)
	assert.True(t, gw.tracing)
}
