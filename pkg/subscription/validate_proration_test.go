package subscription

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateProrationInputs_ValidInputs(t *testing.T) {
	tests := []struct {
		name     string
		oldPrice Price
		newPrice Price
		oldCycle BillingCycle
		now      time.Time
	}{
		{
			name: "valid basic proration",
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
			now: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "valid zero amounts",
			oldPrice: Price{
				Amount:  decimal.NewFromInt(0),
				Cadence: CadenceDaily,
			},
			newPrice: Price{
				Amount:  decimal.NewFromInt(0),
				Cadence: CadenceDaily,
			},
			oldCycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
			},
			now: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "valid large amounts",
			oldPrice: Price{
				Amount:  decimal.NewFromInt(1000000),
				Cadence: CadenceYearly,
			},
			newPrice: Price{
				Amount:  decimal.NewFromInt(5000000),
				Cadence: CadenceYearly,
			},
			oldCycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC),
			},
			now: time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateProrationInputs(tt.oldPrice, tt.newPrice, tt.oldCycle, tt.now)
			require.NoError(t, err)
		})
	}
}

func TestValidateProrationInputs_NegativePrices(t *testing.T) {
	tests := []struct {
		name     string
		oldPrice Price
		newPrice Price
		oldCycle BillingCycle
		now      time.Time
		expected string
	}{
		{
			name: "negative old price",
			oldPrice: Price{
				Amount:  decimal.NewFromInt(-100),
				Cadence: CadenceMonthly,
			},
			newPrice: Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: CadenceMonthly,
			},
			oldCycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now:      time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			expected: "old price amount cannot be negative",
		},
		{
			name: "negative new price",
			oldPrice: Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: CadenceMonthly,
			},
			newPrice: Price{
				Amount:  decimal.NewFromInt(-50),
				Cadence: CadenceMonthly,
			},
			oldCycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now:      time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			expected: "new price amount cannot be negative",
		},
		{
			name: "negative both prices",
			oldPrice: Price{
				Amount:  decimal.NewFromInt(-100),
				Cadence: CadenceMonthly,
			},
			newPrice: Price{
				Amount:  decimal.NewFromInt(-50),
				Cadence: CadenceMonthly,
			},
			oldCycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now:      time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			expected: "old price amount cannot be negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateProrationInputs(tt.oldPrice, tt.newPrice, tt.oldCycle, tt.now)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.expected)
		})
	}
}

func TestValidateProrationInputs_InvalidCycleDates(t *testing.T) {
	tests := []struct {
		name     string
		oldPrice Price
		newPrice Price
		oldCycle BillingCycle
		now      time.Time
		expected string
	}{
		{
			name: "zero start date",
			oldPrice: Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: CadenceMonthly,
			},
			newPrice: Price{
				Amount:  decimal.NewFromInt(150),
				Cadence: CadenceMonthly,
			},
			oldCycle: BillingCycle{
				StartAt: time.Time{},
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now:      time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			expected: "billing cycle must have valid start and end dates",
		},
		{
			name: "zero end date",
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
				EndAt:   time.Time{},
			},
			now:      time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			expected: "billing cycle must have valid start and end dates",
		},
		{
			name: "start after end",
			oldPrice: Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: CadenceMonthly,
			},
			newPrice: Price{
				Amount:  decimal.NewFromInt(150),
				Cadence: CadenceMonthly,
			},
			oldCycle: BillingCycle{
				StartAt: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now:      time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			expected: "billing cycle start date cannot be after end date",
		},
		{
			name: "same start and end date",
			oldPrice: Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: CadenceDaily,
			},
			newPrice: Price{
				Amount:  decimal.NewFromInt(150),
				Cadence: CadenceDaily,
			},
			oldCycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			},
			now:      time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateProrationInputs(tt.oldPrice, tt.newPrice, tt.oldCycle, tt.now)
			if tt.expected == "" {
				// Same start/end with daily cadence should pass
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expected)
			}
		})
	}
}

func TestValidateProrationInputs_DateOutsideCycle(t *testing.T) {
	tests := []struct {
		name     string
		oldPrice Price
		newPrice Price
		oldCycle BillingCycle
		now      time.Time
		expected string
	}{
		{
			name: "now before cycle start",
			oldPrice: Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: CadenceMonthly,
			},
			newPrice: Price{
				Amount:  decimal.NewFromInt(150),
				Cadence: CadenceMonthly,
			},
			oldCycle: BillingCycle{
				StartAt: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 2, 15, 0, 0, 0, 0, time.UTC),
			},
			now:      time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expected: "proration date must be within billing cycle",
		},
		{
			name: "now after cycle end",
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
			now:      time.Date(2024, 2, 5, 0, 0, 0, 0, time.UTC),
			expected: "proration date must be within billing cycle",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateProrationInputs(tt.oldPrice, tt.newPrice, tt.oldCycle, tt.now)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.expected)
		})
	}
}

func TestValidateProrationInputs_BoundaryCases(t *testing.T) {
	tests := []struct {
		name     string
		oldPrice Price
		newPrice Price
		oldCycle BillingCycle
		now      time.Time
	}{
		{
			name: "proration at exact cycle start",
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
			now: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "proration at exact cycle end",
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
			now: time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
		},
		{
			name: "leap year February start",
			oldPrice: Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: CadenceMonthly,
			},
			newPrice: Price{
				Amount:  decimal.NewFromInt(150),
				Cadence: CadenceMonthly,
			},
			oldCycle: BillingCycle{
				StartAt: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 2, 29, 23, 59, 59, 0, time.UTC),
			},
			now: time.Date(2024, 2, 15, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateProrationInputs(tt.oldPrice, tt.newPrice, tt.oldCycle, tt.now)
			require.NoError(t, err)
		})
	}
}
