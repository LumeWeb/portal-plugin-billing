package core

import (
	"context"
	"time"

	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/portal/core"
	"go.lumeweb.com/queryutil"
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
	GetGateway(ctx context.Context, gatewayType string) (GatewayIdentity, error)

	// Subscriber management methods
	// CreateOrUpdateSubscriber creates or updates a subscriber record
	// Optional parameters can be passed using WithBillingPeriodStart and WithBillingPeriodEnd
	CreateOrUpdateSubscriber(ctx context.Context, userID uint, gatewayType, externalID, subscriptionID string, isActive bool, pricingPlanPeriodID *uint, opts ...SubscriberOption) error
	// DeactivateSubscriber deactivates a subscriber
	DeactivateSubscriber(ctx context.Context, userID uint, gatewayType string) error
	// GetActiveSubscriber returns an active subscriber for the given user and gateway
	GetActiveSubscriber(ctx context.Context, userID uint, gatewayType string) (*Subscriber, error)
	// GetSubscriberByExternalID returns a subscriber by external ID and gateway type
	GetSubscriberByExternalID(ctx context.Context, externalID, gatewayType string) (*Subscriber, error)
	// GetSubscriberBySubscriptionID returns a subscriber by subscription ID and gateway type
	GetSubscriberBySubscriptionID(ctx context.Context, subscriptionID, gatewayType string) (*Subscriber, error)
	// IsUserActiveSubscriber checks if a user has an active subscription with any gateway
	IsUserActiveSubscriber(ctx context.Context, userID uint) (bool, error)
	// GetActiveSubscribersByGateway returns all active subscribers for a specific gateway
	GetActiveSubscribersByGateway(ctx context.Context, gatewayType string) ([]Subscriber, error)
	// GetPendingCancellations returns subscribers with pending cancellations for a gateway
	// These are subscribers with WillCancelAt set to a date in the past or equal to now
	GetPendingCancellations(ctx context.Context, gatewayType string, now time.Time) ([]Subscriber, error)
	// GetActiveSubscription returns the first active subscription for a user across all gateways
	GetActiveSubscription(ctx context.Context, userID uint) (*Subscriber, error)
	// GetSubscriberByID returns a subscriber by database ID
	GetSubscriberByID(ctx context.Context, id uint) (*Subscriber, error)
	// GetSubscribersByUserID returns all subscribers for a specific user
	GetSubscribersByUserID(ctx context.Context, userID uint) ([]Subscriber, error)
	// ListSubscribers returns a paginated list of subscribers with optional filtering
	ListSubscribers(ctx context.Context, filters []queryutil.CrudFilter, sorts []queryutil.Sort, pagination queryutil.Pagination) ([]Subscriber, int64, error)
	// GetRegistry returns the gateway registry for querying available gateways
	GetRegistry(ctx context.Context) GatewayRegistry
	// GetCheckoutUI returns checkout UI fragments for a plan
	GetCheckoutUI(ctx context.Context, userID uint, planID uint, gatewayType string, periodID uint) (*CheckoutUIResponse, error)
	// UpdateSubscriberPlan updates a subscriber's pricing plan period in the database
	// This is used for database-only plan changes when the gateway doesn't support backend plan changes
	UpdateSubscriberPlan(ctx context.Context, userID uint, gatewayType string, newPeriodID uint) (*PlanChangeResult, error)
}

// GatewayRegistry provides access to gateway information
type GatewayRegistry interface {
	GetAllGateways() map[string]GatewayIdentity
}
