package core

import (
	"context"

	"go.lumeweb.com/portal/core"
	quotaCore "go.lumeweb.com/portal-plugin-quota/core"
)

// GatewaySetupFunc is a function that sets up and registers a gateway
// Returns a log message (empty if not configured), the gateway instance (nil if not configured), and any error
type GatewaySetupFunc func(opts GatewaySetupOptions) (string, PaymentGateway, error)

// GatewaySetupOptions contains options for gateway setup
type GatewaySetupOptions struct {
	Logger     *core.Logger
	Ctx        core.Context
	Context    context.Context
	BillingSvc BillingService
	PricingSvc PricingService
	HTTP       core.HTTPService
	Quota      quotaCore.QuotaService
	User       core.UserService
}
