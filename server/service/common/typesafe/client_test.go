// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package typesafe

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/superdurable/dex/config"
)

func newTestClient(t *testing.T, server *httptest.Server, cfg config.TypeSafeConfig) *Client {
	t.Helper()
	if cfg.Endpoint == "" {
		cfg.Endpoint = server.URL
	}
	if cfg.APIKeyEnvVar == "" {
		cfg.APIKeyEnvVar = "TEST_TYPESAFE_API_KEY_" + t.Name()
	}
	t.Setenv(cfg.APIKeyEnvVar, "test-api-key")
	cfg.Enabled = true

	client, err := NewClient(&cfg)
	require.NoError(t, err)
	return client
}

func TestNewClientRequiresAPIKey(t *testing.T) {
	envVar := "UNSET_TYPESAFE_API_KEY_" + t.Name()
	_, err := NewClient(&config.TypeSafeConfig{Enabled: true, APIKeyEnvVar: envVar})
	require.ErrorContains(t, err, envVar)
}

func TestNewClientPanicsOnNilConfig(t *testing.T) {
	require.Panics(t, func() {
		_, _ = NewClient(nil)
	})
}

func TestNewClientPanicsWhenDisabled(t *testing.T) {
	require.Panics(t, func() {
		_, _ = NewClient(&config.TypeSafeConfig{Enabled: false})
	})
}

func TestAskSendsAuthorizationAndDecodesTypedAnswers(t *testing.T) {
	var capturedAuth string
	var capturedBody request

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		require.NoError(t, json.NewDecoder(r.Body).Decode(&capturedBody))

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Response{
			Model: "jev-latest",
			Answers: map[string]Answer{
				"is_urgent": {Type: QuestionTypeNoul, Noul: 0.92},
				"category":  {Type: QuestionTypeChoice, Choice: "billing", Confidence: floatPtr(0.81)},
				"severity":  {Type: QuestionTypeScore, Score: 2.4, Confidence: floatPtr(0.6)},
			},
			Usage: Usage{InputTokens: 10, OutputTokens: 5},
		})
	}))
	defer server.Close()

	client := newTestClient(t, server, config.TypeSafeConfig{})

	response, err := client.Ask(context.Background(), "help, my payouts are failing", map[string]Question{
		"is_urgent": NoulQuestion("Does this convey urgency?", nil),
		"category":  ChoiceQuestion("Which category?", map[string]string{"billing": "billing issue", "other": "anything else"}),
		"severity":  ScoreQuestion("How severe?", []string{"low", "medium", "high"}),
	})
	require.NoError(t, err)

	require.Equal(t, "Bearer test-api-key", capturedAuth)
	require.Equal(t, "jev-latest", capturedBody.Model)
	require.Len(t, capturedBody.Questions, 3)

	noul, err := response.Noul("is_urgent")
	require.NoError(t, err)
	require.InDelta(t, 0.92, noul, 0.0001)

	choice, choiceConfidence, err := response.Choice("category")
	require.NoError(t, err)
	require.Equal(t, "billing", choice)
	require.InDelta(t, 0.81, choiceConfidence, 0.0001)

	score, scoreConfidence, err := response.Score("severity")
	require.NoError(t, err)
	require.InDelta(t, 2.4, score, 0.0001)
	require.InDelta(t, 0.6, scoreConfidence, 0.0001)
}

func TestResponseAnswerAccessorsRejectMissingOrWrongType(t *testing.T) {
	response := &Response{Answers: map[string]Answer{
		"category": {Type: QuestionTypeChoice, Choice: "billing"},
	}}

	_, _, err := response.Choice("missing")
	require.ErrorContains(t, err, `no answer for question "missing"`)

	_, err = response.Noul("category")
	require.ErrorContains(t, err, `expected "noul"`)
}

func TestWeakestConfidenceIgnoresNoulAndReturnsTheMinimum(t *testing.T) {
	response := &Response{Answers: map[string]Answer{
		"a": {Type: QuestionTypeNoul, Noul: 0.99},
		"b": {Type: QuestionTypeChoice, Confidence: floatPtr(0.7)},
		"c": {Type: QuestionTypeScore, Confidence: floatPtr(0.4)},
	}}

	weakest, err := response.WeakestConfidence("a", "b", "c")
	require.NoError(t, err)
	require.InDelta(t, 0.4, weakest, 0.0001)
}

func TestWeakestConfidenceErrorsOnMissingQuestion(t *testing.T) {
	response := &Response{Answers: map[string]Answer{
		"b": {Type: QuestionTypeChoice, Confidence: floatPtr(0.7)},
	}}

	_, err := response.WeakestConfidence("b", "missing")
	require.ErrorContains(t, err, `no answer for question "missing"`)
}

func TestAskRejectsEmptyQuestions(t *testing.T) {
	client := newTestClient(t, httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("server should not be called for an empty question set")
	})), config.TypeSafeConfig{})

	_, err := client.Ask(context.Background(), "state", nil)
	require.ErrorContains(t, err, "at least one question")
}

func TestAskSurfacesNonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad question"}`))
	}))
	defer server.Close()

	client := newTestClient(t, server, config.TypeSafeConfig{})

	_, err := client.Ask(context.Background(), "state", map[string]Question{
		"q": NoulQuestion("?", nil),
	})
	require.ErrorContains(t, err, "status 400")
	require.ErrorContains(t, err, "bad question")
}

func TestAskRespectsContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(Response{Answers: map[string]Answer{}})
	}))
	defer server.Close()

	client := newTestClient(t, server, config.TypeSafeConfig{})

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()

	_, err := client.Ask(ctx, "state", map[string]Question{"q": NoulQuestion("?", nil)})
	require.Error(t, err)
}

func floatPtr(value float64) *float64 {
	return &value
}
