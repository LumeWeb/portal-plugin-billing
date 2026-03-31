package subscription

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSequentialProrationChanges(t *testing.T) {
	// Test making multiple proration changes within a single cycle
	basePrice := Price{
		Amount:  decimal.NewFromInt(100),
		Cadence: CadenceMonthly,
	}
	targetPrice := Price{
		Amount:  decimal.NewFromInt(200),
		Cadence: CadenceMonthly,
	}
	cycle := BillingCycle{
		StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
		Cadence:  CadenceMonthly,
	}

	// First change at 1/3 of cycle
	proration1Time := time.Date(2024, 1, 11, 0, 0, 0, 0, time.UTC)
	result1, err := ProratedChange(basePrice, targetPrice, cycle, proration1Time, ProrationBehaviorCreateProrations)
	require.NoError(t, err)
	
	// Second change at 2/3 of cycle (from new price to another)
	proration2Time := time.Date(2024, 1, 21, 0, 0, 0, 0, time.UTC)
	targetPrice2 := Price{
		Amount:  decimal.NewFromInt(150),
		Cadence: CadenceMonthly,
	}
	result2, err := ProratedChange(targetPrice, targetPrice2, cycle, proration2Time, ProrationBehaviorCreateProrations)
	require.NoError(t, err)
	
	// Both should produce valid results
	assert.Greater(t, result1.UnusedCredit.InexactFloat64(), 0.0)
	assert.Greater(t, result1.NewCharge.InexactFloat64(), 0.0)
	assert.Greater(t, result2.UnusedCredit.InexactFloat64(), 0.0)
	assert.Greater(t, result2.NewCharge.InexactFloat64(), 0.0)
}

func TestMultiYearBillingSequence(t *testing.T) {
	// Test calculating billing cycles across multiple years
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	
	// Create sequence of 3 annual cycles
	cycle1 := CalculateFirstCycle(startDate, CadenceYearly)
	assert.Equal(t, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), cycle1.StartAt)
	assert.Equal(t, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), cycle1.EndAt)
	
	cycle2 := CalculateNextCycle(cycle1)
	assert.Equal(t, cycle1.EndAt, cycle2.StartAt)
	assert.Equal(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), cycle2.EndAt)
	
	cycle3 := CalculateNextCycle(cycle2)
	assert.Equal(t, cycle2.EndAt, cycle3.StartAt)
	assert.Equal(t, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), cycle3.EndAt)
	
	// Verify all cycles are exactly 365 days apart (or 366 for leap year)
	assert.Equal(t, CadenceYearly, cycle1.Cadence)
	assert.Equal(t, CadenceYearly, cycle2.Cadence)
	assert.Equal(t, CadenceYearly, cycle3.Cadence)
}

func TestLeapYearProration(t *testing.T) {
	// Test proration during leap year
	leapYearStart := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)
	leapYearEnd := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	cycle := BillingCycle{
		StartAt: leapYearStart,
		EndAt:   leapYearEnd,
		Cadence: CadenceMonthly,
	}
	
	oldPrice := Price{
		Amount:  decimal.NewFromInt(100),
		Cadence: CadenceMonthly,
	}
	newPrice := Price{
		Amount:  decimal.NewFromInt(150),
		Cadence: CadenceMonthly,
	}
	
	// Proration at midpoint of leap year February
	prorationTime := time.Date(2024, 2, 15, 0, 0, 0, 0, time.UTC)
	
	result, err := ProratedChange(oldPrice, newPrice, cycle, prorationTime, ProrationBehaviorCreateProrations)
	require.NoError(t, err)
	
	// Should calculate correctly regardless of actual calendar days
	assert.Greater(t, result.UnusedCredit.InexactFloat64(), 0.0)
	assert.Greater(t, result.NewCharge.InexactFloat64(), 0.0)
	
	// Verify using fixed 30-day billing period
	expectedCreditPerDay := oldPrice.Amount.Div(decimal.NewFromInt(30))
	expectedChargePerDay := newPrice.Amount.Div(decimal.NewFromInt(30))
	
	// Should be based on remaining days, not actual calendar
	actualUnusedCredit := result.UnusedCredit.Div(expectedCreditPerDay)
	actualNewCharge := result.NewCharge.Div(expectedChargePerDay)
	
	// Both should be approximately equal (same remaining days)
	assert.InDelta(t, actualUnusedCredit.InexactFloat64(), actualNewCharge.InexactFloat64(), 0.01)
}

func TestYearBoundaryProration(t *testing.T) {
	// Test proration that crosses year boundary
	cycle := BillingCycle{
		StartAt: time.Date(2023, 12, 1, 0, 0, 0, 0, time.UTC),
		EndAt:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		Cadence: CadenceMonthly,
	}
	
	oldPrice := Price{
		Amount:  decimal.NewFromInt(100),
		Cadence: CadenceMonthly,
	}
	newPrice := Price{
		Amount:  decimal.NewFromInt(150),
		Cadence: CadenceMonthly,
	}
	
	// Proration exactly at year boundary
	prorationTime := time.Date(2023, 12, 31, 0, 0, 0, 0, time.UTC)
	
	result, err := ProratedChange(oldPrice, newPrice, cycle, prorationTime, ProrationBehaviorCreateProrations)
	require.NoError(t, err)
	
	// Should handle year boundary correctly
	assert.Greater(t, result.UnusedCredit.InexactFloat64(), 0.0)
	assert.Greater(t, result.NewCharge.InexactFloat64(), 0.0)
}

func TestMultiMonthQuarterlySequence(t *testing.T) {
	// Test sequence of quarterly cycles
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	
	// Q1
	q1 := CalculateFirstCycle(startDate, CadenceQuarterly)
	assert.Equal(t, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), q1.StartAt)
	assert.Equal(t, time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC), q1.EndAt)
	
	// Q2
	q2 := CalculateNextCycle(q1)
	assert.Equal(t, q1.EndAt, q2.StartAt)
	assert.Equal(t, time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC), q2.EndAt)
	
	// Q3
	q3 := CalculateNextCycle(q2)
	assert.Equal(t, q2.EndAt, q3.StartAt)
	assert.Equal(t, time.Date(2024, 10, 1, 0, 0, 0, 0, time.UTC), q3.EndAt)
	
	// Q4
	q4 := CalculateNextCycle(q3)
	assert.Equal(t, q3.EndAt, q4.StartAt)
	assert.Equal(t, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), q4.EndAt)
	
	// Verify cadences
	assert.Equal(t, CadenceQuarterly, q1.Cadence)
	assert.Equal(t, CadenceQuarterly, q2.Cadence)
	assert.Equal(t, CadenceQuarterly, q3.Cadence)
	assert.Equal(t, CadenceQuarterly, q4.Cadence)
}

func TestBillingCycleProgressIntegration(t *testing.T) {
	// Test progress tracking across multiple cycle dates
	cycle := BillingCycle{
		StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
		Cadence: CadenceMonthly,
	}

	// Progress at various points
	testDates := []struct {
		date     time.Time
		expected float64
	}{
		{
			date:     time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expected: 0.0,
		},
		{
			date:     time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC),
			expected: 0.5,
		},
		{
			date:     time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			expected: 1.0,
		},
	}

	for _, tt := range testDates {
		t.Run(tt.date.Format("2006-01-02"), func(t *testing.T) {
			cycleDate := CycleAtDate(cycle, tt.date)
			assert.InDelta(t, tt.expected, cycleDate.Progress, 0.01)
		})
	}
}

func TestProrationAcrossCycleRenewals(t *testing.T) {
	// Test proration at different points across cycle renewals
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	
	// First cycle
	cycle1 := CalculateFirstCycle(startDate, CadenceMonthly)
	assert.Equal(t, CadenceMonthly, cycle1.Cadence)
	
	// Proration in middle of first cycle
	midCycle1 := time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC)
	oldPrice := Price{
		Amount:  decimal.NewFromInt(100),
		Cadence: CadenceMonthly,
	}
	newPrice := Price{
		Amount:  decimal.NewFromInt(150),
		Cadence: CadenceMonthly,
	}
	
	result1, err := ProratedChange(oldPrice, newPrice, cycle1, midCycle1, ProrationBehaviorCreateProrations)
	require.NoError(t, err)
	assert.Greater(t, result1.UnusedCredit.Add(result1.NewCharge).InexactFloat64(), 0.0)
	
	// Second cycle
	cycle2 := CalculateNextCycle(cycle1)
	assert.Equal(t, CadenceMonthly, cycle2.Cadence)
	
	// Proration in middle of second cycle
	midCycle2 := time.Date(2024, 2, 15, 0, 0, 0, 0, time.UTC)
	
	result2, err := ProratedChange(oldPrice, newPrice, cycle2, midCycle2, ProrationBehaviorCreateProrations)
	require.NoError(t, err)
	assert.Greater(t, result2.UnusedCredit.Add(result2.NewCharge).InexactFloat64(), 0.0)
}

func TestRapidSequentialChanges(t *testing.T) {
	// Test multiple rapid plan changes
	cycle := BillingCycle{
		StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
		Cadence: CadenceMonthly,
	}
	
	prices := []Price{
		{Amount: decimal.NewFromInt(100), Cadence: CadenceMonthly},
		{Amount: decimal.NewFromInt(120), Cadence: CadenceMonthly},
		{Amount: decimal.NewFromInt(150), Cadence: CadenceMonthly},
		{Amount: decimal.NewFromInt(130), Cadence: CadenceMonthly},
		{Amount: decimal.NewFromInt(180), Cadence: CadenceMonthly},
	}
	
	times := []time.Time{
		time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 17, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 19, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 21, 0, 0, 0, 0, time.UTC),
	}
	
	// Simulate sequential changes
	for i := 0; i < len(times); i++ {
		fromPrice := prices[i]
		toPrice := prices[i+1]
		
		result, err := ProratedChange(fromPrice, toPrice, cycle, times[i], ProrationBehaviorCreateProrations)
		require.NoError(t, err, "Change %d should not error", i)
		
		// All should produce valid results
		assert.GreaterOrEqual(t, result.UnusedCredit.InexactFloat64(), 0.0)
		assert.GreaterOrEqual(t, result.NewCharge.InexactFloat64(), 0.0)
	}
}

func TestProrationWithCycleRenewals(t *testing.T) {
	// Test proration that spans across cycle renewals
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	
	// Create monthly cycles
	cycles := []BillingCycle{
		CalculateFirstCycle(startDate, CadenceMonthly),
	}
	
	// Add 3 more cycles
	for i := 1; i < 4; i++ {
		nextCycle := CalculateNextCycle(cycles[i-1])
		if !nextCycle.StartAt.IsZero() {
			cycles = append(cycles, nextCycle)
		}
	}
	
	// Verify 4 cycles were created
	require.Equal(t, 4, len(cycles))
	
	// Each cycle should link to the next
	for i := 0; i < len(cycles)-1; i++ {
		assert.Equal(t, cycles[i].EndAt, cycles[i+1].StartAt, "Cycle %d should link to cycle %d", i, i+1)
		assert.Equal(t, CadenceMonthly, cycles[i].Cadence)
	}
	
	// Test proration at same relative position in each cycle
	oldPrice := Price{
		Amount:  decimal.NewFromInt(100),
		Cadence: CadenceMonthly,
	}
	newPrice := Price{
		Amount:  decimal.NewFromInt(150),
		Cadence: CadenceMonthly,
	}
	
	for _, cycle := range cycles {
		// Mid-cycle
		midCycle := cycle.StartAt.AddDate(0, 0, 15)
		
		result, err := ProratedChange(oldPrice, newPrice, cycle, midCycle, ProrationBehaviorCreateProrations)
		require.NoError(t, err)
		
		// Should produce consistent results across cycles
		assert.Greater(t, result.UnusedCredit.InexactFloat64(), 0.0)
		assert.Greater(t, result.NewCharge.InexactFloat64(), 0.0)
		
		// Mid-cycle should be approximately 50% progress
		// Note: With calendar-accurate proration, exact 50% depends on actual cycle length:
		// - 31-day months (Jan, Mar, May, Jul, Aug, Oct, Dec): 16/31 = 51.6%
		// - 30-day months (Apr, Jun, Sep, Nov): 15/30 = 50.0%
		// - February: 14/28 or 14/29 = 50.0% or 48.3% (leap year)
		// Using 5% tolerance to accommodate natural calendar variations
		creditRatio := result.UnusedCredit.Div(oldPrice.Amount).InexactFloat64()
		assert.InDelta(t, 0.5, creditRatio, 0.05)
	}
}

func TestLongTimeHorizonSequences(t *testing.T) {
	// Test sequence spanning many years
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	
	current := CalculateFirstCycle(startDate, CadenceYearly)
	require.False(t, current.StartAt.IsZero())
	
	// Generate 10 years of cycles
	cycles := []BillingCycle{current}
	for i := 0; i < 9; i++ {
		nextCycle := CalculateNextCycle(cycles[len(cycles)-1])
		if nextCycle.StartAt.IsZero() {
			break
		}
		cycles = append(cycles, nextCycle)
	}
	
	// Should have 10 cycles
	assert.Equal(t, 10, len(cycles))
	
	// First cycle should start at original date
	assert.Equal(t, startDate, cycles[0].StartAt)
	
	// Last cycle should end 10 years later
	expectedLastEnd := startDate.AddDate(10, 0, 0)
	assert.Equal(t, expectedLastEnd, cycles[len(cycles)-1].EndAt)
	
	// All cycles should be exactly one year apart
	for i := 1; i < len(cycles); i++ {
		yearDiff := cycles[i].StartAt.Sub(cycles[i-1].StartAt).Hours() / 24 / 365
		assert.InDelta(t, 1.0, yearDiff, 0.1)
	}
}

func TestWeeklyCycleSequences(t *testing.T) {
	// Test sequence of weekly cycles
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	
	cycles := []BillingCycle{
		CalculateFirstCycle(startDate, CadenceWeekly),
	}
	
	// Generate 8 weeks of cycles
	for i := 0; i < 7; i++ {
		nextCycle := CalculateNextCycle(cycles[len(cycles)-1])
		if nextCycle.StartAt.IsZero() {
			break
		}
		cycles = append(cycles, nextCycle)
	}
	
	assert.Equal(t, 8, len(cycles))
	
	// Verify spacing
	for i := 1; i < len(cycles); i++ {
		daysDiff := cycles[i].StartAt.Sub(cycles[i-1].StartAt).Hours() / 24
		assert.InDelta(t, 7.0, daysDiff, 0.01)
	}
}
