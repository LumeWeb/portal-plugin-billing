// Package subscription provides cancellation lifecycle utilities for subscription management.
package subscription

import (
	"time"
)

// Cancellation status constants.
// These values represent the different states a subscription can be in
// during the cancellation lifecycle.

const (
	// StatusNotCancelled indicates the subscription is active and has not been cancelled.
	StatusNotCancelled = "not_cancelled"

	// StatusPending indicates a cancellation has been scheduled but not yet taken effect.
	StatusPending = "pending"

	// StatusInGracePeriod indicates the cancellation is active but the subscription
	// is still within its grace period.
	StatusInGracePeriod = "in_grace_period"

	// StatusCancelled indicates the grace period has ended and the subscription
	// is fully cancelled.
	StatusCancelled = "cancelled"
)

// GracePeriodDefault returns the standard grace period duration for subscription cancellations.
// The grace period represents the time after cancellation during which access to service
// continues before full termination. The default is 7 days (7 * 24 hours).
func GracePeriodDefault() time.Duration {
	return 7 * 24 * time.Hour
}

// CalculateGraceEnd calculates the end time of a grace period based on the cancellation time.
// It returns the timestamp when the grace period ends by adding the grace period duration
// to the cancellation time.
func CalculateGraceEnd(cancelAt time.Time, gracePeriod time.Duration) time.Time {
	return cancelAt.Add(gracePeriod)
}

// CancelNow creates a CancellationState with immediate cancellation and
// default grace period settings. This convenience function creates a cancellation
// state for scenarios requiring immediate action while maintaining the standard
// grace period duration. The returned state marks the cancellation as happening
// at the current time, sets the grace period to active, and sets the reason to "immediate".
//
// Returns a CancellationState with:
//   - CancelAt: Set to the current time
//   - InGracePeriod: Set to true
//   - GraceEndsAt: Set to current time plus the default grace period (7 days)
//   - Reason: Set to "immediate"
func CancelNow() CancellationState {
	now := time.Now()
	return CancellationState{
		CancelAt:     now,
		InGracePeriod: true,
		GraceEndsAt:  now.Add(GracePeriodDefault()),
		Reason:       "immediate",
	}
}

// ScheduleCancelAt creates a CancellationState with a specific cancellation date.
// This function allows scheduled cancellations to be set for a future date, optionally
// within a grace period. The returned state marks the cancellation as happening at the
// specified date, sets the grace period to active, calculates the grace end time based
// on the default grace period duration, and sets the reason to "requested".
//
// The cancelDate parameter specifies when the cancellation should take effect.
//
// Returns a CancellationState with:
//   - CancelAt: Set to the provided cancelDate
//   - InGracePeriod: Set to true
//   - GraceEndsAt: Set to cancelDate plus the default grace period (7 days)
//   - Reason: Set to "requested"
func ScheduleCancelAt(cancelDate time.Time) CancellationState {
	return CancellationState{
		CancelAt:      cancelDate,
		InGracePeriod: true,
		GraceEndsAt:   CalculateGraceEnd(cancelDate, GracePeriodDefault()),
		Reason:        "requested",
	}
}

// ScheduleEndOfMonthCancel creates a CancellationState for end-of-month cancellation
// based on a billing cycle. This function schedules cancellation at the end of the
// billing cycle, which is a common pattern for subscription services. The returned state
// marks the cancellation as happening at the cycle's end date, sets the grace period
// to active, calculates the grace end time based on the default grace period duration,
// and sets the reason to "end_of_month".
//
// The cycle parameter provides the billing cycle with its end date used for cancellation.
// The now parameter is the current time, which may be used for validation in future
// implementations.
//
// Returns a CancellationState with:
//   - CancelAt: Set to the billing cycle's end date
//   - InGracePeriod: Set to true
//   - GraceEndsAt: Set to cycle end plus the default grace period (7 days)
//   - Reason: Set to "end_of_month"
func ScheduleEndOfMonthCancel(cycle BillingCycle, now time.Time) CancellationState {
	cancelAt := cycle.EndAt
	graceEnd := CalculateGraceEnd(cancelAt, GracePeriodDefault())
	return CancellationState{
		CancelAt:      cancelAt,
		InGracePeriod: true,
		GraceEndsAt:   graceEnd,
		Reason:        "end_of_month",
	}
}

// IsCancellationComplete determines whether a subscription cancellation has taken effect
// based on the current time and the scheduled cancellation time. This function is useful
// for checking if a subscription is effectively cancelled and should no longer provide
// access to services.
//
// The function returns true if the current time is equal to or after the scheduled
// cancellation time, indicating the cancellation is complete. It returns false if the
// cancellation is still pending (scheduled for a future time).
//
// The state parameter contains the cancellation schedule with the CancelAt field
// indicating when cancellation should take effect. The now parameter is the current
// time for comparison against the scheduled cancellation time.
//
// Returns true if cancellation is complete (now >= CancelAt), false otherwise.
func IsCancellationComplete(state CancellationState, now time.Time) bool {
	return now.After(state.CancelAt) || now.Equal(state.CancelAt)
}

// CancellationDaysRemaining calculates the number of whole days remaining until
// a scheduled cancellation takes effect. This function is useful for displaying
// countdown information to users or for triggering reminder notifications.
//
// The function uses the scheduled cancellation time from the CancellationState
// and compares it against the provided current time. If the cancellation date
// has already passed or is the same day, the function returns 0 to indicate
// no days remain.
//
// The state parameter contains the cancellation schedule with the CancelAt field.
// The now parameter is the current time for calculating the remaining days.
//
// Returns the number of whole days remaining until the cancellation date.
// Returns 0 if the cancellation date is in the past or is the same day as now.
func CancellationDaysRemaining(state CancellationState, now time.Time) int {
	days := DaysBetween(now, state.CancelAt)
	if days < 0 {
		return 0
	}
	return days
}

// GetCancellationStatus determines the current cancellation status of a subscription
// based on the cancellation state and current time. This function evaluates the
// cancellation lifecycle and returns the appropriate status string.
//
// The function uses a series of conditional checks to determine the subscription's
// current state relative to its scheduled cancellation time and grace period:
//   - StatusNotCancelled: When no cancellation is scheduled or CancelAt is zero
//   - StatusPending: When cancellation is scheduled but the cancellation date is in the future
//   - StatusInGracePeriod: When the cancellation has taken effect but the subscription
//     is still within its grace period (now >= CancelAt and now <= GraceEndsAt)
//   - StatusCancelled: When the grace period has ended and the subscription is fully cancelled
//
// The state parameter contains the cancellation schedule including CancelAt and GraceEndsAt.
// The now parameter is the current time for status determination.
//
// Returns a string representing the current cancellation status.
func GetCancellationStatus(state CancellationState, now time.Time) string {
	// If no cancellation is scheduled, the subscription is not cancelled
	if state.CancelAt.IsZero() {
		return StatusNotCancelled
	}

	// If cancellation is scheduled but not yet effective
	if now.Before(state.CancelAt) {
		return StatusPending
	}

	// If grace period has ended, subscription is fully cancelled
	if now.After(state.GraceEndsAt) {
		return StatusCancelled
	}

	// Cancellation is active but still within grace period
	return StatusInGracePeriod
}

// DaysInGracePeriod calculates the number of days remaining in the grace period
// for a cancelled subscription. This function is useful for displaying countdown
// information to users or for triggering notifications as the grace period approaches.
//
// The function calculates the difference between the current time and the grace
// period end time, returning the number of whole days remaining. If the current
// time is after the grace period end time, the function returns 0 to indicate
// the grace period has ended.
//
// The state parameter contains the cancellation state with GraceEndsAt field
// indicating when the grace period ends. The now parameter is the current time
// for calculating remaining days in the grace period.
//
// Returns the number of whole days remaining in the grace period.
// Returns 0 if the grace period has ended (now > GraceEndsAt).
func DaysInGracePeriod(state CancellationState, now time.Time) int {
	if now.After(state.GraceEndsAt) {
		return 0
	}
	return DaysBetween(now, state.GraceEndsAt)
}

// IsInCancellationWindow determines whether the current time falls within the
// cancellation window for a subscription. The cancellation window represents the
// period during which a cancellation has taken effect but the subscription is still
// within its grace period, allowing continued access to services before full termination.
// This function is useful for enforcing grace period policies and determining when
// a cancelled subscription should still be considered active.
//
// The function checks three conditions to determine if the current time is within
// the cancellation window:
//   - The subscription must be in grace period (InGracePeriod is true)
//   - The current time must be after the cancellation time (now > CancelAt)
//   - The current time must be before the grace period end time (now < GraceEndsAt)
//
// The state parameter contains the cancellation details including whether the grace
// period is active, the cancellation time, and the grace period end time. The now
// parameter is the current time for comparison against the cancellation window boundaries.
//
// Returns true if the current time is within the cancellation window (all conditions met).
// Returns false otherwise.
func IsInCancellationWindow(state CancellationState, now time.Time) bool {
	return state.InGracePeriod && now.After(state.CancelAt) && now.Before(state.GraceEndsAt)
}
