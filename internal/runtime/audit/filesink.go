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

package audit

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/tochemey/portcullis/mcp"
)

const fileSinkBufSize = 4096

// FileSink writes audit events to a file as newline-delimited JSON (NDJSON).
// Uses a buffered writer to reduce syscalls and a json.Encoder that reuses
// its internal buffer across writes, reducing per-event allocations.
// Safe for concurrent use.
type FileSink struct {
	mu  sync.Mutex
	bw  *bufio.Writer
	enc *json.Encoder
	f   *os.File
}

// NewFileSink creates a FileSink that appends to dir/audit.log.
// Creates the directory if it does not exist. Returns an error if the file
// cannot be opened.
func NewFileSink(dir string) (*FileSink, error) {
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "audit.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return nil, err
	}
	bw := bufio.NewWriterSize(f, fileSinkBufSize)
	return &FileSink{f: f, bw: bw, enc: json.NewEncoder(bw)}, nil
}

// Write appends the event as a JSON line. The encoder writes JSON followed by
// a newline into the buffered writer, which flushes to the file when full or
// on Close.
func (x *FileSink) Write(event *mcp.AuditEvent) error {
	if event == nil {
		return nil
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.f == nil {
		return nil
	}
	return x.enc.Encode(event)
}

// Close flushes the buffer and closes the underlying file.
func (x *FileSink) Close() error {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.f == nil {
		return nil
	}
	flushErr := x.bw.Flush()
	closeErr := x.f.Close()
	x.f = nil
	x.bw = nil
	x.enc = nil
	if flushErr != nil {
		return flushErr
	}
	return closeErr
}
