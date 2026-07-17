package api

import (
	"encoding/json"
	"net/http/httptest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/internal"
	pluginConfig "go.lumeweb.com/portal-plugin-billing/internal/config"
	coreTesting "go.lumeweb.com/portal/core/testing"
)

// getUserAPITestOptions returns test options for tests that need the user-facing API Extension
func getUserAPITestOptions() coreTesting.TestContextBuilderOption {
	return coreTesting.CombineOptions(
		// Register the Dashboard API Extension for user-facing routes
		coreTesting.WithAPIExtension(NewAPIExtension()),
		// Register admin services for any that might be needed by user API
		coreTesting.WithServiceConfig(internal.PLUGIN_NAME, pluginCore.BILLING_SERVICE, &pluginConfig.ServiceConfig{}),
		// Add BillingService mock
		coreTesting.WithMockServiceFactory(pluginCore.BILLING_SERVICE, pluginCore.NewMockBillingService, &pluginConfig.ServiceConfig{}),
		// Add PricingService mock for pricing plan tests
		coreTesting.WithMockServiceFactory(pluginCore.PRICING_SERVICE, pluginCore.NewMockPricingService, &pluginConfig.ServiceConfig{}),
		coreTesting.WithMockServiceFactory(pluginCore.CREDIT_SERVICE, pluginCore.NewMockCreditService, &pluginConfig.ServiceConfig{}),
	)
}

// requireErrorResponse asserts the HTTP response body contains the canonical
// error shape {"error":{"reason":"<reason>",...}} and that reason matches.
func requireErrorResponse(t require.TestingT, w *httptest.ResponseRecorder, reason string) {
	var body map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &body)
	require.NoError(t, err)
	errObj, ok := body["error"].(map[string]any)
	require.True(t, ok, "expected error to be an object, got: %s", w.Body.String())
	assert.Equal(t, reason, errObj["reason"])
}

func getAdminAPITestOptions() coreTesting.TestContextBuilderOption {
	return coreTesting.CombineOptions(
		// Register the Admin API Extension for admin routes
		coreTesting.WithAPIExtension(NewAdminExtension()),
		coreTesting.WithServiceConfig(internal.PLUGIN_NAME, pluginCore.BILLING_SERVICE, &pluginConfig.ServiceConfig{}),
		// Add BillingService mock
		coreTesting.WithMockServiceFactory(pluginCore.BILLING_SERVICE, pluginCore.NewMockBillingService, &pluginConfig.ServiceConfig{}),
		// Add PricingService mock for pricing plan tests
		coreTesting.WithMockServiceFactory(pluginCore.PRICING_SERVICE, pluginCore.NewMockPricingService, &pluginConfig.ServiceConfig{}),
		// Add CreditService mock for credit endpoint tests
		coreTesting.WithMockServiceFactory(pluginCore.CREDIT_SERVICE, pluginCore.NewMockCreditService, &pluginConfig.ServiceConfig{}),
	)
}
