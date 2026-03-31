package subscription

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDaysInCycle(t *testing.T) {
	tests := []struct {
		name           string
		cycle          BillingCycle
		expectedDays   int
	}{
		{
			name: "monthly cycle - January 2024",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			expectedDays: 30,
		},
		{
			name: "February leap year 2024",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 2, 29, 23, 59, 59, 0, time.UTC),
			},
			expectedDays: 28,
		},
		{
			name: "February non-leap year 2023",
			cycle: BillingCycle{
				StartAt: time.Date(2023, 2, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2023, 2, 28, 23, 59, 59, 0, time.UTC),
			},
			expectedDays: 27,
		},
		{
			name: "quarterly cycle - Q1 2024",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 3, 31, 23, 59, 59, 0, time.UTC),
			},
			expectedDays: 90,
		},
		{
			name: "yearly cycle - 2024",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC),
			},
			expectedDays: 365,
		},
		{
			name: "weekly cycle - 7 days",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 7, 23, 59, 59, 0, time.UTC),
			},
			expectedDays: 6,
		},
		{
			name: "single day cycle",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			},
			expectedDays: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DaysInCycle(tt.cycle)
			assert.Equal(t, tt.expectedDays, result)
		})
	}
}

func TestDaysRemaining(t *testing.T) {
	tests := []struct {
		name              string
		cycle             BillingCycle
		now               time.Time
		expectedRemaining int
	}{
		{
			name: "start of cycle",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now:               time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expectedRemaining: 30,
		},
		{
			name: "middle of month",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now:               time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
			expectedRemaining: 16,
		},
		{
			name: "end of cycle",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now:               time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			expectedRemaining: 0,
		},
		{
			name: "after cycle ends - negative",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now:               time.Date(2024, 2, 5, 0, 0, 0, 0, time.UTC),
			expectedRemaining: -4,
		},
		{
			name: "before cycle starts - positive",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now:               time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC),
			expectedRemaining: 26,
		},
		{
			name: "weekly cycle",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 7, 23, 59, 59, 0, time.UTC),
			},
			now:               time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC),
			expectedRemaining: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DaysRemaining(tt.cycle, tt.now)
			assert.Equal(t, tt.expectedRemaining, result)
		})
	}
}

func TestDaysElapsed(t *testing.T) {
	tests := []struct {
		name             string
		cycle            BillingCycle
		now              time.Time
		expectedElapsed  int
	}{
		{
			name: "start of cycle",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now:             time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expectedElapsed: 0,
		},
		{
			name: "middle of month",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now:             time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
			expectedElapsed: 14,
		},
		{
			name: "end of cycle",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now:             time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			expectedElapsed: 30,
		},
		{
			name: "after cycle ends - more than total",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now:             time.Date(2024, 2, 10, 0, 0, 0, 0, time.UTC),
			expectedElapsed: 40,
		},
		{
			name: "before cycle starts - negative",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now:             time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC),
			expectedElapsed: -5,
		},
		{
			name: "yearly cycle elapsed",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC),
			},
			now:             time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC),
			expectedElapsed: 182,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DaysElapsed(tt.cycle, tt.now)
			assert.Equal(t, tt.expectedElapsed, result)
		})
	}
}

func TestCycleProgress(t *testing.T) {
	tests := []struct {
		name                  string
		cycle                 BillingCycle
		now                   time.Time
		expectedProgress      float64
	}{
		{
			name: "before cycle start",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now:                   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expectedProgress:      0,
		},
		{
			name: "exactly at cycle start",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now:                   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expectedProgress:      0,
		},
		{
			name: "50% progress - middle of month",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now:                   time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC),
			expectedProgress:      15.0 / 30.0,
		},
		{
			name: "exactly at cycle end",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now:                   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			expectedProgress:      1,
		},
		{
			name: "after cycle end",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now:                   time.Date(2024, 2, 5, 0, 0, 0, 0, time.UTC),
			expectedProgress:      1,
		},
		{
			name: "25% progress - weekly",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 7, 23, 59, 59, 0, time.UTC),
			},
			now:                   time.Date(2024, 1, 2, 6, 0, 0, 0, time.UTC),
			expectedProgress:      1.0 / 6.0,
		},
		{
			name: "quarterly cycle progress",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 3, 31, 23, 59, 59, 0, time.UTC),
			},
			now:                   time.Date(2024, 2, 15, 0, 0, 0, 0, time.UTC),
			expectedProgress:      45.0 / 90.0,
		},
		{
			name: "zero duration cycle - after cycle end",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			},
			now:                   time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
			expectedProgress:      0, // Zero-duration cycle is invalid - returns 0
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CycleProgress(tt.cycle, tt.now)
			assert.InDelta(t, tt.expectedProgress, result, 0.001)
		})
	}
}

func TestCycleContainsTime(t *testing.T) {
	tests := []struct {
		name     string
		cycle    BillingCycle
		t        time.Time
		expected bool
	}{
		{
			name: "time within cycle",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			t:        time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
			expected: true,
		},
		{
			name: "exactly at cycle start",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			t:        time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expected: true,
		},
		{
			name: "exactly at cycle end",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			t:        time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			expected: true,
		},
		{
			name: "before cycle start",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			t:        time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC),
			expected: false,
		},
		{
			name: "after cycle end",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			t:        time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
			expected: false,
		},
		{
			name: "weekly cycle - within",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 7, 23, 59, 59, 0, time.UTC),
			},
			t:        time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC),
			expected: true,
		},
		{
			name: "adjacent cycles - boundary overlap",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			t:        time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CycleContainsTime(tt.cycle, tt.t)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestOverlapsCycle(t *testing.T) {
	tests := []struct {
		name     string
		a        BillingCycle
		b        BillingCycle
		expected bool
	}{
		{
			name: "complete overlap",
			a: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			b: BillingCycle{
				StartAt: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 2, 15, 23, 59, 59, 0, time.UTC),
			},
			expected: true,
		},
		{
			name: "partial overlap",
			a: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 15, 23, 59, 59, 0, time.UTC),
			},
			b: BillingCycle{
				StartAt: time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			expected: true,
		},
		{
			name: "adjacent cycles - touch at boundary",
			a: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			b: BillingCycle{
				StartAt: time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 2, 28, 23, 59, 59, 0, time.UTC),
			},
			expected: true,
		},
		{
			name: "no overlap - separated",
			a: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 15, 23, 59, 59, 0, time.UTC),
			},
			b: BillingCycle{
				StartAt: time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			expected: false,
		},
		{
			name: "cycle contained within another",
			a: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC),
			},
			b: BillingCycle{
				StartAt: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 6, 30, 23, 59, 59, 0, time.UTC),
			},
			expected: true,
		},
		{
			name: "identical cycles",
			a: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			b: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			expected: true,
		},
		{
			name: "multi-year overlap",
			a: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2025, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			b: BillingCycle{
				StartAt: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 8, 31, 23, 59, 59, 0, time.UTC),
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := OverlapsCycle(tt.a, tt.b)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCalculateNextRenewal(t *testing.T) {
	tests := []struct {
		name              string
		cycle             BillingCycle
		expectedRenewal   time.Time
	}{
		{
			name: "monthly cycle renewal",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			expectedRenewal: time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
		},
		{
			name: "yearly cycle renewal",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC),
			},
			expectedRenewal: time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC),
		},
		{
			name: "weekly cycle renewal",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 7, 23, 59, 59, 0, time.UTC),
			},
			expectedRenewal: time.Date(2024, 1, 7, 23, 59, 59, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateNextRenewal(tt.cycle)
			assert.Equal(t, tt.expectedRenewal, result)
		})
	}
}

func TestCalculateFirstCycle(t *testing.T) {
	tests := []struct {
		name              string
		startDate         time.Time
		cadence           Cadence
		expectedCycle     BillingCycle
		expectZeroCycle   bool
	}{
		{
			name:      "monthly cycle",
			startDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			cadence:   CadenceMonthly,
			expectedCycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
				Cadence: CadenceMonthly,
			},
			expectZeroCycle: false,
		},
		{
			name:      "yearly cycle",
			startDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			cadence:   CadenceYearly,
			expectedCycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
				Cadence: CadenceYearly,
			},
			expectZeroCycle: false,
		},
		{
			name:      "quarterly cycle",
			startDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			cadence:   CadenceQuarterly,
			expectedCycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC),
				Cadence: CadenceQuarterly,
			},
			expectZeroCycle: false,
		},
		{
			name:      "weekly cycle",
			startDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			cadence:   CadenceWeekly,
			expectedCycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 8, 0, 0, 0, 0, time.UTC),
				Cadence: CadenceWeekly,
			},
			expectZeroCycle: false,
		},
		{
			name:            "unsupported cadence",
			startDate:       time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			cadence:         "custom",
			expectZeroCycle: true,
		},
		{
			name:            "invalid cadence",
			startDate:       time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			cadence:         "unknown",
			expectZeroCycle: true,
		},
		{
			name:      "monthly cycle - February leap year",
			startDate: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
			cadence:   CadenceMonthly,
			expectedCycle: BillingCycle{
				StartAt: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
				Cadence: CadenceMonthly,
			},
			expectZeroCycle: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateFirstCycle(tt.startDate, tt.cadence)
			
			if tt.expectZeroCycle {
				assert.Equal(t, BillingCycle{}, result)
			} else {
				assert.Equal(t, tt.expectedCycle.StartAt, result.StartAt)
				assert.Equal(t, tt.expectedCycle.EndAt, result.EndAt)
				assert.Equal(t, tt.expectedCycle.Cadence, result.Cadence)
			}
		})
	}
}

func TestCalculateNextCycle(t *testing.T) {
	tests := []struct {
		name              string
		currentCycle      BillingCycle
		expectedNextCycle BillingCycle
		expectZeroCycle   bool
	}{
		{
			name: "monthly next cycle - January to February",
			currentCycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
				Cadence: CadenceMonthly,
			},
			expectedNextCycle: BillingCycle{
				StartAt: time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
				EndAt:   time.Date(2024, 3, 2, 23, 59, 59, 0, time.UTC),
				Cadence: CadenceMonthly,
			},
			expectZeroCycle: false,
		},
		{
			name: "yearly next cycle",
			currentCycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC),
				Cadence: CadenceYearly,
			},
			expectedNextCycle: BillingCycle{
				StartAt: time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC),
				EndAt:   time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC),
				Cadence: CadenceYearly,
			},
			expectZeroCycle: false,
		},
		{
			name: "quarterly next cycle - Q1 to Q2",
			currentCycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 3, 31, 23, 59, 59, 0, time.UTC),
				Cadence: CadenceQuarterly,
			},
			expectedNextCycle: BillingCycle{
				StartAt: time.Date(2024, 3, 31, 23, 59, 59, 0, time.UTC),
				EndAt:   time.Date(2024, 7, 1, 23, 59, 59, 0, time.UTC),
				Cadence: CadenceQuarterly,
			},
			expectZeroCycle: false,
		},
		{
			name: "weekly next cycle",
			currentCycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 7, 23, 59, 59, 0, time.UTC),
				Cadence: CadenceWeekly,
			},
			expectedNextCycle: BillingCycle{
				StartAt: time.Date(2024, 1, 7, 23, 59, 59, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 14, 23, 59, 59, 0, time.UTC),
				Cadence: CadenceWeekly,
			},
			expectZeroCycle: false,
		},
		{
			name: "custom cadence returns zero cycle",
			currentCycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
				Cadence: "custom",
			},
			expectZeroCycle: true,
		},
		{
			name: "invalid cadence returns zero cycle",
			currentCycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
				Cadence: "invalid",
			},
			expectZeroCycle: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateNextCycle(tt.currentCycle)
			
			if tt.expectZeroCycle {
				assert.Equal(t, BillingCycle{}, result)
			} else {
				assert.Equal(t, tt.expectedNextCycle.StartAt, result.StartAt)
				assert.Equal(t, tt.expectedNextCycle.EndAt, result.EndAt)
				assert.Equal(t, tt.expectedNextCycle.Cadence, result.Cadence)
			}
		})
	}
}

func TestCycleAtDate(t *testing.T) {
	tests := []struct {
		name         string
		cycle        BillingCycle
		t            time.Time
		expected     CycleDate
	}{
		{
			name: "monthly cycle at start",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			t: time.Date(2024, 1, 5, 12, 0, 0, 0, time.UTC),
			expected: CycleDate{
				Date: time.Date(2024, 1, 5, 12, 0, 0, 0, time.UTC),
				Cycle: BillingCycle{
					StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
					EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
				},
				DaysElapsed:   4,
				DaysRemaining: 26,
				Progress:      4.0 / 30.0,
			},
		},
		{
			name: "quarterly cycle at middle",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 3, 31, 23, 59, 59, 0, time.UTC),
			},
			t: time.Date(2024, 2, 15, 0, 0, 0, 0, time.UTC),
			expected: CycleDate{
				Date: time.Date(2024, 2, 15, 0, 0, 0, 0, time.UTC),
				Cycle: BillingCycle{
					StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
					EndAt:   time.Date(2024, 3, 31, 23, 59, 59, 0, time.UTC),
				},
				DaysElapsed:   45,
				DaysRemaining: 45,
				Progress:      45.0 / 90.0,
			},
		},
		{
			name: "weekly cycle at end",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 7, 23, 59, 59, 0, time.UTC),
			},
			t: time.Date(2024, 1, 7, 23, 59, 59, 0, time.UTC),
			expected: CycleDate{
				Date: time.Date(2024, 1, 7, 23, 59, 59, 0, time.UTC),
				Cycle: BillingCycle{
					StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
					EndAt:   time.Date(2024, 1, 7, 23, 59, 59, 0, time.UTC),
				},
				DaysElapsed:   6,
				DaysRemaining: 0,
				Progress:      1,
			},
		},
		{
			name: "yearly cycle at quarter",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC),
			},
			t: time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC),
			expected: CycleDate{
				Date: time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC),
				Cycle: BillingCycle{
					StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
					EndAt:   time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC),
				},
				DaysElapsed:   91,
				DaysRemaining: 274,
				Progress:      91.0 / 365.0,
			},
		},
		{
			name: "before cycle starts",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			t: time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC),
			expected: CycleDate{
				Date: time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC),
				Cycle: BillingCycle{
					StartAt: time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC),
					EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
				},
				DaysElapsed:   -5,
				DaysRemaining: 26,
				Progress:      0,
			},
		},
		{
			name: "after cycle ends",
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			t: time.Date(2024, 2, 5, 0, 0, 0, 0, time.UTC),
			expected: CycleDate{
				Date: time.Date(2024, 2, 5, 0, 0, 0, 0, time.UTC),
				Cycle: BillingCycle{
					StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
					EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
				},
				DaysElapsed:   35,
				DaysRemaining: -4,
				Progress:      1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CycleAtDate(tt.cycle, tt.t)
			assert.Equal(t, tt.expected.Date, result.Date)
			assert.Equal(t, tt.expected.Cycle.StartAt, result.Cycle.StartAt)
			assert.Equal(t, tt.expected.Cycle.EndAt, result.Cycle.EndAt)
			assert.Equal(t, tt.expected.DaysElapsed, result.DaysElapsed)
			assert.Equal(t, tt.expected.DaysRemaining, result.DaysRemaining)
			assert.InDelta(t, tt.expected.Progress, result.Progress, 0.001)
		})
	}
}

func TestBillingCycleIntegration(t *testing.T) {
	t.Run("calculate sequence of monthly cycles", func(t *testing.T) {
		startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		
		firstCycle := CalculateFirstCycle(startDate, CadenceMonthly)
		
		assert.False(t, firstCycle.StartAt.IsZero())
		assert.False(t, firstCycle.EndAt.IsZero())
		
		secondCycle := CalculateNextCycle(firstCycle)
		
		assert.Equal(t, firstCycle.EndAt, secondCycle.StartAt)
		assert.Equal(t, CadenceMonthly, secondCycle.Cadence)
		
		thirdCycle := CalculateNextCycle(secondCycle)
		
		assert.Equal(t, secondCycle.EndAt, thirdCycle.StartAt)
		assert.Equal(t, CadenceMonthly, thirdCycle.Cadence)
	})
	
	t.Run("calculate sequence of quarterly cycles", func(t *testing.T) {
		startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		
		firstCycle := CalculateFirstCycle(startDate, CadenceQuarterly)
		
		assert.Equal(t, startDate, firstCycle.StartAt)
		assert.Greater(t, firstCycle.EndAt, firstCycle.StartAt)
		
		secondCycle := CalculateNextCycle(firstCycle)
		
		assert.Equal(t, firstCycle.EndAt, secondCycle.StartAt)
		assert.Equal(t, CadenceQuarterly, secondCycle.Cadence)
	})
	
	t.Run("monthly cycle with progress tracking", func(t *testing.T) {
		cycle := BillingCycle{
			StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
		}
		
		totalDays := DaysInCycle(cycle)
		assert.Equal(t, 30, totalDays)
		
		assert.Equal(t, 30, DaysRemaining(cycle, cycle.StartAt))
		assert.Equal(t, 0, DaysElapsed(cycle, cycle.StartAt))
		assert.Equal(t, 0.0, CycleProgress(cycle, cycle.StartAt))
		
		assert.Equal(t, 15, DaysRemaining(cycle, time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC)))
		assert.Equal(t, 15, DaysElapsed(cycle, time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC)))
	})
}
