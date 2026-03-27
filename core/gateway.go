package core

import (
	"context"
	"errors"
	"time"

	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
)

var (
	ErrGatewayNotFound = errors.New("gateway not found")
)

// PaymentGateway defines the interface for payment gateway implementations.
// Implementations should handle payment processing, subscriptions, and webhook validation
// for specific payment providers like Stripe, PayPal, etc.
type PaymentGateway interface {
	// ID returns the unique identifier for this gateway (e.g. "stripe", "paypal")
	ID(ctx context.Context) string

	// HandleWebhook processes an incoming webhook event from the payment provider
	// Returns an error if processing failed
	HandleWebhook(ctx context.Context, payload []byte) error

	// ValidateWebhook verifies the authenticity of an incoming webhook request
	// using the provider's signature verification mechanism
	ValidateWebhook(ctx context.Context, signature string, payload []byte) error

	// SignatureHeader returns the HTTP header name that contains the webhook signature
	// This is provider-specific (e.g. "Stripe-Signature" for Stripe)
	SignatureHeader(ctx context.Context) string

	// ExtractEventID extracts the unique event identifier from the webhook payload
	// This is used for deduplication purposes
	ExtractEventID(ctx context.Context, payload []byte) (string, error)

	// ExtractEventType extracts the event type from the webhook payload
	// This is used for logging and monitoring purposes
	ExtractEventType(ctx context.Context, payload []byte) (string, error)

	// GetCustomerPortalURL creates and returns a customer portal session URL for the given user
	// Returns the URL where the user can manage their subscription and payment methods
	GetCustomerPortalURL(ctx context.Context, userID uint, returnUrl string) (string, error)

	// GetCheckoutUI returns UI fragments for checkout flows
	// Each gateway returns fragments appropriate for their payment method:
	// - Redirect-based gateways (Stripe): return link fragment
	// - Embedded gateways (PayPal/Braintree): return script, button, or form fragments
	// - Custom gateways: return html, iframe, or modal fragments
	GetCheckoutUI(ctx context.Context, userID uint, planID uint) (*CheckoutUIResponse, error)

	// GetCustomerPortalMetadata returns metadata for customer portal configuration
	// Returns configuration options for customer portal rendering
	GetCustomerPortalMetadata(ctx context.Context, userID uint) (map[string]any, error)

	// GetName returns display name for the gateway
	GetName(ctx context.Context) string

	// GetDescription returns description for the gateway
	GetDescription(ctx context.Context) string

	// SyncPlan synchronizes a pricing plan with the gateway
	// Creates products and prices in the gateway for the given plan
	SyncPlan(ctx context.Context, plan *models.PricingPlan) (*SyncResult, error)

	// GetLogo returns the logo image data for this gateway
	// Returns the raw logo bytes for public display
	GetLogo(ctx context.Context) ([]byte, error)
}

// CheckoutUIResponse represents UI fragments for checkout flows
type CheckoutUIResponse struct {
	// Fragments provide flexible UI rendering - gateways return what they need
	Fragments []CheckoutUIFragment `json:"fragments"`
	
	// Session identifier for tracking (gateway-specific)
	// e.g., Stripe session ID, PayPal order ID
	SessionID string `json:"session_id,omitempty"`
	
	// When this checkout UI expires (if applicable)
	ExpiresAt time.Time `json:"expires_at,omitempty"`
	
	// Gateway-specific metadata
	Metadata map[string]any `json:"metadata,omitempty"`
}

// CheckoutUIFragment represents a single UI fragment for checkout
type CheckoutUIFragment struct {
	Type     FragmentType `json:"type"`             // Fragment type
	HTML     string       `json:"html,omitempty"`   // HTML content (for html, iframe, modal, button, form)
	Script   string       `json:"script,omitempty"` // JavaScript code to execute
	Link     string       `json:"link,omitempty"`   // Redirect URL
	CSS      string       `json:"css,omitempty"`    // CSS for iframe/embed
	Metadata map[string]any `json:"metadata,omitempty"` // Fragment-specific metadata
}

// FragmentType represents types of UI fragments
type FragmentType string

const (
	// FragmentTypeLink represents a redirect to hosted checkout page
	FragmentTypeLink FragmentType = "link"
	// FragmentTypeHTML represents embedded HTML content
	FragmentTypeHTML FragmentType = "html"
	// FragmentTypeScript represents JavaScript SDK initialization
	FragmentTypeScript FragmentType = "script"
	// FragmentTypeIframe represents an embedded iframe
	FragmentTypeIframe FragmentType = "iframe"
	// FragmentTypeModal represents a modal popup
	FragmentTypeModal FragmentType = "modal"
	// FragmentTypeButton represents a clickable button
	FragmentTypeButton FragmentType = "button"
	// FragmentTypeForm represents a form with fields
	FragmentTypeForm FragmentType = "form"
)

// GatewayCapabilities declares synchronization capabilities for gateways
type GatewayCapabilities interface {
	// SupportsProductSync returns true if gateway supports product/price synchronization
	SupportsProductSync() bool

	// SupportsPriceUpdates returns true if gateway supports updating existing prices
	SupportsPriceUpdates() bool

	// SupportsPlanDeletion returns true if gateway supports deleting plans
	SupportsPlanDeletion() bool

	// RequiredPricingFields returns fields required for pricing plan creation
	RequiredPricingFields() []string
}

// SyncResult represents the result of pricing plan synchronization with a gateway
type SyncResult struct {
	Success               bool   // Whether synchronization succeeded
	ProductID             string // Gateway's product identifier
	MonthlyPriceID        string // Gateway's monthly price identifier
	YearlyPriceID         string // Gateway's yearly price identifier
	PortalConfigurationID string // Gateway's portal configuration identifier
	Error                 error  // Error if synchronization failed
}
