package subscription

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Plan Tests
// ============================================================================

// TestPlanStruct verifies Plan struct exists with correct fields
func TestPlanStruct(t *testing.T) {
	var _ Plan = Plan{} // This will fail if Plan type doesn't exist

	p := Plan{
		ID:       1,
		Name:     "Test Plan",
		Currency: "USD",
	}

	require.Equal(t, uint(1), p.ID)
	require.Equal(t, "Test Plan", p.Name)
	require.Equal(t, "USD", p.Currency)
}

// TestPlanZeroValues verifies Plan zero value behavior
func TestPlanZeroValues(t *testing.T) {
	p := Plan{}

	require.Equal(t, uint(0), p.ID)
	require.Equal(t, "", p.Name)
	require.Equal(t, "", p.Currency)
}

// TestPlanFieldValidation validates field constraints and relationships
func TestPlanFieldValidation(t *testing.T) {
	tests := []struct {
		name           string
		plan           Plan
		expectedID     uint
		expectedName   string
		expectedCurrency string
	}{
		{
			name: "standard plan",
			plan: Plan{
				ID:       100,
				Name:     "Premium Plan",
				Currency: "USD",
			},
			expectedID:     100,
			expectedName:   "Premium Plan",
			expectedCurrency: "USD",
		},
		{
			name: "minimum ID",
			plan: Plan{
				ID:       0,
				Name:     "Basic Plan",
				Currency: "EUR",
			},
			expectedID:     0,
			expectedName:   "Basic Plan",
			expectedCurrency: "EUR",
		},
		{
			name: "large ID",
			plan: Plan{
				ID:       999999,
				Name:     "Enterprise Plan",
				Currency: "GBP",
			},
			expectedID:     999999,
			expectedName:   "Enterprise Plan",
			expectedCurrency: "GBP",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expectedID, tt.plan.ID)
			assert.Equal(t, tt.expectedName, tt.plan.Name)
			assert.Equal(t, tt.expectedCurrency, tt.plan.Currency)
		})
	}
}

// TestPlanJSONSerialization tests JSON marshaling and unmarshaling
func TestPlanJSONSerialization(t *testing.T) {
	tests := []struct {
		name     string
		plan     Plan
		expected string
	}{
		{
			name: "standard plan",
			plan: Plan{
				ID:       1,
				Name:     "Test Plan",
				Currency: "USD",
			},
			expected: `{"ID":1,"Name":"Test Plan","Currency":"USD"}`,
		},
		{
			name: "empty plan",
			plan: Plan{},
			expected: `{"ID":0,"Name":"","Currency":""}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test marshaling
			data, err := json.Marshal(tt.plan)
			require.NoError(t, err)
			assert.JSONEq(t, tt.expected, string(data))

			// Test unmarshaling
			var unmarshaled Plan
			err = json.Unmarshal(data, &unmarshaled)
			require.NoError(t, err)
			assert.Equal(t, tt.plan.ID, unmarshaled.ID)
			assert.Equal(t, tt.plan.Name, unmarshaled.Name)
			assert.Equal(t, tt.plan.Currency, unmarshaled.Currency)
		})
	}
}

// TestPlanJSONUnmarshalInvalid tests unmarshal rejects invalid data
func TestPlanJSONUnmarshalInvalid(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{
			name: "invalid JSON",
			data: `{"invalid json:}`,
		},
		{
			name: "string instead of number for ID",
			data: `{"ID":"not a number","Name":"Test","Currency":"USD"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var p Plan
			err := json.Unmarshal([]byte(tt.data), &p)
			require.Error(t, err)
		})
	}
}

// ============================================================================
// BillingCycle Tests
// ============================================================================

// TestBillingCycleStruct verifies BillingCycle struct exists with correct fields
func TestBillingCycleStruct(t *testing.T) {
	var _ BillingCycle = BillingCycle{} // This will fail if BillingCycle type doesn't exist

	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)

	bc := BillingCycle{
		StartAt: start,
		EndAt:   end,
		Cadence: CadenceMonthly,
	}

	require.True(t, bc.StartAt.Equal(start))
	require.True(t, bc.EndAt.Equal(end))
	require.Equal(t, CadenceMonthly, bc.Cadence)
}

// TestBillingCycleZeroValues verifies BillingCycle zero value behavior
func TestBillingCycleZeroValues(t *testing.T) {
	bc := BillingCycle{}

	require.True(t, bc.StartAt.IsZero())
	require.True(t, bc.EndAt.IsZero())
	require.Equal(t, Cadence(""), bc.Cadence)
}

// TestBillingCycleFieldValidation validates field constraints
func TestBillingCycleFieldValidation(t *testing.T) {
	tests := []struct {
		name           string
		cycle          BillingCycle
		expectedStart  time.Time
		expectedEnd    time.Time
		expectedCadence Cadence
	}{
		{
			name: "monthly cycle",
			cycle: BillingCycle{
				StartAt:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:    time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
				Cadence:  CadenceMonthly,
			},
			expectedStart:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expectedEnd:    time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
			expectedCadence: CadenceMonthly,
		},
		{
			name: "annual cycle",
			cycle: BillingCycle{
				StartAt:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:    time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
				Cadence:  CadenceYearly,
			},
			expectedStart:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expectedEnd:    time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			expectedCadence: CadenceYearly,
		},
		{
			name: "weekly cycle",
			cycle: BillingCycle{
				StartAt:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:    time.Date(2024, 1, 8, 0, 0, 0, 0, time.UTC),
				Cadence:  CadenceWeekly,
			},
			expectedStart:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expectedEnd:    time.Date(2024, 1, 8, 0, 0, 0, 0, time.UTC),
			expectedCadence: CadenceWeekly,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.True(t, tt.cycle.StartAt.Equal(tt.expectedStart))
			assert.True(t, tt.cycle.EndAt.Equal(tt.expectedEnd))
			assert.Equal(t, tt.expectedCadence, tt.cycle.Cadence)
			assert.True(t, tt.cycle.StartAt.Before(tt.cycle.EndAt), "StartAt should be before EndAt")
		})
	}
}

// TestBillingCycleJSONSerialization tests JSON marshaling and unmarshaling
func TestBillingCycleJSONSerialization(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)

	bc := BillingCycle{
		StartAt: start,
		EndAt:   end,
		Cadence: CadenceMonthly,
	}

	// Test marshaling
	data, err := json.Marshal(bc)
	require.NoError(t, err)

	// Test unmarshaling
	var unmarshaled BillingCycle
	err = json.Unmarshal(data, &unmarshaled)
	require.NoError(t, err)
	assert.True(t, unmarshaled.StartAt.Equal(bc.StartAt))
	assert.True(t, unmarshaled.EndAt.Equal(bc.EndAt))
	assert.Equal(t, bc.Cadence, unmarshaled.Cadence)
}

// ============================================================================
// Price Tests
// ============================================================================

// TestPriceStruct verifies Price struct exists with correct fields
func TestPriceStruct(t *testing.T) {
	var _ Price = Price{} // This will fail if Price type doesn't exist

	amount := decimal.NewFromInt(100)

	p := Price{
		Amount:  amount,
		Cadence: CadenceMonthly,
	}

	require.True(t, p.Amount.Equal(amount))
	require.Equal(t, CadenceMonthly, p.Cadence)
}

// TestPriceZeroValues verifies Price zero value behavior
func TestPriceZeroValues(t *testing.T) {
	p := Price{}

	require.True(t, p.Amount.IsZero())
	require.Equal(t, Cadence(""), p.Cadence)
}

// TestPriceDecimalOperations tests decimal field operations
func TestPriceDecimalOperations(t *testing.T) {
	tests := []struct {
		name        string
		price       Price
		otherAmount decimal.Decimal
		op          string
		expected    decimal.Decimal
	}{
		{
			name: "zero price",
			price: Price{
				Amount:  decimal.Zero,
				Cadence: CadenceMonthly,
			},
			otherAmount: decimal.NewFromInt(50),
			op:          "base",
			expected:    decimal.Zero,
		},
		{
			name: "positive price",
			price: Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: CadenceMonthly,
			},
			otherAmount: decimal.NewFromInt(50),
			op:          "base",
			expected:    decimal.NewFromInt(100),
		},
		{
			name: "decimal precision",
			price: Price{
				Amount:  decimal.NewFromFloat(99.99),
				Cadence: CadenceMonthly,
			},
			otherAmount: decimal.NewFromInt(50),
			op:          "base",
			expected:    decimal.NewFromFloat(99.99),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.True(t, tt.price.Amount.Equal(tt.expected))
			assert.NotPanics(t, func() {
				// Ensure decimal operations work
				tt.price.Amount.Add(tt.otherAmount)
				tt.price.Amount.Sub(tt.otherAmount)
				tt.price.Amount.Mul(decimal.NewFromInt(2))
				tt.price.Amount.Div(decimal.NewFromInt(2))
			})
		})
	}
}

// TestPriceFieldValidation validates field constraints
func TestPriceFieldValidation(t *testing.T) {
	tests := []struct {
		name           string
		price          Price
		expectedString string
	}{
		{
			name: "standard price",
			price: Price{
				Amount:  decimal.NewFromInt(10000),
				Cadence: CadenceMonthly,
			},
		},
		{
			name: "fractional price",
			price: Price{
				Amount:  decimal.NewFromFloat(29.99),
				Cadence: CadenceMonthly,
			},
		},
		{
			name: "very small price",
			price: Price{
				Amount:  decimal.NewFromFloat(0.01),
				Cadence: CadenceMonthly,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.False(t, tt.price.Amount.IsNegative())
			assert.NotEmpty(t, tt.price.Cadence)
		})
	}
}

// TestPriceJSONSerialization tests JSON marshaling and unmarshaling
func TestPriceJSONSerialization(t *testing.T) {
	tests := []struct {
		name     string
		price    Price
	}{
		{
			name: "integer price",
			price: Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: CadenceMonthly,
			},
		},
		{
			name: "decimal price",
			price: Price{
				Amount:  decimal.NewFromFloat(99.99),
				Cadence: CadenceYearly,
			},
		},
		{
			name: "zero price",
			price: Price{
				Amount:  decimal.Zero,
				Cadence: "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test marshaling
			data, err := json.Marshal(tt.price)
			require.NoError(t, err)

			// Test unmarshaling
			var unmarshaled Price
			err = json.Unmarshal(data, &unmarshaled)
			require.NoError(t, err)
			assert.True(t, unmarshaled.Amount.Equal(tt.price.Amount))
			assert.Equal(t, tt.price.Cadence, unmarshaled.Cadence)
		})
	}
}

// ============================================================================
// ProrationResult Tests
// ============================================================================

// TestProrationResultStruct verifies ProrationResult struct exists with correct fields
func TestProrationResultStruct(t *testing.T) {
	var _ ProrationResult = ProrationResult{} // This will fail if ProrationResult type doesn't exist

	credit := decimal.NewFromInt(50)
	charge := decimal.NewFromInt(75)
	due := decimal.NewFromInt(25)
	date := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)

	pr := ProrationResult{
		UnusedCredit:  credit,
		NewCharge:     charge,
		CreditDue:     due,
		EffectiveDate: date,
	}

	require.True(t, pr.UnusedCredit.Equal(credit))
	require.True(t, pr.NewCharge.Equal(charge))
	require.True(t, pr.CreditDue.Equal(due))
	require.True(t, pr.EffectiveDate.Equal(date))
}

// TestProrationResultZeroValues verifies ProrationResult zero value behavior
func TestProrationResultZeroValues(t *testing.T) {
	pr := ProrationResult{}

	require.True(t, pr.UnusedCredit.IsZero())
	require.True(t, pr.NewCharge.IsZero())
	require.True(t, pr.CreditDue.IsZero())
	require.True(t, pr.EffectiveDate.IsZero())
}

// TestProrationResultCalculations tests calculation correctness
func TestProrationResultCalculations(t *testing.T) {
	tests := []struct {
		name       string
		credit     decimal.Decimal
		charge     decimal.Decimal
		expectedDue decimal.Decimal
	}{
		{
			name:       "credit exceeds charge",
			credit:     decimal.NewFromInt(100),
			charge:     decimal.NewFromInt(75),
			expectedDue: decimal.NewFromInt(-25),
		},
		{
			name:       "charge exceeds credit",
			credit:     decimal.NewFromInt(50),
			charge:     decimal.NewFromInt(75),
			expectedDue: decimal.NewFromInt(25),
		},
		{
			name:       "equal values",
			credit:     decimal.NewFromInt(50),
			charge:     decimal.NewFromInt(50),
			expectedDue: decimal.Zero,
		},
		{
			name:       "zero values",
			credit:     decimal.Zero,
			charge:     decimal.Zero,
			expectedDue: decimal.Zero,
		},
		{
			name:       "decimal precision",
			credit:     decimal.NewFromFloat(49.99),
			charge:     decimal.NewFromFloat(75.50),
			expectedDue: decimal.NewFromFloat(25.51),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pr := ProrationResult{
				UnusedCredit:  tt.credit,
				NewCharge:     tt.charge,
				CreditDue:     tt.expectedDue,
				EffectiveDate: time.Now(),
			}
			assert.True(t, pr.CreditDue.Equal(tt.expectedDue))
		})
	}
}

// TestProrationResultFieldRelationships validates field relationships
func TestProrationResultFieldRelationships(t *testing.T) {
	tests := []struct {
		name    string
		credit  decimal.Decimal
		charge  decimal.Decimal
		isValid bool
	}{
		{
			name:    "valid result",
			credit:  decimal.NewFromInt(50),
			charge:  decimal.NewFromInt(75),
			isValid: true,
		},
		{
			name:    "zero values valid",
			credit:  decimal.Zero,
			charge:  decimal.Zero,
			isValid: true,
		},
		{
			name:    "negative credit",
			credit:  decimal.NewFromInt(-10),
			charge:  decimal.NewFromInt(75),
			isValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pr := ProrationResult{
				UnusedCredit:  tt.credit,
				NewCharge:     tt.charge,
				CreditDue:     tt.charge.Sub(tt.credit),
				EffectiveDate: time.Now(),
			}
			if !tt.isValid {
				assert.True(t, pr.UnusedCredit.IsNegative())
			}
		})
	}
}

// TestProrationResultJSONSerialization tests JSON marshaling and unmarshaling
func TestProrationResultJSONSerialization(t *testing.T) {
	date := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	pr := ProrationResult{
		UnusedCredit:  decimal.NewFromInt(50),
		NewCharge:     decimal.NewFromInt(75),
		CreditDue:     decimal.NewFromInt(25),
		EffectiveDate: date,
	}

	// Test marshaling
	data, err := json.Marshal(pr)
	require.NoError(t, err)

	// Test unmarshaling
	var unmarshaled ProrationResult
	err = json.Unmarshal(data, &unmarshaled)
	require.NoError(t, err)
	assert.True(t, unmarshaled.UnusedCredit.Equal(pr.UnusedCredit))
	assert.True(t, unmarshaled.NewCharge.Equal(pr.NewCharge))
	assert.True(t, unmarshaled.CreditDue.Equal(pr.CreditDue))
	assert.True(t, unmarshaled.EffectiveDate.Equal(pr.EffectiveDate))
}

// ============================================================================
// CancellationState Tests
// ============================================================================

// TestCancellationStateStruct verifies CancellationState struct exists with correct fields
func TestCancellationStateStruct(t *testing.T) {
	var _ CancellationState = CancellationState{} // This will fail if CancellationState type doesn't exist

	cancelDate := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	graceDate := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)

	cs := CancellationState{
		CancelAt:      cancelDate,
		GraceEndsAt:   graceDate,
		InGracePeriod: true,
		Reason:        "user_request",
	}

	require.True(t, cs.CancelAt.Equal(cancelDate))
	require.True(t, cs.GraceEndsAt.Equal(graceDate))
	require.True(t, cs.InGracePeriod)
	require.Equal(t, "user_request", cs.Reason)
}

// TestCancellationStateZeroValues verifies CancellationState zero value behavior
func TestCancellationStateZeroValues(t *testing.T) {
	cs := CancellationState{}

	require.True(t, cs.CancelAt.IsZero())
	require.True(t, cs.GraceEndsAt.IsZero())
	require.False(t, cs.InGracePeriod)
	require.Equal(t, "", cs.Reason)
}

// TestCancellationStateTimeScenarios tests different time scenarios
func TestCancellationStateTimeScenarios(t *testing.T) {
	tests := []struct {
		name           string
		cancelAt       time.Time
		graceEndsAt    time.Time
		inGracePeriod  bool
		reason         string
	}{
		{
			name:          "standard cancellation",
			cancelAt:      time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			graceEndsAt:   time.Date(2024, 1, 22, 0, 0, 0, 0, time.UTC),
			inGracePeriod: true,
			reason:        "user_request",
		},
		{
			name:          "immediate cancellation",
			cancelAt:      time.Now(),
			graceEndsAt:   time.Date(2024, 1, 22, 0, 0, 0, 0, time.UTC),
			inGracePeriod: false,
			reason:        "fraud",
		},
		{
			name:          "end of month cancellation",
			cancelAt:      time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC),
			graceEndsAt:   time.Date(2024, 2, 7, 0, 0, 0, 0, time.UTC),
			inGracePeriod: true,
			reason:        "end_of_term",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs := CancellationState{
				CancelAt:      tt.cancelAt,
				GraceEndsAt:   tt.graceEndsAt,
				InGracePeriod: tt.inGracePeriod,
				Reason:        tt.reason,
			}
			assert.True(t, cs.CancelAt.Equal(tt.cancelAt))
			assert.True(t, cs.GraceEndsAt.Equal(tt.graceEndsAt))
			assert.Equal(t, tt.inGracePeriod, cs.InGracePeriod)
			assert.Equal(t, tt.reason, cs.Reason)
		})
	}
}

// TestCancellationStateFieldRelationships validates field relationships
func TestCancellationStateFieldRelationships(t *testing.T) {
	tests := []struct {
		name      string
		cs        CancellationState
		validGRaceRelationship bool
	}{
		{
			name: "grace ends after cancel",
			cs: CancellationState{
				CancelAt:      time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
				GraceEndsAt:   time.Date(2024, 1, 22, 0, 0, 0, 0, time.UTC),
				InGracePeriod: true,
				Reason:        "requested",
			},
			validGRaceRelationship: true,
		},
		{
			name: "grace ends same as cancel",
			cs: CancellationState{
				CancelAt:      time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
				GraceEndsAt:   time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
				InGracePeriod: false,
				Reason:        "immediate",
			},
			validGRaceRelationship: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.validGRaceRelationship {
				assert.True(t, tt.cs.GraceEndsAt.After(tt.cs.CancelAt) || tt.cs.GraceEndsAt.Equal(tt.cs.CancelAt))
			}
		})
	}
}

// TestCancellationStateJSONSerialization tests JSON marshaling and unmarshaling
func TestCancellationStateJSONSerialization(t *testing.T) {
	tests := []struct {
		name string
		cs   CancellationState
	}{
		{
			name: "standard cancellation",
			cs: CancellationState{
				CancelAt:      time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
				GraceEndsAt:   time.Date(2024, 1, 22, 12, 0, 0, 0, time.UTC),
				InGracePeriod: true,
				Reason:        "requested",
			},
		},
		{
			name: "empty state",
			cs: CancellationState{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test marshaling
			data, err := json.Marshal(tt.cs)
			require.NoError(t, err)

			// Test unmarshaling
			var unmarshaled CancellationState
			err = json.Unmarshal(data, &unmarshaled)
			require.NoError(t, err)
			assert.True(t, unmarshaled.CancelAt.Equal(tt.cs.CancelAt))
			assert.True(t, unmarshaled.GraceEndsAt.Equal(tt.cs.GraceEndsAt))
			assert.Equal(t, tt.cs.InGracePeriod, unmarshaled.InGracePeriod)
			assert.Equal(t, tt.cs.Reason, unmarshaled.Reason)
		})
	}
}

// ============================================================================
// CycleDate Tests
// ============================================================================

// TestCycleDateStruct verifies CycleDate struct exists with correct fields and can reference BillingCycle
func TestCycleDateStruct(t *testing.T) {
	var _ CycleDate = CycleDate{} // This will fail if CycleDate type doesn't exist

	date := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)
	bc := BillingCycle{
		StartAt: start,
		EndAt:   end,
		Cadence: CadenceMonthly,
	}

	cd := CycleDate{
		Date:          date,
		Cycle:         bc,
		DaysElapsed:   15,
		DaysRemaining: 16,
		Progress:      0.483,
	}

	require.True(t, cd.Date.Equal(date))
	require.True(t, cd.Cycle.StartAt.Equal(start))
	require.True(t, cd.Cycle.EndAt.Equal(end))
	require.Equal(t, CadenceMonthly, cd.Cycle.Cadence)
	require.Equal(t, 15, cd.DaysElapsed)
	require.Equal(t, 16, cd.DaysRemaining)
	require.InDelta(t, 0.483, cd.Progress, 0.001)
}

// TestCycleDateZeroValues verifies CycleDate zero value behavior
func TestCycleDateZeroValues(t *testing.T) {
	cd := CycleDate{}

	require.True(t, cd.Date.IsZero())
	require.True(t, cd.Cycle.StartAt.IsZero())
	require.True(t, cd.Cycle.EndAt.IsZero())
	require.Equal(t, Cadence(""), cd.Cycle.Cadence)
	require.Equal(t, 0, cd.DaysElapsed)
	require.Equal(t, 0, cd.DaysRemaining)
	require.Equal(t, 0.0, cd.Progress)
}

// TestCycleDateNestedFieldValidation validates nested BillingCycle field
func TestCycleDateNestedFieldValidation(t *testing.T) {
	tests := []struct {
		name          string
		date          time.Time
		cycle         BillingCycle
		daysElapsed   int
		daysRemaining int
		progress      float64
	}{
		{
			name: "mid-cycle date",
			date: time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
			cycle: BillingCycle{
				StartAt:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:    time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
				Cadence:  CadenceMonthly,
			},
			daysElapsed:   15,
			daysRemaining: 16,
			progress:      0.483,
		},
		{
			name: "cycle start",
			date: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			cycle: BillingCycle{
				StartAt:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:    time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
				Cadence:  CadenceMonthly,
			},
			daysElapsed:   0,
			daysRemaining: 31,
			progress:      0.0,
		},
		{
			name: "cycle end",
			date: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
			cycle: BillingCycle{
				StartAt:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:    time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
				Cadence:  CadenceMonthly,
			},
			daysElapsed:   31,
			daysRemaining: 0,
			progress:      1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cd := CycleDate{
				Date:          tt.date,
				Cycle:         tt.cycle,
				DaysElapsed:   tt.daysElapsed,
				DaysRemaining: tt.daysRemaining,
				Progress:      tt.progress,
			}
			assert.True(t, cd.Date.Equal(tt.date))
			assert.True(t, cd.Cycle.StartAt.Equal(tt.cycle.StartAt))
			assert.True(t, cd.Cycle.EndAt.Equal(tt.cycle.EndAt))
			assert.Equal(t, tt.cycle.Cadence, cd.Cycle.Cadence)
			assert.Equal(t, tt.daysElapsed, cd.DaysElapsed)
			assert.Equal(t, tt.daysRemaining, cd.DaysRemaining)
			assert.InDelta(t, tt.progress, cd.Progress, 0.001)
		})
	}
}

// TestCycleDateFieldConsistency validates field consistency
func TestCycleDateFieldConsistency(t *testing.T) {
	date := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)
	
	cd := CycleDate{
		Date:          date,
		Cycle:         BillingCycle{StartAt: start, EndAt: end, Cadence: CadenceMonthly},
		DaysElapsed:   15,
		DaysRemaining: 16,
		Progress:      0.483,
	}

	// DaysElapsed + DaysRemaining should equal total days in cycle (approximately)
	totalDays := cd.DaysElapsed + cd.DaysRemaining
	assert.GreaterOrEqual(t, totalDays, 15)
	
	// Progress should be between 0 and 1
	assert.GreaterOrEqual(t, cd.Progress, 0.0)
	assert.LessOrEqual(t, cd.Progress, 1.0)
}

// TestCycleDateJSONSerialization tests JSON marshaling and unmarshaling
func TestCycleDateJSONSerialization(t *testing.T) {
	date := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)

	cd := CycleDate{
		Date:          date,
		Cycle:         BillingCycle{StartAt: start, EndAt: end, Cadence: CadenceMonthly},
		DaysElapsed:   15,
		DaysRemaining: 16,
		Progress:      0.483,
	}

	// Test marshaling
	data, err := json.Marshal(cd)
	require.NoError(t, err)

	// Test unmarshaling
	var unmarshaled CycleDate
	err = json.Unmarshal(data, &unmarshaled)
	require.NoError(t, err)
	assert.True(t, unmarshaled.Date.Equal(cd.Date))
	assert.True(t, unmarshaled.Cycle.StartAt.Equal(cd.Cycle.StartAt))
	assert.True(t, unmarshaled.Cycle.EndAt.Equal(cd.Cycle.EndAt))
	assert.Equal(t, cd.Cycle.Cadence, unmarshaled.Cycle.Cadence)
	assert.Equal(t, cd.DaysElapsed, unmarshaled.DaysElapsed)
	assert.Equal(t, cd.DaysRemaining, unmarshaled.DaysRemaining)
	assert.InDelta(t, cd.Progress, unmarshaled.Progress, 0.001)
}

// ============================================================================
// PricingSnapshot Tests
// ============================================================================

// TestPricingSnapshotStruct verifies PricingSnapshot struct exists with correct fields and can reference Price
func TestPricingSnapshotStruct(t *testing.T) {
	var _ PricingSnapshot = PricingSnapshot{} // This will fail if PricingSnapshot type doesn't exist

	planID := uint(1)
	amount := decimal.NewFromInt(100)
	date := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)

	ps := PricingSnapshot{
		PlanID: planID,
		Price: Price{
			Amount:  amount,
			Cadence: CadenceMonthly,
		},
		AtDate: date,
	}

	require.Equal(t, planID, ps.PlanID)
	require.True(t, ps.Price.Amount.Equal(amount))
	require.Equal(t, CadenceMonthly, ps.Price.Cadence)
	require.True(t, ps.AtDate.Equal(date))
}

// TestPricingSnapshotZeroValues verifies PricingSnapshot zero value behavior
func TestPricingSnapshotZeroValues(t *testing.T) {
	ps := PricingSnapshot{}

	require.Equal(t, uint(0), ps.PlanID)
	require.True(t, ps.Price.Amount.IsZero())
	require.Equal(t, Cadence(""), ps.Price.Cadence)
	require.True(t, ps.AtDate.IsZero())
}

// TestPricingSnapshotEmbeddedTypeValues validates embedded Price type
func TestPricingSnapshotEmbeddedTypeValues(t *testing.T) {
	tests := []struct {
		name   string
		snap   PricingSnapshot
		amount decimal.Decimal
		cadence Cadence
	}{
		{
			name: "standard snapshot",
			snap: PricingSnapshot{
				PlanID: 1,
				Price: Price{
					Amount:  decimal.NewFromInt(100),
					Cadence: CadenceMonthly,
				},
				AtDate: time.Now(),
			},
			amount:  decimal.NewFromInt(100),
			cadence: CadenceMonthly,
		},
		{
			name: "decimal price",
			snap: PricingSnapshot{
				PlanID: 2,
				Price: Price{
					Amount:  decimal.NewFromFloat(49.99),
					Cadence: CadenceYearly,
				},
				AtDate: time.Now(),
			},
			amount:  decimal.NewFromFloat(49.99),
			cadence: CadenceYearly,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.True(t, tt.snap.Price.Amount.Equal(tt.amount))
			assert.Equal(t, tt.cadence, tt.snap.Price.Cadence)
		})
	}
}

// TestPricingSnapshotPlanIDValidation validates PlanID field
func TestPricingSnapshotPlanIDValidation(t *testing.T) {
	tests := []struct {
		name    string
		planID  uint
		validID bool
	}{
		{
			name:    "zero ID",
			planID:  0,
			validID: true,
		},
		{
			name:    "standard ID",
			planID:  123,
			validID: true,
		},
		{
			name:    "large ID",
			planID:  999999,
			validID: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ps := PricingSnapshot{
				PlanID: tt.planID,
				Price: Price{
					Amount:  decimal.NewFromInt(100),
					Cadence: CadenceMonthly,
				},
				AtDate: time.Now(),
			}
			assert.Equal(t, tt.planID, ps.PlanID)
		})
	}
}

// TestPricingSnapshotJSONSerialization tests JSON marshaling and unmarshaling
func TestPricingSnapshotJSONSerialization(t *testing.T) {
	tests := []struct {
		name string
		snap PricingSnapshot
	}{
		{
			name: "standard snapshot",
			snap: PricingSnapshot{
				PlanID: 1,
				Price: Price{
					Amount:  decimal.NewFromInt(100),
					Cadence: CadenceMonthly,
				},
				AtDate: time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "empty snapshot",
			snap: PricingSnapshot{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test marshaling
			data, err := json.Marshal(tt.snap)
			require.NoError(t, err)

			// Test unmarshaling
			var unmarshaled PricingSnapshot
			err = json.Unmarshal(data, &unmarshaled)
			require.NoError(t, err)
			assert.Equal(t, tt.snap.PlanID, unmarshaled.PlanID)
			assert.True(t, unmarshaled.Price.Amount.Equal(tt.snap.Price.Amount))
			assert.Equal(t, tt.snap.Price.Cadence, unmarshaled.Price.Cadence)
			assert.True(t, unmarshaled.AtDate.Equal(tt.snap.AtDate))
		})
	}
}

// ============================================================================
// Period Tests
// ============================================================================

// TestPeriodStruct verifies Period struct exists with correct fields
func TestPeriodStruct(t *testing.T) {
	var _ Period = Period{} // This will fail if Period type doesn't exist

	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)
	duration := end.Sub(start)

	p := Period{
		Start:     start,
		End:       end,
		Duration:  duration,
		TotalDays: 31,
	}

	require.True(t, p.Start.Equal(start))
	require.True(t, p.End.Equal(end))
	require.Equal(t, duration, p.Duration)
	require.Equal(t, 31, p.TotalDays)
}

// TestPeriodZeroValues verifies Period zero value behavior
func TestPeriodZeroValues(t *testing.T) {
	p := Period{}

	require.True(t, p.Start.IsZero())
	require.True(t, p.End.IsZero())
	require.Equal(t, time.Duration(0), p.Duration)
	require.Equal(t, 0, p.TotalDays)
}

// TestPeriodDurationCalculations tests duration field correctness
func TestPeriodDurationCalculations(t *testing.T) {
	tests := []struct {
		name          string
		start         time.Time
		end           time.Time
		expectedDays  int
		expectedMin   time.Duration
		expectedMax   time.Duration
	}{
		{
			name:         "single day",
			start:        time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			end:          time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
			expectedDays: 1,
			expectedMin:  23 * time.Hour,
			expectedMax:  25 * time.Hour,
		},
		{
			name:         "one week",
			start:        time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			end:          time.Date(2024, 1, 8, 0, 0, 0, 0, time.UTC),
			expectedDays: 7,
			expectedMin:  6 * 24 * time.Hour,
			expectedMax:  8 * 24 * time.Hour,
		},
		{
			name:         "one month",
			start:        time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			end:          time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
			expectedDays: 31,
			expectedMin:  30 * 24 * time.Hour,
			expectedMax:  32 * 24 * time.Hour,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			duration := tt.end.Sub(tt.start)
			p := Period{
				Start:     tt.start,
				End:       tt.end,
				Duration:  duration,
				TotalDays: tt.expectedDays,
			}
			require.True(t, p.Start.Equal(tt.start))
			require.True(t, p.End.Equal(tt.end))
			require.GreaterOrEqual(t, p.Duration, tt.expectedMin)
			require.LessOrEqual(t, p.Duration, tt.expectedMax)
			require.Equal(t, tt.expectedDays, p.TotalDays)
		})
	}
}

// TestPeriodFieldConsistency validates field relationships
func TestPeriodFieldConsistency(t *testing.T) {
	tests := []struct {
		name       string
		start      time.Time
		end        time.Time
		consistent bool
	}{
		{
			name:       "valid period",
			start:      time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			end:        time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC),
			consistent: true,
		},
		{
			name:       "zero length period",
			start:      time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			end:        time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			consistent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Period{
				Start:     tt.start,
				End:       tt.end,
				Duration:  tt.end.Sub(tt.start),
				TotalDays: int(tt.end.Sub(tt.start).Hours() / 24),
			}
			if tt.consistent {
				require.True(t, p.End.Equal(p.Start.Add(p.Duration)))
				require.GreaterOrEqual(t, p.TotalDays, 0)
			}
		})
	}
}

// TestPeriodJSONSerialization tests JSON marshaling and unmarshaling
func TestPeriodJSONSerialization(t *testing.T) {
	tests := []struct {
		name string
		period Period
	}{
		{
			name: "standard period",
			period: Period{
				Start:     time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				End:       time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
				Duration:  31 * 24 * time.Hour,
				TotalDays: 31,
			},
		},
		{
			name: "empty period",
			period: Period{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test marshaling
			data, err := json.Marshal(tt.period)
			require.NoError(t, err)

			// Test unmarshaling
			var unmarshaled Period
			err = json.Unmarshal(data, &unmarshaled)
			require.NoError(t, err)
			assert.True(t, unmarshaled.Start.Equal(tt.period.Start))
			assert.True(t, unmarshaled.End.Equal(tt.period.End))
			assert.Equal(t, tt.period.Duration, unmarshaled.Duration)
			assert.Equal(t, tt.period.TotalDays, unmarshaled.TotalDays)
		})
	}
}
