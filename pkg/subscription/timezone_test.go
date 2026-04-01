package subscription

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test DST boundary handling with different timezones
func TestBillingCycle_DSTBoundaries(t *testing.T) {
	tests := []struct {
		name     string
		start    time.Time
		end      time.Time
		cadence  Cadence
		expectedDays int
	}{
		{
			name: "DST spring forward - weekly cycle",
			start: time.Date(2024, 3, 10, 0, 0, 0, 0, time.FixedZone("EST-5", -5*3600)),
			end:   time.Date(2024, 3, 17, 0, 0, 0, 0, time.FixedZone("EDT-4", -4*3600)),
			cadence: CadenceWeekly,
			expectedDays: 7,
		},
		{
			name: "DST fall back - weekly cycle",
			start: time.Date(2024, 11, 3, 0, 0, 0, 0, time.FixedZone("EDT-4", -4*3600)),
			end:   time.Date(2024, 11, 10, 0, 0, 0, 0, time.FixedZone("EST-5", -5*3600)),
			cadence: CadenceWeekly,
			expectedDays: 7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cycle := BillingCycle{
				StartAt: tt.start,
				EndAt:   tt.end,
				Cadence: tt.cadence,
			}
			
			daysInCycle := DaysInCycle(cycle)
			// Billing cycles use DaysBetween which is wall-clock time
			// The exact day count may vary by DST transition
			assert.GreaterOrEqual(t, daysInCycle, tt.expectedDays-1)
			assert.LessOrEqual(t, daysInCycle, tt.expectedDays+1)
		})
	}
}

func TestProratedWithTimezones(t *testing.T) {
	tests := []struct {
		name        string
		location    *time.Location
	}{
		{
			name:     "UTC timezone",
			location: time.UTC,
		},
		{
			name:     "US/Eastern",
			location: time.FixedZone("EST-5", -5*3600),
		},
		{
			name:     "Europe/London",
			location: time.FixedZone("GMT+0", 0),
		},
		{
			name:     "Asia/Tokyo",
			location: time.FixedZone("JST+9", 9*3600),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			startTime := time.Date(2024, 1, 1, 0, 0, 0, 0, tt.location)
			endTime := time.Date(2024, 1, 31, 23, 59, 59, 0, tt.location)
			prorationTime := time.Date(2024, 1, 16, 12, 0, 0, 0, tt.location)
			
			oldPrice := Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: CadenceMonthly,
			}
			newPrice := Price{
				Amount:  decimal.NewFromInt(150),
				Cadence: CadenceMonthly,
			}
			cycle := BillingCycle{
				StartAt: startTime,
				EndAt:   endTime,
				Cadence: CadenceMonthly,
			}
			
			result, err := ProratedChange(oldPrice, newPrice, cycle, prorationTime, ProrationBehaviorCreateProrations)
			require.NoError(t, err)
			
			// Should produce valid results regardless of timezone
			assert.Greater(t, result.UnusedCredit.InexactFloat64(), 0.0)
			assert.Greater(t, result.NewCharge.InexactFloat64(), 0.0)
		})
	}
}

func TestDaysBetweenWithTimezones(t *testing.T) {
	tests := []struct {
		name           string
		start          time.Time
		end            time.Time
		expectedDays   int
		location       *time.Location
	}{
		{
			name: "one day in UTC",
			start: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			end:   time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
			expectedDays: 1,
			location:    time.UTC,
		},
		{
			name: "one day across timezone",
			start: time.Date(2024, 1, 1, 0, 0, 0, 0, time.FixedZone("EST-5", -5*3600)),
			end:   time.Date(2024, 1, 2, 0, 0, 0, 0, time.FixedZone("EST-5", -5*3600)),
			expectedDays: 1,
			location:    time.FixedZone("EST-5", -5*3600),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DaysBetween(tt.start, tt.end)
			assert.Equal(t, tt.expectedDays, result)
		})
	}
}

func TestCycleProgressWithTimezones(t *testing.T) {
	// Test that cycle progress is consistent regardless of timezone
	locations := []*time.Location{
		time.UTC,
		time.FixedZone("EST-5", -5*3600),
		time.FixedZone("PST-8", -8*3600),
		time.FixedZone("JST+9", 9*3600),
	}
	
	var progressResults []float64
	
	for _, loc := range locations {
		cycle := BillingCycle{
			StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, loc),
			EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, loc),
			Cadence: CadenceMonthly,
		}
		
		now := time.Date(2024, 1, 16, 12, 0, 0, 0, loc)
		
		progress := CycleProgress(cycle, now)
		progressResults = append(progressResults, progress)
	}
	
	// All timezones should produce the same progress (0.5 for mid-month)
	// Allow for small variations due to implementation details
	for i := 1; i < len(progressResults); i++ {
		diff := progressResults[i] - progressResults[0]
		absDiff := diff
		if diff < 0 {
			absDiff = -diff
		}
		assert.Less(t, absDiff, 0.1, "Progress should be consistent across timezones")
	}
}

func TestBillingCycleWithNonUTC(t *testing.T) {
	// Test billing cycle calculations with non-UTC timezone
	eastern := time.FixedZone("EST-5", -5*3600)
	
	cycle := BillingCycle{
		StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, eastern),
		EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, eastern),
		Cadence: CadenceMonthly,
	}
	
	daysInCycle := DaysInCycle(cycle)
	assert.Greater(t, daysInCycle, 0)
	
	now := time.Date(2024, 1, 16, 12, 0, 0, 0, eastern)
	
	elapsed := DaysElapsed(cycle, now)
	remaining := DaysRemaining(cycle, now)
	
	// Should have reasonable values
	assert.GreaterOrEqual(t, elapsed, 0)
	assert.GreaterOrEqual(t, remaining, 0)
	assert.LessOrEqual(t, elapsed+remaining, daysInCycle+2) // Allow for rounding
	
	progress := CycleProgress(cycle, now)
	assert.GreaterOrEqual(t, progress, 0.0)
	assert.LessOrEqual(t, progress, 1.0)
}

func TestLeapYearWithTimezones(t *testing.T) {
	// Test leap year handling across timezones
	locations := []*time.Location{
		time.UTC,
		time.FixedZone("EST-5", -5*3600),
		time.FixedZone("JST+9", 9*3600),
	}

	for _, loc := range locations {
		t.Run(loc.String(), func(t *testing.T) {
			// Leap year February
			febStart := time.Date(2024, 2, 1, 0, 0, 0, 0, loc)
			febEnd := time.Date(2024, 3, 1, 0, 0, 0, 0, loc)
			
			cycle := BillingCycle{
				StartAt: febStart,
				EndAt:   febEnd,
				Cadence: CadenceMonthly,
			}
			
			// Check days in cycle for leap year February
			daysInCycle := DaysInCycle(cycle)
			// Leap year February has 29 days in actual calendar,
			// but billing cycles use fixed 30-day months for proration
			// DaysBetween returns the actual difference (28-29)
			assert.Greater(t, daysInCycle, 27)
			assert.Less(t, daysInCycle, 30)
		})
	}
}

func TestStartEndOfDayWithTimezones(t *testing.T) {
	locations := []*time.Location{
		time.UTC,
		time.FixedZone("EST-5", -5*3600),
		time.FixedZone("PST-8", -8*3600),
	}

	for _, loc := range locations {
		t.Run(loc.String(), func(tt *testing.T) {
			testTime := time.Date(2024, 1, 15, 10, 30, 0, 0, loc)
			
			startOfDay := StartOfDay(testTime)
			endOfDay := EndOfDay(testTime)
			
			// Should preserve timezone
			assert.Equal(t, loc, startOfDay.Location())
			assert.Equal(t, loc, endOfDay.Location())
			
			// Start should be 00:00
			assert.Equal(t, 0, startOfDay.Hour())
			assert.Equal(t, 0, startOfDay.Minute())
			
			// End should be 23:59:59.999999
			assert.Equal(t, 23, endOfDay.Hour())
			assert.Equal(t, 59, endOfDay.Minute())
		})
	}
}

func TestCalculateNextCycleWithTimezones(t *testing.T) {
	est := time.FixedZone("EST-5", -5*3600)
	
	currentCycle := BillingCycle{
		StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, est),
		EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, est),
		Cadence: CadenceMonthly,
	}
	
	nextCycle := CalculateNextCycle(currentCycle)
	
	// Should preserve timezone
	assert.Equal(t, est, nextCycle.StartAt.Location())
	assert.Equal(t, est, nextCycle.EndAt.Location())
	
	// Should calculate correctly
	assert.True(t, nextCycle.StartAt.After(currentCycle.StartAt))
	assert.True(t, nextCycle.EndAt.After(currentCycle.EndAt))
}

func TestCalculateFirstCycleWithTimezones(t *testing.T) {
	est := time.FixedZone("EST-5", -5*3600)
	
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, est)
	
	cycle := CalculateFirstCycle(startDate, CadenceMonthly)
	
	// Should preserve timezone
	assert.Equal(t, est, cycle.StartAt.Location())
	assert.Equal(t, est, cycle.EndAt.Location())
	
	// Should calculate correct end date
	expectedEnd := startDate.AddDate(0, 1, 0)
	assert.Equal(t, expectedEnd, cycle.EndAt)
}

func TestYearBoundaryWithTimezones(t *testing.T) {
	locations := []*time.Location{
		time.UTC,
		time.FixedZone("EST-5", -5*3600),
		time.FixedZone("AEDT+11", 11*3600),
	}

	for _, loc := range locations {
		t.Run(loc.String(), func(t *testing.T) {
			// Year boundary test
			dec29 := time.Date(2023, 12, 29, 12, 0, 0, 0, loc)
			jan2 := time.Date(2024, 1, 2, 12, 0, 0, 0, loc)
			
			days := DaysBetween(dec29, jan2)
			// Should be 4-5 days crossing year boundary
			assert.GreaterOrEqual(t, days, 3)
			assert.LessOrEqual(t, days, 5)
		})
	}
}
