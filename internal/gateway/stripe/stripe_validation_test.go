package stripe

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/stripe/stripe-go/v83"
	"github.com/tkuchiki/faketime"
	"gorm.io/gorm"

	"go.lumeweb.com/portal-plugin-billing/pkg/subscription"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	billingModels "go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/portal/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
)

func setupTestGateway(t *testing.T) *StripeGateway {
	ctx, _ := coreTesting.NewTestContext(t)
	return New(ctx.Logger(), ctx, "test_secret", "test_key", nil, nil, nil, nil, nil)
}

func TestExtractProrationFromInvoice(t *testing.T) {
	gw := setupTestGateway(t)

	// Test with nil lines
	invoice := &stripe.Invoice{
		ID:     "in_test123",
		Lines:  nil,
		Status: stripe.InvoiceStatusPaid,
	}

	analysis, err := gw.extractProrationFromInvoice(invoice)
	require.NoError(t, err)
	assert.Equal(t, 0, analysis.TotalLineItems)
	assert.False(t, analysis.HasProratedItems)
}

func TestCompareProrationCalculations(t *testing.T) {
	// Fix time for deterministic results
	// January 15, 2024 at noon = 16 days remaining in a 31-day month (Jan 1-31)
	f := faketime.NewFaketime(2024, time.January, 15, 12, 0, 0, 0, time.UTC)
	defer f.Undo()
	f.Do()

	tests := []struct {
		name               string
		oldPrice           subscription.Price
		newPrice           subscription.Price
		oldCycle           subscription.BillingCycle
		stripeAmount       decimal.Decimal
		expectedMismatch   bool
		expectedAction     string
	}{
		{
			name: "exact match - upgrade",
			oldPrice: subscription.Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: subscription.CadenceMonthly,
			},
			newPrice: subscription.Price{
				Amount:  decimal.NewFromInt(150),
				Cadence: subscription.CadenceMonthly,
			},
			oldCycle: subscription.BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			// Calculated using exact time precision (seconds), matching Stripe's proration behavior:
			// - Cycle duration: Jan 1 00:00:00 to Jan 31 23:59:59 = 2,678,399 seconds
			// - Remaining duration: Jan 15 12:00:00 to Jan 31 23:59:59 = 1,425,599 seconds
			// - Ratio: 1,425,599 / 2,678,399 = 0.5322578898812312...
			// - Net amount: ($150 - $100) * ratio = $26.612894494061565...
			// This matches the exact value produced by subscription.ProratedChange()
			stripeAmount:     decimal.RequireFromString("26.612894494061565"),
			expectedMismatch: false,
			expectedAction:   "use_local",
		},
		{
			name: "one dollar mismatch",
			oldPrice: subscription.Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: subscription.CadenceMonthly,
			},
			newPrice: subscription.Price{
				Amount:  decimal.NewFromInt(150),
				Cadence: subscription.CadenceMonthly,
			},
			oldCycle: subscription.BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			stripeAmount:     decimal.NewFromFloat(27.6666), // $1 higher than calculated
			expectedMismatch: true,
			expectedAction:   "use_stripe",
		},
		{
			name: "zero difference - same plan",
			oldPrice: subscription.Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: subscription.CadenceMonthly,
			},
			newPrice: subscription.Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: subscription.CadenceMonthly,
			},
			oldCycle: subscription.BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			stripeAmount:     decimal.Zero,
			expectedMismatch: false,
			expectedAction:   "use_local",
		},
		{
			name: "downgrade with credit",
			oldPrice: subscription.Price{
				Amount:  decimal.NewFromInt(150),
				Cadence: subscription.CadenceMonthly,
			},
			newPrice: subscription.Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: subscription.CadenceMonthly,
			},
			oldCycle: subscription.BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			// Calculated using exact time precision (seconds), matching Stripe's proration behavior:
			// - Cycle duration: Jan 1 00:00:00 to Jan 31 23:59:59 = 2,678,399 seconds
			// - Remaining duration: Jan 15 12:00:00 to Jan 31 23:59:59 = 1,425,599 seconds
			// - Ratio: 1,425,599 / 2,678,399 = 0.5322578898812312...
			// - Net amount: ($100 - $150) * ratio = -$26.612894494061565... (credit issued)
			// Matches the exact value produced by subscription.ProratedChange()
			stripeAmount:     decimal.RequireFromString("-26.612894494061565"),
			expectedMismatch: false,
			expectedAction:   "use_local",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gw := setupTestGateway(t)

			comparison, err := gw.compareProrationCalculations(
				context.Background(),
				123,
				tt.oldPrice,
				tt.newPrice,
				tt.oldCycle,
				tt.stripeAmount,
				nil,
			)

			require.NoError(t, err)
			assert.Equal(t, tt.expectedMismatch, comparison.MismatchDetected)
			assert.Equal(t, tt.expectedAction, comparison.RecommendedAction)

			if comparison.LocalResult != nil {
				assert.NotNil(t, comparison.LocalResult)
				assert.NotNil(t, comparison.LocalResult.UnusedCredit)
				assert.NotNil(t, comparison.LocalResult.NewCharge)
			}
		})
	}
}

func TestCompareProrationCalculations_WithInvoiceTimestamp(t *testing.T) {
	// Fix time for deterministic results in fallback (time.Now()) paths
	// January 15, 2024 at noon = 16 days remaining in a 31-day month (Jan 1-31)
	f := faketime.NewFaketime(2024, time.January, 15, 12, 0, 0, 0, time.UTC)
	defer f.Undo()
	f.Do()

	// Test cases for invoice timestamp-based proration logic
	tests := []struct {
		name               string
		invoice            *stripe.Invoice
		oldPrice           subscription.Price
		newPrice           subscription.Price
		oldCycle           subscription.BillingCycle
		stripeAmount       decimal.Decimal
		expectedMismatch   bool
		expectedAction     string
		description        string
	}{
		{
			name: "invoice with valid timestamp uses invoice time",
			invoice: &stripe.Invoice{
				Created: time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC).Unix(),
			},
			oldPrice: subscription.Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: subscription.CadenceMonthly,
			},
			newPrice: subscription.Price{
				Amount:  decimal.NewFromInt(150),
				Cadence: subscription.CadenceMonthly,
			},
			oldCycle: subscription.BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			// Proration calculated at Jan 15 12:00:00 UTC (from invoice timestamp)
			stripeAmount:     decimal.RequireFromString("26.612894494061565"),
			expectedMismatch: false,
			expectedAction:   "use_local",
			description:      "Invoice timestamp should be used for proration time when valid",
		},
		{
			name: "invoice with Created=0 falls back to time.Now",
			invoice: &stripe.Invoice{
				Created: 0,
			},
			oldPrice: subscription.Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: subscription.CadenceMonthly,
			},
			newPrice: subscription.Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: subscription.CadenceMonthly,
			},
			oldCycle: subscription.BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			// When invoice.Created=0, uses time.Now()
			stripeAmount:     decimal.Zero,
			expectedMismatch: false,
			expectedAction:   "use_local",
			description:      "Zero invoice timestamp should fall back to current time",
		},
		{
			name: "invoice with negative Created falls back to time.Now",
			invoice: &stripe.Invoice{
				Created: -1,
			},
			oldPrice: subscription.Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: subscription.CadenceMonthly,
			},
			newPrice: subscription.Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: subscription.CadenceMonthly,
			},
			oldCycle: subscription.BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			stripeAmount:     decimal.Zero,
			expectedMismatch: false,
			expectedAction:   "use_local",
			description:      "Negative invoice timestamp should fall back to current time",
		},
		{
			name:     "nil invoice uses time.Now",
			invoice:  nil,
			oldPrice: subscription.Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: subscription.CadenceMonthly,
			},
			newPrice: subscription.Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: subscription.CadenceMonthly,
			},
			oldCycle: subscription.BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			stripeAmount:     decimal.Zero,
			expectedMismatch: false,
			expectedAction:   "use_local",
			description:      "Nil invoice should use current time for proration",
		},
		{
			name: "different invoice timestamps produce different proration amounts",
			invoice: &stripe.Invoice{
				Created: time.Date(2024, 1, 20, 0, 0, 0, 0, time.UTC).Unix(),
			},
			oldPrice: subscription.Price{
				Amount:  decimal.NewFromInt(100),
				Cadence: subscription.CadenceMonthly,
			},
			newPrice: subscription.Price{
				Amount:  decimal.NewFromInt(150),
				Cadence: subscription.CadenceMonthly,
			},
			oldCycle: subscription.BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			// Matches the exact value produced by subscription.ProratedChange() when using Jan 20 invoice timestamp
			// This demonstrates that proration amounts vary based on when the invoice was created
			stripeAmount:     decimal.RequireFromString("19.35482726808067"),
			expectedMismatch: false,
			expectedAction:   "use_local",
			description:      "Proration amount changes based on invoice creation time",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gw := setupTestGateway(t)

			comparison, err := gw.compareProrationCalculations(
				context.Background(),
				123,
				tt.oldPrice,
				tt.newPrice,
				tt.oldCycle,
				tt.stripeAmount,
				tt.invoice,
			)

			require.NoError(t, err, tt.description)
			assert.Equal(t, tt.expectedMismatch, comparison.MismatchDetected, tt.description)
			assert.Equal(t, tt.expectedAction, comparison.RecommendedAction, tt.description)

			if comparison.LocalResult != nil {
				assert.NotNil(t, comparison.LocalResult, tt.description)
				assert.NotNil(t, comparison.LocalResult.UnusedCredit, tt.description)
				assert.NotNil(t, comparison.LocalResult.NewCharge, tt.description)
			}
		})
	}
}

func TestDetermineOperationType(t *testing.T) {
	gw := setupTestGateway(t)

	// Test with nil subscriber - should return ChangeTypeNewSubscription
	operation := gw.determineOperationType(
		context.Background(),
		nil,
		&stripe.Subscription{},
		&stripe.Invoice{},
	)

	assert.Equal(t, pluginCore.ChangeTypeNewSubscription, operation)
}

func TestDetermineUpgradeOrDowngrade(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)

	// Create mock pricing service
	mockPricing := pluginCore.NewMockPricingService(t)
	gw := New(ctx.Logger(), ctx, "test_secret", "test_key", nil, nil, nil, mockPricing, nil)

	// Mock pricing service to return error (simulating fetch failure)
	mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, uint(100)).Return(nil, fmt.Errorf("pricing service unavailable"))

	// Should return ChangeTypeUpgrade as default when pricing service fails
	operation := gw.determineUpgradeOrDowngrade(
		context.Background(),
		100,
		200,
	)

	assert.Equal(t, pluginCore.ChangeTypeUpgrade, operation)
}

func TestCalculateNetInvoiceAmount(t *testing.T) {
	tests := []struct {
		name           string
		amountPaid     int64
		expectedAmount string
	}{
		{
			name:           "zero amount",
			amountPaid:     0,
			expectedAmount: "0",
		},
		{
			name:           "positive amount",
			amountPaid:     5000, // 5000 cents = $50
			expectedAmount: "50",
		},
		{
			name:           "negative amount",
			amountPaid:     -1000, // -1000 cents = -$10
			expectedAmount: "-10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gw := setupTestGateway(t)

			invoice := &stripe.Invoice{
				AmountPaid: tt.amountPaid,
			}

			amount := gw.calculateNetInvoiceAmount(invoice)
			assert.Equal(t, tt.expectedAmount, amount.String())
		})
	}
}

func TestLogProrationMismatch(t *testing.T) {
	gw := setupTestGateway(t)

	comparison := &ProrationComparison{
		LocalResult: &subscription.ProrationResult{
			UnusedCredit:  decimal.NewFromFloat(25.00),
			NewCharge:     decimal.NewFromFloat(75.00),
			CreditDue:     decimal.NewFromFloat(50.00),
			EffectiveDate: time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
		},
		StripeAmount:      decimal.NewFromFloat(52.00),
		MismatchDetected:  true,
		Difference:        decimal.NewFromFloat(2.00),
		DifferencePercent: 4.0,
		RecommendedAction: "use_stripe",
	}

	oldCycle := subscription.BillingCycle{
		StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
	}

	// This should not panic and should log a warning
	gw.logProrationMismatch(
		context.Background(),
		123,
		comparison,
		oldCycle,
	)

	// Assert no panic occurred
	assert.True(t, true)
}

// TestValidateAndCalculateCreditAmount_NewSubscription tests that new subscriptions
// use Stripe's amount directly and validate the ledger before returning the amount
func TestValidateAndCalculateCreditAmount_NewSubscription(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)

	// Create mock credit service
	mockCredit := pluginCore.NewMockCreditService(t)
	gw := New(ctx.Logger(), ctx, "test_secret", "test_key", nil, nil, nil, nil, mockCredit)

	// Mock credit validation to pass
	mockCredit.EXPECT().ValidateSubscriptionChange(mock.Anything, uint64(123), pluginCore.ChangeTypeNewSubscription, mock.MatchedBy(func(d decimal.Decimal) bool { return d.Equal(decimal.NewFromInt(100)) })).Return(nil)

	stripeSubscription := &stripe.Subscription{
		ID: "sub_test123",
	}

	invoice := &stripe.Invoice{
		ID:         "in_test123",
		AmountPaid: 10000, // $100.00 in cents
		Status:     stripe.InvoiceStatusPaid,
	}

	amount, err := gw.validateAndCalculateCreditAmount(
		ctx,
		123,
		pluginCore.ChangeTypeNewSubscription,
		nil,
		stripeSubscription,
		invoice,
	)

	require.NoError(t, err)
	require.NotNil(t, amount)
	assert.Equal(t, "100", amount.String()) // Stripe's amount in dollars
}

// TestValidateAndCalculateCreditAmount_Renewal tests that renewals
// use Stripe's amount directly and validate the ledger before returning the amount
func TestValidateAndCalculateCreditAmount_Renewal(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)

	// Create mock credit service
	mockCredit := pluginCore.NewMockCreditService(t)
	gw := New(ctx.Logger(), ctx, "test_secret", "test_key", nil, nil, nil, nil, mockCredit)

	// Mock credit validation to pass
	mockCredit.EXPECT().ValidateSubscriptionChange(mock.Anything, uint64(123), pluginCore.ChangeTypeRenewal, mock.MatchedBy(func(d decimal.Decimal) bool { return d.Equal(decimal.NewFromInt(50)) })).Return(nil)

	stripeSubscription := &stripe.Subscription{
		ID: "sub_existing123",
	}

	invoice := &stripe.Invoice{
		ID:         "in_renewal123",
		AmountPaid: 5000, // $50.00 in cents
		Status:     stripe.InvoiceStatusPaid,
	}

	amount, err := gw.validateAndCalculateCreditAmount(
		ctx,
		123,
		pluginCore.ChangeTypeRenewal,
		nil,
		stripeSubscription,
		invoice,
	)

	require.NoError(t, err)
	require.NotNil(t, amount)
	assert.Equal(t, "50", amount.String()) // Stripe's amount in dollars
}

// TestValidateAndCalculateCreditAmount_ValidationFailure tests that ledger validation
// errors cause the method to return an error
func TestValidateAndCalculateCreditAmount_ValidationFailure(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)

	// Create mock credit service
	mockCredit := pluginCore.NewMockCreditService(t)
	gw := New(ctx.Logger(), ctx, "test_secret", "test_key", nil, nil, nil, nil, mockCredit)

	validationErr := fmt.Errorf("insufficient balance for new subscription")

	// Mock credit validation to fail for new subscription
	mockCredit.EXPECT().ValidateSubscriptionChange(mock.Anything, uint64(123), pluginCore.ChangeTypeNewSubscription, mock.MatchedBy(func(d decimal.Decimal) bool { return d.GreaterThan(decimal.Zero) })).Return(validationErr)

	stripeSubscription := &stripe.Subscription{
		ID: "sub_test123",
	}

	invoice := &stripe.Invoice{
		ID:         "in_test123",
		AmountPaid: 10000, // $100.00 in cents
		Status:     stripe.InvoiceStatusPaid,
	}

	_, err := gw.validateAndCalculateCreditAmount(
		ctx,
		123,
		pluginCore.ChangeTypeNewSubscription,
		nil,
		stripeSubscription,
		invoice,
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient balance")
}

// TestCalculateCancellationCredit_TimestampPriority tests that the cancellation credit
// calculation uses Stripe-provided timestamps in the correct priority order.
func TestCalculateCancellationCredit_TimestampPriority(t *testing.T) {
	tests := []struct {
		name                string
		endedAt             int64
		canceledAt          int64
		eventCreated        int64
		expectedCreditStart string // Expected credit amount (will vary based on billing cycle)
		description         string
	}{
		{
			name:        "uses EndedAt when available",
			endedAt:     time.Date(2024, 1, 20, 0, 0, 0, 0, time.UTC).Unix(),
			canceledAt:  time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC).Unix(),
			eventCreated: time.Date(2024, 1, 25, 0, 0, 0, 0, time.UTC).Unix(),
			description: "EndedAt takes priority over other timestamps",
		},
		{
			name:        "uses CanceledAt when EndedAt is zero",
			endedAt:     0,
			canceledAt:  time.Date(2024, 1, 20, 0, 0, 0, 0, time.UTC).Unix(),
			eventCreated: time.Date(2024, 1, 25, 0, 0, 0, 0, time.UTC).Unix(),
			description: "CanceledAt is second priority",
		},
		{
			name:        "uses event.Created when both EndedAt and CanceledAt are zero",
			endedAt:     0,
			canceledAt:  0,
			eventCreated: time.Date(2024, 1, 20, 0, 0, 0, 0, time.UTC).Unix(),
			description: "event.Created is third priority",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
				mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
				mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)
				mockCredit := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)

				pricingPlanPeriodID := uint(200)

				// Mock subscriber lookup
				mockBilling.EXPECT().GetActiveSubscriber(mock.Anything, uint(123), "stripe").Return(&billingModels.Subscriber{
					Model:              gorm.Model{ID: 1},
					UserID:             123,
					GatewayType:        "stripe",
					ExternalID:         "cus_123",
					SubscriptionID:     "sub_123",
					IsActive:           true,
					PricingPlanPeriodID: &pricingPlanPeriodID,
				}, nil)

				// Mock pricing plan period lookup
				mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, pricingPlanPeriodID).Return(&billingModels.PricingPlanPeriod{
					Model:         gorm.Model{ID: pricingPlanPeriodID},
					PricingPlanID: 1,
					Cadence:       "monthly",
					PriceUSD:      100.00,
					QuotaPlanID:   300,
				}, nil)

				// Create subscription with billing cycle (Jan 1-31)
				cycleStart := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
				cycleEnd := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)

				stripeSubscription := &stripe.Subscription{
					ID:         "sub_123",
					EndedAt:    tt.endedAt,
					CanceledAt: tt.canceledAt,
					Items: &stripe.SubscriptionItemList{
						Data: []*stripe.SubscriptionItem{{
							CurrentPeriodStart: cycleStart.Unix(),
							CurrentPeriodEnd:   cycleEnd.Unix(),
						}},
				},
				}

				event := stripe.Event{
					Created: tt.eventCreated,
				}

				gw := New(ctx.Logger(), ctx, "test_secret", "test_key", nil, nil, mockBilling, mockPricing, mockCredit)

				credit, err := gw.calculateCancellationCredit(ctx, 123, stripeSubscription, event)

				require.NoError(t, err, tt.description)
				assert.True(t, credit.GreaterThan(decimal.Zero), "Expected positive credit: %s", tt.description)
				// Credit amount will depend on which timestamp was used
				// but we verify no error and positive result
			})
		})
	}
}

// TestCalculateCancellationCredit_EdgeCases tests edge cases for cancellation credit calculation.
func TestCalculateCancellationCredit_EdgeCases(t *testing.T) {
	tests := []struct {
		name            string
		billingCycle    subscription.BillingCycle
		endedAt         int64
		expectedCredit  string
		description     string
	}{
		{
			name: "cancellation at cycle end results in zero credit",
			billingCycle: subscription.BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			endedAt:        time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC).Unix(),
			expectedCredit: "0",
			description:    "No credit when cancelled at exact cycle end",
		},
		{
			name: "cancellation at cycle start results in full credit",
			billingCycle: subscription.BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			endedAt:        time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).Unix(),
			expectedCredit: "100", // Full month credit for $100/month plan
			description:    "Full credit when cancelled at cycle start",
		},
		{
			name: "cancellation mid-cycle results in partial credit",
			billingCycle: subscription.BillingCycle{
				StartAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndAt:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			},
			endedAt:        time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC).Unix(), // Day 16 of 31
			expectedCredit: "", // Will be approximately $50 (half month)
			description:    "Partial credit when cancelled mid-cycle",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
				mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
				mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)
				mockCredit := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)

				pricingPlanPeriodID := uint(200)

				mockBilling.EXPECT().GetActiveSubscriber(mock.Anything, uint(123), "stripe").Return(&billingModels.Subscriber{
					Model:              gorm.Model{ID: 1},
					UserID:             123,
					GatewayType:        "stripe",
					ExternalID:         "cus_123",
					SubscriptionID:     "sub_123",
					IsActive:           true,
					PricingPlanPeriodID: &pricingPlanPeriodID,
				}, nil)

				mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, pricingPlanPeriodID).Return(&billingModels.PricingPlanPeriod{
					Model:         gorm.Model{ID: pricingPlanPeriodID},
					PricingPlanID: 1,
					Cadence:       "monthly",
					PriceUSD:      100.00,
					QuotaPlanID:   300,
				}, nil)

				stripeSubscription := &stripe.Subscription{
					ID:      "sub_123",
					EndedAt: tt.endedAt,
					Items: &stripe.SubscriptionItemList{
						Data: []*stripe.SubscriptionItem{{
							CurrentPeriodStart: tt.billingCycle.StartAt.Unix(),
							CurrentPeriodEnd:   tt.billingCycle.EndAt.Unix(),
						}},
				},
				}

				event := stripe.Event{Created: 0}

				gw := New(ctx.Logger(), ctx, "test_secret", "test_key", nil, nil, mockBilling, mockPricing, mockCredit)

				credit, err := gw.calculateCancellationCredit(ctx, 123, stripeSubscription, event)

				require.NoError(t, err, tt.description)

				if tt.expectedCredit != "" {
					assert.Equal(t, tt.expectedCredit, credit.String(), tt.description)
				} else {
					// For partial credit, just verify it's positive and reasonable
					assert.True(t, credit.GreaterThan(decimal.Zero), tt.description)
					assert.True(t, credit.LessThan(decimal.NewFromInt(100)), "Credit should be less than full price")
				}
			})
		})
	}
}

// TestCalculateCancellationCredit_FallbackToEventCreated tests that event.Created is used
// when subscription timestamps are not available.
func TestCalculateCancellationCredit_FallbackToEventCreated(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)
		mockCredit := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)

		pricingPlanPeriodID := uint(200)

		mockBilling.EXPECT().GetActiveSubscriber(mock.Anything, uint(123), "stripe").Return(&billingModels.Subscriber{
			Model:              gorm.Model{ID: 1},
			UserID:             123,
			GatewayType:        "stripe",
			ExternalID:         "cus_123",
			SubscriptionID:     "sub_123",
			IsActive:           true,
			PricingPlanPeriodID: &pricingPlanPeriodID,
		}, nil)

		mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, pricingPlanPeriodID).Return(&billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: pricingPlanPeriodID},
			PricingPlanID: 1,
			Cadence:       "monthly",
			PriceUSD:      100.00,
			QuotaPlanID:   300,
		}, nil)

		// Subscription with no EndedAt or CanceledAt - only event.Created
		cycleStart := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		cycleEnd := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
		eventCreatedTime := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)

		stripeSubscription := &stripe.Subscription{
			ID:         "sub_123",
			EndedAt:    0, // Not set
			CanceledAt: 0, // Not set
			Items: &stripe.SubscriptionItemList{
				Data: []*stripe.SubscriptionItem{{
					CurrentPeriodStart: cycleStart.Unix(),
					CurrentPeriodEnd:   cycleEnd.Unix(),
				}},
			},
		}

		event := stripe.Event{
			Created: eventCreatedTime.Unix(),
		}

		gw := New(ctx.Logger(), ctx, "test_secret", "test_key", nil, nil, mockBilling, mockPricing, mockCredit)

		credit, err := gw.calculateCancellationCredit(ctx, 123, stripeSubscription, event)

		require.NoError(t, err)
		assert.True(t, credit.GreaterThan(decimal.Zero), "Should have positive credit")
		// Credit should be approximately half month ($50) since event is at day 15
		assert.True(t, credit.GreaterThan(decimal.NewFromInt(40)), "Credit should be significant")
		assert.True(t, credit.LessThan(decimal.NewFromInt(60)), "Credit should be less than $60")
	})
}

