package subscription

import (
	"testing"
	"time"
)

// TestGracePeriodDefault verifies that the default grace period is 7 days
func TestGracePeriodDefault(t *testing.T) {
	expected := 7 * 24 * time.Hour
	result := GracePeriodDefault()
	if result != expected {
		t.Errorf("GracePeriodDefault() = %v, want %v", result, expected)
	}
}

// TestCalculateGraceEnd verifies correct calculation of grace period end time
func TestCalculateGraceEnd(t *testing.T) {
	cancelAt := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	gracePeriod := 24 * time.Hour
	expected := time.Date(2024, 1, 16, 10, 30, 0, 0, time.UTC)
	result := CalculateGraceEnd(cancelAt, gracePeriod)
	if !result.Equal(expected) {
		t.Errorf("CalculateGraceEnd(%v, %v) = %v, want %v", cancelAt, gracePeriod, result, expected)
	}

	// Test with default grace period
	resultDefault := CalculateGraceEnd(cancelAt, GracePeriodDefault())
	expectedDefault := time.Date(2024, 1, 22, 10, 30, 0, 0, time.UTC)
	if !resultDefault.Equal(expectedDefault) {
		t.Errorf("CalculateGraceEnd(%v, GracePeriodDefault()) = %v, want %v", cancelAt, resultDefault, expectedDefault)
	}
}

// TestCancelNow verifies immediate cancellation state creation
func TestCancelNow(t *testing.T) {
	before := time.Now()
	result := CancelNow()
	after := time.Now()

	if result.Reason != "immediate" {
		t.Errorf("CancelNow().Reason = %v, want 'immediate'", result.Reason)
	}
	if !result.InGracePeriod {
		t.Errorf("CancelNow().InGracePeriod = %v, want true", result.InGracePeriod)
	}
	if result.CancelAt.Before(before) || result.CancelAt.After(after) {
		t.Errorf("CancelNow().CancelAt = %v, want time between %v and %v", result.CancelAt, before, after)
	}
	expectedGraceEnd := result.CancelAt.Add(GracePeriodDefault())
	if !result.GraceEndsAt.Equal(expectedGraceEnd) {
		t.Errorf("CancelNow().GraceEndsAt = %v, want %v", result.GraceEndsAt, expectedGraceEnd)
	}
}

// TestScheduleCancelAt verifies scheduled cancellation state creation
func TestScheduleCancelAt(t *testing.T) {
	cancelDate := time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC)
	result := ScheduleCancelAt(cancelDate)

	if result.Reason != "requested" {
		t.Errorf("ScheduleCancelAt().Reason = %v, want 'requested'", result.Reason)
	}
	if !result.InGracePeriod {
		t.Errorf("ScheduleCancelAt().InGracePeriod = %v, want true", result.InGracePeriod)
	}
	if !result.CancelAt.Equal(cancelDate) {
		t.Errorf("ScheduleCancelAt().CancelAt = %v, want %v", result.CancelAt, cancelDate)
	}
	expectedGraceEnd := cancelDate.Add(GracePeriodDefault())
	if !result.GraceEndsAt.Equal(expectedGraceEnd) {
		t.Errorf("ScheduleCancelAt().GraceEndsAt = %v, want %v", result.GraceEndsAt, expectedGraceEnd)
	}
}

// TestScheduleEndOfMonthCancel verifies end-of-month cancellation state creation
func TestScheduleEndOfMonthCancel(t *testing.T) {
	now := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	endOfMonth := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	cycle := BillingCycle{
		StartAt:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndAt:    endOfMonth,
		Cadence:  "monthly",
	}

	result := ScheduleEndOfMonthCancel(cycle, now)

	if result.Reason != "end_of_month" {
		t.Errorf("ScheduleEndOfMonthCancel().Reason = %v, want 'end_of_month'", result.Reason)
	}
	if !result.InGracePeriod {
		t.Errorf("ScheduleEndOfMonthCancel().InGracePeriod = %v, want true", result.InGracePeriod)
	}
	if !result.CancelAt.Equal(endOfMonth) {
		t.Errorf("ScheduleEndOfMonthCancel().CancelAt = %v, want %v", result.CancelAt, endOfMonth)
	}
	expectedGraceEnd := endOfMonth.Add(GracePeriodDefault())
	if !result.GraceEndsAt.Equal(expectedGraceEnd) {
		t.Errorf("ScheduleEndOfMonthCancel().GraceEndsAt = %v, want %v", result.GraceEndsAt, expectedGraceEnd)
	}
}

// TestIsCancellationComplete verifies cancellation completeness detection
func TestIsCancellationComplete(t *testing.T) {
	cancelAt := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	state := CancellationState{
		CancelAt:      cancelAt,
		InGracePeriod: true,
		GraceEndsAt:   cancelAt.Add(GracePeriodDefault()),
		Reason:        "requested",
	}

	tests := []struct {
		name     string
		now      time.Time
		expected bool
	}{
		{
			name:     "before cancellation date",
			now:      time.Date(2024, 1, 14, 0, 0, 0, 0, time.UTC),
			expected: false,
		},
		{
			name:     "exactly at cancellation date",
			now:      cancelAt,
			expected: true,
		},
		{
			name:     "cancellation date in progress",
			now:      time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
			expected: true,
		},
		{
			name:     "after cancellation date",
			now:      time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsCancellationComplete(state, tt.now)
			if result != tt.expected {
				t.Errorf("IsCancellationComplete(state, %v) = %v, want %v", tt.now, result, tt.expected)
			}
		})
	}
}

// TestCancellationDaysRemaining verifies countdown calculation prior to cancellation
func TestCancellationDaysRemaining(t *testing.T) {
	cancelAt := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	state := CancellationState{
		CancelAt:      cancelAt,
		InGracePeriod: true,
		GraceEndsAt:   cancelAt.Add(GracePeriodDefault()),
		Reason:        "requested",
	}

	tests := []struct {
		name     string
		now      time.Time
		expected int
	}{
		{
			name:     "3 days before cancellation",
			now:      time.Date(2024, 1, 12, 0, 0, 0, 0, time.UTC),
			expected: 3,
		},
		{
			name:     "1 day before cancellation",
			now:      time.Date(2024, 1, 14, 0, 0, 0, 0, time.UTC),
			expected: 1,
		},
		{
			name:     "same day as cancellation",
			now:      cancelAt,
			expected: 0,
		},
		{
			name:     "after cancellation date",
			now:      time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC),
			expected: 0,
		},
		{
			name:     "many days before cancellation",
			now:      time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expected: 14,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CancellationDaysRemaining(state, tt.now)
			if result != tt.expected {
				t.Errorf("CancellationDaysRemaining(state, %v) = %v, want %v", tt.now, result, tt.expected)
			}
		})
	}
}

// TestGetCancellationStatus verifies status detection across lifecycle stages
func TestGetCancellationStatus(t *testing.T) {
	tests := []struct {
		name     string
		state    CancellationState
		now      time.Time
		expected string
	}{
		{
			name:     "not cancelled - zero cancel date",
			state:    CancellationState{},
			now:      time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			expected: StatusNotCancelled,
		},
		{
			name:     "pending - cancellation scheduled in future",
			state: CancellationState{
				CancelAt:      time.Date(2024, 1, 20, 0, 0, 0, 0, time.UTC),
				InGracePeriod: true,
				GraceEndsAt:   time.Date(2024, 1, 27, 0, 0, 0, 0, time.UTC),
				Reason:        "requested",
			},
			now:      time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			expected: StatusPending,
		},
		{
			name:     "in grace period - cancellation active, grace still valid",
			state: CancellationState{
				CancelAt:      time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
				InGracePeriod: true,
				GraceEndsAt:   time.Date(2024, 1, 22, 0, 0, 0, 0, time.UTC),
				Reason:        "requested",
			},
			now:      time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC),
			expected: StatusInGracePeriod,
		},
		{
			name:     "cancelled - grace period ended",
			state: CancellationState{
				CancelAt:      time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
				InGracePeriod: true,
				GraceEndsAt:   time.Date(2024, 1, 22, 0, 0, 0, 0, time.UTC),
				Reason:        "requested",
			},
			now:      time.Date(2024, 1, 23, 0, 0, 0, 0, time.UTC),
			expected: StatusCancelled,
		},
		{
			name:     "in grace period - exact start of grace",
			state: CancellationState{
				CancelAt:      time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
				InGracePeriod: true,
				GraceEndsAt:   time.Date(2024, 1, 22, 0, 0, 0, 0, time.UTC),
				Reason:        "requested",
			},
			now:      time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			expected: StatusInGracePeriod,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetCancellationStatus(tt.state, tt.now)
			if result != tt.expected {
				t.Errorf("GetCancellationStatus(state, %v) = %v, want %v", tt.now, result, tt.expected)
			}
		})
	}
}

// TestDaysInGracePeriod verifies grace period countdown calculation
func TestDaysInGracePeriod(t *testing.T) {
	cancelAt := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	graceEnd := time.Date(2024, 1, 22, 0, 0, 0, 0, time.UTC)
	state := CancellationState{
		CancelAt:      cancelAt,
		InGracePeriod: true,
		GraceEndsAt:   graceEnd,
		Reason:        "immediate",
	}

	tests := []struct {
		name     string
		now      time.Time
		expected int
	}{
		{
			name:     "start of grace period",
			now:      time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			expected: 7,
		},
		{
			name:     "middle of grace period",
			now:      time.Date(2024, 1, 18, 0, 0, 0, 0, time.UTC),
			expected: 4,
		},
		{
			name:     "near end of grace period",
			now:      time.Date(2024, 1, 21, 0, 0, 0, 0, time.UTC),
			expected: 1,
		},
		{
			name:     "exactly at grace end",
			now:      graceEnd,
			expected: 0,
		},
		{
			name:     "after grace period ends",
			now:      time.Date(2024, 1, 23, 0, 0, 0, 0, time.UTC),
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DaysInGracePeriod(state, tt.now)
			if result != tt.expected {
				t.Errorf("DaysInGracePeriod(state, %v) = %v, want %v", tt.now, result, tt.expected)
			}
		})
	}
}

// TestIsInCancellationWindow verifies active grace period checks
func TestIsInCancellationWindow(t *testing.T) {
	cancelAt := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	graceEnd := time.Date(2024, 1, 22, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		state    CancellationState
		now      time.Time
		expected bool
	}{
		{
			name: "within cancellation window",
			state: CancellationState{
				CancelAt:      cancelAt,
				InGracePeriod: true,
				GraceEndsAt:   graceEnd,
				Reason:        "immediate",
			},
			now:      time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC),
			expected: true,
		},
		{
			name: "before cancellation window starts",
			state: CancellationState{
				CancelAt:      cancelAt,
				InGracePeriod: true,
				GraceEndsAt:   graceEnd,
				Reason:        "immediate",
			},
			now:      time.Date(2024, 1, 14, 0, 0, 0, 0, time.UTC),
			expected: false,
		},
		{
			name: "after cancellation window ends",
			state: CancellationState{
				CancelAt:      cancelAt,
				InGracePeriod: true,
				GraceEndsAt:   graceEnd,
				Reason:        "immediate",
			},
			now:      time.Date(2024, 1, 23, 0, 0, 0, 0, time.UTC),
			expected: false,
		},
		{
			name: "outside window due to InGracePeriod false",
			state: CancellationState{
				CancelAt:      cancelAt,
				InGracePeriod: false,
				GraceEndsAt:   graceEnd,
				Reason:        "immediate",
			},
			now:      time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC),
			expected: false,
		},
		{
			name: "boundary - exactly at cancel time",
			state: CancellationState{
				CancelAt:      cancelAt,
				InGracePeriod: true,
				GraceEndsAt:   graceEnd,
				Reason:        "immediate",
			},
			now:      cancelAt,
			expected: false,
		},
		{
			name: "boundary - exactly at grace end",
			state: CancellationState{
				CancelAt:      cancelAt,
				InGracePeriod: true,
				GraceEndsAt:   graceEnd,
				Reason:        "immediate",
			},
			now:      graceEnd,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsInCancellationWindow(tt.state, tt.now)
			if result != tt.expected {
				t.Errorf("IsInCancellationWindow(state, %v) = %v, want %v", tt.now, result, tt.expected)
			}
		})
	}
}

// TestImmediateCancellationScenario tests complete workflow of immediate cancellation covering
// immediate cancellation and grace period entry
func TestImmediateCancellationScenario(t *testing.T) {
	now := time.Date(2024, 3, 15, 10, 0, 0, 0, time.UTC)
	t.Setenv("TZ", "UTC")

	state := CancellationState{
		CancelAt:      now,
		InGracePeriod: true,
		GraceEndsAt:   now.Add(GracePeriodDefault()),
		Reason:        "immediate",
	}

	// Verify immediate cancellation created correct state
	if state.Reason != "immediate" {
		t.Errorf("Expected reason 'immediate', got %v", state.Reason)
	}

	// Test entering grace period
	inGracePeriod := IsInCancellationWindow(state, now.Add(1*time.Hour))
	if !inGracePeriod {
		t.Errorf("Expected to be in cancellation window 1 hour after cancellation")
	}

	// Test still in grace period
	middleOfGrace := now.Add(3 * 24 * time.Hour)
	status := GetCancellationStatus(state, middleOfGrace)
	if status != StatusInGracePeriod {
		t.Errorf("Expected status '%v' 3 days after cancellation, got %v", StatusInGracePeriod, status)
	}

	// Test exact grace period boundaries
	graceStart := now
	graceStartStatus := GetCancellationStatus(state, graceStart)
	if graceStartStatus != StatusInGracePeriod {
		t.Errorf("Expected status '%v' at grace start, got %v", StatusInGracePeriod, graceStartStatus)
	}

	graceEnd := state.GraceEndsAt
	graceEndStatus := GetCancellationStatus(state, graceEnd)
	if graceEndStatus != StatusInGracePeriod {
		t.Errorf("Expected status '%v' at grace end, got %v", StatusInGracePeriod, graceEndStatus)
	}

	// Test exiting grace period
	afterGrace := state.GraceEndsAt.Add(1*time.Hour)
	cancelledStatus := GetCancellationStatus(state, afterGrace)
	if cancelledStatus != StatusCancelled {
		t.Errorf("Expected status %v after grace period, got %v", StatusCancelled, cancelledStatus)
	}

	// Verify not in cancellation window after grace ends
	stillInWindow := IsInCancellationWindow(state, afterGrace)
	if stillInWindow {
		t.Errorf("Expected not to be in cancellation window after grace period ends")
	}
}

// TestEndOfMonthCancellationScenario tests complete workflow of end-of-month cancellation
func TestEndOfMonthCancellationScenario(t *testing.T) {
	januaryStart := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	januaryEnd := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	now := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	cycle := BillingCycle{
		StartAt:  januaryStart,
		EndAt:    januaryEnd,
		Cadence:  "monthly",
	}

	// Schedule end of month cancellation
	state := ScheduleEndOfMonthCancel(cycle, now)

	// Verify cancellation scheduled at month end
	if !state.CancelAt.Equal(januaryEnd) {
		t.Errorf("Expected cancellation at month end %v, got %v", januaryEnd, state.CancelAt)
	}

	if state.Reason != "end_of_month" {
		t.Errorf("Expected reason 'end_of_month', got %v", state.Reason)
	}

	// Test pending status before cancellation date
	newYearsEve := time.Date(2024, 1, 30, 0, 0, 0, 0, time.UTC)
	statusBefore := GetCancellationStatus(state, newYearsEve)
	if statusBefore != StatusPending {
		t.Errorf("Expected status '%v' before cancellation date, got %v", StatusPending, statusBefore)
	}

	daysRemaining := CancellationDaysRemaining(state, newYearsEve)
	if daysRemaining != 1 {
		t.Errorf("Expected 1 day remaining on Jan 30, got %v", daysRemaining)
	}

	// Test cancellation day
	statusOnCancelDay := GetCancellationStatus(state, januaryEnd)
	if statusOnCancelDay != StatusInGracePeriod {
		t.Errorf("Expected status '%v' on cancellation day, got %v", StatusInGracePeriod, statusOnCancelDay)
	}

	daysRemainingOnCancelDay := CancellationDaysRemaining(state, januaryEnd)
	if daysRemainingOnCancelDay != 0 {
		t.Errorf("Expected 0 days remaining on cancellation day, got %v", daysRemainingOnCancelDay)
	}

	cancellationComplete := IsCancellationComplete(state, januaryEnd)
	if !cancellationComplete {
		t.Errorf("Expected cancellation to be complete on cancellation day")
	}

	// Test still in grace period after cancellation
	febFirst := time.Date(2024, 2, 3, 0, 0, 0, 0, time.UTC)
	statusInGrace := GetCancellationStatus(state, febFirst)
	if statusInGrace != StatusInGracePeriod {
		t.Errorf("Expected status '%v' in grace period, got %v", StatusInGracePeriod, statusInGrace)
	}

	daysInGrace := DaysInGracePeriod(state, febFirst)
	expectedGraceDays := int(state.GraceEndsAt.Sub(febFirst).Hours() / 24)
	if daysInGrace != expectedGraceDays {
		t.Errorf("Expected %v days in grace period on Feb 3, got %v", expectedGraceDays, daysInGrace)
	}

	// Test fully cancelled after grace period
	afterGrace := state.GraceEndsAt.Add(1 * time.Hour)
	statusAfterGrace := GetCancellationStatus(state, afterGrace)
	if statusAfterGrace != StatusCancelled {
		t.Errorf("Expected status '%v' after grace period, got %v", StatusCancelled, statusAfterGrace)
	}
}

// TestGracePeriodEntryExitScenario tests entering and leaving the grace period
func TestGracePeriodEntryExitScenario(t *testing.T) {
	cancelAt := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	state := ScheduleCancelAt(cancelAt)
	graceEnd := state.GraceEndsAt

	// Before cancellation - pending status
	beforeCancel := cancelAt.Add(-1 * time.Hour)
	statusBefore := GetCancellationStatus(state, beforeCancel)
	if statusBefore != StatusPending {
		t.Errorf("Expected pending status before cancellation, got %v", statusBefore)
	}

	inWindowBefore := IsInCancellationWindow(state, beforeCancel)
	if inWindowBefore {
		t.Errorf("Should not be in cancellation window before cancellation")
	}

	// At cancellation time - enter grace period
	atCancel := cancelAt
	statusAtCancel := GetCancellationStatus(state, atCancel)
	if statusAtCancel != StatusInGracePeriod {
		t.Errorf("Expected in_grace_period status at cancellation time, got %v", statusAtCancel)
	}

	cancellationComplete := IsCancellationComplete(state, atCancel)
	if !cancellationComplete {
		t.Errorf("Cancellation should be complete at cancellation time")
	}

	// Inside grace period - should be in cancellation window
	insideGrace := cancelAt.Add(24 * time.Hour)
	statusInside := GetCancellationStatus(state, insideGrace)
	if statusInside != StatusInGracePeriod {
		t.Errorf("Expected in_grace_period status during grace period, got %v", statusInside)
	}

	inWindowInside := IsInCancellationWindow(state, insideGrace)
	if !inWindowInside {
		t.Errorf("Should be in cancellation window during grace period")
	}

	daysInGrace := DaysInGracePeriod(state, insideGrace)
	if daysInGrace <= 0 || daysInGrace > 7 {
		t.Errorf("Expected 1-7 days remaining in grace period 1 day after start, got %v", daysInGrace)
	}

	// At grace end - still in grace period
	atGraceEnd := graceEnd
	statusAtGraceEnd := GetCancellationStatus(state, atGraceEnd)
	if statusAtGraceEnd != StatusInGracePeriod {
		t.Errorf("Expected in_grace_period status at grace end, got %v", statusAtGraceEnd)
	}

	inWindowAtEnd := IsInCancellationWindow(state, atGraceEnd)
	if inWindowAtEnd {
		t.Errorf("Should not be in cancellation window at exact grace end (exclusive)")
	}

	// After grace period - cancelled status
	afterGrace := graceEnd.Add(1 * time.Hour)
	statusAfterGrace := GetCancellationStatus(state, afterGrace)
	if statusAfterGrace != StatusCancelled {
		t.Errorf("Expected cancelled status after grace period, got %v", statusAfterGrace)
	}

	inWindowAfter := IsInCancellationWindow(state, afterGrace)
	if inWindowAfter {
		t.Errorf("Should not be in cancellation window after grace period ends")
	}

	daysInGraceAfter := DaysInGracePeriod(state, afterGrace)
	if daysInGraceAfter != 0 {
		t.Errorf("Expected 0 days in grace period after grace ends, got %v", daysInGraceAfter)
	}
}

// TestResubscriptionWindowScenario tests the state where a user can reactivate before cancellation takes effect
func TestResubscriptionWindowScenario(t *testing.T) {
	scheduledDate := time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC)
	state := ScheduleCancelAt(scheduledDate)

	// Far in advance - ample time to reactivate
	earlyWindow := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)
	statusEarly := GetCancellationStatus(state, earlyWindow)
	if statusEarly != StatusPending {
		t.Errorf("Expected pending status in early resubscription window, got %v", statusEarly)
	}

	daysRemainingEarly := CancellationDaysRemaining(state, earlyWindow)
	expectedDaysEarly := 17
	if daysRemainingEarly != expectedDaysEarly {
		t.Errorf("Expected %v days remaining March 15, got %v", expectedDaysEarly, daysRemainingEarly)
	}

	cancellationCompleteEarly := IsCancellationComplete(state, earlyWindow)
	if cancellationCompleteEarly {
		t.Errorf("Cancellation should not be complete during resubscription window")
	}

	// Narrow window still pending
	narrowWindow := time.Date(2024, 3, 31, 23, 0, 0, 0, time.UTC)
	statusNarrow := GetCancellationStatus(state, narrowWindow)
	if statusNarrow != StatusPending {
		t.Errorf("Expected pending status in narrow resubscription window, got %v", statusNarrow)
	}

	// Reactivation threshold - last moment before cancellation
	lastMoment := time.Date(2024, 3, 31, 23, 59, 59, 0, time.UTC)
	statusLast := GetCancellationStatus(state, lastMoment)
	if statusLast != StatusPending {
		t.Errorf("Expected pending status at last reactivation moment, got %v", statusLast)
	}

	// After threshold - too late to reactivate (grace period starts)
	afterThreshold := scheduledDate.Add(1 * time.Second)
	statusAfter := GetCancellationStatus(state, afterThreshold)
	if statusAfter != StatusInGracePeriod {
		t.Errorf("Expected in_grace_period status after threshold, got %v", statusAfter)
	}

	// Verify grace period provides final access window but no true resubscription
	middleOfGrace := scheduledDate.Add(3 * 24 * time.Hour)
	graceStatus := GetCancellationStatus(state, middleOfGrace)
	if graceStatus != StatusInGracePeriod {
		t.Errorf("Expected in_grace_period status after cancellation, got %v", graceStatus)
	}

	stillInWindow := IsInCancellationWindow(state, middleOfGrace)
	if !stillInWindow {
		t.Errorf("Should be in cancellation window during grace period")
	}
}
