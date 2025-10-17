package core

import (
	"context"

	"go.lumeweb.com/portal/core"
)

const BILLING_SERVICE = "billing"

// BillingService handles billing operations and webhook processing
type BillingService interface {
	core.Service
	core.Configurable
	// ProcessWebhook processes an incoming webhook from a payment gateway
	ProcessWebhook(ctx context.Context, gatewayType string, signature string, payload []byte) error
	// GetSignatureHeader returns the HTTP header name used for webhook signature verification
	GetSignatureHeader(gatewayType string) (string, error)
	// RegisterGateway registers a PaymentGateway with the billing service and returns an error if registration fails.
	RegisterGateway(gateway PaymentGateway) error
}
