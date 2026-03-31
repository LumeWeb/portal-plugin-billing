// Package subscription provides billing cycle calculation utilities for subscription management.
package subscription

import "time"

// DaysInCycle calculates the total number of days in a billing cycle.
//
// This is a pure function that simply delegates to DaysBetween, using
// the start and end dates from the provided BillingCycle.
//
// Parameters:
//   - cycle: The billing cycle for which to calculate total days.
//
// Returns:
//   - The total number of days in the cycle between cycle.StartAt and cycle.EndAt.
func DaysInCycle(cycle BillingCycle) int {
	return DaysBetween(cycle.StartAt, cycle.EndAt)
}

// DaysRemaining calculates the number of days remaining until the end of a billing cycle.
//
// This is a pure function that computes the time difference between the provided
// "now" timestamp and the cycle's end date using DaysBetween.
//
// Parameters:
//   - cycle: The billing cycle for which to calculate remaining days.
//   - now: The current timestamp to measure from.
//
// Returns:
//   - The number of days remaining until cycle.EndAt. May be negative if now is
//     after the cycle's end date.
func DaysRemaining(cycle BillingCycle, now time.Time) int {
	return DaysBetween(now, cycle.EndAt)
}

// DaysElapsed calculates the number of days that have passed since the start of a billing cycle.
//
// This is a pure function that computes the time difference between the cycle's start date
// and the provided "now" timestamp using DaysBetween.
//
// Parameters:
//   - cycle: The billing cycle for which to calculate elapsed days.
//   - now: The current timestamp to measure from.
//
// Returns:
//   - The number of days elapsed since cycle.StartAt. May be negative if now is
//     before the cycle's start date.
func DaysElapsed(cycle BillingCycle, now time.Time) int {
	return DaysBetween(cycle.StartAt, now)
}

// CycleProgress calculates the progress of a billing cycle as a value between 0 and 1.
//
// This is a pure function that returns the fraction of the billing cycle that has
// elapsed relative to the provided "now" timestamp. It ensures the result is bounded
// between 0 (before cycle start) and 1 (after cycle end).
//
// Behavior:
//   - Returns 0 if now is before or at the cycle's start date.
//   - Returns 0 if the cycle duration is zero (invalid cycle, division by zero protection).
//   - Returns 1 if now is at or after the cycle's end date.
//   - Otherwise, returns the fraction of elapsed days to total cycle days.
//
// Parameters:
//   - cycle: The billing cycle for which to calculate progress.
//   - now: The timestamp to calculate progress at.
//
// Returns:
//   - A value between 0 and 1 representing the fraction of cycle completion.
func CycleProgress(cycle BillingCycle, now time.Time) float64 {
	// If now is before or at the start, progress is 0
	if now.Before(cycle.StartAt) || now.Equal(cycle.StartAt) {
		return 0
	}

	// Calculate total days in cycle first
	totalDays := DaysInCycle(cycle)
	if totalDays == 0 {
		// Avoid division by zero for zero-duration cycles
		return 0
	}

	// If now is at or after the end, progress is 1
	if now.After(cycle.EndAt) || now.Equal(cycle.EndAt) {
		return 1
	}

	// Calculate progress as elapsed days divided by total days
	elapsedDays := DaysElapsed(cycle, now)
	return float64(elapsedDays) / float64(totalDays)
}

// CycleContainsTime checks whether a given time falls within a billing cycle.
//
// This is a pure function that returns true if the provided time is within the
// cycle's boundaries, including times that exactly match either the start or end
// dates.
//
// Parameters:
//   - cycle: The billing cycle to check against.
//   - t: The time to check.
//
// Returns:
//   - true if t is at or after cycle.StartAt and at or before cycle.EndAt.
//   - false otherwise.
func CycleContainsTime(cycle BillingCycle, t time.Time) bool {
	return (t.After(cycle.StartAt) || t.Equal(cycle.StartAt)) &&
		(t.Before(cycle.EndAt) || t.Equal(cycle.EndAt))
}

// CycleAtDate creates a CycleDate representing the state of a billing cycle at a specific point in time.
//
// This is a pure function that aggregates all relevant cycle information (progress, elapsed days,
// remaining days) into a single struct for the requested date. It does not modify the provided
// cycle or time.
//
// The returned CycleDate contains:
//   - The requested date
//   - The billing cycle this date falls within
//   - The number of days elapsed since cycle start at the requested date
//   - The number of days remaining until cycle end at the requested date
//   - The progress value (0.0 to 1.0) of the cycle at the requested date
//
// Parameters:
//   - cycle: The billing cycle to analyze.
//   - t: The specific date and time for which to calculate the cycle state.
//
// Returns:
//   - A CycleDate struct containing all relevant information about the cycle's state at time t.
func CycleAtDate(cycle BillingCycle, t time.Time) CycleDate {
	return CycleDate{
		Date:          t,
		Cycle:         cycle,
		DaysElapsed:   DaysElapsed(cycle, t),
		DaysRemaining: DaysRemaining(cycle, t),
		Progress:      CycleProgress(cycle, t),
	}
}

// OverlapsCycle checks whether two billing cycles overlap in time.
//
// This is a pure function that determines if there is any time intersection between
// the two provided cycles, including boundaries.
//
// Algorithm:
//   - Two cycles overlap if cycle A has not ended before cycle B starts, and
//     cycle B has not ended before cycle A starts.
//   - This is the standard interval overlap check for half-open intervals.
//
// Parameters:
//   - a: The first billing cycle to check.
//   - b: The second billing cycle to check.
//
// Returns:
//   - true if cycles a and b overlap in time.
//   - false otherwise.
//
// Examples:
//   - If a = [Jan 1, Jan 31] and b = [Jan 15, Feb 15], returns true (overlap).
//   - If a = [Jan 1, Jan 15] and b = [Jan 16, Jan 31], returns false (no overlap).
//   - If a = [Jan 1, Jan 31] and b = [Jan 31, Feb 28], returns true (boundary overlap).
func OverlapsCycle(a, b BillingCycle) bool {
	return !a.EndAt.Before(b.StartAt) && !b.EndAt.Before(a.StartAt)
}

// CalculateNextRenewal returns the date when the billing cycle renews.
//
// This is a pure function that returns the end date of the billing cycle,
// which represents when the next renewal should occur based on the provided
// cycle's end date.
//
// Parameters:
//   - cycle: The billing cycle for which to calculate the renewal date.
//
// Returns:
//   - The renewal date (cycle.EndAt) when the billing cycle is due to renew.
func CalculateNextRenewal(cycle BillingCycle) time.Time {
	return cycle.EndAt
}

// CalculateFirstCycle creates the first billing cycle starting from a given date and cadence.
//
// This is a pure function that constructs a BillingCycle with the start date set to the provided
// startDate and the end date calculated by adding one billing period using the specified cadence.
//
// Parameters:
//   - startDate: The start date and time for the billing cycle.
//   - cadence: The billing cadence.
//
// Returns:
//   - A BillingCycle with StartAt set to startDate and EndAt set to one billing period later.
//   - Returns a zero-initialized BillingCycle if the cadence method fails.
func CalculateFirstCycle(startDate time.Time, cadence Cadence) BillingCycle {
	endDate, err := cadence.AddTo(startDate)
	if err != nil {
		return BillingCycle{}
	}
	return BillingCycle{
		StartAt: startDate,
		EndAt:   endDate,
		Cadence: cadence,
	}
}

// CalculateNextCycle calculates the subsequent billing cycle from a given cycle.
//
// This is a pure function that returns a new BillingCycle where the start date is the end date
// of the current cycle, and the end date is one billing period after the current cycle's end date,
// using the same cadence. This allows for constructing a sequence of consecutive billing cycles.
//
// This function uses the Cadence.AddTo method to advance the end date by one period based on the
// cycle's cadence. If the method returns an error, this function returns a zero-initialized BillingCycle.
//
// Parameters:
//   - currentCycle: The billing cycle for which to calculate the next cycle.
//
// Returns:
//   - A new BillingCycle with:
//     - StartAt set to currentCycle.EndAt
//     - EndAt set to one billing period after currentCycle.EndAt
//     - Cadence set to currentCycle.Cadence
//   - Returns a zero-initialized BillingCycle if the AddTo method fails.
func CalculateNextCycle(currentCycle BillingCycle) BillingCycle {
	endDate, err := currentCycle.Cadence.AddTo(currentCycle.EndAt)
	if err != nil {
		return BillingCycle{}
	}
	return BillingCycle{
		StartAt: currentCycle.EndAt,
		EndAt:   endDate,
		Cadence: currentCycle.Cadence,
	}
}
