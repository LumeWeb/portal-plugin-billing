package stripe

import (
	"embed"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/stripe/stripe-go/v83"
	"github.com/stripe/stripe-go/v83/webhook"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/internal/gateway"
	"go.lumeweb.com/portal/core"
	quotaCore "go.lumeweb.com/portal-plugin-quota/core"
	"go.lumeweb.com/portal/db/models"
	"go.uber.org/zap"
)

//go:embed assets/*.svg
var gatewayLogoFiles embed.FS

const (
	GatewayID                         = "stripe"
	EventTypeCheckoutSessionCompleted = "checkout.session.completed"
	EventTypeSubscriptionDeleted      = "customer.subscription.deleted"
	EventTypeSubscriptionPaused       = "customer.subscription.paused"
	EventTypeSubscriptionResumed      = "customer.subscription.resumed"
	EventTypeSubscriptionUpdated      = "customer.subscription.updated"
	PlanIDMetadataKey                 = "plan_id"
	UserIDMetadataKey                 = "user_id"
	CustomerIDPrefix                  = "cus_"
)

// Setup creates and configures a Stripe gateway if webhook secret is configured.
// Returns a log message (empty if not configured), the gateway instance (nil if not configured), and an error.
func Setup(opts pluginCore.GatewaySetupOptions, webhookSecret string, secretKey string) (string, pluginCore.PaymentGateway, error) {
	if webhookSecret == "" {
		return "", nil, nil
	}
	if secretKey == "" {
		return "", nil, fmt.Errorf("secret key is required when webhook secret is configured")
	}

	gw := New(opts.Logger, webhookSecret, secretKey, nil, nil, opts.BillingSvc, opts.PricingSvc)
	return "Stripe gateway registered successfully", gw, nil
}

// SubscriptionRetriever is an interface for retrieving Stripe subscriptions.
// It provides a way to fetch subscription details from Stripe by ID, allowing
// for both real API calls and mock implementations for testing.
//
// The interface is designed to abstract the Stripe API client's subscription
// retrieval functionality, making it easier to test webhook handlers without
// making actual API calls.
type SubscriptionRetriever interface {
	// Get retrieves a Stripe subscription by its ID.
	//
	// Parameters:
	// - ctx: The context for the request
	// - id: The Stripe subscription ID to retrieve
	// - params: Optional parameters for the subscription retrieval
	//
	// Returns:
	// - *stripe.Subscription: The retrieved subscription object
	// - error: Any error that occurred during retrieval
	Get(ctx context.Context, id string, params *stripe.SubscriptionRetrieveParams) (*stripe.Subscription, error)
}

// subscriptionRetriever implements SubscriptionRetriever using the actual Stripe API.
// This implementation makes real calls to the Stripe API to retrieve subscription information.
type subscriptionRetriever struct {
	client Client
}

// Get retrieves a Stripe subscription by its ID using the Stripe API client.
// It delegates directly to the Stripe client's subscription retrieval method.
func (r *subscriptionRetriever) Get(ctx context.Context, id string, params *stripe.SubscriptionRetrieveParams) (*stripe.Subscription, error) {
	ctx, span := core.TraceMethod(ctx, "subscriptionRetriever.Get")
	defer span.End()

	return r.client.V1Subscriptions().Retrieve(ctx, id, params)
}

// Client defines the interface for Stripe client operations
type Client interface {
	V1Products() Products
	V1Prices() Prices
	V1BillingPortalConfigurations() BillingPortalConfigurations
	V1BillingPortalSessions() BillingPortalSessions
	V1Customers() Customers
	V1Subscriptions() Subscriptions
	V1CheckoutSessions() CheckoutSessions
}

// CheckoutSessions defines the interface for checkout session operations
type CheckoutSessions interface {
	Create(ctx context.Context, params *stripe.CheckoutSessionCreateParams) (*stripe.CheckoutSession, error)
	Retrieve(ctx context.Context, id string, params *stripe.CheckoutSessionRetrieveParams) (*stripe.CheckoutSession, error)
}

// BillingPortalSessions defines the interface for billing portal session operations
type BillingPortalSessions interface {
	Create(ctx context.Context, params *stripe.BillingPortalSessionCreateParams) (*stripe.BillingPortalSession, error)
}

// Customers defines the interface for customer operations
type Customers interface {
	Create(ctx context.Context, params *stripe.CustomerCreateParams) (*stripe.Customer, error)
	Retrieve(ctx context.Context, id string, params *stripe.CustomerRetrieveParams) (*stripe.Customer, error)
	Update(ctx context.Context, id string, params *stripe.CustomerUpdateParams) (*stripe.Customer, error)
}

// Subscriptions defines the interface for subscription operations
type Subscriptions interface {
	Retrieve(ctx context.Context, id string, params *stripe.SubscriptionRetrieveParams) (*stripe.Subscription, error)
}

// Products defines the interface for product operations
type Products interface {
	Create(ctx context.Context, params *stripe.ProductCreateParams) (*stripe.Product, error)
	Retrieve(ctx context.Context, id string, params *stripe.ProductRetrieveParams) (*stripe.Product, error)
}

// Prices defines the interface for price operations
type Prices interface {
	Create(ctx context.Context, params *stripe.PriceCreateParams) (*stripe.Price, error)
	Retrieve(ctx context.Context, id string, params *stripe.PriceRetrieveParams) (*stripe.Price, error)
}

// BillingPortalConfigurations defines the interface for billing portal configuration operations
type BillingPortalConfigurations interface {
	Create(ctx context.Context, params *stripe.BillingPortalConfigurationCreateParams) (*stripe.BillingPortalConfiguration, error)
}
func (w *client) V1CheckoutSessions() CheckoutSessions {
	return w.client.V1CheckoutSessions
}

// client wraps the stripe.Client to implement Client
type client struct {
	client *stripe.Client
}

func (w *client) V1Products() Products {
	return w.client.V1Products
}

func (w *client) V1Prices() Prices {
	return w.client.V1Prices
}

func (w *client) V1BillingPortalConfigurations() BillingPortalConfigurations {
	return w.client.V1BillingPortalConfigurations
}

func (w *client) V1BillingPortalSessions() BillingPortalSessions {
	return w.client.V1BillingPortalSessions
}

func (w *client) V1Customers() Customers {
	return w.client.V1Customers
}

func (w *client) V1Subscriptions() Subscriptions {
	return w.client.V1Subscriptions
}

// StripeGateway implements the PaymentGateway interface for Stripe
type StripeGateway struct {
	logger          *core.Logger
	endpointSecret  string
	secretKey       string
	stripeClient    Client
	quota           quotaCore.QuotaService
	users           core.UserService
	billing         pluginCore.BillingService
	pricing         pluginCore.PricingService
	subService      SubscriptionRetriever
	customerService CustomerRetriever
	fs              fs.FS // filesystem for logo files, nil uses embedded files
}

// newGateway is the internal constructor that creates a StripeGateway instance
// with a custom filesystem
func newGateway(logger *core.Logger, endpointSecret string, secretKey string, quota quotaCore.QuotaService, users core.UserService, billing pluginCore.BillingService, pricing pluginCore.PricingService, fs fs.FS) *StripeGateway {
	stripeClient := &client{client: stripe.NewClient(secretKey)}

	gateway := &StripeGateway{
		logger:         logger,
		endpointSecret: endpointSecret,
		secretKey:      secretKey,
		stripeClient:   stripeClient,
		quota:          quota,
		users:          users,
		billing:        billing,
		pricing:        pricing,
		fs:             fs,
	}

	gateway.subService = gateway.subscriptionRetriever()
	gateway.customerService = gateway.customerRetriever()

	return gateway
}

// New creates a StripeGateway instance with the default embedded filesystem
func New(logger *core.Logger, endpointSecret string, secretKey string, quota quotaCore.QuotaService, users core.UserService, billing pluginCore.BillingService, pricing pluginCore.PricingService) *StripeGateway {
	return newGateway(logger, endpointSecret, secretKey, quota, users, billing, pricing, gatewayLogoFiles)
}

// NewWithFS creates a StripeGateway instance with a custom filesystem for testing
func NewWithFS(logger *core.Logger, endpointSecret string, secretKey string, quota quotaCore.QuotaService, users core.UserService, billing pluginCore.BillingService, pricing pluginCore.PricingService, fs fs.FS) *StripeGateway {
	return newGateway(logger, endpointSecret, secretKey, quota, users, billing, pricing, fs)
}

// customerRetriever returns a customer retriever instance
func (g *StripeGateway) customerRetriever() CustomerRetriever {
	return &customerRetriever{client: g.stripeClient}
}

// subscriptionRetriever returns a subscription retriever instance
func (g *StripeGateway) subscriptionRetriever() SubscriptionRetriever {
	return &subscriptionRetriever{client: g.stripeClient}
}

func (g *StripeGateway) ID(ctx context.Context) string {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.ID")
	defer span.End()

	return GatewayID
}

func (g *StripeGateway) SignatureHeader(ctx context.Context) string {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.SignatureHeader")
	defer span.End()

	return "Stripe-Signature"
}

func (g *StripeGateway) ExtractEventID(ctx context.Context, payload []byte) (string, error) {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.ExtractEventID")
	defer span.End()

	var event stripe.Event
	if err := json.Unmarshal(payload, &event); err != nil {
		return "", err
	}

	// Always return event.ID as the primary identifier if it's not empty
	if event.ID != "" {
		return event.ID, nil
	}

	// Only fall back to IdempotencyKey if event.ID is empty
	if event.Request != nil && event.Request.IdempotencyKey != "" {
		return event.Request.IdempotencyKey, nil
	}

	return "", fmt.Errorf("no event ID found in payload")
}

func (g *StripeGateway) ExtractEventType(ctx context.Context, payload []byte) (string, error) {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.ExtractEventType")
	defer span.End()

	var event stripe.Event
	if err := json.Unmarshal(payload, &event); err != nil {
		return "", err
	}
	return string(event.Type), nil
}

func (g *StripeGateway) GetCustomerPortalURL(ctx context.Context, userID uint, returnUrl string) (string, error) {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.GetCustomerPortalURL")
	defer span.End()

	return core.MetricTrackResult(
		nil,
		CustomerPortalCreated.WithLabelValues(LabelStatusError),
		func() (string, error) {
			// Get the subscriber for this user and gateway
			subscriber, err := g.billing.GetActiveSubscription(ctx, userID)
			if err != nil {
				return "", fmt.Errorf("failed to get active subscription: %w", err)
			}
			if subscriber == nil || subscriber.GatewayType != GatewayID {
				return "", fmt.Errorf("no active stripe subscription found for user %d", userID)
			}

			// Defensive check: ensure ExternalID is a valid Stripe customer ID
			if subscriber.ExternalID == "" {
				return "", fmt.Errorf("subscriber ExternalID is empty")
			}
			if !strings.HasPrefix(subscriber.ExternalID, CustomerIDPrefix) {
				return "", fmt.Errorf("invalid ExternalID: must be a Stripe customer ID starting with '%s'", CustomerIDPrefix)
			}

			// Create a billing portal session
			params := &stripe.BillingPortalSessionCreateParams{
				Customer:  stripe.String(subscriber.ExternalID),
				ReturnURL: stripe.String(returnUrl),
			}

			// Set portal configuration if available for the user's plan
			if subscriber.PlanID != nil && g.pricing != nil {
				mapping, err := g.pricing.GetGatewayProductMapping(ctx, *subscriber.PlanID, GatewayID)
				if err == nil && mapping != nil && mapping.PortalConfigurationID != nil {
					params.Configuration = stripe.String(*mapping.PortalConfigurationID)
					g.logger.Debug("using plan-specific portal configuration",
						zap.Uint("plan_id", *subscriber.PlanID),
						zap.String("config_id", *mapping.PortalConfigurationID),
						zap.Uint("user_id", userID))
				}
			}

			sess, err := g.stripeClient.V1BillingPortalSessions().Create(ctx, params)
			if err != nil {
				return "", fmt.Errorf("failed to create billing portal session: %w", err)
			}

			return sess.URL, nil
		},
	)
}

func (g *StripeGateway) ValidateWebhook(ctx context.Context, signature string, payload []byte) error {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.ValidateWebhook")
	defer span.End()

	_, err := webhook.ConstructEvent(payload, signature, g.endpointSecret)
	return err
}

func (g *StripeGateway) HandleWebhook(ctx context.Context, payload []byte) error {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.HandleWebhook")
	defer span.End()

	var event stripe.Event
	if err := json.Unmarshal(payload, &event); err != nil {
		return err
	}

	switch event.Type {
	case EventTypeCheckoutSessionCompleted:
		return g.handleCheckoutSessionCompleted(ctx, event)
	case EventTypeSubscriptionResumed:
		return g.handleSubscriptionActivated(ctx, event)
	case EventTypeSubscriptionDeleted, EventTypeSubscriptionPaused:
		return g.handleSubscriptionDeactivated(ctx, event)
	case EventTypeSubscriptionUpdated:
		return g.handleSubscriptionUpdated(ctx, event)
	default:
		return nil // Ignore all other event types
	}
}

func (g *StripeGateway) handleSubscriptionActivated(ctx context.Context, event stripe.Event) error {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.handleSubscriptionActivated")
	defer span.End()

	return g.handleSubscriptionEvent(ctx, event, g.activateSubscription)
}

func (g *StripeGateway) handleSubscriptionDeactivated(ctx context.Context, event stripe.Event) error {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.handleSubscriptionDeactivated")
	defer span.End()

	return g.handleSubscriptionEvent(ctx, event, g.deactivateSubscription)
}

func (g *StripeGateway) handleSubscriptionUpdated(ctx context.Context, event stripe.Event) error {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.handleSubscriptionUpdated")
	defer span.End()

	return g.handleSubscriptionEvent(ctx, event, g.handleSubscriptionUpdatedEvent)
}

func (g *StripeGateway) handleSubscriptionUpdatedEvent(ctx context.Context, userID uint, subscription *stripe.Subscription, event stripe.Event) error {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.handleSubscriptionUpdatedEvent")
	defer span.End()

	return core.MetricTrack(
		nil,
		SubscriptionUpdated.WithLabelValues(LabelStatusError),
		func() error {
			// Check if this is a cancellation request
			if g.isCancellationRequest(subscription) {
				g.logger.Debug("subscription cancellation request received - ignoring until deletion event",
					zap.Uint("user_id", userID),
					zap.String("subscription_id", subscription.ID),
					zap.String("event_id", event.ID),
					zap.Time("cancel_at", time.Unix(subscription.CancelAt, 0)))

				// Don't make any changes for cancellation requests - wait for the actual deletion event
				return nil
			}

			// Check if the subscription has a plan
			planID, hasPlan, err := findPlanIDFromSubscription(subscription)
			if err != nil {
				return err
			}

			// If no plan is found, treat as deactivation
			if !hasPlan {
				g.logger.Warn("subscription updated but product metadata missing plan_id",
					zap.String("subscription_id", subscription.ID),
					zap.String("event_id", event.ID))

				return g.deactivateSubscription(ctx, userID, subscription, event)
			}

			// If plan is found, treat as activation
			return g.activateSubscriptionWithPlanID(ctx, userID, subscription, event, planID)
		},
	)
}

func (g *StripeGateway) SetQuota(quota quotaCore.QuotaService) {
	g.quota = quota
}

// GetSubscriptionRetriever returns the subscription retriever for testing purposes
func (g *StripeGateway) GetSubscriptionRetriever() SubscriptionRetriever {
	return g.subService
}

// GetCustomerRetriever returns the customer retriever for testing purposes
func (g *StripeGateway) GetCustomerRetriever() CustomerRetriever {
	return g.customerService
}

// GetStripeClient returns the Stripe client for testing purposes
func (g *StripeGateway) GetStripeClient() Client {
	return g.stripeClient
}

// Helper function to parse user ID from customer metadata
func parseUserIDFromCustomer(customer *stripe.Customer) (uint, error) {
	if customer == nil {
		return 0, fmt.Errorf("customer is nil")
	}
	return parseUserIDFromMetadata(customer.Metadata, "customer")
}

// CustomerRetriever is an interface for retrieving Stripe customers.
// It provides a way to fetch customer details from Stripe by ID, allowing
// for both real API calls and mock implementations for testing.
type CustomerRetriever interface {
	// Get retrieves a Stripe customer by its ID.
	//
	// Parameters:
	// - ctx: The context for the request
	// - id: The Stripe customer ID to retrieve
	// - params: Optional parameters for the customer retrieval
	//
	// Returns:
	// - *stripe.Customer: The retrieved customer object
	// - error: Any error that occurred during retrieval
	Get(ctx context.Context, id string, params *stripe.CustomerRetrieveParams) (*stripe.Customer, error)
}

// customerRetriever implements CustomerRetriever using the actual Stripe API.
// This implementation makes real calls to the Stripe API to retrieve customer information.
type customerRetriever struct {
	client Client
}

// Get retrieves a Stripe customer by its ID using the Stripe API client.
// It delegates directly to the Stripe client's customer retrieval method.
func (r *customerRetriever) Get(ctx context.Context, id string, params *stripe.CustomerRetrieveParams) (*stripe.Customer, error) {
	ctx, span := core.TraceMethod(ctx, "customerRetriever.Get")
	defer span.End()

	return r.client.V1Customers().Retrieve(ctx, id, params)
}

// parseUserIDFromCustomerWithFallback attempts to parse user ID from customer metadata,
// and if that fails, fetches the customer from Stripe API and tries again.
func (g *StripeGateway) parseUserIDFromCustomerWithFallback(ctx context.Context, customerID string) (uint, error) {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.parseUserIDFromCustomerWithFallback")
	defer span.End()

	// Fetch customer directly from Stripe API using injected customer service
	customer, err := g.customerService.Get(ctx, customerID, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch customer %s: %w", customerID, err)
	}

	return parseUserIDFromCustomer(customer)
}

// Helper function to parse user ID from any metadata map
func parseUserIDFromMetadata(meta map[string]string, source string) (uint, error) {
	userID := ""
	if meta != nil {
		userID = meta[UserIDMetadataKey]
	}
	if userID == "" {
		return 0, fmt.Errorf("%s metadata missing user_id", source)
	}

	userIDUint, err := strconv.ParseUint(userID, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid user_id format in %s metadata: %w", source, err)
	}

	return uint(userIDUint), nil
}

// Helper function to extract plan ID from product metadata
func extractPlanIDFromProduct(product *stripe.Product) (uint, bool, error) {
	if product == nil || product.Metadata == nil {
		return 0, false, nil
	}

	planID := product.Metadata[PlanIDMetadataKey]
	if planID == "" {
		return 0, false, nil
	}

	planIDUint, err := strconv.ParseUint(planID, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("invalid plan_id format in product metadata: %w", err)
	}

	return uint(planIDUint), true, nil
}

// isCancellationRequest checks if a subscription update represents a cancellation request
// This happens when a user requests cancellation through the Stripe customer portal
// but the subscription remains active until the end of the billing period
func (g *StripeGateway) isCancellationRequest(subscription *stripe.Subscription) bool {
	// Check if subscription is scheduled for cancellation at a specific time
	if subscription.CancelAt > 0 {
		return true
	}

	// Check if cancellation details indicate a cancellation was requested
	if subscription.CancellationDetails != nil && subscription.CancellationDetails.Reason == "cancellation_requested" {
		return true
	}

	return false
}

// extractUserIDFromSubscription extracts user ID from subscription customer metadata with fallback
func (g *StripeGateway) extractUserIDFromSubscription(ctx context.Context, subscription *stripe.Subscription) (uint, error) {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.extractUserIDFromSubscription")
	defer span.End()

	userID, err := parseUserIDFromCustomer(subscription.Customer)
	if err != nil {
		// Try fallback if customer metadata is missing
		if subscription.Customer != nil && subscription.Customer.ID != "" {
			// First try to look up user_id from our database using external_id (customer_id)
			if g.billing != nil {
				subscriber, err := g.billing.GetSubscriberByExternalID(ctx, subscription.Customer.ID, GatewayID)
				if err == nil && subscriber != nil {
					g.logger.Debug("found user_id from billing_subscribers table",
						zap.String("customer_id", subscription.Customer.ID),
						zap.Uint("user_id", subscriber.UserID),
						zap.String("subscription_id", subscription.ID))
					return subscriber.UserID, nil
				}
				if err != nil {
					g.logger.Warn("failed to look up subscriber by gateway id; falling back to Stripe",
						zap.String("customer_id", subscription.Customer.ID),
						zap.String("subscription_id", subscription.ID),
						zap.Error(err))
				}
			}

			// Final fallback: try to fetch from Stripe API
			userID, err = g.parseUserIDFromCustomerWithFallback(ctx, subscription.Customer.ID)
			if err != nil {
				// Log detailed error for debugging but don't fail the webhook
				g.logger.Info("customer metadata missing user_id - webhook ignored (customer may need manual metadata update)",
					zap.String("customer_id", subscription.Customer.ID),
					zap.String("subscription_id", subscription.ID),
					zap.String("event_type", "subscription_processing"),
					zap.Error(err))
				return 0, nil // Return 0 to indicate no valid user ID, but don't fail
			}
		} else {
			// Log error for customers without ID
			g.logger.Error("subscription has no customer ID - webhook ignored",
				zap.String("subscription_id", subscription.ID))
			return 0, nil // Return 0 to indicate no valid user ID, but don't fail
		}
	}
	return userID, nil
}

// handleSubscriptionEvent is a generic function to handle subscription events
func (g *StripeGateway) handleSubscriptionEvent(ctx context.Context, event stripe.Event, handler func(ctx context.Context, userID uint, subscription *stripe.Subscription, event stripe.Event) error) error {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.handleSubscriptionEvent")
	defer span.End()

	subscription, err := g.getExpandedSubscriptionFromEvent(ctx, event)
	if err != nil {
		return err
	}

	userID, err := g.extractUserIDFromSubscription(ctx, subscription)
	if err != nil {
		return err
	}

	// If userID is 0, it means we couldn't extract a valid user ID
	// In this case, we should ignore the event rather than fail
	if userID == 0 {
		g.logger.Debug("ignoring subscription event due to missing user ID",
			zap.String("event_id", event.ID),
			zap.String("event_type", string(event.Type)),
			zap.String("subscription_id", subscription.ID))
		return nil
	}

	return handler(ctx, userID, subscription, event)
}

// getExpandedSubscription retrieves a subscription with expanded product data
func (g *StripeGateway) getExpandedSubscription(ctx context.Context, subscriptionID string) (*stripe.Subscription, error) {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.getExpandedSubscription")
	defer span.End()

	params := &stripe.SubscriptionRetrieveParams{}
	params.AddExpand("items.data.price.product")
	return g.subService.Get(ctx, subscriptionID, params)
}

// getExpandedSubscriptionFromEvent extracts the subscription ID from a Stripe event and retrieves the expanded subscription
func (g *StripeGateway) getExpandedSubscriptionFromEvent(ctx context.Context, event stripe.Event) (*stripe.Subscription, error) {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.getExpandedSubscriptionFromEvent")
	defer span.End()

	subscriptionID, err := g.extractSubscriptionIDFromEvent(event)
	if err != nil {
		return nil, err
	}

	subscription, err := g.getExpandedSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch expanded subscription: %w", err)
	}

	return subscription, nil
}

// extractSubscriptionIDFromEvent extracts the subscription ID from a Stripe event
func (g *StripeGateway) extractSubscriptionIDFromEvent(event stripe.Event) (string, error) {
	if event.Data == nil || len(event.Data.Raw) == 0 {
		return "", fmt.Errorf("event data is nil or empty")
	}

	var eventData struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(event.Data.Raw, &eventData); err != nil {
		return "", fmt.Errorf("failed to unmarshal event data: %w", err)
	}

	if eventData.ID == "" {
		return "", fmt.Errorf("subscription ID not found in event data")
	}

	return eventData.ID, nil
}

// Helper function to find plan ID from subscription using expanded product data
func findPlanIDFromSubscription(sub *stripe.Subscription) (uint, bool, error) {
	if sub.Items == nil || len(sub.Items.Data) == 0 {
		return 0, false, nil
	}

	for _, item := range sub.Items.Data {
		if item == nil || item.Price == nil || item.Price.Product == nil {
			continue
		}

		planID, found, err := extractPlanIDFromProduct(item.Price.Product)
		if err != nil {
			return 0, false, err
		}
		if found {
			return planID, true, nil
		}
	}

	return 0, false, nil
}

// handleCheckoutSessionCompleted processes a completed checkout session
func (g *StripeGateway) handleCheckoutSessionCompleted(ctx context.Context, event stripe.Event) error {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.handleCheckoutSessionCompleted")
	defer span.End()

	return core.MetricTrack(
		nil,
		CheckoutCompleted.WithLabelValues(LabelStatusError),
		func() error {
			if event.Data == nil {
				return fmt.Errorf("event data is nil")
			}

			if len(event.Data.Raw) == 0 {
				return fmt.Errorf("event data raw payload is empty")
			}

			var session stripe.CheckoutSession
			if err := json.Unmarshal(event.Data.Raw, &session); err != nil {
				return err
			}

			// Verify that the session mode is "subscription"
			if session.Mode != "subscription" {
				return fmt.Errorf("checkout session mode is not 'subscription': got '%s'", session.Mode)
			}

			// Parse user ID from client_reference_id
			if session.ClientReferenceID == "" {
				return fmt.Errorf("checkout session missing client_reference_id")
			}

			userID, err := strconv.ParseUint(session.ClientReferenceID, 10, 64)
			if err != nil {
				return fmt.Errorf("invalid client_reference_id format: %w", err)
			}

			userIDUint := uint(userID)

			// Verify that session.Subscription is non-nil and has a valid ID
			if session.Subscription == nil {
				return fmt.Errorf("checkout session missing subscription object")
			}

			if session.Subscription.ID == "" {
				return fmt.Errorf("checkout session subscription missing ID")
			}

			// Fetch subscription data using Stripe API with expanded product data
			subscription, err := g.getExpandedSubscription(ctx, session.Subscription.ID)
			if err != nil {
				return fmt.Errorf("failed to fetch subscription: %w", err)
			}

			return g.activateSubscription(ctx, userIDUint, subscription, event)
		},
	)
}

// activateSubscription is a common function to handle subscription activation
// for checkout.session.completed and customer.subscription.resumed events
func (g *StripeGateway) activateSubscription(ctx context.Context, userID uint, subscription *stripe.Subscription, event stripe.Event) error {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.activateSubscription")
	defer span.End()

	return core.MetricTrack(
		nil,
		SubscriptionActivated.WithLabelValues(LabelStatusError),
		func() error {
			// Validate services
			if err := g.validateServices(); err != nil {
				return err
			}

			// Get and validate user exists
			if _, err := g.getUser(ctx, userID); err != nil {
				return err
			}

			planID, hasPlan, err := findPlanIDFromSubscription(subscription)
			if err != nil {
				return err
			}

			if !hasPlan {
				g.logger.Warn("subscription activated but product metadata missing plan_id",
					zap.Uint("user_id", userID),
					zap.String("subscription_id", subscription.ID),
					zap.String("event_id", event.ID))
				return nil
			}

			return g.activateSubscriptionWithPlanID(ctx, userID, subscription, event, planID)
		},
	)
}

// activateSubscriptionWithPlanID handles subscription activation with a known plan ID.
// The planID parameter is the PricingPlan.ID, which must be looked up to get the QuotaPlanID.
func (g *StripeGateway) activateSubscriptionWithPlanID(ctx context.Context, userID uint, subscription *stripe.Subscription, event stripe.Event, pricingPlanID uint) error {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.activateSubscriptionWithPlanID")
	defer span.End()

	// Validate services
	if err := g.validateServices(); err != nil {
		return err
	}

	// Get and validate user
	user, err := g.getUser(ctx, userID)
	if err != nil {
		return err
	}

	// Look up PricingPlan to get QuotaPlanID
	pricingPlan, err := g.pricing.GetPricingPlan(ctx, pricingPlanID)
	if err != nil {
		return fmt.Errorf("pricing plan with ID %d not found: %w", pricingPlanID, err)
	}
	if pricingPlan == nil {
		return fmt.Errorf("pricing plan with ID %d not found", pricingPlanID)
	}

	// If pricing plan has a quota plan assigned, assign user to it
	if pricingPlan.QuotaPlanID != nil {
		// Validate quota plan exists
		quotaPlan, err := g.quota.GetQuotaPlan(ctx, *pricingPlan.QuotaPlanID)
		if err != nil {
			return fmt.Errorf("quota plan with ID %d not found: %w", *pricingPlan.QuotaPlanID, err)
		}
		if quotaPlan == nil {
			return fmt.Errorf("quota plan with ID %d not found", *pricingPlan.QuotaPlanID)
		}

		// Assign user to quota plan
		if err := g.quota.AssignUserToPlan(ctx, user.ID, *pricingPlan.QuotaPlanID); err != nil {
			return fmt.Errorf("failed to assign user to quota plan %d: %w", *pricingPlan.QuotaPlanID, err)
		}

		g.logger.Debug("assigned user to quota plan",
			zap.Uint("user_id", userID),
			zap.Uint("quota_plan_id", *pricingPlan.QuotaPlanID),
			zap.Uint("pricing_plan_id", pricingPlanID))
	} else {
		g.logger.Debug("pricing plan has no quota plan assignment",
			zap.Uint("user_id", userID),
			zap.Uint("pricing_plan_id", pricingPlanID))
	}

	// Track subscriber in billing service with PricingPlan ID
	if subscription.Customer == nil {
		return fmt.Errorf("subscription missing customer id")
	}

	if subscription.Customer.ID == "" {
		return fmt.Errorf("subscription missing customer id")
	}

	if err := g.trackSubscriber(ctx, user.ID, subscription.Customer.ID, subscription.ID, true, &pricingPlanID); err != nil {
		g.logger.Error("failed to track subscriber",
			zap.Error(err),
			zap.Uint("user_id", userID),
			zap.String("customer_id", subscription.Customer.ID),
			zap.String("subscription_id", subscription.ID))
	}

	// Update customer metadata with user ID
	g.updateCustomerMetadata(ctx, g.secretKey, subscription.Customer.ID, userID)

	g.logger.Debug("subscription activated",
		zap.Uint("user_id", userID),
		zap.String("subscription_id", subscription.ID),
		zap.Uint("pricing_plan_id", pricingPlanID),
		zap.String("event_id", event.ID),
		zap.Uint("user_db_id", user.ID))

	return nil
}

// deactivateSubscription is a common function to handle subscription deactivation
// for customer.subscription.deleted and customer.subscription.paused events
func (g *StripeGateway) deactivateSubscription(ctx context.Context, userID uint, subscription *stripe.Subscription, event stripe.Event) error {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.deactivateSubscription")
	defer span.End()

	return core.MetricTrack(
		nil,
		SubscriptionDeactivated.WithLabelValues(LabelStatusError),
		func() error {
			// Validate services
			if err := g.validateServices(); err != nil {
				return err
			}

			// Get and validate user
			user, err := g.getUser(ctx, userID)
			if err != nil {
				return err
			}

			// Remove user from their current plan
			if err := g.quota.RemoveUserFromPlan(ctx, user.ID); err != nil {
				return fmt.Errorf("failed to remove user from plan: %w", err)
			}

			// Check if subscription.Customer is nil before accessing it
			if subscription.Customer == nil {
				g.logger.Error("subscription customer is nil",
					zap.Uint("user_id", userID),
					zap.String("subscription_id", subscription.ID),
					zap.String("event_id", event.ID))
				return fmt.Errorf("subscription customer is nil for subscription %s", subscription.ID)
			}

			// Update subscriber status in billing service
			if err := g.trackSubscriber(ctx, user.ID, subscription.Customer.ID, "", false, nil); err != nil {
				g.logger.Error("failed to deactivate subscriber",
					zap.Error(err),
					zap.Uint("user_id", userID),
					zap.String("customer_id", subscription.Customer.ID))
			}

			g.logger.Debug("subscription deactivated - removed quota plan",
				zap.Uint("user_id", userID),
				zap.String("subscription_id", subscription.ID),
				zap.String("customer_id", subscription.Customer.ID),
				zap.String("event_id", event.ID),
				zap.Uint("user_db_id", user.ID))

			return nil
		},
	)
}

// validateServices checks if required services are available
func (g *StripeGateway) validateServices() error {
	if g.users == nil {
		return fmt.Errorf("user service not configured")
	}
	if g.quota == nil {
		return fmt.Errorf("quota service not configured")
	}
	return nil
}

// getUser retrieves and validates a user exists
func (g *StripeGateway) getUser(ctx context.Context, userID uint) (*models.User, error) {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.getUser")
	defer span.End()

	exists, user, err := g.users.AccountExists(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to check user existence: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("user with ID %d not found", userID)
	}
	return user, nil
}

// trackSubscriber handles subscriber tracking in the billing service
func (g *StripeGateway) trackSubscriber(ctx context.Context, userID uint, externalID string, subscriptionID string, isActive bool, planID *uint) error {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.trackSubscriber")
	defer span.End()

	if g.billing == nil {
		return nil // No billing service configured, nothing to track
	}

	if isActive {
		return g.billing.CreateOrUpdateSubscriber(ctx, userID, GatewayID, externalID, subscriptionID, isActive, planID)
	} else {
		return g.billing.DeactivateSubscriber(ctx, userID, GatewayID)
	}
}

// updateCustomerMetadata updates the customer's metadata with the user ID
func (g *StripeGateway) updateCustomerMetadata(ctx context.Context, secretKey string, customerID string, userID uint) {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.updateCustomerMetadata")
	defer span.End()

	// Short-circuit if secret key is empty (for tests/dev)
	if secretKey == "" {
		return
	}

	params := &stripe.CustomerUpdateParams{}
	params.AddMetadata(UserIDMetadataKey, strconv.FormatUint(uint64(userID), 10))

	_, err := g.stripeClient.V1Customers().Update(ctx, customerID, params)
	if err != nil {
		// Best-effort update - log error but don't return it
		g.logger.Warn("failed to update customer metadata",
			zap.String("customer_id", customerID),
			zap.Uint("user_id", userID),
			zap.Error(err))
	}
}

// SetCustomerRetrieverForTesting sets a mock customer retriever for testing purposes.
// This method should only be used in tests and allows injection of a mock
// customer retriever to avoid making actual Stripe API calls.
func (g *StripeGateway) SetCustomerRetrieverForTesting(retriever CustomerRetriever) {
	g.customerService = retriever
}

// ExtractUserIDFromSubscriptionForTesting extracts the user ID from a Stripe subscription.
// This is a test-only method that exposes the internal user ID extraction logic.
// GetCheckoutUI returns UI fragments for Stripe checkout flows
func (g *StripeGateway) GetCheckoutUI(ctx context.Context, userID uint, planID uint) (*pluginCore.CheckoutUIResponse, error) {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.GetCheckoutUI")
	defer span.End()

	return core.MetricTrackResult(
		nil,
		CheckoutSessionCreated.WithLabelValues(LabelStatusError),
		func() (*pluginCore.CheckoutUIResponse, error) {
			// 1. Validate services are available
			if err := g.validateServices(); err != nil {
				return nil, err
			}

			// 2. Get plan details and validate
			plan, err := g.pricing.GetPricingPlan(ctx, planID)
			if err != nil {
				return nil, fmt.Errorf("failed to get pricing plan: %w", err)
			}
			if plan == nil {
				return nil, fmt.Errorf("pricing plan not found")
			}
			if !plan.IsActive {
				return nil, fmt.Errorf("plan is not active")
			}

			// 3. Get Stripe price mapping
			mapping, err := g.pricing.GetGatewayProductMapping(ctx, planID, GatewayID)
			if err != nil {
				return nil, fmt.Errorf("failed to get gateway product mapping: %w", err)
			}
			if mapping == nil || mapping.RemoteMonthlyPriceID == "" {
				return nil, fmt.Errorf("plan not synced with stripe (missing remote price ID)")
			}

			// 4. Get user details
			user, err := g.getUser(ctx, userID)
			if err != nil {
				return nil, fmt.Errorf("failed to get user: %w", err)
			}

			// 5. Get or create Stripe customer
			customerID, err := g.getOrCreateStripeCustomer(ctx, userID, user.Email)
			if err != nil {
				return nil, fmt.Errorf("failed to get/create stripe customer: %w", err)
			}

			// 6. Create checkout session
			params := &stripe.CheckoutSessionCreateParams{
				Mode:    stripe.String(stripe.CheckoutSessionModeSubscription),
				LineItems: []*stripe.CheckoutSessionCreateLineItemParams{
					{
						Price:    stripe.String(mapping.RemoteMonthlyPriceID),
						Quantity: stripe.Int64(1),
					},
				},
				Customer:          stripe.String(customerID),
				ClientReferenceID: stripe.String(strconv.FormatUint(uint64(userID), 10)),
				SuccessURL:        stripe.String(g.getCheckoutSuccessURL()),
				CancelURL:         stripe.String(g.getCheckoutCancelURL()),
			}

			session, err := g.stripeClient.V1CheckoutSessions().Create(ctx, params)
			if err != nil {
				g.logger.Error("failed to create checkout session",
					zap.Error(err),
					zap.Uint("user_id", userID),
					zap.Uint("plan_id", planID))
				return nil, fmt.Errorf("failed to create checkout session: %w", err)
			}

			// 7. Build response with link fragment
			response := &pluginCore.CheckoutUIResponse{
				SessionID: session.ID,
				ExpiresAt: time.Unix(session.ExpiresAt, 0),
				Fragments: []pluginCore.CheckoutUIFragment{
					{
						Type: pluginCore.FragmentTypeLink,
						Link: session.URL,
					},
				},
			}

			g.logger.Debug("checkout session created",
				zap.String("session_id", session.ID),
				zap.Uint("user_id", userID),
				zap.Uint("plan_id", planID),
			)

			return response, nil
		},
	)
}

// getCheckoutSuccessURL returns the success URL for checkout
func (g *StripeGateway) getCheckoutSuccessURL() string {
	return "/billing/checkout/success"
}

// getCheckoutCancelURL returns the cancel URL for checkout
func (g *StripeGateway) getCheckoutCancelURL() string {
	return "/billing/checkout/cancel"
}

// getOrCreateStripeCustomer gets an existing or creates a new Stripe customer
func (g *StripeGateway) getOrCreateStripeCustomer(ctx context.Context, userID uint, email string) (string, error) {
	// Check if customer already exists in subscriber table
	subscriber, err := g.billing.GetActiveSubscriber(ctx, userID, GatewayID)
	if err == nil && subscriber != nil && subscriber.ExternalID != "" {
		return subscriber.ExternalID, nil
	}

	// Create new Stripe customer
	customerParams := &stripe.CustomerCreateParams{
		Email: stripe.String(email),
		Metadata: map[string]string{
			UserIDMetadataKey: strconv.FormatUint(uint64(userID), 10),
		},
	}

	customer, err := g.stripeClient.V1Customers().Create(ctx, customerParams)
	if err != nil {
		return "", fmt.Errorf("failed to create stripe customer: %w", err)
	}

	return customer.ID, nil
}

// GetCustomerPortalMetadata returns metadata for Stripe customer portal
func (g *StripeGateway) GetCustomerPortalMetadata(ctx context.Context, userID uint) (map[string]interface{}, error) {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.GetCustomerPortalMetadata")
	defer span.End()

	// Return empty metadata for now
	// Future: Could include portal configuration options
	return map[string]any{}, nil
}

// SupportsProductSync returns true - Stripe supports product/price synchronization
func (g *StripeGateway) SupportsProductSync() bool {
	return true
}

// SyncPlan synchronizes a pricing plan with Stripe
func (g *StripeGateway) SyncPlan(ctx context.Context, plan *pluginCore.PricingPlanInfo) (*pluginCore.SyncResult, error) {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.SyncPlan")
	defer span.End()

	// Check if pricing service is available
	if g.pricing == nil {
		return &pluginCore.SyncResult{
			Success: false,
		}, fmt.Errorf("pricing service not configured")
	}

	// Get the price line information for this plan
	priceLinePlans, err := g.pricing.GetPriceLinesForPlan(ctx, plan.ID)
	if err != nil {
		return &pluginCore.SyncResult{
			Success: false,
		}, fmt.Errorf("failed to get price lines for plan: %w", err)
	}

	var priceLineID uint
	if len(priceLinePlans) > 0 {
		priceLineID = priceLinePlans[0].PriceLineID
		g.logger.Debug("found plan in price line",
			zap.Uint("plan_id", plan.ID),
			zap.Uint("price_line_id", priceLineID),
			zap.Int("position", priceLinePlans[0].Position))
	} else {
		g.logger.Info("plan not in any price line, syncing without upgrade/downgrade metadata",
			zap.Uint("plan_id", plan.ID))
	}

	// Get upgrade/downgrade paths if plan is in a price line
	var upgradePlanIDs, downgradePlanIDs []string
	var planPosition int
	if priceLineID > 0 && len(priceLinePlans) > 0 {
		planPosition = priceLinePlans[0].Position
		paths, err := g.pricing.GetUpgradeDowngradePlans(ctx, plan.ID, priceLineID)
		if err != nil {
			g.logger.Warn("failed to get upgrade/downgrade paths",
				zap.Uint("plan_id", plan.ID),
				zap.Uint("price_line_id", priceLineID),
				zap.Error(err))
		} else {
			// Extract plan IDs for metadata
			for _, upgrade := range paths.Upgrades {
				upgradePlanIDs = append(upgradePlanIDs, strconv.FormatUint(uint64(upgrade.ID), 10))
			}
			for _, downgrade := range paths.Downgrades {
				downgradePlanIDs = append(downgradePlanIDs, strconv.FormatUint(uint64(downgrade.ID), 10))
			}
			g.logger.Debug("upgrade/downgrade paths found",
				zap.Uint("plan_id", plan.ID),
				zap.Int("num_upgrades", len(upgradePlanIDs)),
				zap.Int("num_downgrades", len(downgradePlanIDs)))
		}
	}

	// Build product metadata
	metadata := map[string]string{
		PlanIDMetadataKey: strconv.FormatUint(uint64(plan.ID), 10),
	}

	// Add price line position if available
	if planPosition > 0 {
		metadata["price_line_position"] = strconv.Itoa(planPosition)
	}

	// Add upgrade/downgrade paths
	if len(upgradePlanIDs) > 0 {
		metadata["upgrade_path_plan_ids"] = strings.Join(upgradePlanIDs, ",")
	}
	if len(downgradePlanIDs) > 0 {
		metadata["downgrade_path_plan_ids"] = strings.Join(downgradePlanIDs, ",")
	}

	// Create or update Stripe Product
	stripeProduct, err := g.createOrUpdateStripeProduct(ctx, plan, metadata)
	if err != nil {
		return &pluginCore.SyncResult{
			Success: false,
		}, fmt.Errorf("failed to create/update product: %w", err)
	}

	// Create or update monthly price
	var monthlyPriceID string
	if plan.MonthlyPriceUSD != nil {
		monthlyPriceID, err = g.createOrUpdateStripePrice(ctx, plan.MonthlyPriceUSD, plan.Currency, stripeProduct.ID, "monthly")
		if err != nil {
			return &pluginCore.SyncResult{
				Success: false,
			}, fmt.Errorf("failed to create/update monthly price: %w", err)
		}
	}

	// Create or update yearly price
	var yearlyPriceID string
	if plan.YearlyPriceUSD != nil {
		yearlyPriceID, err = g.createOrUpdateStripePrice(ctx, plan.YearlyPriceUSD, plan.Currency, stripeProduct.ID, "yearly")
		if err != nil {
			return &pluginCore.SyncResult{
				Success: false,
			}, fmt.Errorf("failed to create/update yearly price: %w", err)
		}
	}

	g.logger.Info("successfully synced pricing plan to Stripe",
		zap.Uint("plan_id", plan.ID),
		zap.String("stripe_product_id", stripeProduct.ID),
		zap.String("monthly_price_id", monthlyPriceID),
		zap.String("yearly_price_id", yearlyPriceID))

	// Create or update portal configuration if plan has upgrade/downgrade paths
	var portalConfigID string
	if priceLineID > 0 && (len(upgradePlanIDs) > 0 || len(downgradePlanIDs) > 0) {
		priceIDs, err := g.resolvePriceIDsFromPlanIDs(ctx, upgradePlanIDs, downgradePlanIDs)
		if err != nil {
			g.logger.Warn("failed to resolve price IDs for portal configuration",
				zap.Uint("plan_id", plan.ID),
				zap.Error(err))
		} else {
			configID, err := g.createOrUpdatePortalConfiguration(ctx, plan, stripeProduct.ID, priceIDs)
			if err != nil {
				g.logger.Error("failed to create portal configuration",
					zap.Uint("plan_id", plan.ID),
					zap.Error(err))
			} else {
				portalConfigID = configID
				g.logger.Info("created portal configuration",
					zap.Uint("plan_id", plan.ID),
					zap.String("config_id", configID),
					zap.Int("allowed_prices", len(priceIDs)))
			}
		}
	}

	return &pluginCore.SyncResult{
		Success:            true,
		ProductID:          stripeProduct.ID,
		MonthlyPriceID:     monthlyPriceID,
		YearlyPriceID:      yearlyPriceID,
		PortalConfigurationID: portalConfigID,
	}, nil
}

// SupportsPriceUpdates returns true - Stripe supports updating existing prices
func (g *StripeGateway) SupportsPriceUpdates() bool {
	return true
}

// SupportsPlanDeletion returns false - Stripe doesn't support direct product deletion
func (g *StripeGateway) SupportsPlanDeletion() bool {
	return false
}

// RequiredPricingFields returns fields required for Stripe product creation
func (g *StripeGateway) RequiredPricingFields() []string {
	return []string{"name", "amount", "currency"}
}

// GetName returns display name for Stripe gateway
func (g *StripeGateway) GetName(ctx context.Context) string {
	return "Stripe"
}

// GetDescription returns description for Stripe gateway
func (g *StripeGateway) GetDescription(ctx context.Context) string {
	return "Industry-leading payment processor"
}

// GetLogo returns the logo image bytes for Stripe gateway
func (g *StripeGateway) GetLogo(ctx context.Context) ([]byte, error) {
	return gateway.ReadGatewayLogo(GatewayID, gatewayLogoFiles, g.fs)
}

// It first checks the customer metadata for a user_id, then falls back to database lookup.
func (g *StripeGateway) ExtractUserIDFromSubscriptionForTesting(ctx context.Context, subscription *stripe.Subscription) (uint, error) {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.ExtractUserIDFromSubscriptionForTesting")
	defer span.End()

	return g.extractUserIDFromSubscription(ctx, subscription)
}

// createOrUpdateStripeProduct creates or updates a Stripe product for a pricing plan
func (g *StripeGateway) createOrUpdateStripeProduct(ctx context.Context, plan *pluginCore.PricingPlanInfo, metadata map[string]string) (*stripe.Product, error) {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.createOrUpdateStripeProduct")
	defer span.End()

	// For now, we'll always create a new product
	// In a production implementation, you should:
	// 1. Check if a product with this plan_id already exists in gateway_product_mappings
	// 2. Update if exists, or create if not
	// This allows for proper syncing without duplicates

	productParams := &stripe.ProductCreateParams{
		Name:     stripe.String(plan.Name),
		Metadata: metadata,
	}

	if plan.Description != "" {
		productParams.Description = stripe.String(plan.Description)
	}

	// Create the product using the gateway's client
	product, err := g.stripeClient.V1Products().Create(ctx, productParams)
	if err != nil {
		return nil, fmt.Errorf("failed to create Stripe product: %w", err)
	}

	g.logger.Debug("created Stripe product",
		zap.Uint("plan_id", plan.ID),
		zap.String("product_id", product.ID),
		zap.String("product_name", product.Name))

	return product, nil
}

// createOrUpdateStripePrice creates or updates a Stripe price for a pricing plan
func (g *StripeGateway) createOrUpdateStripePrice(ctx context.Context, amount *float64, currency string, productID string, intervalPrefix string) (string, error) {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.createOrUpdateStripePrice")
	defer span.End()

	if amount == nil {
		return "", fmt.Errorf("amount cannot be nil for %s price", intervalPrefix)
	}

	// Convert amount to cents (Stripe uses smallest currency unit)
	amountCents := int64(*amount * 100)

	// Determine the interval
	interval := stripe.PriceRecurringIntervalMonth
	isYearly := intervalPrefix == "yearly"

	if isYearly {
		interval = stripe.PriceRecurringIntervalYear
	}

	priceParams := &stripe.PriceCreateParams{
		Currency:   stripe.String(currency),
		UnitAmount: stripe.Int64(amountCents),
		Product:    stripe.String(productID),
		Recurring: &stripe.PriceCreateRecurringParams{
			Interval: stripe.String(string(interval)),
		},
	}

	// Create the price using the gateway's client
	price, err := g.stripeClient.V1Prices().Create(ctx, priceParams)
	if err != nil {
		return "", fmt.Errorf("failed to create Stripe price: %w", err)
	}

	g.logger.Debug("created Stripe price",
		zap.String("product_id", productID),
		zap.String("price_id", price.ID),
		zap.String("interval", intervalPrefix),
		zap.Int64("amount_cents", amountCents))

	return price.ID, nil
}

// createOrUpdatePortalConfiguration creates or updates a billing portal configuration
// for a plan that restricts upgrade/downgrade paths based on PriceLine position
func (g *StripeGateway) createOrUpdatePortalConfiguration(ctx context.Context, plan *pluginCore.PricingPlanInfo, stripeProductID string, priceIDs []string) (string, error) {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.createOrUpdatePortalConfiguration")
	defer span.End()

	priceLinePlans, err := g.pricing.GetPriceLinesForPlan(ctx, plan.ID)
	if err != nil || len(priceLinePlans) == 0 {
		return "", fmt.Errorf("plan not in price line, cannot create portal configuration")
	}

	priceLineID := priceLinePlans[0].PriceLineID

	configName := fmt.Sprintf("Plan %d - Portal Config", plan.ID)

	enabled := true
	priceUpdate := stripe.String("price")

	if len(priceIDs) == 0 {
		return "", fmt.Errorf("no price IDs available for portal configuration")
	}

	pricePtrs := make([]*string, len(priceIDs))
	for i := range priceIDs {
		pricePtrs[i] = &priceIDs[i]
	}

	productsParam := []*stripe.BillingPortalConfigurationCreateFeaturesSubscriptionUpdateProductParams{
		{
			Product: stripe.String(stripeProductID),
			Prices:  pricePtrs,
		},
	}

	params := &stripe.BillingPortalConfigurationCreateParams{
		Name: stripe.String(configName),
		Features: &stripe.BillingPortalConfigurationCreateFeaturesParams{
			SubscriptionUpdate: &stripe.BillingPortalConfigurationCreateFeaturesSubscriptionUpdateParams{
				Enabled:              &enabled,
				DefaultAllowedUpdates: []*string{priceUpdate},
				Products:              productsParam,
				ProrationBehavior:     stripe.String("create_prorations"),
			},
		},
	}

	config, err := g.stripeClient.V1BillingPortalConfigurations().Create(ctx, params)
	if err != nil {
		return "", fmt.Errorf("failed to create portal configuration: %w", err)
	}

	g.logger.Info("created portal configuration",
		zap.Uint("plan_id", plan.ID),
		zap.Uint("price_line_id", priceLineID),
		zap.String("config_id", config.ID),
		zap.Int("allowed_prices", len(priceIDs)))

	return config.ID, nil
}

// resolvePriceIDsFromPlanIDs resolves internal plan IDs to Stripe price IDs
// by fetching gateway product mappings and extracting price IDs
func (g *StripeGateway) resolvePriceIDsFromPlanIDs(ctx context.Context, upgradePlanIDs, downgradePlanIDs []string) ([]string, error) {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.resolvePriceIDsFromPlanIDs")
	defer span.End()

	// Combine all plan IDs
	allPlanIDs := make(map[string]bool)
	for _, id := range upgradePlanIDs {
		allPlanIDs[id] = true
	}
	for _, id := range downgradePlanIDs {
		allPlanIDs[id] = true
	}

	// Collect all unique price IDs
	priceIDSet := make(map[string]bool)

	for planIDStr := range allPlanIDs {
		var planID uint
		_, err := fmt.Sscanf(planIDStr, "%d", &planID)
		if err != nil {
			g.logger.Warn("invalid plan ID format", zap.String("plan_id", planIDStr), zap.Error(err))
			continue
		}

		// Get gateway product mapping for this plan
		mapping, err := g.pricing.GetGatewayProductMapping(ctx, planID, GatewayID)
		if err != nil {
			g.logger.Debug("could not get gateway mapping for plan",
				zap.Uint("plan_id", planID),
				zap.Error(err))
			continue
		}

		// Add monthly price ID if available
		if mapping.RemoteMonthlyPriceID != "" {
			priceIDSet[mapping.RemoteMonthlyPriceID] = true
		}

		// Add yearly price ID if available
		if mapping.RemoteYearlyPriceID != "" {
			priceIDSet[mapping.RemoteYearlyPriceID] = true
		}
	}

	// Convert set to slice
	priceIDs := make([]string, 0, len(priceIDSet))
	for priceID := range priceIDSet {
		priceIDs = append(priceIDs, priceID)
	}

	g.logger.Debug("resolved price IDs from plan IDs",
		zap.Int("total_plans", len(allPlanIDs)),
		zap.Int("price_ids_found", len(priceIDs)))

	return priceIDs, nil
}

// GetManagementInfo returns management capabilities for operations
func (g *StripeGateway) GetManagementInfo(ctx context.Context, userID uint) (*pluginCore.ManagementCapabilities, error) {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.GetManagementInfo")
	defer span.End()

	// Stripe supports portal-based management for all operations
	operations := map[pluginCore.ManagementOperation]bool{
		pluginCore.OperationCancel:     true,
		pluginCore.OperationChangePlan: true,
	}

	return &pluginCore.ManagementCapabilities{
		ManagementMode: pluginCore.ModePortal,
		Operations:     operations,
	}, nil
}

// GetManagementURL returns the appropriate action for a management operation
func (g *StripeGateway) GetManagementURL(ctx context.Context, userID uint, operation pluginCore.ManagementOperation) (*pluginCore.ManagementResult, error) {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.GetManagementURL")
	defer span.End()

	if g.billing == nil {
		return nil, fmt.Errorf("billing service not configured")
	}

	// Check if user has an active Stripe subscription
	subscriber, err := g.billing.GetActiveSubscription(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get active subscription: %w", err)
	}
	if subscriber == nil || subscriber.GatewayType != GatewayID {
		return nil, fmt.Errorf("no active stripe subscription found for user %d", userID)
	}

	// Get customer portal URL
	portalURL, err := g.GetCustomerPortalURL(ctx, userID, "")
	if err != nil {
		return nil, fmt.Errorf("failed to get customer portal URL: %w", err)
	}

	// Return portal redirect for all supported operations
	return &pluginCore.ManagementResult{
		Action: pluginCore.ActionRedirect,
		URL:    portalURL,
	}, nil
}

var _ pluginCore.PaymentGateway = (*StripeGateway)(nil)
