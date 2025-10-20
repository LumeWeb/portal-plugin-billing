package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.lumeweb.com/portal-middleware/auth/jwt"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/internal"
	"go.lumeweb.com/portal-plugin-billing/internal/api/dto"
	pluginConfig "go.lumeweb.com/portal-plugin-billing/internal/config"
	"go.lumeweb.com/portal/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
	"go.lumeweb.com/portal/core/testing/mocks"
)

// Helper function to create a valid JWT token for testing
func createTestJWT(ctx coreTesting.TestContext, userID string) (string, error) {
	pk := ctx.Config().Config().Core.Identity.PrivateKey()
	return jwt.CreateToken(pk, ctx.Config().Config().Core.Domain, userID, jwt.PurposeLogin, 90*24*time.Hour)
}

func TestMain(m *testing.M) {
	// Use the new framework's TestMain helper to set up the shared environment.
	// We use WithOptions because these tests do not require a real database.
	coreTesting.WithOptions(m,
		// Configure the domain for the API
		coreTesting.WithConfig("core.domain", "example.com"),
		// Register the Dashboard API using the helper
		coreTesting.WithAPIExtension(NewAPIExtension()),
		coreTesting.WithConfig("plugin.dashboard.api.subdomain", "account"),
		coreTesting.WithServiceConfig(internal.PLUGIN_NAME, pluginCore.BILLING_SERVICE, &pluginConfig.ServiceConfig{}),
		// Explicitly add the BillingService mock, as it's not in the core defaults
		coreTesting.WithMockServiceFactory(pluginCore.BILLING_SERVICE, pluginCore.NewMockBillingService),
	)
}

func TestHandleWebhook_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Retrieve necessary services and router from the context
		billingSvc := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		router := ctx.Router()
		domain := ctx.APISubdomain("dashboard", false)

		// Mock expectations
		billingSvc.On("GetSignatureHeader", "stripe").Return("Stripe-Signature", nil).Once()
		
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

		billingSvc.On("ProcessWebhook", mock.Anything, "stripe", "test_sig", []byte(webhookPayload)).
			Return(nil).Once()

		// Create request
		req := httptest.NewRequest("POST", "/api/account/billing/webhooks/stripe", bytes.NewReader([]byte(webhookPayload)))
		req.Host = domain
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Stripe-Signature", "test_sig")
		w := httptest.NewRecorder()

		// Execute
		router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusNoContent, w.Code)

	})
}

func TestHandleWebhook_InvalidGateway(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Retrieve necessary services and router from the context
		billingSvc := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		router := ctx.Router()
		domain := ctx.APISubdomain("dashboard", false)

		// Mock expectations
		billingSvc.On("GetSignatureHeader", "invalid").Return("", pluginCore.ErrGatewayNotFound).Once()

		// Create request
		req := httptest.NewRequest("POST", "/api/account/billing/webhooks/invalid", bytes.NewReader([]byte(`{"test":"payload"}`)))
		req.Host = domain
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		// Execute
		router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusBadRequest, w.Code)

	})
}

func TestHandleSubscriptionStatus_ActiveSubscription(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Retrieve necessary services and router from the context
		billingSvc := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		userSvc := core.GetService[*mocks.MockUserService](ctx, core.USER_SERVICE)
		router := ctx.Router()
		domain := ctx.APISubdomain("dashboard", false)

		userSvc.On("AccountExists", uint(1)).Return(true, nil, nil)

		// Mock the billing service to return an active subscription
		planID := uint(42)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:      1,
			GatewayType: "stripe",
			GatewayID:   "cus_123", // Changed to customer ID format
			IsActive:    true,
			PlanID:      &planID,
		}
		billingSvc.On("GetActiveSubscription", uint(1)).Return(mockSubscriber, nil)

		// Create valid JWT token using helper
		jwtToken, err := createTestJWT(ctx, "1")
		assert.NoError(tb, err, "Failed to generate test JWT")

		// Create authenticated request
		req := httptest.NewRequest("GET", "/api/account/billing/subscription", nil)
		req.Host = domain
		req.Header.Set("Authorization", "Bearer "+jwtToken)
		w := httptest.NewRecorder()

		// Execute
		router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		// Parse response using DTO
		var response dto.SubscriptionStatusResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(tb, err)

		assert.True(tb, response.IsSubscribed)
		assert.Equal(tb, "stripe", response.GatewayType)
		assert.NotNil(tb, response.PlanID)
		assert.Equal(tb, uint(42), *response.PlanID)

	})
}

func TestHandleSubscriptionStatus_NoActiveSubscription(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Retrieve necessary services and router from the context
		billingSvc := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		userSvc := core.GetService[*mocks.MockUserService](ctx, core.USER_SERVICE)
		router := ctx.Router()
		domain := ctx.APISubdomain("dashboard", false)

		userSvc.On("AccountExists", uint(1)).Return(true, nil, nil)

		// Mock the billing service to return no active subscription
		// This covers both scenarios: no subscription exists and inactive subscriptions
		// (GetActiveSubscription only returns active subscriptions)
		billingSvc.On("GetActiveSubscription", uint(1)).Return((*pluginCore.Subscriber)(nil), nil)

		// Create valid JWT token using helper
		jwtToken, err := createTestJWT(ctx, "1")
		assert.NoError(tb, err, "Failed to generate test JWT")

		// Create authenticated request
		req := httptest.NewRequest("GET", "/api/account/billing/subscription", nil)
		req.Host = domain
		req.Header.Set("Authorization", "Bearer "+jwtToken)
		w := httptest.NewRecorder()

		// Execute
		router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		// Parse response using DTO
		var response dto.SubscriptionStatusResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(tb, err)

		// Both no subscription and inactive subscription scenarios should return the same response
		assert.False(tb, response.IsSubscribed, "Should return is_subscribed=false when no active subscription exists")
		assert.Equal(tb, "", response.GatewayType, "GatewayType should be empty when no active subscription")
		assert.Nil(tb, response.PlanID, "PlanID should be nil when no active subscription")
	})
}

func TestHandleSubscriptionStatus_MultipleGateways(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Retrieve necessary services and router from the context
		billingSvc := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		userSvc := core.GetService[*mocks.MockUserService](ctx, core.USER_SERVICE)
		router := ctx.Router()
		domain := ctx.APISubdomain("dashboard", false)

		userSvc.On("AccountExists", uint(1)).Return(true, nil, nil)

		// Mock the billing service to return an active subscription (could be any gateway)
		planID := uint(99)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:      1,
			GatewayType: "paypal", // Different gateway to test multiple scenarios
			GatewayID:   "cus_456", // Changed to customer ID format
			IsActive:    true,
			PlanID:      &planID,
		}
		billingSvc.On("GetActiveSubscription", uint(1)).Return(mockSubscriber, nil)

		// Create valid JWT token using helper
		jwtToken, err := createTestJWT(ctx, "1")
		assert.NoError(tb, err, "Failed to generate test JWT")

		// Create authenticated request
		req := httptest.NewRequest("GET", "/api/account/billing/subscription", nil)
		req.Host = domain
		req.Header.Set("Authorization", "Bearer "+jwtToken)
		w := httptest.NewRecorder()

		// Execute
		router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		// Parse response using DTO
		var response dto.SubscriptionStatusResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(tb, err)

		// Should return the mocked subscription
		assert.True(tb, response.IsSubscribed)
		assert.Equal(tb, "paypal", response.GatewayType)
		assert.NotNil(tb, response.PlanID)
		assert.Equal(tb, uint(99), *response.PlanID)
	})
}

func TestHandleSubscriptionStatus_Unauthorized(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Retrieve necessary services and router from the context
		router := ctx.Router()
		domain := ctx.APISubdomain("dashboard", false)

		// Create unauthenticated request (no auth header)
		req := httptest.NewRequest("GET", "/api/account/billing/subscription", nil)
		req.Host = domain
		w := httptest.NewRecorder()

		// Execute
		router.ServeHTTP(w, req)

		// Verify - should return unauthorized
		assert.Equal(tb, http.StatusUnauthorized, w.Code)
	})
}

func TestHandleCustomerPortal_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Retrieve necessary services and router from the context
		billingSvc := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		userSvc := core.GetService[*mocks.MockUserService](ctx, core.USER_SERVICE)
		router := ctx.Router()
		domain := ctx.APISubdomain("dashboard", false)

		userSvc.On("AccountExists", uint(1)).Return(true, nil, nil)

		// Mock active subscription
		planID := uint(42)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:      1,
			GatewayType: "stripe",
			GatewayID:   "cus_123",
			IsActive:    true,
			PlanID:      &planID,
		}
		billingSvc.On("GetActiveSubscription", uint(1)).Return(mockSubscriber, nil)

		// Mock gateway retrieval
		mockGateway := pluginCore.NewMockPaymentGateway(t)
		billingSvc.On("GetGateway", "stripe").Return(mockGateway, nil)

		// Mock customer portal URL generation
		mockGateway.On("GetCustomerPortalURL", mock.Anything, uint(1), "https://example.com/return").
			Return("https://billing.stripe.com/session/123", nil)

		// Create valid JWT token
		jwtToken, err := createTestJWT(ctx, "1")
		assert.NoError(tb, err, "Failed to generate test JWT")

		// Create request body
		requestBody := map[string]string{
			"return_url": "https://example.com/return",
		}
		bodyBytes, _ := json.Marshal(requestBody)

		// Create authenticated request
		req := httptest.NewRequest("POST", "/api/account/billing/customer-portal", bytes.NewReader(bodyBytes))
		req.Host = domain
		req.Header.Set("Authorization", "Bearer "+jwtToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		// Execute
		router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		// Parse response
		var response dto.CustomerPortalResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(tb, err)

		assert.Equal(tb, "https://billing.stripe.com/session/123", response.URL)

		// Verify mocks were called
		billingSvc.AssertExpectations(tb)
		mockGateway.AssertExpectations(tb)
	})
}

func TestHandleCustomerPortal_NoActiveSubscription(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Retrieve necessary services and router from the context
		billingSvc := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		userSvc := core.GetService[*mocks.MockUserService](ctx, core.USER_SERVICE)
		router := ctx.Router()
		domain := ctx.APISubdomain("dashboard", false)

		userSvc.On("AccountExists", uint(1)).Return(true, nil, nil)

		// Mock no active subscription
		billingSvc.On("GetActiveSubscription", uint(1)).Return((*pluginCore.Subscriber)(nil), nil)

		// Create valid JWT token
		jwtToken, err := createTestJWT(ctx, "1")
		assert.NoError(tb, err, "Failed to generate test JWT")

		// Create request body
		requestBody := map[string]string{
			"return_url": "https://example.com/return",
		}
		bodyBytes, _ := json.Marshal(requestBody)

		// Create authenticated request
		req := httptest.NewRequest("POST", "/api/account/billing/customer-portal", bytes.NewReader(bodyBytes))
		req.Host = domain
		req.Header.Set("Authorization", "Bearer "+jwtToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		// Execute
		router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusBadRequest, w.Code)

		// Parse error response
		var errorResponse map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &errorResponse)
		assert.NoError(tb, err)

		assert.Contains(tb, errorResponse["error"], "no active subscription found")
	})
}

func TestHandleCustomerPortal_MissingReturnURL(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Retrieve necessary services and router from the context
		userSvc := core.GetService[*mocks.MockUserService](ctx, core.USER_SERVICE)
		router := ctx.Router()
		domain := ctx.APISubdomain("dashboard", false)

		userSvc.On("AccountExists", uint(1)).Return(true, nil, nil)

		// Create valid JWT token
		jwtToken, err := createTestJWT(ctx, "1")
		assert.NoError(tb, err, "Failed to generate test JWT")

		// Create request body without return_url
		requestBody := map[string]string{}
		bodyBytes, _ := json.Marshal(requestBody)

		// Create authenticated request
		req := httptest.NewRequest("POST", "/api/account/billing/customer-portal", bytes.NewReader(bodyBytes))
		req.Host = domain
		req.Header.Set("Authorization", "Bearer "+jwtToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		// Execute
		router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusBadRequest, w.Code)

		// Parse error response
		var errorResponse map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &errorResponse)
		assert.NoError(tb, err)

		assert.Contains(tb, errorResponse["error"], "return_url is required")
	})
}

func TestHandleCustomerPortal_Unauthorized(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Retrieve necessary services and router from the context
		router := ctx.Router()
		domain := ctx.APISubdomain("dashboard", false)

		// Create request body
		requestBody := map[string]string{
			"return_url": "https://example.com/return",
		}
		bodyBytes, _ := json.Marshal(requestBody)

		// Create unauthenticated request (no auth header)
		req := httptest.NewRequest("POST", "/api/account/billing/customer-portal", bytes.NewReader(bodyBytes))
		req.Host = domain
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		// Execute
		router.ServeHTTP(w, req)

		// Verify - should return unauthorized
		assert.Equal(tb, http.StatusUnauthorized, w.Code)
	})
}
