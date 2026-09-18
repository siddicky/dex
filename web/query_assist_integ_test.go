// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package web_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/superdurable/dex/gen/dexpb"
	"github.com/superdurable/dex/service/common/typesafe"
	dexweb "github.com/superdurable/dex/web"
)

// fakeTypeSafeAnswer is the minimal shape client_test.go's counterpart in
// package typesafe also uses, duplicated here because this file is an
// external (web_test) package and cannot reach typesafe's unexported wire
// type.
type fakeTypeSafeAnswer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice,omitempty"`
	Noul          float64            `json:"noul,omitempty"`
	Confidence    *float64           `json:"confidence,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
}

func floatPtr(value float64) *float64 { return &value }

// newFakeTypeSafeServer returns an httptest.Server that always answers with
// answers, regardless of the questions actually asked. Tests only need to
// control the answers TypeSafe would give; BuildQuestions is exercised for
// real inside the running Dex Web process.
func newFakeTypeSafeServer(t *testing.T, answers map[string]fakeTypeSafeAnswer) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "jev-latest",
			"answers": answers,
			"usage":   map[string]int{"input_tokens": 1, "output_tokens": 1},
		})
	}))
	t.Cleanup(server.Close)
	return server
}

// notConstrainedAnswers fills every field BuildQuestions asks about (the
// four built-ins) with "not constrained" / "none", so a test only needs to
// override the handful of answers relevant to its scenario.
func notConstrainedAnswers() map[string]fakeTypeSafeAnswer {
	answers := map[string]fakeTypeSafeAnswer{
		"time.field":  {Type: "choice", Choice: "none", Confidence: floatPtr(0.95)},
		"time.window": {Type: "choice", Choice: "none", Confidence: floatPtr(0.95)},
	}
	for _, field := range []string{"ExecutionStatus", "WorkflowId", "RunId", "FlowType"} {
		answers[field+".constrained"] = fakeTypeSafeAnswer{Type: "noul", Noul: 0.05}
		answers[field+".operator"] = fakeTypeSafeAnswer{Type: "choice", Choice: "=", Confidence: floatPtr(0.9)}
		answers[field+".value"] = fakeTypeSafeAnswer{Type: "choice", Choice: "none", Confidence: floatPtr(0.9)}
	}
	return answers
}

func newQueryAssistHarness(t *testing.T, service dexpb.FlowServiceServer, typeSafeServerURL string) *harness {
	t.Helper()
	envVar := "TEST_TYPESAFE_API_KEY_" + t.Name()
	t.Setenv(envVar, "test-api-key")
	return newHarnessWithConfig(t, service, &dexweb.Config{
		BindAddress: "127.0.0.1",
		Port:        dexweb.DefaultPort,
		TypeSafe: &typesafe.Config{
			Enabled:      true,
			Endpoint:     typeSafeServerURL,
			APIKeyEnvVar: envVar,
		},
	})
}

func TestInterpretSearchProducesFiltersThatRunThroughRealSearchFlows(t *testing.T) {
	answers := notConstrainedAnswers()
	answers["ExecutionStatus.constrained"] = fakeTypeSafeAnswer{Type: "noul", Noul: 0.92}
	answers["ExecutionStatus.operator"] = fakeTypeSafeAnswer{Type: "choice", Choice: "=", Confidence: floatPtr(0.9)}
	answers["ExecutionStatus.value"] = fakeTypeSafeAnswer{Type: "choice", Choice: "Failed", Confidence: floatPtr(0.85)}
	typeSafeServer := newFakeTypeSafeServer(t, answers)

	service := &flowService{searchRequests: make(chan *dexpb.SearchFlowsRequest, 1)}
	harness := newQueryAssistHarness(t, service, typeSafeServer.URL)

	response := postJSON(t, harness.http.URL+"/api/flows/search/interpret", `{"request":"show failed flows"}`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("interpret status = %d, body = %s", response.StatusCode, readBody(t, response))
	}

	var result struct {
		Filters []struct {
			Field    string `json:"field"`
			Operator string `json:"operator"`
			Value    string `json:"value"`
		} `json:"filters"`
		Confidence  float64 `json:"confidence"`
		NeedsReview bool    `json:"needsReview"`
	}
	decodeResponse(t, response, &result)
	if len(result.Filters) != 1 || result.Filters[0].Field != "ExecutionStatus" ||
		result.Filters[0].Operator != "=" || result.Filters[0].Value != "Failed" {
		t.Fatalf("unexpected filters: %+v", result.Filters)
	}
	if result.NeedsReview {
		t.Fatalf("expected NeedsReview = false at confidence %v", result.Confidence)
	}

	// Prove the filter is not just well-shaped JSON: build the equivalent
	// visibility query and confirm it runs through the real SearchFlows path.
	query := fmt.Sprintf("%s %s %q", result.Filters[0].Field, result.Filters[0].Operator, result.Filters[0].Value)
	searchResponse := postJSON(t, harness.http.URL+"/api/flows/search", fmt.Sprintf(`{"query":%q,"pageSize":10}`, query))
	defer searchResponse.Body.Close()
	if searchResponse.StatusCode != http.StatusOK {
		t.Fatalf("search status = %d", searchResponse.StatusCode)
	}
	select {
	case searchRequest := <-service.searchRequests:
		wantQuery := fmt.Sprintf("(%s) AND (WorkflowType = \"Engine\")", query)
		if searchRequest.GetQuery() != wantQuery {
			t.Fatalf("search query = %q, want %q", searchRequest.GetQuery(), wantQuery)
		}
	case <-time.After(time.Second):
		t.Fatal("SearchFlows was never called with the interpreted filter")
	}
}

func TestInterpretSearchRejectsAHallucinatedFieldWithoutCallingSearchFlows(t *testing.T) {
	answers := notConstrainedAnswers()
	answers["ExecutionStatus.constrained"] = fakeTypeSafeAnswer{Type: "noul", Noul: 0.9}
	// "InProgress" was never offered as a criterion for ExecutionStatus.
	answers["ExecutionStatus.operator"] = fakeTypeSafeAnswer{Type: "choice", Choice: "=", Confidence: floatPtr(0.9)}
	answers["ExecutionStatus.value"] = fakeTypeSafeAnswer{Type: "choice", Choice: "InProgress", Confidence: floatPtr(0.9)}
	typeSafeServer := newFakeTypeSafeServer(t, answers)

	service := &flowService{searchRequests: make(chan *dexpb.SearchFlowsRequest, 1)}
	harness := newQueryAssistHarness(t, service, typeSafeServer.URL)

	response := postJSON(t, harness.http.URL+"/api/flows/search/interpret", `{"request":"show flows"}`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("interpret status = %d, want 422; body = %s", response.StatusCode, readBody(t, response))
	}
	select {
	case request := <-service.searchRequests:
		t.Fatalf("SearchFlows must not be called for a rejected interpretation, got %v", request)
	case <-time.After(50 * time.Millisecond):
		// expected: no call was made
	}
}

func TestInterpretSearchIsDisabledByDefault(t *testing.T) {
	harness := newHarness(t, &flowService{})

	response := postJSON(t, harness.http.URL+"/api/flows/search/interpret", `{"request":"show failed flows"}`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotImplemented {
		t.Fatalf("interpret status = %d, want 501; body = %s", response.StatusCode, readBody(t, response))
	}
}

func TestInterpretSearchRejectsACustomFieldCollidingWithABuiltIn(t *testing.T) {
	typeSafeServer := newFakeTypeSafeServer(t, notConstrainedAnswers())
	harness := newQueryAssistHarness(t, &flowService{}, typeSafeServer.URL)

	response := postJSON(t, harness.http.URL+"/api/flows/search/interpret",
		`{"request":"show flows","customFields":[{"name":"FlowType","kind":"keyword"}]}`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("interpret status = %d, want 400; body = %s", response.StatusCode, readBody(t, response))
	}
}

func TestInterpretSearchRejectsAnEmptyRequest(t *testing.T) {
	typeSafeServer := newFakeTypeSafeServer(t, notConstrainedAnswers())
	harness := newQueryAssistHarness(t, &flowService{}, typeSafeServer.URL)

	response := postJSON(t, harness.http.URL+"/api/flows/search/interpret", `{"request":""}`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("interpret status = %d, want 400; body = %s", response.StatusCode, readBody(t, response))
	}
}
