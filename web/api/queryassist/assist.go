// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package queryassist

import (
	"context"
	"fmt"
	"time"

	"github.com/superdurable/dex/service/common/typesafe"
)

// requestState is the JSON shape sent to TypeSafe as state, following
// https://docs.typesafe.ai/concepts/state's guidance to use named fields
// rather than one flat string, so instructions can address `literals`
// distinctly from `request`.
type requestState struct {
	Request  string   `json:"request"`
	Literals []string `json:"literals"`
}

// Interpret turns a natural-language Flow search request into the Filters
// the console's Basic editor should populate. customFields lists every
// indexed Attribute the caller wants TypeSafe to consider beyond the
// built-in fields; Interpret never considers a field customFields does not
// list. now anchors time-window arithmetic and is normally time.Now().
func Interpret(ctx context.Context, client *typesafe.Client, request string, customFields []Field, now time.Time) (Result, error) {
	questions := BuildQuestions(request, customFields)
	state := requestState{Request: request, Literals: candidateLiterals(request)}

	response, err := client.Ask(ctx, state, questions)
	if err != nil {
		return Result{}, fmt.Errorf("queryassist: ask TypeSafe: %w", err)
	}
	return Compose(response, customFields, now)
}
