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

package stdio

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tochemey/portcullis/mcp"
)

var (
	envServerOnce sync.Once
	envServerPath string
	envServerErr  error
)

// buildEnvServer compiles the testdata envserver MCP stdio server once per
// test run and returns the binary path. The server's list endpoints return one
// item per page, and its "env" tool echoes environment variable values.
func buildEnvServer(t *testing.T) string {
	t.Helper()
	envServerOnce.Do(func() {
		dir, err := os.MkdirTemp("", "portcullis-envserver")
		if err != nil {
			envServerErr = err
			return
		}
		bin := filepath.Join(dir, "envserver")
		cmd := exec.Command("go", "build", "-o", bin, "./testdata/envserver")
		out, err := cmd.CombinedOutput()
		if err != nil {
			envServerErr = err
			envServerPath = string(out)
			return
		}
		envServerPath = bin
	})
	if envServerErr != nil {
		t.Fatalf("cannot build envserver test binary: %v: %s", envServerErr, envServerPath)
	}
	return envServerPath
}

func TestFetchSchemas_Paginated(t *testing.T) {
	bin := buildEnvServer(t)

	cfg := &mcp.StdioTransportConfig{Command: bin}
	schemas, err := FetchSchemas(context.Background(), cfg, 15*time.Second)
	require.NoError(t, err)

	require.Len(t, schemas, 3, "all pages must be collected")
	names := make([]string, 0, len(schemas))
	for _, s := range schemas {
		names = append(names, s.Name)
	}
	assert.ElementsMatch(t, []string{"env", "alpha", "bravo"}, names)
}

func TestFetchResources_Paginated(t *testing.T) {
	bin := buildEnvServer(t)

	cfg := &mcp.StdioTransportConfig{Command: bin}
	resources, templates, err := FetchResources(context.Background(), cfg, 15*time.Second)
	require.NoError(t, err)

	require.Len(t, resources, 2, "all resource pages must be collected")
	uris := []string{resources[0].URI, resources[1].URI}
	assert.ElementsMatch(t, []string{"file:///one.txt", "file:///two.txt"}, uris)

	require.Len(t, templates, 2, "all template pages must be collected")
	tmpls := []string{templates[0].URITemplate, templates[1].URITemplate}
	assert.ElementsMatch(t, []string{"file:///{a}", "dir:///{b}"}, tmpls)
}
