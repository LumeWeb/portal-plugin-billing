package subscription

import (
	"fmt"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tkuchiki/faketime"
)

// simulateMySQLRoundTrip truncates a time.Time to microsecond precision,
// simulating the MySQL TIMESTAMP write→read round-trip.
func simulateMySQLRoundTrip(t time.Time) time.Time {
	return t.Truncate(time.Microsecond)
}

// TestProratedChange_MySQLTimestampPrecision reproduces the timing edge case
// where MySQL TIMESTAMP columns truncate nanosecond precision to microseconds.
//
// When a billing cycle end time is stored (e.g., via CalculateFirstCycle),
// the value has Go's nanosecond precision. MySQL TIMESTAMP truncates to
// microsecond precision (6 decimal places). When proration is calculated
// using time.Now().UTC(), the current time retains full nanosecond precision.
//
// This test verifies that proration still works correctly after the
// microsecond truncation that occurs during a MySQL round-trip.
func TestProratedChange_MySQLTimestampPrecision_MonthlyCycle(t *testing.T) {
	// Freeze time: subscription created at a specific moment
	createdAt := time.Date(2026, 4, 22, 14, 30, 0, 123456789, time.UTC)
	f := faketime.NewFaketimeWithTime(createdAt)
	f.Do()

	cycle := CalculateFirstCycle(time.Now().UTC(), CadenceMonthly)

	// Simulate MySQL TIMESTAMP write→read
	storedEnd := simulateMySQLRoundTrip(cycle.EndAt)

	oldPrice := Price{Amount: decimal.NewFromFloat(49.99), Cadence: CadenceMonthly}
	newPrice := Price{Amount: decimal.NewFromFloat(19.99), Cadence: CadenceMonthly}

	oldCycle := BillingCycle{
		StartAt: cycle.StartAt,
		EndAt:   storedEnd,
		Cadence: CadenceMonthly,
	}

	// Advance time by 1 second (simulate plan change moments after subscription)
	f.Undo()
	f = faketime.NewFaketimeWithTime(createdAt.Add(1 * time.Second))
	f.Do()
	defer f.Undo()

	prorationTime := time.Now().UTC()

	result, err := ProratedChange(oldPrice, newPrice, oldCycle, prorationTime, ProrationBehaviorCreateProrations)
	require.NoError(t, err, "ProratedChange should succeed within a monthly billing cycle after MySQL truncation")
	assert.True(t, result.UnusedCredit.GreaterThan(decimal.Zero))
	assert.True(t, result.NewCharge.GreaterThan(decimal.Zero))
}

// TestProratedChange_MySQLTimestampPrecision_YearlyCycle tests the yearly
// cadence variant, covering cross-cadence downgrade scenarios.
func TestProratedChange_MySQLTimestampPrecision_YearlyCycle(t *testing.T) {
	createdAt := time.Date(2026, 4, 22, 14, 30, 0, 999999999, time.UTC)
	f := faketime.NewFaketimeWithTime(createdAt)
	f.Do()

	cycle := CalculateFirstCycle(time.Now().UTC(), CadenceYearly)
	storedEnd := simulateMySQLRoundTrip(cycle.EndAt)

	oldPrice := Price{Amount: decimal.NewFromFloat(499.99), Cadence: CadenceYearly}
	newPrice := Price{Amount: decimal.NewFromFloat(19.99), Cadence: CadenceMonthly}

	oldCycle := BillingCycle{
		StartAt: cycle.StartAt,
		EndAt:   storedEnd,
		Cadence: CadenceYearly,
	}

	f.Undo()
	f = faketime.NewFaketimeWithTime(createdAt.Add(1 * time.Second))
	f.Do()
	defer f.Undo()

	result, err := ProratedChange(oldPrice, newPrice, oldCycle, time.Now().UTC(), ProrationBehaviorCreateProrations)
	require.NoError(t, err, "Cross-cadence proration should succeed within a yearly billing cycle")
	assert.True(t, result.UnusedCredit.GreaterThan(decimal.Zero))
}

// TestProratedChange_MySQLTimestampPrecision_NanosecondVariants exercises
// different nanosecond values to ensure truncation at any boundary
// doesn't push time.Now() past the stored BillingPeriodEnd.
func TestProratedChange_MySQLTimestampPrecision_NanosecondVariants(t *testing.T) {
	nanoseconds := []int{0, 1, 500, 999, 999999, 999999999}

	for _, ns := range nanoseconds {
		t.Run(fmt.Sprintf("nanosecond_%d", ns), func(t *testing.T) {
			createdAt := time.Date(2026, 6, 15, 10, 0, 0, ns, time.UTC)
			f := faketime.NewFaketimeWithTime(createdAt)
			f.Do()

			cycle := CalculateFirstCycle(time.Now().UTC(), CadenceMonthly)
			storedEnd := simulateMySQLRoundTrip(cycle.EndAt)

			oldCycle := BillingCycle{
				StartAt: cycle.StartAt,
				EndAt:   storedEnd,
				Cadence: CadenceMonthly,
			}

			// Advance 5 seconds
			f.Undo()
			f = faketime.NewFaketimeWithTime(createdAt.Add(5 * time.Second))
			f.Do()
			defer f.Undo()

			err := ValidateProrationInputs(
				Price{Amount: decimal.NewFromInt(100), Cadence: CadenceMonthly},
				Price{Amount: decimal.NewFromInt(50), Cadence: CadenceMonthly},
				oldCycle, time.Now().UTC(),
			)
			assert.NoError(t, err, "validation should pass for any nanosecond boundary after MySQL truncation")
		})
	}
}

// TestProratedChange_EndToEndWorkflow simulates a realistic workflow:
// billing cycle creation → MySQL round-trip → plan change proration.
//
// This uses faketime to control the exact moment of each operation,
// eliminating the non-determinism of real time.Now() calls.
func TestProratedChange_EndToEndWorkflow(t *testing.T) {
	tests := []struct {
		name         string
		oldCadence   Cadence
		newCadence   Cadence
		oldAmount    float64
		newAmount    float64
		createdAt    time.Time
		prorationAt  time.Duration
	}{
		{
			name:        "Enterprise monthly to Pro monthly",
			oldCadence:  CadenceMonthly,
			newCadence:  CadenceMonthly,
			oldAmount:   49.99,
			newAmount:   19.99,
			createdAt:   time.Date(2026, 4, 22, 14, 30, 0, 0, time.UTC),
			prorationAt: 2 * time.Second,
		},
		{
			name:        "Enterprise yearly to Pro monthly (cross-cadence)",
			oldCadence:  CadenceYearly,
			newCadence:  CadenceMonthly,
			oldAmount:   499.99,
			newAmount:   19.99,
			createdAt:   time.Date(2026, 4, 22, 14, 30, 0, 0, time.UTC),
			prorationAt: 2 * time.Second,
		},
		{
			name:        "Pro monthly to Basic monthly",
			oldCadence:  CadenceMonthly,
			newCadence:  CadenceMonthly,
			oldAmount:   19.99,
			newAmount:   9.99,
			createdAt:   time.Date(2026, 1, 31, 23, 59, 58, 0, time.UTC),
			prorationAt: 2 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Step 1: Create billing cycle
			f := faketime.NewFaketimeWithTime(tt.createdAt)
			f.Do()

			cycle := CalculateFirstCycle(time.Now().UTC(), tt.oldCadence)
			storedEnd := simulateMySQLRoundTrip(cycle.EndAt)

			oldCycle := BillingCycle{
				StartAt: cycle.StartAt,
				EndAt:   storedEnd,
				Cadence: tt.oldCadence,
			}

			// Step 2: Calculate proration later in the cycle
			f.Undo()
			prorationTime := tt.createdAt.Add(tt.prorationAt)
			f = faketime.NewFaketimeWithTime(prorationTime)
			f.Do()
			defer f.Undo()

			oldPrice := Price{Amount: decimal.NewFromFloat(tt.oldAmount), Cadence: tt.oldCadence}
			newPrice := Price{Amount: decimal.NewFromFloat(tt.newAmount), Cadence: tt.newCadence}

			result, err := ProratedChange(oldPrice, newPrice, oldCycle, time.Now().UTC(), ProrationBehaviorCreateProrations)
			require.NoError(t, err, "downgrade proration should succeed within billing cycle")
			assert.True(t, result.UnusedCredit.GreaterThan(decimal.Zero), "downgrade should produce unused credit")
		})
	}
}

// TestProratedChange_BoundaryAtCycleEnd verifies behavior when proration
// time lands exactly at BillingPeriodEnd after MySQL truncation.
// With the clamping fix, this should succeed by using EndAt as the timestamp.
func TestProratedChange_BoundaryAtCycleEnd(t *testing.T) {
	cycleStart := time.Date(2026, 4, 22, 14, 30, 0, 0, time.UTC)
	cycleEnd := time.Date(2026, 5, 22, 14, 30, 0, 0, time.UTC)

	oldPrice := Price{Amount: decimal.NewFromInt(49), Cadence: CadenceMonthly}
	newPrice := Price{Amount: decimal.NewFromInt(19), Cadence: CadenceMonthly}

	cycle := BillingCycle{
		StartAt: cycleStart,
		EndAt:   cycleEnd,
		Cadence: CadenceMonthly,
	}

	// Proration at exact cycle end should succeed
	result, err := ProratedChange(oldPrice, newPrice, cycle, cycleEnd, ProrationBehaviorCreateProrations)
	require.NoError(t, err, "proration at exact cycle end should succeed")
	assert.True(t, result.UnusedCredit.IsZero(), "no unused credit at cycle end")
	assert.True(t, result.NewCharge.IsZero(), "no new charge at cycle end")
}

// TestProratedChange_BoundaryJustPastCycleEnd tests that when time.Now().UTC()
// is just barely past BillingPeriodEnd (e.g., due to MySQL truncation or
// clock skew), ValidateProrationInputs correctly rejects it. This demonstrates
// the need for timestamp clamping to ensure proration time is always within
// the billing cycle bounds.
func TestProratedChange_BoundaryJustPastCycleEnd(t *testing.T) {
	cycleStart := time.Date(2026, 4, 22, 14, 30, 0, 0, time.UTC)
	cycleEnd := time.Date(2026, 5, 22, 14, 30, 0, 0, time.UTC)

	oldPrice := Price{Amount: decimal.NewFromInt(49), Cadence: CadenceMonthly}
	newPrice := Price{Amount: decimal.NewFromInt(19), Cadence: CadenceMonthly}

	cycle := BillingCycle{
		StartAt: cycleStart,
		EndAt:   cycleEnd,
		Cadence: CadenceMonthly,
	}

	// 1 nanosecond past cycle end should fail validation
	justPastEnd := cycleEnd.Add(1)
	err := ValidateProrationInputs(oldPrice, newPrice, cycle, justPastEnd)
	require.Error(t, err, "validation should reject proration time past cycle end")
	assert.Contains(t, err.Error(), "proration date must be within billing cycle")
}

// TestProratedChange_MySQLTruncationCreatesApparentExpiry reproduces the
// specific scenario where MySQL's microsecond truncation of BillingPeriodEnd
// causes time.Now() to appear after EndAt, even though the original EndAt
// was in the future.
//
// Given:
//   - BillingPeriodStart = T (with nanosecond X)
//   - BillingPeriodEnd = T.AddDate(0,1,0) (with nanosecond Y)
//   - MySQL truncates EndAt to microsecond → loses up to 999ns
//   - time.Now() returns the original nanosecond-precise time
//   - If "now" happens to equal the un-truncated EndAt, and EndAt was truncated
//     downward by up to 999ns, then now > storedEndAt
//
// However, for a 1-month cycle this requires "now" to be exactly at EndAt,
// which is 1 month after cycle creation. This test shows the mechanism
// even though it requires an extreme time alignment.
func TestProratedChange_MySQLTruncationCreatesApparentExpiry(t *testing.T) {
	// Construct a BillingPeriodEnd whose nanosecond component will be truncated
	// by MySQL, creating a stored value slightly earlier than the computed value.
	start := time.Date(2026, 4, 22, 14, 30, 0, 0, time.UTC)
	endComputed := start.AddDate(0, 1, 0) // 2026-05-22T14:30:00Z

	// Add nanosecond precision that gets lost in MySQL
	endWithNanos := endComputed.Add(999) // Add 999ns
	storedEnd := simulateMySQLRoundTrip(endWithNanos)

	// storedEnd < endWithNanos by 999ns (the truncation loss)
	assert.True(t, storedEnd.Before(endWithNanos),
		"MySQL truncation should make stored EndAt earlier than computed EndAt")

	// If proration time equals the un-truncated end, it exceeds the stored end
	err := ValidateProrationInputs(
		Price{Amount: decimal.NewFromInt(100), Cadence: CadenceMonthly},
		Price{Amount: decimal.NewFromInt(50), Cadence: CadenceMonthly},
		BillingCycle{StartAt: start, EndAt: storedEnd, Cadence: CadenceMonthly},
		endWithNanos,
	)
	require.Error(t, err, "nanosecond-precise now should exceed microsecond-truncated EndAt")
	assert.Contains(t, err.Error(), "proration date must be within billing cycle")

	// Verify that using the truncated (stored) end time would pass validation
	err = ValidateProrationInputs(
		Price{Amount: decimal.NewFromInt(100), Cadence: CadenceMonthly},
		Price{Amount: decimal.NewFromInt(50), Cadence: CadenceMonthly},
		BillingCycle{StartAt: start, EndAt: storedEnd, Cadence: CadenceMonthly},
		storedEnd,
	)
	require.NoError(t, err, "clamped proration time should pass validation")
}
