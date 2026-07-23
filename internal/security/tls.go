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

package security

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	gtls "github.com/tochemey/goakt/v4/tls"

	"github.com/tochemey/portcullis/mcp"
)

// productionCipherSuites lists the TLS 1.2 cipher suites permitted for production use.
// All entries use ECDHE (forward secrecy) and AEAD encryption (GCM or ChaCha20-Poly1305).
// CBC-based and RSA key-exchange suites are intentionally excluded.
// TLS 1.3 cipher suites are managed by the Go runtime and are not configurable here.
var productionCipherSuites = []uint16{
	tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
	tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
	tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
	tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
	tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
	tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
}

// productionCurvePreferences lists the elliptic curves preferred for ECDHE key exchange.
// X25519 is listed first for performance; P-256 and P-384 follow for broader compatibility.
var productionCurvePreferences = []tls.CurveID{
	tls.X25519,
	tls.CurveP256,
	tls.CurveP384,
}

// BuildServerTLSConfig constructs a hardened tls.Config for an HTTPS server.
//
// certFile and keyFile are required. clientCAFile, when non-empty, enables
// mutual TLS by requiring client certificates signed by that CA.
//
// The returned config enforces TLS 1.2 as the minimum version, restricts cipher
// suites to ECDHE+AEAD combinations, and prefers modern elliptic curves.
func BuildServerTLSConfig(certFile, keyFile, clientCAFile string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load server cert: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates:     []tls.Certificate{cert},
		MinVersion:       tls.VersionTLS12,
		CipherSuites:     productionCipherSuites,
		CurvePreferences: productionCurvePreferences,
	}

	if clientCAFile != "" {
		caPEM, err := os.ReadFile(clientCAFile)
		if err != nil {
			return nil, fmt.Errorf("read client CA: %w", err)
		}

		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("parse client CA: no valid certificates")
		}

		tlsConfig.ClientCAs = pool
		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
	}

	return tlsConfig, nil
}

// BuildClientTLSConfig constructs a hardened tls.Config for outbound TLS connections.
// When cfg is nil, returns nil (use default client behavior).
//
// The returned config enforces TLS 1.2 as the minimum version, restricts cipher
// suites to ECDHE+AEAD combinations, prefers modern elliptic curves, and
// disables TLS renegotiation.
//
// Validation errors:
//   - InsecureSkipVerify and CACertFile cannot both be set (contradictory)
//   - ClientCertFile and ClientKeyFile must be provided together or not at all
func BuildClientTLSConfig(config *mcp.TLSClientConfig) (*tls.Config, error) {
	if config == nil {
		return nil, nil
	}

	if config.InsecureSkipVerify && config.CACertFile != "" {
		return nil, fmt.Errorf("TLS client config: InsecureSkipVerify and CACertFile are mutually exclusive")
	}

	hasClientCert := config.ClientCertFile != ""
	hasClientKey := config.ClientKeyFile != ""
	if hasClientCert != hasClientKey {
		return nil, fmt.Errorf("TLS client config: ClientCertFile and ClientKeyFile must both be set or both be empty")
	}

	tlsConfig := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		CipherSuites:       productionCipherSuites,
		CurvePreferences:   productionCurvePreferences,
		Renegotiation:      tls.RenegotiateNever,
		InsecureSkipVerify: config.InsecureSkipVerify, //nolint:gosec
	}

	if config.CACertFile != "" {
		caPEM, err := os.ReadFile(config.CACertFile)
		if err != nil {
			return nil, fmt.Errorf("read CA cert: %w", err)
		}

		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("parse CA cert: no valid certificates")
		}

		tlsConfig.RootCAs = pool
	}

	if hasClientCert {
		cert, err := tls.LoadX509KeyPair(config.ClientCertFile, config.ClientKeyFile)
		if err != nil {
			return nil, fmt.Errorf("load client cert: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return tlsConfig, nil
}

// BuildRemotingTLSInfo constructs a goakt tls.Info from RemotingTLSConfig for
// use with GoAkt remoting and cluster. Returns nil when cfg is nil.
//
// ServerConfig is built from CertFile, KeyFile, and ClientCAFile.
// ClientConfig is built from CACertFile, ClientCertFile, ClientKeyFile, and
// InsecureSkipVerify.
func BuildRemotingTLSInfo(config *mcp.RemotingTLSConfig) (*gtls.Info, error) {
	if config == nil {
		return nil, nil
	}

	if config.CertFile == "" || config.KeyFile == "" {
		return nil, fmt.Errorf("remoting TLS: CertFile and KeyFile are required")
	}

	serverCfg, err := BuildServerTLSConfig(config.CertFile, config.KeyFile, config.ClientCAFile)
	if err != nil {
		return nil, fmt.Errorf("remoting TLS server: %w", err)
	}

	clientCfg, err := BuildClientTLSConfig(&mcp.TLSClientConfig{
		CACertFile:         config.CACertFile,
		ClientCertFile:     config.ClientCertFile,
		ClientKeyFile:      config.ClientKeyFile,
		InsecureSkipVerify: config.InsecureSkipVerify,
	})

	if err != nil {
		return nil, fmt.Errorf("remoting TLS client: %w", err)
	}

	if clientCfg == nil {
		clientCfg = &tls.Config{MinVersion: tls.VersionTLS12} //nolint:gosec
	}

	return &gtls.Info{
		ServerConfig: serverCfg,
		ClientConfig: clientCfg,
	}, nil
}
