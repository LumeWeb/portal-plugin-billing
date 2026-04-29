package core

import (
	"context"
	"errors"
	"time"
)

// PricingVariant represents a single pricing variant with billing period and pricing details.
type PricingVariant struct {
	BillingPeriodID uint    // Billing period identifier
	PriceUSD        float64 // Price in USD
	QuotaPlanID     uint    // Associated quota plan identifier
	Cadence         string  // Billing cadence (e.g., "monthly", "yearly", "quarterly")
	RollingDays     *int    // Optional number of rolling days for rolling cadence
}

// PricingPlanInfo defines the contract for pricing plan data passed to gateways
type PricingPlanInfo struct {
	ID              uint             // Plan identifier
	Name            string           // Plan name
	Description     string           // Plan description
	Currency        string           // Currency code (e.g., "USD")
	PricingVariants []PricingVariant // List of pricing variants for this plan
	IsActive        bool             // Whether the plan is active
	IsPublic        bool             // Whether the plan is publicly available
}

var (
	ErrGatewayNotFound     = errors.New("gateway not found")
	ErrGatewayNotSupported = errors.New("gateway does not support this interface")
)

// RemotePriceMapping represents a mapping between a pricing plan period and a gateway price ID.
// PricingPlanPeriodID maps back to the database pricing_plan_period.id field.
type RemotePriceMapping struct {
	PricingPlanPeriodID uint   // Pricing plan period ID from database
	PriceID             string // Gateway's price identifier for this variant
}

// GatewayIdentity defines methods for gateway identification and display information.
// All gateways must implement this interface.
type GatewayIdentity interface {
	// ID returns the unique identifier for this gateway (e.g. "stripe", "paypal")
	ID(ctx context.Context) string

	// GetName returns display name for the gateway
	GetName(ctx context.Context) string

	// GetDescription returns description for the gateway
	GetDescription(ctx context.Context) string

	// GetLogo returns the logo image data for this gateway
	// Returns the raw logo bytes for public display
	GetLogo(ctx context.Context) ([]byte, error)
}

// WebhookHandler defines methods for processing webhook events.
// Only gateways that receive webhooks need to implement this interface.
type WebhookHandler interface {
	// SignatureHeader returns the HTTP header name that contains the webhook signature
	// This is provider-specific (e.g. "Stripe-Signature" for Stripe)
	SignatureHeader(ctx context.Context) string

	// ValidateWebhook verifies the authenticity of an incoming webhook request
	// using the provider's signature verification mechanism
	ValidateWebhook(ctx context.Context, signature string, payload []byte) error

	// ExtractEventID extracts the unique event identifier from the webhook payload
	// This is used for deduplication purposes
	ExtractEventID(ctx context.Context, payload []byte) (string, error)

	// ExtractEventType extracts the event type from the webhook payload
	// This is used for logging and monitoring purposes
	ExtractEventType(ctx context.Context, payload []byte) (string, error)

	// HandleWebhook processes an incoming webhook event from the payment provider
	// Returns an error if processing failed
	HandleWebhook(ctx context.Context, payload []byte) error
}

// CustomerPortal defines methods for customer portal functionality.
// Only gateways that support customer portals need to implement this interface.
type CustomerPortal interface {
	// GetCustomerPortalURL creates and returns a customer portal session URL for the given user
	// Returns the URL where the user can manage their subscription and payment methods
	GetCustomerPortalURL(ctx context.Context, userID uint, returnUrl string) (string, error)

	// GetCustomerPortalMetadata returns metadata for customer portal configuration
	// Returns configuration options for customer portal rendering
	GetCustomerPortalMetadata(ctx context.Context, userID uint) (map[string]any, error)
}

// CheckoutProvider defines methods for providing checkout UI.
// Only gateways that handle checkout UI need to implement this interface.
type CheckoutProvider interface {
	// GetCheckoutUI returns UI fragments for checkout flows
	// Each gateway returns fragments appropriate for their payment method:
	// - Redirect-based gateways (Stripe): return link fragment
	// - Embedded gateways (PayPal/Braintree): return script, button, or form fragments
	// - Custom gateways: return html, iframe, or modal fragments
	GetCheckoutUI(ctx context.Context, userID uint, planID uint, periodID uint) (*CheckoutUIResponse, error)
}

// GatewayCapabilities declares synchronization capabilities for gateways.
// Only gateways that support synchronization need to implement this interface.
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

// GatewaySync defines the interface for gateways that support synchronization.
// This combines GatewayCapabilities with the sync operation.
type GatewaySync interface {
	GatewayCapabilities
	// SyncPlan synchronizes a pricing plan with the gateway
	// Creates products and prices in the gateway for the given plan
	SyncPlan(ctx context.Context, plan *PricingPlanInfo) (*SyncResult, error)
}

// PaymentGateway defines the interface for payment gateway implementations.
// It composes all gateway interface, allowing gateways to implement only the functionality they support.
// Individual gateway can implement only the methods they need, while tests can use a comprehensive mock.
//
// Note: This is a union of all interfaces for type convenience in tests and services.
// Gateways may implement subsets using the helper functions As*() and Is*() below.
type PaymentGateway interface {
	// All interfaces used in the billing system
	GatewayIdentity
	WebhookHandler
	CustomerPortal
	CheckoutProvider
	GatewayCapabilities
	GatewaySync
	SubscriptionManager
	CancellationExecutor
	PlanChangeExecutor
	PauseResumeExecutor
}

// CheckoutUIResponse represents UI fragments for checkout flows
type CheckoutUIResponse struct {
	// Fragments provide flexible UI rendering - gateways return what they need
	Fragments []CheckoutUIFragment `json:"fragments"`

	// Session identifier for tracking (gateway-specific)
	// e.g., Stripe session ID, PayPal order ID
	SessionID string `json:"session_id,omitempty"`

	// When this checkout UI expires (if applicable)
	ExpiresAt time.Time `json:"expires_at"`

	// Gateway-specific metadata
	Metadata map[string]any `json:"metadata,omitempty"`
}

// CheckoutUIFragment represents a single UI fragment for checkout
type CheckoutUIFragment struct {
	Type     FragmentType   `json:"type"`               // Fragment type
	HTML     string         `json:"html,omitempty"`     // HTML content (for html, iframe, modal, button, form)
	Script   string         `json:"script,omitempty"`   // JavaScript code to execute
	Link     string         `json:"link,omitempty"`     // Redirect URL
	CSS      string         `json:"css,omitempty"`      // CSS for iframe/embed
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

// SyncResult represents the result of pricing plan synchronization with a gateway
type SyncResult struct {
	Success               bool                 // Whether synchronization succeeded
	ProductID             string               // Gateway's product identifier
	PortalConfigurationID string               // Gateway's portal configuration identifier
	RemotePriceIDs        []RemotePriceMapping // Mappings of pricing variants to gateway price IDs
	Error                 error                // Error if synchronization failed
}

// GatewayHelpers provides utility functions for checking and accessing gateway sub-interfaces.
// These helpers provide a safe, idiomatic way to check for and cast to specific gateway capabilities.

// IsWebhookHandler checks if the gateway implements the WebhookHandler interface.
func IsWebhookHandler(gateway PaymentGateway) bool {
	_, ok := gateway.(WebhookHandler)
	return ok
}

// AsWebhookHandler attempts to cast the gateway to WebhookHandler.
// Returns nil and an error if the gateway does not implement WebhookHandler.
func AsWebhookHandler(gateway GatewayIdentity) (WebhookHandler, error) {
	handler, ok := gateway.(WebhookHandler)
	if !ok {
		return nil, ErrGatewayNotSupported
	}
	return handler, nil
}

// IsCustomerPortal checks if the gateway implements the CustomerPortal interface.
func IsCustomerPortal(gateway GatewayIdentity) bool {
	_, ok := gateway.(CustomerPortal)
	return ok
}

// AsCustomerPortal attempts to cast the gateway to CustomerPortal.
// Returns nil and an error if the gateway does not implement CustomerPortal.
func AsCustomerPortal(gateway GatewayIdentity) (CustomerPortal, error) {
	portal, ok := gateway.(CustomerPortal)
	if !ok {
		return nil, ErrGatewayNotSupported
	}
	return portal, nil
}

// IsCheckoutProvider checks if the gateway implements the CheckoutProvider interface.
func IsCheckoutProvider(gateway GatewayIdentity) bool {
	_, ok := gateway.(CheckoutProvider)
	return ok
}

// AsCheckoutProvider attempts to cast the gateway to CheckoutProvider.
// Returns nil and an error if the gateway does not implement CheckoutProvider.
func AsCheckoutProvider(gateway GatewayIdentity) (CheckoutProvider, error) {
	provider, ok := gateway.(CheckoutProvider)
	if !ok {
		return nil, ErrGatewayNotSupported
	}
	return provider, nil
}

// IsGatewayCapabilities checks if the gateway implements the GatewayCapabilities interface.
func IsGatewayCapabilities(gateway GatewayIdentity) bool {
	_, ok := gateway.(GatewayCapabilities)
	return ok
}

// AsGatewayCapabilities attempts to cast the gateway to GatewayCapabilities.
// Returns nil and an error if the gateway does not implement GatewayCapabilities.
func AsGatewayCapabilities(gateway GatewayIdentity) (GatewayCapabilities, error) {
	caps, ok := gateway.(GatewayCapabilities)
	if !ok {
		return nil, ErrGatewayNotSupported
	}
	return caps, nil
}

// IsGatewaySync checks if the gateway implements the GatewaySync interface.
func IsGatewaySync(gateway GatewayIdentity) bool {
	_, ok := gateway.(GatewaySync)
	return ok
}

// AsGatewaySync attempts to cast the gateway to GatewaySync.
// Returns nil and an error if the gateway does not implement GatewaySync.
func AsGatewaySync(gateway GatewayIdentity) (GatewaySync, error) {
	sync, ok := gateway.(GatewaySync)
	if !ok {
		return nil, ErrGatewayNotSupported
	}
	return sync, nil
}

// IsSubscriptionManager checks if the gateway implements the SubscriptionManager interface.
func IsSubscriptionManager(gateway GatewayIdentity) bool {
	_, ok := gateway.(SubscriptionManager)
	return ok
}

// AsSubscriptionManager attempts to cast the gateway to SubscriptionManager.
// Returns nil and an error if the gateway does not implement SubscriptionManager.
func AsSubscriptionManager(gateway GatewayIdentity) (SubscriptionManager, error) {
	manager, ok := gateway.(SubscriptionManager)
	if !ok {
		return nil, ErrGatewayNotSupported
	}
	return manager, nil
}

// IsCancellationExecutor checks if a gateway implements the CancellationExecutor interface.
func IsCancellationExecutor(gateway GatewayIdentity) bool {
	_, ok := gateway.(CancellationExecutor)
	return ok
}

// AsCancellationExecutor attempts to cast a gateway to CancellationExecutor.
// Returns nil and an error if the gateway does not implement CancellationExecutor.
func AsCancellationExecutor(gateway GatewayIdentity) (CancellationExecutor, error) {
	executor, ok := gateway.(CancellationExecutor)
	if !ok {
		return nil, ErrGatewayNotSupported
	}
	return executor, nil
}

// IsPlanChangeExecutor checks if a gateway implements the PlanChangeExecutor interface.
func IsPlanChangeExecutor(gateway GatewayIdentity) bool {
	_, ok := gateway.(PlanChangeExecutor)
	return ok
}

// AsPlanChangeExecutor attempts to cast a gateway to PlanChangeExecutor.
// Returns nil and an error if the gateway does not implement PlanChangeExecutor.
func AsPlanChangeExecutor(gateway GatewayIdentity) (PlanChangeExecutor, error) {
	executor, ok := gateway.(PlanChangeExecutor)
	if !ok {
		return nil, ErrGatewayNotSupported
	}
	return executor, nil
}

// IsPauseResumeExecutor checks if a gateway implements the PauseResumeExecutor interface.
func IsPauseResumeExecutor(gateway GatewayIdentity) bool {
	_, ok := gateway.(PauseResumeExecutor)
	return ok
}

// AsPauseResumeExecutor attempts to cast a gateway to PauseResumeExecutor.
// Returns nil and an error if the gateway does not implement PauseResumeExecutor.
func AsPauseResumeExecutor(gateway GatewayIdentity) (PauseResumeExecutor, error) {
	executor, ok := gateway.(PauseResumeExecutor)
	if !ok {
		return nil, ErrGatewayNotSupported
	}
	return executor, nil
}

// IsSubscriptionExecutor checks if a gateway implements the full SubscriptionExecutor interface.
func IsSubscriptionExecutor(gateway GatewayIdentity) bool {
	_, ok := gateway.(SubscriptionExecutor)
	return ok
}

// AsSubscriptionExecutor attempts to cast a gateway to SubscriptionExecutor.
// Returns nil and an error if the gateway does not implement SubscriptionExecutor.
func AsSubscriptionExecutor(gateway GatewayIdentity) (SubscriptionExecutor, error) {
	executor, ok := gateway.(SubscriptionExecutor)
	if !ok {
		return nil, ErrGatewayNotSupported
	}
	return executor, nil
}

// SessionStatusProvider is an optional interface for gateways that support
// retrieving session status after embedded checkout completion.
// Used by return pages to verify payment status.
type SessionStatusProvider interface {
	// GetSessionStatus retrieves the current status of a checkout session
	// sessionID: the gateway's session identifier (e.g., Stripe's cs_xxx)
	// Returns: status ('open', 'complete', 'expired'), customer email, or error
	GetSessionStatus(ctx context.Context, sessionID string) (*SessionStatus, error)
}

// SessionStatus represents the status of a checkout session
type SessionStatus struct {
	Status        string // 'open', 'complete', 'expired'
	CustomerEmail string // Customer email if available
	SessionID     string // Gateway session ID
}

// IsSessionStatusProvider checks if the gateway implements the SessionStatusProvider interface.
func IsSessionStatusProvider(gateway GatewayIdentity) bool {
	_, ok := gateway.(SessionStatusProvider)
	return ok
}

// AsSessionStatusProvider attempts to cast the gateway to SessionStatusProvider.
// Returns nil and an error if the gateway does not implement SessionStatusProvider.
func AsSessionStatusProvider(gateway GatewayIdentity) (SessionStatusProvider, error) {
	provider, ok := gateway.(SessionStatusProvider)
	if !ok {
		return nil, ErrGatewayNotSupported
	}
	return provider, nil
}


