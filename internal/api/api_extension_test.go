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
	billingModels "go.lumeweb.com/portal-plugin-billing/internal/db/models"
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
		billingSvc.On("ProcessWebhook", mock.Anything, "stripe", "test_sig", []byte(`{"test":"payload"}`)).
			Return(nil).Once()

		// Create request
		req := httptest.NewRequest("POST", "/api/account/billing/webhooks/stripe", bytes.NewReader([]byte(`{"test":"payload"}`)))
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
		mockSubscriber := &billingModels.Subscriber{
			UserID:      1,
			GatewayType: "stripe",
			GatewayID:   "sub_123",
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

func TestHandleSubscriptionStatus_NoSubscription(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Retrieve necessary services and router from the context
		billingSvc := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		userSvc := core.GetService[*mocks.MockUserService](ctx, core.USER_SERVICE)
		router := ctx.Router()
		domain := ctx.APISubdomain("dashboard", false)

		userSvc.On("AccountExists", uint(1)).Return(true, nil, nil)

		// Mock the billing service to return no subscription
		billingSvc.On("GetActiveSubscription", uint(1)).Return((*billingModels.Subscriber)(nil), nil)

		// Create valid JWT token using helper
		jwtToken, err := createTestJWT(ctx, "1")
		assert.NoError(tb, err, "Failed to generate test JWT")

		// Create authenticated request (no subscription created)
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

		assert.False(tb, response.IsSubscribed)
		assert.Equal(tb, "", response.GatewayType)
		assert.Nil(tb, response.PlanID)

	})
}

func TestHandleSubscriptionStatus_InactiveSubscription(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		// Retrieve necessary services and router from the context
		billingSvc := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		userSvc := core.GetService[*mocks.MockUserService](ctx, core.USER_SERVICE)
		router := ctx.Router()
		domain := ctx.APISubdomain("dashboard", false)

		userSvc.On("AccountExists", uint(1)).Return(true, nil, nil)

		// Mock the billing service to return no subscription (inactive)
		billingSvc.On("GetActiveSubscription", uint(1)).Return((*billingModels.Subscriber)(nil), nil)

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

		assert.False(tb, response.IsSubscribed)
		assert.Equal(tb, "", response.GatewayType)
		assert.Nil(tb, response.PlanID)
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
		mockSubscriber := &billingModels.Subscriber{
			UserID:      1,
			GatewayType: "paypal", // Different gateway to test multiple scenarios
			GatewayID:   "sub_456",
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
