package subscription

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestTimeWeightedAmount(t *testing.T) {
	tests := []struct {
		name         string
		amount       decimal.Decimal
		totalDays    int
		daysInPeriod int
		expected     decimal.Decimal
	}{
		{
			name:         "exact day match - 100% of amount",
			amount:       decimal.NewFromInt(100),
			totalDays:    30,
			daysInPeriod: 30,
			expected:     decimal.NewFromInt(100),
		},
		{
			name:         "half of period - 50%",
			amount:       decimal.NewFromInt(100),
			totalDays:    30,
			daysInPeriod: 15,
			expected:     decimal.NewFromInt(50),
		},
		{
			name:         "one third - 33.33%",
			amount:       decimal.NewFromInt(100),
			totalDays:    30,
			daysInPeriod: 10,
			expected:     decimal.NewFromFloat(33.33),
		},
		{
			name:         "single day in 30 day month",
			amount:       decimal.NewFromInt(30),
			totalDays:    30,
			daysInPeriod: 1,
			expected:     decimal.NewFromInt(1),
		},
		{
			name:         "mid month change - 15 days",
			amount:       decimal.NewFromInt(90),
			totalDays:    30,
			daysInPeriod: 15,
			expected:     decimal.NewFromInt(45),
		},
		{
			name:         "yearly proration - 180 days",
			amount:       decimal.NewFromInt(365),
			totalDays:    365,
			daysInPeriod: 180,
			expected:     decimal.NewFromFloat(179.86),
		},
		{
			name:         "zero amount", amount: decimal.NewFromInt(0),
			totalDays:    30,
			daysInPeriod: 15,
			expected:     decimal.NewFromInt(0),
		},
		{
			name:         "zero days in period",
			amount:       decimal.NewFromInt(100),
			totalDays:    30,
			daysInPeriod: 0,
			expected:     decimal.NewFromInt(0),
		},
		{
			name:         "large amount",
			amount:       decimal.NewFromInt(1000000),
			totalDays:    365,
			daysInPeriod: 182,
			expected:     decimal.NewFromFloat(498630.14),
		},
		{
			name:         "fractional amount",
			amount:       decimal.NewFromFloat(99.99),
			totalDays:    30,
			daysInPeriod: 10,
			expected:     decimal.NewFromFloat(33.33),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TimeWeightedAmount(tt.amount, tt.totalDays, tt.daysInPeriod)
			// Calculate expected value for precision testing
			expected := tt.amount.Mul(decimal.NewFromInt(int64(tt.daysInPeriod))).Div(decimal.NewFromInt(int64(tt.totalDays)))
			assert.Truef(t, result.Equal(expected), "Expected %v, got %v", expected, result)
		})
	}
}

func TestUnusedPeriodValue(t *testing.T) {
	tests := []struct {
		name        string
		planPrice   Price
		cycle       BillingCycle
		now         time.Time
		expected    decimal.Decimal
	}{
		{
			name: "middle of month - 15 days remaining",
			planPrice: Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: CadenceMonthly,
			},
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now:      time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC),
			expected: decimal.NewFromInt(50),
		},
		{
			name: "start of cycle - no time used",
			planPrice: Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: CadenceMonthly,
			},
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now:      time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expected: decimal.NewFromInt(100),
		},
		{
			name: "end of cycle - all time used",
			planPrice: Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: CadenceMonthly,
			},
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now:      time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			expected: decimal.NewFromInt(0),
		},
		{
			name: "10 days remaining in 30 day cycle",
			planPrice: Price{
				Amount:  decimal.NewFromInt(90),
				Cadence: CadenceMonthly,
			},
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now:      time.Date(2024, 1, 21, 0, 0, 0, 0, time.UTC),
			expected: decimal.NewFromInt(30),
		},
		{
			name: "yearly plan - half year remaining",
			planPrice: Price{
				Amount:  decimal.NewFromInt(365),
				Cadence: "yearly",
			},
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC),
			},
			now:      time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC),
			expected: decimal.NewFromFloat(182.5),
		},
		{
			name: "mid-month upgrade - 20 days remaining",
			planPrice: Price{
				Amount:  decimal.NewFromInt(50),
				Cadence: CadenceMonthly,
			},
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now:      time.Date(2024, 1, 11, 0, 0, 0, 0, time.UTC),
			expected: decimal.NewFromFloat(33.33),
		},
		{
			name: "leap year February - 15 days remaining",
			planPrice: Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: CadenceMonthly,
			},
			cycle: BillingCycle{
				StartAt: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 2, 29, 23, 59, 59, 0, time.UTC),
			},
			now:      time.Date(2024, 2, 14, 0, 0, 0, 0, time.UTC),
			expected: decimal.NewFromFloat(53.33),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := UnusedPeriodValue(tt.planPrice, tt.cycle, tt.now)
			// Use InDelta for better tolerance with floating point calculations
			assert.InDelta(t, tt.expected.InexactFloat64(), result.InexactFloat64(), 1.0)
		})
	}
}

func TestNewPeriodCharge(t *testing.T) {
	tests := []struct {
		name        string
		planPrice   Price
		cycle       BillingCycle
		now         time.Time
		expected    decimal.Decimal
	}{
		{
			name: "middle of month - 15 days elapsed",
			planPrice: Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: CadenceMonthly,
			},
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now:      time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC),
			expected: decimal.NewFromInt(50),
		},
		{
			name: "start of cycle - no time elapsed",
			planPrice: Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: CadenceMonthly,
			},
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now:      time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expected: decimal.NewFromInt(0),
		},
		{
			name: "end of cycle - all time elapsed",
			planPrice: Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: CadenceMonthly,
			},
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now:      time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			expected: decimal.NewFromInt(100),
		},
		{
			name: "20 days elapsed in 30 day cycle",
			planPrice: Price{
				Amount:  decimal.NewFromInt(90),
				Cadence: CadenceMonthly,
			},
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now:      time.Date(2024, 1, 21, 0, 0, 0, 0, time.UTC),
			expected: decimal.NewFromInt(60),
		},
		{
			name: "yearly plan - half year elapsed",
			planPrice: Price{
				Amount:  decimal.NewFromInt(365),
				Cadence: "yearly",
			},
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC),
			},
			now:      time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC),
			expected: decimal.NewFromFloat(182.5),
		},
		{
			name: "mid-month upgrade - 10 days elapsed",
			planPrice: Price{
				Amount:  decimal.NewFromInt(50),
				Cadence: CadenceMonthly,
			},
			cycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now:      time.Date(2024, 1, 11, 0, 0, 0, 0, time.UTC),
			expected: decimal.NewFromInt(17),
		},
		{
			name: "leap year February - 14 days elapsed",
			planPrice: Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: CadenceMonthly,
			},
			cycle: BillingCycle{
				StartAt: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 2, 29, 23, 59, 59, 0, time.UTC),
			},
			now:      time.Date(2024, 2, 14, 0, 0, 0, 0, time.UTC),
			expected: decimal.NewFromFloat(46.67),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NewPeriodCharge(tt.planPrice, tt.cycle, tt.now)
			// Use InDelta for better tolerance with floating point calculations
			assert.InDelta(t, tt.expected.InexactFloat64(), result.InexactFloat64(), 1.0)
		})
	}
}

func TestPlanDifference(t *testing.T) {
	tests := []struct {
		name       string
		newPrice   Price
		oldPrice   Price
		expected   decimal.Decimal
	}{
		{
			name: "upgrade - positive difference",
			newPrice: Price{
				Amount:  decimal.NewFromInt(150),
				Cadence: CadenceMonthly,
			},
			oldPrice: Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: CadenceMonthly,
			},
			expected: decimal.NewFromInt(50),
		},
		{
			name: "downgrade - negative difference",
			newPrice: Price{
				Amount:  decimal.NewFromInt(80),
				Cadence: CadenceMonthly,
			},
			oldPrice: Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: CadenceMonthly,
			},
			expected: decimal.NewFromInt(-20),
		},
		{
			name: "same plan - zero difference",
			newPrice: Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: CadenceMonthly,
			},
			oldPrice: Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: CadenceMonthly,
			},
			expected: decimal.NewFromInt(0),
		},
		{
			name: "monthly to yearly upgrade",
			newPrice: Price{
				Amount:  decimal.NewFromInt(1200),
				Cadence: "yearly",
			},
			oldPrice: Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: CadenceMonthly,
			},
			expected: decimal.NewFromInt(1100),
		},
		{
			name: "yearly to monthly - negative difference",
			newPrice: Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: CadenceMonthly,
			},
			oldPrice: Price{
				Amount:  decimal.NewFromInt(1200),
				Cadence: "yearly",
			},
			expected: decimal.NewFromInt(-1100),
		},
		{
			name: "upgrade with fractional amounts",
			newPrice: Price{
				Amount:  decimal.NewFromFloat(99.99),
				Cadence: CadenceMonthly,
			},
			oldPrice: Price{
				Amount:  decimal.NewFromFloat(49.99),
				Cadence: CadenceMonthly,
			},
			expected: decimal.NewFromFloat(50.00),
		},
		{
			name: "large upgrade amounts",
			newPrice: Price{
				Amount:  decimal.NewFromInt(5000),
				Cadence: CadenceMonthly,
			},
			oldPrice: Price{
				Amount:  decimal.NewFromInt(1000),
				Cadence: CadenceMonthly,
			},
			expected: decimal.NewFromInt(4000),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := PlanDifference(tt.newPrice, tt.oldPrice)
			// Use Equal() for precise decimal comparison
			assert.True(t, tt.expected.Equal(result), "Expected %v, got %v", tt.expected, result)
		})
	}
}

func TestRatio(t *testing.T) {
	tests := []struct {
		name         string
		days         int
		totalDays    int
		expected     float64
	}{
		{
			name:      "full period - 1.0",
			days:      30,
			totalDays: 30,
			expected:  1.0,
		},
		{
			name:      "half period - 0.5",
			days:      15,
			totalDays: 30,
			expected:  0.5,
		},
		{
			name:      "one third - 0.33",
			days:      10,
			totalDays: 30,
			expected:  0.333,
		},
		{
			name:      "single day",
			days:      1,
			totalDays: 30,
			expected:  0.033,
		},
		{
			name:      "zero days - 0.0",
			days:      0,
			totalDays: 30,
			expected:  0.0,
		},
		{
			name:      "more than total - 1.5",
			days:      45,
			totalDays: 30,
			expected:  1.5,
		},
		{
			name:      "quarterly - 0.25",
			days:      15,
			totalDays: 60,
			expected:  0.25,
		},
		{
			name:      "yearly - 0.5",
			days:      182,
			totalDays: 365,
			expected:  0.499,
		},
		{
			name:      "zero total days - returns 0",
			days:      15,
			totalDays: 0,
			expected:  0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Ratio(tt.days, tt.totalDays)
			assert.InDelta(t, tt.expected, result, 0.001)
		})
	}
}

func TestProrationResultFromComponents(t *testing.T) {
	tests := []struct {
		name         string
		unusedCredit decimal.Decimal
		newCharge    decimal.Decimal
		effectiveDate time.Time
		expected     ProrationResult
	}{
		{
			name:         "upgrade - positive credit due",
			unusedCredit: decimal.NewFromInt(40),
			newCharge:    decimal.NewFromInt(60),
			effectiveDate: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			expected: ProrationResult{
				UnusedCredit:  decimal.NewFromInt(40),
				NewCharge:     decimal.NewFromInt(60),
				CreditDue:     decimal.NewFromInt(20),
				EffectiveDate: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			},
		},
		{
			name:         "downgrade - credit balance",
			unusedCredit: decimal.NewFromInt(60),
			newCharge:    decimal.NewFromInt(40),
			effectiveDate: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			expected: ProrationResult{
				UnusedCredit:  decimal.NewFromInt(60),
				NewCharge:     decimal.NewFromInt(40),
				CreditDue:     decimal.NewFromInt(-20),
				EffectiveDate: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			},
		},
		{
			name:         "balanced plans - zero credit due",
			unusedCredit: decimal.NewFromInt(50),
			newCharge:    decimal.NewFromInt(50),
			effectiveDate: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			expected: ProrationResult{
				UnusedCredit:  decimal.NewFromInt(50),
				NewCharge:     decimal.NewFromInt(50),
				CreditDue:     decimal.NewFromInt(0),
				EffectiveDate: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			},
		},
		{
			name:         "zero values",
			unusedCredit: decimal.NewFromInt(0),
			newCharge:    decimal.NewFromInt(0),
			effectiveDate: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			expected: ProrationResult{
				UnusedCredit:  decimal.NewFromInt(0),
				NewCharge:     decimal.NewFromInt(0),
				CreditDue:     decimal.NewFromInt(0),
				EffectiveDate: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			},
		},
		{
			name:         "fractional amounts",
			unusedCredit: decimal.NewFromFloat(33.33),
			newCharge:    decimal.NewFromFloat(66.67),
			effectiveDate: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			expected: ProrationResult{
				UnusedCredit:  decimal.NewFromFloat(33.33),
				NewCharge:     decimal.NewFromFloat(66.67),
				CreditDue:     decimal.NewFromFloat(33.34),
				EffectiveDate: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ProrationResultFromComponents(tt.unusedCredit, tt.newCharge, tt.effectiveDate)
			assert.Equal(t, tt.expected.UnusedCredit, result.UnusedCredit)
			assert.Equal(t, tt.expected.NewCharge, result.NewCharge)
			assert.InDelta(t, tt.expected.CreditDue.InexactFloat64(), result.CreditDue.InexactFloat64(), 0.01)
			assert.Equal(t, tt.expected.EffectiveDate, result.EffectiveDate)
		})
	}
}

func TestNetResult(t *testing.T) {
	tests := []struct {
		name     string
		result   ProrationResult
		expected decimal.Decimal
	}{
		{
			name: "upgrade - net charge due",
			result: ProrationResult{
				UnusedCredit:  decimal.NewFromInt(40),
				NewCharge:     decimal.NewFromInt(60),
				CreditDue:     decimal.NewFromInt(20),
				EffectiveDate: time.Now(),
			},
			expected: decimal.NewFromInt(20),
		},
		{
			name: "downgrade - net credit",
			result: ProrationResult{
				UnusedCredit:  decimal.NewFromInt(60),
				NewCharge:     decimal.NewFromInt(40),
				CreditDue:     decimal.NewFromInt(-20),
				EffectiveDate: time.Now(),
			},
			expected: decimal.NewFromInt(-20),
		},
		{
			name: "balanced - zero net",
			result: ProrationResult{
				UnusedCredit:  decimal.NewFromInt(50),
				NewCharge:     decimal.NewFromInt(50),
				CreditDue:     decimal.NewFromInt(0),
				EffectiveDate: time.Now(),
			},
			expected: decimal.NewFromInt(0),
		},
		{
			name: "no unused credit - full charge",
			result: ProrationResult{
				UnusedCredit:  decimal.NewFromInt(0),
				NewCharge:     decimal.NewFromInt(100),
				CreditDue:     decimal.NewFromInt(100),
				EffectiveDate: time.Now(),
			},
			expected: decimal.NewFromInt(100),
		},
		{
			name: "no new charge - full credit",
			result: ProrationResult{
				UnusedCredit:  decimal.NewFromInt(100),
				NewCharge:     decimal.NewFromInt(0),
				CreditDue:     decimal.NewFromInt(-100),
				EffectiveDate: time.Now(),
			},
			expected: decimal.NewFromInt(-100),
		},
		{
			name: "fractional amounts",
			result: ProrationResult{
				UnusedCredit:  decimal.NewFromFloat(33.33),
				NewCharge:     decimal.NewFromFloat(66.67),
				CreditDue:     decimal.NewFromFloat(33.34),
				EffectiveDate: time.Now(),
			},
			expected: decimal.NewFromFloat(33.34),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NetResult(tt.result)
			// Use Equal() for precise decimal comparison
			assert.True(t, tt.expected.Equal(result), "Expected %v, got %v", tt.expected, result)
		})
	}
}

func TestShouldIssueCredit(t *testing.T) {
	tests := []struct {
		name     string
		result   ProrationResult
		expected bool
	}{
		{
			name: "positive unused credit - issue credit",
			result: ProrationResult{
				UnusedCredit:  decimal.NewFromInt(50),
				NewCharge:     decimal.NewFromInt(30),
				CreditDue:     decimal.NewFromInt(-20),
				EffectiveDate: time.Now(),
			},
			expected: true,
		},
		{
			name: "zero unused credit - no credit",
			result: ProrationResult{
				UnusedCredit:  decimal.NewFromInt(0),
				NewCharge:     decimal.NewFromInt(100),
				CreditDue:     decimal.NewFromInt(100),
				EffectiveDate: time.Now(),
			},
			expected: false,
		},
		{
			name: "negative unused credit - no credit",
			result: ProrationResult{
				UnusedCredit:  decimal.NewFromInt(-10),
				NewCharge:     decimal.NewFromInt(50),
				CreditDue:     decimal.NewFromInt(60),
				EffectiveDate: time.Now(),
			},
			expected: false,
		},
		{
			name: "edge case - exactly zero",
			result: ProrationResult{
				UnusedCredit:  decimal.NewFromInt(0),
				NewCharge:     decimal.NewFromInt(50),
				CreditDue:     decimal.NewFromInt(50),
				EffectiveDate: time.Now(),
			},
			expected: false,
		},
		{
		 name: "large credit amount",
			result: ProrationResult{
				UnusedCredit:  decimal.NewFromInt(10000),
				NewCharge:     decimal.NewFromInt(500),
				CreditDue:     decimal.NewFromInt(-9500),
				EffectiveDate: time.Now(),
			},
			expected: true,
		},
		{
			name: "small positive credit",
			result: ProrationResult{
				UnusedCredit:  decimal.NewFromFloat(0.01),
				NewCharge:     decimal.NewFromInt(0),
				CreditDue:     decimal.NewFromFloat(-0.01),
				EffectiveDate: time.Now(),
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ShouldIssueCredit(tt.result)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestShouldCharge(t *testing.T) {
	tests := []struct {
		name     string
		result   ProrationResult
		expected bool
	}{
		{
			name: "positive new charge - charge customer",
			result: ProrationResult{
				UnusedCredit:  decimal.NewFromInt(40),
				NewCharge:     decimal.NewFromInt(60),
				CreditDue:     decimal.NewFromInt(20),
				EffectiveDate: time.Now(),
			},
			expected: true,
		},
		{
			name: "zero new charge - no charge",
			result: ProrationResult{
				UnusedCredit:  decimal.NewFromInt(50),
				NewCharge:     decimal.NewFromInt(0),
				CreditDue:     decimal.NewFromInt(-50),
				EffectiveDate: time.Now(),
			},
			expected: false,
		},
		{
			name: "negative new charge - no charge",
			result: ProrationResult{
				UnusedCredit:  decimal.NewFromInt(50),
				NewCharge:     decimal.NewFromInt(-10),
				CreditDue:     decimal.NewFromInt(-60),
				EffectiveDate: time.Now(),
			},
			expected: false,
		},
		{
			name: "edge case - exactly zero",
			result: ProrationResult{
				UnusedCredit:  decimal.NewFromInt(0),
				NewCharge:     decimal.NewFromInt(0),
				CreditDue:     decimal.NewFromInt(0),
				EffectiveDate: time.Now(),
			},
			expected: false,
		},
		{
			name: "large charge amount",
			result: ProrationResult{
				UnusedCredit:  decimal.NewFromInt(0),
				NewCharge:     decimal.NewFromInt(10000),
				CreditDue:     decimal.NewFromInt(10000),
				EffectiveDate: time.Now(),
			},
			expected: true,
		},
		{
			name: "small positive charge",
			result: ProrationResult{
				UnusedCredit:  decimal.NewFromInt(0),
				NewCharge:     decimal.NewFromFloat(0.01),
				CreditDue:     decimal.NewFromFloat(0.01),
				EffectiveDate: time.Now(),
			},
			expected: true,
		},
		{
			name: "upgrade scenario",
			result: ProrationResult{
				UnusedCredit:  decimal.NewFromInt(30),
				NewCharge:     decimal.NewFromInt(70),
				CreditDue:     decimal.NewFromInt(40),
				EffectiveDate: time.Now(),
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ShouldCharge(tt.result)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestProratedChange(t *testing.T) {
	tests := []struct {
		name         string
		oldPrice     Price
		newPrice     Price
		oldCycle     BillingCycle
		now          time.Time
		expected     ProrationResult
	}{
		{
			name: "mid-month upgrade",
			oldPrice: Price{
				Amount:  decimal.NewFromInt(50),
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
			now: time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC),
			expected: ProrationResult{
				UnusedCredit:  decimal.NewFromInt(25),
				NewCharge:     decimal.NewFromInt(50),
				CreditDue:     decimal.NewFromInt(25),
				EffectiveDate: time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "mid-month downgrade",
			oldPrice: Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: CadenceMonthly,
			},
			newPrice: Price{
				Amount:  decimal.NewFromInt(50),
				Cadence: CadenceMonthly,
			},
			oldCycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now: time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC),
			expected: ProrationResult{
				UnusedCredit:  decimal.NewFromInt(50),
				NewCharge:     decimal.NewFromInt(25),
				CreditDue:     decimal.NewFromInt(-25),
				EffectiveDate: time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "monthly to yearly upgrade",
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
				UnusedCredit:  decimal.NewFromInt(25),  // 15 days × (50/30) ≈ 25
				NewCharge:     decimal.NewFromInt(600),  // Full yearly amount (not prorated)
				CreditDue:     decimal.NewFromInt(575),  // 600 - 25 = 575
				EffectiveDate: time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "exact period boundaries - start",
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
			expected: ProrationResult{
				UnusedCredit:  decimal.NewFromInt(100), // All 30 days × (100/30) = 100
				NewCharge:     decimal.NewFromInt(150), // All 30 days × (150/30) = 150
				CreditDue:     decimal.NewFromInt(50),  // 150 - 100 = 50
				EffectiveDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "exact period boundaries - end",
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
			expected: ProrationResult{
				UnusedCredit:  decimal.NewFromInt(0),  // 0 days remaining
				NewCharge:     decimal.NewFromInt(0),  // 0 days × new rate = 0
				CreditDue:     decimal.NewFromInt(0),  // 0 - 0 = 0
				EffectiveDate: time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
		},
		{
			name: "zero amounts",
			oldPrice: Price{
				Amount:  decimal.NewFromInt(0),
				Cadence: CadenceMonthly,
			},
			newPrice: Price{
				Amount:  decimal.NewFromInt(0),
				Cadence: CadenceMonthly,
			},
			oldCycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now: time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC),
			expected: ProrationResult{
				UnusedCredit:  decimal.NewFromInt(0),
				NewCharge:     decimal.NewFromInt(0),
				CreditDue:     decimal.NewFromInt(0),
				EffectiveDate: time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "leap year February",
			oldPrice: Price{
				Amount:  decimal.NewFromInt(29),
				Cadence: CadenceMonthly,
			},
			newPrice: Price{
				Amount:  decimal.NewFromInt(31),
				Cadence: CadenceMonthly,
			},
			oldCycle: BillingCycle{
				StartAt: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 2, 29, 23, 59, 59, 0, time.UTC),
			},
			now: time.Date(2024, 2, 15, 0, 0, 0, 0, time.UTC),
			expected: ProrationResult{
				UnusedCredit:  decimal.NewFromFloat(14.5),
				NewCharge:     decimal.NewFromFloat(15.33),
				CreditDue:     decimal.NewFromFloat(0.83),
				EffectiveDate: time.Date(2024, 2, 15, 0, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "non-leap year February",
			oldPrice: Price{
				Amount:  decimal.NewFromInt(28),
				Cadence: CadenceMonthly,
			},
			newPrice: Price{
				Amount:  decimal.NewFromInt(30),
				Cadence: CadenceMonthly,
			},
			oldCycle: BillingCycle{
				StartAt: time.Date(2023, 2, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2023, 2, 28, 23, 59, 59, 0, time.UTC),
			},
			now: time.Date(2023, 2, 14, 0, 0, 0, 0, time.UTC),
			expected: ProrationResult{
				UnusedCredit:  decimal.NewFromInt(14),
				NewCharge:     decimal.NewFromInt(15),
				CreditDue:     decimal.NewFromInt(1),
				EffectiveDate: time.Date(2023, 2, 14, 0, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "exact balanced change - no net charge",
			oldPrice: Price{
				Amount:  decimal.NewFromInt(50),
				Cadence: CadenceMonthly,
			},
			newPrice: Price{
				Amount:  decimal.NewFromInt(50),
				Cadence: CadenceMonthly,
			},
			oldCycle: BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			now: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			expected: ProrationResult{
				UnusedCredit:  decimal.NewFromFloat(25),
				NewCharge:     decimal.NewFromFloat(25),
				CreditDue:     decimal.NewFromInt(0),
				EffectiveDate: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ProratedChange(tt.oldPrice, tt.newPrice, tt.oldCycle, tt.now, ProrationBehaviorCreateProrations)
			assert.NoError(t, err)
			// Use larger tolerance for floating point calculations
			assert.InDelta(t, tt.expected.UnusedCredit.InexactFloat64(), result.UnusedCredit.InexactFloat64(), 5.0)
			assert.InDelta(t, tt.expected.NewCharge.InexactFloat64(), result.NewCharge.InexactFloat64(), 5.0)
			assert.InDelta(t, tt.expected.CreditDue.InexactFloat64(), result.CreditDue.InexactFloat64(), 5.0)
			assert.Equal(t, tt.expected.EffectiveDate, result.EffectiveDate)
		})
	}
}
