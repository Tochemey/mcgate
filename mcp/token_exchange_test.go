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

package mcp_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tochemey/mcgate/mcp"
)

const (
	testIDPBaseURL        = "https://idp.example.com"
	testTokenEndpointPath = "/oauth/token"        //nolint:gosec // test endpoint path, not a credential
	testIssuedAccessToken = "issued-id-jag-token" //nolint:gosec // fake test token value

	testIssuedTokenType     = oauthex.TokenTypeIDJAG
	testIssuedTokenTypeResp = "N_A"

	testMaxFormBodyBytes = 1 << 20
)

func TestNewOAuthTokenExchanger_RejectsMissingConfig(t *testing.T) {
	t.Run("nil config returns ErrTokenExchangerConfigRequired", func(t *testing.T) {
		exchanger, err := mcp.NewOAuthTokenExchanger(nil)

		require.Error(t, err)
		assert.True(t, errors.Is(err, mcp.ErrTokenExchangerConfigRequired))
		assert.Nil(t, exchanger)
	})

	t.Run("empty token endpoint returns ErrTokenExchangerConfigRequired", func(t *testing.T) {
		exchanger, err := mcp.NewOAuthTokenExchanger(&mcp.OAuthTokenExchangerConfig{
			ClientCredentials: basicClientCredentials(),
		})

		require.Error(t, err)
		assert.True(t, errors.Is(err, mcp.ErrTokenExchangerConfigRequired))
		assert.Nil(t, exchanger)
	})

	t.Run("nil client credentials returns ErrTokenExchangerConfigRequired", func(t *testing.T) {
		exchanger, err := mcp.NewOAuthTokenExchanger(&mcp.OAuthTokenExchangerConfig{
			TokenEndpoint: testIDPBaseURL + testTokenEndpointPath,
		})

		require.Error(t, err)
		assert.True(t, errors.Is(err, mcp.ErrTokenExchangerConfigRequired))
		assert.Nil(t, exchanger)
	})
}

func TestOAuthTokenExchanger_ExchangeToken(t *testing.T) {
	t.Run("nil request returns a typed error", func(t *testing.T) {
		exchanger := testExchanger(t, nil)

		result, err := exchanger.ExchangeToken(context.Background(), nil)

		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "token exchange request is required")
	})

	t.Run("issues a token against a compliant IdP", func(t *testing.T) {
		server := newTestIDPServer(t, stubIDPResponse{
			issuedToken:     testIssuedAccessToken,
			issuedTokenType: testIssuedTokenType,
			tokenType:       testIssuedTokenTypeResp,
			expiresIn:       3600,
			scope:           "tools:read tools:write",
		})
		exchanger := testExchanger(t, server)

		result, err := exchanger.ExchangeToken(context.Background(), &mcp.TokenExchangeRequest{
			SubjectToken:     "subject-id-token",
			SubjectTokenType: oauthex.TokenTypeIDToken,
			Audience:         "https://mcp-auth.example.com",
			Resource:         "https://mcp.example.com",
			Scope:            []string{"tools:read"},
		})

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, testIssuedAccessToken, result.AccessToken)
		assert.Equal(t, testIssuedTokenType, result.IssuedTokenType)
		assert.Equal(t, testIssuedTokenTypeResp, result.TokenType)
		assert.True(t, result.Expiry.After(time.Now()))
		assert.Equal(t, []string{"tools:read", "tools:write"}, result.Scope)
	})

	t.Run("applies the SEP-990 default requested_token_type", func(t *testing.T) {
		var seenRequestedType string
		server := newTestIDPServer(t, stubIDPResponse{
			issuedToken:     testIssuedAccessToken,
			issuedTokenType: testIssuedTokenType,
			tokenType:       testIssuedTokenTypeResp,
			onRequest: func(values url.Values) {
				seenRequestedType = values.Get("requested_token_type")
			},
		})
		exchanger := testExchanger(t, server)

		_, err := exchanger.ExchangeToken(context.Background(), &mcp.TokenExchangeRequest{
			SubjectToken:     "subject-id-token",
			SubjectTokenType: oauthex.TokenTypeIDToken,
			Audience:         "https://mcp-auth.example.com",
			Resource:         "https://mcp.example.com",
		})

		require.NoError(t, err)
		assert.Equal(t, oauthex.TokenTypeIDJAG, seenRequestedType)
	})

	t.Run("propagates an explicit RequestedTokenType", func(t *testing.T) {
		var seenRequestedType string
		const customType = "urn:example:params:oauth:token-type:custom"

		server := newTestIDPServer(t, stubIDPResponse{
			issuedToken:     testIssuedAccessToken,
			issuedTokenType: customType,
			tokenType:       testIssuedTokenTypeResp,
			onRequest: func(values url.Values) {
				seenRequestedType = values.Get("requested_token_type")
			},
		})
		exchanger := testExchanger(t, server)

		_, err := exchanger.ExchangeToken(context.Background(), &mcp.TokenExchangeRequest{
			SubjectToken:       "subject-id-token",
			SubjectTokenType:   oauthex.TokenTypeIDToken,
			Audience:           "https://mcp-auth.example.com",
			Resource:           "https://mcp.example.com",
			RequestedTokenType: customType,
		})

		require.NoError(t, err)
		assert.Equal(t, customType, seenRequestedType)
	})

	t.Run("surfaces IdP errors as wrapped internal errors", func(t *testing.T) {
		server := newTestIDPServer(t, stubIDPResponse{
			forceStatus: http.StatusUnauthorized,
			body:        `{"error":"invalid_client"}`,
		})
		exchanger := testExchanger(t, server)

		result, err := exchanger.ExchangeToken(context.Background(), &mcp.TokenExchangeRequest{
			SubjectToken:     "subject-id-token",
			SubjectTokenType: oauthex.TokenTypeIDToken,
			Audience:         "https://mcp-auth.example.com",
			Resource:         "https://mcp.example.com",
		})

		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "token exchange failed")
	})
}

// basicClientCredentials returns a fully-valid ClientCredentials used by the
// config-validation tests. Values are placeholders; no network traffic is
// issued in those tests.
func basicClientCredentials() *oauthex.ClientCredentials {
	return &oauthex.ClientCredentials{
		ClientID: "test-client",
		ClientSecretAuth: &oauthex.ClientSecretAuth{
			ClientSecret: "test-secret",
		},
	}
}

// testExchanger builds an OAuthTokenExchanger pointed at the given stub
// server. When server is nil, the exchanger points at an https URL that is
// never dialed; this lets validation-only tests exercise NewOAuthTokenExchanger
// without a live listener.
func testExchanger(t *testing.T, server *httptest.Server) mcp.TokenExchanger {
	t.Helper()

	endpoint := testIDPBaseURL + testTokenEndpointPath
	var httpClient *http.Client

	if server != nil {
		endpoint = server.URL + testTokenEndpointPath
		httpClient = server.Client()
	}

	exchanger, err := mcp.NewOAuthTokenExchanger(&mcp.OAuthTokenExchangerConfig{
		TokenEndpoint:     endpoint,
		ClientCredentials: basicClientCredentials(),
		HTTPClient:        httpClient,
	})
	require.NoError(t, err)

	return exchanger
}

// stubIDPResponse parameterises the test IdP behavior for a single request.
type stubIDPResponse struct {
	issuedToken     string
	issuedTokenType string
	tokenType       string
	expiresIn       int
	scope           string
	forceStatus     int
	body            string
	onRequest       func(url.Values)
}

// newTestIDPServer spins up an httptest TLS server that responds to RFC 8693
// token exchange requests with the configured response. The server's client
// certificate is automatically trusted; callers should pass server.Client()
// into the exchanger so TLS validation succeeds.
func newTestIDPServer(t *testing.T, resp stubIDPResponse) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc(testTokenEndpointPath, func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, testMaxFormBodyBytes)
		require.NoError(t, r.ParseForm())
		if resp.onRequest != nil {
			resp.onRequest(r.PostForm)
		}

		if resp.forceStatus != 0 {
			http.Error(w, resp.body, resp.forceStatus)
			return
		}

		expiresIn := resp.expiresIn
		if expiresIn == 0 {
			expiresIn = 3600
		}

		payload := map[string]any{
			"access_token":      resp.issuedToken,
			"issued_token_type": resp.issuedTokenType,
			"token_type":        resp.tokenType,
			"expires_in":        expiresIn,
		}
		if resp.scope != "" {
			payload["scope"] = resp.scope
		}

		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(payload))
	})

	server := httptest.NewTLSServer(mux)
	t.Cleanup(server.Close)

	// Trust the server's self-signed cert on the client side so the oauthex
	// HTTPS scheme check and the TLS handshake both succeed.
	certPool := x509.NewCertPool()
	certPool.AddCert(server.Certificate())
	transport := server.Client().Transport.(*http.Transport)
	transport.TLSClientConfig = &tls.Config{RootCAs: certPool, MinVersion: tls.VersionTLS12}

	return server
}
