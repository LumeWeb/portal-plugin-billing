package subscription

import (
	"testing"

	"github.com/stretchr/testify/assert"
)



func TestCadence_StringBehavior(t *testing.T) {
	// Test that Cadence behaves as expected as a string
	tests := []struct {
		name     string
		cadence  Cadence
		expected string
	}{
		{
			name:     "daily string",
			cadence:  CadenceDaily,
			expected: "daily",
		},
		{
			name:     "weekly string",
			cadence:  CadenceWeekly,
			expected: "weekly",
		},
		{
			name:     "monthly string",
			cadence:  CadenceMonthly,
			expected: "monthly",
		},
		{
			name:     "quarterly string",
			cadence:  CadenceQuarterly,
			expected: "quarterly",
		},
		{
			name:     "yearly string",
			cadence:  CadenceYearly,
			expected: "yearly",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, string(tt.cadence))
		})
	}
}
