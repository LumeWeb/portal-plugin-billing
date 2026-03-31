// Package subscription provides domain types and utilities for subscription management.
package subscription

import (
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

// Cadence represents a billing frequency.
type Cadence string

// Constant valid values for Cadence.
const (
	CadenceDaily     Cadence = "daily"
	CadenceWeekly    Cadence = "weekly"
	CadenceMonthly   Cadence = "monthly"
	CadenceQuarterly Cadence = "quarterly"
	CadenceYearly    Cadence = "yearly"
	CadenceRolling   Cadence = "rolling"
)

// ParseCadence validates and returns a Cadence from a string.
// Returns an error if the string is not a valid cadence.
func ParseCadence(s string) (Cadence, error) {
	c := Cadence(s)
	switch c {
	case CadenceDaily, CadenceWeekly, CadenceMonthly, CadenceQuarterly, CadenceYearly, CadenceRolling:
		return c, nil
	default:
		return "", fmt.Errorf("invalid cadence: %s", s)
	}
}

// AddTo adds one billing period to the provided time based on the cadence.
// Returns an error if the time period addition fails.
func (c Cadence) AddTo(t time.Time) (time.Time, error) {
	switch c {
	case CadenceDaily:
		return t.AddDate(0, 0, 1), nil
	case CadenceWeekly:
		return t.AddDate(0, 0, 7), nil
	case CadenceMonthly:
		return t.AddDate(0, 1, 0), nil
	case CadenceQuarterly:
		return t.AddDate(0, 3, 0), nil
	case CadenceYearly:
		return t.AddDate(0, 12, 0), nil
	case CadenceRolling:
		return time.Time{}, fmt.Errorf("rolling period requires rolling_days context")
	default:
		return time.Time{}, fmt.Errorf("unsupported cadence: %s", c)
	}
}

// Plan represents a subscription plan with its pricing tier and currency.
type Plan struct {
	// ID uniquely identifies the plan.
	ID uint
	// Name is the human-readable plan name.
	Name string
	// Currency is the ISO 4217 currency code for pricing.
	Currency string
}

// BillingCycle represents the time boundaries and cadence of a billing period.
type BillingCycle struct {
	// StartAt is the start date of the billing cycle.
	StartAt time.Time
	// EndAt is the end date of the billing cycle.
	EndAt time.Time
	// Cadence is the billing frequency.
	Cadence Cadence
}

// Price represents the monetary amount and billing cadence for a plan.
type Price struct {
	// Amount is the monetary value using decimal for precise financial calculations.
	Amount decimal.Decimal
	// Cadence is the billing frequency.
	Cadence Cadence
}

// ProrationResult represents the calculation of credits and charges when changing plans mid-cycle.
type ProrationResult struct {
	// UnusedCredit is the amount of unused credit from the previous plan.
	UnusedCredit decimal.Decimal
	// NewCharge is the total charge for the new plan prorated to the remaining cycle time.
	NewCharge decimal.Decimal
	// CreditDue is the net amount owed after accounting unused credit against new charges.
	CreditDue decimal.Decimal
	// EffectiveDate is when the proration takes effect.
	EffectiveDate time.Time
}

// CancellationState represents the state of a subscription cancellation including grace period status.
type CancellationState struct {
	// CancelAt is the scheduled cancellation date.
	CancelAt time.Time
	// GraceEndsAt is the end of the grace period after cancellation.
	GraceEndsAt time.Time
	// InGracePeriod indicates whether the subscription is currently in a grace period.
	InGracePeriod bool
	// Reason is the cancellation reason.
	Reason string
}

// CycleDate represents a specific date within a billing cycle with progress tracking.
type CycleDate struct {
	// Date is the specific calendar date.
	Date time.Time
	// Cycle is the billing cycle this date falls within.
	Cycle BillingCycle
	// DaysElapsed is the number of days that have passed in the current cycle.
	DaysElapsed int
	// DaysRemaining is the number of days remaining in the current cycle.
	DaysRemaining int
	// Progress is the percentage of cycle completion (0.0 to 1.0).
	Progress float64
}

// PricingSnapshot represents a historical record of a plan's pricing at a specific point in time.
type PricingSnapshot struct {
	// PlanID is the identifier of the plan.
	PlanID uint
	// Price is the pricing information at the time of capture.
	Price Price
	// AtDate is the timestamp when this pricing snapshot was recorded.
	AtDate time.Time
}

// Period represents a time period with start, end, duration, and day count.
type Period struct {
	// Start is the period start time.
	Start time.Time
	// End is the period end time.
	End time.Time
	// Duration is the time span between start and end.
	Duration time.Duration
	// TotalDays is the total number of days in the period.
	TotalDays int
}
