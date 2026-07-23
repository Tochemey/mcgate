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

package config

import "github.com/tochemey/portcullis/mcp"

// ApplyDefaults fills zero values in cfg with defaults.
func ApplyDefaults(cfg *mcp.Config) {
	if cfg.Runtime.SessionIdleTimeout <= 0 {
		cfg.Runtime.SessionIdleTimeout = mcp.DefaultSessionIdleTimeout
	}
	if cfg.Runtime.RequestTimeout <= 0 {
		cfg.Runtime.RequestTimeout = mcp.DefaultRequestTimeout
	}
	if cfg.Runtime.StartupTimeout <= 0 {
		cfg.Runtime.StartupTimeout = mcp.DefaultStartupTimeout
	}
	if cfg.Runtime.HealthProbeInterval <= 0 {
		cfg.Runtime.HealthProbeInterval = mcp.DefaultHealthProbeInterval
	}
	if cfg.Runtime.ShutdownTimeout <= 0 {
		cfg.Runtime.ShutdownTimeout = mcp.DefaultShutdownTimeout
	}
	if cfg.Runtime.RouterPoolSize <= 0 {
		cfg.Runtime.RouterPoolSize = mcp.DefaultRouterPoolSize
	}
	if cfg.Tools == nil {
		cfg.Tools = []mcp.Tool{}
	}
	// HealthProbe.Interval falls back to Runtime.HealthProbeInterval (defaulted
	// above) so that health probing is always scheduled with a default config.
	// The health actor reads HealthProbe.Interval, not Runtime.HealthProbeInterval,
	// and never schedules a probe when the interval is zero.
	if cfg.HealthProbe.Interval <= 0 {
		cfg.HealthProbe.Interval = cfg.Runtime.HealthProbeInterval
	}
	if cfg.HealthProbe.Timeout <= 0 {
		cfg.HealthProbe.Timeout = mcp.DefaultHealthProbeTimeout
	}
	if cfg.Credentials.MaxCacheEntries <= 0 {
		cfg.Credentials.MaxCacheEntries = mcp.DefaultMaxCacheEntries
	}
	if cfg.Audit.MailboxSize <= 0 {
		cfg.Audit.MailboxSize = mcp.DefaultAuditMailboxSize
	}
	if cfg.Tenants == nil {
		cfg.Tenants = []mcp.TenantConfig{}
	}
}
