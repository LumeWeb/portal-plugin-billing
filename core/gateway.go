package core

import (
	"context"
	"errors"
)

var (
	ErrGatewayNotFound = errors.New("gateway not found")
)

// PaymentGateway defines the interface for payment gateway implementations.
// Implementations should handle payment processing, subscriptions, and webhook validation
// for specific payment providers like Stripe, PayPal, etc.
type PaymentGateway interface {
	// ID returns the unique identifier for this gateway (e.g. "stripe", "paypal")
	ID() string

	// HandleWebhook processes an incoming webhook event from the payment provider
	// Returns an error if processing failed
	HandleWebhook(ctx context.Context, payload []byte) error

	// ValidateWebhook verifies the authenticity of an incoming webhook request
	// using the provider's signature verification mechanism
	ValidateWebhook(ctx context.Context, signature string, payload []byte) error

	// SignatureHeader returns the HTTP header name that contains the webhook signature
	// This is provider-specific (e.g. "Stripe-Signature" for Stripe)
	SignatureHeader() string

	// ExtractEventID extracts the unique event identifier from the webhook payload
	// This is used for deduplication purposes
	ExtractEventID(payload []byte) (string, error)

	// ExtractEventType extracts the event type from the webhook payload
	// This is used for logging and monitoring purposes
	ExtractEventType(payload []byte) (string, error)
}
