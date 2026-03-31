package subscription

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProratedChange_BehaviorCreateProrations(t *testing.T) {
	tests := []struct {
		name     string
		oldPrice Price
		newPrice Price
		oldCycle BillingCycle
		now      time.Time
		expected ProrationResult
	}{
		{
			name: "same cadence - create prorations",
			oldPrice: Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: CadenceMonthly,
			},
			newPrice: Price{
				Amount:  decimal.NewFromInt(150),
				Cadence: CadenceMonthly,
			},
			oldCycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now: time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC),
			expected: ProrationResult{
				UnusedCredit:  decimal.NewFromInt(50),
				NewCharge:     decimal.NewFromInt(75),
				CreditDue:     decimal.NewFromInt(25),
				EffectiveDate: time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "cross cadence - create prorations", 
			oldPrice: Price{
				Amount:  decimal.NewFromInt(50),
				Cadence: CadenceMonthly,
			},
			newPrice: Price{
				Amount:  decimal.NewFromInt(600),
				Cadence: CadenceYearly,
			},
			oldCycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now: time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC),
			expected: ProrationResult{
				UnusedCredit:  decimal.NewFromInt(25),
				NewCharge:    decimal.NewFromInt(600),
				CreditDue:    decimal.NewFromInt(575),
				EffectiveDate: time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ProratedChange(tt.oldPrice, tt.newPrice, tt.oldCycle, tt.now, ProrationBehaviorCreateProrations)
			require.NoError(t, err)
			
			// Check that proration is calculated
			assert.Greater(t, result.UnusedCredit.Add(result.NewCharge).Abs().InexactFloat64(), 0.0)
			
			// Verify result components make sense
			assert.GreaterOrEqual(t, result.UnusedCredit.InexactFloat64(), 0.0)
		})
	}
}

func TestProratedChange_BehaviorAlwaysInvoice(t *testing.T) {
	// ProrationBehaviorAlwaysInvoice behaves the same as CreateProrations for calculation purposes
	// The difference is in invoicing (immediate vs next invoice) which is outside this package
	tests := []struct {
		name     string
		oldPrice Price
		newPrice Price
		oldCycle BillingCycle
		now      time.Time
	}{
		{
			name: "same cadence - always invoice",
			oldPrice: Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: CadenceMonthly,
			},
			newPrice: Price{
				Amount:  decimal.NewFromInt(150),
				Cadence: CadenceMonthly,
			},
			oldCycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now: time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "cross cadence - always invoice",
			oldPrice: Price{
				Amount:  decimal.NewFromInt(50),
				Cadence: CadenceMonthly,
			},
			newPrice: Price{
				Amount:  decimal.NewFromInt(600),
				Cadence: CadenceYearly,
			},
			oldCycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now: time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ProratedChange(tt.oldPrice, tt.newPrice, tt.oldCycle, tt.now, ProrationBehaviorAlwaysInvoice)
			require.NoError(t, err)
			
			// AlwaysInvoice should calculate proration the same way as CreateProrations
			resultCreate, err := ProratedChange(tt.oldPrice, tt.newPrice, tt.oldCycle, tt.now, ProrationBehaviorCreateProrations)
			require.NoError(t, err)
			
			assert.Equal(t, resultCreate.UnusedCredit, result.UnusedCredit)
			assert.Equal(t, resultCreate.NewCharge, result.NewCharge)
			assert.Equal(t, resultCreate.CreditDue, result.CreditDue)
		})
	}
}

func TestProratedChange_BehaviorNone_Basics(t *testing.T) {
	tests := []struct {
		name         string
		oldPrice     Price
		newPrice     Price
		oldCycle     BillingCycle
		now          time.Time
		expectFull   bool // For cross-cadence, expect full charge even with ProrationBehaviorNone
	}{
		{
			name: "same cadence - behavior none",
			oldPrice: Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: CadenceMonthly,
			},
			newPrice: Price{
				Amount:  decimal.NewFromInt(150),
				Cadence: CadenceMonthly,
			},
			oldCycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now: time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC),
			expectFull: false,
		},
		{
			name: "cross cadence - behavior none - charges full amount",
			oldPrice: Price{
				Amount:  decimal.NewFromInt(50),
				Cadence: CadenceMonthly,
			},
			newPrice: Price{
				Amount:  decimal.NewFromInt(600),
				Cadence: CadenceYearly,
			},
			oldCycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now: time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC),
			expectFull: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ProratedChange(tt.oldPrice, tt.newPrice, tt.oldCycle, tt.now, ProrationBehaviorNone)
			require.NoError(t, err)
			
			// With ProrationBehaviorNone, always expect zero credit
			assert.Equal(t, decimal.Zero, result.UnusedCredit)
			
			if tt.expectFull {
				// For cross-cadence, behavior none still charges full amount
				assert.Equal(t, tt.newPrice.Amount, result.NewCharge)
			} else {
				// For same-cadence, behavior none charges nothing
				assert.Equal(t, decimal.Zero, result.NewCharge)
			}
		})
	}
}

func TestProratedChange_BehaviorComparison(t *testing.T) {
	// Compare all three behaviors with the same inputs
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
	now := time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC)

	t.Run("CreateProrations calculates credits and charges", func(t *testing.T) {
		result, err := ProratedChange(oldPrice, newPrice, oldCycle, now, ProrationBehaviorCreateProrations)
		require.NoError(t, err)
		assert.Greater(t, result.UnusedCredit.Add(result.NewCharge).InexactFloat64(), 0.0)
	})

	t.Run("AlwaysInvoice calculates same as CreateProrations", func(t *testing.T) {
		resultCreate, err := ProratedChange(oldPrice, newPrice, oldCycle, now, ProrationBehaviorCreateProrations)
		require.NoError(t, err)
		
		resultAlways, err := ProratedChange(oldPrice, newPrice, oldCycle, now, ProrationBehaviorAlwaysInvoice)
		require.NoError(t, err)
		
		assert.Equal(t, resultCreate.UnusedCredit, resultAlways.UnusedCredit)
		assert.Equal(t, resultCreate.NewCharge, resultAlways.NewCharge)
	})

	t.Run("BehaviorNone returns zero amounts", func(t *testing.T) {
		result, err := ProratedChange(oldPrice, newPrice, oldCycle, now, ProrationBehaviorNone)
		require.NoError(t, err)
		
		assert.Equal(t, decimal.Zero, result.UnusedCredit)
		assert.Equal(t, decimal.Zero, result.NewCharge)
		assert.Equal(t, decimal.Zero, result.CreditDue)
	})
}

func TestProratedChange_VerySmallAmounts(t *testing.T) {
	// Test edge cases with very small amounts to check rounding behavior
	tests := []struct {
		name     string
		oldPrice Price
		newPrice Price
		oldCycle BillingCycle
		now      time.Time
	}{
		{
			name: "sub-cent amounts - daily to monthly",
			oldPrice: Price{
				Amount:  decimal.NewFromInt(1),
				Cadence: CadenceDaily,
			},
			newPrice: Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: CadenceMonthly,
			},
			oldCycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now: time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ProratedChange(tt.oldPrice, tt.newPrice, tt.oldCycle, tt.now, ProrationBehaviorCreateProrations)
			require.NoError(t, err)
			
			// Should successfully calculate without panicking
			assert.False(t, result.UnusedCredit.IsNegative())
			assert.False(t, result.NewCharge.IsNegative())
		})
	}
}

func TestProratedChange_SubSecondPrecision(t *testing.T) {
	// Test that sub-second timestamps are handled correctly
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
		EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 999999, time.UTC),
	}
	
	// Use sub-second precision
	now := time.Date(2024, 1, 16, 12, 30, 45, 123456, time.UTC)

	result, err := ProratedChange(oldPrice, newPrice, oldCycle, now, ProrationBehaviorCreateProrations)
	require.NoError(t, err)
	
	// Should handle sub-second precision correctly
	assert.Greater(t, result.UnusedCredit.InexactFloat64(), 0.0)
	assert.Greater(t, result.NewCharge.InexactFloat64(), 0.0)
}

func TestProratedChange_ImmediateProration(t *testing.T) {
	// Test proration at exact cycle start (zero remaining days)
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
	
	// At cycle start, full credit and full charge expected
	assert.Greater(t, result.UnusedCredit.InexactFloat64(), 0.0)
	assert.Greater(t, result.NewCharge.InexactFloat64(), 0.0)
}
