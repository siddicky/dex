// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package typesafe

import "time"

const (
	// DefaultAPIKeyEnvVar is the environment variable Config reads its API key from when APIKeyEnvVar is empty.
	DefaultAPIKeyEnvVar = "TYPESAFE_API_KEY"
	// DefaultTimeout bounds one TypeSafe HTTP request when Config.Timeout is zero.
	DefaultTimeout = 10 * time.Second
	// DefaultMaxConcurrentRequests bounds concurrent outbound TypeSafe requests when Config.MaxConcurrentRequests is zero.
	DefaultMaxConcurrentRequests = 8
	// DefaultModel is the TypeSafe model name sent with every request when Config.Model is empty.
	DefaultModel = "jev-latest"
	// DefaultEndpoint is the TypeSafe System One HTTP endpoint used when Config.Endpoint is empty.
	DefaultEndpoint = "https://api.typesafe.ai/v1/systemone"
)

// Config configures the optional TypeSafe System One integration
// (https://docs.typesafe.ai/api) used to turn natural-language input into
// typed judgments, such as interpreting a Flow search request. It is
// deliberately self-contained (stdlib-only dependencies): both the Dex
// server, which embeds it as the TypeSafe field of its own heavier Config,
// and Dex Web, which otherwise avoids importing the server's config package
// to keep its own dependency graph light, import this type directly.
// Immutable after startup. When Enabled is false, the process makes no
// TypeSafe calls and no other field is read.
type Config struct {
	// Enabled turns the integration on. Default false: a stock Dex server makes no outbound
	// TypeSafe calls and needs no API key.
	Enabled bool `yaml:"enabled"`
	// Endpoint is the TypeSafe System One HTTP endpoint. Default DefaultEndpoint.
	Endpoint string `yaml:"endpoint"`
	// Model is the TypeSafe model name sent with every request. Default DefaultModel.
	Model string `yaml:"model"`
	// APIKeyEnvVar names the environment variable holding the TypeSafe API key. Default
	// DefaultAPIKeyEnvVar. The key itself is never read from this config, so it cannot be checked
	// in by mistake; the server reads it from the named environment variable at startup and never
	// forwards it to a browser client.
	APIKeyEnvVar string `yaml:"apiKeyEnvVar"`
	// Timeout bounds one TypeSafe HTTP request. Non-positive defaults to DefaultTimeout.
	Timeout time.Duration `yaml:"timeout"`
	// MaxConcurrentRequests caps concurrent outbound TypeSafe requests across all callers sharing
	// one Client. Non-positive defaults to DefaultMaxConcurrentRequests.
	MaxConcurrentRequests int `yaml:"maxConcurrentRequests"`
}
