// Legacy Materials in this file remain under their original licenses.
// See LEGACY_NOTICES.md.

// Modifications Copyright (c) 2026 Super Durable, Inc.
//
// Modifications after the Legacy Cutoff are licensed under the
// Sustainable Use License 1.0.
// Legacy Materials remain under their original licenses.
// See LICENSE and LEGACY_NOTICES.md.

package timeparser

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseTimeRFC3339Nano(t *testing.T) {
	dateTime := time.Date(2026, 7, 30, 10, 0, 0, 123456789, time.UTC)

	parsed, err := ParseTime(dateTime.Format(time.RFC3339Nano))
	require.NoError(t, err)
	require.Equal(t, dateTime.UnixNano(), parsed)
}

func TestParseTimeRawUnixNano(t *testing.T) {
	parsed, err := ParseTime("20240101")
	require.NoError(t, err)
	require.Equal(t, int64(20240101), parsed)

	_, err = ParseRFC3339Nano("20240101")
	require.Error(t, err)
}

func TestParseTimeRangeShortAndLongForm(t *testing.T) {
	before := time.Now()
	parsed, err := ParseTime("3d")
	require.NoError(t, err)
	require.WithinDuration(t, before.Add(-3*24*time.Hour), time.Unix(0, parsed), time.Minute)

	parsed, err = ParseTime("3day")
	require.NoError(t, err)
	require.WithinDuration(t, before.Add(-3*24*time.Hour), time.Unix(0, parsed), time.Minute)
}

// TestParseTimeRangeRejectsUnknownUnit guards against the range validation
// silently accepting a value neither the short-form nor long-form grammar
// describes. The check must actually reject, not just discard a match result.
func TestParseTimeRangeRejectsUnknownUnit(t *testing.T) {
	_, err := ParseTime("3days")
	require.Error(t, err)
	require.ErrorContains(t, err, "cannot parse timeRange")
}

func TestParseTimeErrorMessageNamesTheOffendingInput(t *testing.T) {
	_, err := ParseTime("not-a-time")
	require.Error(t, err)
	require.ErrorContains(t, err, "cannot parse time 'not-a-time'")
	require.ErrorContains(t, err, DateTimeFormat)
}
