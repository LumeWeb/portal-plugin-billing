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
	GetSignatureHeader(ctx context.Context, gatewayType string) (string, error)
	// RegisterGateway registers a PaymentGateway with the billing service and returns an error if registration fails.
	RegisterGateway(ctx context.Context, gateway PaymentGateway) error
	// GetGateway returns a registered payment gateway by type
	// Returns pluginCore.ErrGatewayNotFound if the gateway is not registered
	GetGateway(ctx context.Context, gatewayType string) (PaymentGateway, error)

	// Subscriber management methods
	// CreateOrUpdateSubscriber creates or updates a subscriber record
	CreateOrUpdateSubscriber(ctx context.Context, userID uint, gatewayType, gatewayID string, isActive bool, planID *uint) error
	// DeactivateSubscriber deactivates a subscriber
	DeactivateSubscriber(ctx context.Context, userID uint, gatewayType string) error
	// GetActiveSubscriber returns an active subscriber for the given user and gateway
	GetActiveSubscriber(ctx context.Context, userID uint, gatewayType string) (*Subscriber, error)
	// GetSubscriberByGatewayID returns a subscriber by gateway ID and gateway type
	GetSubscriberByGatewayID(ctx context.Context, gatewayID, gatewayType string) (*Subscriber, error)
	// IsUserActiveSubscriber checks if a user has an active subscription with any gateway
	IsUserActiveSubscriber(ctx context.Context, userID uint) (bool, error)
	// GetActiveSubscribersByGateway returns all active subscribers for a specific gateway
	GetActiveSubscribersByGateway(ctx context.Context, gatewayType string) ([]Subscriber, error)
	// GetActiveSubscription returns the first active subscription for a user across all gateways
	GetActiveSubscription(ctx context.Context, userID uint) (*Subscriber, error)
	// GetRegistry returns the gateway registry for querying available gateways
	GetRegistry(ctx context.Context) GatewayRegistry
	// GetCheckoutUI returns checkout UI fragments for a plan
	GetCheckoutUI(ctx context.Context, userID uint, planID uint, gatewayType string) (*CheckoutUIResponse, error)
}

// GatewayRegistry provides access to gateway information
type GatewayRegistry interface {
	GetAllGateways() map[string]PaymentGateway
}
