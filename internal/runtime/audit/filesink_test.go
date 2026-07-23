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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tochemey/portcullis/mcp"
)

func TestFileSink_WriteAndClose(t *testing.T) {
	dir := t.TempDir()
	sink, err := NewFileSink(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sink.Close() })

	ev := &mcp.AuditEvent{
		Type:     mcp.AuditEventTypeInvocationComplete,
		TenantID: "tenant-1",
		ToolID:   "tool-1",
		Outcome:  "success",
	}
	require.NoError(t, sink.Write(ev))
	require.NoError(t, sink.Close())

	data, err := os.ReadFile(filepath.Join(dir, "audit.log"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "tenant-1")
	assert.Contains(t, string(data), "tool-1")
	assert.Contains(t, string(data), "invocation_complete")
}

func TestFileSink_NewFileSink_CreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "audit")
	sink, err := NewFileSink(dir)
	require.NoError(t, err)
	require.NoError(t, sink.Close())
	assert.DirExists(t, dir)
}

func TestFileSink_EmptyDir_UsesCurrentDir(t *testing.T) {
	// NewFileSink("") defaults to "." — verify it creates audit.log in cwd.
	tmp := t.TempDir()
	orig, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(orig) })

	sink, err := NewFileSink("")
	require.NoError(t, err)
	require.NoError(t, sink.Close())
	assert.FileExists(t, filepath.Join(tmp, "audit.log"))
}

func TestFileSink_WriteNilEvent(t *testing.T) {
	dir := t.TempDir()
	sink, err := NewFileSink(dir)
	require.NoError(t, err)
	defer func() { _ = sink.Close() }()
	// nil event should be a no-op
	require.NoError(t, sink.Write(nil))
}

func TestFileSink_WriteAfterClose(t *testing.T) {
	dir := t.TempDir()
	sink, err := NewFileSink(dir)
	require.NoError(t, err)
	require.NoError(t, sink.Close())
	// Write after Close should be a no-op (f is nil)
	require.NoError(t, sink.Write(&mcp.AuditEvent{TenantID: "t1"}))
}

func TestFileSink_CloseIdempotent(t *testing.T) {
	dir := t.TempDir()
	sink, err := NewFileSink(dir)
	require.NoError(t, err)
	require.NoError(t, sink.Close())
	// Second close should be a no-op
	require.NoError(t, sink.Close())
}
