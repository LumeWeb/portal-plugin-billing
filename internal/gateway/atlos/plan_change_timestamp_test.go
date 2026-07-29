package atlos

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/tkuchiki/faketime"
	"gorm.io/gorm"

	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	billingModels "go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/portal-plugin-billing/pkg/subscription"
	core "go.lumeweb.com/portal/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
)

// TestCalculatePlanChangeProration_ProrationTimeClamping verifies that
// calculatePlanChangeProration clamps the proration timestamp to billing
// cycle boundaries. Without clamping, a proration time outside the cycle
// triggers "proration date must be within billing cycle" from
// subscription.ValidateProrationInputs.
//
// This scenario occurs when MySQL TIMESTAMP truncation (nanosecond → microsecond)
// makes stored BillingPeriodEnd slightly earlier than time.Now(), or when
// clock skew causes time.Now() to exceed the cycle end.
func TestCalculatePlanChangeProration_ProrationTimeClampedToCycle(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		gateway := &AtlosGateway{
			coreCtx: ctx,
			pricing: mockPricing,
			billing: mockBilling,
		}

		userID := uint(1)
		newPeriodID := uint(2)

		// Freeze time at subscription creation
		createdAt := time.Date(2026, 4, 22, 14, 30, 0, 123456789, time.UTC)
		f := faketime.NewFaketimeWithTime(createdAt)
		f.Do()

		cycle := subscription.CalculateFirstCycle(time.Now().UTC(), subscription.CadenceMonthly)

		// Simulate MySQL TIMESTAMP truncation (loses nanosecond precision)
		storedStart := cycle.StartAt.Truncate(time.Microsecond)
		storedEnd := cycle.EndAt.Truncate(time.Microsecond)

		// Set up mock expectations
		newPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: 2},
			PricingPlanID: 2,
			PriceUSD:      19.99,
			Cadence:       "monthly",
		}
		mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, newPeriodID).Return(newPeriod, nil).Once()

		plan := &billingModels.PricingPlan{
			Model:       gorm.Model{ID: 2},
			Name:        "Pro Plan",
			Description: "Pro",
			IsActive:    true,
		}
		mockPricing.EXPECT().GetPricingPlan(mock.Anything, uint(2)).Return(plan, nil).Once()

		oldPeriodID := uint(1)
		currentSub := &billingModels.Subscriber{
			Model:               gorm.Model{ID: 1},
			UserID:              userID,
			GatewayType:         GatewayID,
			PricingPlanPeriodID: &oldPeriodID,
			BillingPeriodStart:  &storedStart,
			BillingPeriodEnd:    &storedEnd,
			SubscriptionID:      "sub-123",
			ExternalID:          "ext-123",
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(currentSub, nil).Once()

		oldPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: 1},
			PricingPlanID: 1,
			PriceUSD:      49.99,
			Cadence:       "monthly",
		}
		mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, oldPeriodID).Return(oldPeriod, nil).Once()

		// Advance time to 1 second after subscription creation
		f.Undo()
		prorationTime := createdAt.Add(1 * time.Second)
		f = faketime.NewFaketimeWithTime(prorationTime)
		f.Do()
		defer f.Undo()

		calc, err := gateway.calculatePlanChangeProration(context.Background(), userID, newPeriodID)

		require.NoError(t, err, "calculatePlanChangeProration should succeed with clamped proration time")
		require.NotNil(t, calc)
		assert.Equal(t, PlanChangeActionCreditOnly, calc.ActionType, "downgrade should be credit-only")
		assert.True(t, calc.CreditToIssue.GreaterThan(decimal.Zero), "downgrade should produce credit")
	})
}

// TestCalculatePlanChangeProration_ExpiredBillingCycleClamped verifies that
// calculatePlanChangeProration handles the edge case where BillingPeriodEnd
// has already passed (stale subscriber record). The clamping ensures the
// proration calculation uses the EndAt instead of failing.
func TestCalculatePlanChangeProration_ExpiredBillingCycleClamped(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		gateway := &AtlosGateway{
			coreCtx: ctx,
			pricing: mockPricing,
			billing: mockBilling,
		}

		userID := uint(1)
		newPeriodID := uint(2)

		// Create a billing cycle that expired 2 months ago
		now := time.Now().UTC()
		twoMonthsAgo := now.AddDate(0, -2, 0)
		oneMonthAgo := now.AddDate(0, -1, 0)

		newPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: 2},
			PricingPlanID: 2,
			PriceUSD:      19.99,
			Cadence:       "monthly",
		}
		mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, newPeriodID).Return(newPeriod, nil).Once()

		plan := &billingModels.PricingPlan{
			Model:       gorm.Model{ID: 2},
			Name:        "Pro Plan",
			Description: "Pro",
			IsActive:    true,
		}
		mockPricing.EXPECT().GetPricingPlan(mock.Anything, uint(2)).Return(plan, nil).Once()

		oldPeriodID := uint(1)
		currentSub := &billingModels.Subscriber{
			Model:               gorm.Model{ID: 1},
			UserID:              userID,
			GatewayType:         GatewayID,
			PricingPlanPeriodID: &oldPeriodID,
			BillingPeriodStart:  &twoMonthsAgo,
			BillingPeriodEnd:    &oneMonthAgo,
			SubscriptionID:      "sub-123",
			ExternalID:          "ext-123",
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(currentSub, nil).Once()

		oldPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: 1},
			PricingPlanID: 1,
			PriceUSD:      49.99,
			Cadence:       "monthly",
		}
		mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, oldPeriodID).Return(oldPeriod, nil).Once()

		// With clamping, this should succeed (proration time clamped to cycle end)
		calc, err := gateway.calculatePlanChangeProration(context.Background(), userID, newPeriodID)

		require.NoError(t, err, "calculatePlanChangeProration should succeed with clamped proration time for expired cycle")
		require.NotNil(t, calc)
		// When proration time is clamped to cycle end, unused credit = 0, new charge = 0 → zero_amount
		assert.Equal(t, PlanChangeActionZeroAmount, calc.ActionType, "clamped-to-end expired cycle should be zero_amount")
	})
}
