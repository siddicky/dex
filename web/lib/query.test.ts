// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { describe, expect, it } from 'vitest';
import { buildVisibilityQuery, fieldQuotingFromSampleValue, parseVisibilityQuery } from './query';

describe('visibility query', () => {
  it('builds the basic filters used by the search page', () => {
    expect(buildVisibilityQuery([
      { id: '1', field: 'ExecutionStatus', operator: '=', value: 'Running' },
      { id: '2', field: 'FlowType', operator: '=', value: 'Checkout' },
    ])).toBe('ExecutionStatus = "Running" AND FlowType = "Checkout"');
  });

  it('round-trips simple advanced queries into basic mode', () => {
    const query = 'WorkflowId = "order-42" AND StartTime >= "2026-07-01T00:00:00Z"';
    expect(buildVisibilityQuery(parseVisibilityQuery(query) ?? [])).toBe(query);
  });

  it('keeps unsupported advanced syntax in advanced mode', () => {
    expect(parseVisibilityQuery('ExecutionStatus IN ("Running", "Failed")')).toBeNull();
  });

  it('quotes a numeric-looking value for a KEYWORD custom field instead of guessing from its shape', () => {
    // OrderId is a KEYWORD attribute whose values happen to be numeric strings.
    // Without a declared quoting, the safe default is quoted, not a shape guess.
    expect(buildVisibilityQuery([
      { id: '1', field: 'OrderId', operator: '=', value: '12345' },
    ])).toBe('OrderId = "12345"');
  });

  it('leaves a custom field unquoted only when its declared quoting says so', () => {
    expect(buildVisibilityQuery(
      [{ id: '1', field: 'RetryCount', operator: '>', value: '3' }],
      { RetryCount: 'unquoted' },
    )).toBe('RetryCount > 3');
  });

  it('never unquotes a built-in field even if declared quoting says unquoted', () => {
    expect(buildVisibilityQuery(
      [{ id: '1', field: 'WorkflowId', operator: '=', value: '12345' }],
      { WorkflowId: 'unquoted' },
    )).toBe('WorkflowId = "12345"');
  });

  it('quotes an unquoted-declared field whose value is not actually numeric', () => {
    expect(buildVisibilityQuery(
      [{ id: '1', field: 'RetryCount', operator: '=', value: 'not-a-number' }],
      { RetryCount: 'unquoted' },
    )).toBe('RetryCount = "not-a-number"');
  });
});

describe('fieldQuotingFromSampleValue', () => {
  it('infers unquoted only for a numeric sample value', () => {
    expect(fieldQuotingFromSampleValue(42)).toBe('unquoted');
    expect(fieldQuotingFromSampleValue('42')).toBe('quoted');
    expect(fieldQuotingFromSampleValue(true)).toBe('quoted');
    expect(fieldQuotingFromSampleValue(null)).toBe('quoted');
  });
});
