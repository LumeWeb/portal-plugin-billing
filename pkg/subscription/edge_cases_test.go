package subscription

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProratedChange_ExtremeValues(t *testing.T) {
	tests := []struct {
		name     string
		oldPrice Price
		newPrice Price
		oldCycle BillingCycle
		now      time.Time
	}{
		{
			name: "very large amounts - millions",
			oldPrice: Price{
				Amount:  decimal.NewFromInt(100000000),
				Cadence: CadenceYearly,
			},
			newPrice: Price{
				Amount:  decimal.NewFromInt(500000000),
				Cadence: CadenceYearly,
			},
			oldCycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC),
			},
			now: time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "sub-cent precision - thousandths",
			oldPrice: Price{
				Amount:  decimal.NewFromFloat(0.001),
				Cadence: CadenceDaily,
			},
			newPrice: Price{
				Amount:  decimal.NewFromFloat(0.002),
				Cadence: CadenceDaily,
			},
			oldCycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
			},
			now: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		},
		{
			name: "maximum decimal precision",
			oldPrice: Price{
				Amount:  decimal.NewFromFloat(123.456789012345678901234567890),
				Cadence: CadenceMonthly,
			},
			newPrice: Price{
				Amount:  decimal.NewFromFloat(456.789012345678901234567890123),
				Cadence: CadenceMonthly,
			},
			oldCycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now: time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "very small fraction near zero",
			oldPrice: Price{
				Amount:  decimal.NewFromFloat(0.000000001),
				Cadence: CadenceDaily,
			},
			newPrice: Price{
				Amount:  decimal.NewFromFloat(0.000000002),
				Cadence: CadenceDaily,
			},
			oldCycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
			},
			now: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ProratedChange(tt.oldPrice, tt.newPrice, tt.oldCycle, tt.now, ProrationBehaviorCreateProrations)
			require.NoError(t, err)
			
			// Should handle extreme values without panicking
			assert.False(t, result.UnusedCredit.IsNegative())
			assert.False(t, result.NewCharge.IsNegative())
			
			// Verify calculations are reasonable
			netResult := NetResult(result)
			assert.False(t, checkForOverflow(netResult))
		})
	}
}

func TestProratedChange_ZeroRemainingDays(t *testing.T) {
	// Test proration when cycle is at end (0 days remaining)
	oldPrice := Price{
		Amount:  decimal.NewFromInt(100),
		Cadence: CadenceMonthly,
	}
	newPrice := Price{
		Amount:  decimal.NewFromInt(150),
		Cadence: CadenceMonthly,
	}
	oldCycle := BillingCycle{
		StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
	}
	now := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)

	result, err := ProratedChange(oldPrice, newPrice, oldCycle, now, ProrationBehaviorCreateProrations)
	require.NoError(t, err)
	
	// At cycle end, remaining days is 0, so should get minimal credit/charge
	assert.Equal(t, decimal.Zero, result.UnusedCredit)
	assert.Equal(t, decimal.Zero, result.NewCharge)
}

func TestProratedChange_ZeroElapsedDays(t *testing.T) {
	// Test proration at cycle start (0 days elapsed)
	oldPrice := Price{
		Amount:  decimal.NewFromInt(100),
		Cadence: CadenceMonthly,
	}
	newPrice := Price{
		Amount:  decimal.NewFromInt(150),
		Cadence: CadenceMonthly,
	}
	oldCycle := BillingCycle{
		StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
	}
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	result, err := ProratedChange(oldPrice, newPrice, oldCycle, now, ProrationBehaviorCreateProrations)
	require.NoError(t, err)
	
	// At cycle start, should have full credit and full charge
	assert.Greater(t, result.UnusedCredit.InexactFloat64(), 0.0)
	assert.Greater(t, result.NewCharge.InexactFloat64(), 0.0)
	
	// Should be based on full 30-day cycle
	expectedCreditRatio := result.UnusedCredit.Div(oldPrice.Amount).InexactFloat64()
	expectedChargeRatio := result.NewCharge.Div(newPrice.Amount).InexactFloat64()
	assert.InDelta(t, 1.0, expectedCreditRatio, 0.01)
	assert.InDelta(t, 1.0, expectedChargeRatio, 0.01)
}

func TestProratedChange_ImmediateRetroactive(t *testing.T) {
	// Test proration at the start of a cycle
	oldPrice := Price{
		Amount:  decimal.NewFromInt(100),
		Cadence: CadenceMonthly,
	}
	newPrice := Price{
		Amount:  decimal.NewFromInt(150),
		Cadence: CadenceMonthly,
	}
	oldCycle := BillingCycle{
		StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
	}
	
	// Proration at exact cycle start
	now := oldCycle.StartAt

	result, err := ProratedChange(oldPrice, newPrice, oldCycle, now, ProrationBehaviorCreateProrations)
	require.NoError(t, err)
	
	// At cycle start, all time is unused
	assert.Greater(t, result.UnusedCredit.InexactFloat64(), 0.0)
	assert.Greater(t, result.NewCharge.InexactFloat64(), 0.0)
}

func TestProratedChange_SamePrice(t *testing.T) {
	// Test when old and new prices are the same
	oldPrice := Price{
		Amount:  decimal.NewFromInt(100),
		Cadence: CadenceMonthly,
	}
	newPrice := Price{
		Amount:  decimal.NewFromInt(100),
		Cadence: CadenceMonthly,
	}
	oldCycle := BillingCycle{
		StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
	}
	now := time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC)

	result, err := ProratedChange(oldPrice, newPrice, oldCycle, now, ProrationBehaviorCreateProrations)
	require.NoError(t, err)
	
	// Same price means credit equals charge, net is zero
	assert.True(t, result.UnusedCredit.Equal(result.NewCharge))
	assert.True(t, NetResult(result).Equal(decimal.Zero))
}

func TestProratedChange_DowngradeToZero(t *testing.T) {
	// Test downgrading to a free plan
	oldPrice := Price{
		Amount:  decimal.NewFromInt(100),
		Cadence: CadenceMonthly,
	}
	newPrice := Price{
		Amount:  decimal.NewFromInt(0),
		Cadence: CadenceMonthly,
	}
	oldCycle := BillingCycle{
		StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
	}
	now := time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC)

	result, err := ProratedChange(oldPrice, newPrice, oldCycle, now, ProrationBehaviorCreateProrations)
	require.NoError(t, err)
	
	// New charge should be zero
	assert.True(t, result.NewCharge.Equal(decimal.Zero))
	// Should get credit for unused time
	assert.Greater(t, result.UnusedCredit.InexactFloat64(), 0.0)
	// Net should be negative (credit due)
	assert.Less(t, NetResult(result).InexactFloat64(), 0.0)
}

func TestProratedChange_UpgradeFromZero(t *testing.T) {
	// Test upgrading from a free plan
	oldPrice := Price{
		Amount:  decimal.NewFromInt(0),
		Cadence: CadenceMonthly,
	}
	newPrice := Price{
		Amount:  decimal.NewFromInt(100),
		Cadence: CadenceMonthly,
	}
	oldCycle := BillingCycle{
		StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
	}
	now := time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC)

	result, err := ProratedChange(oldPrice, newPrice, oldCycle, now, ProrationBehaviorCreateProrations)
	require.NoError(t, err)
	
	// Old credit should be zero
	assert.True(t, result.UnusedCredit.Equal(decimal.Zero))
	// Should charge for remaining time
	assert.Greater(t, result.NewCharge.InexactFloat64(), 0.0)
	// Net should be positive (charge due)
	assert.Greater(t, NetResult(result).InexactFloat64(), 0.0)
}

func TestProratedChange_MaxPrecision(t *testing.T) {
	// Test that calculations maintain maximum decimal precision
	oldPrice := Price{
		Amount:  decimal.NewFromInt(1),
		Cadence: CadenceDaily,
	}
	newPrice := Price{
		Amount:  decimal.NewFromInt(2),
		Cadence: CadenceDaily,
	}
	oldCycle := BillingCycle{
		StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndAt:   time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
	}
	
	// Proration at midpoint
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	result, err := ProratedChange(oldPrice, newPrice, oldCycle, now, ProrationBehaviorCreateProrations)
	require.NoError(t, err)
	
	// Verify precision is maintained by checking decimal places
	creditStr := result.UnusedCredit.String()
	chargeStr := result.NewCharge.String()
	
	// Should have some decimal places for precision
	assert.NotContains(t, creditStr, ".000")
	assert.NotContains(t, chargeStr, ".000")
}

// Helper function to check for obvious overflow
func checkForOverflow(d decimal.Decimal) bool {
	// Check if value is unreasonably large (indicates overflow)
	maxReasonable := decimal.NewFromInt(1000000000000) // 1 trillion
	negMaxReasonable := maxReasonable.Neg()
	
	return d.GreaterThan(maxReasonable) || d.LessThan(negMaxReasonable)
}

func TestNetResult_ExtremeValues(t *testing.T) {
	tests := []struct {
		name                string
		unusedCredit        decimal.Decimal
		newCharge           decimal.Decimal
		expectedComparison  string // "positive", "negative", "zero"
	}{
		{
			name:               "large credit exceeds charge",
			unusedCredit:       decimal.NewFromInt(1000000000),
			newCharge:          decimal.NewFromInt(100),
			expectedComparison: "negative",
		},
		{
			name:               "large charge exceeds credit",
			unusedCredit:       decimal.NewFromInt(100),
			newCharge:          decimal.NewFromInt(1000000000),
			expectedComparison: "positive",
		},
		{
			name:               "maximum precision balance",
			unusedCredit:       decimal.NewFromFloat(123456789.123456789),
			newCharge:          decimal.NewFromFloat(123456789.123456789),
			expectedComparison: "zero",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ProrationResult{
				UnusedCredit:  tt.unusedCredit,
				NewCharge:     tt.newCharge,
				CreditDue:     decimal.Zero,
				EffectiveDate: time.Now(),
			}
			
			net := NetResult(result)
			
			switch tt.expectedComparison {
			case "positive":
				assert.Greater(t, net.InexactFloat64(), 0.0)
			case "negative":
				assert.Less(t, net.InexactFloat64(), 0.0)
			case "zero":
				assert.LessOrEqual(t, net.Abs().InexactFloat64(), 0.01)
			}
		})
	}
}

func TestTimeWeightedAmount_PrecisionHandling(t *testing.T) {
	tests := []struct {
		name         string
		amount       decimal.Decimal
		totalDays    int
		daysInPeriod int
	}{
		{
			name:         "fractional division precision",
			amount:       decimal.NewFromInt(100),
			totalDays:    7,
			daysInPeriod: 3,
		},
		{
			name:         "small amount precision",
			amount:       decimal.NewFromFloat(0.001),
			totalDays:    365,
			daysInPeriod: 180,
		},
		{
			name:         "large amount precision",
			amount:       decimal.NewFromInt(999999),
			totalDays:    365,
			daysInPeriod: 182,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TimeWeightedAmount(tt.amount, tt.totalDays, tt.daysInPeriod)
			
			// Should not panic on any input
			assert.False(t, checkForOverflow(result))
		})
	}
}
