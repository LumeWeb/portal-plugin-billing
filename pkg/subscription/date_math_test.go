package subscription

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper to create a fixed time in UTC for consistent testing
func fixedTime(year int, month time.Month, day int, hour, min, sec, nsec int) time.Time {
	return time.Date(year, month, day, hour, min, sec, nsec, time.UTC)
}

func TestDaysBetween(t *testing.T) {
	tests := []struct {
		name     string
		start    time.Time
		end      time.Time
		expected int
	}{
		{
			name:     "same day",
			start:    fixedTime(2024, time.January, 15, 10, 0, 0, 0),
			end:      fixedTime(2024, time.January, 15, 18, 30, 0, 0),
			expected: 0,
		},
		{
			name:     "next day less than 24 hours",
			start:    fixedTime(2024, time.January, 15, 10, 0, 0, 0),
			end:      fixedTime(2024, time.January, 16, 5, 0, 0, 0),
			expected: 0,
		},
		{
			name:     "next day over 24 hours",
			start:    fixedTime(2024, time.January, 15, 10, 0, 0, 0),
			end:      fixedTime(2024, time.January, 16, 12, 0, 0, 0),
			expected: 1,
		},
		{
			name:     "multiple days",
			start:    fixedTime(2024, time.January, 1, 0, 0, 0, 0),
			end:      fixedTime(2024, time.January, 10, 0, 0, 0, 0),
			expected: 9,
		},
		{
			name:     "negative - end before start",
			start:    fixedTime(2024, time.January, 15, 10, 0, 0, 0),
			end:      fixedTime(2024, time.January, 10, 10, 0, 0, 0),
			expected: -5,
		},
		{
			name:     "year boundary less than 48 hours",
			start:    fixedTime(2023, time.December, 31, 20, 0, 0, 0),
			end:      fixedTime(2024, time.January, 2, 5, 0, 0, 0),
			expected: 1,
		},
		{
			name:     "leap year February 28 to March 1",
			start:    fixedTime(2024, time.February, 28, 0, 0, 0, 0),
			end:      fixedTime(2024, time.March, 1, 0, 0, 0, 0),
			expected: 2,
		},
		{
			name:     "exact 24 hours same day",
			start:    fixedTime(2024, time.January, 15, 10, 0, 0, 0),
			end:      fixedTime(2024, time.January, 16, 10, 0, 0, 0),
			expected: 1,
		},
}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DaysBetween(tt.start, tt.end)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEndOfDay(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Time
		expected time.Time
		location *time.Location
	}{
		{
			name:     "UTC",
			input:    fixedTime(2024, time.January, 15, 10, 30, 45, 123456),
			expected: time.Date(2024, time.January, 15, 23, 59, 59, 999999, time.UTC),
		},
		{
			name:     "start of day",
			input:    fixedTime(2024, time.January, 15, 0, 0, 0, 0),
			expected: time.Date(2024, time.January, 15, 23, 59, 59, 999999, time.UTC),
		},
		{
			name:     "leap year February 29",
			input:    fixedTime(2024, time.February, 29, 12, 0, 0, 0),
			expected: time.Date(2024, time.February, 29, 23, 59, 59, 999999, time.UTC),
		},
		{
			name:     "year end",
			input:    fixedTime(2023, time.December, 31, 23, 0, 0, 0),
			expected: time.Date(2023, time.December, 31, 23, 59, 59, 999999, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EndOfDay(tt.input)
			assert.Equal(t, tt.expected, result)
			assert.Equal(t, tt.input.Location(), result.Location(), "location should be preserved")
		})
	}
}

func TestStartOfDay(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Time
		expected time.Time
	}{
		{
			name:     "UTC mid day",
			input:    fixedTime(2024, time.January, 15, 10, 30, 45, 123456),
			expected: fixedTime(2024, time.January, 15, 0, 0, 0, 0),
		},
		{
			name:     "already start of day",
			input:    fixedTime(2024, time.January, 15, 0, 0, 0, 0),
			expected: fixedTime(2024, time.January, 15, 0, 0, 0, 0),
		},
		{
			name:     "leap year February 29",
			input:    fixedTime(2024, time.February, 29, 23, 59, 59, 999999),
			expected: fixedTime(2024, time.February, 29, 0, 0, 0, 0),
		},
		{
			name:     "year end",
			input:    fixedTime(2023, time.December, 31, 23, 59, 59, 999999),
			expected: fixedTime(2023, time.December, 31, 0, 0, 0, 0),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := StartOfDay(tt.input)
			assert.Equal(t, tt.expected, result)
			assert.Equal(t, tt.input.Location(), result.Location(), "location should be preserved")
		})
	}
}

func TestEndOfMonth(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Time
		expected time.Time
	}{
		{
			name:     "January",
			input:    fixedTime(2024, time.January, 15, 10, 0, 0, 0),
			expected: time.Date(2024, time.January, 31, 23, 59, 59, 999999, time.UTC),
		},
		{
			name:     "February non-leap",
			input:    fixedTime(2023, time.February, 15, 10, 0, 0, 0),
			expected: time.Date(2023, time.February, 28, 23, 59, 59, 999999, time.UTC),
		},
		{
			name:     "February leap year",
			input:    fixedTime(2024, time.February, 15, 10, 0, 0, 0),
			expected: time.Date(2024, time.February, 29, 23, 59, 59, 999999, time.UTC),
		},
		{
			name:     "April 30 days",
			input:    fixedTime(2024, time.April, 15, 10, 0, 0, 0),
			expected: time.Date(2024, time.April, 30, 23, 59, 59, 999999, time.UTC),
		},
		{
			name:     "December",
			input:    fixedTime(2024, time.December, 15, 10, 0, 0, 0),
			expected: time.Date(2024, time.December, 31, 23, 59, 59, 999999, time.UTC),
		},
		{
			name:     "first day of month",
			input:    fixedTime(2024, time.January, 1, 0, 0, 0, 0),
			expected: time.Date(2024, time.January, 31, 23, 59, 59, 999999, time.UTC),
		},
		{
			name:     "year boundary December",
			input:    fixedTime(2023, time.December, 15, 10, 0, 0, 0),
			expected: time.Date(2023, time.December, 31, 23, 59, 59, 999999, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EndOfMonth(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestStartOfMonth(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Time
		expected time.Time
	}{
		{
			name:     "mid month",
			input:    fixedTime(2024, time.January, 15, 10, 30, 0, 0),
			expected: fixedTime(2024, time.January, 1, 0, 0, 0, 0),
		},
		{
			name:     "first of month",
			input:    fixedTime(2024, time.January, 1, 10, 30, 0, 0),
			expected: fixedTime(2024, time.January, 1, 0, 0, 0, 0),
		},
		{
			name:     "leap year February",
			input:    fixedTime(2024, time.February, 29, 12, 0, 0, 0),
			expected: fixedTime(2024, time.February, 1, 0, 0, 0, 0),
		},
		{
			name:     "December",
			input:    fixedTime(2023, time.December, 25, 0, 0, 0, 0),
			expected: fixedTime(2023, time.December, 1, 0, 0, 0, 0),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := StartOfMonth(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAddMonths(t *testing.T) {
	tests := []struct {
		name         string
		base         time.Time
		months       int
		expected     time.Time
	}{
		{
			name:     "add 1 month",
			base:     fixedTime(2024, time.January, 15, 10, 30, 45, 123456),
			months:   1,
			expected: time.Date(2024, time.February, 15, 10, 30, 45, 123456, time.UTC),
		},
		{
			name:     "add 12 months",
			base:     fixedTime(2024, time.January, 15, 10, 30, 45, 123456),
			months:   12,
			expected: time.Date(2025, time.January, 15, 10, 30, 45, 123456, time.UTC),
		},
		{
			name:     "month rollover November + 2 months January",
			base:     fixedTime(2024, time.November, 15, 10, 0, 0, 0),
			months:   2,
			expected: time.Date(2025, time.January, 15, 10, 0, 0, 0, time.UTC),
		},
		{
			name:     "month rollover December + 1 month",
			base:     fixedTime(2024, time.December, 15, 10, 0, 0, 0),
			months:   1,
			expected: time.Date(2025, time.January, 15, 10, 0, 0, 0, time.UTC),
		},
		{
			name:     "leap year March to January next year",
			base:     fixedTime(2024, time.March, 1, 10, 0, 0, 0),
			months:   24,
			expected: time.Date(2026, time.March, 1, 10, 0, 0, 0, time.UTC),
		},
		{
			name:     "subtract months",
			base:     fixedTime(2024, time.March, 15, 10, 0, 0, 0),
			months:   -1,
			expected: time.Date(2024, time.February, 15, 10, 0, 0, 0, time.UTC),
		},
		{
			name:     "subtract to previous year",
			base:     fixedTime(2024, time.January, 15, 10, 0, 0, 0),
			months:   -1,
			expected: time.Date(2023, time.December, 15, 10, 0, 0, 0, time.UTC),
		},
		{
			name:     "add zero months",
			base:     fixedTime(2024, time.January, 15, 10, 0, 0, 0),
			months:   0,
			expected: time.Date(2024, time.January, 15, 10, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AddMonths(tt.base, tt.months)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCadenceAddTo(t *testing.T) {
	tests := []struct {
		name         string
		base         time.Time
		cadence      Cadence
		expectedTime time.Time
		expectError  bool
		errorMsg     string
	}{
		{
			name:         "monthly",
			base:         fixedTime(2024, time.January, 15, 10, 0, 0, 0),
			cadence:      CadenceMonthly,
			expectedTime: time.Date(2024, time.February, 15, 10, 0, 0, 0, time.UTC),
			expectError:  false,
		},
		{
			name:         "quarterly",
			base:         fixedTime(2024, time.January, 15, 10, 0, 0, 0),
			cadence:      CadenceQuarterly,
			expectedTime: time.Date(2024, time.April, 15, 10, 0, 0, 0, time.UTC),
			expectError:  false,
		},
		{
			name:         "yearly",
			base:         fixedTime(2024, time.January, 15, 10, 0, 0, 0),
			cadence:      CadenceYearly,
			expectedTime: time.Date(2025, time.January, 15, 10, 0, 0, 0, time.UTC),
			expectError:  false,
		},
		{
			name:         "weekly",
			base:         fixedTime(2024, time.January, 15, 10, 0, 0, 0),
			cadence:      CadenceWeekly,
			expectedTime: time.Date(2024, time.January, 22, 10, 0, 0, 0, time.UTC),
			expectError:  false,
		},
		{
			name:         "monthly November rollover",
			base:         fixedTime(2024, time.November, 30, 10, 0, 0, 0),
			cadence:      CadenceMonthly,
			expectedTime: time.Date(2024, time.December, 30, 10, 0, 0, 0, time.UTC),
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.cadence.AddTo(tt.base)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedTime, result)
		})
	}
}

func TestCadenceRolling_ConstantExists(t *testing.T) {
	// Test that CadenceRolling constant exists and has correct string value
	c := CadenceRolling
	assert.Equal(t, Cadence("rolling"), c, "CadenceRolling should equal 'rolling'")
}

func TestParseCadence(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    Cadence
		expectError bool
		errorMsg    string
	}{
		{
			name:     "monthly",
			input:    "monthly",
			expected: CadenceMonthly,
		},
		{
			name:     "quarterly",
			input:    "quarterly",
			expected: CadenceQuarterly,
		},
		{
			name:     "yearly",
			input:    "yearly",
			expected: CadenceYearly,
		},
		{
			name:     "weekly",
			input:    "weekly",
			expected: CadenceWeekly,
		},
		{
			name:     "rolling",
			input:    "rolling",
			expected: CadenceRolling,
		},
		{
			name:        "invalid cadence",
			input:       "invalid",
			expectError: true,
			errorMsg:    "invalid cadence: invalid",
		},
		{
			name:        "custom not supported",
			input:       "custom",
			expectError: true,
			errorMsg:    "invalid cadence: custom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseCadence(tt.input)
			if tt.expectError {
				require.Error(t, err)
				assert.Equal(t, tt.errorMsg, err.Error())
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestCadenceAddTo_RollingReturnsError(t *testing.T) {
	// Test that rolling cadence returns an error when trying to add without context
	base := fixedTime(2024, time.January, 15, 10, 0, 0, 0)
	
	result, err := CadenceRolling.AddTo(base)
	
	assert.Error(t, err, "AddTo with rolling cadence should return an error")
	assert.Equal(t, "rolling period requires rolling_days context", err.Error(), 
		"Error message should explain that rolling_days context is needed")
	assert.True(t, result.IsZero(), "Result should be zero time on error")
}

func TestIsSameDay(t *testing.T) {
	tests := []struct {
		name     string
		a        time.Time
		b        time.Time
		expected bool
	}{
		{
			name:     "same day different times",
			a:        fixedTime(2024, time.January, 15, 10, 0, 0, 0),
			b:        fixedTime(2024, time.January, 15, 23, 59, 59, 999999),
			expected: true,
		},
		{
			name:     "same day same time",
			a:        fixedTime(2024, time.January, 15, 10, 30, 0, 0),
			b:        fixedTime(2024, time.January, 15, 10, 30, 0, 0),
			expected: true,
		},
		{
			name:     "different day same month",
			a:        fixedTime(2024, time.January, 15, 10, 0, 0, 0),
			b:        fixedTime(2024, time.January, 16, 10, 0, 0, 0),
			expected: false,
		},
		{
			name:     "different month",
			a:        fixedTime(2024, time.January, 15, 10, 0, 0, 0),
			b:        fixedTime(2024, time.February, 15, 10, 0, 0, 0),
			expected: false,
		},
		{
			name:     "different year",
			a:        fixedTime(2024, time.January, 15, 10, 0, 0, 0),
			b:        fixedTime(2025, time.January, 15, 10, 0, 0, 0),
			expected: false,
		},
		{
			name:     "leap year February 29 same day",
			a:        fixedTime(2024, time.February, 29, 10, 0, 0, 0),
			b:        fixedTime(2024, time.February, 29, 15, 30, 0, 0),
			expected: true,
		},
		{
			name:     "start and end of same day",
			a:        fixedTime(2024, time.January, 15, 0, 0, 0, 0),
			b:        fixedTime(2024, time.January, 15, 23, 59, 59, 999999),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsSameDay(tt.a, tt.b)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestYearsInPeriod(t *testing.T) {
	tests := []struct {
		name     string
		start    time.Time
		end      time.Time
		expected int
	}{
		{
			name:     "same year",
			start:   fixedTime(2024, time.January, 15, 10, 0, 0, 0),
			end:     fixedTime(2024, time.December, 31, 23, 59, 59, 999999),
			expected: 0,
		},
		{
			name:     "one year apart",
			start:   fixedTime(2024, time.January, 1, 0, 0, 0, 0),
			end:     fixedTime(2025, time.January, 1, 0, 0, 0, 0),
			expected: 1,
		},
		{
			name:     "multiple years",
			start:   fixedTime(2020, time.January, 1, 0, 0, 0, 0),
			end:     fixedTime(2024, time.December, 31, 23, 59, 59, 999999),
			expected: 4,
		},
		{
			name:     "negative - end before start",
			start:   fixedTime(2024, time.January, 1, 0, 0, 0, 0),
			end:     fixedTime(2022, time.January, 1, 0, 0, 0, 0),
			expected: -2,
		},
		{
			name:     "leap year to next year",
			start:   fixedTime(2024, time.February, 29, 0, 0, 0, 0),
			end:     fixedTime(2025, time.February, 28, 23, 59, 59, 999999),
			expected: 1,
		},
		{
			name:     "year boundary December to January",
			start:   fixedTime(2023, time.December, 31, 23, 59, 59, 999999),
			end:     fixedTime(2024, time.January, 1, 0, 0, 0, 0),
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := YearsInPeriod(tt.start, tt.end)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMonthsInPeriod(t *testing.T) {
	tests := []struct {
		name     string
		start    time.Time
		end      time.Time
		expected int
	}{
		{
			name:     "same month",
			start:   fixedTime(2024, time.January, 1, 0, 0, 0, 0),
			end:     fixedTime(2024, time.January, 31, 23, 59, 59, 999999),
			expected: 0,
		},
		{
			name:     "one month apart",
			start:   fixedTime(2024, time.January, 15, 10, 0, 0, 0),
			end:     fixedTime(2024, time.February, 15, 10, 0, 0, 0),
			expected: 1,
		},
		{
			name:     "multiple months same year",
			start:   fixedTime(2024, time.January, 1, 0, 0, 0, 0),
			end:     fixedTime(2024, time.June, 30, 23, 59, 59, 999999),
			expected: 5,
		},
		{
			name:     "year rollover December to January",
			start:   fixedTime(2023, time.December, 15, 10, 0, 0, 0),
			end:     fixedTime(2024, time.January, 15, 10, 0, 0, 0),
			expected: 1,
		},
		{
			name:     "across years",
			start:   fixedTime(2023, time.January, 15, 10, 0, 0, 0),
			end:     fixedTime(2024, time.January, 15, 10, 0, 0, 0),
			expected: 12,
		},
		{
			name:     "negative - end before start",
			start:   fixedTime(2024, time.June, 15, 10, 0, 0, 0),
			end:     fixedTime(2024, time.January, 15, 10, 0, 0, 0),
			expected: -5,
		},
		{
			name:     "multiple years",
			start:   fixedTime(2022, time.January, 1, 0, 0, 0, 0),
			end:     fixedTime(2024, time.January, 1, 0, 0, 0, 0),
			expected: 24,
		},
		{
			name:     "leap year February",
			start:   fixedTime(2024, time.February, 1, 0, 0, 0, 0),
			end:     fixedTime(2024, time.March, 1, 0, 0, 0, 0),
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MonthsInPeriod(tt.start, tt.end)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDaysInMonth(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Time
		expected int
	}{
		{
			name:     "January 31 days",
			input:    fixedTime(2024, time.January, 15, 10, 0, 0, 0),
			expected: 31,
		},
		{
			name:     "February 28 days non-leap",
			input:    fixedTime(2023, time.February, 10, 0, 0, 0, 0),
			expected: 28,
		},
		{
			name:     "February 29 days leap year 2024",
			input:    fixedTime(2024, time.February, 15, 10, 0, 0, 0),
			expected: 29,
		},
		{
			name:     "February 28 days non-leap 2023",
			input:    fixedTime(2023, time.February, 15, 10, 0, 0, 0),
			expected: 28,
		},
		{
			name:     "March 31 days",
			input:    fixedTime(2024, time.March, 10, 0, 0, 0, 0),
			expected: 31,
		},
		{
			name:     "April 30 days",
			input:    fixedTime(2024, time.April, 10, 0, 0, 0, 0),
			expected: 30,
		},
		{
			name:     "May 31 days",
			input:    fixedTime(2024, time.May, 10, 0, 0, 0, 0),
			expected: 31,
		},
		{
			name:     "June 30 days",
			input:    fixedTime(2024, time.June, 10, 0, 0, 0, 0),
			expected: 30,
		},
		{
			name:     "July 31 days",
			input:    fixedTime(2024, time.July, 10, 0, 0, 0, 0),
			expected: 31,
		},
		{
			name:     "August 31 days",
			input:    fixedTime(2024, time.August, 10, 0, 0, 0, 0),
			expected: 31,
		},
		{
			name:     "September 30 days",
			input:    fixedTime(2024, time.September, 10, 0, 0, 0, 0),
			expected: 30,
		},
		{
			name:     "October 31 days",
			input:    fixedTime(2024, time.October, 10, 0, 0, 0, 0),
			expected: 31,
		},
		{
			name:     "November 30 days",
			input:    fixedTime(2024, time.November, 10, 0, 0, 0, 0),
			expected: 30,
		},
		{
			name:     "December 31 days",
			input:    fixedTime(2024, time.December, 10, 0, 0, 0, 0),
			expected: 31,
		},
		{
			name:     "century leap year 2000",
			input:    fixedTime(2000, time.February, 1, 0, 0, 0, 0),
			expected: 29,
		},
		{
			name:     "century non-leap 2100",
			input:    fixedTime(2100, time.February, 1, 0, 0, 0, 0),
			expected: 28,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DaysInMonth(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
