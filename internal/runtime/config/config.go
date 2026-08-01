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

// Package config provides configuration helpers for the mcgate gateway:
// zero-value defaulting (ApplyDefaults) and logger construction. Public
// configuration types live in the mcp/ package.
package config

import (
	"os"

	goaktlog "github.com/tochemey/goakt/v4/log"
)

// NewLogger creates a goaktlog.Logger based on the configured LogLevel string.
// Uses GoAkt's slog implementation. When the level is empty or invalid,
// the GoAkt DefaultLogger is returned (InfoLevel to stdout).
func NewLogger(level string) goaktlog.Logger {
	parsed := ParseLogLevel(level)
	if parsed == goaktlog.InvalidLevel {
		return goaktlog.DiscardLogger
	}
	return goaktlog.NewSlog(parsed, os.Stdout)
}

// ParseLogLevel converts a log level string to a goaktlog.Level.
func ParseLogLevel(s string) goaktlog.Level {
	switch s {
	case "debug":
		return goaktlog.DebugLevel
	case "info":
		return goaktlog.InfoLevel
	case "warning", "warn":
		return goaktlog.WarningLevel
	case "error":
		return goaktlog.ErrorLevel
	case "fatal":
		return goaktlog.FatalLevel
	case "panic":
		return goaktlog.PanicLevel
	case "":
		return goaktlog.InvalidLevel
	default:
		return goaktlog.InvalidLevel
	}
}
