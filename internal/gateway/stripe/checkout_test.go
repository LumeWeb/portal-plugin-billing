package stripe

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/stripe/stripe-go/v85"
	"go.lumeweb.com/portal/core"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/internal/gateway"
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
) (*quotaCore.MockQuotaService, *coreTesting.MockUserService, *pluginCore.MockBillingService, *pluginCore.MockPricingService, *pluginCore.MockCreditService) {
	mockQuota := core.GetService[*quotaCore.MockQuotaService](ctx, quotaCore.QUOTA_SERVICE)
	mockUsers := core.GetService[*coreTesting.MockUserService](ctx, core.USER_SERVICE)
	mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
	mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)
	mockCredit := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)

	return mockQuota, mockUsers, mockBilling, mockPricing, mockCredit
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
	mockCredit *pluginCore.MockCreditService,
) *StripeGateway {
	cfg := testConfigWithSecrets(TestWebhookSecret, apiKey)
	gw := NewWithConfig(ctx.Logger(), ctx, cfg, mockQuota, mockUsers, mockBilling, mockPricing, mockCredit)
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

// assertCheckoutSuccess asserts that the checkout response meets all success criteria for hosted checkout
func assertCheckoutSuccess(t *testing.T, response *pluginCore.CheckoutUIResponse, sessionID string, sessionURL string) {
	require.NotNil(t, response)
	assert.Equal(t, sessionID, response.SessionID)
	assert.Len(t, response.Fragments, 1)
	assert.Equal(t, pluginCore.FragmentTypeLink, response.Fragments[0].Type)
	assert.Equal(t, sessionURL, response.Fragments[0].Link)
}

// assertEmbeddedCheckoutSuccess asserts that the checkout response returns an embedded HTML fragment
func assertEmbeddedCheckoutSuccess(t *testing.T, response *pluginCore.CheckoutUIResponse, sessionID string, clientSecret string) {
	require.NotNil(t, response)
	assert.Equal(t, sessionID, response.SessionID)
	assert.Len(t, response.Fragments, 1)
	assert.Equal(t, pluginCore.FragmentTypeHTML, response.Fragments[0].Type)
	// Verify the HTML contains expected elements for embedded checkout
	assert.Contains(t, response.Fragments[0].HTML, "stripe-checkout")
	assert.Contains(t, response.Fragments[0].HTML, "js.stripe.com/dahlia/stripe.js")
	if clientSecret != "" {
		assert.Contains(t, response.Fragments[0].HTML, clientSecret)
	}
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

	periodID := uint(1)
	mockPricing.EXPECT().GetPricingPlanPeriods(mock.Anything, planID).Return([]*billingModels.PricingPlanPeriod{
		{Model: gorm.Model{ID: periodID}, PricingPlanID: planID, Cadence: "monthly", PriceUSD: 9.99, QuotaPlanID: 1},
	}, nil)

	mapping := &billingModels.GatewayProductMapping{
		Model:                gorm.Model{},
		PricingPlanPeriodID:  &periodID,
		GatewayType:          "stripe",
		RemotePriceID:        priceID,
		SyncStatus:           "synced",
	}
	mockPricing.EXPECT().GetGatewayProductMapping(mock.Anything, periodID, "stripe").Return(mapping, nil)
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
		mockQuota, mockUsers, mockBilling, mockPricing, mockCredit := setupCheckoutMocks(ctx)
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
			ID:           "sess_test123",
			ClientSecret: "cs_test_secret_123",
		}

		mockStripeCheckoutSession(mockStripeClient, mockBilling, customer, checkoutSession, nil, nil)

		gw := createGateway(ctx, "sk_test", mockStripeClient, mockQuota, mockUsers, mockBilling, mockPricing, mockCredit)

		response, err := gw.GetCheckoutUI(context.Background(), userID, planID, 1)

		require.NoError(t, err)
		assertEmbeddedCheckoutSuccess(t, response, "sess_test123", "cs_test_secret_123")

		mockStripeClient.CustomersService.AssertExpectations(t)
		mockStripeClient.V1CheckoutSessionsService.AssertExpectations(t)
	})
}

// testOptionsWithDashboardAPI creates a test option that registers a mock "dashboard" API
// with subdomain "dashboard" and configures the HTTP service to return the subdomain domain.
func testOptionsWithDashboardAPI(tb coreTesting.TB) coreTesting.TestContextBuilderOption {
	return coreTesting.WithAPI(gateway.DashboardPluginID, func() (core.API, []core.ContextBuilderOption, error) {
		return coreTesting.NewMockAPI(tb, gateway.DashboardPluginID).WithSubdomain("account"), nil, nil
	})
}

func TestGetCheckoutReturnURL_WithDashboardAPI(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling, mockPricing, mockCredit := setupCheckoutMocks(ctx)
		gw := createGateway(ctx, "sk_test", nil, mockQuota, mockUsers, mockBilling, mockPricing, mockCredit)

		result := gw.getCheckoutReturnURL()
		// URL gets encoded by BuildAbsoluteURL - the placeholder is preserved but encoded
		assert.Equal(t, "https://account.test.local/billing/checkout/return%3Fsession_id=%7BCHECKOUT_SESSION_ID%7D", result)
	}, testOptionsWithDashboardAPI(t))
}

func TestGetCheckoutReturnURL_FallbackToRelative(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		cfg := testConfigWithSecrets(TestWebhookSecret, "test_key")
		gw := NewWithConfig(ctx.Logger(), ctx, cfg, nil, nil, nil, nil, nil)

		result := gw.getCheckoutReturnURL()
		assert.Equal(t, "/billing/checkout/return?session_id={CHECKOUT_SESSION_ID}", result)
	})
}

func TestStripeGateway_GetCheckoutUI_UserNotFound(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling, mockPricing, mockCredit := setupCheckoutMocks(ctx)

		userID := TestUserID
		planID := TestPlanID

		mockPricingPlan(mockPricing, planID, "Test Plan", "USD", true, "price_123")

		mockUsers.EXPECT().AccountExists(mock.Anything, userID).Return(false, nil, nil)

		gw := createGateway(ctx, "", nil, mockQuota, mockUsers, mockBilling, mockPricing, mockCredit)

		response, err := gw.GetCheckoutUI(context.Background(), userID, planID, 1)

		assertCheckoutError(t, err, response, "failed to get user")
	})
}

func TestStripeGateway_GetCheckoutUI_PlanNotFound(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling, mockPricing, mockCredit := setupCheckoutMocks(ctx)

		userID := TestUserID
		planID := uint(999)

		mockPricing.EXPECT().GetPricingPlan(mock.Anything, planID).Return(nil, assert.AnError)

		gw := createGateway(ctx, "", nil, mockQuota, mockUsers, mockBilling, mockPricing, mockCredit)

		response, err := gw.GetCheckoutUI(context.Background(), userID, planID, 1)

		assert.Error(t, err)
		assert.Nil(t, response)
	})
}

func TestStripeGateway_GetCheckoutUI_PlanNotActive(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling, mockPricing, mockCredit := setupCheckoutMocks(ctx)

		userID := TestUserID
		planID := TestPlanID

		plan := &billingModels.PricingPlan{
			Model:    gorm.Model{ID: planID},
			Name:     "Inactive Plan",
			IsActive: false,
		}
		mockPricing.EXPECT().GetPricingPlan(mock.Anything, planID).Return(plan, nil)

		gw := createGateway(ctx, "", nil, mockQuota, mockUsers, mockBilling, mockPricing, mockCredit)

		response, err := gw.GetCheckoutUI(context.Background(), userID, planID, 1)

		assertCheckoutError(t, err, response, "plan is not active")

		// Verify that GetPricingPlanPeriods was NOT called (early return due to inactive plan)
		mockPricing.AssertNotCalled(t, "GetPricingPlanPeriods")
	})
}

func TestStripeGateway_GetCheckoutUI_MissingPriceID(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling, mockPricing, mockCredit := setupCheckoutMocks(ctx)

		userID := TestUserID
		planID := TestPlanID

		plan := &billingModels.PricingPlan{
			Model:    gorm.Model{ID: planID},
			Name:     "No Price Plan",
			IsActive: true,
		}
		mockPricing.EXPECT().GetPricingPlan(mock.Anything, planID).Return(plan, nil)

		periodID := uint(1)
		mockPricing.EXPECT().GetPricingPlanPeriods(mock.Anything, planID).Return([]*billingModels.PricingPlanPeriod{
			{Model: gorm.Model{ID: periodID}, PricingPlanID: planID, Cadence: "monthly", PriceUSD: 9.99, QuotaPlanID: 1},
		}, nil)

		mapping := &billingModels.GatewayProductMapping{
			Model:                gorm.Model{},
			PricingPlanPeriodID:  &periodID,
			GatewayType:          "stripe",
			RemotePriceID:        "",
			SyncStatus:           "synced",
		}
		mockPricing.EXPECT().GetGatewayProductMapping(mock.Anything, periodID, "stripe").Return(mapping, nil)

		gw := createGateway(ctx, "", nil, mockQuota, mockUsers, mockBilling, mockPricing, mockCredit)

		response, err := gw.GetCheckoutUI(context.Background(), userID, planID, 1)

		assertCheckoutError(t, err, response, "remote price ID")
	})
}

func TestStripeGateway_GetCheckoutUI_ExistingCustomer(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling, mockPricing, mockCredit := setupCheckoutMocks(ctx)
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
			ID:           "sess_test456",
			ClientSecret: "cs_test_secret_456",
		}, existingSubscriber, nil)

		gw := createGateway(ctx, "sk_test", mockStripeClient, mockQuota, mockUsers, mockBilling, mockPricing, mockCredit)

		response, err := gw.GetCheckoutUI(context.Background(), userID, planID, 1)

		require.NoError(t, err)
		assertEmbeddedCheckoutSuccess(t, response, "sess_test456", "cs_test_secret_456")

		mockStripeClient.CustomersService.AssertExpectations(t)
		mockStripeClient.V1CheckoutSessionsService.AssertExpectations(t)
	})
}

func TestStripeGateway_ImplementsSessionStatusProvider(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling, mockPricing, mockCredit := setupCheckoutMocks(ctx)
		gw := createGateway(ctx, "sk_test", nil, mockQuota, mockUsers, mockBilling, mockPricing, mockCredit)

		assert.True(t, pluginCore.IsSessionStatusProvider(gw), "StripeGateway should implement SessionStatusProvider")

		provider, err := pluginCore.AsSessionStatusProvider(gw)
		require.NoError(t, err)
		assert.NotNil(t, provider)
	})
}

func TestStripeGateway_GetSessionStatus_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling, mockPricing, mockCredit := setupCheckoutMocks(ctx)
		mockStripeClient := NewMockStripeClient()

		sessionID := "cs_test_123"
		customerEmail := "test@example.com"

		session := &stripe.CheckoutSession{
			ID:     sessionID,
			Status: stripe.CheckoutSessionStatusComplete,
			CustomerDetails: &stripe.CheckoutSessionCustomerDetails{
				Email: customerEmail,
			},
		}

		mockStripeClient.V1CheckoutSessionsService.On("Retrieve", mock.Anything, sessionID, mock.Anything).Return(session, nil)

		gw := createGateway(ctx, "sk_test", mockStripeClient, mockQuota, mockUsers, mockBilling, mockPricing, mockCredit)

		status, err := gw.GetSessionStatus(context.Background(), sessionID)

		require.NoError(t, err)
		assert.Equal(t, sessionID, status.SessionID)
		assert.Equal(t, string(stripe.CheckoutSessionStatusComplete), status.Status)
		assert.Equal(t, customerEmail, status.CustomerEmail)

		mockStripeClient.V1CheckoutSessionsService.AssertExpectations(t)
	})
}

func TestStripeGateway_GetSessionStatus_WithCustomerObject(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling, mockPricing, mockCredit := setupCheckoutMocks(ctx)
		mockStripeClient := NewMockStripeClient()

		sessionID := "cs_test_456"
		customerEmail := "customer@example.com"

		session := &stripe.CheckoutSession{
			ID:     sessionID,
			Status: stripe.CheckoutSessionStatusOpen,
			Customer: &stripe.Customer{
				Email: customerEmail,
			},
			CustomerDetails: nil, // No CustomerDetails, should fallback to Customer
		}

		mockStripeClient.V1CheckoutSessionsService.On("Retrieve", mock.Anything, sessionID, mock.Anything).Return(session, nil)

		gw := createGateway(ctx, "sk_test", mockStripeClient, mockQuota, mockUsers, mockBilling, mockPricing, mockCredit)

		status, err := gw.GetSessionStatus(context.Background(), sessionID)

		require.NoError(t, err)
		assert.Equal(t, sessionID, status.SessionID)
		assert.Equal(t, string(stripe.CheckoutSessionStatusOpen), status.Status)
		assert.Equal(t, customerEmail, status.CustomerEmail)

		mockStripeClient.V1CheckoutSessionsService.AssertExpectations(t)
	})
}

func TestStripeGateway_GetSessionStatus_NoEmail(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling, mockPricing, mockCredit := setupCheckoutMocks(ctx)
		mockStripeClient := NewMockStripeClient()

		sessionID := "cs_test_789"

		session := &stripe.CheckoutSession{
			ID:              sessionID,
			Status:          stripe.CheckoutSessionStatusExpired,
			CustomerDetails: nil,
			Customer:        nil,
		}

		mockStripeClient.V1CheckoutSessionsService.On("Retrieve", mock.Anything, sessionID, mock.Anything).Return(session, nil)

		gw := createGateway(ctx, "sk_test", mockStripeClient, mockQuota, mockUsers, mockBilling, mockPricing, mockCredit)

		status, err := gw.GetSessionStatus(context.Background(), sessionID)

		require.NoError(t, err)
		assert.Equal(t, sessionID, status.SessionID)
		assert.Equal(t, string(stripe.CheckoutSessionStatusExpired), status.Status)
		assert.Empty(t, status.CustomerEmail)

		mockStripeClient.V1CheckoutSessionsService.AssertExpectations(t)
	})
}

func TestStripeGateway_GetSessionStatus_Error(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota, mockUsers, mockBilling, mockPricing, mockCredit := setupCheckoutMocks(ctx)
		mockStripeClient := NewMockStripeClient()

		sessionID := "cs_test_invalid"

		mockStripeClient.V1CheckoutSessionsService.On("Retrieve", mock.Anything, sessionID, mock.Anything).Return(nil, assert.AnError)

		gw := createGateway(ctx, "sk_test", mockStripeClient, mockQuota, mockUsers, mockBilling, mockPricing, mockCredit)

		status, err := gw.GetSessionStatus(context.Background(), sessionID)

		assert.Error(t, err)
		assert.Nil(t, status)
		assert.Contains(t, err.Error(), "failed to retrieve checkout session")

		mockStripeClient.V1CheckoutSessionsService.AssertExpectations(t)
	})
}
