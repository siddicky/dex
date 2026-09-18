// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package queryassist

import (
	"fmt"
	"time"

	"github.com/superdurable/dex/service/common/typesafe"
)

const (
	// constrainedThreshold is the minimum Noul probability for treating a
	// field as constrained at all. Below it, the field is left out of the
	// result the same as if TypeSafe had said no.
	constrainedThreshold = 0.6
	// reviewThreshold is the minimum weakest confidence across the filters
	// actually produced for Result.NeedsReview to be false. Below it, the
	// console must show the filters for a human to check before running them.
	reviewThreshold = 0.7
)

// Filter mirrors web/lib/query.ts's BasicFilter shape byte for byte, so the
// console can drop Result.Filters directly into its existing filter chips.
type Filter struct {
	ID       string `json:"id"`
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
}

// Result is what Interpret returns to the console for one natural-language
// search request.
type Result struct {
	Filters     []Filter `json:"filters"`
	Confidence  float64  `json:"confidence"`
	NeedsReview bool     `json:"needsReview"`
}

// Compose turns a TypeSafe Response for the given fields into the Filters a
// search should run. now anchors time-window arithmetic, which Compose does
// entirely in code — TypeSafe only ever names a window, per
// https://docs.typesafe.ai/model-jaggedness/jev-1.13 ("Jev is not a
// calculator"). It errors only when TypeSafe answered a question outside
// what BuildQuestions asked for the same fields, which means the two have
// drifted out of sync; a low-confidence or empty result is not an error.
func Compose(response *typesafe.Response, customFields []Field, now time.Time) (Result, error) {
	fields := allFields(customFields)

	var filters []Filter
	var usedQuestions []string
	for _, field := range fields {
		fieldFilters, fieldQuestions, err := composeField(response, field)
		if err != nil {
			return Result{}, err
		}
		filters = append(filters, fieldFilters...)
		usedQuestions = append(usedQuestions, fieldQuestions...)
	}

	timeFilters, timeQuestions, err := composeTimeFilters(response, now)
	if err != nil {
		return Result{}, err
	}
	filters = append(filters, timeFilters...)
	usedQuestions = append(usedQuestions, timeQuestions...)

	if len(filters) == 0 {
		return Result{NeedsReview: true}, nil
	}

	confidence, err := response.WeakestConfidence(usedQuestions...)
	if err != nil {
		// Every used question here is a Choice, which always carries a
		// confidence, so this means Compose and BuildQuestions have drifted.
		return Result{}, fmt.Errorf("queryassist: compute confidence: %w", err)
	}
	return Result{
		Filters:     filters,
		Confidence:  confidence,
		NeedsReview: confidence < reviewThreshold,
	}, nil
}

func composeField(response *typesafe.Response, field Field) ([]Filter, []string, error) {
	constrained, err := response.Noul(constrainedQuestionID(field.Name))
	if err != nil {
		return nil, nil, err
	}
	if constrained < constrainedThreshold {
		return nil, nil, nil
	}

	operator, _, err := response.Choice(operatorQuestionID(field.Name))
	if err != nil {
		return nil, nil, err
	}
	if !validOperator(operator, field.Kind) {
		return nil, nil, fmt.Errorf("queryassist: field %q got operator %q, which BuildQuestions never offered it", field.Name, operator)
	}

	value, _, err := response.Choice(valueQuestionID(field.Name))
	if err != nil {
		return nil, nil, err
	}
	if value == "none" {
		return nil, nil, nil
	}
	if field.Kind == FieldKindEnum && !containsString(field.Values, value) {
		return nil, nil, fmt.Errorf("queryassist: field %q got value %q, which BuildQuestions never offered it", field.Name, value)
	}

	filter := Filter{ID: filterID(field.Name, "value"), Field: field.Name, Operator: operator, Value: value}
	questions := []string{operatorQuestionID(field.Name), valueQuestionID(field.Name)}
	return []Filter{filter}, questions, nil
}

func composeTimeFilters(response *typesafe.Response, now time.Time) ([]Filter, []string, error) {
	field, _, err := response.Choice(timeFieldQuestionID)
	if err != nil {
		return nil, nil, err
	}
	if field == timeFieldNone {
		return nil, nil, nil
	}
	if field != timeFieldStartTime && field != timeFieldCloseTime {
		return nil, nil, fmt.Errorf("queryassist: time.field got %q, which BuildQuestions never offered", field)
	}

	window, _, err := response.Choice(timeWindowQuestionID)
	if err != nil {
		return nil, nil, err
	}
	windowRange, ok := resolveWindow(window, now)
	if !ok {
		return nil, nil, fmt.Errorf("queryassist: time.window got %q, which BuildQuestions never offered", window)
	}

	var filters []Filter
	if windowRange.from != nil {
		filters = append(filters, Filter{
			ID: filterID(field, "from"), Field: field, Operator: ">=", Value: formatTime(*windowRange.from),
		})
	}
	if windowRange.to != nil {
		filters = append(filters, Filter{
			ID: filterID(field, "to"), Field: field, Operator: "<", Value: formatTime(*windowRange.to),
		})
	}
	if len(filters) == 0 {
		return nil, nil, nil // window == "none"
	}
	return filters, []string{timeFieldQuestionID, timeWindowQuestionID}, nil
}

// windowRange is the absolute [from, to) bound a named time window resolves
// to. Either bound may be open (nil): "last_hour" has no upper bound, and
// "older_than_30_days" has no lower bound.
type windowRange struct {
	from *time.Time
	to   *time.Time
}

// resolveWindow implements exactly the window names timeWindows offers
// TypeSafe. "today" and "yesterday" use now's own location, i.e. the
// server's local time zone, matching their descriptions in timeWindows.
func resolveWindow(window string, now time.Time) (windowRange, bool) {
	switch window {
	case "none":
		return windowRange{}, true
	case "last_hour":
		from := now.Add(-time.Hour)
		return windowRange{from: &from}, true
	case "last_24_hours":
		from := now.Add(-24 * time.Hour)
		return windowRange{from: &from}, true
	case "today":
		from := midnight(now)
		return windowRange{from: &from}, true
	case "yesterday":
		todayMidnight := midnight(now)
		yesterdayMidnight := todayMidnight.AddDate(0, 0, -1)
		return windowRange{from: &yesterdayMidnight, to: &todayMidnight}, true
	case "last_7_days":
		from := now.AddDate(0, 0, -7)
		return windowRange{from: &from}, true
	case "last_30_days":
		from := now.AddDate(0, 0, -30)
		return windowRange{from: &from}, true
	case "older_than_30_days":
		to := now.AddDate(0, 0, -30)
		return windowRange{to: &to}, true
	default:
		return windowRange{}, false
	}
}

func midnight(t time.Time) time.Time {
	year, month, day := t.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, t.Location())
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func filterID(field string, suffix string) string {
	return "ask:" + field + ":" + suffix
}

func validOperator(operator string, kind FieldKind) bool {
	_, ok := operatorCriteria(kind)[operator]
	return ok
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
