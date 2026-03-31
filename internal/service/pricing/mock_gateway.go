package pricing

import (
	"context"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
)

// MockablePaymentGateway is a composite interface that combines all payment gateway related interfaces
// for easier mocking in tests. This interface is used specifically for testing purposes.
type MockablePaymentGateway interface {
	pluginCore.PaymentGateway
	pluginCore.GatewayCapabilities
	pluginCore.WebhookHandler
	pluginCore.CustomerPortal
	pluginCore.CheckoutProvider
	pluginCore.GatewaySync
}

// MockableBillingService extends the core BillingService for testing purposes
type MockableBillingService interface {
	pluginCore.BillingService
}

// GatewayRegistry is a subset of the billing service for accessing gateways in tests
type GatewayRegistry interface {
	GetAllGateways() map[string]pluginCore.GatewayIdentity
	GetRegistry(ctx context.Context) GatewayRegistry
}
