package stripe

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stripe/stripe-go/v85"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
)

func TestStripeGateway_ExecutePause_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		mockStripeClient := &MockStripeClient{}

		userID := uint(123)
		planID := uint(42)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:              userID,
			GatewayType:         "stripe",
			ExternalID:          TestCustomerID,
			SubscriptionID:      TestSubscriptionID,
			IsActive:            true,
			PricingPlanPeriodID: &planID,
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(mockSubscriber, nil)

		mockSubService := &MockSubscriptions{}
		mockSubService.On("Update", mock.Anything, TestSubscriptionID, mock.AnythingOfType("*stripe.SubscriptionUpdateParams")).
			Return(&stripe.Subscription{ID: TestSubscriptionID}, nil)
		mockStripeClient.SubscriptionsService = mockSubService

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), nil, nil, mockBilling, nil, nil)
		gw.stripeClient = mockStripeClient

		err := gw.ExecutePause(context.Background(), userID)
		assert.NoError(tb, err)
		mockSubService.AssertExpectations(tb)
	})
}

func TestStripeGateway_ExecutePause_NoActiveSubscription(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		userID := uint(123)
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(nil, nil)

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), nil, nil, mockBilling, nil, nil)

		err := gw.ExecutePause(context.Background(), userID)
		assert.Error(tb, err)
		assert.Contains(tb, err.Error(), "no active stripe subscription found")
	})
}

func TestStripeGateway_ExecutePause_WrongGateway(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		userID := uint(123)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:         userID,
			GatewayType:    "atlos",
			SubscriptionID: TestSubscriptionID,
			IsActive:       true,
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(mockSubscriber, nil)

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), nil, nil, mockBilling, nil, nil)

		err := gw.ExecutePause(context.Background(), userID)
		assert.Error(tb, err)
		assert.Contains(tb, err.Error(), "no active stripe subscription found")
	})
}

func TestStripeGateway_ExecutePause_NoBillingService(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), nil, nil, nil, nil, nil)

		err := gw.ExecutePause(context.Background(), 123)
		assert.Error(tb, err)
		assert.Contains(tb, err.Error(), "billing service not configured")
	})
}

func TestStripeGateway_ExecutePause_NoSubscriptionID(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		userID := uint(123)
		planID := uint(42)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:              userID,
			GatewayType:         "stripe",
			ExternalID:          TestCustomerID,
			SubscriptionID:      "",
			IsActive:            true,
			PricingPlanPeriodID: &planID,
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(mockSubscriber, nil)

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), nil, nil, mockBilling, nil, nil)

		err := gw.ExecutePause(context.Background(), userID)
		assert.Error(tb, err)
		assert.Contains(tb, err.Error(), "no stripe subscription ID found")
	})
}

func TestStripeGateway_ExecutePause_ApiError(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		mockStripeClient := &MockStripeClient{}

		userID := uint(123)
		planID := uint(42)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:              userID,
			GatewayType:         "stripe",
			ExternalID:          TestCustomerID,
			SubscriptionID:      TestSubscriptionID,
			IsActive:            true,
			PricingPlanPeriodID: &planID,
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(mockSubscriber, nil)

		mockSubService := &MockSubscriptions{}
		mockSubService.On("Update", mock.Anything, TestSubscriptionID, mock.AnythingOfType("*stripe.SubscriptionUpdateParams")).
			Return((*stripe.Subscription)(nil), fmt.Errorf("stripe api error"))
		mockStripeClient.SubscriptionsService = mockSubService

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), nil, nil, mockBilling, nil, nil)
		gw.stripeClient = mockStripeClient

		err := gw.ExecutePause(context.Background(), userID)
		assert.Error(tb, err)
		assert.Contains(tb, err.Error(), "failed to pause subscription")
	})
}

func TestStripeGateway_ExecuteResume_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		mockStripeClient := &MockStripeClient{}

		userID := uint(123)
		planID := uint(42)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:              userID,
			GatewayType:         "stripe",
			ExternalID:          TestCustomerID,
			SubscriptionID:      TestSubscriptionID,
			IsActive:            true,
			PricingPlanPeriodID: &planID,
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(mockSubscriber, nil)

		mockSubService := &MockSubscriptions{}
		mockSubService.On("Update", mock.Anything, TestSubscriptionID, mock.MatchedBy(func(params *stripe.SubscriptionUpdateParams) bool {
			return len(params.UnsetFields) == 1 && params.UnsetFields[0] == stripe.SubscriptionUpdateParamsUnsetFieldPauseCollection
		})).Return(&stripe.Subscription{ID: TestSubscriptionID}, nil)
		mockStripeClient.SubscriptionsService = mockSubService

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), nil, nil, mockBilling, nil, nil)
		gw.stripeClient = mockStripeClient

		err := gw.ExecuteResume(context.Background(), userID)
		assert.NoError(tb, err)
		mockSubService.AssertExpectations(tb)
	})
}

func TestStripeGateway_ExecuteResume_NoActiveSubscription(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		userID := uint(123)
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(nil, nil)
		mockBilling.EXPECT().GetPausedSubscription(mock.Anything, userID).Return(nil, nil)

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), nil, nil, mockBilling, nil, nil)

		err := gw.ExecuteResume(context.Background(), userID)
		assert.Error(tb, err)
		assert.Contains(tb, err.Error(), "no active or paused stripe subscription found")
	})
}

func TestStripeGateway_ExecuteResume_WrongGateway(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		userID := uint(123)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:         userID,
			GatewayType:    "atlos",
			SubscriptionID: TestSubscriptionID,
			IsActive:       true,
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(mockSubscriber, nil)
		mockBilling.EXPECT().GetPausedSubscription(mock.Anything, userID).Return(nil, nil)

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), nil, nil, mockBilling, nil, nil)

		err := gw.ExecuteResume(context.Background(), userID)
		assert.Error(tb, err)
		assert.Contains(tb, err.Error(), "no active or paused stripe subscription found")
	})
}

func TestStripeGateway_ExecuteResume_NoBillingService(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), nil, nil, nil, nil, nil)

		err := gw.ExecuteResume(context.Background(), 123)
		assert.Error(tb, err)
		assert.Contains(tb, err.Error(), "billing service not configured")
	})
}

func TestStripeGateway_ExecuteResume_NoSubscriptionID(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		userID := uint(123)
		planID := uint(42)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:              userID,
			GatewayType:         "stripe",
			ExternalID:          TestCustomerID,
			SubscriptionID:      "",
			IsActive:            true,
			PricingPlanPeriodID: &planID,
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(mockSubscriber, nil)

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), nil, nil, mockBilling, nil, nil)

		err := gw.ExecuteResume(context.Background(), userID)
		assert.Error(tb, err)
		assert.Contains(tb, err.Error(), "no stripe subscription ID found")
	})
}

func TestStripeGateway_ExecuteResume_ApiError(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		mockStripeClient := &MockStripeClient{}

		userID := uint(123)
		planID := uint(42)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:              userID,
			GatewayType:         "stripe",
			ExternalID:          TestCustomerID,
			SubscriptionID:      TestSubscriptionID,
			IsActive:            true,
			PricingPlanPeriodID: &planID,
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(mockSubscriber, nil)

		mockSubService := &MockSubscriptions{}
		mockSubService.On("Update", mock.Anything, TestSubscriptionID, mock.AnythingOfType("*stripe.SubscriptionUpdateParams")).
			Return((*stripe.Subscription)(nil), fmt.Errorf("stripe api error"))
		mockStripeClient.SubscriptionsService = mockSubService

		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), nil, nil, mockBilling, nil, nil)
		gw.stripeClient = mockStripeClient

		err := gw.ExecuteResume(context.Background(), userID)
		assert.Error(tb, err)
		assert.Contains(tb, err.Error(), "failed to resume subscription")
	})
}

func TestStripeGateway_GetManagementInfo_IncludesPauseResume(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		gw := NewWithConfig(ctx.Logger(), ctx, testConfig(), nil, nil, nil, nil, nil)

		capabilities, err := gw.GetManagementInfo(context.Background(), 123)
		assert.NoError(tb, err)
		assert.Equal(tb, pluginCore.ModePortal, capabilities.ManagementMode)

		// User operations should include pause/resume
		assert.True(tb, capabilities.Operations[pluginCore.OperationPause])
		assert.True(tb, capabilities.Operations[pluginCore.OperationResume])
		assert.True(tb, capabilities.Operations[pluginCore.OperationCancel])
		assert.True(tb, capabilities.Operations[pluginCore.OperationChangePlan])

		// Admin operations should include pause/resume
		assert.True(tb, capabilities.AdminOperations[pluginCore.OperationPause])
		assert.True(tb, capabilities.AdminOperations[pluginCore.OperationResume])
		assert.True(tb, capabilities.AdminOperations[pluginCore.OperationCancel])
		assert.True(tb, capabilities.AdminOperations[pluginCore.OperationChangePlan])
	})
}
