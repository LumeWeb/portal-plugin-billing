package stripe

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/stripe/stripe-go/v83"
	"go.lumeweb.com/portal/core"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	billingModels "go.lumeweb.com/portal-plugin-billing/internal/db/models"
	quotaCore "go.lumeweb.com/portal-plugin-quota/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
	"go.lumeweb.com/portal/db/models"
	"gorm.io/gorm"
)

// Helper functions for DRY test setup

// setupCheckoutMocks configures all mock services for checkout tests
func setupCheckoutMocks(
	ctx coreTesting.TestContext,
) (*quotaCore.MockQuotaService, *coreTesting.MockUserService, *pluginCore.MockBillingService, *pluginCore.MockPricingService) {
	mockQuota := core.GetService[*quotaCore.MockQuotaService](ctx, quotaCore.QUOTA_SERVICE)
	mockUsers := core.GetService[*coreTesting.MockUserService](ctx, core.USER_SERVICE)
	mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
	mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

	return mockQuota, mockUsers, mockBilling, mockPricing
}

// createGateway creates a new StripeGateway with standard test configuration
// If stripeClient is provided, it will be used instead of the default client
func createGateway(
	ctx coreTesting.TestContext,
	apiKey string,
	stripeClient *MockStripeClient,
	mockQuota *quotaCore.MockQuotaService,
	mockUsers *coreTesting.MockUserService,
	mockBilling *pluginCore.MockBillingService,
	mockPricing *pluginCore.MockPricingService,
) *StripeGateway {
	gw := New(ctx.Logger(), TestWebhookSecret, apiKey, mockQuota, mockUsers, mockBilling, mockPricing)
	if stripeClient != nil {
		gw.stripeClient = stripeClient
	}
	return gw
}

// assertCheckoutError asserts that an error occurred with the expected message
func assertCheckoutError(t *testing.T, err error, response *pluginCore.CheckoutUIResponse, errContains string) {
	assert.Error(t, err)
	assert.Nil(t, response)
	assert.Contains(t, err.Error(), errContains)
}

// assertCheckoutSuccess asserts that the checkout response meets all success criteria
func assertCheckoutSuccess(t *testing.T, response *pluginCore.CheckoutUIResponse, sessionID string, sessionURL string) {
	require.NotNil(t, response)
	assert.Equal(t, sessionID, response.SessionID)
	assert.Len(t, response.Fragments, 1)
	assert.Equal(t, pluginCore.FragmentTypeLink, response.Fragments[0].Type)
	assert.Equal(t, sessionURL, response.Fragments[0].Link)
}

// mockUserExists sets up user retrieval mock
func mockUserExists(
	mockUsers *coreTesting.MockUserService,
	userID uint,
	email string,
) {
	mockUsers.EXPECT().AccountExists(mock.Anything, userID).Return(true, &models.User{
		Model: gorm.Model{ID: userID},
		Email: email,
	}, nil)
}

// mockPricingPlan sets up pricing plan and gateway product mapping mocks
func mockPricingPlan(
	mockPricing *pluginCore.MockPricingService,
	planID uint,
	name string,
	currency string,
	isActive bool,
	priceID string,
) {
	plan := &billingModels.PricingPlan{
		Model:    gorm.Model{ID: planID},
		Name:     name,
		Currency: currency,
		IsActive: isActive,
	}
	mockPricing.EXPECT().GetPricingPlan(mock.Anything, planID).Return(plan, nil)

	mapping := &billingModels.GatewayProductMapping{
		Model:                gorm.Model{},
		PlanID:               planID,
		GatewayType:          "stripe",
		RemoteMonthlyPriceID: priceID,
		SyncStatus:           "synced",
	}
	mockPricing.EXPECT().GetGatewayProductMapping(mock.Anything, planID, "stripe").Return(mapping, nil)
}

// mockStripeCheckoutSession sets up Stripe customer and checkout session mocks
func mockStripeCheckoutSession(
	mockStripeClient *MockStripeClient,
	mockBilling *pluginCore.MockBillingService,
	customer *stripe.Customer,
	session *stripe.CheckoutSession,
	getSubscriberResp *pluginCore.Subscriber,
	getSubscriberErr error,
) {
	// Mock GetActiveSubscriber to return the provided subscriber response
	mockBilling.EXPECT().GetActiveSubscriber(mock.Anything, TestUserID, "stripe").Return(getSubscriberResp, getSubscriberErr)

	// Only set up customer creation mock if there's no existing subscriber
	// (when getSubscriberResp is nil, the code will create a new customer)
	if customer != nil && getSubscriberResp == nil {
		mockStripeClient.SetupCustomerCreate(customer)
	}

	if session != nil {
		mockStripeClient.SetupCheckoutSessionCreate(session)
	}
}

func TestStripeGateway_GetCheckoutUI_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling, mockPricing := setupCheckoutMocks(ctx)
		mockStripeClient := NewMockStripeClient()

		userID := TestUserID
		planID := TestPlanID
		customerID := "cus_123"

		mockUserExists(mockUsers, userID, "test@example.com")
		mockPricingPlan(mockPricing, planID, "Test Plan", "USD", true, "price_123")

		customer := &stripe.Customer{
			ID:    customerID,
			Name:  "Test User",
			Email: "test@example.com",
		}
		checkoutSession := &stripe.CheckoutSession{
			ID:  "sess_test123",
			URL: "https://checkout.stripe.com/pay/sess_test123",
		}

		mockStripeCheckoutSession(mockStripeClient, mockBilling, customer, checkoutSession, nil, nil)

		gw := createGateway(ctx, "sk_test", mockStripeClient, mockQuota, mockUsers, mockBilling, mockPricing)

		response, err := gw.GetCheckoutUI(context.Background(), userID, planID)

		require.NoError(t, err)
		assertCheckoutSuccess(t, response, "sess_test123", "https://checkout.stripe.com/pay/sess_test123")

		mockStripeClient.CustomersService.AssertExpectations(t)
		mockStripeClient.V1CheckoutSessionsService.AssertExpectations(t)
	})
}

func TestStripeGateway_GetCheckoutUI_UserNotFound(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling, mockPricing := setupCheckoutMocks(ctx)

		userID := TestUserID
		planID := TestPlanID

		mockPricingPlan(mockPricing, planID, "Test Plan", "USD", true, "price_123")

		mockUsers.EXPECT().AccountExists(mock.Anything, userID).Return(false, nil, nil)

		gw := createGateway(ctx, "", nil, mockQuota, mockUsers, mockBilling, mockPricing)

		response, err := gw.GetCheckoutUI(context.Background(), userID, planID)

		assertCheckoutError(t, err, response, "failed to get user")
	})
}

func TestStripeGateway_GetCheckoutUI_PlanNotFound(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling, mockPricing := setupCheckoutMocks(ctx)

		userID := TestUserID
		planID := uint(999)

		mockPricing.EXPECT().GetPricingPlan(mock.Anything, planID).Return(nil, assert.AnError)

		gw := createGateway(ctx, "", nil, mockQuota, mockUsers, mockBilling, mockPricing)

		response, err := gw.GetCheckoutUI(context.Background(), userID, planID)

		assert.Error(t, err)
		assert.Nil(t, response)
	})
}

func TestStripeGateway_GetCheckoutUI_PlanNotActive(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling, mockPricing := setupCheckoutMocks(ctx)

		userID := TestUserID
		planID := TestPlanID

		plan := &billingModels.PricingPlan{
			Model:    gorm.Model{ID: planID},
			Name:     "Inactive Plan",
			IsActive: false,
		}
		mockPricing.EXPECT().GetPricingPlan(mock.Anything, planID).Return(plan, nil)

		gw := createGateway(ctx, "", nil, mockQuota, mockUsers, mockBilling, mockPricing)

		response, err := gw.GetCheckoutUI(context.Background(), userID, planID)

		assertCheckoutError(t, err, response, "plan is not active")
	})
}

func TestStripeGateway_GetCheckoutUI_MissingPriceID(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling, mockPricing := setupCheckoutMocks(ctx)

		userID := TestUserID
		planID := TestPlanID

		plan := &billingModels.PricingPlan{
			Model:    gorm.Model{ID: planID},
			Name:     "No Price Plan",
			IsActive: true,
		}
		mockPricing.EXPECT().GetPricingPlan(mock.Anything, planID).Return(plan, nil)

		mapping := &billingModels.GatewayProductMapping{
			Model:                gorm.Model{},
			PlanID:               planID,
			GatewayType:          "stripe",
			RemoteMonthlyPriceID: "",
			SyncStatus:           "synced",
		}
		mockPricing.EXPECT().GetGatewayProductMapping(mock.Anything, planID, "stripe").Return(mapping, nil)

		gw := createGateway(ctx, "", nil, mockQuota, mockUsers, mockBilling, mockPricing)

		response, err := gw.GetCheckoutUI(context.Background(), userID, planID)

		assertCheckoutError(t, err, response, "remote price ID")
	})
}

func TestStripeGateway_GetCheckoutUI_ExistingCustomer(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling, mockPricing := setupCheckoutMocks(ctx)
		mockStripeClient := NewMockStripeClient()

		userID := TestUserID
		planID := TestPlanID
		customerID := "cus_existing"

		mockUserExists(mockUsers, userID, "existing@example.com")
		mockPricingPlan(mockPricing, planID, "Test Plan", "USD", true, "price_123")

		existingSubscriber := &pluginCore.Subscriber{
			UserID:         userID,
			GatewayType:    "stripe",
			ExternalID:     customerID,
			SubscriptionID: "",
			IsActive:       false,
		}

		customer := &stripe.Customer{
			ID:    customerID,
			Name:  "Existing User",
			Email: "existing@example.com",
		}

		mockStripeCheckoutSession(mockStripeClient, mockBilling, customer, &stripe.CheckoutSession{
			ID:  "sess_test456",
			URL: "https://checkout.stripe.com/pay/sess_test456",
		}, existingSubscriber, nil)

		gw := createGateway(ctx, "sk_test", mockStripeClient, mockQuota, mockUsers, mockBilling, mockPricing)

		response, err := gw.GetCheckoutUI(context.Background(), userID, planID)

		require.NoError(t, err)
		assertCheckoutSuccess(t, response, "sess_test456", "https://checkout.stripe.com/pay/sess_test456")

		mockStripeClient.CustomersService.AssertExpectations(t)
		mockStripeClient.V1CheckoutSessionsService.AssertExpectations(t)
	})
}
