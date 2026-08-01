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

package naming_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/tochemey/mcgate/internal/naming"
	"github.com/tochemey/mcgate/mcp"
)

func TestActorNameConstants(t *testing.T) {
	assert.Equal(t, "gateway-manager", naming.ActorNameGatewayManager)
	assert.Equal(t, "registrar", naming.ActorNameRegistrar)
	assert.Equal(t, "health", naming.ActorNameHealth)
	assert.Equal(t, "journal", naming.ActorNameJournal)
	assert.Equal(t, "credential-broker", naming.ActorNameCredentialBroker)
	assert.Equal(t, "router", naming.ActorNameRouter)
	assert.Equal(t, "policy", naming.ActorNamePolicy)
}

func TestToolSupervisorName(t *testing.T) {
	assert.Equal(t, "supervisor-my-tool", naming.ToolSupervisorName("my-tool"))
}

func TestSessionName(t *testing.T) {
	assert.Equal(t, "session-t1-c1-tool-a", naming.SessionName("t1", "c1", "tool-a"))
}

func TestToolIDFromSupervisorName(t *testing.T) {
	assert.Equal(t, mcp.ToolID("my-tool"), naming.ToolIDFromSupervisorName("supervisor-my-tool"))
	assert.Equal(t, mcp.ToolID(""), naming.ToolIDFromSupervisorName("supervisor-"))
	assert.Equal(t, mcp.ToolID("other"), naming.ToolIDFromSupervisorName("other"))
}
