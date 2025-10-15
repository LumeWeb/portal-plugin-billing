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
	ProcessWebhook(ctx context.Context, gatewayType string, signature string, payload []byte) error
	GetSignatureHeader(gatewayType string) (string, error)
}
