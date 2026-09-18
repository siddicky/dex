// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package typesafe

import "fmt"

// QuestionType identifies which of TypeSafe's three primitives a Question
// asks: Choice, Score, or Noul. See https://docs.typesafe.ai/primitives.
type QuestionType string

const (
	QuestionTypeChoice QuestionType = "choice"
	QuestionTypeScore  QuestionType = "score"
	QuestionTypeNoul   QuestionType = "noul"
)

// Question is one named judgment sent to TypeSafe alongside shared state.
// Build one with ChoiceQuestion, ScoreQuestion, or NoulQuestion rather than
// constructing it directly, so Criteria always matches Type.
type Question struct {
	Type         QuestionType `json:"type"`
	Instructions string       `json:"instructions"`
	Criteria     any          `json:"criteria,omitempty"`
}

// ChoiceQuestion asks TypeSafe to select exactly one mutually exclusive
// option. criteria maps each option's key to a description of what it means;
// the answer's Choice value is always one of those keys. Use this only when
// the options are mutually exclusive: several independently applicable
// options need one NoulQuestion each instead.
func ChoiceQuestion(instructions string, criteria map[string]string) Question {
	return Question{Type: QuestionTypeChoice, Instructions: instructions, Criteria: criteria}
}

// ScoreQuestion asks TypeSafe to position instructions along an ordered
// spectrum described by levels, from lowest to highest. levels must have at
// least two entries; each entry describes what that point on the spectrum
// means.
func ScoreQuestion(instructions string, levels []string) Question {
	return Question{Type: QuestionTypeScore, Instructions: instructions, Criteria: levels}
}

// NoulQuestion asks TypeSafe for the probability that instructions is true.
// criteria is optional and, when given, must have exactly the keys "true"
// and "false" clarifying what each answer means for this question.
func NoulQuestion(instructions string, criteria map[string]string) Question {
	question := Question{Type: QuestionTypeNoul, Instructions: instructions}
	if len(criteria) > 0 {
		question.Criteria = criteria
	}
	return question
}

// Answer is one named judgment TypeSafe returned. Its populated fields
// depend on Type: Choice and Confidence for QuestionTypeChoice; Score,
// Legend, and Confidence for QuestionTypeScore; Noul for QuestionTypeNoul,
// which never carries a Confidence.
type Answer struct {
	Type          QuestionType       `json:"type"`
	Choice        string             `json:"choice,omitempty"`
	Score         float64            `json:"score,omitempty"`
	Noul          float64            `json:"noul,omitempty"`
	Legend        map[string]string  `json:"legend,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Confidence    *float64           `json:"confidence,omitempty"`
}

// Usage reports token consumption for one Ask call.
type Usage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

// Response is the full result of one Ask call: every requested question's
// Answer, keyed by the same names passed to Ask, plus token Usage.
type Response struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
	Usage   Usage             `json:"usage"`
}

// Noul returns the named answer's probability that its statement is true.
// It errors if the question is missing or was not a NoulQuestion, so a typo
// in a question name fails loudly instead of silently returning 0.
func (r *Response) Noul(question string) (float64, error) {
	answer, err := r.answer(question, QuestionTypeNoul)
	if err != nil {
		return 0, err
	}
	return answer.Noul, nil
}

// Choice returns the named answer's selected option key and TypeSafe's
// confidence in that selection. It errors if the question is missing or was
// not a ChoiceQuestion.
func (r *Response) Choice(question string) (choice string, confidence float64, err error) {
	answer, err := r.answer(question, QuestionTypeChoice)
	if err != nil {
		return "", 0, err
	}
	return answer.Choice, confidenceOrZero(answer.Confidence), nil
}

// Score returns the named answer's position along its levels and TypeSafe's
// confidence in that position. It errors if the question is missing or was
// not a ScoreQuestion.
func (r *Response) Score(question string) (score float64, confidence float64, err error) {
	answer, err := r.answer(question, QuestionTypeScore)
	if err != nil {
		return 0, 0, err
	}
	return answer.Score, confidenceOrZero(answer.Confidence), nil
}

// WeakestConfidence returns the minimum Confidence across the named Choice
// and Score answers, ignoring Noul answers, which carry none. It follows
// TypeSafe's own function-calling guidance: report the weakest judgment
// used, not a product of probabilities, so a call with more parts is never
// penalized just for having more parts. It errors if any named question is
// missing.
func (r *Response) WeakestConfidence(questions ...string) (float64, error) {
	weakest := 1.0
	found := false
	for _, question := range questions {
		answer, ok := r.Answers[question]
		if !ok {
			return 0, fmt.Errorf("typesafe: no answer for question %q", question)
		}
		if answer.Confidence == nil {
			continue
		}
		found = true
		if *answer.Confidence < weakest {
			weakest = *answer.Confidence
		}
	}
	if !found {
		return 0, fmt.Errorf("typesafe: none of the given questions carry a confidence")
	}
	return weakest, nil
}

func (r *Response) answer(question string, expectedType QuestionType) (Answer, error) {
	answer, ok := r.Answers[question]
	if !ok {
		return Answer{}, fmt.Errorf("typesafe: no answer for question %q", question)
	}
	if answer.Type != expectedType {
		return Answer{}, fmt.Errorf("typesafe: question %q answered as %q, expected %q", question, answer.Type, expectedType)
	}
	return answer, nil
}

func confidenceOrZero(confidence *float64) float64 {
	if confidence == nil {
		return 0
	}
	return *confidence
}
