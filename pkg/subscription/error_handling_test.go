package subscription

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCadenceAddTo_ErrorCases(t *testing.T) {
	tests := []struct {
		name        string
		cadence     Cadence
		baseTime    time.Time
		expectError bool
	}{
		{
			name:        "unknown cadence returns error",
			cadence:     Cadence("unknown"),
			baseTime:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expectError: true,
		},
		{
			name:        "empty cadence returns error",
			cadence:     Cadence(""),
			baseTime:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expectError: true,
		},
		{
			name:        "invalid cadence string",
			cadence:     Cadence("custom"),
			baseTime:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.cadence.AddTo(tt.baseTime)
			
			if tt.expectError {
				require.Error(t, err)
				assert.True(t, result.IsZero())
			} else {
				require.NoError(t, err)
				assert.False(t, result.IsZero())
			}
		})
	}
}

func TestParseCadence_ErrorCases(t *testing.T) {
	tests := []struct {
		input       string
		expectError bool
	}{
		{
			input:       "",
			expectError: true,
		},
		{
			input:       "invalid",
			expectError: true,
		},
		{
			input:       "Custom",
			expectError: true,
		},
		{
			input:       "DAILY", // Case sensitive
			expectError: true,
		},
		{
			input:       "Weekly ", // Trailing space
			expectError: true,
		},
		{
			input:       " weekly", // Leading space
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			_, err := ParseCadence(tt.input)
			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCalculateFirstCycle_ErrorPropagates(t *testing.T) {
	tests := []struct {
		name        string
		cadence     Cadence
		expectValid bool
	}{
		{
			name:        "unknown cadence returns zero cycle",
			cadence:     Cadence("unknown"),
			expectValid: false,
		},
		{
			name:        "empty cadence returns zero cycle",
			cadence:     Cadence(""),
			expectValid: false,
		},
		{
			name:        "invalid cadence returns zero cycle",
			cadence:     Cadence("invalid"),
			expectValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
			result := CalculateFirstCycle(startDate, tt.cadence)
			
			if tt.expectValid {
				assert.False(t, result.StartAt.IsZero())
				assert.False(t, result.EndAt.IsZero())
			} else {
				assert.Equal(t, BillingCycle{}, result)
			}
		})
	}
}

func TestCalculateNextCycle_ErrorPropagates(t *testing.T) {
	tests := []struct {
		name        string
		cadence     Cadence
		expectValid bool
	}{
		{
			name:        "unknown cadence returns zero cycle",
			cadence:     Cadence("unknown"),
			expectValid: false,
		},
		{
			name:        "invalid cadence returns zero cycle",
			cadence:     Cadence("invalid"),
			expectValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			currentCycle := BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
				Cadence: tt.cadence,
			}
			
			result := CalculateNextCycle(currentCycle)
			
			if tt.expectValid {
				assert.False(t, result.StartAt.IsZero())
				assert.False(t, result.EndAt.IsZero())
			} else {
				assert.Equal(t, BillingCycle{}, result)
			}
		})
	}
}

func TestProratedChange_InvalidInputs(t *testing.T) {
	tests := []struct {
		name        string
		oldPrice    Price
		newPrice    Price
		oldCycle    BillingCycle
		now         time.Time
		expectError bool
		errorMsg    string
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
			now:         time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC),
			expectError: true,
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
			now:         time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC),
			expectError: true,
		},
		{
			name: "invalid cycle dates",
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
			now:         time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC),
			expectError: true,
		},
		{
			name: "proration date outside cycle - before",
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
			now:         time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expectError: true,
		},
		{
			name: "proration date outside cycle - after",
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
				EndAt:   time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC),
			},
			now:         time.Date(2024, 2, 15, 0, 0, 0, 0, time.UTC),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ProratedChange(tt.oldPrice, tt.newPrice, tt.oldCycle, tt.now, ProrationBehaviorCreateProrations)
			
			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestProratedChange_DivisionByZeroProtection(t *testing.T) {
	// Test for division by zero in proration calculations
	
	// Single day cycle should not cause division by zero
	oldPrice := Price{
		Amount:  decimal.NewFromInt(100),
		Cadence: CadenceDaily,
	}
	newPrice := Price{
		Amount:  decimal.NewFromInt(150),
		Cadence: CadenceDaily,
	}
	cycle := BillingCycle{
		StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndAt:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		Cadence:  CadenceDaily,
	}
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	result, err := ProratedChange(oldPrice, newPrice, cycle, now, ProrationBehaviorCreateProrations)
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestDaysBetween_EdgeCases(t *testing.T) {
 tests := []struct {
		name     string
		start    time.Time
		end      time.Time
		expected int
	}{
		{
			name:     "zero duration",
			start:    time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
			end:      time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
			expected: 0,
		},
		{
			name:     "one second difference",
			start:    time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			end:      time.Date(2024, 1, 1, 12, 0, 1, 0, time.UTC),
			expected: 0,
		},
		{
			name:     "23 hours 59 minutes",
			start:    time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
			end:      time.Date(2024, 1, 2, 9, 59, 0, 0, time.UTC),
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DaysBetween(tt.start, tt.end)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRatio_DivisionByZero(t *testing.T) {
	// Test that Ratio handles zero totalDays gracefully
	result := Ratio(10, 0)
	assert.Equal(t, 0.0, result)
}

func TestTimeWeightedAmount_DivisionByZero(t *testing.T) {
	// Test division by zero protection returns zero
	amount := decimal.NewFromInt(100)
	
	result := TimeWeightedAmount(amount, 0, 10)
	assert.Equal(t, decimal.Zero, result)
}

func TestCycleProgress_DivisionByZeroProtection(t *testing.T) {
	// Test for division by zero in progress calculation
	cycle := BillingCycle{
		StartAt: time.Time{},
		EndAt:   time.Time{},
		Cadence: CadenceDaily,
	}
	now := time.Now()

	// Should not panic, should return 0
	progress := CycleProgress(cycle, now)
	assert.Equal(t, 0.0, progress)
}

func TestRatio_ZeroValues(t *testing.T) {
	tests := []struct {
		name      string
		days      int
		totalDays int
		expected  float64
	}{
		{
			name:      "zero days with normal total",
			days:      0,
			totalDays: 30,
			expected:  0.0,
		},
		{
			name:      "zero totalDays returns zero",
			days:      15,
			totalDays: 0,
			expected:  0.0,
		},
		{
			name:      "both zero",
			days:      0,
			totalDays: 0,
			expected:  0.0,
		},
		{
			name:      "negative days with normal total",
			days:      -10,
			totalDays: 30,
			expected:  -0.333,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Ratio(tt.days, tt.totalDays)
			assert.InDelta(t, tt.expected, result, 0.001)
		})
	}
}



func TestNetResult_ComponentValidation(t *testing.T) {
	tests := []struct {
		name          string
		unusedCredit  decimal.Decimal
		newCharge     decimal.Decimal
		expectedRange string // "positive", "negative", "zero"
	}{
		{
			name:          "credit exceeds charge",
			unusedCredit:  decimal.NewFromInt(100),
			newCharge:     decimal.NewFromInt(50),
			expectedRange: "negative",
		},
		{
			name:          "charge exceeds credit",
			unusedCredit:  decimal.NewFromInt(50),
			newCharge:     decimal.NewFromInt(100),
			expectedRange: "positive",
		},
		{
			name:          "equal values",
			unusedCredit:  decimal.NewFromInt(100),
			newCharge:     decimal.NewFromInt(100),
			expectedRange: "zero",
		},
		{
			name:          "both zero",
			unusedCredit:  decimal.NewFromInt(0),
			newCharge:     decimal.NewFromInt(0),
			expectedRange: "zero",
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
			
			switch tt.expectedRange {
			case "positive":
				assert.Greater(t, net.InexactFloat64(), 0.0)
			case "negative":
				assert.Less(t, net.InexactFloat64(), 0.0)
			case "zero":
				assert.LessOrEqual(t, net.Abs().InexactFloat64(), 0.0001)
			}
		})
	}
}
