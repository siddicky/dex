// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Package queryassist turns a natural-language Flow search request into the
// filters the console's Basic search editor already knows how to run. It
// never invents a field name, operator, or value: every candidate is
// supplied by code, and TypeSafe (https://docs.typesafe.ai) only ever
// selects among them. See queryassist.Interpret for the entry point.
package queryassist

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/superdurable/dex/service/common/typesafe"
)

// FieldKind identifies the shape of a candidate field's value, which decides
// which operators are valid and how the field's value is asked for.
type FieldKind string

const (
	FieldKindKeyword FieldKind = "keyword"
	FieldKindNumber  FieldKind = "number"
	FieldKindBoolean FieldKind = "boolean"
	FieldKindEnum    FieldKind = "enum"
)

// ParseFieldKind validates a field kind supplied over the wire. FieldKindEnum
// is deliberately not accepted here: an enum's closed value set is known only
// for the built-in fields this package defines, never for a caller-supplied
// custom attribute.
func ParseFieldKind(value string) (FieldKind, error) {
	switch FieldKind(value) {
	case FieldKindKeyword, FieldKindNumber, FieldKindBoolean:
		return FieldKind(value), nil
	default:
		return "", fmt.Errorf("queryassist: unknown field kind %q", value)
	}
}

// Field is one candidate the request may constrain. Name must be a real
// Dex visibility query field: a built-in (ExecutionStatus, WorkflowId,
// RunId, FlowType) or an indexed Attribute name the caller has already
// observed. Compose rejects any answer naming a field outside the list it
// was given, so supplying every real field here is what keeps TypeSafe from
// ever producing a filter on a field that does not exist.
type Field struct {
	Name    string
	Meaning string
	Kind    FieldKind
	Values  []string // populated only when Kind is FieldKindEnum
}

// timeField names the two built-in timestamp fields, handled separately from
// Field because they are asked about once as a pair (which field, which
// window) rather than per-field like every other candidate.
const (
	timeFieldStartTime = "StartTime"
	timeFieldCloseTime = "CloseTime"
	timeFieldNone      = "none"
)

// timeWindows enumerates every named window TypeSafe may choose. Compose's
// resolveWindow implements exactly this set; the two must stay in sync.
var timeWindows = map[string]string{
	"none":               "the request does not name a time window",
	"last_hour":          "within the last hour",
	"last_24_hours":      "within the last 24 hours",
	"today":              "since midnight today, in the server's local time",
	"yesterday":          "the full previous calendar day, in the server's local time",
	"last_7_days":        "within the last 7 days",
	"last_30_days":       "within the last 30 days",
	"older_than_30_days": "more than 30 days ago",
}

// builtInFields mirrors web/lib/query.ts's builtInFields and quotedFields:
// the visibility query fields every Dex Flow search understands regardless
// of application-defined Attributes.
var builtInFields = []Field{
	{
		Name: "ExecutionStatus", Kind: FieldKindEnum,
		Meaning: "the run's lifecycle status",
		Values:  []string{"Running", "Completed", "Failed", "Canceled", "Terminated", "ContinuedAsNew", "TimedOut"},
	},
	{Name: "WorkflowId", Kind: FieldKindKeyword, Meaning: "the Flow ID"},
	{Name: "RunId", Kind: FieldKindKeyword, Meaning: "the run ID"},
	{Name: "FlowType", Kind: FieldKindKeyword, Meaning: "the application's Flow type name"},
}

// literalPattern finds candidate literal spans in a natural-language
// request: quoted phrases first, then bare identifier- or number-shaped
// tokens. TypeSafe selects among these; it never emits a value this pattern
// did not first surface.
var literalPattern = regexp.MustCompile(`"[^"]*"|'[^']*'|[A-Za-z0-9][A-Za-z0-9_.:-]*`)

// candidateLiterals extracts the deduplicated, order-preserving literal
// spans BuildQuestions offers TypeSafe to choose among for keyword- and
// number-kind fields.
func candidateLiterals(request string) []string {
	matches := literalPattern.FindAllString(request, -1)
	seen := make(map[string]struct{}, len(matches))
	literals := make([]string, 0, len(matches))
	for _, match := range matches {
		trimmed := strings.Trim(match, `"'`)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		literals = append(literals, trimmed)
	}
	return literals
}

// BuildQuestions returns every question Interpret sends to TypeSafe in one
// batch for the given request and customFields, alongside the built-in
// fields. Every question is asked speculatively; Compose reads only the
// answers a field's own constrained Noul clears the threshold for.
func BuildQuestions(request string, customFields []Field) map[string]typesafe.Question {
	fields := allFields(customFields)
	literals := candidateLiterals(request)

	questions := make(map[string]typesafe.Question, len(fields)*3+2)
	for _, field := range fields {
		questions[constrainedQuestionID(field.Name)] = typesafe.NoulQuestion(
			fmt.Sprintf("Does `request` state a constraint on %s (%s)?", field.Name, field.Meaning), nil)
		questions[operatorQuestionID(field.Name)] = typesafe.ChoiceQuestion(
			fmt.Sprintf("Which comparison does `request` use for %s, if it constrains it?", field.Name),
			operatorCriteria(field.Kind))
		questions[valueQuestionID(field.Name)] = valueQuestion(field, literals)
	}
	questions[timeFieldQuestionID] = typesafe.ChoiceQuestion(
		"Which timestamp does `request` constrain, if any?",
		map[string]string{
			timeFieldStartTime: "when the run started",
			timeFieldCloseTime: "when the run finished",
			timeFieldNone:      "the request names no timestamp constraint",
		})
	questions[timeWindowQuestionID] = typesafe.ChoiceQuestion(
		"Which named time window does `request` mean, if it names a timestamp constraint?", timeWindows)
	return questions
}

const (
	timeFieldQuestionID  = "time.field"
	timeWindowQuestionID = "time.window"
)

func allFields(customFields []Field) []Field {
	fields := make([]Field, 0, len(builtInFields)+len(customFields))
	fields = append(fields, builtInFields...)
	fields = append(fields, customFields...)
	return fields
}

// IsBuiltInFieldName reports whether name is one of the built-in fields
// BuildQuestions always asks about. Callers accepting custom fields from an
// untrusted request must reject a name for which this returns true: a
// colliding name would silently overwrite the built-in field's questions.
func IsBuiltInFieldName(name string) bool {
	for _, field := range builtInFields {
		if field.Name == name {
			return true
		}
	}
	return false
}

func constrainedQuestionID(field string) string { return field + ".constrained" }
func operatorQuestionID(field string) string    { return field + ".operator" }
func valueQuestionID(field string) string       { return field + ".value" }

func valueQuestion(field Field, literals []string) typesafe.Question {
	if field.Kind == FieldKindEnum {
		return typesafe.ChoiceQuestion(
			fmt.Sprintf("Which value of %s does `request` name, if it constrains it?", field.Name),
			withNone(valuesToCriteria(field.Values)))
	}
	return typesafe.ChoiceQuestion(
		fmt.Sprintf("Which literal from `literals` does `request` intend for %s, if it constrains it?", field.Name),
		withNone(literalsToCriteria(literals)))
}

func operatorCriteria(kind FieldKind) map[string]string {
	if kind == FieldKindNumber {
		return map[string]string{
			"=": "equal to", "!=": "not equal to",
			">": "greater than", ">=": "greater than or equal to",
			"<": "less than", "<=": "less than or equal to",
		}
	}
	return map[string]string{"=": "equal to", "!=": "not equal to"}
}

func valuesToCriteria(values []string) map[string]string {
	criteria := make(map[string]string, len(values))
	for _, value := range values {
		criteria[value] = value
	}
	return criteria
}

func literalsToCriteria(literals []string) map[string]string {
	criteria := make(map[string]string, len(literals))
	for _, literal := range literals {
		criteria[literal] = fmt.Sprintf("the literal text %q from the request", literal)
	}
	return criteria
}

func withNone(criteria map[string]string) map[string]string {
	criteria["none"] = "no listed option fits"
	return criteria
}
