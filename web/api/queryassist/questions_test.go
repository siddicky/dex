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

	"github.com/stretchr/testify/require"
)

func TestParseFieldKindAcceptsNonEnumKinds(t *testing.T) {
	for _, kind := range []string{"keyword", "number", "boolean"} {
		parsed, err := ParseFieldKind(kind)
		require.NoError(t, err)
		require.Equal(t, FieldKind(kind), parsed)
	}
}

func TestParseFieldKindRejectsEnumAndUnknownKinds(t *testing.T) {
	_, err := ParseFieldKind("enum")
	require.ErrorContains(t, err, `unknown field kind "enum"`)

	_, err = ParseFieldKind("bogus")
	require.ErrorContains(t, err, `unknown field kind "bogus"`)
}

func TestBuildQuestionsAsksAConstrainedGateForNonEnumFieldsOnly(t *testing.T) {
	customFields := []Field{
		{Name: "OrderStatus", Kind: FieldKindKeyword, Meaning: "the order's status"},
		{Name: "Amount", Kind: FieldKindNumber, Meaning: "the order amount"},
	}
	questions := BuildQuestions("failed orders over 100 from yesterday", customFields)

	// 1 enum built-in (2 questions) + 3 keyword/number built-ins and 2 custom
	// fields (3 questions each), plus the 2 time questions.
	require.Len(t, questions, 1*2+5*3+2)
	require.Contains(t, questions, "OrderStatus.constrained")
	require.Contains(t, questions, "OrderStatus.operator")
	require.Contains(t, questions, "OrderStatus.value")
	require.Contains(t, questions, "Amount.constrained")
	require.Contains(t, questions, "Amount.operator")
	require.NotContains(t, questions, "ExecutionStatus.constrained")
	require.Contains(t, questions, timeFieldQuestionID)
	require.Contains(t, questions, timeWindowQuestionID)
}

func TestBuildQuestionsGivesNumberFieldsComparisonOperators(t *testing.T) {
	questions := BuildQuestions("amount over 100", []Field{
		{Name: "Amount", Kind: FieldKindNumber, Meaning: "the order amount"},
	})
	criteria := questions["Amount.operator"].Criteria.(map[string]string)
	require.Contains(t, criteria, ">")
	require.Contains(t, criteria, ">=")
	require.Contains(t, criteria, "<")
}

func TestBuildQuestionsGivesKeywordFieldsOnlyEqualityOperators(t *testing.T) {
	questions := BuildQuestions("status is completed", []Field{
		{Name: "OrderStatus", Kind: FieldKindKeyword, Meaning: "the order's status"},
	})
	criteria := questions["OrderStatus.operator"].Criteria.(map[string]string)
	require.Equal(t, map[string]string{"=": "equal to", "!=": "not equal to"}, criteria)
}

func TestBuildQuestionsOffersOnlyDeclaredEnumValuesPlusNone(t *testing.T) {
	questions := BuildQuestions("failed flows", nil)
	criteria := questions["ExecutionStatus.value"].Criteria.(map[string]string)
	require.Contains(t, criteria, "Failed")
	require.Contains(t, criteria, "none")
	require.NotContains(t, criteria, "InProgress")
}

// TestBuildQuestionsGivesEveryEnumValueARealDescription guards against a
// criterion that just repeats its own name. A live calibration run found
// the model reliably chose self-describing values like "Failed" but missed
// less obvious ones like "TimedOut" when their only criterion was "TimedOut".
func TestBuildQuestionsGivesEveryEnumValueARealDescription(t *testing.T) {
	questions := BuildQuestions("failed flows", nil)
	criteria := questions["ExecutionStatus.value"].Criteria.(map[string]string)
	for value, description := range criteria {
		if value == "none" {
			continue
		}
		require.NotEqual(t, value, description, "criterion for %q must describe it, not repeat it", value)
	}
}

func TestCandidateLiteralsExtractsQuotedAndBareTokensWithoutDuplicates(t *testing.T) {
	literals := candidateLiterals(`flows for "Order Service" and OrderService and 42`)
	require.Contains(t, literals, "Order Service")
	require.Contains(t, literals, "OrderService")
	require.Contains(t, literals, "42")

	countAnd := 0
	for _, literal := range literals {
		if literal == "and" {
			countAnd++
		}
	}
	require.Equal(t, 1, countAnd, "duplicate literal tokens must be deduplicated")
}

func TestIsBuiltInFieldName(t *testing.T) {
	require.True(t, IsBuiltInFieldName("ExecutionStatus"))
	require.True(t, IsBuiltInFieldName("FlowType"))
	require.False(t, IsBuiltInFieldName("OrderStatus"))
}

func TestBuildQuestionsOffersLiteralsToKeywordFields(t *testing.T) {
	questions := BuildQuestions(`orders for "Order Service"`, []Field{
		{Name: "OrderStatus", Kind: FieldKindKeyword, Meaning: "the order's status"},
	})
	criteria := questions["OrderStatus.value"].Criteria.(map[string]string)
	require.Contains(t, criteria, "Order Service")
	require.Contains(t, criteria, "none")
}
