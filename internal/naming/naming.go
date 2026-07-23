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

// Package naming provides deterministic actor name constants and construction
// functions for the portcullis runtime.
package naming

import (
	"strconv"
	"strings"

	"github.com/tochemey/portcullis/mcp"
)

// Actor name constants for well-known singleton actors.
const (
	ActorNameGatewayManager   = "gateway-manager"
	ActorNameRegistrar        = "registrar"
	ActorNameHealth           = "health"
	ActorNameJournal          = "journal"
	ActorNameJournalWriter    = "journal-writer"
	ActorNameCredentialBroker = "credential-broker" //nolint:gosec
	ActorNameRouter           = "router"
	ActorNamePolicy           = "policy"
)

// RouterName returns the deterministic actor name for the router at the given
// pool index: "router-0", "router-1", ...
func RouterName(index int) string {
	if index < 0 {
		index = 0
	}
	return ActorNameRouter + "-" + strconv.Itoa(index)
}

// ToolSupervisorName returns the deterministic actor name for the tool supervisor
// responsible for the given tool.
func ToolSupervisorName(toolID mcp.ToolID) string {
	return "supervisor-" + string(toolID)
}

// ToolIDFromSupervisorName derives the ToolID from an actor name produced by
// ToolSupervisorName. Returns an empty ToolID when the name does not carry the
// expected "supervisor-" prefix.
func ToolIDFromSupervisorName(name string) mcp.ToolID {
	return mcp.ToolID(strings.TrimPrefix(name, "supervisor-"))
}

// SessionName returns the deterministic actor name for the session owned by the
// given tenant, client, and tool combination.
func SessionName(tenantID mcp.TenantID, clientID mcp.ClientID, toolID mcp.ToolID) string {
	return "session-" + string(tenantID) + "-" + string(clientID) + "-" + string(toolID)
}
