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

package extension

import (
	"bytes"
	"encoding/gob"
	"maps"

	goaktextension "github.com/tochemey/goakt/v4/extension"

	"github.com/tochemey/portcullis/mcp"
)

// SessionDependencyID is the fixed identifier for the session dependency.
const SessionDependencyID = "session"

// SessionDependency carries the serializable identity and configuration a
// session grain needs to build its own executor during OnActivate.
//
// The executor itself is NOT held here on purpose: under grain semantics,
// passing a pre-created executor via WithGrainDependencies is unsafe because
// goakt's grain engine always invokes the factory and dependency options on
// every GrainIdentity call, yet only runs OnActivate for the first
// activation of an identity. Any pre-created executor attached on repeat
// calls would be discarded — and leaked — because the already-active grain
// never sees it. Moving executor construction into the grain's OnActivate
// (via ExecutorFactoryExtension lookup) eliminates that leak entirely.
type SessionDependency struct {
	tenantID    mcp.TenantID
	clientID    mcp.ClientID
	toolID      mcp.ToolID
	tool        mcp.Tool
	credentials map[string]string
}

// sessionDependencyPayload is used for gob encoding.
type sessionDependencyPayload struct {
	TenantID    string
	ClientID    string
	ToolID      string
	Tool        mcp.Tool
	Credentials map[string]string
}

var _ goaktextension.Dependency = (*SessionDependency)(nil)

func init() {
	gob.Register(&mcp.StdioTransportConfig{})
	gob.Register(&mcp.HTTPTransportConfig{})
}

// NewSessionDependency creates a dependency for the given session identity,
// tool, and resolved credentials. The credentials map is defensively copied
// to preserve immutability across dependency reuse.
func NewSessionDependency(tenantID mcp.TenantID, clientID mcp.ClientID, toolID mcp.ToolID, tool mcp.Tool, credentials map[string]string) *SessionDependency {
	var credsCopy map[string]string
	if len(credentials) > 0 {
		credsCopy = make(map[string]string, len(credentials))
		maps.Copy(credsCopy, credentials)
	}
	return &SessionDependency{
		tenantID:    tenantID,
		clientID:    clientID,
		toolID:      toolID,
		tool:        tool,
		credentials: credsCopy,
	}
}

// ID returns the unique identifier for this dependency.
func (s *SessionDependency) ID() string {
	return SessionDependencyID
}

// MarshalBinary encodes the session dependency for transport or persistence.
func (s *SessionDependency) MarshalBinary() ([]byte, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	payload := sessionDependencyPayload{
		TenantID:    string(s.tenantID),
		ClientID:    string(s.clientID),
		ToolID:      string(s.toolID),
		Tool:        s.tool,
		Credentials: s.credentials,
	}
	if err := enc.Encode(&payload); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// UnmarshalBinary decodes the session dependency from binary form.
func (s *SessionDependency) UnmarshalBinary(data []byte) error {
	dec := gob.NewDecoder(bytes.NewReader(data))
	var payload sessionDependencyPayload
	if err := dec.Decode(&payload); err != nil {
		return err
	}
	s.tenantID = mcp.TenantID(payload.TenantID)
	s.clientID = mcp.ClientID(payload.ClientID)
	s.toolID = mcp.ToolID(payload.ToolID)
	s.tool = payload.Tool
	s.credentials = payload.Credentials
	return nil
}

// TenantID returns the tenant identifier.
func (s *SessionDependency) TenantID() mcp.TenantID { return s.tenantID }

// ClientID returns the client identifier.
func (s *SessionDependency) ClientID() mcp.ClientID { return s.clientID }

// ToolID returns the tool identifier.
func (s *SessionDependency) ToolID() mcp.ToolID { return s.toolID }

// Tool returns the tool definition (used by the grain to size idle timeouts
// and to hand to the executor factory during activation).
func (s *SessionDependency) Tool() mcp.Tool { return s.tool }

// Credentials returns the credentials used to create the executor.
// The grain uses these when invoking the ExecutorFactory in OnActivate.
func (s *SessionDependency) Credentials() map[string]string { return s.credentials }
