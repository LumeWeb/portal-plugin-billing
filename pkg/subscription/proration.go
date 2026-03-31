// Package subscription provides domain types and utilities for subscription billing cycles,
// pricing, and date/time calculations.
package subscription

import (
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

// ProrationBehavior represents how proration is handled for subscription changes.
//
// Matches Stripe's proration_behavior parameter for subscription updates.
type ProrationBehavior string

const (
	// ProrationBehaviorNone disables proration. No credits are created for unused time,
	// and the customer is charged full amounts immediately. For cross-cadence changes,
	// this means no credit for unused old plan time but full charge for new plan.
	ProrationBehaviorNone ProrationBehavior = "none"
	
	// ProrationBehaviorCreateProrations creates proration items when applicable.
	// Proration items are added to the next invoice (or immediately for cross-cadence changes).
	// This is the default Stripe behavior.
	ProrationBehaviorCreateProrations ProrationBehavior = "create_prorations"
	
	// ProrationBehaviorAlwaysInvoice creates prorations and immediately invoices the customer,
	// attempting payment collection regardless of billing cycle requirements.
	ProrationBehaviorAlwaysInvoice ProrationBehavior = "always_invoice"
)

// TimeWeightedAmount calculates the time-weighted amount for a prorated period.
//
// It computes the proportion of the total amount based on the number of days in the
// billing cycle versus the total days in the billing period. This is useful for
// calculating prorated charges or credits when a subscription changes mid-cycle.
//
// The formula used is: (amount * daysInPeriod) / totalDays
//
// This is a pure function with no side effects.
//
// Parameters:
//   - amount: The total monetary amount to be prorated.
//   - totalDays: The total number of days in the billing period (must be > 0).
//   - daysInPeriod: The number of days in the billing cycle within the period.
//
// Returns:
//   - The time-weighted amount calculated as (amount * daysInPeriod) / totalDays.
func TimeWeightedAmount(amount decimal.Decimal, totalDays, daysInPeriod int) decimal.Decimal {
	if totalDays == 0 || daysInPeriod == 0 {
		return decimal.Zero
	}
	return amount.Mul(decimal.NewFromInt(int64(daysInPeriod))).Div(decimal.NewFromInt(int64(totalDays)))
}

// UnusedPeriodValue calculates the unused portion of a billing period's price.
//
// This is a pure function that computes the proportional value of the unused time
// remaining in a billing cycle at a specific point in time. It calculates how much
// of the plan's price should be credited back based on the days remaining in the
// current billing cycle.
//
// The calculation uses time-weighted proration: the plan price is multiplied by the
// fraction of days remaining relative to the total days in the cycle. This is useful
// for calculating refunds or credits when a subscription is cancelled mid-cycle.
//
// Parameters:
//   - planPrice: The price of the plan including amount and cadence.
//   - cycle: The billing cycle for which to calculate unused value.
//   - now: The current timestamp to calculate unused value from.
//
// Returns:
//   - The time-weighted amount representing the unused portion of the billing period.
func UnusedPeriodValue(planPrice Price, cycle BillingCycle, now time.Time) decimal.Decimal {
	return TimeWeightedAmount(planPrice.Amount, DaysInCycle(cycle), DaysRemaining(cycle, now))
}

// NewPeriodCharge calculates the charge for a partial billing period based on elapsed time.
//
// This is a pure function that computes the proportional charge for the portion of a
// billing cycle that has already passed. It calculates how much of the plan's price
// has already been "consumed" by the subscription at a specific point in time.
//
// The calculation uses time-weighted proration: the plan price is multiplied by the
// fraction of days elapsed relative to the total days in the cycle. This is useful
// for calculating charges for partial periods, mid-cycle upgrades, or prorated billing.
//
// Parameters:
//   - planPrice: The price of the plan including amount and cadence.
//   - cycle: The billing cycle for which to calculate the period charge.
//   - now: The current timestamp to calculate elapsed time from.
//
// Returns:
//   - The time-weighted amount representing the charge for the elapsed portion of the billing period.
func NewPeriodCharge(planPrice Price, cycle BillingCycle, now time.Time) decimal.Decimal {
	return TimeWeightedAmount(planPrice.Amount, DaysInCycle(cycle), DaysInCycle(cycle)-DaysRemaining(cycle, now))
}

// PlanDifference calculates the difference between two plan prices.
//
// This is a pure function that computes the difference between a new plan price
// and an old plan price. It can be positive (upgrade), negative (downgrade), or zero.
// This is useful for calculating proration amounts when subscription plans change.
//
// Parameters:
//   - newPrice: The price of the new plan.
//   - oldPrice: The price of the old plan.
//
// Returns:
//   - The difference between new and old prices as a decimal value.
func PlanDifference(newPrice, oldPrice Price) decimal.Decimal {
	return newPrice.Amount.Sub(oldPrice.Amount)
}

// Ratio calculates the ratio of days to totalDays as a float64.
//
// This is a pure utility function for proration calculations. It converts both inputs
// to float64 and returns their division. When totalDays is zero, returns zero to avoid
// division by zero.
//
// Common use cases:
//   - Determining the percentage of a billing period that has elapsed
//   - Calculating proration factors for subscription changes
//   - Computing time-based weights for billing calculations
//
// Parameters:
//   - days: The numerator (number of days elapsed or in period).
//   - totalDays: The denominator (total days in period, must be > 0 for meaningful results).
//
// Returns:
//   - The ratio as a float64: days / totalDays. Returns 0 when totalDays is 0.
func Ratio(days, totalDays int) float64 {
	if totalDays == 0 {
		return 0
	}
	return float64(days) / float64(totalDays)
}

// ProrationTimeRatio calculates the precise ratio of elapsed time to total cycle duration.
//
// This function uses exact time differences (seconds) rather than truncated whole days,
// matching Stripe's proration behavior which calculates to the second precision.
//
// Parameters:
//   - elapsed: The amount of time that has passed.
//   - totalDuration: The total duration of the billing period.
//
// Returns:
//   - A value between 0 and 1 representing the fraction of time elapsed.
//   - Returns 0 if totalDuration is zero to avoid division by zero.
func ProrationTimeRatio(elapsed, totalDuration time.Duration) float64 {
	if totalDuration == 0 {
		return 0
	}
	return float64(elapsed) / float64(totalDuration)
}

// ProrationAmountByTime calculates a prorated amount based on time precision.
//
// This function uses the exact time remaining in a billing cycle to calculate the prorated
// amount, matching Stripe's behavior which calculates prorations to the second. This is
// more accurate than day-based calculations because it accounts for partial days.
//
// The formula used is: amount * (remainingDuration / totalDuration)
//
// Parameters:
//   - amount: The total amount to be prorated.
//   - totalDuration: The total duration of the billing cycle.
//   - remainingDuration: The duration remaining in the billing cycle.
//
// Returns:
//   - The prorated amount based on the exact time ratio.
func ProrationAmountByTime(amount decimal.Decimal, totalDuration, remainingDuration time.Duration) decimal.Decimal {
	if totalDuration == 0 || remainingDuration == 0 {
		return decimal.Zero
	}
	ratio := decimal.NewFromFloat(float64(remainingDuration) / float64(totalDuration))
	return amount.Mul(ratio)
}


// ProrationResultFromComponents constructs a proration result from its component parts.
//
// This is a pure function that builds a ProrationResult from an unused credit amount,
// a new charge amount, and an effective date. It computes CreditDue as the difference
// between new charges and available unused credit.
//
// This is useful for constructing proration results when the component values have
// been calculated separately, or for testing and validation purposes.
//
// Note: Stripe creates invoice line items directly (negative amounts for credits, 
// positive for charges) on the same invoice. Credits offset charges directly on the 
// invoice total, and only unused credits after offset go to customer balance.
//
// Parameters:
//   - unusedCredit: The amount of unused credit from the previous plan.
//   - newCharge: The total charge for the new plan prorated to the remaining cycle time.
//   - effectiveDate: When the proration takes effect.
//
// Returns:
//   - A ProrationResult with CreditDue computed as newCharge minus unusedCredit.
func ProrationResultFromComponents(unusedCredit, newCharge decimal.Decimal, effectiveDate time.Time) ProrationResult {
	return ProrationResult{
		UnusedCredit:  unusedCredit,
		NewCharge:     newCharge,
		CreditDue:     newCharge.Sub(unusedCredit),
		EffectiveDate: effectiveDate,
	}
}

// NetResult calculates the net result of a proration operation.
//
// This is a pure function that computes the net amount by subtracting the unused
// credit from the new charge. This represents the final net amount to be charged
// after accounting for any credits from the previous plan.
//
// Parameters:
//   - result: The proration result containing unused credit and new charge amounts.
//
// Returns:
//   - The net amount: NewCharge minus UnusedCredit.
func NetResult(result ProrationResult) decimal.Decimal {
	return result.NewCharge.Sub(result.UnusedCredit)
}

// ShouldIssueCredit determines whether a credit should be issued based on the proration result.
//
// This is a pure function that evaluates whether unused credit from the previous plan
// exceeds zero, indicating that a credit should be issued to the customer. This is useful
// for determining when to process refunds or credit adjustments during plan changes or cancellations.
//
// The function checks if there is any positive unused credit amount that should be returned
// to the customer as a credit balance or refund.
//
// Parameters:
//   - result: The proration result containing the unused credit amount.
//
// Returns:
//   - true if unused credit is greater than zero, false otherwise.
func ShouldIssueCredit(result ProrationResult) bool {
	return result.UnusedCredit.GreaterThan(decimal.Zero)
}

// ShouldCharge determines whether a charge should be applied based on the proration result.
//
// This is a pure function that evaluates whether the new charge amount from the plan
// change exceeds zero, indicating that a payment should be collected from the customer.
// This is useful for determining when to process payments or invoices during plan upgrades.
//
// The function checks if there is any positive new charge amount that should be billed
// to the customer, after accounting for any unused credit from the previous plan.
//
// Parameters:
//   - result: The proration result containing the new charge amount.
//
// Returns:
//   - true if new charge is greater than zero, false otherwise.
func ShouldCharge(result ProrationResult) bool {
	return result.NewCharge.GreaterThan(decimal.Zero)
}

// ValidateProrationInputs validates inputs for proration calculations.
//
// Returns an error if inputs are invalid, allowing callers to handle edge cases.
func ValidateProrationInputs(oldPrice, newPrice Price, oldCycle BillingCycle, now time.Time) error {
	// Check for zero amounts
	if oldPrice.Amount.LessThan(decimal.Zero) {
		return fmt.Errorf("old price amount cannot be negative")
	}
	if newPrice.Amount.LessThan(decimal.Zero) {
		return fmt.Errorf("new price amount cannot be negative")
	}
	
	// Check cycle boundaries
	if oldCycle.StartAt.IsZero() || oldCycle.EndAt.IsZero() {
		return fmt.Errorf("billing cycle must have valid start and end dates")
	}
	if oldCycle.StartAt.After(oldCycle.EndAt) {
		return fmt.Errorf("billing cycle start date cannot be after end date")
	}
	
	// Check if now is within cycle
	if now.Before(oldCycle.StartAt) || now.After(oldCycle.EndAt) {
		return fmt.Errorf("proration date must be within billing cycle")
	}
	
	return nil
}

// ProratedChange calculates the prorated billing when changing plans mid-cycle.
//
// This implementation follows Stripe's calendar-accurate proration model:
//
// Same Cadence (e.g., monthly → monthly):
//   - Credits: Time-prorated old unused time using exact time precision
//   - Charges: Time-prorated new plan rate for remaining time
//   - Preserves original billing cycle anchor
//   - Example: Monthly $100→$150 with 15 days remaining in 30-day month
//     Credit: $100 × (15/30) = $50
//     Charge: $150 × (15/30) = $75
//     Net: $25 (same regardless of exact cycle length due to proportional calculation)
//
// Different Cadence (e.g., monthly → yearly):
//   - Credits: Time-prorated old unused time using exact time precision
//   - Charges: FULL new plan amount immediately (not time-prorated by remaining time)
//   - New billing cycle starts at upgrade time
//   - Example: Monthly $50→Yearly $600 with 15 days remaining in 30-day month
//     Credit: $50 × (15/30) = $25
//     Charge: $600 (full yearly amount)
//     Net: $575
//     Next bill: Upgrade date + 365 days
//
// Billing Period Calculation:
//   - Uses exact time precision (seconds) for proration calculations
//   - Accounts for calendar variations (Jan 31 → Feb 28, leap years, etc.)
//   - Provides accurate proration based on real cycle duration
//   - Matches Stripe's proration behavior which calculates to the second precision
//
// Invoice Line Items (Not Customer Balance):
//   - Credits are invoice line items (negative amounts)
//   - Charges are invoice line items (positive amounts)
//   - Credits offset charges directly on invoice total
//   - Only unused credits after offset go to customer.balance
//
// Proration Behavior:
//   - None: No credits, full charges (if applicable)
//   - CreateProrations: Credits + charges as appropriate
//   - AlwaysInvoice: Credits + charges + immediate invoice
//
// See: https://docs.stripe.com/api/subscriptions/update
//
// Parameters:
//   - oldPrice: The price of the old plan being changed from.
//   - newPrice: The price of the new plan being changed to.
//   - oldCycle: The billing cycle for the old plan.
//   - now: The current timestamp for proration calculations.
//   - behavior: How to handle proration credits and charges.
//
// Returns:
//   - A ProrationResult containing unused credit, new charge, credit due, and effective date.
//   - Error if inputs are invalid.
func ProratedChange(oldPrice, newPrice Price, oldCycle BillingCycle, now time.Time, behavior ProrationBehavior) (ProrationResult, error) {
	// Validate inputs
	if err := ValidateProrationInputs(oldPrice, newPrice, oldCycle, now); err != nil {
		return ProrationResult{}, err
	}
	
	sameCadence := oldPrice.Cadence == newPrice.Cadence
	
	if sameCadence {
		// Same cadence: prorate both for remaining time
		return sameCadenceProration(oldPrice, newPrice, oldCycle, now, behavior), nil
	} else {
		// Different cadence: credit old time, charge full new amount
		return crossCadenceProration(oldPrice, newPrice, oldCycle, now, behavior), nil
	}
}

// sameCadenceProration handles plan changes where old and new cadences match.
//
// Calculates proration using time precision (seconds) to match Stripe's calendar-accurate proration.
// Preserves original billing cycle anchor.
//
// With ProrationBehaviorNone:
//   - No credit for unused time
//   - Charges full new amount at next cycle (not immediate)
//   - Billing cycle anchor preserved
//
// With CreateProrations/AlwaysInvoice:
//   - Credit: Time-prorated old unused time using exact time remaining
//   - Charge: Time-prorated new plan rate for remaining time using exact time remaining
//   - If AlwaysInvoice: charges immediately
func sameCadenceProration(oldPrice, newPrice Price, oldCycle BillingCycle, now time.Time, behavior ProrationBehavior) ProrationResult {
	cycleDuration := oldCycle.EndAt.Sub(oldCycle.StartAt)
	remainingDuration := oldCycle.EndAt.Sub(now)
	
	if behavior == ProrationBehaviorNone {
		// No proration - customer pays full new rate at next cycle
		// No immediate charge, no credit issued
		return ProrationResult{
			UnusedCredit:  decimal.Zero,
			NewCharge:     decimal.Zero, // No immediate charge
			CreditDue:     decimal.Zero,
			EffectiveDate: now,
		}
	}
	
	// Ensure exact zero when remaining time is zero (avoid decimal precision issues)
	if remainingDuration <= 0 {
		return ProrationResultFromComponents(decimal.Zero, decimal.Zero, now)
	}
	
	// Protect against zero cycle duration (invalid cycle)
	if cycleDuration == 0 {
		return ProrationResultFromComponents(decimal.Zero, decimal.Zero, now)
	}
	
	unusedCredit := ProrationAmountByTime(oldPrice.Amount, cycleDuration, remainingDuration)
	newCharge := ProrationAmountByTime(newPrice.Amount, cycleDuration, remainingDuration)
	
	return ProrationResultFromComponents(unusedCredit, newCharge, now)
}

// crossCadenceProration handles plan changes where old and new cadences differ.
//
// This function implements Stripe's behavior for interval changes:
//   - Credit: Time-prorated old unused time at the old daily rate using exact time remaining
//   - Charge: FULL new plan amount immediately (not time-prorated by remaining time)
//   - Billing date resets to NOW
//
// Example: Monthly $50 → Yearly $600, 15 days remaining in 30-day month
//   - Credit: $50 × (15 days/30 days) = $25 (using exact time precision)
//   - Charge: $600 (full yearly amount)
//   - Net: $575
//   - Next billing: Resets to upgrade date + 1 year
//
// With ProrationBehaviorNone:
//   - No credit for unused time
//   - Full new charge immediately
//   - Billing date still resets
//
// With CreateProrations/AlwaysInvoice:
//   - Credit calculated and applied to same invoice
//   - Full new charge on same invoice
//   - Invoice total = new charge - credit
func crossCadenceProration(oldPrice, newPrice Price, oldCycle BillingCycle, now time.Time, behavior ProrationBehavior) ProrationResult {
	// For cross-cadence, always charge immediately regardless of behavior
	// (except for credit calculation)
	
	cycleDuration := oldCycle.EndAt.Sub(oldCycle.StartAt)
	remainingDuration := oldCycle.EndAt.Sub(now)
	
	// Handle ProrationBehaviorNone - no credit for unused time
	if behavior == ProrationBehaviorNone {
		return ProrationResult{
			UnusedCredit:  decimal.Zero,
			NewCharge:    newPrice.Amount, // Full charge
			CreditDue:    newPrice.Amount,
			EffectiveDate: now,
		}
	}
	
	// Ensure exact zero when remaining time is zero (avoid decimal precision issues)
	if remainingDuration <= 0 {
		return ProrationResultFromComponents(decimal.Zero, newPrice.Amount, now)
	}
	
	// Protect against zero cycle duration (invalid cycle)
	if cycleDuration == 0 {
		return ProrationResultFromComponents(decimal.Zero, newPrice.Amount, now)
	}
	
	// Calculate credit using exact time precision matching Stripe's behavior
	unusedCredit := ProrationAmountByTime(oldPrice.Amount, cycleDuration, remainingDuration)
	
	// Charge full new amount immediately (not prorated by time)
	newCharge := newPrice.Amount
	
	return ProrationResultFromComponents(unusedCredit, newCharge, now)
}
