// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package typesafe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

// maxErrorBodyBytes bounds how much of a non-2xx response body Ask reads into
// its returned error, so a misbehaving endpoint cannot exhaust memory.
const maxErrorBodyBytes = 4 * 1024

// Client calls the TypeSafe System One API (https://docs.typesafe.ai/api). It
// is safe for concurrent use. Callers construct one Client per process and
// share it; MaxConcurrentRequests bounds concurrent outbound calls across all
// callers sharing that Client.
type Client struct {
	httpClient *http.Client
	endpoint   string
	model      string
	apiKey     string
	semaphore  chan struct{}
}

// NewClient builds a Client from cfg. It panics if cfg is nil: callers must
// only construct a Client when cfg.Enabled is true, deciding that once at
// bootstrap rather than checking it on every call. NewClient returns an error
// if cfg.Enabled is true but the configured API key environment variable is
// unset or empty, since sending a request with a blank bearer token would
// fail confusingly at the TypeSafe API instead of at startup.
func NewClient(cfg *Config) (*Client, error) {
	if cfg == nil {
		panic("typesafe: NewClient requires a non-nil *Config")
	}
	if !cfg.Enabled {
		panic("typesafe: NewClient must not be called when Config.Enabled is false")
	}

	apiKeyEnvVar := cfg.APIKeyEnvVar
	if apiKeyEnvVar == "" {
		apiKeyEnvVar = DefaultAPIKeyEnvVar
	}
	apiKey := os.Getenv(apiKeyEnvVar)
	if apiKey == "" {
		return nil, fmt.Errorf("typesafe: environment variable %s is unset or empty", apiKeyEnvVar)
	}

	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	model := cfg.Model
	if model == "" {
		model = DefaultModel
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	maxConcurrentRequests := cfg.MaxConcurrentRequests
	if maxConcurrentRequests <= 0 {
		maxConcurrentRequests = DefaultMaxConcurrentRequests
	}

	return &Client{
		httpClient: &http.Client{Timeout: timeout},
		endpoint:   endpoint,
		model:      model,
		apiKey:     apiKey,
		semaphore:  make(chan struct{}, maxConcurrentRequests),
	}, nil
}

// Ask sends state and a batch of named Questions to TypeSafe in a single
// request and returns the Answers, keyed by the same question names. Send
// every question the caller might need, including speculative ones about
// branches that may turn out not to apply: TypeSafe evaluates them all in
// parallel against the same state, and the caller reads only the answers it
// ends up needing. Ask blocks until a slot under MaxConcurrentRequests is
// free or ctx is done.
func (c *Client) Ask(ctx context.Context, state any, questions map[string]Question) (*Response, error) {
	if len(questions) == 0 {
		return nil, fmt.Errorf("typesafe: Ask requires at least one question")
	}

	select {
	case c.semaphore <- struct{}{}:
		defer func() { <-c.semaphore }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	requestBody, err := json.Marshal(request{State: state, Model: c.model, Questions: questions})
	if err != nil {
		return nil, fmt.Errorf("typesafe: encode request: %w", err)
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(requestBody))
	if err != nil {
		return nil, fmt.Errorf("typesafe: build request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+c.apiKey)

	httpResponse, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("typesafe: request failed: %w", err)
	}
	defer httpResponse.Body.Close()

	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		errorBody, _ := io.ReadAll(io.LimitReader(httpResponse.Body, maxErrorBodyBytes))
		return nil, fmt.Errorf("typesafe: request failed with status %d: %s", httpResponse.StatusCode, errorBody)
	}

	var response Response
	if err := json.NewDecoder(httpResponse.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("typesafe: decode response: %w", err)
	}
	return &response, nil
}

// request is the wire shape TypeSafe expects. It is unexported because
// callers build a request only through Ask, never directly.
type request struct {
	State     any                 `json:"state"`
	Model     string              `json:"model"`
	Questions map[string]Question `json:"questions"`
}
