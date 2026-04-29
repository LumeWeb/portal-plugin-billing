package stripe

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stripe/stripe-go/v85"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	billingModels "go.lumeweb.com/portal-plugin-billing/internal/db/models"
	quotaCore "go.lumeweb.com/portal-plugin-quota/core"
	"go.lumeweb.com/portal/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
	"gorm.io/gorm"
)

// setupPlanChangeMocks configures all mock services for plan change tests
func setupPlanChangeMocks(ctx coreTesting.TestContext) (*quotaCore.MockQuotaService, *coreTesting.MockUserService, *pluginCore.MockBillingService, *pluginCore.MockPricingService) {
	mockQuota := core.GetService[*quotaCore.MockQuotaService](ctx, quotaCore.QUOTA_SERVICE)
	mockUsers := core.GetService[*coreTesting.MockUserService](ctx, core.USER_SERVICE)
	mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
	mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)
	return mockQuota, mockUsers, mockBilling, mockPricing
}

// TestExecutePlanChange_Success tests the successful plan change flow
func TestExecutePlanChange_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		_, _, mockBilling, mockPricing := setupPlanChangeMocks(ctx)
		mockStripeClient := NewMockStripeClient()

		userID := uint(1)
		newPeriodID := uint(2)
		oldPeriodID := uint(1)
		subscriptionID := "sub_123"
		itemID := "si_123"
		newPriceID := "price_456"
		stripeProductID := "prod_123"

		// Setup new period mock
		newPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: newPeriodID},
			PricingPlanID: 2,
			PriceUSD:      45.00,
			Cadence:       "monthly",
		}
		mockPricing.On("GetPricingPlanPeriod", mock.Anything, newPeriodID).Return(newPeriod, nil).Once()

		// Setup plan mock
		plan := &billingModels.PricingPlan{
			Model:    gorm.Model{ID: 2},
			Name:     "Pro Plan",
			IsActive: true,
		}
		mockPricing.On("GetPricingPlan", mock.Anything, newPeriod.PricingPlanID).Return(plan, nil).Once()

		// Setup gateway product mapping mock
		mapping := &billingModels.GatewayProductMapping{
			Model:                 gorm.Model{ID: 1},
			PricingPlanPeriodID:   &newPeriodID,
			GatewayType:           GatewayID,
			RemoteProductID:       stripeProductID,
			RemotePriceID:         newPriceID,
		}
		mockPricing.On("GetGatewayProductMapping", mock.Anything, newPeriodID, GatewayID).Return(mapping, nil).Once()

		// Setup current subscription mock
		currentSub := &billingModels.Subscriber{
			Model:               gorm.Model{ID: 1},
			UserID:              userID,
			GatewayType:         GatewayID,
			PricingPlanPeriodID: &oldPeriodID,
			SubscriptionID:      subscriptionID,
			ExternalID:          "cus_123",
		}
		mockBilling.On("GetActiveSubscription", mock.Anything, userID).Return(currentSub, nil).Once()

		// Setup Stripe subscription retrieval
		stripeSub := &stripe.Subscription{
			ID: subscriptionID,
			Items: &stripe.SubscriptionItemList{
				Data: []*stripe.SubscriptionItem{
					{
						ID: itemID,
					},
				},
			},
		}
		mockStripeClient.SubscriptionsService.On("Retrieve", mock.Anything, subscriptionID, mock.Anything).Return(stripeSub, nil).Once()

		// Setup Stripe subscription update
		mockStripeClient.SubscriptionsService.On("Update", mock.Anything, subscriptionID, mock.MatchedBy(func(params *stripe.SubscriptionUpdateParams) bool {
			if len(params.Items) != 1 {
				return false
			}
			if params.Items[0].ID == nil || *params.Items[0].ID != itemID {
				return false
			}
			if params.Items[0].Price == nil || *params.Items[0].Price != newPriceID {
				return false
			}
			if params.ProrationBehavior == nil {
				return false
			}
			return *params.ProrationBehavior == "create_prorations"
		})).Return(stripeSub, nil).Once()

		// Create gateway with mock client
		gw := createGateway(ctx, TestWebhookSecret, mockStripeClient, nil, nil, mockBilling, mockPricing, nil)

		// Execute plan change
		result, err := gw.ExecutePlanChange(context.Background(), userID, newPeriodID)

		// Assertions
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, pluginCore.PlanChangeActionComplete, result.Action)
		mockStripeClient.SubscriptionsService.AssertExpectations(t)
	})
}

// TestExecutePlanChange_NewPeriodNotFound tests error when new period doesn't exist
func TestExecutePlanChange_NewPeriodNotFound(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		_, _, mockBilling, mockPricing := setupPlanChangeMocks(ctx)

		userID := uint(1)
		newPeriodID := uint(999)

		mockPricing.On("GetPricingPlanPeriod", mock.Anything, newPeriodID).Return(nil, nil).Once()

		gw := createGateway(ctx, TestWebhookSecret, nil, nil, nil, mockBilling, mockPricing, nil)

		result, err := gw.ExecutePlanChange(context.Background(), userID, newPeriodID)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "new pricing plan period not found")
	})
}

// TestExecutePlanChange_PlanInactive tests error when plan is inactive
func TestExecutePlanChange_PlanInactive(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		_, _, mockBilling, mockPricing := setupPlanChangeMocks(ctx)

		userID := uint(1)
		newPeriodID := uint(2)

		newPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: newPeriodID},
			PricingPlanID: 2,
		}
		mockPricing.On("GetPricingPlanPeriod", mock.Anything, newPeriodID).Return(newPeriod, nil).Once()

		plan := &billingModels.PricingPlan{
			Model:    gorm.Model{ID: 2},
			Name:     "Inactive Plan",
			IsActive: false,
		}
		mockPricing.On("GetPricingPlan", mock.Anything, newPeriod.PricingPlanID).Return(plan, nil).Once()

		gw := createGateway(ctx, TestWebhookSecret, nil, nil, nil, mockBilling, mockPricing, nil)

		result, err := gw.ExecutePlanChange(context.Background(), userID, newPeriodID)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "new plan is not active")
	})
}

// TestExecutePlanChange_NoStripePriceMapping tests error when no Stripe price ID exists
func TestExecutePlanChange_NoStripePriceMapping(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		_, _, mockBilling, mockPricing := setupPlanChangeMocks(ctx)

		userID := uint(1)
		newPeriodID := uint(2)

		newPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: newPeriodID},
			PricingPlanID: 2,
		}
		mockPricing.On("GetPricingPlanPeriod", mock.Anything, newPeriodID).Return(newPeriod, nil).Once()

		plan := &billingModels.PricingPlan{
			Model:    gorm.Model{ID: 2},
			Name:     "Pro Plan",
			IsActive: true,
		}
		mockPricing.On("GetPricingPlan", mock.Anything, newPeriod.PricingPlanID).Return(plan, nil).Once()

		// Return mapping with empty price ID
		mapping := &billingModels.GatewayProductMapping{
			Model:                 gorm.Model{ID: 1},
			PricingPlanPeriodID:   &newPeriodID,
			GatewayType:           GatewayID,
			RemoteProductID:       "prod_123",
			RemotePriceID:         "", // Empty price ID
		}
		mockPricing.On("GetGatewayProductMapping", mock.Anything, newPeriodID, GatewayID).Return(mapping, nil).Once()

		gw := createGateway(ctx, TestWebhookSecret, nil, nil, nil, mockBilling, mockPricing, nil)

		result, err := gw.ExecutePlanChange(context.Background(), userID, newPeriodID)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "no Stripe price ID found")
	})
}

// TestExecutePlanChange_NoActiveSubscription tests error when user has no active subscription
func TestExecutePlanChange_NoActiveSubscription(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		_, _, mockBilling, mockPricing := setupPlanChangeMocks(ctx)
		mockStripeClient := NewMockStripeClient()

		userID := uint(1)
		newPeriodID := uint(2)

		newPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: newPeriodID},
			PricingPlanID: 2,
		}
		mockPricing.On("GetPricingPlanPeriod", mock.Anything, newPeriodID).Return(newPeriod, nil).Once()

		plan := &billingModels.PricingPlan{
			Model:    gorm.Model{ID: 2},
			Name:     "Pro Plan",
			IsActive: true,
		}
		mockPricing.On("GetPricingPlan", mock.Anything, newPeriod.PricingPlanID).Return(plan, nil).Once()

		mapping := &billingModels.GatewayProductMapping{
			Model:                 gorm.Model{ID: 1},
			PricingPlanPeriodID:   &newPeriodID,
			GatewayType:           GatewayID,
			RemoteProductID:       "prod_123",
			RemotePriceID:         "price_456",
		}
		mockPricing.On("GetGatewayProductMapping", mock.Anything, newPeriodID, GatewayID).Return(mapping, nil).Once()

		// No active subscription
		mockBilling.On("GetActiveSubscription", mock.Anything, userID).Return(nil, nil).Once()

		gw := createGateway(ctx, TestWebhookSecret, mockStripeClient, nil, nil, mockBilling, mockPricing, nil)

		result, err := gw.ExecutePlanChange(context.Background(), userID, newPeriodID)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "no active subscription found")
	})
}

// TestExecutePlanChange_WrongGatewayType tests error when subscription is from another gateway
func TestExecutePlanChange_WrongGatewayType(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		_, _, mockBilling, mockPricing := setupPlanChangeMocks(ctx)
		mockStripeClient := NewMockStripeClient()

		userID := uint(1)
		newPeriodID := uint(2)

		newPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: newPeriodID},
			PricingPlanID: 2,
		}
		mockPricing.On("GetPricingPlanPeriod", mock.Anything, newPeriodID).Return(newPeriod, nil).Once()

		plan := &billingModels.PricingPlan{
			Model:    gorm.Model{ID: 2},
			Name:     "Pro Plan",
			IsActive: true,
		}
		mockPricing.On("GetPricingPlan", mock.Anything, newPeriod.PricingPlanID).Return(plan, nil).Once()

		mapping := &billingModels.GatewayProductMapping{
			Model:                 gorm.Model{ID: 1},
			PricingPlanPeriodID:   &newPeriodID,
			GatewayType:           GatewayID,
			RemoteProductID:       "prod_123",
			RemotePriceID:         "price_456",
		}
		mockPricing.On("GetGatewayProductMapping", mock.Anything, newPeriodID, GatewayID).Return(mapping, nil).Once()

		// Subscription from another gateway
		oldPeriodID := uint(1)
		currentSub := &billingModels.Subscriber{
			Model:               gorm.Model{ID: 1},
			UserID:              userID,
			GatewayType:         "atlos", // Wrong gateway
			PricingPlanPeriodID: &oldPeriodID,
			SubscriptionID:      "sub_123",
		}
		mockBilling.On("GetActiveSubscription", mock.Anything, userID).Return(currentSub, nil).Once()

		gw := createGateway(ctx, TestWebhookSecret, mockStripeClient, nil, nil, mockBilling, mockPricing, nil)

		result, err := gw.ExecutePlanChange(context.Background(), userID, newPeriodID)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "active subscription is not from Stripe")
	})
}

// TestExecutePlanChange_EmptySubscriptionID tests error when subscription ID is missing
func TestExecutePlanChange_EmptySubscriptionID(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		_, _, mockBilling, mockPricing := setupPlanChangeMocks(ctx)
		mockStripeClient := NewMockStripeClient()

		userID := uint(1)
		newPeriodID := uint(2)

		newPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: newPeriodID},
			PricingPlanID: 2,
		}
		mockPricing.On("GetPricingPlanPeriod", mock.Anything, newPeriodID).Return(newPeriod, nil).Once()

		plan := &billingModels.PricingPlan{
			Model:    gorm.Model{ID: 2},
			Name:     "Pro Plan",
			IsActive: true,
		}
		mockPricing.On("GetPricingPlan", mock.Anything, newPeriod.PricingPlanID).Return(plan, nil).Once()

		mapping := &billingModels.GatewayProductMapping{
			Model:                 gorm.Model{ID: 1},
			PricingPlanPeriodID:   &newPeriodID,
			GatewayType:           GatewayID,
			RemoteProductID:       "prod_123",
			RemotePriceID:         "price_456",
		}
		mockPricing.On("GetGatewayProductMapping", mock.Anything, newPeriodID, GatewayID).Return(mapping, nil).Once()

		// Subscription with empty subscription ID
		oldPeriodID := uint(1)
		currentSub := &billingModels.Subscriber{
			Model:               gorm.Model{ID: 1},
			UserID:              userID,
			GatewayType:         GatewayID,
			PricingPlanPeriodID: &oldPeriodID,
			SubscriptionID:      "", // Empty subscription ID
		}
		mockBilling.On("GetActiveSubscription", mock.Anything, userID).Return(currentSub, nil).Once()

		gw := createGateway(ctx, TestWebhookSecret, mockStripeClient, nil, nil, mockBilling, mockPricing, nil)

		result, err := gw.ExecutePlanChange(context.Background(), userID, newPeriodID)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "no Stripe subscription ID")
	})
}

// TestExecutePlanChange_StripeSubscriptionRetrieveError tests error handling when Stripe fails to retrieve
func TestExecutePlanChange_StripeSubscriptionRetrieveError(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		_, _, mockBilling, mockPricing := setupPlanChangeMocks(ctx)
		mockStripeClient := NewMockStripeClient()

		userID := uint(1)
		newPeriodID := uint(2)
		oldPeriodID := uint(1)
		subscriptionID := "sub_123"

		newPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: newPeriodID},
			PricingPlanID: 2,
		}
		mockPricing.On("GetPricingPlanPeriod", mock.Anything, newPeriodID).Return(newPeriod, nil).Once()

		plan := &billingModels.PricingPlan{
			Model:    gorm.Model{ID: 2},
			Name:     "Pro Plan",
			IsActive: true,
		}
		mockPricing.On("GetPricingPlan", mock.Anything, newPeriod.PricingPlanID).Return(plan, nil).Once()

		mapping := &billingModels.GatewayProductMapping{
			Model:                 gorm.Model{ID: 1},
			PricingPlanPeriodID:   &newPeriodID,
			GatewayType:           GatewayID,
			RemoteProductID:       "prod_123",
			RemotePriceID:         "price_456",
		}
		mockPricing.On("GetGatewayProductMapping", mock.Anything, newPeriodID, GatewayID).Return(mapping, nil).Once()

		currentSub := &billingModels.Subscriber{
			Model:               gorm.Model{ID: 1},
			UserID:              userID,
			GatewayType:         GatewayID,
			PricingPlanPeriodID: &oldPeriodID,
			SubscriptionID:      subscriptionID,
		}
		mockBilling.On("GetActiveSubscription", mock.Anything, userID).Return(currentSub, nil).Once()

		// Stripe API error
		mockStripeClient.SubscriptionsService.On("Retrieve", mock.Anything, subscriptionID, mock.Anything).Return(
			(*stripe.Subscription)(nil), errors.New("stripe api error")).Once()

		gw := createGateway(ctx, TestWebhookSecret, mockStripeClient, nil, nil, mockBilling, mockPricing, nil)

		result, err := gw.ExecutePlanChange(context.Background(), userID, newPeriodID)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "failed to retrieve Stripe subscription")
	})
}

// TestExecutePlanChange_StripeUpdateError tests error handling when Stripe fails to update
func TestExecutePlanChange_StripeUpdateError(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		_, _, mockBilling, mockPricing := setupPlanChangeMocks(ctx)
		mockStripeClient := NewMockStripeClient()

		userID := uint(1)
		newPeriodID := uint(2)
		oldPeriodID := uint(1)
		subscriptionID := "sub_123"
		itemID := "si_123"
		newPriceID := "price_456"

		newPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: newPeriodID},
			PricingPlanID: 2,
		}
		mockPricing.On("GetPricingPlanPeriod", mock.Anything, newPeriodID).Return(newPeriod, nil).Once()

		plan := &billingModels.PricingPlan{
			Model:    gorm.Model{ID: 2},
			Name:     "Pro Plan",
			IsActive: true,
		}
		mockPricing.On("GetPricingPlan", mock.Anything, newPeriod.PricingPlanID).Return(plan, nil).Once()

		mapping := &billingModels.GatewayProductMapping{
			Model:                 gorm.Model{ID: 1},
			PricingPlanPeriodID:   &newPeriodID,
			GatewayType:           GatewayID,
			RemoteProductID:       "prod_123",
			RemotePriceID:         newPriceID,
		}
		mockPricing.On("GetGatewayProductMapping", mock.Anything, newPeriodID, GatewayID).Return(mapping, nil).Once()

		currentSub := &billingModels.Subscriber{
			Model:               gorm.Model{ID: 1},
			UserID:              userID,
			GatewayType:         GatewayID,
			PricingPlanPeriodID: &oldPeriodID,
			SubscriptionID:      subscriptionID,
		}
		mockBilling.On("GetActiveSubscription", mock.Anything, userID).Return(currentSub, nil).Once()

		stripeSub := &stripe.Subscription{
			ID: subscriptionID,
			Items: &stripe.SubscriptionItemList{
				Data: []*stripe.SubscriptionItem{
					{
						ID: itemID,
					},
				},
			},
		}
		mockStripeClient.SubscriptionsService.On("Retrieve", mock.Anything, subscriptionID, mock.Anything).Return(stripeSub, nil).Once()

		// Stripe update error
		stripeErr := errors.New("stripe update error: no such price")
		mockStripeClient.SubscriptionsService.On("Update", mock.Anything, subscriptionID, mock.Anything).Return(
			(*stripe.Subscription)(nil), stripeErr).Once()

		gw := createGateway(ctx, TestWebhookSecret, mockStripeClient, nil, nil, mockBilling, mockPricing, nil)

		result, err := gw.ExecutePlanChange(context.Background(), userID, newPeriodID)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "failed to update subscription")
	})
}

// TestExecutePlanChange_NoSubscriptionItems tests error when subscription has no items
func TestExecutePlanChange_NoSubscriptionItems(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		_, _, mockBilling, mockPricing := setupPlanChangeMocks(ctx)
		mockStripeClient := NewMockStripeClient()

		userID := uint(1)
		newPeriodID := uint(2)
		oldPeriodID := uint(1)
		subscriptionID := "sub_123"

		newPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: newPeriodID},
			PricingPlanID: 2,
		}
		mockPricing.On("GetPricingPlanPeriod", mock.Anything, newPeriodID).Return(newPeriod, nil).Once()

		plan := &billingModels.PricingPlan{
			Model:    gorm.Model{ID: 2},
			Name:     "Pro Plan",
			IsActive: true,
		}
		mockPricing.On("GetPricingPlan", mock.Anything, newPeriod.PricingPlanID).Return(plan, nil).Once()

		mapping := &billingModels.GatewayProductMapping{
			Model:                 gorm.Model{ID: 1},
			PricingPlanPeriodID:   &newPeriodID,
			GatewayType:           GatewayID,
			RemoteProductID:       "prod_123",
			RemotePriceID:         "price_456",
		}
		mockPricing.On("GetGatewayProductMapping", mock.Anything, newPeriodID, GatewayID).Return(mapping, nil).Once()

		currentSub := &billingModels.Subscriber{
			Model:               gorm.Model{ID: 1},
			UserID:              userID,
			GatewayType:         GatewayID,
			PricingPlanPeriodID: &oldPeriodID,
			SubscriptionID:      subscriptionID,
		}
		mockBilling.On("GetActiveSubscription", mock.Anything, userID).Return(currentSub, nil).Once()

		// Subscription with no items
		stripeSub := &stripe.Subscription{
			ID:    subscriptionID,
			Items: &stripe.SubscriptionItemList{Data: []*stripe.SubscriptionItem{}},
		}
		mockStripeClient.SubscriptionsService.On("Retrieve", mock.Anything, subscriptionID, mock.Anything).Return(stripeSub, nil).Once()

		gw := createGateway(ctx, TestWebhookSecret, mockStripeClient, nil, nil, mockBilling, mockPricing, nil)

		result, err := gw.ExecutePlanChange(context.Background(), userID, newPeriodID)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "subscription has no items")
	})
}

// TestExecutePlanChange_MappingNotFound tests error when gateway mapping is nil
func TestExecutePlanChange_MappingNotFound(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		_, _, mockBilling, mockPricing := setupPlanChangeMocks(ctx)

		userID := uint(1)
		newPeriodID := uint(2)

		newPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: newPeriodID},
			PricingPlanID: 2,
		}
		mockPricing.On("GetPricingPlanPeriod", mock.Anything, newPeriodID).Return(newPeriod, nil).Once()

		plan := &billingModels.PricingPlan{
			Model:    gorm.Model{ID: 2},
			Name:     "Pro Plan",
			IsActive: true,
		}
		mockPricing.On("GetPricingPlan", mock.Anything, newPeriod.PricingPlanID).Return(plan, nil).Once()

		// Nil mapping - no Stripe price configured for this period
		mockPricing.On("GetGatewayProductMapping", mock.Anything, newPeriodID, GatewayID).Return(nil, nil).Once()

		gw := createGateway(ctx, TestWebhookSecret, nil, nil, nil, mockBilling, mockPricing, nil)

		result, err := gw.ExecutePlanChange(context.Background(), userID, newPeriodID)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "no Stripe price ID found")
	})
}

// TestGetManagementInfo_AdminCanChangePlan tests that admin plan changes are now supported
func TestGetManagementInfo_AdminCanChangePlan(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		_, _, mockBilling, mockPricing := setupPlanChangeMocks(ctx)

		gw := createGateway(ctx, TestWebhookSecret, nil, nil, nil, mockBilling, mockPricing, nil)

		info, err := gw.GetManagementInfo(context.Background(), uint(1))

		assert.NoError(t, err)
		assert.NotNil(t, info)
		// Admin should now be able to change plans
		assert.True(t, info.AdminOperations[pluginCore.OperationChangePlan])
	})
}
