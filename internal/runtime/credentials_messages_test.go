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

package runtime

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/tochemey/mcgate/mcp"
)

func TestResolveResult_Resolved(t *testing.T) {
	t.Run("returns true when credentials resolved successfully", func(t *testing.T) {
		r := &ResolveResult{
			Credentials: &mcp.Credentials{Values: map[string]string{"api-key": "secret"}},
			Err:         nil,
		}
		assert.True(t, r.Resolved())
	})

	t.Run("returns false when Err is non-nil", func(t *testing.T) {
		r := &ResolveResult{
			Credentials: &mcp.Credentials{Values: map[string]string{"api-key": "secret"}},
			Err:         errors.New("resolution failed"),
		}
		assert.False(t, r.Resolved())
	})

	t.Run("returns false when Credentials is nil", func(t *testing.T) {
		r := &ResolveResult{
			Credentials: nil,
			Err:         nil,
		}
		assert.False(t, r.Resolved())
	})

	t.Run("returns false when Credentials.Values is empty", func(t *testing.T) {
		r := &ResolveResult{
			Credentials: &mcp.Credentials{Values: map[string]string{}},
			Err:         nil,
		}
		assert.False(t, r.Resolved())
	})

	t.Run("returns false when Credentials.Values is nil", func(t *testing.T) {
		r := &ResolveResult{
			Credentials: &mcp.Credentials{Values: nil},
			Err:         nil,
		}
		assert.False(t, r.Resolved())
	})
}
