package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/internal"
	"go.lumeweb.com/portal-plugin-billing/internal/api/dto"
	pluginConfig "go.lumeweb.com/portal-plugin-billing/internal/config"
	"go.lumeweb.com/portal-plugin-billing/internal/gateway"
	"go.lumeweb.com/portal-plugin-billing/internal/service/pricing"
	"go.lumeweb.com/portal/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
	"gorm.io/gorm"

	"go.lumeweb.com/queryutil"

	internalModels "go.lumeweb.com/portal-plugin-billing/internal/db/models"
)

// combinedGatewayMock combines PaymentGateway, SubscriptionManager, and SubscriptionExecutor mocks for testing
type combinedGatewayMock struct {
	*pluginCore.MockPaymentGateway
	*pluginCore.MockSubscriptionManager
	*pluginCore.MockSubscriptionExecutor
}

// newCombinedGatewayMock creates a new combinedGatewayMock with all three mock types initialized
func newCombinedGatewayMock(t interface {
	mock.TestingT
	Cleanup(func())
}) *combinedGatewayMock {
	return &combinedGatewayMock{
		MockPaymentGateway:       pluginCore.NewMockPaymentGateway(t),
		MockSubscriptionManager:  pluginCore.NewMockSubscriptionManager(t),
		MockSubscriptionExecutor: pluginCore.NewMockSubscriptionExecutor(t),
	}
}

// testSetup holds common test dependencies
type testSetup struct {
	billingSvc *pluginCore.MockBillingService
	userSvc    *coreTesting.MockUserService
	router     http.Handler
}

// setupTest creates common test dependencies
func setupTest(ctx coreTesting.TestContext) *testSetup {
	return &testSetup{
		billingSvc: core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE),
		userSvc:    core.GetService[*coreTesting.MockUserService](ctx, core.USER_SERVICE),
		router:     ctx.Router(),
	}
}

// createAuthenticatedRequest creates an authenticated HTTP request with a valid JWT token
func (ts *testSetup) createAuthenticatedRequest(ctx coreTesting.TestContext, method, url string, body []byte, userID string) (*http.Request, error) {
	// Create a test user ID (default to 1 if not specified)
	userIDUint := uint(1)
	if userID != "" {
		if id, err := strconv.ParseUint(userID, 10, 32); err == nil {
			userIDUint = uint(id)
		}
	}

	// Generate a JWT token directly without setting up LoginPassword expectations
	// The CreateTestLoginToken function creates a valid JWT token for testing
	userIDStr := strconv.Itoa(int(userIDUint))
	token := coreTesting.CreateTestLoginToken(ctx.T(), ctx, userIDStr)

	req := ctx.NewAPIRequest(method, url, body)
	req.Header.Set("Authorization", "Bearer "+token)

	return req, nil
}

// createMockSubscriber creates a mock subscriber with the given parameters
func createMockSubscriber(userID uint, gatewayType, externalID string, isActive bool, planID *uint) *pluginCore.Subscriber {
	return &pluginCore.Subscriber{
		UserID:        userID,
		GatewayType:   gatewayType,
		ExternalID:    externalID,
		SubscriptionID: "",
		IsActive:      isActive,
		PlanID:        planID,
	}
}

// assertSubscriptionStatus verifies subscription status response
func assertSubscriptionStatus(t coreTesting.TB, response dto.SubscriptionStatusResponse, expectedSubscribed bool, expectedGateway string, expectedPlanID *uint) {
	assert.Equal(t, expectedSubscribed, response.IsSubscribed)
	assert.Equal(t, expectedGateway, response.GatewayType)
	assert.Equal(t, expectedPlanID, response.PlanID)
}

func TestMain(m *testing.M) {
	// Base test setup without global API extensions.
	// Individual tests should call getUserAPITestOptions() or getAdminAPITestOptions()
	// as the third argument to RunTestCase.
	coreTesting.WithOptions(m,
		// Base configuration without API extensions
		coreTesting.WithServiceConfig(internal.PLUGIN_NAME, pluginCore.BILLING_SERVICE, &pluginConfig.ServiceConfig{}),
	)
}

func TestHandleWebhook_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock expectations
		ts.billingSvc.EXPECT().GetSignatureHeader(mock.Anything, "stripe").Return("Stripe-Signature", nil).Once()

		// Create a test webhook payload for a checkout.session.completed event
		webhookPayload := `{
			"id": "evt_test_webhook",
			"type": "checkout.session.completed",
			"data": {
				"object": {
					"id": "cs_test_session",
					"object": "checkout.session",
					"mode": "subscription",
					"client_reference_id": "1",
					"subscription": {
						"id": "sub_test_subscription"
					}
				}
			}
		}`

		ts.billingSvc.EXPECT().ProcessWebhook(mock.Anything, "stripe", "test_sig", []byte(webhookPayload)).
			Return(nil).Once()

		// Create request
		req := ctx.NewAPIRequest("POST", "/api/account/billing/webhooks/stripe", []byte(webhookPayload))
		req.Header.Set("Stripe-Signature", "test_sig")
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusNoContent, w.Code)

	}, getUserAPITestOptions())
}

func TestHandleWebhook_InvalidGateway(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock expectations
		ts.billingSvc.EXPECT().GetSignatureHeader(mock.Anything, "invalid").Return("", pluginCore.ErrGatewayNotFound).Once()

		// Create request
		req := ctx.NewAPIRequest("POST", "/api/account/billing/webhooks/invalid", []byte(`{"test":"payload"}`))
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusNotFound, w.Code)

	}, getUserAPITestOptions())
}

func TestHandleSubscriptionStatus_ActiveSubscription(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock the billing service to return an active subscription
		planID := uint(42)
		mockSubscriber := createMockSubscriber(1, "stripe", "cus_123", true, &planID)
		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(mockSubscriber, nil)

		// Create authenticated request
		req, err := ts.createAuthenticatedRequest(ctx, "GET", "/api/account/billing/subscription", nil, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		// Parse response using DTO
		var response dto.SubscriptionStatusResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(tb, err)

		assertSubscriptionStatus(tb, response, true, "stripe", &planID)

	}, getUserAPITestOptions())
}

func TestHandleSubscriptionStatus_NoActiveSubscription(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock the billing service to return no active subscription
		// This covers both scenarios: no subscription exists and inactive subscriptions
		// (GetActiveSubscription only returns active subscriptions)
		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return((*pluginCore.Subscriber)(nil), nil)

		// Create authenticated request
		req, err := ts.createAuthenticatedRequest(ctx, "GET", "/api/account/billing/subscription", nil, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		// Parse response using DTO
		var response dto.SubscriptionStatusResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(tb, err)

		// Both no subscription and inactive subscription scenarios should return the same response
		assertSubscriptionStatus(tb, response, false, "", nil)
	}, getUserAPITestOptions())
}

func TestHandleSubscriptionStatus_MultipleGateways(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock the billing service to return an active subscription (could be any gateway)
		planID := uint(99)
		mockSubscriber := createMockSubscriber(1, "paypal", "cus_456", true, &planID)
		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(mockSubscriber, nil)

		// Create authenticated request
		req, err := ts.createAuthenticatedRequest(ctx, "GET", "/api/account/billing/subscription", nil, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		// Parse response using DTO
		var response dto.SubscriptionStatusResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(tb, err)

		// Should return the mocked subscription
		assertSubscriptionStatus(tb, response, true, "paypal", &planID)
	}, getUserAPITestOptions())
}

func TestHandleSubscriptionStatus_Unauthorized(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Create unauthenticated request (no auth header)
		req := ctx.NewAPIRequest("GET", "/api/account/billing/subscription", nil)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return unauthorized
		assert.Equal(tb, http.StatusUnauthorized, w.Code)
	}, getUserAPITestOptions())
}

func TestHandleGetGateways_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Create a new registry for testing
		mockRegistry := gateway.NewRegistry()
		mockGateway1 := pluginCore.NewMockPaymentGateway(t)
		mockGateway1.EXPECT().ID(mock.Anything).Return("stripe").Once()
		mockGateway1.EXPECT().GetName(mock.Anything).Return("Stripe").Once()
		mockGateway1.EXPECT().GetDescription(mock.Anything).Return("Industry-leading payment processor").Once()
		mockGateway2 := pluginCore.NewMockPaymentGateway(t)
		mockGateway2.EXPECT().ID(mock.Anything).Return("paypal").Once()
		mockGateway2.EXPECT().GetName(mock.Anything).Return("PayPal").Once()
		mockGateway2.EXPECT().GetDescription(mock.Anything).Return("Fast and secure payments").Once()

		// Register gateways manually for test
		ctxForReg := context.Background()
		err := mockRegistry.Register(ctxForReg, mockGateway1)
		assert.NoError(tb, err)
		err = mockRegistry.Register(ctxForReg, mockGateway2)
		assert.NoError(tb, err)

		ts.billingSvc.EXPECT().GetRegistry(mock.Anything).Return(mockRegistry).Once()

		// Create request
		req := ctx.NewAPIRequest("GET", "/api/billing/gateways", nil)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		// Parse response
		var response dto.GatewayListResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(tb, err)
		assert.Len(tb, response, 2)

		// Verify gateways by ID (order not guaranteed)
		gatewayMap := make(map[string]dto.GatewayPublicInfo)
		for _, gateway := range response {
			gatewayMap[gateway.ID] = gateway
		}

		// Verify stripe gateway
		stripeGateway, exists := gatewayMap["stripe"]
		assert.True(tb, exists)
		assert.Equal(tb, "Stripe", stripeGateway.Name)
		assert.Equal(tb, "Industry-leading payment processor", stripeGateway.Description)
		assert.Equal(tb, "/api/billing/gateways/stripe/logo", stripeGateway.LogoURL)
		assert.True(tb, stripeGateway.IsActive)

		// Verify paypal gateway
		paypalGateway, exists := gatewayMap["paypal"]
		assert.True(tb, exists)
		assert.Equal(tb, "PayPal", paypalGateway.Name)
		assert.Equal(tb, "Fast and secure payments", paypalGateway.Description)
		assert.Equal(tb, "/api/billing/gateways/paypal/logo", paypalGateway.LogoURL)
		assert.True(tb, paypalGateway.IsActive)
	}, getUserAPITestOptions())
}

func TestHandleGetGateways_EmptyRegistry(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Create empty registry for testing
		mockRegistry := gateway.NewRegistry()

		ts.billingSvc.EXPECT().GetRegistry(mock.Anything).Return(mockRegistry).Once()

		// Create request
		req := ctx.NewAPIRequest("GET", "/api/billing/gateways", nil)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		// Parse response
		var response dto.GatewayListResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(tb, err)
		assert.Len(tb, response, 0)
	}, getUserAPITestOptions())
}

func TestHandleGetGateways_RegistryNil(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock nil registry
		ts.billingSvc.EXPECT().GetRegistry(mock.Anything).Return(pluginCore.GatewayRegistry(nil)).Once()

		// Create request
		req := ctx.NewAPIRequest("GET", "/api/billing/gateways", nil)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return internal server error
		assert.Equal(tb, http.StatusInternalServerError, w.Code)

		// Parse error response
		var errorResponse map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &errorResponse)
		assert.NoError(tb, err)
		assert.Contains(tb, errorResponse["error"], "gateway registry not initialized")
	}, getUserAPITestOptions())
}

func TestHandleGetGatewayLogo_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Create mock gateway
		mockGateway := pricing.NewMockMockablePaymentGateway(tb)

		// Mock getting gateway from billing service
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "stripe").
			Return(mockGateway, nil).
			Once()

		// Mock getting logo from gateway
		logoData := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<svg width="100" height="100" xmlns="http://www.w3.org/2000/svg">
  <rect width="100" height="100" fill="red"/>
</svg>`)
		mockGateway.EXPECT().GetLogo(mock.Anything).
			Return(logoData, nil).
			Once()

		// Create request for stripe gateway
		req := ctx.NewAPIRequest("GET", "/api/billing/gateways/stripe/logo", nil)

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return OK with logo data
		assert.Equal(tb, http.StatusOK, w.Code)
		assert.True(tb, len(w.Body.Bytes()) > 0)
		assert.Contains(tb, w.Header().Get("Content-Type"), "svg")
	}, getUserAPITestOptions())
}

func TestHandleGetGatewayLogo_NotFound(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock gateway not found
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "nonexistent").
			Return(nil, pluginCore.ErrGatewayNotFound).
			Once()

		// Create request for non-existent gateway
		req := ctx.NewAPIRequest("GET", "/api/billing/gateways/nonexistent/logo", nil)

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return not found
		assert.Equal(tb, http.StatusNotFound, w.Code)
	}, getUserAPITestOptions())
}

func TestHandleGetGatewayLogo_GetLogoError(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Create mock gateway
		mockGateway := pricing.NewMockMockablePaymentGateway(tb)

		// Mock getting gateway from billing service
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "stripe").
			Return(mockGateway, nil).
			Once()

		// Mock GetLogo returning an error
		mockGateway.EXPECT().GetLogo(mock.Anything).
			Return(nil, fmt.Errorf("logo file not found")).
			Once()

		// Create request for stripe gateway
		req := ctx.NewAPIRequest("GET", "/api/billing/gateways/stripe/logo", nil)

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return not found
		assert.Equal(tb, http.StatusNotFound, w.Code)
	}, getUserAPITestOptions())
}

func TestHandleGetGatewayLogo_PNGContentType(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Create mock gateway
		mockGateway := pricing.NewMockMockablePaymentGateway(tb)

		// Mock getting gateway from billing service
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "stripe").
			Return(mockGateway, nil).
			Once()

		// Mock getting a PNG logo from gateway
		// PNG magic bytes: 89 50 4E 47 0D 0A 1A 0A
		logoData := []byte{
			0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
			// Minimal PNG
			0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
			// ... more PNG data would follow
		}
		mockGateway.EXPECT().GetLogo(mock.Anything).
			Return(logoData, nil).
			Once()

		// Create request for stripe gateway
		req := ctx.NewAPIRequest("GET", "/api/billing/gateways/stripe/logo", nil)

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return OK and PNG content type
		assert.Equal(tb, http.StatusOK, w.Code)
		assert.True(tb, len(w.Body.Bytes()) > 0)
		assert.Contains(tb, w.Header().Get("Content-Type"), "png")
	}, getUserAPITestOptions())
}

func TestHandleGetGatewayLogo_ContentTypeFallback_Unknown(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Create mock gateway
		mockGateway := pricing.NewMockMockablePaymentGateway(tb)

		// Mock getting gateway from billing service
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "stripe").
			Return(mockGateway, nil).
			Once()

		// Mock getting content that mimetype detects as text/plain
		logoData := []byte("not-a-real-image-type")
		mockGateway.EXPECT().GetLogo(mock.Anything).
			Return(logoData, nil).
			Once()

		// Create request for stripe gateway
		req := ctx.NewAPIRequest("GET", "/api/billing/gateways/stripe/logo", nil)

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return OK, mimetype will detect as text/plain
		assert.Equal(tb, http.StatusOK, w.Code)
		assert.Contains(tb, w.Header().Get("Content-Type"), "text/plain")
	}, getUserAPITestOptions())
}

func TestHandleGetGatewayLogo_EmptyData(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Create mock gateway
		mockGateway := pricing.NewMockMockablePaymentGateway(tb)

		// Mock getting gateway from billing service
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "stripe").
			Return(mockGateway, nil).
			Once()

		// Mock getting empty logo data
		logoData := []byte{}
		mockGateway.EXPECT().GetLogo(mock.Anything).
			Return(logoData, nil).
			Once()

		// Create request for stripe gateway
		req := ctx.NewAPIRequest("GET", "/api/billing/gateways/stripe/logo", nil)

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return OK even with empty data
		assert.Equal(tb, http.StatusOK, w.Code)
	}, getUserAPITestOptions())
}

func TestHandleListPricingPlans_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Get pricing service
		pricingSvc := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		// Mock effective price line
		priceLine := &internalModels.PriceLine{
			Model: gorm.Model{ID: 100},
			Name:  "Default Price Line",
		}
		pricingSvc.EXPECT().GetEffectivePriceLineForUser(mock.Anything, uint(1)).Return(priceLine, nil).Once()

		// Mock pricing plans
		monthlyPrice := 9.99
		yearlyPrice := 99.99
		plans := []*internalModels.PricingPlan{
			{
				Model:           gorm.Model{ID: 1},
				Name:            "Basic Plan",
				Description:     "Entry level plan",
				MonthlyPriceUSD: &monthlyPrice,
				YearlyPriceUSD:  &yearlyPrice,
				Currency:        "USD",
				IsActive:        true,
				IsPublic:        true,
			},
			{
				Model:           gorm.Model{ID: 2},
				Name:            "Pro Plan",
				Description:     "Professional plan",
				MonthlyPriceUSD: nil,
				YearlyPriceUSD:  nil,
				Currency:        "USD",
				IsActive:        true,
				IsPublic:        true,
			},
		}
		pricingSvc.EXPECT().GetPlansForPriceLine(mock.Anything, uint(100)).Return(plans, nil).Once()

		// Create authenticated request
		req, requestErr := ts.createAuthenticatedRequest(ctx, "GET", "/api/billing/plans", nil, "1")
		assert.NoError(tb, requestErr, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		// Parse response
		var response queryutil.Response[[]dto.PricingPlanResponse]
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(tb, err)
		assert.Len(tb, response.Data, 2)

		// Verify plan 1
		assert.Equal(tb, uint(1), response.Data[0].ID)
		assert.Equal(tb, "Basic Plan", response.Data[0].Name)
		assert.Equal(tb, "Entry level plan", response.Data[0].Description)
		assert.NotNil(tb, response.Data[0].MonthlyPrice)
		assert.Equal(tb, 9.99, *response.Data[0].MonthlyPrice)
		assert.NotNil(tb, response.Data[0].YearlyPrice)
		assert.Equal(tb, 99.99, *response.Data[0].YearlyPrice)
		assert.Equal(tb, "USD", response.Data[0].Currency)
		assert.True(tb, response.Data[0].IsActive)
		assert.True(tb, response.Data[0].IsPublic)
	}, getUserAPITestOptions())
}

func TestHandleListPricingPlans_Unauthenticated(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Get pricing service
		pricingSvc := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		// Mock default price line
		priceLine := &internalModels.PriceLine{
			Model: gorm.Model{ID: 999},
			Name:  "Default Price Line",
		}
		pricingSvc.EXPECT().GetDefaultPriceLine(mock.Anything).Return(priceLine, nil).Once()

		// Mock pricing plans
		monthlyPrice := 9.99
		plans := []*internalModels.PricingPlan{
			{
				Model:           gorm.Model{ID: 1},
				Name:            "Basic Plan",
				Description:     "Entry level plan",
				MonthlyPriceUSD: &monthlyPrice,
				Currency:        "USD",
				IsActive:        true,
				IsPublic:        true,
			},
		}
		pricingSvc.EXPECT().GetPlansForPriceLine(mock.Anything, uint(999)).Return(plans, nil).Once()

		// Create unauthenticated request (no auth header)
		req := ctx.NewAPIRequest("GET", "/api/billing/plans", nil)

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return OK with default plans
		assert.Equal(tb, http.StatusOK, w.Code)

		// Parse response
		var response queryutil.Response[[]dto.PricingPlanResponse]
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(tb, err)
		assert.Len(tb, response.Data, 1)

		// Verify plan details
		assert.Equal(tb, uint(1), response.Data[0].ID)
		assert.Equal(tb, "Basic Plan", response.Data[0].Name)
		assert.Equal(tb, 9.99, *response.Data[0].MonthlyPrice)
	}, getUserAPITestOptions())
}

func TestHandleGetPricingPlanDetail_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Get pricing service
		pricingSvc := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		// Mock pricing plan
		monthlyPrice := 19.99
		plan := &internalModels.PricingPlan{
			Model:           gorm.Model{ID: 1},
			Name:            "Premium Plan",
			Description:     "Premium features for professionals",
			MonthlyPriceUSD: &monthlyPrice,
			Currency:        "USD",
			IsActive:        true,
			IsPublic:        true,
		}
		pricingSvc.EXPECT().GetPricingPlan(mock.Anything, uint(1)).Return(plan, nil).Once()

		// Create authenticated request
		req, requestErr := ts.createAuthenticatedRequest(ctx, "GET", "/api/billing/plans/1", nil, "1")
		assert.NoError(tb, requestErr, "Failed to create authenticated request")
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		// Parse response
		var response dto.PricingPlanResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(tb, err)

		// Verify plan details
		assert.Equal(tb, uint(1), response.ID)
		assert.Equal(tb, "Premium Plan", response.Name)
		assert.Equal(tb, "Premium features for professionals", response.Description)
		assert.NotNil(tb, response.MonthlyPrice)
		assert.Equal(tb, 19.99, *response.MonthlyPrice)
		assert.Equal(tb, "USD", response.Currency)
		assert.True(tb, response.IsActive)
		assert.True(tb, response.IsPublic)
	}, getUserAPITestOptions())
}

func TestHandleGetPricingPlanDetail_NotFound(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Get pricing service
		pricingSvc := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		// Mock plan not found
		pricingSvc.EXPECT().GetPricingPlan(mock.Anything, uint(999)).Return((*internalModels.PricingPlan)(nil), gorm.ErrRecordNotFound).Once()

		// Create request
		req := ctx.NewAPIRequest("GET", "/api/billing/plans/999", nil)

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return not found
		assert.Equal(tb, http.StatusNotFound, w.Code)
	}, getUserAPITestOptions())
}

func TestHandleGetPricingPlanDetail_InvalidID(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Create request with invalid ID
		req := ctx.NewAPIRequest("GET", "/api/billing/plans/invalid", nil)

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return bad request
		assert.Equal(tb, http.StatusBadRequest, w.Code)
	}, getUserAPITestOptions())
}

// Checkout UI Tests

func TestHandleGetCheckoutUI_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		planID := uint(42)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock GetCheckoutUI response
		checkoutResponse := &pluginCore.CheckoutUIResponse{
			SessionID: "sess_123",
			ExpiresAt: createTestExpirationTime(),
			Metadata: map[string]any{
				"plan_id": planID,
			},
			Fragments: []pluginCore.CheckoutUIFragment{
				{
					Type: pluginCore.FragmentTypeLink,
					Link: "https://checkout.stripe.com/pay/sess_123",
				},
			},
		}

		// Mock GetCheckoutUI on billing service
		ts.billingSvc.EXPECT().GetCheckoutUI(mock.Anything, uint(1), planID, "stripe").
			Return(checkoutResponse, nil).Once()

		// Create authenticated request
		req := ctx.NewAPIRequest("GET", "/api/account/billing/checkout/ui/"+strconv.Itoa(int(planID)), nil)
		token := coreTesting.CreateTestLoginToken(ctx.T(), ctx, "1")
		req.Header.Set("Authorization", "Bearer "+token)

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		var response pluginCore.CheckoutUIResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(tb, err)

		assert.Equal(tb, checkoutResponse.SessionID, response.SessionID)
		// JSON numbers are unmarshaled as float64
		assert.Equal(tb, float64(planID), response.Metadata["plan_id"])
		assert.Len(tb, response.Fragments, 1)
		assert.Equal(tb, pluginCore.FragmentTypeLink, response.Fragments[0].Type)
	}, getUserAPITestOptions())
}

func TestHandleGetCheckoutUI_WithCustomGateway(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		planID := uint(99)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock checkout UI with custom gateway
		checkoutResponse := &pluginCore.CheckoutUIResponse{
			SessionID: "sess_custom",
			Fragments: []pluginCore.CheckoutUIFragment{
				{
					Type:   pluginCore.FragmentTypeScript,
					Script: "https://checkout.example.com/sdk.js",
				},
			},
		}

		// Mock GetCheckoutUI on billing service
		ts.billingSvc.EXPECT().GetCheckoutUI(mock.Anything, uint(1), planID, "paypal").
			Return(checkoutResponse, nil).Once()

		// Create request with gateway query parameter (note: parameter name is "gateway", not "gateway_type")
		req := ctx.NewAPIRequest("GET", "/api/account/billing/checkout/ui/99?gateway=paypal", nil)
		token := coreTesting.CreateTestLoginToken(ctx.T(), ctx, "1")
		req.Header.Set("Authorization", "Bearer "+token)

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		var response pluginCore.CheckoutUIResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(tb, err)

		assert.Equal(tb, "sess_custom", response.SessionID)
		assert.Equal(tb, pluginCore.FragmentTypeScript, response.Fragments[0].Type)
	}, getUserAPITestOptions())
}

func TestHandleGetCheckoutUI_Unauthorized(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		planID := uint(42)

		// Create unauthenticated request
		req := ctx.NewAPIRequest("GET", "/api/account/billing/checkout/ui/"+strconv.Itoa(int(planID)), nil)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusUnauthorized, w.Code)
	}, getUserAPITestOptions())
}

func TestHandleGetCheckoutUI_InvalidPlanID(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		token := coreTesting.CreateTestLoginToken(ctx.T(), ctx, "1")
		req := ctx.NewAPIRequest("GET", "/api/account/billing/checkout/ui/invalid", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusBadRequest, w.Code)

		var errResponse map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &errResponse)
		require.NoError(tb, err)
		assert.Contains(tb, errResponse["error"], "invalid plan ID")
	}, getUserAPITestOptions())
}

func TestHandleGetCheckoutUI_GetCheckoutUIError(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		planID := uint(42)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock GetCheckoutUI to return an error
		ts.billingSvc.EXPECT().GetCheckoutUI(mock.Anything, uint(1), planID, "stripe").
			Return(nil, assert.AnError).Once()

		// Create authenticated request
		req := ctx.NewAPIRequest("GET", "/api/account/billing/checkout/ui/"+strconv.Itoa(int(planID)), nil)
		token := coreTesting.CreateTestLoginToken(ctx.T(), ctx, "1")
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusInternalServerError, w.Code)

		var errResponse map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &errResponse)
		require.NoError(tb, err)
	}, getUserAPITestOptions())
}

func TestHandleGetCheckoutUI_GatewayNotFound(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		planID := uint(42)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock GetCheckoutUI to return gateway not found error
		ts.billingSvc.EXPECT().GetCheckoutUI(mock.Anything, uint(1), planID, "nonexistent").
			Return(nil, pluginCore.ErrGatewayNotFound).Once()

		// Create authenticated request - use "gateway" query param, not "gateway_type"
		req := ctx.NewAPIRequest("GET", "/api/account/billing/checkout/ui/"+strconv.Itoa(int(planID))+"?gateway=nonexistent", nil)
		token := coreTesting.CreateTestLoginToken(ctx.T(), ctx, "1")
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - GetGateway errors return 500 Internal Server Error
		assert.Equal(tb, http.StatusInternalServerError, w.Code)

		var errResponse map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &errResponse)
		require.NoError(tb, err)
		// Error message should indicate gateway failure
		assert.Contains(tb, errResponse["error"], "gateway")
	}, getUserAPITestOptions())
}

func TestHandleGetCheckoutUI_UserAlreadySubscribed(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		planID := uint(42)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock GetCheckoutUI to return error about active subscription
		ts.billingSvc.EXPECT().GetCheckoutUI(mock.Anything, uint(1), planID, "stripe").
			Return(nil, fmt.Errorf("user already has an active subscription")).Once()

		// Create authenticated request
		req := ctx.NewAPIRequest("GET", "/api/account/billing/checkout/ui/"+strconv.Itoa(int(planID)), nil)
		token := coreTesting.CreateTestLoginToken(ctx.T(), ctx, "1")
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return 409 Conflict due to active subscription
		assert.Equal(tb, http.StatusConflict, w.Code)
	}, getUserAPITestOptions())
}

func TestHandleGetCheckoutUI_RequestBodyParsing(t *testing.T) {
	// This test name is misleading - endpoint uses GET with path params, not POST with body
	// Renaming to match actual implementation
	t.Run("ValidPlanIDInPath", func(t *testing.T) {
		coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
			ts := setupTest(ctx)

			planID := uint(42)

			// Mock user account validation
			ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

			checkoutResponse := &pluginCore.CheckoutUIResponse{
				Fragments: []pluginCore.CheckoutUIFragment{
					{Type: pluginCore.FragmentTypeLink},
				},
			}

			// Mock GetCheckoutUI
			ts.billingSvc.EXPECT().GetCheckoutUI(mock.Anything, uint(1), planID, "stripe").
				Return(checkoutResponse, nil).Once()

			// Create authenticated request
			req := ctx.NewAPIRequest("GET", "/api/account/billing/checkout/ui/"+strconv.Itoa(int(planID)), nil)
			token := coreTesting.CreateTestLoginToken(ctx.T(), ctx, "1")
			req.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()

			// Execute
			ts.router.ServeHTTP(w, req)

			// Verify
			assert.Equal(tb, http.StatusOK, w.Code)
		}, getUserAPITestOptions())
	})
}

// Helper function for checkout tests

func createTestExpirationTime() time.Time {
	return time.Now().Add(30 * time.Minute)
}

// Management operation tests

func TestHandleCancelOperation_Success_APIBased(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock active subscription
		planID := uint(42)
		mockSubscriber := createMockSubscriber(1, "atlos", "sub_123", true, &planID)
		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(mockSubscriber, nil).Once()

		// Mock gateway
		mockGateway := newCombinedGatewayMock(t)
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "atlos").Return(mockGateway, nil).Once()

		// Mock management capabilities - Atlas supports cancellation
		capabilities := &pluginCore.ManagementCapabilities{
			ManagementMode: pluginCore.ModeAPI,
			Operations: map[pluginCore.ManagementOperation]bool{
				pluginCore.OperationCancel:     true,
				pluginCore.OperationChangePlan: false,
			},
		}
		mockGateway.MockSubscriptionManager.EXPECT().GetManagementInfo(mock.Anything, uint(1)).Return(capabilities, nil).Once()

		// Mock management result - API required
		endpoint := &pluginCore.APIEndpointInfo{
			Method: "POST",
			Path:   "/api/account/billing/cancel",
		}
		managementResult := &pluginCore.ManagementResult{
			Action:      pluginCore.ActionAPIRequired,
			APIEndpoint: endpoint,
		}
		mockGateway.MockSubscriptionManager.EXPECT().GetManagementURL(mock.Anything, uint(1), pluginCore.OperationCancel).Return(managementResult, nil).Once()

		// Mock ExecuteCancel - backend executes the cancellation
		mockGateway.MockSubscriptionExecutor.EXPECT().ExecuteCancel(mock.Anything, uint(1)).Return(nil).Once()

		// Create authenticated request
		req, err := ts.createAuthenticatedRequest(ctx, "POST", "/api/account/billing/cancel", nil, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		var response dto.ManagementResultResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(tb, err)

		// After successful execution, action is "show_ui" (success)
		assert.Equal(tb, pluginCore.ActionShowUI, response.Action)

	}, getUserAPITestOptions())
}

func TestHandleCancelOperation_Success_PortalRedirect(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock active subscription
		planID := uint(42)
		mockSubscriber := createMockSubscriber(1, "stripe", "sub_123", true, &planID)
		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(mockSubscriber, nil).Once()

		// Mock gateway
		mockGateway := newCombinedGatewayMock(t)
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "stripe").Return(mockGateway, nil).Once()

		// Mock management capabilities - Stripe supports cancellation
		capabilities := &pluginCore.ManagementCapabilities{
			ManagementMode: pluginCore.ModePortal,
			Operations: map[pluginCore.ManagementOperation]bool{
				pluginCore.OperationCancel:     true,
				pluginCore.OperationChangePlan: true,
			},
		}
		mockGateway.MockSubscriptionManager.EXPECT().GetManagementInfo(mock.Anything, uint(1)).Return(capabilities, nil).Once()

		// Mock management result - redirect to portal
		managementResult := &pluginCore.ManagementResult{
			Action: pluginCore.ActionRedirect,
			URL:    "https://dashboard.stripe.com/customer/portal/session_123",
		}
		mockGateway.MockSubscriptionManager.EXPECT().GetManagementURL(mock.Anything, uint(1), pluginCore.OperationCancel).Return(managementResult, nil).Once()

		// Create authenticated request
		req, err := ts.createAuthenticatedRequest(ctx, "POST", "/api/account/billing/cancel", nil, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		var response dto.ManagementResultResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(tb, err)

		assert.Equal(tb, pluginCore.ActionRedirect, response.Action)
		assert.Equal(tb, "https://dashboard.stripe.com/customer/portal/session_123", response.URL)

	}, getUserAPITestOptions())
}

func TestHandleCancelOperation_NotSupported(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock active subscription
		planID := uint(42)
		mockSubscriber := createMockSubscriber(1, "atlos", "sub_123", true, &planID)
		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(mockSubscriber, nil).Once()

		// Mock gateway
		mockGateway := newCombinedGatewayMock(t)
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "atlos").Return(mockGateway, nil).Once()

		// Mock management capabilities - Atlas doesn't support cancellation in this scenario
		capabilities := &pluginCore.ManagementCapabilities{
			ManagementMode: pluginCore.ModeAPI,
			Operations: map[pluginCore.ManagementOperation]bool{
				pluginCore.OperationCancel:     false,
				pluginCore.OperationChangePlan: false,
			},
		}
		mockGateway.MockSubscriptionManager.EXPECT().GetManagementInfo(mock.Anything, uint(1)).Return(capabilities, nil).Once()

		// Create authenticated request
		req, err := ts.createAuthenticatedRequest(ctx, "POST", "/api/account/billing/cancel", nil, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return 400 Bad Request
		assert.Equal(tb, http.StatusBadRequest, w.Code)

		var errResponse map[string]any
		err = json.Unmarshal(w.Body.Bytes(), &errResponse)
		require.NoError(tb, err)
		assert.Contains(tb, errResponse["error"], "cancellation is not supported")

	}, getUserAPITestOptions())
}

func TestHandleCancelOperation_NoActiveSubscription(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock no active subscription
		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(nil, nil).Once()

		// Create authenticated request
		req, err := ts.createAuthenticatedRequest(ctx, "POST", "/api/account/billing/cancel", nil, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return 404 Not Found
		assert.Equal(tb, http.StatusNotFound, w.Code)

	}, getUserAPITestOptions())
}

func TestHandleCancelOperation_Unauthorized(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Create unauthenticated request
		req := ctx.NewAPIRequest("POST", "/api/account/billing/cancel", nil)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusUnauthorized, w.Code)
	}, getUserAPITestOptions())
}

func TestHandleCancelOperation_GatewayNotSubscriptionManager(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock active subscription
		planID := uint(42)
		mockSubscriber := createMockSubscriber(1, "basic-gateway", "sub_123", true, &planID)
		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(mockSubscriber, nil).Once()

		// Mock gateway that doesn't implement SubscriptionManager (just PaymentGateway)
		mockGateway := &pluginCore.MockPaymentGateway{}
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "basic-gateway").Return(mockGateway, nil).Once()

		// Create authenticated request
		req, err := ts.createAuthenticatedRequest(ctx, "POST", "/api/account/billing/cancel", nil, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusInternalServerError, w.Code)

	}, getUserAPITestOptions())
}

func TestHandleChangePlanOperation_Success_APIBased(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock active subscription
		planID := uint(42)
		mockSubscriber := createMockSubscriber(1, "atlos", "sub_123", true, &planID)
		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(mockSubscriber, nil).Once()

		// Mock gateway
		mockGateway := newCombinedGatewayMock(t)
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "atlos").Return(mockGateway, nil).Once()

		// Mock management capabilities - Atlas supports plan change
		capabilities := &pluginCore.ManagementCapabilities{
			ManagementMode: pluginCore.ModeAPI,
			Operations: map[pluginCore.ManagementOperation]bool{
				pluginCore.OperationCancel:     true,
				pluginCore.OperationChangePlan: true,
			},
		}
		mockGateway.MockSubscriptionManager.EXPECT().GetManagementInfo(mock.Anything, uint(1)).Return(capabilities, nil).Once()

		// Mock management result - API required
		endpoint := &pluginCore.APIEndpointInfo{
			Method: "POST",
			Path:   "/api/account/billing/change-plan",
		}
		managementResult := &pluginCore.ManagementResult{
			Action:      pluginCore.ActionAPIRequired,
			APIEndpoint: endpoint,
		}
		mockGateway.MockSubscriptionManager.EXPECT().GetManagementURL(mock.Anything, uint(1), pluginCore.OperationChangePlan).Return(managementResult, nil).Once()

		// Create authenticated request
		req, err := ts.createAuthenticatedRequest(ctx, "POST", "/api/account/billing/change-plan", nil, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		var response dto.ManagementResultResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(tb, err)

		assert.Equal(tb, pluginCore.ActionAPIRequired, response.Action)
		assert.Equal(tb, "POST", response.APIEndpoint.Method)
		assert.Equal(tb, "/api/account/billing/change-plan", response.APIEndpoint.Path)

	}, getUserAPITestOptions())
}

func TestHandleChangePlanOperation_Success_PortalRedirect(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock active subscription
		planID := uint(42)
		mockSubscriber := createMockSubscriber(1, "stripe", "sub_123", true, &planID)
		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(mockSubscriber, nil).Once()

		// Mock gateway
		mockGateway := &combinedGatewayMock{
			MockPaymentGateway:      pluginCore.NewMockPaymentGateway(t),
			MockSubscriptionManager: pluginCore.NewMockSubscriptionManager(t),
		}
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "stripe").Return(mockGateway, nil).Once()

		// Mock management capabilities - Stripe supports plan change
		capabilities := &pluginCore.ManagementCapabilities{
			ManagementMode: pluginCore.ModePortal,
			Operations: map[pluginCore.ManagementOperation]bool{
				pluginCore.OperationCancel:     true,
				pluginCore.OperationChangePlan: true,
			},
		}
		mockGateway.MockSubscriptionManager.EXPECT().GetManagementInfo(mock.Anything, uint(1)).Return(capabilities, nil).Once()

		// Mock management result - redirect to portal
		managementResult := &pluginCore.ManagementResult{
			Action: pluginCore.ActionRedirect,
			URL:    "https://dashboard.stripe.com/customer/portal/session_456",
		}
		mockGateway.MockSubscriptionManager.EXPECT().GetManagementURL(mock.Anything, uint(1), pluginCore.OperationChangePlan).Return(managementResult, nil).Once()

		// Create authenticated request
		req, err := ts.createAuthenticatedRequest(ctx, "POST", "/api/account/billing/change-plan", nil, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		var response dto.ManagementResultResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(tb, err)

		assert.Equal(tb, pluginCore.ActionRedirect, response.Action)
		assert.Equal(tb, "https://dashboard.stripe.com/customer/portal/session_456", response.URL)

	}, getUserAPITestOptions())
}

func TestHandleChangePlanOperation_NotSupported(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock active subscription
		planID := uint(42)
		mockSubscriber := createMockSubscriber(1, "atlos", "sub_123", true, &planID)
		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(mockSubscriber, nil).Once()

		// Mock gateway
		mockGateway := newCombinedGatewayMock(t)
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "atlos").Return(mockGateway, nil).Once()

		// Mock management capabilities - Atlas doesn't support plan change in this scenario
		capabilities := &pluginCore.ManagementCapabilities{
			ManagementMode: pluginCore.ModeAPI,
			Operations: map[pluginCore.ManagementOperation]bool{
				pluginCore.OperationCancel:     true,
				pluginCore.OperationChangePlan: false,
			},
		}
		mockGateway.MockSubscriptionManager.EXPECT().GetManagementInfo(mock.Anything, uint(1)).Return(capabilities, nil).Once()

		// Create authenticated request
		req, err := ts.createAuthenticatedRequest(ctx, "POST", "/api/account/billing/change-plan", nil, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return 400 Bad Request
		assert.Equal(tb, http.StatusBadRequest, w.Code)

		var errResponse map[string]any
		err = json.Unmarshal(w.Body.Bytes(), &errResponse)
		require.NoError(tb, err)
		assert.Contains(tb, errResponse["error"], "plan change is not supported")

	}, getUserAPITestOptions())
}

func TestHandleChangePlanOperation_NoActiveSubscription(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock no active subscription
		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(nil, nil).Once()

		// Create authenticated request
		req, err := ts.createAuthenticatedRequest(ctx, "POST", "/api/account/billing/change-plan", nil, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return 404 Not Found
		assert.Equal(tb, http.StatusNotFound, w.Code)

	}, getUserAPITestOptions())
}

func TestHandleChangePlanOperation_Unauthorized(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Create unauthenticated request
		req := ctx.NewAPIRequest("POST", "/api/account/billing/change-plan", nil)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusUnauthorized, w.Code)

	}, getUserAPITestOptions())
}

func TestHandleChangePlanOperation_GatewayNotSubscriptionManager(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock active subscription
		planID := uint(42)
		mockSubscriber := createMockSubscriber(1, "basic-gateway", "sub_123", true, &planID)
		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(mockSubscriber, nil).Once()

		// Mock gateway that doesn't implement SubscriptionManager (just PaymentGateway)
		mockGateway := &pluginCore.MockPaymentGateway{}
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "basic-gateway").Return(mockGateway, nil).Once()

		// Create authenticated request
		req, err := ts.createAuthenticatedRequest(ctx, "POST", "/api/account/billing/change-plan", nil, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusInternalServerError, w.Code)

	}, getUserAPITestOptions())
}
