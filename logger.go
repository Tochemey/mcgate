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

package mcgate

import (
	"context"
	"fmt"
	"io"
	golog "log"
	"os"
	"reflect"
	"strings"

	goaktlog "github.com/tochemey/goakt/v4/log"
)

// Logger is the logging interface that developers can implement to plug in
// their own logging backend (e.g., zap, zerolog, slog, logrus).
//
// Methods follow the slog convention: msg is the log message and args are
// optional key-value pairs (alternating string keys and arbitrary values)
// for structured logging. Implementations that do not support structured
// fields may ignore args.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// LeveledLogger is an optional interface that a Logger can implement to
// advertise its minimum log level. When the inner Logger implements this
// interface, the adapter uses its level for engine-side log gating (via
// LogLevel and Enabled), avoiding unnecessary log formatting for messages
// below the configured threshold.
//
// Level returns a log level string: "debug", "info", "warn"/"warning",
// "error", "fatal", or "panic". Unrecognized values are treated as "info".
type LeveledLogger interface {
	Level() string
}

// loggerAdapter wraps a Logger and satisfies the goaktlog.Logger interface
// used internally by the underlying engine.
type loggerAdapter struct {
	inner  Logger
	level  goaktlog.Level
	fields []any // accumulated key-value pairs from With()
}

// loggerWriter adapts Logger.Info to io.Writer for use with *log.Logger.
type loggerWriter struct {
	inner Logger
}

// compile-time check
var _ goaktlog.Logger = (*loggerAdapter)(nil)

// newLoggerAdapter creates a loggerAdapter wrapping the given Logger. If the
// Logger also implements LeveledLogger, the adapter uses its level for
// engine-side gating; otherwise it defaults to InfoLevel.
func newLoggerAdapter(inner Logger) *loggerAdapter {
	level := goaktlog.InfoLevel
	if ll, ok := inner.(LeveledLogger); ok {
		level = parseLevel(ll.Level())
	}
	return &loggerAdapter{inner: inner, level: level}
}

// isNilLogger returns true when l is nil or a typed-nil (e.g. (*MyLogger)(nil)).
// A typed-nil interface value is non-nil at the interface level but wraps a nil
// pointer, which would cause a nil-dereference panic on the first log call.
func isNilLogger(l Logger) bool {
	if l == nil {
		return true
	}
	v := reflect.ValueOf(l)
	return v.Kind() == reflect.Ptr && v.IsNil()
}

// levelSeverity maps a goaktlog.Level to an integer severity for comparison.
// Lower values are more verbose. GoAkt's level enum has non-standard ordering
// (Debug=5 in the iota) so direct numeric comparison does not reflect severity.
func levelSeverity(l goaktlog.Level) int {
	switch l {
	case goaktlog.DebugLevel:
		return 0
	case goaktlog.InfoLevel:
		return 1
	case goaktlog.WarningLevel:
		return 2
	case goaktlog.ErrorLevel:
		return 3
	case goaktlog.FatalLevel:
		return 4
	case goaktlog.PanicLevel:
		return 5
	default:
		return 1
	}
}

// parseLevel converts a human-readable level string to a goaktlog.Level.
// Unrecognized values default to InfoLevel.
func parseLevel(s string) goaktlog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return goaktlog.DebugLevel
	case "info":
		return goaktlog.InfoLevel
	case "warn", "warning":
		return goaktlog.WarningLevel
	case "error":
		return goaktlog.ErrorLevel
	case "fatal":
		return goaktlog.FatalLevel
	case "panic":
		return goaktlog.PanicLevel
	default:
		return goaktlog.InfoLevel
	}
}

// goaktArgsToMsg splits GoAkt variadic log arguments into a message string
// and optional key-value fields. GoAkt follows the slog convention where the
// first argument is the log message and subsequent arguments are alternating
// key-value pairs for structured logging.
func goaktArgsToMsg(args []any) (string, []any) {
	if len(args) == 0 {
		return "", nil
	}
	if len(args) == 1 {
		return fmt.Sprint(args[0]), nil
	}
	return fmt.Sprint(args[0]), args[1:]
}

// mergeFields returns the adapter's accumulated fields combined with additional
// fields. If the adapter has no accumulated fields, extra is returned as-is.
func (a *loggerAdapter) mergeFields(extra []any) []any {
	if len(a.fields) == 0 {
		return extra
	}
	if len(extra) == 0 {
		return a.fields
	}
	merged := make([]any, 0, len(a.fields)+len(extra))
	merged = append(merged, a.fields...)
	merged = append(merged, extra...)
	return merged
}

func (a *loggerAdapter) Debug(args ...any) {
	msg, fields := goaktArgsToMsg(args)
	a.inner.Debug(msg, a.mergeFields(fields)...)
}
func (a *loggerAdapter) Debugf(format string, args ...any) {
	a.inner.Debug(fmt.Sprintf(format, args...), a.fields...)
}
func (a *loggerAdapter) DebugContext(_ context.Context, args ...any) {
	msg, fields := goaktArgsToMsg(args)
	a.inner.Debug(msg, a.mergeFields(fields)...)
}
func (a *loggerAdapter) DebugfContext(_ context.Context, format string, args ...any) {
	a.inner.Debug(fmt.Sprintf(format, args...), a.fields...)
}
func (a *loggerAdapter) Info(args ...any) {
	msg, fields := goaktArgsToMsg(args)
	a.inner.Info(msg, a.mergeFields(fields)...)
}
func (a *loggerAdapter) Infof(format string, args ...any) {
	a.inner.Info(fmt.Sprintf(format, args...), a.fields...)
}
func (a *loggerAdapter) InfoContext(_ context.Context, args ...any) {
	msg, fields := goaktArgsToMsg(args)
	a.inner.Info(msg, a.mergeFields(fields)...)
}
func (a *loggerAdapter) InfofContext(_ context.Context, format string, args ...any) {
	a.inner.Info(fmt.Sprintf(format, args...), a.fields...)
}
func (a *loggerAdapter) Warn(args ...any) {
	msg, fields := goaktArgsToMsg(args)
	a.inner.Warn(msg, a.mergeFields(fields)...)
}
func (a *loggerAdapter) Warnf(format string, args ...any) {
	a.inner.Warn(fmt.Sprintf(format, args...), a.fields...)
}
func (a *loggerAdapter) WarnContext(_ context.Context, args ...any) {
	msg, fields := goaktArgsToMsg(args)
	a.inner.Warn(msg, a.mergeFields(fields)...)
}
func (a *loggerAdapter) WarnfContext(_ context.Context, format string, args ...any) {
	a.inner.Warn(fmt.Sprintf(format, args...), a.fields...)
}
func (a *loggerAdapter) Error(args ...any) {
	msg, fields := goaktArgsToMsg(args)
	a.inner.Error(msg, a.mergeFields(fields)...)
}
func (a *loggerAdapter) Errorf(format string, args ...any) {
	a.inner.Error(fmt.Sprintf(format, args...), a.fields...)
}
func (a *loggerAdapter) ErrorContext(_ context.Context, args ...any) {
	msg, fields := goaktArgsToMsg(args)
	a.inner.Error(msg, a.mergeFields(fields)...)
}
func (a *loggerAdapter) ErrorfContext(_ context.Context, format string, args ...any) {
	a.inner.Error(fmt.Sprintf(format, args...), a.fields...)
}

func (a *loggerAdapter) Fatal(args ...any) {
	msg, fields := goaktArgsToMsg(args)
	a.inner.Error(msg, a.mergeFields(fields)...)
	os.Exit(1)
}

func (a *loggerAdapter) Fatalf(format string, args ...any) {
	a.inner.Error(fmt.Sprintf(format, args...), a.fields...)
	os.Exit(1)
}

func (a *loggerAdapter) Panic(args ...any) {
	msg, fields := goaktArgsToMsg(args)
	a.inner.Error(msg, a.mergeFields(fields)...)
	panic(msg)
}

func (a *loggerAdapter) Panicf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	a.inner.Error(msg, a.fields...)
	panic(msg)
}

func (a *loggerAdapter) LogLevel() goaktlog.Level { return a.level }
func (a *loggerAdapter) Enabled(l goaktlog.Level) bool {
	return levelSeverity(l) >= levelSeverity(a.level)
}
func (a *loggerAdapter) With(keyValues ...any) goaktlog.Logger {
	if len(keyValues) == 0 {
		return a
	}
	merged := make([]any, 0, len(a.fields)+len(keyValues))
	merged = append(merged, a.fields...)
	merged = append(merged, keyValues...)
	return &loggerAdapter{inner: a.inner, level: a.level, fields: merged}
}
func (a *loggerAdapter) LogOutput() []io.Writer { return nil }
func (a *loggerAdapter) Flush() error           { return nil }

func (a *loggerAdapter) StdLogger() *golog.Logger {
	return golog.New(&loggerWriter{inner: a.inner}, "", 0)
}

func (w *loggerWriter) Write(p []byte) (int, error) {
	w.inner.Info(strings.TrimRight(string(p), "\r\n"))
	return len(p), nil
}
