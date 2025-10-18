package core

import (
	"context"

	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/portal/core"
)

// Re-export the Subscriber type from internal models for external access
type Subscriber = models.Subscriber

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

	// Subscriber management methods
	// CreateOrUpdateSubscriber creates or updates a subscriber record
	CreateOrUpdateSubscriber(userID uint, gatewayType, gatewayID string, isActive bool, planID *uint) error
	// DeactivateSubscriber deactivates a subscriber
	DeactivateSubscriber(userID uint, gatewayType string) error
	// GetActiveSubscriber returns an active subscriber for the given user and gateway
	GetActiveSubscriber(userID uint, gatewayType string) (*Subscriber, error)
	// IsUserActiveSubscriber checks if a user has an active subscription with any gateway
	IsUserActiveSubscriber(userID uint) (bool, error)
	// GetActiveSubscribersByGateway returns all active subscribers for a specific gateway
	GetActiveSubscribersByGateway(gatewayType string) ([]Subscriber, error)
	// GetActiveSubscription returns the first active subscription for a user across all gateways
	GetActiveSubscription(userID uint) (*Subscriber, error)
}
