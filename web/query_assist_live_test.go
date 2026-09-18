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
	"os"
	"testing"

	"github.com/superdurable/dex/service/common/typesafe"
	dexweb "github.com/superdurable/dex/web"
)

func toJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

// TestInterpretSearchAgainstLiveTypeSafeAPI is a calibration suite, not a
// correctness gate. It runs the real /api/flows/search/interpret endpoint —
// through the actual Dex Web HTTP server, exercising BuildQuestions and
// Compose exactly as production does — against the live TypeSafe API. It is
// skipped unless TYPESAFE_API_KEY is set, so it never runs in ordinary CI.
//
// Each case logs what TypeSafe actually returned and only fails on a
// genuinely missing expected filter; it does not fail on an extra filter the
// model added, since that is calibration data about real model behavior,
// not a defect in this code. Read the test log, not just pass/fail, to
// judge whether the 0.6/0.7 thresholds in queryassist/compose.go still fit
// observed confidence.
func TestInterpretSearchAgainstLiveTypeSafeAPI(t *testing.T) {
	if os.Getenv("TYPESAFE_API_KEY") == "" {
		t.Skip("TYPESAFE_API_KEY not set; skipping live TypeSafe calibration suite")
	}

	harness := newHarnessWithConfig(t, &flowService{}, &dexweb.Config{
		BindAddress: "127.0.0.1",
		Port:        dexweb.DefaultPort,
		TypeSafe:    &typesafe.Config{Enabled: true}, // real endpoint, real model, key from the environment
	})

	type expectedFilter struct {
		field    string
		operator string
		value    string
	}
	type liveCase struct {
		name         string
		request      string
		customFields []map[string]string
		want         []expectedFilter // each must appear among the produced filters
		wantEmpty    bool             // request is deliberately unanswerable
		// knownLimitation names a documented, unfixed model weakness (see
		// https://docs.typesafe.ai/model-jaggedness/jev-1.13 on negation) a
		// miss is logged, not failed, so the case still runs every time this
		// suite does without permanently red CI once the key is set.
		knownLimitation string
	}

	cases := []liveCase{
		{name: "failed flows", request: "show me failed flows",
			want: []expectedFilter{{"ExecutionStatus", "=", "Failed"}}},
		{name: "running flows", request: "flows that are still running",
			want: []expectedFilter{{"ExecutionStatus", "=", "Running"}}},
		{name: "completed flows", request: "completed flows",
			want: []expectedFilter{{"ExecutionStatus", "=", "Completed"}}},
		{name: "canceled flows", request: "cancelled flows",
			want: []expectedFilter{{"ExecutionStatus", "=", "Canceled"}}},
		{name: "timed out flows", request: "flows that timed out",
			want: []expectedFilter{{"ExecutionStatus", "=", "TimedOut"}}},
		{name: "continued as new flows", request: "flows that continued as new",
			want: []expectedFilter{{"ExecutionStatus", "=", "ContinuedAsNew"}}},
		{name: "negated status", request: "flows that are not failed",
			want:            []expectedFilter{{"ExecutionStatus", "!=", "Failed"}},
			knownLimitation: "negation: the value question answers \"none\" instead of \"Failed\" under a negated statement"},
		{name: "flow type bare word", request: "checkout flows",
			want:            []expectedFilter{{"FlowType", "=", "checkout"}},
			knownLimitation: "ambiguous phrasing: \"checkout flows\" reasonably reads as generic prose, not a FlowType name"},
		{name: "flow type explicit", request: "flows of type PaymentFlow",
			want: []expectedFilter{{"FlowType", "=", "PaymentFlow"}}},
		{name: "workflow id", request: `flow with id "order-42"`,
			want: []expectedFilter{{"WorkflowId", "=", "order-42"}}},
		{name: "run id", request: `run id "abc-123"`,
			want: []expectedFilter{{"RunId", "=", "abc-123"}}},
		{name: "last hour", request: "flows from the last hour",
			want: []expectedFilter{{"StartTime", ">=", ""}}}, // value is a computed timestamp; presence is what's checked
		{name: "last 24 hours", request: "flows from the last 24 hours",
			want: []expectedFilter{{"StartTime", ">=", ""}}},
		{name: "yesterday", request: "flows from yesterday",
			want: []expectedFilter{{"StartTime", ">=", ""}, {"StartTime", "<", ""}}},
		{name: "last 7 days", request: "flows from the last 7 days",
			want: []expectedFilter{{"StartTime", ">=", ""}}},
		{name: "older than 30 days", request: "flows older than 30 days",
			want: []expectedFilter{{"StartTime", "<", ""}}},
		{name: "combined status and time", request: "failed flows that started yesterday",
			want: []expectedFilter{
				{"ExecutionStatus", "=", "Failed"},
				{"StartTime", ">=", ""},
				{"StartTime", "<", ""},
			}},
		{name: "custom keyword field", request: "orders with status completed",
			customFields: []map[string]string{{"name": "OrderStatus", "kind": "keyword"}},
			want:         []expectedFilter{{"OrderStatus", "=", "completed"}}},
		{name: "custom number field", request: "orders over 100 dollars",
			customFields: []map[string]string{{"name": "Amount", "kind": "number"}},
			want:         []expectedFilter{{"Amount", ">", "100"}}},
		{name: "unanswerable request", request: "hello", wantEmpty: true},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			body := map[string]any{"request": testCase.request}
			if testCase.customFields != nil {
				body["customFields"] = testCase.customFields
			}
			response := postJSON(t, harness.http.URL+"/api/flows/search/interpret", toJSON(t, body))
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
			t.Logf("request=%q filters=%+v confidence=%.2f needsReview=%v",
				testCase.request, result.Filters, result.Confidence, result.NeedsReview)

			if testCase.wantEmpty {
				if len(result.Filters) != 0 && !result.NeedsReview {
					t.Errorf("expected an unanswerable request to produce no confident filters, got %+v", result.Filters)
				}
				return
			}

			for _, want := range testCase.want {
				found := false
				for _, got := range result.Filters {
					if got.Field != want.field || got.Operator != want.operator {
						continue
					}
					if want.value != "" && got.Value != want.value {
						continue
					}
					found = true
					break
				}
				if found {
					continue
				}
				message := fmt.Sprintf("missing expected filter %+v in produced filters %+v", want, result.Filters)
				if testCase.knownLimitation != "" {
					t.Logf("known limitation (%s): %s", testCase.knownLimitation, message)
					continue
				}
				t.Errorf("%s", message)
			}
		})
	}
}
