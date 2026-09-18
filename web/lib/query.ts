// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

export type QueryOperator = '=' | '!=' | '>' | '>=' | '<' | '<=';

export interface BasicFilter {
  id: string;
  field: string;
  operator: QueryOperator;
  value: string;
}

const quotedFields = new Set([
  'ExecutionStatus',
  'WorkflowId',
  'RunId',
  'FlowType',
  'StartTime',
  'CloseTime',
]);

/**
 * Whether an indexed Attribute's visibility-query values are written
 * unquoted (an INT or DOUBLE index) or quoted (everything else, including
 * KEYWORD, BOOL, and DATETIME). Callers derive this from the Attribute's
 * declared IndexType, or from an observed sample value's JSON type; they
 * never guess it from the shape of a query-editor input string, which is
 * indistinguishable between "the KEYWORD value happens to be numeric" and
 * "this is actually a numeric index."
 */
export type FieldQuoting = 'quoted' | 'unquoted';

/**
 * fieldQuotingFromSampleValue infers a custom field's FieldQuoting from one
 * already-typed value the server returned for it, e.g. an entry from a
 * FlowExecution's indexedAttributes. The server decodes that value using the
 * Attribute's actual IndexType (see server/service/common/index), so its
 * JSON type is a real signal, unlike the text a user types into a filter.
 */
export function fieldQuotingFromSampleValue(value: unknown): FieldQuoting {
  return typeof value === 'number' ? 'unquoted' : 'quoted';
}

export function escapeQueryValue(value: string): string {
  return value.replaceAll('\\', '\\\\').replaceAll('"', '\\"');
}

/**
 * buildVisibilityQuery renders filters as a Dex visibility query string.
 * fieldQuoting supplies each custom field's FieldQuoting, keyed by field
 * name; a field absent from it (including every built-in, which quotedFields
 * already covers) defaults to quoted, the only choice that is always valid
 * for a KEYWORD, BOOL, or DATETIME index. A field is never unquoted merely
 * because its current value happens to look numeric.
 */
export function buildVisibilityQuery(
  filters: BasicFilter[],
  fieldQuoting: Record<string, FieldQuoting> = {},
): string {
  return filters
    .filter((filter) => filter.field.trim() && filter.value.trim())
    .map((filter) => {
      const field = filter.field.trim();
      const value = filter.value.trim();
      const isNumericLiteral = /^-?\d+(\.\d+)?$/.test(value);
      const unquoted = !quotedFields.has(field)
        && fieldQuoting[field] === 'unquoted'
        && isNumericLiteral;
      const encoded = unquoted ? value : `"${escapeQueryValue(value)}"`;
      return `${field} ${filter.operator} ${encoded}`;
    })
    .join(' AND ');
}

export function parseVisibilityQuery(query: string): BasicFilter[] | null {
  if (!query.trim()) return [];
  const clauses = query.split(/\s+AND\s+/i);
  const filters: BasicFilter[] = [];
  for (const [index, clause] of clauses.entries()) {
    const match = clause.match(
      /^\s*([A-Za-z_][A-Za-z0-9_]*)\s*(=|!=|>=|<=|>|<)\s*(?:"((?:\\.|[^"])*)"|(-?\d+(?:\.\d+)?))\s*$/,
    );
    if (!match) return null;
    filters.push({
      id: `parsed-${index}`,
      field: match[1],
      operator: match[2] as QueryOperator,
      value: match[3] !== undefined
        ? match[3].replaceAll('\\"', '"').replaceAll('\\\\', '\\')
        : match[4],
    });
  }
  return filters;
}
