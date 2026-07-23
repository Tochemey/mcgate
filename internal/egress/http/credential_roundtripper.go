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

package http

import "net/http"

// credentialRoundTripper injects resolved credentials as HTTP request headers
// on every outbound request. Each credential key is used as the header name
// and its value as the header value.
type credentialRoundTripper struct {
	base    http.RoundTripper
	headers map[string]string
}

// RoundTrip implements http.RoundTripper. The request is cloned before the
// headers are set, per the RoundTripper contract that forbids mutating the
// caller's request.
func (c *credentialRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	for k, v := range c.headers {
		clone.Header.Set(k, v)
	}
	return c.base.RoundTrip(clone)
}

// withCredentialHeaders wraps base with a credential-injecting round tripper.
// Returns base unchanged when there are no credentials.
func withCredentialHeaders(base http.RoundTripper, creds map[string]string) http.RoundTripper {
	if len(creds) == 0 {
		return base
	}
	return &credentialRoundTripper{base: base, headers: creds}
}
