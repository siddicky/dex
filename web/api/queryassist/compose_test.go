// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package queryassist

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/superdurable/dex/service/common/typesafe"
)

func floatPtr(value float64) *float64 { return &value }

func choiceAnswer(choice string, confidence float64) typesafe.Answer {
	return typesafe.Answer{Type: typesafe.QuestionTypeChoice, Choice: choice, Confidence: floatPtr(confidence)}
}

func noulAnswer(probability float64) typesafe.Answer {
	return typesafe.Answer{Type: typesafe.QuestionTypeNoul, Noul: probability}
}

// baseAnswers returns a response where nothing is constrained: every field
// Compose considers (the built-ins plus customFields) answers "not
// constrained" / "none". Tests override just the answers relevant to the
// case under test.
func baseAnswers(customFields []Field) map[string]typesafe.Answer {
	answers := map[string]typesafe.Answer{
		timeFieldQuestionID:  choiceAnswer(timeFieldNone, 0.95),
		timeWindowQuestionID: choiceAnswer("none", 0.95),
	}
	for _, field := range allFields(customFields) {
		answers[constrainedQuestionID(field.Name)] = noulAnswer(0.05)
		answers[operatorQuestionID(field.Name)] = choiceAnswer("=", 0.9)
		answers[valueQuestionID(field.Name)] = choiceAnswer("none", 0.9)
	}
	return answers
}

func TestComposeReturnsNeedsReviewWhenNothingIsConstrained(t *testing.T) {
	response := &typesafe.Response{Answers: baseAnswers(nil)}

	result, err := Compose(response, nil, time.Now())
	require.NoError(t, err)
	require.Empty(t, result.Filters)
	require.True(t, result.NeedsReview)
}

func TestComposeProducesAFilterForAConstrainedEnumField(t *testing.T) {
	answers := baseAnswers(nil)
	answers[constrainedQuestionID("ExecutionStatus")] = noulAnswer(0.92)
	answers[operatorQuestionID("ExecutionStatus")] = choiceAnswer("=", 0.9)
	answers[valueQuestionID("ExecutionStatus")] = choiceAnswer("Failed", 0.85)
	response := &typesafe.Response{Answers: answers}

	result, err := Compose(response, nil, time.Now())
	require.NoError(t, err)
	require.Equal(t, []Filter{{ID: "ask:ExecutionStatus:value", Field: "ExecutionStatus", Operator: "=", Value: "Failed"}}, result.Filters)
	require.InDelta(t, 0.85, result.Confidence, 0.0001)
	require.False(t, result.NeedsReview)
}

func TestComposeProducesAFilterForAConstrainedCustomKeywordField(t *testing.T) {
	customFields := []Field{{Name: "OrderStatus", Kind: FieldKindKeyword, Meaning: "the order's status"}}
	answers := baseAnswers(customFields)
	answers[constrainedQuestionID("OrderStatus")] = noulAnswer(0.9)
	answers[operatorQuestionID("OrderStatus")] = choiceAnswer("=", 0.88)
	answers[valueQuestionID("OrderStatus")] = choiceAnswer("completed", 0.8)
	response := &typesafe.Response{Answers: answers}

	result, err := Compose(response, customFields, time.Now())
	require.NoError(t, err)
	require.Equal(t, []Filter{{ID: "ask:OrderStatus:value", Field: "OrderStatus", Operator: "=", Value: "completed"}}, result.Filters)
}

func TestComposeRejectsAHallucinatedEnumValue(t *testing.T) {
	answers := baseAnswers(nil)
	answers[constrainedQuestionID("ExecutionStatus")] = noulAnswer(0.9)
	answers[operatorQuestionID("ExecutionStatus")] = choiceAnswer("=", 0.9)
	// "InProgress" was never offered as a criterion for ExecutionStatus.
	answers[valueQuestionID("ExecutionStatus")] = choiceAnswer("InProgress", 0.9)
	response := &typesafe.Response{Answers: answers}

	_, err := Compose(response, nil, time.Now())
	require.ErrorContains(t, err, `field "ExecutionStatus" got value "InProgress"`)
}

func TestComposeRejectsAnOperatorItNeverOffered(t *testing.T) {
	answers := baseAnswers(nil)
	answers[constrainedQuestionID("ExecutionStatus")] = noulAnswer(0.9)
	// ">" is not a valid operator for an enum field.
	answers[operatorQuestionID("ExecutionStatus")] = choiceAnswer(">", 0.9)
	answers[valueQuestionID("ExecutionStatus")] = choiceAnswer("Failed", 0.9)
	response := &typesafe.Response{Answers: answers}

	_, err := Compose(response, nil, time.Now())
	require.ErrorContains(t, err, `field "ExecutionStatus" got operator ">"`)
}

func TestComposeLowConfidenceNeedsReview(t *testing.T) {
	answers := baseAnswers(nil)
	answers[constrainedQuestionID("FlowType")] = noulAnswer(0.9)
	answers[operatorQuestionID("FlowType")] = choiceAnswer("=", 0.9)
	answers[valueQuestionID("FlowType")] = choiceAnswer("Checkout", 0.4) // below reviewThreshold
	response := &typesafe.Response{Answers: answers}

	result, err := Compose(response, nil, time.Now())
	require.NoError(t, err)
	require.NotEmpty(t, result.Filters)
	require.True(t, result.NeedsReview)
}

func TestComposeTimeWindowLastHour(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 30, 0, 0, time.UTC)
	answers := baseAnswers(nil)
	answers[timeFieldQuestionID] = choiceAnswer(timeFieldStartTime, 0.9)
	answers[timeWindowQuestionID] = choiceAnswer("last_hour", 0.9)
	response := &typesafe.Response{Answers: answers}

	result, err := Compose(response, nil, now)
	require.NoError(t, err)
	require.Equal(t, []Filter{
		{ID: "ask:StartTime:from", Field: "StartTime", Operator: ">=", Value: "2026-06-15T11:30:00Z"},
	}, result.Filters)
}

func TestComposeTimeWindowYesterdayUsesLocalMidnightBounds(t *testing.T) {
	location := time.FixedZone("UTC-7", -7*60*60)
	now := time.Date(2026, 6, 15, 9, 0, 0, 0, location) // 09:00 local on the 15th
	answers := baseAnswers(nil)
	answers[timeFieldQuestionID] = choiceAnswer(timeFieldCloseTime, 0.9)
	answers[timeWindowQuestionID] = choiceAnswer("yesterday", 0.9)
	response := &typesafe.Response{Answers: answers}

	result, err := Compose(response, nil, now)
	require.NoError(t, err)
	require.Len(t, result.Filters, 2)
	require.Equal(t, Filter{ID: "ask:CloseTime:from", Field: "CloseTime", Operator: ">=", Value: "2026-06-14T07:00:00Z"}, result.Filters[0])
	require.Equal(t, Filter{ID: "ask:CloseTime:to", Field: "CloseTime", Operator: "<", Value: "2026-06-15T07:00:00Z"}, result.Filters[1])
}

func TestComposeTimeWindowOlderThan30DaysHasOnlyAnUpperBound(t *testing.T) {
	now := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	answers := baseAnswers(nil)
	answers[timeFieldQuestionID] = choiceAnswer(timeFieldStartTime, 0.9)
	answers[timeWindowQuestionID] = choiceAnswer("older_than_30_days", 0.9)
	response := &typesafe.Response{Answers: answers}

	result, err := Compose(response, nil, now)
	require.NoError(t, err)
	require.Equal(t, []Filter{
		{ID: "ask:StartTime:to", Field: "StartTime", Operator: "<", Value: "2026-05-16T00:00:00Z"},
	}, result.Filters)
}

func TestComposeRejectsAnUnknownTimeWindow(t *testing.T) {
	answers := baseAnswers(nil)
	answers[timeFieldQuestionID] = choiceAnswer(timeFieldStartTime, 0.9)
	answers[timeWindowQuestionID] = choiceAnswer("next_week", 0.9) // never offered
	response := &typesafe.Response{Answers: answers}

	_, err := Compose(response, nil, time.Now())
	require.ErrorContains(t, err, `time.window got "next_week"`)
}
