package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/internal"
	pluginConfig "go.lumeweb.com/portal-plugin-billing/internal/config"
	"go.lumeweb.com/portal/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
)

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

		// Verify mock expectations
		billingSvc.AssertExpectations(tb)
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

		// Verify mock expectations
		billingSvc.AssertExpectations(tb)
	})
}
