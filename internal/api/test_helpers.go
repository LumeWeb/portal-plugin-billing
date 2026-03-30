package api

import (
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
	pluginConfig "go.lumeweb.com/portal-plugin-billing/internal/config"
	"go.lumeweb.com/portal-plugin-billing/internal"
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
	)
}

// getAdminAPITestOptions returns test options for tests that need the Admin API Extension
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
