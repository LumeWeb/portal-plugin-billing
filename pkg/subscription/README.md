# Subscription Package

The `subscription` package provides domain types and utilities for subscription billing cycles, pricing, and date/time calculations. This package implements a pure functional API for subscription management operations including prorated billing, cancellation lifecycle management, and billing cycle calculations.

## Overview

The subscription package is designed to be the foundational domain layer for subscription billing logic. It provides:

- **Domain Types**: Core data structures representing subscription entities (Plan, Price, BillingCycle, etc.)
- **Pure Functional API**: All functions are side-effect free, making them ideal for testing and deterministic calculations
- **Precise Financial Calculations**: Uses `github.com/shopspring/decimal` for exact monetary arithmetic
- **Type-Safe API**: Strongly-typed `Cadence` type eliminates string-based error possibilities
- **Billing Cycle Management**: Utilities for cycle progress, elapsed/remaining time, and cycle sequences
- **Proration Logic**: Time-weighted calculations for mid-cycle plan changes and cancellations
- **Cancellation Lifecycle**: Grace period management and status determination

## Table of Contents

- [Features](#features)
- [Installation](#installation)
- [Usage Examples](#usage-examples)
- [Design Principles](#design-principles)
- [Integration Patterns](#integration-patterns)
- [API Reference](#api-reference)
- [Contributing](#contributing)

## Features

### Domain Types

The package defines core domain types for subscription billing:

- **Plan**: Subscription plan with pricing tier and currency
- **Price**: Monetary amount and billing cadence
- **BillingCycle**: Time boundaries and cadence for billing periods
- **ProrationResult**: Credit/charge breakdown for plan changes
- **CancellationState**: Cancellation lifecycle including grace period
- **CycleDate**: Billing cycle state at a specific point in time
- **PricingSnapshot**: Historical pricing record
- **Period**: Time span with duration and day count
- **Cadence**: Type-safe billing frequency (Daily, Weekly, Monthly, Quarterly, Yearly, Rolling)
- **ProrationBehavior**: Proration behavior for subscription updates (None, CreateProrations, AlwaysInvoice)

### Date/Time Mathematics

Precise date calculations with awareness of business day semantics:

```go
// Days between two dates (whole days)
days := subscription.DaysBetween(start, end)

// Day/month calculations
duration := subscription.EndOfMonth(t)
startOfMonth := subscription.StartOfMonth(t)
daysInMonth := subscription.DaysInMonth(t)

// Billing period arithmetic using Cadence type
nextMonth := subscription.AddMonths(t, 1)
nextBillingDate, err := subscription.CadenceMonthly.AddTo(t)
```

### Type-Safe Billing Cadences

Billing cadences are strongly-typed, eliminating string-based errors:

```go
// Parse cadence from string input (validated)
cadence, err := subscription.ParseCadence("monthly")
if err != nil {
    // Handle invalid cadence
}

// Use constants for compile-time safety
cadence := subscription.CadenceDaily
cadence := subscription.CadenceWeekly
cadence := subscription.CadenceMonthly
cadence := subscription.CadenceQuarterly
cadence := subscription.CadenceYearly
cadence := subscription.CadenceRolling

// Add billing period using method
nextDate, err := cadence.AddTo(time.Now())
```

### Billing Cycle Utilities

Calculate cycle state and relationships:

```go
// Cycle days and progress
totalDays := subscription.DaysInCycle(cycle)
elapsedDays := subscription.DaysElapsed(cycle, now)
remainingDays := subscription.DaysRemaining(cycle, now)
progress := subscription.CycleProgress(cycle, now) // 0.0 to 1.0

// Cycle relationships
isWithinCycle := subscription.CycleContainsTime(cycle, t)
hasOverlap := subscription.OverlapsCycle(cycleA, cycleB)

// Cycle sequences
nextCycle := subscription.CalculateNextCycle(currentCycle)
firstCycle := subscription.CalculateFirstCycle(startDate, subscription.CadenceMonthly)
renewalDate := subscription.CalculateNextRenewal(cycle)

// Complete cycle snapshot
cycleDate := subscription.CycleAtDate(cycle, t)
```

### Proration Calculations

Time-weighted calculations for mid-cycle plan changes:

```go
// Calculate unused credit from old plan
unusedCredit := subscription.UnusedPeriodValue(oldPrice, cycle, now)

// Calculate new charge for elapsed time
newCharge := subscription.NewPeriodCharge(newPrice, cycle, now)

// End-to-end proration for plan changes with behavior parameter
result, err := subscription.ProratedChange(oldPrice, newPrice, cycle, now, subscription.ProrationBehaviorCreateProrations)
if err != nil {
    // Handle validation errors
}
// result contains:
//   - UnusedCredit: Credit from unused time
//   - NewCharge: Charge for remaining time
//   - CreditDue: Net amount to charge/credit
//   - EffectiveDate: When proration takes effect

// Determine billing actions
if subscription.ShouldIssueCredit(result) {
    // Issue credit to customer
}
if subscription.ShouldCharge(result) {
    // Collect payment from customer
}
```

### Cancellation Lifecycle

Manage subscription cancellations with grace periods:

```go
// Create cancellation states
immediateCancel := subscription.CancelNow()
scheduledCancel := subscription.ScheduleCancelAt(futureDate)
endOfMonthCancel := subscription.ScheduleEndOfMonthCancel(cycle, now)

// Query cancellation status
status := subscription.GetCancellationStatus(state, now)
// Returns: "not_cancelled", "pending", "in_grace_period", "cancelled"

// Check grace period
isComplete := subscription.IsCancellationComplete(state, now)
inWindow := subscription.IsInCancellationWindow(state, now)
graceDaysRemaining := subscription.DaysInGracePeriod(state, now)
daysUntilCancel := subscription.CancellationDaysRemaining(state, now)
```

## Installation

```bash
go get project/subscription
```

## Usage Examples

### Example 1: Basic Plan Change Proration

```go
package main

import (
    "time"
    "github.com/shopspring/decimal"
    "project/subscription"
)

func handlePlanChange() {
    now := time.Now()
    
    // Current billing cycle
    cycle := subscription.BillingCycle{
        StartAt:  time.Date(2025, 8, 1, 0, 0, 0, 0, time.UTC),
        EndAt:    time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC),
        Cadence:  subscription.CadenceMonthly,
    }
    
    // Old plan: $50/month
    oldPrice := subscription.Price{
        Amount:  decimal.NewFromInt(50),
        Cadence: subscription.CadenceMonthly,
    }
    
    // New plan: $100/month
    newPrice := subscription.Price{
        Amount:  decimal.NewFromInt(100),
        Cadence: subscription.CadenceMonthly,
    }
    
    // Calculate prorated change (on Aug 15)
    result, err := subscription.ProratedChange(oldPrice, newPrice, cycle, now, subscription.ProrationBehaviorCreateProrations)
    if err != nil {
        // Handle validation errors
        return
    }
    
    // Result:
    // - UnusedCredit: ~$25 (15 days remaining)
    // - NewCharge: ~$50 (15 days of new plan)
    // - CreditDue: ~$25 (50 - 25)
    
    // Apply billing actions
    if subscription.ShouldIssueCredit(result) {
        // Credit customer with result.UnusedCredit
    }
    if subscription.ShouldCharge(result) {
        // Charge customer with result.CreditDue
    }
}
```

### Example 2: Mid-Cycle Cancellation

```go
package main

import (
    "fmt"
    "time"
    "project/subscription"
)

func handleCancellation() {
    now := time.Now()
    
    // Current billing cycle
    cycle := subscription.BillingCycle{
        StartAt:  time.Date(2025, 8, 1, 0, 0, 0, 0, time.UTC),
        EndAt:    time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC),
        Cadence:  subscription.CadenceMonthly,
    }
    
    // Plan pricing
    planPrice := subscription.Price{
        Amount:  decimal.NewFromInt(100),
        Cadence: subscription.CadenceMonthly,
    }
    
    // Schedule end-of-month cancellation
    cancelState := subscription.ScheduleEndOfMonthCancel(cycle, now)
    
    // Check cancellation status
    status := subscription.GetCancellationStatus(cancelState, now)
    switch status {
    case subscription.StatusPending:
        fmt.Println("Cancellation scheduled for future date")
    case subscription.StatusInGracePeriod:
        daysLeft := subscription.DaysInGracePeriod(cancelState, now)
        fmt.Printf("In grace period, %d days remaining\n", daysLeft)
    case subscription.StatusCancelled:
        fmt.Println("Subscription fully cancelled")
    }
    
    // Calculate prorated credit for unused time
    if subscription.IsInCancellationWindow(cancelState, now) {
        unusedValue := subscription.UnusedPeriodValue(planPrice, cycle, now)
        fmt.Printf("Credit due: %s\n", unusedValue.String())
    }
}
```

### Example 3: Billing Cycle Progress

```go
package main

import (
    "fmt"
    "time"
    "project/subscription"
)

func monitorCycleProgress() {
    now := time.Now()
    
    // Current billing cycle
    cycle := subscription.BillingCycle{
        StartAt:  time.Date(2025, 8, 1, 0, 0, 0, 0, time.UTC),
        EndAt:    time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC),
        Cadence:  subscription.CadenceMonthly,
    }
    
    // Get cycle snapshot
    cycleDate := subscription.CycleAtDate(cycle, now)
    
    fmt.Printf("Days in cycle: %d\n", cycleDate.DaysElapsed+cycleDate.DaysRemaining)
    fmt.Printf("Days elapsed: %d\n", cycleDate.DaysElapsed)
    fmt.Printf("Days remaining: %d\n", cycleDate.DaysRemaining)
    fmt.Printf("Cycle progress: %.1f%%\n", cycleDate.Progress*100)
    fmt.Printf("Renewal date: %s\n", subscription.CalculateNextRenewal(cycle).Format("2006-01-02"))
}
```

### Example 4: Integration with Ledger Service

```go
package main

import (
    "fmt"
    "time"
    "github.com/shopspring/decimal"
    "project/ledger"
    "project/subscription"
)

// CancellationWorkflow orchestrates cancellation operations
func CancellationWorkflow(
    subscriptionID uint64,
    userID uint64,
    ledgerService ledger.Service,
    subscriptionRepo interface{},
) error {
    now := time.Now()
    
    // 1. Retrieve subscription and billing cycle
    sub, cycle, err := getSubscription(subscriptionRepo, subscriptionID)
    if err != nil {
        return err
    }
    
    // 2. Parse cadence and determine cancellation state
    cadence, err := subscription.ParseCadence(sub.CadenceString)
    if err != nil {
        return fmt.Errorf("invalid cadence: %w", err)
    }
    
    planPrice := subscription.Price{
        Amount:  decimal.NewFromInt(sub.PlanAmount),
        Cadence: cadence,
    }
    cancelState := subscription.ScheduleEndOfMonthCancel(cycle, now)
    
    // 3. Calculate prorated credit
    unusedCredit := subscription.UnusedPeriodValue(planPrice, cycle, now)
    
    // 4. Record credit in ledger
    if unusedCredit.GreaterThan(decimal.Zero) {
        ledgerTx := ledger.Transaction{
            Type:          "cancellation_credit",
            Direction:     ledger.GetDirection(ledger.CreditDirection),
            Amount:        unusedCredit,
            AccountID:     userID,
            Description:   "Prorated credit for subscription cancellation",
            ReferenceID:   fmt.Sprintf("cancel_%d", subscriptionID),
            ReferenceType: "subscription_cancellation",
        }
        if err := ledgerService.RecordCredit(ledgerTx); err != nil {
            return err
        }
    }
    
    // 5. Update subscription status
    update := SubscriptionUpdate{
        Status:        "cancelled",
        CancelledAt:   cancelState.CancelAt,
        GraceEndsAt:   cancelState.GraceEndsAt,
        CancelReason:  "user_requested",
    }
    
    return updateSubscription(subscriptionRepo, subscriptionID, update)
}
```

### Example 5: Cadence Validation and Type Safety

```go
package main

import (
    "fmt"
    "time"
    "project/subscription"
)

func processSubscriptionUpdate(cadenceStr string, startDate time.Time) error {
    // Parse cadence from string (validated at runtime)
    cadence, err := subscription.ParseCadence(cadenceStr)
    if err != nil {
        return fmt.Errorf("invalid cadence %q: %w", cadenceStr, err)
    }
    
    // Create first billing cycle using type-safe cadence
    cycle := subscription.CalculateFirstCycle(startDate, cadence)
    
    // Calculate next renewal date
    renewalDate := subscription.CalculateNextRenewal(cycle)
    
    fmt.Printf("Cycle created: %s to %s\n", 
        cycle.StartAt.Format("2006-01-02"),
        cycle.EndAt.Format("2006-01-02"))
    fmt.Printf("Renewal date: %s\n", renewalDate.Format("2006-01-02"))
    
    return nil
}

// Compile-time checked cadence usage
func createMonthlyCycle(startDate time.Time) subscription.BillingCycle {
    return subscription.CalculateFirstCycle(startDate, subscription.CadenceMonthly)
}

func createYearlyCycle(startDate time.Time) subscription.BillingCycle {
    return subscription.CalculateFirstCycle(startDate, subscription.CadenceYearly)
}
```

## Design Principles

### Pure Functional API

All functions in the subscription package are pure (no side effects):

- **Deterministic**: Given the same inputs, always produces the same outputs
- **No Global State**: Functions don't rely on or modify external state
- **Testable**: Easy to unit test with predictable behavior
- **Composable**: Functions can be combined to build complex workflows

```go
// Pure function example
func TimeWeightedAmount(amount decimal.Decimal, totalDays, daysInPeriod int) decimal.Decimal {
    return amount.Mul(decimal.NewFromInt(int64(daysInPeriod))).Div(decimal.NewFromInt(int64(totalDays)))
}
```

### Domain-Driven Design

Types clearly represent business concepts:

```go
type Cadence string

const (
    CadenceDaily    Cadence = "daily"
    CadenceWeekly   Cadence = "weekly"
    CadenceMonthly  Cadence = "monthly"
    CadenceQuarterly Cadence = "quarterly"
    CadenceYearly   Cadence = "yearly"
    CadenceRolling  Cadence = "rolling"
)

type BillingCycle struct {
    StartAt time.Time
    EndAt   time.Time
    Cadence Cadence
}

type ProrationResult struct {
    UnusedCredit decimal.Decimal
    NewCharge    decimal.Decimal
    CreditDue    decimal.Decimal
    EffectiveDate time.Time
}

type ProrationBehavior string

const (
    ProrationBehaviorNone           ProrationBehavior = "none"
    ProrationBehaviorCreateProrations ProrationBehavior = "create_prorations"
    ProrationBehaviorAlwaysInvoice   ProrationBehavior = "always_invoice"
)
```

### Type Safety Over String-Based APIs

Replace string constants with type-safe alternatives:

```go
// BEFORE (string-based, error-prone)
cadence := "monthly"  // Typo at runtime!
nextDate, _ := AddBillingPeriod(now, cadence)

// AFTER (type-safe)
cadence := subscription.CadenceMonthly  // Compile-time checked
nextDate, _ := cadence.AddTo(now)
```

### Precision and Accuracy

Financial calculations use decimal arithmetic:

```go
import "github.com/shopspring/decimal"

price := decimal.NewFromInt(100)
result := price.Mul(decimal.NewFromFloat(0.5)) // Precise half value
```

### Clear Separation of Concerns

Each Go file has a single responsibility:

- `types.go`: Domain type definitions (Cadence, Plan, Price, BillingCycle, etc.)
- `date_math.go`: Date/time arithmetic utilities
- `billing_cycle.go`: Billing cycle calculations
- `proration.go`: Proration logic
- `cancellation.go`: Cancellation lifecycle management

### Comprehensive Documentation

All public functions include godoc comments with:

- Purpose description
- Parameter documentation
- Return value documentation
- Usage notes where applicable
- Edge case handling

### Test Coverage

The package includes comprehensive tests with 98.8% coverage:

- Unit tests for pure functions
- Table-driven tests for edge cases
- All Cadence methods tested
- Examples demonstrating integration patterns

## Integration Patterns

### Orchestration Layer Pattern

The subscription package provides domain logic while an orchestration layer handles:

- Database persistence
- External API calls
- Transaction management
- Business workflow coordination

```go
type OrchestrationLayer struct {
    pricingService       PricingService
    creditLedger         ledger.Service
    subscriptionRepo     SubscriptionRepository
    notificationService  NotificationService
}

func (o *OrchestrationLayer) HandlePlanChange(
    userID uint64,
    subscriptionID uint64,
    oldPlanID uint64,
    newPlanID uint64,
) error {
    // 1. Fetch data from services
    sub, cycle := o.subscriptionRepo.GetWithCycle(subscriptionID)
    oldPrice := o.pricingService.GetPrice(oldPlanID)
    newPrice := o.pricingService.GetPrice(newPlanID)
    
    // 2. Use subscription package for calculations
    result, err := subscription.ProratedChange(oldPrice, newPrice, cycle, time.Now(), subscription.ProrationBehaviorCreateProrations)
    if err != nil {
        return err
    }
    
    // 3. Execute transactions
    if subscription.ShouldIssueCredit(result) {
        o.creditLedger.RecordCredit(ledger.Transaction{...})
    }
    if subscription.ShouldCharge(result) {
        o.paymentGateway.Charge(result.CreditDue)
    }
    
    // 4. Update subscription
    o.subscriptionRepo.UpdatePlan(subscriptionID, newPlanID)
    
    // 5. Send notifications
    o.notificationService.SendProrationNotice(userID, result)
    
    return nil
}
```

### Service Layer Pattern

Wrap subscription functions in service methods that include business logic:

```go
type SubscriptionService struct {
    repo    SubscriptionRepository
    ledger  ledger.Service
    gateway PaymentGateway
}

func (s *SubscriptionService) CalculateProration(
    ctx context.Context,
    subID uint64,
    now time.Time,
) (*ProrationResult, error) {
    sub, err := s.repo.Get(ctx, subID)
    if err != nil {
        return nil, err
    }
    
    oldPrice := sub.Price
    newPrice := s.pricing.GetPrice(sub.TargetPlanID)
    
    result, err := subscription.ProratedChange(oldPrice, newPrice, sub.Cycle, now, subscription.ProrationBehaviorCreateProrations)
    if err != nil {
        return nil, err
    }
    return &result, nil
}
```

### Validation Layer

Before calling subscription functions, validate inputs:

```go
func ValidatePlanChange(oldPlan, newPlan Plan, cycle BillingCycle) error {
    if cycle.Cadence != oldPlan.Cadence || cycle.Cadence != newPlan.Cadence {
        return errors.New("cadence mismatch")
    }
    
    if time.Now().Before(cycle.StartAt) || time.Now().After(cycle.EndAt) {
        return errors.New("not within billing cycle")
    }
    
    return nil
}
```

## API Reference

### Core Types

#### `type Cadence string`

Type-safe billing frequency with validation.

**Constants:**
- `CadenceDaily` - Daily billing
- `CadenceWeekly` - Weekly billing
- `CadenceMonthly` - Monthly billing
- `CadenceQuarterly` - Quarterly billing
- `CadenceYearly` - Yearly billing
- `CadenceRolling` - Rolling billing period (requires context)

**Functions:**
- `ParseCadence(s string) (Cadence, error)` - Validates and creates Cadence from string

**Methods:**
- `AddTo(t time.Time) (time.Time, error)` - Adds one billing period to the given time

**Example:**
```go
cadence, err := subscription.ParseCadence("monthly")
if err != nil {
    return err
}
nextDate, err := cadence.AddTo(time.Now())
```

### Date/Time Math

#### `DaysBetween(start, end time.Time) int`

Calculates whole days between two timestamps.

#### `StartOfDay(t time.Time) time.Time` / `EndOfDay(t time.Time) time.Time`

Returns the start or end of the day for the given time.

#### `StartOfMonth(t time.Time) time.Time` / `EndOfMonth(t time.Time) time.Time`

Returns the start or end of the month for the given time.

#### `DaysInMonth(t time.Time) int`

Returns the number of days in the month of the given time.

#### `AddMonths(t time.Time, months int) time.Time`

Adds the specified number of months to a timestamp.

#### `IsSameDay(a, b time.Time) bool`

Compares two times and returns whether they occur on the same calendar day.

#### `YearsInPeriod(start, end time.Time) int`

Calculates the number of years between two dates.

#### `MonthsInPeriod(start, end time.Time) int`

Calculates the number of months between two dates.

### Billing Cycle

#### `DaysInCycle(cycle BillingCycle) int`

Returns total days in the billing cycle.

#### `DaysElapsed(cycle BillingCycle, now time.Time) int`

Returns days elapsed since cycle start.

#### `DaysRemaining(cycle BillingCycle, now time.Time) int`

Returns days remaining until cycle end.

#### `CycleProgress(cycle BillingCycle, now time.Time) float64`

Returns progress as a value between 0.0 and 1.0.

#### `CycleContainsTime(cycle BillingCycle, t time.Time) bool`

Checks if a time falls within the cycle boundaries.

#### `OverlapsCycle(a, b BillingCycle) bool`

Determines if two cycles overlap in time.

#### `CalculateNextCycle(currentCycle BillingCycle) BillingCycle`

Calculates the subsequent billing cycle.

#### `CalculateFirstCycle(startDate time.Time, cadence Cadence) BillingCycle`

Creates the initial billing cycle from a start date and cadence.

#### `CalculateNextRenewal(cycle BillingCycle) time.Time`

Returns the cycle end date (renewal date).

#### `CycleAtDate(cycle BillingCycle, t time.Time) CycleDate`

Creates a CycleDate representing the state of a billing cycle at a specific point in time.

### Proration

#### `type ProrationBehavior string`

Proration behavior for subscription updates. Matches Stripe's proration_behavior parameter.

**Constants:**
- `ProrationBehaviorNone` - Disables proration. No credits for unused time, full charges immediately.
- `ProrationBehaviorCreateProrations` - Creates proration items (default). Added to next invoice or immediately for cross-cadence changes.
- `ProrationBehaviorAlwaysInvoice` - Creates prorations and immediately invoices regardless of billing cycle.

#### `TimeWeightedAmount(amount decimal.Decimal, totalDays, daysInPeriod int) decimal.Decimal`

Calculates the time-weighted amount: `(amount * daysInPeriod) / totalDays`.

#### `UnusedPeriodValue(planPrice Price, cycle BillingCycle, now time.Time) decimal.Decimal`

Calculates credit due for unused billing period time.

#### `NewPeriodCharge(planPrice Price, cycle BillingCycle, now time.Time) decimal.Decimal`

Calculates charge for elapsed time in the billing period.

#### `ProrationAmountByTime(amount decimal.Decimal, totalDuration, remainingDuration time.Duration) decimal.Decimal`

Calculates a prorated amount based on exact time precision (seconds) rather than whole days, matching Stripe's behavior.

#### `ProrationTimeRatio(elapsed, totalDuration time.Duration) float64`

Calculates the precise ratio of elapsed time to total cycle duration using exact time differences.

#### `Ratio(days, totalDays int) float64`

Utility function for proration calculations. Returns the ratio of days to totalDays as a float64.

#### `PlanDifference(newPrice, oldPrice Price) decimal.Decimal`

Calculates the difference between two plan prices. Returns newPrice - oldPrice.

#### `ValidateProrationInputs(oldPrice, newPrice Price, oldCycle BillingCycle, now time.Time) error`

Validates inputs for proration calculations. Returns an error if inputs are invalid.

#### `ProratedChange(oldPrice, newPrice Price, oldCycle BillingCycle, now time.Time, behavior ProrationBehavior) (ProrationResult, error)`

Calculates complete proration result for a plan change with specified behavior.

#### `ProrationResultFromComponents(unusedCredit, newCharge decimal.Decimal, effectiveDate time.Time) ProrationResult`

Constructs a proration result from its component parts.

#### `NetResult(result ProrationResult) decimal.Decimal`

Returns the net amount by subtracting unused credit from new charge.

#### `ShouldIssueCredit(result ProrationResult) bool`

Determines if a credit should be issued.

#### `ShouldCharge(result ProrationResult) bool`

Determines if a charge should be applied.

### Cancellation

#### `GracePeriodDefault() time.Duration`

Returns the standard grace period duration (7 days) for subscription cancellations.

#### `CalculateGraceEnd(cancelAt time.Time, gracePeriod time.Duration) time.Time`

Calculates the end time of a grace period based on the cancellation time.

#### `CancelNow() CancellationState`

Creates a cancellation state for immediate cancellation with default grace period.

#### `ScheduleCancelAt(cancelDate time.Time) CancellationState`

Creates a cancellation state for a specific future date.

#### `ScheduleEndOfMonthCancel(cycle BillingCycle, now time.Time) CancellationState`

Creates a cancellation state for end-of-month (cycle end) cancellation.

#### `GetCancellationStatus(state CancellationState, now time.Time) string`

Returns current cancellation status: "not_cancelled", "pending", "in_grace_period", "cancelled".

#### `IsCancellationComplete(state CancellationState, now time.Time) bool`

Determines if cancellation has taken effect.

#### `IsInCancellationWindow(state CancellationState, now time.Time) bool`

Checks if current time is within the cancellation grace period.

#### `DaysInGracePeriod(state CancellationState, now time.Time) int`

Returns days remaining in the grace period.

#### `CancellationDaysRemaining(state CancellationState, now time.Time) int`

Returns days until cancellation takes effect.

### Cancellation Status Constants

- `StatusNotCancelled` - "not_cancelled"
- `StatusPending` - "pending"
- `StatusInGracePeriod` - "in_grace_period"
- `StatusCancelled` - "cancelled"

## Contributing

We welcome contributions to the subscription package! Please follow these guidelines:

### Development Setup

1. Clone the repository
2. Ensure Go 1.21+ is installed
3. Run tests: `go test ./pkg/subscription/...`
4. Check coverage: `go test -cover ./pkg/subscription/...`

### Code Standards

- Follow Go coding standards and `gofmt` formatting
- Write comprehensive unit tests for new functions
- Include godoc comments for all public APIs
- Use table-driven tests for multiple scenarios
- Ensure all functions are pure (no side effects)
- Use `Cadence` type instead of strings for billing frequencies

### Adding New Features

1. Identify if the new logic belongs in an existing file or requires a new file
2. Define clear types with appropriate struct tags
3. Implement pure functions with no external dependencies
4. Write tests before implementation (TDD)
5. Update this README with usage examples
6. Add integration examples

### Testing

```bash
# Run all tests
go test ./pkg/subscription/...

# Run with coverage
go test -coverprofile=coverage.out ./pkg/subscription/...

# Run specific test
go test -run TestProration ./pkg/subscription/proration_test.go
```

### Documentation

- Use clear, concise descriptions in function comments
- Provide usage examples for complex operations
- Document edge cases and error conditions
- Keep the README.md up-to-date with new features

### Cadence Type Guidelines

When working with billing cadences:

1. Always use `Cadence` type instead of raw strings
2. Use constants (`CadenceMonthly`, etc.) when the cadence is known at compile time
3. Use `ParseCadence()` when cadence comes from external input (API, DB, config)
4. Call `cadence.AddTo(t)` to calculate next billing period

### Pull Request Process

1. Fork the repository
2. Create a feature branch
3. Make your changes with tests
4. Ensure all tests pass
5. Submit a pull request with description

## License

[Specify your license here]

## Support

For questions, issues, or contributions, please [open an issue](link-to-issue-tracker) or contact the maintainers.
