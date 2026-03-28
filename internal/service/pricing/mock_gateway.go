package pricing

import (
	"context"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
)

// MockablePaymentGateway is a composite interface that combines PaymentGateway and GatewaySyncCapabilities
// for easier mocking in tests. This interface is used specifically for testing purposes.
//go:generate mockery --output=../../mocks --name=MockablePaymentGateway
type MockablePaymentGateway interface {
	pluginCore.PaymentGateway
	pluginCore.GatewaySyncCapabilities
}

// MockableBillingService extends the core BillingService for testing purposes
//go:generate mockery --output=../../mocks --name=MockableBillingService
type MockableBillingService interface {
	pluginCore.BillingService
}

// GatewayRegistry is a subset of the billing service for accessing gateways in tests
type GatewayRegistry interface {
	GetAllGateways() map[string]pluginCore.PaymentGateway
	GetRegistry(ctx context.Context) GatewayRegistry
}
