package stripe

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stripe/stripe-go/v85"
	"github.com/stripe/stripe-go/v85/webhook"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/internal/config"
	billingModels "go.lumeweb.com/portal-plugin-billing/internal/db/models"
	billingEvent "go.lumeweb.com/portal-plugin-billing/internal/event"
	"go.lumeweb.com/portal-plugin-billing/internal/gateway"
	"go.lumeweb.com/portal-plugin-billing/pkg/subscription"
	quotaCore "go.lumeweb.com/portal-plugin-quota/core"
	"go.lumeweb.com/portal/core"
	"go.lumeweb.com/portal/db/models"
	"go.uber.org/zap"
)

// Package stripe provides a payment gateway implementation for processing Stripe webhooks and managing subscriptions.
//
// Webhook Event Flows
//1
// This package handles Stripe webhook events to manage subscriptions and credit transactions. The primary flows are:
//
// New Subscription Flow:
//   1. checkout.session.completed
//      - Creates a pending subscriber entry locally (inactive state)
//      - Does NOT activate the subscription
//      - Does NOT issue credits
//   2. invoice.paid
//      - Issues debit for subscription period cost (TransactionTypeTime)
//      - Issues credit for the payment amount (TransactionTypeCharge)
//      - Looks up the pending subscriber by subscription ID
//      - Activates the subscriber and assigns quota plan
//   3. invoice.payment_failed (optional, on failure)
//      - Logs payment failure
//      - Does not activate or issue credits
//   4. invoice.payment_action_required (optional, on 3D Secure)
//      - Logs requirement for customer action
//      - Awaits customer authentication
//
// Subscription Renewal Flow:
//   1. invoice.created
//      - Not processed directly
//   2. invoice.upcoming
//      - Not processed directly
//   3. invoice.finalized
//      - Not processed directly
//   4. invoice.paid
//      - Issues debit for subscription period cost (TransactionTypeTime)
//      - Issues credit for the payment amount (TransactionTypeCharge)
//      - Activates/renews the subscriber if inactive
//   5. invoice.payment_failed
//      - Logs payment failure
//
// Subscription Cancellation Flow:
//   customer.subscription.deleted
//   - Deactivates subscriber in local database
//   - Removes quota plan assignment
//
// Subscription Upgrade/Downgrade Flow:
//   1. customer.subscription.updated
//      - Processes plan changes
//      - May create pending proration credits
//   2. invoice.paid (follows with prorated charges/credits)
//      - Net amount issues credit (may be zero for full credit offset)
//      - Activates with new plan
//
// Idempotency
// All handlers are designed to be idempotent. Credit issuance uses invoice IDs as reference keys
// and will not duplicate credits for the same invoice. Subscriber state transitions are safe
// to replay.
//
// Credit/Debit Issuance Rules
//   - Debits and credits are issued on invoice.paid events
//   - First issues a DEBIT for the subscription period cost (TransactionTypeTime)
//   - Then issues a CREDIT for the payment amount (TransactionTypeCharge)
//   - This implements a bank-account-like model: Debit=withdrawal, Credit=deposit
//   - Invoice IDs are used as reference keys for idempotency
//   - Invoice.paid is the single source of truth for payment confirmation
//
// Subscriber Tracking
//   - Subscribers are tracked in local database via BillingService
//   - Stripe Customer metadata is NOT used for tracking
//   - Pending subscriptions (inactive until invoice.paid)
//   - Active subscriptions (after successful payment)
//   - Subscribers are looked up by subscription ID or customer ID

//go:embed assets/*.svg
var gatewayLogoFiles embed.FS

//go:embed templates/*.tpl
var templatesFS embed.FS

const (
	GatewayID                             = "stripe"
	EventTypeCheckoutSessionCompleted     = "checkout.session.completed"
	EventTypeSubscriptionDeleted          = "customer.subscription.deleted"
	EventTypeSubscriptionPaused           = "customer.subscription.paused"
	EventTypeSubscriptionResumed          = "customer.subscription.resumed"
	EventTypeSubscriptionUpdated          = "customer.subscription.updated"
	EventTypeInvoicePaid                  = "invoice.paid"
	EventTypeInvoicePaymentFailed         = "invoice.payment_failed"
	EventTypeInvoicePaymentActionRequired = "invoice.payment_action_required"
	PlanIDMetadataKey                     = "plan_id"
	UserIDMetadataKey                     = "user_id"
	CustomerIDPrefix                      = "cus_"
)

// Setup creates and configures a Stripe gateway if webhook secret is configured.
// Returns a log message (empty if not configured), the gateway instance (nil if not configured), and an error.
func Setup(opts pluginCore.GatewaySetupOptions, cfg *config.ServiceConfig) (string, pluginCore.GatewayIdentity, error) {
	if cfg.Stripe.WebhookSecret == "" {
		return "", nil, nil
	}
	if cfg.Stripe.SecretKey == "" {
		return "", nil, fmt.Errorf("secret key is required when webhook secret is configured")
	}

	gw := NewWithConfig(opts.Logger, opts.Ctx, cfg, opts.Quota, opts.User, opts.BillingSvc, opts.PricingSvc, opts.CreditSvc)
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
	Cancel(ctx context.Context, id string, params *stripe.SubscriptionCancelParams) (*stripe.Subscription, error)
	Update(ctx context.Context, id string, params *stripe.SubscriptionUpdateParams) (*stripe.Subscription, error)
}

// Products defines the interface for product operations
type Products interface {
	Create(ctx context.Context, params *stripe.ProductCreateParams) (*stripe.Product, error)
	Retrieve(ctx context.Context, id string, params *stripe.ProductRetrieveParams) (*stripe.Product, error)
	Update(ctx context.Context, id string, params *stripe.ProductUpdateParams) (*stripe.Product, error)
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
	logger              *core.Logger
	coreCtx             core.Context
	endpointSecret      string
	secretKey           string
	publishableKey      string
	stripeClient        Client
	quota               quotaCore.QuotaService
	users               core.UserService
	billing             pluginCore.BillingService
	pricing             pluginCore.PricingService
	subService          SubscriptionRetriever
	customerService     CustomerRetriever
	fs                  fs.FS // filesystem for logo files, nil uses embedded files
	credit              pluginCore.CreditService
	defaultPriceCadence string
}

// InvoiceProrationAnalysis extracts proration details from Stripe invoice line items
// Following the pattern from stripe_preview_test.go's calculateProratedAmounts()
type InvoiceProrationAnalysis struct {
	InvoiceID            string
	HasProratedItems     bool
	ProrationChargeTotal int64           // Sum of positive prorated line amounts (cents)
	ProrationCreditTotal int64           // Sum of negative prorated line amounts (cents)
	NetProrationDollars  decimal.Decimal // (charge + credit) / 100
	TotalLineItems       int
}

// ProrationComparison holds the results of comparing local and Stripe proration calculations
type ProrationComparison struct {
	LocalResult       *subscription.ProrationResult // Our local calculation
	StripeAmount      decimal.Decimal               // Stripe's net proration (dollars)
	MismatchDetected  bool
	Difference        decimal.Decimal
	DifferencePercent float64
	RecommendedAction string // "use_local" or "use_stripe"
	InvoiceAnalysis   *InvoiceProrationAnalysis
}

// EmbeddedCheckoutData holds template data for the embedded checkout form.
type EmbeddedCheckoutData struct {
	PublishableKey string
	ClientSecret   string
	Appearance     string
}

// newGateway is the internal constructor that creates a StripeGateway instance
// with a custom filesystem
func newGateway(coreCtx core.Context, logger *core.Logger, cfg *config.ServiceConfig, quota quotaCore.QuotaService, users core.UserService, billing pluginCore.BillingService, pricing pluginCore.PricingService, credit pluginCore.CreditService, fs fs.FS) *StripeGateway {
	// Configure backend to use HTTP only when testMode is enabled
	// Note: TestMode config flag is the sole determinant, we ignore the secret key completely
	if cfg.Stripe.TestMode {
		// Get the existing API backend
		existingBackend := stripe.GetBackend(stripe.APIBackend)
		existingImpl, ok := existingBackend.(*stripe.BackendImplementation)
		if ok {
			if existingImpl.URL == stripe.APIURL {
				// Replace the existing API backend with one configured to use HTTP
				httpBackend := &stripe.BackendImplementation{
					Type:              stripe.APIBackend,
					URL:               "http://api.stripe.com",
					HTTPClient:        existingImpl.HTTPClient,
					LeveledLogger:     existingImpl.LeveledLogger,
					MaxNetworkRetries: existingImpl.MaxNetworkRetries,
				}
				stripe.SetBackend(stripe.APIBackend, httpBackend)
			}
		}
	}

	stripeClient := &client{client: stripe.NewClient(cfg.Stripe.SecretKey)}

	gateway := &StripeGateway{
		logger:              logger,
		coreCtx:             coreCtx,
		endpointSecret:      cfg.Stripe.WebhookSecret,
		secretKey:           cfg.Stripe.SecretKey,
		publishableKey:      cfg.Stripe.PublishableKey,
		stripeClient:        stripeClient,
		quota:               quota,
		users:               users,
		billing:             billing,
		pricing:             pricing,
		fs:                  fs,
		credit:              credit,
		defaultPriceCadence: cfg.DefaultPriceCadence,
	}

	gateway.subService = gateway.subscriptionRetriever()
	gateway.customerService = gateway.customerRetriever()

	return gateway
}

// NewWithConfig creates a StripeGateway instance with the full config
func NewWithConfig(logger *core.Logger, coreCtx core.Context, cfg *config.ServiceConfig, quota quotaCore.QuotaService, users core.UserService, billing pluginCore.BillingService, pricing pluginCore.PricingService, credit pluginCore.CreditService) *StripeGateway {
	return newGateway(coreCtx, logger, cfg, quota, users, billing, pricing, credit, gatewayLogoFiles)
}

// NewWithConfigAndFS creates a StripeGateway instance with config and custom filesystem for testing
func NewWithConfigAndFS(logger *core.Logger, coreCtx core.Context, cfg *config.ServiceConfig, quota quotaCore.QuotaService, users core.UserService, billing pluginCore.BillingService, pricing pluginCore.PricingService, credit pluginCore.CreditService, fs fs.FS) *StripeGateway {
	return newGateway(coreCtx, logger, cfg, quota, users, billing, pricing, credit, fs)
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
			// Support both active and paused subscriptions for customer portal access
			return g.createPortalSession(ctx, userID, returnUrl, true, nil)
		},
	)
}

// createPortalSession creates a billing portal session with optional deep link flow data.
// When flowData is nil, a generic portal session is created (portal homepage).
// When flowData is provided, the session deep links directly to the specified flow action.
// The checkPaused parameter determines whether to look for paused subscriptions in addition to active ones.
func (g *StripeGateway) createPortalSession(ctx context.Context, userID uint, returnUrl string, checkPaused bool, flowData *stripe.BillingPortalSessionCreateFlowDataParams) (string, error) {
	// Get the subscriber for this user and gateway (supports both active and paused subscriptions)
	subscriber, err := g.getActiveOrPausedSubscription(ctx, userID, checkPaused)
	if err != nil {
		return "", fmt.Errorf("failed to get subscription: %w", err)
	}
	if subscriber == nil || subscriber.GatewayType != GatewayID {
		return "", fmt.Errorf("no stripe subscription found for user %d", userID)
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
	if subscriber.PricingPlanPeriodID != nil && g.pricing != nil {
		mapping, err := g.pricing.GetGatewayProductMapping(ctx, *subscriber.PricingPlanPeriodID, GatewayID)
		if err == nil && mapping != nil && mapping.PortalConfigurationID != nil {
			params.Configuration = stripe.String(*mapping.PortalConfigurationID)
			g.logger.Debug("using plan-specific portal configuration",
				zap.Uint("period_id", *subscriber.PricingPlanPeriodID),
				zap.String("config_id", *mapping.PortalConfigurationID),
				zap.Uint("user_id", userID))
		}
	}

	// Apply deep link flow data when provided
	if flowData != nil {
		params.FlowData = flowData
	}

	sess, err := g.stripeClient.V1BillingPortalSessions().Create(ctx, params)
	if err != nil {
		return "", fmt.Errorf("failed to create billing portal session: %w", err)
	}

	return sess.URL, nil
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
		return g.handleSubscriptionResumed(ctx, event)
	case EventTypeSubscriptionDeleted:
		return g.handleSubscriptionDeactivated(ctx, event)
	case EventTypeSubscriptionPaused:
		return g.handleSubscriptionPaused(ctx, event)
	case EventTypeSubscriptionUpdated:
		return g.handleSubscriptionUpdated(ctx, event)
	case EventTypeInvoicePaid:
		return g.handleInvoicePaid(ctx, event)
	case EventTypeInvoicePaymentFailed:
		return g.handleInvoicePaymentFailed(ctx, event)
	case EventTypeInvoicePaymentActionRequired:
		return g.handleInvoicePaymentActionRequired(ctx, event)
	default:
		g.logger.Debug("unhandled event type", zap.String("event_type", string(event.Type)))
		return nil
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

func (g *StripeGateway) handleSubscriptionPaused(ctx context.Context, event stripe.Event) error {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.handleSubscriptionPaused")
	defer span.End()

	return g.handleSubscriptionEvent(ctx, event, g.pauseSubscription)
}

func (g *StripeGateway) handleSubscriptionResumed(ctx context.Context, event stripe.Event) error {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.handleSubscriptionResumed")
	defer span.End()

	return g.handleSubscriptionEvent(ctx, event, g.resumeSubscription)
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
			isCancellationRequest := g.isCancellationRequest(subscription)

			// Handle cancellation request
			if isCancellationRequest {
				cancelAt := time.Unix(subscription.CancelAt, 0)
				g.logger.Debug("subscription cancellation request received - updating WillCancelAt",
					zap.Uint("user_id", userID),
					zap.String("subscription_id", subscription.ID),
					zap.String("event_id", event.ID),
					zap.Time("cancel_at", cancelAt))

				// Get current subscriber to preserve pricing plan period and billing period dates
				subscriber, err := g.billing.GetActiveSubscription(ctx, userID)
				if err != nil {
					g.logger.Error("failed to get current subscriber for cancellation request",
						zap.Error(err),
						zap.Uint("user_id", userID),
						zap.String("subscription_id", subscription.ID))
					return err
				}

				if subscriber == nil || subscriber.GatewayType != GatewayID {
					g.logger.Warn("no active subscriber found for cancellation request - may have been deactivated",
						zap.Uint("user_id", userID),
						zap.String("subscription_id", subscription.ID))
					return nil // Not an error - subscriber might have been deactivated already
				}

				if subscription.Customer == nil {
					return fmt.Errorf("subscription missing customer for cancellation request")
				}

				// Update subscriber with WillCancelAt, keeping all other fields intact
				if err := g.billing.CreateOrUpdateSubscriber(
					ctx,
					userID,
					GatewayID,
					subscription.Customer.ID,
					subscription.ID,
					true, // Keep active - will be deactivated when deletion event arrives
					subscriber.PricingPlanPeriodID,
					pluginCore.WithWillCancelAt(&cancelAt),
					pluginCore.WithBillingPeriodStart(subscriber.BillingPeriodStart),
					pluginCore.WithBillingPeriodEnd(subscriber.BillingPeriodEnd),
				); err != nil {
					g.logger.Error("failed to update subscriber WillCancelAt",
						zap.Error(err),
						zap.Uint("user_id", userID),
						zap.String("subscription_id", subscription.ID))
					return err
				}

				g.logger.Info("successfully updated subscriber WillCancelAt for cancellation request",
					zap.Uint("user_id", userID),
					zap.String("subscription_id", subscription.ID),
					zap.Time("will_cancel_at", cancelAt),
					zap.String("event_id", event.ID))

				return nil
			}

			// Uncancel detection: when an active subscription no longer has CancelAt > 0,
			// check if the subscriber has a WillCancelAt set and clear it.
			if subscription.CancelAt == 0 && subscription.CancellationDetails == nil && subscription.Status != stripe.SubscriptionStatusCanceled && g.billing != nil {
				subscriber, err := g.billing.GetActiveSubscription(ctx, userID)
				if err != nil {
					g.logger.Error("failed to get current subscriber for uncancel detection",
						zap.Error(err),
						zap.Uint("user_id", userID),
						zap.String("subscription_id", subscription.ID))
					return err
				}

				if subscriber != nil && subscriber.GatewayType == GatewayID && subscriber.WillCancelAt != nil {
					g.logger.Info("uncancel detected - clearing WillCancelAt",
						zap.Uint("user_id", userID),
						zap.String("subscription_id", subscription.ID),
						zap.Time("previous_will_cancel_at", *subscriber.WillCancelAt),
						zap.String("event_id", event.ID))

					if err := g.billing.CreateOrUpdateSubscriber(
						ctx,
						userID,
						GatewayID,
						subscription.Customer.ID,
						subscription.ID,
						true,
						subscriber.PricingPlanPeriodID,
						pluginCore.WithClearWillCancelAt(),
						pluginCore.WithBillingPeriodStart(subscriber.BillingPeriodStart),
						pluginCore.WithBillingPeriodEnd(subscriber.BillingPeriodEnd),
					); err != nil {
						g.logger.Error("failed to clear WillCancelAt on uncancel",
							zap.Error(err),
							zap.Uint("user_id", userID),
							zap.String("subscription_id", subscription.ID))
						return err
					}

					g.logger.Info("successfully cleared WillCancelAt for uncancel",
						zap.Uint("user_id", userID),
						zap.String("subscription_id", subscription.ID),
						zap.String("event_id", event.ID))

					return nil
				}
			}

			if subscription.Status == stripe.SubscriptionStatusCanceled {
				g.logger.Debug("subscription is canceled in Stripe - ignoring update event",
					zap.Uint("user_id", userID),
					zap.String("subscription_id", subscription.ID),
					zap.String("event_id", event.ID))

				return nil
			}

			// Check if the subscription has a period
			periodID, hasPeriod, err := findPeriodIDFromSubscription(subscription)
			if err != nil {
				return err
			}

			// If no period is found, treat as deactivation
			if !hasPeriod {
				g.logger.Warn("subscription updated but price metadata missing period_id",
					zap.String("subscription_id", subscription.ID),
					zap.String("event_id", event.ID))

				return g.deactivateSubscription(ctx, userID, subscription, event)
			}

			// If period is found, treat as activation
			return g.activateSubscriptionWithPeriodID(ctx, userID, subscription, event, periodID)
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

// findPeriodIDFromSubscription extracts the period ID from the Stripe price metadata
func findPeriodIDFromSubscription(sub *stripe.Subscription) (uint, bool, error) {
	if sub.Items == nil || len(sub.Items.Data) == 0 {
		return 0, false, nil
	}

	for _, item := range sub.Items.Data {
		if item == nil || item.Price == nil {
			continue
		}

		periodID, found, err := extractPeriodIDFromPrice(item.Price)
		if err != nil {
			return 0, false, err
		}
		if found {
			return periodID, true, nil
		}
	}

	return 0, false, nil
}

// extractPeriodIDFromPrice extracts the period ID from Stripe price metadata
func extractPeriodIDFromPrice(price *stripe.Price) (uint, bool, error) {
	if price == nil || price.Metadata == nil {
		return 0, false, nil
	}

	periodIDStr := price.Metadata["period_id"]
	if periodIDStr == "" {
		return 0, false, nil
	}

	periodID, err := strconv.ParseUint(periodIDStr, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("invalid period_id format in price metadata: %w", err)
	}

	return uint(periodID), true, nil
}

// handleCheckoutSessionCompleted processes a completed checkout session.
// This is the first event in the subscription flow and creates a pending subscriber entry locally.
//
// Expected payload: stripe.CheckoutSession containing:
//   - ClientReferenceID: User ID from portal
//   - Customer: Stripe Customer object with ID
//   - Subscription: Stripe Subscription object with ID
//
// Actions taken:
//   - Creates a pending subscriber in local database (isActive=false)
//   - Does NOT issue credits
//   - Does NOT activate the subscription
//   - Waits for invoice.paid to finalize activation
//
// Error conditions:
//   - Missing ClientReferenceID: returns error
//   - Missing Customer or Customer ID: returns error
//   - Missing Subscription or Subscription ID: returns error
//   - Billing service failure: returns error
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

			subscriptionID := session.Subscription.ID
			if subscriptionID == "" {
				return fmt.Errorf("checkout session subscription missing ID")
			}

			// Verify we have a customer ID
			var customerID string
			if session.Customer == nil || session.Customer.ID == "" {
				return fmt.Errorf("checkout session missing customer id")
			}
			customerID = session.Customer.ID

			// Check if subscriber already exists (race condition: invoice.paid may have already activated)
			existingSubscriber, err := g.billing.GetSubscriberBySubscriptionID(ctx, subscriptionID, GatewayID)
			if err != nil {
				g.logger.Warn("failed to check for existing subscriber during checkout",
					zap.Error(err),
					zap.String("session_id", session.ID),
					zap.String("subscription_id", subscriptionID))
				// Continue - we'll try to create/update anyway
			}

			if existingSubscriber != nil && existingSubscriber.IsActive {
				// Race condition: invoice.paid arrived first and already activated
				// Skip to avoid clobbering the active state
				g.logger.Info("checkout completed but subscription already active (invoice.paid won race)",
					zap.String("session_id", session.ID),
					zap.String("subscription_id", subscriptionID),
					zap.Uint("user_id", userIDUint))
				return nil
			}

			// Fetch the subscription to get period_id from price metadata
			subscription, err := g.getExpandedSubscription(ctx, subscriptionID)
			if err != nil {
				g.logger.Warn("failed to fetch subscription for checkout, creating subscriber without period",
					zap.Error(err),
					zap.String("session_id", session.ID),
					zap.String("subscription_id", subscriptionID))
			}

			var periodID *uint
			if subscription != nil {
				if pid, found, _ := findPeriodIDFromSubscription(subscription); found {
					periodID = &pid
				}
			}

			// Create pending subscriber entry for this subscription
			// This will be activated when invoice.paid fires (or may already be active if we lost the race)
			if err := g.billing.CreateOrUpdateSubscriber(
				ctx,
				userIDUint,
				GatewayID,
				customerID,
				subscriptionID,
				false,
				periodID,
			); err != nil {
				g.logger.Error("failed to create pending subscriber for checkout",
					zap.Error(err),
					zap.String("session_id", session.ID),
					zap.String("subscription_id", subscriptionID),
					zap.String("customer_id", customerID),
					zap.Uint("user_id", userIDUint))
				return fmt.Errorf("failed to create pending subscriber: %w", err)
			}

			// Log checkout completion - subscription will be activated when invoice.paid fires
			g.logger.Debug("checkout completed - subscription pending activation on payment",
				zap.String("session_id", session.ID),
				zap.String("subscription_id", subscriptionID),
				zap.String("customer_id", customerID),
				zap.Uint("user_id", userIDUint))

			return nil
		},
	)
}

// calculateNetInvoiceAmount calculates the net payment amount from an invoice.
// Accounts for amount paid (in cents, converted to decimal dollars).
func (g *StripeGateway) calculateNetInvoiceAmount(invoice *stripe.Invoice) decimal.Decimal {
	// Convert from cents to dollars
	amount := decimal.NewFromInt(invoice.AmountPaid).Div(decimal.NewFromInt(100))
	return amount
}

// resolveSubscriberForInvoice attempts to resolve a subscriber when the primary lookup by subscription ID fails.
// This handles the race condition where invoice.paid arrives before checkout.session.completed.
//
// Fallback chain:
//  1. GetSubscriberByExternalID — checkout may have created the record under the customer ID
//  2. GetActiveSubscription — user may already have an active subscription from another flow
//  3. Create a new pending subscriber — extract userID from subscription metadata
func (g *StripeGateway) resolveSubscriberForInvoice(ctx context.Context, subscriptionID, customerID string, subscription *stripe.Subscription) (*pluginCore.Subscriber, error) {
	// Fallback 1: lookup by external ID (customer ID)
	subscriber, err := g.billing.GetSubscriberByExternalID(ctx, customerID, GatewayID)
	if err != nil {
		g.logger.Warn("failed to look up subscriber by external ID during race fallback",
			zap.String("customer_id", customerID),
			zap.Error(err))
	} else if subscriber != nil {
		g.logger.Debug("resolved subscriber by external ID during race fallback",
			zap.String("customer_id", customerID),
			zap.Uint("user_id", subscriber.UserID))
		return subscriber, nil
	}

	// Extract userID from subscription to enable remaining fallbacks
	userID, err := g.extractUserIDFromSubscription(ctx, subscription)
	if err != nil || userID == 0 {
		g.logger.Warn("cannot resolve subscriber without user ID from subscription",
			zap.String("subscription_id", subscriptionID),
			zap.Error(err))
		return nil, nil
	}

	// Fallback 2: check if user already has an active subscription
	activeSub, err := g.billing.GetActiveSubscription(ctx, userID)
	if err != nil {
		g.logger.Warn("failed to check for active subscription during race fallback",
			zap.Uint("user_id", userID),
			zap.Error(err))
	} else if activeSub != nil {
		g.logger.Debug("resolved subscriber via active subscription during race fallback",
			zap.Uint("user_id", userID),
			zap.String("active_subscription_id", activeSub.SubscriptionID))
		return activeSub, nil
	}

	// Fallback 3: create a pending subscriber on the spot
	var periodID *uint
	if pid, found, _ := findPeriodIDFromSubscription(subscription); found {
		periodID = &pid
	}

	if err := g.billing.CreateOrUpdateSubscriber(
		ctx,
		userID,
		GatewayID,
		customerID,
		subscriptionID,
		false,
		periodID,
	); err != nil {
		return nil, fmt.Errorf("failed to create pending subscriber during race fallback: %w", err)
	}

	g.logger.Info("created pending subscriber during race fallback with invoice.paid",
		zap.Uint("user_id", userID),
		zap.String("subscription_id", subscriptionID),
		zap.String("customer_id", customerID))

	subscriber, err = g.billing.GetSubscriberBySubscriptionID(ctx, subscriptionID, GatewayID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch newly created subscriber: %w", err)
	}
	return subscriber, nil
}

// handleInvoicePaid processes a successful invoice payment.
// This is the single source of truth for issuing credits and activating subscriptions.
//
// Expected payload: stripe.Invoice containing:
//   - Customer: Stripe Customer object with ID
//   - Lines: Invoice line items with subscription IDs
//   - AmountPaid: Net payment amount in cents
//   - ID: Invoice ID for idempotency
//
// Actions taken:
//   - Looks up pending subscriber by subscription ID from invoice lines
//   - Issues credit for payment amount (if positive) using invoice ID as reference
//   - Activates subscriber and assigns quota plan
//   - Returns error if credit issuance fails (client may retry)
//
// Error conditions:
//   - Missing Customer or Customer ID: returns error
//   - No subscription ID in invoice lines: logs warning and returns nil (no-op)
//   - No pending subscriber found: attempts fallback resolution (external ID, active subscription, auto-create)
//     If all fallbacks fail, logs warning and returns nil (no-op)
//   - Credit issuance failure: returns error
func (g *StripeGateway) handleInvoicePaid(ctx context.Context, event stripe.Event) error {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.handleInvoicePaid")
	defer span.End()

	return core.MetricTrack(
		nil,
		InvoicePaid.WithLabelValues(LabelStatusError),
		func() error {
			if event.Data == nil {
				return fmt.Errorf("event data is nil")
			}

			if len(event.Data.Raw) == 0 {
				return fmt.Errorf("event data raw payload is empty")
			}

			var invoice stripe.Invoice
			if err := json.Unmarshal(event.Data.Raw, &invoice); err != nil {
				return err
			}

			// Validate we have customer details
			if invoice.Customer == nil || invoice.Customer.ID == "" {
				return fmt.Errorf("invoice missing customer")
			}

			customerIDStr := invoice.Customer.ID

			// Look for subscription ID in invoice lines via parent.subscription_item_details.subscription
			subscriptionID := ""
			if invoice.Lines != nil && len(invoice.Lines.Data) > 0 {
				for _, line := range invoice.Lines.Data {
					if line.Parent != nil &&
						line.Parent.SubscriptionItemDetails != nil &&
						line.Parent.SubscriptionItemDetails.Subscription != "" {
						subscriptionID = line.Parent.SubscriptionItemDetails.Subscription
						break
					}
				}
			}

			// Fallback to invoice-level parent.subscription_details.subscription
			if subscriptionID == "" &&
				invoice.Parent != nil &&
				invoice.Parent.SubscriptionDetails != nil &&
				invoice.Parent.SubscriptionDetails.Subscription != nil &&
				invoice.Parent.SubscriptionDetails.Subscription.ID != "" {
				subscriptionID = invoice.Parent.SubscriptionDetails.Subscription.ID
			}

			// If we still don't have a subscription ID, log and skip
			if subscriptionID == "" {
				g.logger.Warn("invoice paid but cannot find subscription ID - cannot activate",
					zap.String("invoice_id", invoice.ID),
					zap.String("customer_id", customerIDStr))
				return nil
			}

			// Look up local pending subscriber entry
			var subscription *stripe.Subscription
			subscriber, err := g.billing.GetSubscriberBySubscriptionID(ctx, subscriptionID, GatewayID)
			if err != nil {
				g.logger.Error("failed to look up subscriber by subscription ID",
					zap.Error(err),
					zap.String("subscription_id", subscriptionID),
					zap.String("invoice_id", invoice.ID))
				return nil
			}

			if subscriber == nil {
				// Race condition: invoice.paid arrived before checkout.session.completed
				// Try fallbacks to resolve or create the subscriber
				subscription, err = g.getExpandedSubscription(ctx, subscriptionID)
				if err != nil {
					g.logger.Warn("invoice paid but no subscriber found and failed to fetch subscription for fallback",
						zap.String("subscription_id", subscriptionID),
						zap.String("invoice_id", invoice.ID),
						zap.Error(err))
					return nil
				}

				subscriber, err = g.resolveSubscriberForInvoice(ctx, subscriptionID, customerIDStr, subscription)
				if err != nil {
					g.logger.Error("invoice paid but failed to resolve subscriber via fallbacks",
						zap.String("subscription_id", subscriptionID),
						zap.String("invoice_id", invoice.ID),
						zap.Error(err))
					return nil
				}
				if subscriber == nil {
					g.logger.Warn("invoice paid but no subscriber could be resolved - cannot activate",
						zap.String("subscription_id", subscriptionID),
						zap.String("invoice_id", invoice.ID))
					return nil
				}
				g.logger.Info("resolved subscriber via fallback after race with checkout",
					zap.String("subscription_id", subscriptionID),
					zap.String("invoice_id", invoice.ID),
					zap.Uint("user_id", subscriber.UserID))
			}

			// Verify this invoice is for the same user
			userID := subscriber.UserID

			// Expand subscription to get product/price details for operation detection and validation
			if subscription == nil {
				subscription, err = g.getExpandedSubscription(ctx, subscriptionID)
				if err != nil {
					g.logger.Warn("failed to fetch subscription for validation",
						zap.String("subscription_id", subscriptionID),
						zap.Error(err))
					return nil
				}
			}

			// Determine operation type and validate before issuing credit
			operation := g.determineOperationType(ctx, subscriber, subscription, &invoice)
			g.logger.Debug("determined operation type for invoice",
				zap.String("operation", string(operation)),
				zap.String("invoice_id", invoice.ID),
				zap.Uint("user_id", userID))

			// Validate and calculate credit amount with proration comparison
			validatedAmount, err := g.validateAndCalculateCreditAmount(
				ctx,
				userID,
				operation,
				subscriber,
				subscription,
				&invoice,
			)
			if err != nil {
				return fmt.Errorf("credit validation failed: %w", err)
			}

			if validatedAmount.GreaterThan(decimal.Zero) {
				if g.credit == nil {
					return fmt.Errorf("credit service not configured")
				}
				if err := g.credit.IssueCreditWithIdempotency(
					ctx,
					uint64(userID),
					pluginCore.TransactionTypeCharge,
					validatedAmount,
					pluginCore.ReferenceTypeStripeInvoice,
					invoice.ID,
					fmt.Sprintf("Invoice %s paid (%s)", invoice.ID, operation),
					0, // createdBy: 0 for system
				); err != nil {
					g.logger.Error("failed to issue invoice payment credit",
						zap.Error(err),
						zap.Uint("user_id", userID),
						zap.String("invoice_id", invoice.ID),
						zap.String("amount", validatedAmount.String()))
					return fmt.Errorf("failed to issue invoice payment credit: %w", err)
				}

				g.logger.Info("invoice payment credit issued",
					zap.String("invoice_id", invoice.ID),
					zap.Uint("user_id", userID),
					zap.String("amount", validatedAmount.String()),
					zap.String("operation", string(operation)))

				// Fire payment completed event
				evt := billingEvent.NewPaymentCompletedEvent(
					ctx,
					userID,
					validatedAmount,
					GatewayID,
					invoice.ID,
					subscriptionID,
				)
				core.Fire(g.coreCtx, billingEvent.EVENT_PAYMENT_COMPLETED, evt)
			} else {
				g.logger.Debug("invoice has zero or negative validated amount",
					zap.String("invoice_id", invoice.ID),
					zap.String("validated_amount", validatedAmount.String()),
					zap.String("operation", string(operation)))
			}

			// Check if user has sufficient balance to activate subscription
			// We verify our ledger AFTER issuing the charge credit but BEFORE issuing time debit
			if g.credit != nil {
				balance, err := g.credit.GetUserBalance(ctx, uint64(userID))
				if err != nil {
					g.logger.Error("failed to get user balance before activation check",
						zap.Error(err),
						zap.Uint("user_id", userID),
						zap.String("subscription_id", subscriptionID),
						zap.String("invoice_id", invoice.ID))
					return fmt.Errorf("failed to get user balance: %w", err)
				}

				// Check if user has sufficient credits (positive or zero balance)
				if balance.LessThan(decimal.Zero) {
					g.logger.Warn("subscription activation skipped - insufficient balance in ledger after recording payment",
						zap.Uint("user_id", userID),
						zap.String("subscription_id", subscriptionID),
						zap.String("invoice_id", invoice.ID),
						zap.String("balance", balance.String()))
					// Stripe recorded the payment, but user cannot afford to activate yet
					// They will need to accumulate more credits before accessing service
					// Ledger is synced, subscription remains inactive
					return nil
				}

				g.logger.Debug("user has sufficient balance, issuing time debit and activating subscription",
					zap.Uint("user_id", userID),
					zap.String("subscription_id", subscriptionID),
					zap.String("invoice_id", invoice.ID),
					zap.String("balance", balance.String()))

				planPeriodID, hasPlan, err := findPeriodIDFromSubscription(subscription)
				if err == nil && hasPlan {
					period, err := g.pricing.GetPricingPlanPeriod(ctx, planPeriodID)
					if err == nil && period != nil {
						periodPrice := decimal.NewFromFloat(period.PriceUSD)
						billingCycleStart := time.Time{}
						billingCycleEnd := time.Time{}

						if subscription.Items != nil && len(subscription.Items.Data) > 0 && subscription.Items.Data[0] != nil {
							if subscription.Items.Data[0].CurrentPeriodStart > 0 {
								billingCycleStart = time.Unix(subscription.Items.Data[0].CurrentPeriodStart, 0)
							}
							if subscription.Items.Data[0].CurrentPeriodEnd > 0 {
								billingCycleEnd = time.Unix(subscription.Items.Data[0].CurrentPeriodEnd, 0)
							}
						}

						err = g.credit.IssueUsageCredit(
							ctx,
							uint64(userID),
							pluginCore.TransactionTypeTime,
							periodPrice,
							invoice.ID,
							fmt.Sprintf("Subscription period %s",
								billingCycleStart.Format("2006-01-02")),
							0, // createdBy: 0 for system
						)
						if err != nil {
							g.logger.Error("failed to debit credit for subscription period",
								zap.Error(err),
								zap.Uint("user_id", userID),
								zap.Uint("period_id", planPeriodID),
								zap.String("subscription_id", subscriptionID),
								zap.String("invoice_id", invoice.ID),
								zap.String("amount", periodPrice.String()))
							return fmt.Errorf("failed to debit credit for subscription period: %w", err)
						}

						g.logger.Info("subscription period debit issued successfully",
							zap.Uint("user_id", userID),
							zap.Uint("period_id", planPeriodID),
							zap.String("subscription_id", subscriptionID),
							zap.String("invoice_id", invoice.ID),
							zap.String("period_start", billingCycleStart.Format("2006-01-02")),
							zap.String("period_end", billingCycleEnd.Format("2006-01-02")),
							zap.String("amount", periodPrice.String()))
					}
				}
			}

			// Activate subscription - assignUserToPlan is called inside activateSubscriptionWithPeriodID
			return g.activateSubscription(ctx, userID, subscription, event)
		},
	)
}

// handleInvoicePaymentFailed processes a failed invoice payment.
// This handler logs payment failures for alerting and monitoring purposes.
//
// Expected payload: stripe.Invoice containing:
//   - Customer: Stripe Customer object (may be nil)
//   - AmountDue: Outstanding amount in cents
//   - AttemptCount: Number of payment retry attempts
//   - Status: Invoice status string
//   - HostedInvoiceURL: Link for customer to retry payment
//
// Actions taken:
//   - Logs warning with payment details
//   - Does NOT change subscriber state
//   - Does NOT issue credits
//   - Notification/monitoring hooks can be added here
//
// Error handling:
//   - Returns nil for all errors (best-effort logging)
//   - Does not fail webhook processing on failure
func (g *StripeGateway) handleInvoicePaymentFailed(ctx context.Context, event stripe.Event) error {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.handleInvoicePaymentFailed")
	defer span.End()

	// Track payment failure metric
	InvoicePaymentFailed.WithLabelValues().Inc()

	if event.Data == nil || len(event.Data.Raw) == 0 {
		g.logger.Warn("invoice payment failed event has no data")
		return nil
	}

	var invoice stripe.Invoice
	if err := json.Unmarshal(event.Data.Raw, &invoice); err != nil {
		g.logger.Warn("failed to unmarshal invoice payment failed event", zap.Error(err))
		return nil
	}

	customerID := ""
	if invoice.Customer != nil {
		customerID = invoice.Customer.ID
	}

	g.logger.Warn("invoice payment failed",
		zap.String("invoice_id", invoice.ID),
		zap.String("customer_id", customerID),
		zap.Int64("amount_due", invoice.AmountDue),
		zap.String("attempt_count", fmt.Sprintf("%d", invoice.AttemptCount)),
		zap.String("status", string(invoice.Status)),
		zap.String("hosted_invoice_url", invoice.HostedInvoiceURL))

	// Optional: Send notification to admin/user
	// Optional: Track payment failure in billing system

	return nil
}

// handleInvoicePaymentActionRequired processes invoices requiring payment action (e.g., 3D Secure).
// Does not issue credit or activate subscription - waits for successful payment.
func (g *StripeGateway) handleInvoicePaymentActionRequired(ctx context.Context, event stripe.Event) error {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.handleInvoicePaymentActionRequired")
	defer span.End()

	// Track payment action required metric
	InvoicePaymentActionRequired.WithLabelValues().Inc()

	if event.Data == nil || len(event.Data.Raw) == 0 {
		g.logger.Warn("invoice payment action required event has no data")
		return nil
	}

	var invoice stripe.Invoice
	if err := json.Unmarshal(event.Data.Raw, &invoice); err != nil {
		g.logger.Warn("failed to unmarshal invoice payment action required event", zap.Error(err))
		return nil
	}

	customerID := ""
	if invoice.Customer != nil {
		customerID = invoice.Customer.ID
	}

	g.logger.Info("invoice payment action required - awaiting customer action",
		zap.String("invoice_id", invoice.ID),
		zap.String("customer_id", customerID),
		zap.String("hosted_invoice_url", invoice.HostedInvoiceURL),
		zap.Int64("amount_due", invoice.AmountDue))

	return nil
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

			periodID, hasPeriod, err := findPeriodIDFromSubscription(subscription)
			if err != nil {
				return err
			}

			if !hasPeriod {
				g.logger.Warn("subscription activated but price metadata missing period_id",
					zap.Uint("user_id", userID),
					zap.String("subscription_id", subscription.ID),
					zap.String("event_id", event.ID))
				return nil
			}

			return g.activateSubscriptionWithPeriodID(ctx, userID, subscription, event, periodID)
		},
	)
}

// activateSubscriptionWithPeriodID handles subscription activation with a known PricingPlanPeriod ID.
// It assigns the user to the QuotaPlan if one is configured, and tracks the subscriber
// in the local billing database.
func (g *StripeGateway) activateSubscriptionWithPeriodID(ctx context.Context, userID uint, subscription *stripe.Subscription, event stripe.Event, pricingPlanPeriodID uint) error {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.activateSubscriptionWithPeriodID")
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

	// Fetch the pricing plan period to get the quota plan ID
	period, err := g.pricing.GetPricingPlanPeriod(ctx, pricingPlanPeriodID)
	if err != nil {
		g.logger.Error("failed to fetch pricing plan period for quota assignment",
			zap.Error(err),
			zap.Uint("pricing_plan_period_id", pricingPlanPeriodID))
		// Continue without quota assignment - but we still need to track the subscriber
		period = nil
	} else if period == nil {
		g.logger.Warn("pricing plan period not found for quota assignment",
			zap.Uint("pricing_plan_period_id", pricingPlanPeriodID))
		// Continue without quota assignment
		period = nil
	}

	// Assign quota plan if configured
	if period != nil && period.QuotaPlanID != 0 {
		if g.quota == nil {
			g.logger.Error("quota service not configured, cannot assign quota plan",
				zap.Uint("pricing_plan_period_id", pricingPlanPeriodID),
				zap.Uint("quota_plan_id", period.QuotaPlanID))
		} else {
			if err := g.quota.AssignUserToPlan(ctx, user.ID, period.QuotaPlanID); err != nil {
				g.logger.Error("failed to assign quota plan",
					zap.Error(err),
					zap.Uint("user_id", user.ID),
					zap.Uint("pricing_plan_period_id", pricingPlanPeriodID),
					zap.Uint("quota_plan_id", period.QuotaPlanID))
				// Don't fail activation on quota assignment failure - user still needs access
			}
		}
	}

	// Track subscriber in billing service with PricingPlanPeriod ID
	if subscription.Customer == nil {
		return fmt.Errorf("subscription missing customer id")
	}

	if subscription.Customer.ID == "" {
		return fmt.Errorf("subscription missing customer id")
	}

	if err := g.trackSubscriber(ctx, user.ID, subscription.Customer.ID, subscription.ID, true, &pricingPlanPeriodID); err != nil {
		g.logger.Error("failed to track subscriber",
			zap.Error(err),
			zap.Uint("user_id", userID),
			zap.String("customer_id", subscription.Customer.ID),
			zap.String("subscription_id", subscription.ID))
	}

	// Fetch the period to get the actual plan ID
	period, err = g.pricing.GetPricingPlanPeriod(ctx, pricingPlanPeriodID)
	if err != nil {
		return fmt.Errorf("failed to fetch pricing plan period: %w", err)
	}

	// Fire subscription active event
	evt := billingEvent.NewSubscriptionActiveEvent(
		ctx,
		user.ID,
		subscription.ID,
		GatewayID,
		period.PricingPlanID,
		pricingPlanPeriodID,
	)
	core.Fire(g.coreCtx, billingEvent.EVENT_SUBSCRIPTION_ACTIVE, evt)

	g.logger.Debug("subscription activated",
		zap.Uint("user_id", userID),
		zap.String("subscription_id", subscription.ID),
		zap.Uint("pricing_plan_period_id", pricingPlanPeriodID),
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
			if g.quota != nil {
				if err := g.quota.RemoveUserFromPlan(ctx, user.ID); err != nil {
					g.logger.Error("failed to remove user from plan",
						zap.Error(err),
						zap.Uint("user_id", user.ID))
					// Continue with deactivation even if quota removal fails
				}
			}

			// Check if subscription.Customer is nil before accessing it
			if subscription.Customer == nil {
				g.logger.Error("subscription customer is nil",
					zap.Uint("user_id", userID),
					zap.String("subscription_id", subscription.ID),
					zap.String("event_id", event.ID))
				return fmt.Errorf("subscription customer is nil for subscription %s", subscription.ID)
			}

			// CALCULATE CANCELLATION CREDIT before deactivating
			var creditAmount decimal.Decimal
			var creditErr error

			if g.credit != nil {
				creditAmount, creditErr = g.calculateCancellationCredit(ctx, userID, subscription, event)
				if creditErr != nil {
					g.logger.Error("failed to calculate cancellation credit",
						zap.Error(creditErr),
						zap.Uint("user_id", userID),
						zap.String("subscription_id", subscription.ID))
					// Continue with deactivation even if credit calculation fails
				}

				// ISSUE CREDIT if applicable
				if creditAmount.GreaterThan(decimal.Zero) {
					if err := g.credit.IssueCreditWithIdempotency(
						ctx,
						uint64(userID),
						pluginCore.TransactionTypeComp,
						creditAmount,
						pluginCore.ReferenceTypeStripeInvoice,
						subscription.ID, // Use subscription ID as reference
						fmt.Sprintf("Subscription cancellation credit - %s", subscription.ID),
						0,
					); err != nil {
						g.logger.Error("failed to issue cancellation credit",
							zap.Error(err),
							zap.Uint("user_id", userID),
							zap.String("subscription_id", subscription.ID))
						// Continue with deactivation
					} else {
						g.logger.Info("cancellation credit issued",
							zap.Uint("user_id", userID),
							zap.String("subscription_id", subscription.ID),
							zap.String("credit_amount", creditAmount.String()))
					}
				}
			}

			// Update subscriber status in billing service
			if err := g.trackSubscriber(ctx, user.ID, subscription.Customer.ID, "", false, nil); err != nil {
				g.logger.Error("failed to deactivate subscriber",
					zap.Error(err),
					zap.Uint("user_id", userID),
					zap.String("customer_id", subscription.Customer.ID))
			}

			// Get plan ID for the event
			planID, hasPlan, err := findPlanIDFromSubscription(subscription)
			if err == nil && hasPlan {
				// Fire subscription cancelled event
				evt := billingEvent.NewSubscriptionCancelledEvent(
					ctx,
					user.ID,
					subscription.ID,
					GatewayID,
					planID,
				)
				core.Fire(g.coreCtx, billingEvent.EVENT_SUBSCRIPTION_CANCELLED, evt)
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

// pauseSubscription handles subscription pause - maintains the subscription record but marks as paused
func (g *StripeGateway) pauseSubscription(ctx context.Context, userID uint, subscription *stripe.Subscription, event stripe.Event) error {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.pauseSubscription")
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

			// Remove user from their current plan (pause = no quota access)
			if g.quota != nil {
				if err := g.quota.RemoveUserFromPlan(ctx, user.ID); err != nil {
					g.logger.Error("failed to remove user from plan",
						zap.Error(err),
						zap.Uint("user_id", user.ID))
				}
			}

			// Pause the subscriber in billing service
			if err := g.billing.PauseSubscriber(ctx, user.ID, GatewayID); err != nil {
				g.logger.Error("failed to pause subscriber",
					zap.Error(err),
					zap.Uint("user_id", userID))
			}

			g.logger.Debug("subscription paused - removed quota plan",
				zap.Uint("user_id", userID),
				zap.String("subscription_id", subscription.ID),
				zap.String("event_id", event.ID))

			return nil
		},
	)
}

// resumeSubscription handles subscription resume - reactivates a paused subscription
func (g *StripeGateway) resumeSubscription(ctx context.Context, userID uint, subscription *stripe.Subscription, event stripe.Event) error {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.resumeSubscription")
	defer span.End()

	return core.MetricTrack(
		nil,
		SubscriptionActivated.WithLabelValues(LabelStatusError),
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

			// Resume the subscriber in billing service
			if err := g.billing.ResumeSubscriber(ctx, user.ID, GatewayID); err != nil {
				g.logger.Error("failed to resume subscriber",
					zap.Error(err),
					zap.Uint("user_id", userID))
			}

			// Get period ID from subscription to re-assign quota
			periodID, hasPeriod, err := findPeriodIDFromSubscription(subscription)
			if err != nil {
				return err
			}

			if hasPeriod {
				// Fetch the pricing plan period to get the quota plan ID
				period, err := g.pricing.GetPricingPlanPeriod(ctx, periodID)
				if err != nil {
					g.logger.Error("failed to fetch pricing plan period for quota assignment",
						zap.Error(err),
						zap.Uint("pricing_plan_period_id", periodID))
				}

				// Assign quota plan if configured
				if g.quota != nil && period != nil && period.QuotaPlanID != 0 {
					if err := g.quota.AssignUserToPlan(ctx, user.ID, period.QuotaPlanID); err != nil {
						g.logger.Error("failed to assign user to quota plan after resume",
							zap.Error(err),
							zap.Uint("user_id", user.ID),
							zap.Uint("quota_plan_id", period.QuotaPlanID))
					}
				}
			}

			g.logger.Debug("subscription resumed - quota plan assigned",
				zap.Uint("user_id", userID),
				zap.String("subscription_id", subscription.ID),
				zap.String("event_id", event.ID))

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
func (g *StripeGateway) trackSubscriber(ctx context.Context, userID uint, externalID string, subscriptionID string, isActive bool, pricingPlanPeriodID *uint) error {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.trackSubscriber")
	defer span.End()

	if g.billing == nil {
		return nil // No billing service configured, nothing to track
	}

	if isActive {
		return g.billing.CreateOrUpdateSubscriber(ctx, userID, GatewayID, externalID, subscriptionID, isActive, pricingPlanPeriodID)
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
func (g *StripeGateway) GetCheckoutUI(ctx context.Context, userID uint, planID uint, periodID uint) (*pluginCore.CheckoutUIResponse, error) {
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

			// 3. Get pricing plan periods and find the specific period by ID
			periods, err := g.pricing.GetPricingPlanPeriods(ctx, planID)
			if err != nil {
				return nil, fmt.Errorf("failed to get pricing plan periods: %w", err)
			}
			if len(periods) == 0 {
				return nil, fmt.Errorf("no pricing plan periods found for plan")
			}

			// Find the specific period by ID
			var matchedPeriod *billingModels.PricingPlanPeriod
			for _, p := range periods {
				if p.ID == periodID {
					matchedPeriod = p
					break
				}
			}
			if matchedPeriod == nil {
				return nil, fmt.Errorf("period %d not found for plan %d", periodID, planID)
			}

			mapping, err := g.pricing.GetGatewayProductMapping(ctx, matchedPeriod.ID, GatewayID)
			if err != nil {
				return nil, fmt.Errorf("failed to get gateway product mapping: %w", err)
			}
			if mapping == nil || mapping.RemotePriceID == "" {
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

			// 6. Create checkout session with embedded UI
			priceID := mapping.RemotePriceID
			params := &stripe.CheckoutSessionCreateParams{
				UIMode: stripe.String(string(stripe.CheckoutSessionUIModeEmbeddedPage)),
				Mode:   stripe.String(stripe.CheckoutSessionModeSubscription),
				LineItems: []*stripe.CheckoutSessionCreateLineItemParams{
					{
						Price:    stripe.String(priceID),
						Quantity: stripe.Int64(1),
					},
				},
				Customer:          stripe.String(customerID),
				ClientReferenceID: stripe.String(strconv.FormatUint(uint64(userID), 10)),
				RedirectOnCompletion: stripe.String(string(stripe.CheckoutSessionRedirectOnCompletionIfRequired)),
				ReturnURL:            stripe.String(g.getCheckoutReturnURL()),
				AutomaticTax:         &stripe.CheckoutSessionCreateAutomaticTaxParams{Enabled: stripe.Bool(true)},
				CustomerUpdate: &stripe.CheckoutSessionCreateCustomerUpdateParams{
					Address: stripe.String("auto"),
				},
				AllowPromotionCodes: stripe.Bool(true),
			}

			session, err := g.stripeClient.V1CheckoutSessions().Create(ctx, params)
			if err != nil {
				g.logger.Error("failed to create checkout session",
					zap.Error(err),
					zap.Uint("user_id", userID),
					zap.Uint("plan_id", planID),
					zap.Uint("period_id", periodID))
				return nil, fmt.Errorf("failed to create checkout session: %w", err)
			}

			// 7. Build response with embedded checkout HTML fragment
			fragments, err := g.buildEmbeddedCheckoutFragment(session.ClientSecret)
			if err != nil {
				g.logger.Error("failed to build embedded checkout fragment",
					zap.Error(err),
					zap.String("session_id", session.ID))
				return nil, fmt.Errorf("failed to build checkout fragment: %w", err)
			}

			response := &pluginCore.CheckoutUIResponse{
				SessionID: session.ID,
				ExpiresAt: time.Unix(session.ExpiresAt, 0),
				Fragments: fragments,
			}

			g.logger.Debug("checkout session created",
				zap.String("session_id", session.ID),
				zap.Uint("user_id", userID),
				zap.Uint("plan_id", planID),
				zap.Uint("period_id", periodID),
			)

			return response, nil
		},
	)
}

// getCheckoutReturnURL returns the return URL for embedded checkout completion.
// In embedded mode this is primarily used for 3DS/fallback redirects — the user
// should land back at the subscription page so BillingContext can detect the completion.
func (g *StripeGateway) getCheckoutReturnURL() string {
	http := core.GetService[core.HTTPService](g.coreCtx, core.HTTP_SERVICE)
	secure := g.coreCtx.Config().Config().Core.Secure
	return gateway.BuildAbsoluteURL(http, gateway.DashboardPluginID, "/account/subscription?checkout_return=1&session_id={CHECKOUT_SESSION_ID}", secure)
}

// buildEmbeddedCheckoutFragment creates fragments for Stripe SDK, container HTML, and initialization script
func (g *StripeGateway) buildEmbeddedCheckoutFragment(clientSecret string) ([]pluginCore.CheckoutUIFragment, error) {
	data := EmbeddedCheckoutData{
		PublishableKey: g.publishableKey,
		ClientSecret:   clientSecret,
		Appearance:     "stripe",
	}

	// Execute template for the main JS
	tmpl, err := template.New("embeddedCheckout").ParseFS(templatesFS, "templates/embedded_checkout.tpl")
	if err != nil {
		return nil, fmt.Errorf("failed to parse template: %w", err)
	}
	var scriptBuf strings.Builder
	if err := tmpl.ExecuteTemplate(&scriptBuf, "embedded_checkout.tpl", data); err != nil {
		return nil, fmt.Errorf("failed to execute template: %w", err)
	}

	return []pluginCore.CheckoutUIFragment{
		{
			Type:   pluginCore.FragmentTypeScriptURL,
			Script: "https://js.stripe.com/dahlia/stripe.js",
		},
		{
			Type: pluginCore.FragmentTypeHTML,
			HTML: `<div id="stripe-checkout-container"><div id="stripe-checkout"></div></div>`,
		},
		{
			Type:   pluginCore.FragmentTypeScript,
			Script: scriptBuf.String(),
		},
	}, nil
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

	// Get pricing plan periods for this plan
	periods, err := g.pricing.GetPricingPlanPeriods(ctx, plan.ID)
	if err != nil {
		return &pluginCore.SyncResult{
			Success: false,
		}, fmt.Errorf("failed to get pricing plan periods: %w", err)
	}

	if len(periods) == 0 {
		g.logger.Warn("no pricing plan periods found for plan",
			zap.Uint("plan_id", plan.ID))
		return &pluginCore.SyncResult{
			Success:   true,
			ProductID: stripeProduct.ID,
		}, nil
	}

	// Create or update prices for each pricing plan period
	var remotePriceIDs []pluginCore.RemotePriceMapping
	for _, period := range periods {
		priceID, err := g.createOrUpdateStripePriceForPeriod(ctx, period, plan.Currency, stripeProduct.ID)
		if err != nil {
			return &pluginCore.SyncResult{
				Success: false,
			}, fmt.Errorf("failed to create/update price for period %d: %w", period.ID, err)
		}

		remotePriceIDs = append(remotePriceIDs, pluginCore.RemotePriceMapping{
			PricingPlanPeriodID: period.ID,
			PriceID:             priceID,
		})

		err = g.createOrUpdateGatewayProductMapping(ctx, plan.ID, period.ID, GatewayID, stripeProduct.ID, priceID, "")
		if err != nil {
			g.logger.Warn("failed to create/update gateway product mapping",
				zap.Uint("plan_id", plan.ID),
				zap.Uint("period_id", period.ID),
				zap.Error(err))
		}
	}

	g.logger.Info("successfully synced pricing plan to Stripe",
		zap.Uint("plan_id", plan.ID),
		zap.String("stripe_product_id", stripeProduct.ID),
		zap.Int("num_prices", len(remotePriceIDs)))

	// Set the default price on the product
	if len(remotePriceIDs) > 0 {
		defaultPriceID := g.pickDefaultPrice(periods, remotePriceIDs)
		_, err = g.stripeClient.V1Products().Update(ctx, stripeProduct.ID, &stripe.ProductUpdateParams{
			DefaultPrice: stripe.String(defaultPriceID),
		})
		if err != nil {
			g.logger.Warn("failed to set default price on product",
				zap.Uint("plan_id", plan.ID),
				zap.String("product_id", stripeProduct.ID),
				zap.String("price_id", defaultPriceID),
				zap.Error(err))
		} else {
			g.logger.Debug("set default price on product",
				zap.Uint("plan_id", plan.ID),
				zap.String("product_id", stripeProduct.ID),
				zap.String("price_id", defaultPriceID))
		}
	}

	// Create or update portal configuration if plan has upgrade/downgrade paths
	var portalConfigID string
	if priceLineID > 0 && (len(upgradePlanIDs) > 0 || len(downgradePlanIDs) > 0) {
		// Build list of price IDs from remotePriceIDs
		var priceIDs []string
		for _, mapping := range remotePriceIDs {
			priceIDs = append(priceIDs, mapping.PriceID)
		}

		if len(priceIDs) > 0 {
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
		Success:               true,
		ProductID:             stripeProduct.ID,
		PortalConfigurationID: portalConfigID,
		RemotePriceIDs:        remotePriceIDs,
	}, nil
}

// pickDefaultPrice selects the default price ID based on configured cadence preference
func (g *StripeGateway) pickDefaultPrice(periods []*billingModels.PricingPlanPeriod, priceIDs []pluginCore.RemotePriceMapping) string {
	// Get the configured default cadence, fallback to monthly
	preferredCadence := g.defaultPriceCadence
	if preferredCadence == "" {
		preferredCadence = string(subscription.CadenceMonthly)
	}

	// Find the price matching the preferred cadence
	for i, period := range periods {
		if period.Cadence == preferredCadence && i < len(priceIDs) {
			return priceIDs[i].PriceID
		}
	}

	// Fallback to first price if preferred cadence not found
	if len(priceIDs) > 0 {
		return priceIDs[0].PriceID
	}

	return ""
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

// createOrUpdateStripePriceForPeriod creates or updates a Stripe price for a pricing plan period
func (g *StripeGateway) createOrUpdateStripePriceForPeriod(ctx context.Context, period *billingModels.PricingPlanPeriod, currency string, productID string) (string, error) {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.createOrUpdateStripePriceForPeriod")
	defer span.End()

	// Map cadence to Stripe interval
	var interval stripe.PriceRecurringInterval
	var intervalCount *int64

	switch period.Cadence {
	case "monthly":
		interval = stripe.PriceRecurringIntervalMonth
	case "yearly":
		interval = stripe.PriceRecurringIntervalYear
	case "quarterly":
		interval = stripe.PriceRecurringIntervalMonth
		count := int64(3)
		intervalCount = &count
	case "weekly":
		interval = stripe.PriceRecurringIntervalWeek
	case "rolling":
		return "", fmt.Errorf("rolling periods not supported by Stripe")
	default:
		return "", fmt.Errorf("unsupported cadence '%s' for Stripe", period.Cadence)
	}

	// Convert amount to cents (Stripe uses smallest currency unit)
	amountCents := int64(period.PriceUSD * 100)

	priceParams := &stripe.PriceCreateParams{
		Currency:   stripe.String(currency),
		UnitAmount: stripe.Int64(amountCents),
		Product:    stripe.String(productID),
		Recurring: &stripe.PriceCreateRecurringParams{
			Interval: stripe.String(string(interval)),
		},
		Metadata: map[string]string{
			"period_id": strconv.FormatUint(uint64(period.ID), 10),
		},
	}

	// Add interval_count for quarterly (every 3 months)
	if intervalCount != nil {
		priceParams.Recurring.IntervalCount = stripe.Int64(*intervalCount)
	}

	// Create the price using the gateway's client
	price, err := g.stripeClient.V1Prices().Create(ctx, priceParams)
	if err != nil {
		return "", fmt.Errorf("failed to create Stripe price: %w", err)
	}

	g.logger.Debug("created Stripe price for period",
		zap.Uint("plan_id", period.PricingPlanID),
		zap.Uint("period_id", period.ID),
		zap.String("cadence", period.Cadence),
		zap.String("price_id", price.ID),
		zap.Int64("amount_cents", amountCents))

	return price.ID, nil
}

// createOrUpdateGatewayProductMapping creates or updates a gateway product mapping for a pricing plan period
func (g *StripeGateway) createOrUpdateGatewayProductMapping(ctx context.Context, planID uint, periodID uint, gatewayType string, productID string, priceID string, portalConfigID string) error {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.createOrUpdateGatewayProductMapping")
	defer span.End()

	if g.pricing == nil {
		return fmt.Errorf("pricing service not configured")
	}

	// Check if a mapping already exists for this period and gateway
	mappings, err := g.pricing.GetGatewayProductMappingsByPlan(ctx, planID)
	if err != nil {
		g.logger.Warn("failed to get existing gateway product mappings",
			zap.Uint("plan_id", planID),
			zap.Error(err))
	}

	var existingMapping *billingModels.GatewayProductMapping
	for _, mapping := range mappings {
		if mapping.GatewayType == gatewayType && mapping.PricingPlanPeriodID != nil && *mapping.PricingPlanPeriodID == periodID {
			existingMapping = mapping
			break
		}
	}

	if existingMapping != nil {
		// Update existing mapping
		now := time.Now()
		existingMapping.RemoteProductID = productID
		existingMapping.RemotePriceID = priceID
		if portalConfigID != "" {
			existingMapping.PortalConfigurationID = &portalConfigID
		}
		existingMapping.SyncStatus = "synced"
		existingMapping.LastSyncedAt = &now
		existingMapping.ErrorMessage = ""
		existingMapping.Retries = 0

		err = g.pricing.UpdateGatewayProductMapping(ctx, existingMapping.ID, existingMapping)
		if err != nil {
			return fmt.Errorf("failed to update gateway product mapping: %w", err)
		}

		g.logger.Debug("updated gateway product mapping",
			zap.Uint("plan_id", planID),
			zap.Uint("period_id", periodID),
			zap.String("product_id", productID),
			zap.String("price_id", priceID))
	} else {
		// Create new mapping
		var portalConfigPtr *string
		if portalConfigID != "" {
			portalConfigPtr = &portalConfigID
		}

		newMapping := &billingModels.GatewayProductMapping{
			PricingPlanPeriodID:   &periodID,
			GatewayType:           gatewayType,
			RemoteProductID:       productID,
			RemotePriceID:         priceID,
			PortalConfigurationID: portalConfigPtr,
			SyncStatus:            "synced",
		}

		err = g.pricing.CreateGatewayProductMapping(ctx, newMapping)
		if err != nil {
			return fmt.Errorf("failed to create gateway product mapping: %w", err)
		}

		g.logger.Debug("created gateway product mapping",
			zap.Uint("plan_id", planID),
			zap.Uint("period_id", periodID),
			zap.String("product_id", productID),
			zap.String("price_id", priceID))
	}

	return nil
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
				Enabled:               &enabled,
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
// by fetching gateway product mappings and extracting price IDs.
// With the flexible pricing architecture, each plan can have multiple periods,
// and each period has a single price ID.
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

	// For each plan, get its periods and their associated price IDs
	for planIDStr := range allPlanIDs {
		var planID uint
		_, err := fmt.Sscanf(planIDStr, "%d", &planID)
		if err != nil {
			g.logger.Warn("invalid plan ID format", zap.String("plan_id", planIDStr), zap.Error(err))
			continue
		}

		// If pricing service is available, query pricing plan periods
		if g.pricing != nil {
			periods, err := g.pricing.GetPricingPlanPeriods(ctx, planID)
			if err != nil {
				g.logger.Debug("could not get pricing plan periods for plan",
					zap.Uint("plan_id", planID),
					zap.Error(err))
				continue
			}

			// For each period, get the gateway product mapping and extract price ID
			for _, period := range periods {
				mapping, err := g.pricing.GetGatewayProductMapping(ctx, period.ID, GatewayID)
				if err != nil {
					g.logger.Debug("could not get gateway mapping for period",
						zap.Uint("plan_id", planID),
						zap.Uint("period_id", period.ID),
						zap.Error(err))
					continue
				}

				// Add price ID if available
				if mapping != nil && mapping.RemotePriceID != "" {
					priceIDSet[mapping.RemotePriceID] = true
				}
			}
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

	// User operations: portal-based management (via customer portal deep link)
	userOperations := map[pluginCore.ManagementOperation]bool{
		pluginCore.OperationCancel:     true,
		pluginCore.OperationChangePlan: true,
		pluginCore.OperationPause:      true,
		pluginCore.OperationResume:     true,
	}

	// Admin operations: backend API calls (includes pause/resume for direct admin control)
	adminOperations := map[pluginCore.ManagementOperation]bool{
		pluginCore.OperationCancel:     true,
		pluginCore.OperationChangePlan: true,
		pluginCore.OperationPause:      true,
		pluginCore.OperationResume:     true,
	}

	return &pluginCore.ManagementCapabilities{
		ManagementMode:  pluginCore.ModePortal,
		Operations:      userOperations,
		AdminOperations: adminOperations,
	}, nil
}

// getActiveOrPausedSubscription returns the active subscription or paused subscription
// for a user if checkPaused is true. It validates the subscription belongs to Stripe.
func (g *StripeGateway) getActiveOrPausedSubscription(ctx context.Context, userID uint, checkPaused bool) (*pluginCore.Subscriber, error) {
	subscriber, err := g.billing.GetActiveSubscription(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get active subscription: %w", err)
	}
	if (subscriber == nil || subscriber.GatewayType != GatewayID) && checkPaused {
		subscriber, err = g.billing.GetPausedSubscription(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("failed to get paused subscription: %w", err)
		}
	}
	if subscriber == nil || subscriber.GatewayType != GatewayID {
		return nil, fmt.Errorf("no active or paused stripe subscription found for user %d", userID)
	}
	return subscriber, nil
}

// GetManagementURL returns the appropriate action for a management operation
// using Stripe's portal deep linking to direct users straight to the relevant action page.
func (g *StripeGateway) GetManagementURL(ctx context.Context, userID uint, operation pluginCore.ManagementOperation) (*pluginCore.ManagementResult, error) {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.GetManagementURL")
	defer span.End()

	if g.billing == nil {
		return nil, fmt.Errorf("billing service not configured")
	}

	subscriber, err := g.getActiveOrPausedSubscription(ctx, userID, operation == pluginCore.OperationResume)
	if err != nil {
		return nil, err
	}

	// Build deep link flow data based on the requested operation
	flowData := g.buildFlowData(operation, subscriber)

	// Create a portal session with deep link flow
	// Check for paused subscriptions only for OperationResume
	portalURL, err := g.createPortalSession(ctx, userID, "", operation == pluginCore.OperationResume, flowData)
	if err != nil {
		return nil, fmt.Errorf("failed to create portal session with deep link: %w", err)
	}

	g.logger.Debug("created deep-linked portal session",
		zap.String("operation", string(operation)),
		zap.String("subscription_id", subscriber.SubscriptionID),
		zap.Uint("user_id", userID))

	return &pluginCore.ManagementResult{
		Action: pluginCore.ActionRedirect,
		URL:    portalURL,
	}, nil
}

// buildFlowData maps a management operation to the corresponding Stripe portal deep link flow.
// Returns nil for operations that have no Stripe deep link equivalent (e.g., pause/resume),
// which results in a generic portal session.
func (g *StripeGateway) buildFlowData(operation pluginCore.ManagementOperation, subscriber *billingModels.Subscriber) *stripe.BillingPortalSessionCreateFlowDataParams {
	switch operation {
	case pluginCore.OperationCancel:
		return &stripe.BillingPortalSessionCreateFlowDataParams{
			Type: stripe.String(string(stripe.BillingPortalSessionFlowTypeSubscriptionCancel)),
			SubscriptionCancel: &stripe.BillingPortalSessionCreateFlowDataSubscriptionCancelParams{
				Subscription: stripe.String(subscriber.SubscriptionID),
			},
		}

	case pluginCore.OperationChangePlan:
		return &stripe.BillingPortalSessionCreateFlowDataParams{
			Type: stripe.String(string(stripe.BillingPortalSessionFlowTypeSubscriptionUpdate)),
			SubscriptionUpdate: &stripe.BillingPortalSessionCreateFlowDataSubscriptionUpdateParams{
				Subscription: stripe.String(subscriber.SubscriptionID),
			},
		}

	default:
		// Pause/resume and other unsupported operations fall back to the generic portal
		return nil
	}
}

// extractProrationFromInvoice extracts proration details from Stripe invoice line items
// Following the pattern from stripe_preview_test.go's calculateProratedAmounts()
func (g *StripeGateway) extractProrationFromInvoice(invoice *stripe.Invoice) (*InvoiceProrationAnalysis, error) {
	analysis := &InvoiceProrationAnalysis{
		InvoiceID:      invoice.ID,
		TotalLineItems: 0,
	}

	if invoice.Lines == nil {
		return analysis, nil
	}

	analysis.TotalLineItems = len(invoice.Lines.Data)

	for _, line := range invoice.Lines.Data {
		// Check if this is a prorated line item
		isProrated := line.Parent != nil &&
			line.Parent.SubscriptionItemDetails != nil &&
			line.Parent.SubscriptionItemDetails.Proration

		if isProrated {
			analysis.HasProratedItems = true
			if line.Amount >= 0 {
				analysis.ProrationChargeTotal += line.Amount
			} else {
				analysis.ProrationCreditTotal += line.Amount
			}
		}
	}

	// Calculate net proration in dollars
	if analysis.HasProratedItems {
		analysis.NetProrationDollars = decimal.NewFromInt(analysis.ProrationChargeTotal + analysis.ProrationCreditTotal).Div(decimal.NewFromInt(100))
	}

	return analysis, nil
}

// compareProrationCalculations compares local proration calculation with Stripe's invoice amount
// and returns a recommendation on which to use.
func (g *StripeGateway) compareProrationCalculations(
	ctx context.Context,
	userID uint,
	oldPrice, newPrice subscription.Price,
	oldCycle subscription.BillingCycle,
	stripeAmount decimal.Decimal,
	invoice *stripe.Invoice,
) (*ProrationComparison, error) {

	// Use invoice creation time for deterministic proration calculation
	prorationTime := time.Now()
	if invoice != nil && invoice.Created > 0 {
		prorationTime = time.Unix(invoice.Created, 0)
	}

	// Calculate local proration using our subscription package
	localResult, err := subscription.ProratedChange(
		oldPrice,
		newPrice,
		oldCycle,
		prorationTime,
		subscription.ProrationBehaviorCreateProrations,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate local proration: %w", err)
	}

	// Get local net amount
	localNet := subscription.NetResult(localResult)

	// Calculate difference and percentage
	difference := stripeAmount.Sub(localNet)
	averageAmount := localNet.Add(stripeAmount)
	differencePercent := 0.0
	if !averageAmount.IsZero() {
		differencePercent = difference.Abs().Div(averageAmount).Mul(decimal.NewFromInt(100)).InexactFloat64()
	}

	// Determine if mismatch is significant (any non-zero difference)
	mismatchDetected := !difference.IsZero()

	comparison := &ProrationComparison{
		LocalResult:       &localResult,
		StripeAmount:      stripeAmount,
		MismatchDetected:  mismatchDetected,
		Difference:        difference,
		DifferencePercent: differencePercent,
		InvoiceAnalysis:   nil,
	}

	// Decide which to use
	if mismatchDetected {
		comparison.RecommendedAction = "use_stripe"
		g.logProrationMismatch(ctx, userID, comparison, oldCycle)
	} else {
		comparison.RecommendedAction = "use_local"
	}

	return comparison, nil
}

// logProrationMismatch logs detailed information about proration calculation mismatches
func (g *StripeGateway) logProrationMismatch(
	ctx context.Context,
	userID uint,
	comparison *ProrationComparison,
	oldCycle subscription.BillingCycle,
) {
	g.logger.Warn("proration calculation mismatch detected",
		zap.Uint("user_id", userID),
		zap.String("local_net_amount", subscription.NetResult(*comparison.LocalResult).String()),
		zap.String("stripe_net_amount", comparison.StripeAmount.String()),
		zap.String("difference", comparison.Difference.String()),
		zap.Float64("difference_percent", comparison.DifferencePercent),
		zap.String("recommended_action", comparison.RecommendedAction),
		zap.String("local_unused_credit", comparison.LocalResult.UnusedCredit.String()),
		zap.String("local_new_charge", comparison.LocalResult.NewCharge.String()),
		zap.String("billing_cycle_start", oldCycle.StartAt.Format(time.RFC3339)),
		zap.String("billing_cycle_end", oldCycle.EndAt.Format(time.RFC3339)),
	)
}

// calculateCancellationCredit computes the credit amount for a subscription cancellation
// using local proration logic (subscription.UnusedPeriodValue).
//
// Timestamp Priority (for deterministic, testable behavior):
//  1. subscription.EndedAt - When Stripe actually terminated the subscription
//  2. subscription.CanceledAt - When cancellation was requested
//  3. event.Created - When Stripe created the webhook event
//  4. time.Now() - Fallback only when no Stripe timestamp available
//
// Using Stripe-provided timestamps ensures:
//   - Deterministic results for e2e testing (set timestamps in mock data)
//   - Accurate credit calculation aligned with Stripe's view of cancellation time
//   - No need for fake time packages in tests
func (g *StripeGateway) calculateCancellationCredit(
	ctx context.Context,
	userID uint,
	stripeSubscription *stripe.Subscription,
	event stripe.Event,
) (decimal.Decimal, error) {

	// Get current subscriber state
	subscriber, err := g.billing.GetActiveSubscriber(ctx, userID, GatewayID)
	if err != nil {
		return decimal.Zero, fmt.Errorf("failed to get subscriber: %w", err)
	}
	if subscriber == nil {
		return decimal.Zero, fmt.Errorf("no active subscriber found")
	}

	// Get billing cycle from Stripe subscription
	// CurrentPeriodStart and CurrentPeriodEnd are on the subscription items
	var currentPeriodStart, currentPeriodEnd time.Time
	if stripeSubscription.Items != nil && len(stripeSubscription.Items.Data) > 0 && stripeSubscription.Items.Data[0] != nil {
		currentPeriodStart = time.Unix(stripeSubscription.Items.Data[0].CurrentPeriodStart, 0)
		currentPeriodEnd = time.Unix(stripeSubscription.Items.Data[0].CurrentPeriodEnd, 0)
	}
	billingCycle := subscription.BillingCycle{
		StartAt: currentPeriodStart,
		EndAt:   currentPeriodEnd,
	}

	// Get old plan details from pricing plan period
	var oldPrice subscription.Price
	if subscriber.PricingPlanPeriodID != nil {
		period, err := g.pricing.GetPricingPlanPeriod(ctx, *subscriber.PricingPlanPeriodID)
		if err != nil {
			return decimal.Zero, fmt.Errorf("failed to get pricing plan period: %w", err)
		}
		if period == nil {
			return decimal.Zero, fmt.Errorf("pricing plan period not found")
		}

		oldPrice = subscription.Price{
			Amount:  decimal.NewFromFloat(period.PriceUSD),
			Cadence: subscription.Cadence(period.Cadence),
		}
	} else {
		g.logger.Warn("no pricing plan period for cancellation credit, returning zero",
			zap.Uint("user_id", userID),
			zap.String("subscription_id", stripeSubscription.ID))
		return decimal.Zero, nil
	}

	// Determine cancellation time using Stripe-provided timestamps
	// Priority: EndedAt > CanceledAt > event.Created > time.Now()
	var cancellationTime time.Time
	switch {
	case stripeSubscription.EndedAt > 0:
		// Subscription was actually terminated - use this as most accurate
		cancellationTime = time.Unix(stripeSubscription.EndedAt, 0)
	case stripeSubscription.CanceledAt > 0:
		// Cancellation was requested but may not have taken effect yet
		cancellationTime = time.Unix(stripeSubscription.CanceledAt, 0)
	case event.Created > 0:
		// Webhook event creation time
		cancellationTime = time.Unix(event.Created, 0)
	default:
		// Last resort fallback - should not happen with real Stripe webhooks
		cancellationTime = time.Now()
	}

	// Calculate local proration using existing subscription package
	localCredit := subscription.UnusedPeriodValue(oldPrice, billingCycle, cancellationTime)

	// Stripe typically doesn't automatically issue credits on cancellation
	// The customer just isn't billed again. We issue a credit to the ledger.
	return localCredit, nil
}

// determineOperationType determines what type of operation triggered this invoice
func (g *StripeGateway) determineOperationType(
	ctx context.Context,
	currentSubscriber *billingModels.Subscriber,
	subscription *stripe.Subscription,
	invoice *stripe.Invoice,
) pluginCore.SubscriptionChangeType {
	if currentSubscriber == nil {
		return pluginCore.ChangeTypeNewSubscription
	}

	// Check for plan changes
	if currentSubscriber.PricingPlanPeriodID != nil {
		newPlanPeriodID, found, err := findPeriodIDFromSubscription(subscription)
		if err == nil && found {
			if *currentSubscriber.PricingPlanPeriodID != newPlanPeriodID {
				// Plan changed - determine upgrade or downgrade
				return g.determineUpgradeOrDowngrade(ctx, *currentSubscriber.PricingPlanPeriodID, newPlanPeriodID)
			}
		}
	}

	return pluginCore.ChangeTypeRenewal
}

// determineUpgradeOrDowngrade determines if a plan change is an upgrade or downgrade
func (g *StripeGateway) determineUpgradeOrDowngrade(
	ctx context.Context,
	oldPlanPeriodID uint,
	newPlanPeriodID uint,
) pluginCore.SubscriptionChangeType {
	// Get old plan period
	oldPeriod, err := g.pricing.GetPricingPlanPeriod(ctx, oldPlanPeriodID)
	if err != nil {
		g.logger.Warn("failed to get old pricing plan period, defaulting to upgrade",
			zap.Error(err),
			zap.Uint("old_plan_period_id", oldPlanPeriodID))
		return pluginCore.ChangeTypeUpgrade
	}

	// Get new plan period
	newPeriod, err := g.pricing.GetPricingPlanPeriod(ctx, newPlanPeriodID)
	if err != nil {
		g.logger.Warn("failed to get new pricing plan period, defaulting to upgrade",
			zap.Error(err),
			zap.Uint("new_plan_period_id", newPlanPeriodID))
		return pluginCore.ChangeTypeUpgrade
	}

	// Compare prices
	if newPeriod.PriceUSD > oldPeriod.PriceUSD {
		return pluginCore.ChangeTypeUpgrade
	} else if newPeriod.PriceUSD < oldPeriod.PriceUSD {
		return pluginCore.ChangeTypeDowngrade
	}

	// Same price, treat as upgrade
	return pluginCore.ChangeTypeUpgrade
}

// validateAndCalculateCreditAmount validates the credit amount using local logic
// and returns the amount to actually credit (matching Stripe if mismatch)
func (g *StripeGateway) validateAndCalculateCreditAmount(
	ctx context.Context,
	userID uint,
	operation pluginCore.SubscriptionChangeType,
	currentSubscriber *billingModels.Subscriber,
	stripeSubscription *stripe.Subscription,
	invoice *stripe.Invoice,
) (decimal.Decimal, error) {

	stripeAmount := g.calculateNetInvoiceAmount(invoice)

	switch operation {
	case pluginCore.ChangeTypeNewSubscription, pluginCore.ChangeTypeRenewal:
		// No local calculation needed - use Stripe's amount directly
		// But still validate ledger
		if err := g.credit.ValidateSubscriptionChange(ctx, uint64(userID), operation, stripeAmount); err != nil {
			return decimal.Zero, err
		}
		return stripeAmount, nil

	case pluginCore.ChangeTypeUpgrade, pluginCore.ChangeTypeDowngrade:
		// Get old and new plan details using subscription types
		var oldPrice, newPrice subscription.Price
		var oldCycle subscription.BillingCycle

		// Populate old price and cycle from current subscriber
		if currentSubscriber.PricingPlanPeriodID != nil {
			period, err := g.pricing.GetPricingPlanPeriod(ctx, *currentSubscriber.PricingPlanPeriodID)
			if err != nil {
				return decimal.Zero, fmt.Errorf("failed to get old pricing plan period: %w", err)
			}
			oldPrice = subscription.Price{
				Amount:  decimal.NewFromFloat(period.PriceUSD),
				Cadence: subscription.Cadence(period.Cadence),
			}
		}

		// Populate new price from subscription
		newPlanPeriodID, found, err := findPeriodIDFromSubscription(stripeSubscription)
		if !found || err != nil {
			return stripeAmount, nil // Can't compare, use Stripe's amount
		}

		newPeriod, err := g.pricing.GetPricingPlanPeriod(ctx, newPlanPeriodID)
		if err != nil {
			return stripeAmount, nil
		}
		newPrice = subscription.Price{
			Amount:  decimal.NewFromFloat(newPeriod.PriceUSD),
			Cadence: subscription.Cadence(newPeriod.Cadence),
		}

		// Get billing cycle from Stripe subscription
		// CurrentPeriodStart and CurrentPeriodEnd are on the subscription items
		var currentPeriodStart, currentPeriodEnd time.Time
		if stripeSubscription.Items != nil && len(stripeSubscription.Items.Data) > 0 && stripeSubscription.Items.Data[0] != nil {
			currentPeriodStart = time.Unix(stripeSubscription.Items.Data[0].CurrentPeriodStart, 0)
			currentPeriodEnd = time.Unix(stripeSubscription.Items.Data[0].CurrentPeriodEnd, 0)
		}
		oldCycle = subscription.BillingCycle{
			StartAt: currentPeriodStart,
			EndAt:   currentPeriodEnd,
		}

		// Compare calculations
		comparison, err := g.compareProrationCalculations(ctx, userID, oldPrice, newPrice, oldCycle, stripeAmount, invoice)
		if err != nil {
			g.logger.Warn("failed to compare proration calculations, using Stripe amount",
				zap.Uint("user_id", userID),
				zap.Error(err))
			return stripeAmount, nil
		}

		// Validate ledger
		if err := g.credit.ValidateSubscriptionChange(ctx, uint64(userID), operation, comparison.StripeAmount); err != nil {
			return decimal.Zero, err
		}

		// Use Stripe's amount
		return comparison.StripeAmount, nil

	default:
		return stripeAmount, nil
	}
}

// Compile-time interface checks
var (
	_ pluginCore.GatewayIdentity      = (*StripeGateway)(nil)
	_ pluginCore.WebhookHandler       = (*StripeGateway)(nil)
	_ pluginCore.CustomerPortal       = (*StripeGateway)(nil)
	_ pluginCore.CheckoutProvider     = (*StripeGateway)(nil)
	_ pluginCore.GatewayCapabilities  = (*StripeGateway)(nil)
	_ pluginCore.GatewaySync          = (*StripeGateway)(nil)
	_ pluginCore.SubscriptionManager  = (*StripeGateway)(nil)
	_ pluginCore.SubscriptionExecutor = (*StripeGateway)(nil) // Admin backend operations
)

// ExecuteCancel cancels a subscription through Stripe's API.
// Both immediate and scheduled cancellations rely on the customer.subscription.deleted
// webhook to handle deactivation, credit, and event firing — keeping logic DRY.
func (g *StripeGateway) ExecuteCancel(ctx context.Context, userID uint, immediate bool) (*pluginCore.CancellationResult, error) {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.ExecuteCancel")
	defer span.End()

	if g.billing == nil {
		return nil, fmt.Errorf("billing service not configured")
	}

	// Get active subscription
	subscriber, err := g.billing.GetActiveSubscription(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get active subscription: %w", err)
	}
	if subscriber == nil || subscriber.GatewayType != GatewayID {
		return nil, fmt.Errorf("no active stripe subscription found for user %d", userID)
	}

	if immediate {
		return g.executeImmediateCancel(ctx, userID, subscriber)
	} else {
		return g.executeScheduledCancel(ctx, userID, subscriber)
	}
}

// executeImmediateCancel cancels a subscription immediately using Stripe's Cancel API.
// Stripe fires customer.subscription.deleted upon cancellation, which the webhook handler
// processes via deactivateSubscription (deactivation, credit, events, etc.).
func (g *StripeGateway) executeImmediateCancel(ctx context.Context, userID uint, subscriber *billingModels.Subscriber) (*pluginCore.CancellationResult, error) {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.executeImmediateCancel")
	defer span.End()

	params := &stripe.SubscriptionCancelParams{}
	_, err := g.stripeClient.V1Subscriptions().Cancel(ctx, subscriber.SubscriptionID, params)
	if err != nil {
		g.logger.Error("failed to cancel subscription immediately via Stripe API",
			zap.Uint("user_id", userID),
			zap.String("subscription_id", subscriber.SubscriptionID),
			zap.Error(err))
		return nil, fmt.Errorf("failed to cancel subscription: %w", err)
	}

	effectiveAt := time.Now()
	g.logger.Info("Cancelled subscription immediately via Stripe API",
		zap.Uint("user_id", userID),
		zap.String("subscription_id", subscriber.SubscriptionID),
		zap.Time("effective_at", effectiveAt))

	return &pluginCore.CancellationResult{
		Status:      pluginCore.CancellationStatusImmediate,
		EffectiveAt: &effectiveAt,
		CanAbort:    false,
	}, nil
}

// executeScheduledCancel schedules cancellation at the end of the billing period using Stripe's Update API.
func (g *StripeGateway) executeScheduledCancel(ctx context.Context, userID uint, subscriber *billingModels.Subscriber) (*pluginCore.CancellationResult, error) {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.executeScheduledCancel")
	defer span.End()

	// Schedule cancellation at period end via Update
	params := &stripe.SubscriptionUpdateParams{
		CancelAtPeriodEnd: stripe.Bool(true),
	}

	updatedSub, err := g.stripeClient.V1Subscriptions().Update(ctx, subscriber.SubscriptionID, params)
	if err != nil {
		g.logger.Error("failed to schedule cancellation via Stripe API",
			zap.Uint("user_id", userID),
			zap.String("subscription_id", subscriber.SubscriptionID),
			zap.Error(err))
		return nil, fmt.Errorf("failed to cancel subscription: %w", err)
	}

	// Calculate effective time (end of current billing period)
	// CurrentPeriodEnd is on subscription items in the v83 SDK
	var effectiveAt *time.Time
	if len(updatedSub.Items.Data) > 0 && updatedSub.Items.Data[0].CurrentPeriodEnd > 0 {
		t := time.Unix(updatedSub.Items.Data[0].CurrentPeriodEnd, 0)
		effectiveAt = &t
	}

	if effectiveAt != nil {
		g.logger.Info("Scheduled subscription cancellation via Stripe API",
			zap.Uint("user_id", userID),
			zap.String("subscription_id", subscriber.SubscriptionID),
			zap.Time("effective_at", *effectiveAt))
	} else {
		g.logger.Info("Scheduled subscription cancellation via Stripe API",
			zap.Uint("user_id", userID),
			zap.String("subscription_id", subscriber.SubscriptionID))
	}

	return &pluginCore.CancellationResult{
		Status:      pluginCore.CancellationStatusScheduled,
		EffectiveAt: effectiveAt,
		CanAbort:    true, // Can be aborted by updating subscription with CancelAtPeriodEnd=false
	}, nil
}

// AbortCancellation reverses a scheduled subscription cancellation.
// This removes the cancel_at_period_end flag from the subscription via Update.
func (g *StripeGateway) AbortCancellation(ctx context.Context, userID uint) error {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.AbortCancellation")
	defer span.End()

	if g.billing == nil {
		return fmt.Errorf("billing service not configured")
	}

	// Get active subscription
	subscriber, err := g.billing.GetActiveSubscription(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to get active subscription: %w", err)
	}
	if subscriber == nil || subscriber.GatewayType != GatewayID {
		return fmt.Errorf("no active stripe subscription found for user %d", userID)
	}

	// Remove cancel_at_period_end by updating subscription
	params := &stripe.SubscriptionUpdateParams{
		CancelAtPeriodEnd: stripe.Bool(false),
	}

	_, err = g.stripeClient.V1Subscriptions().Update(ctx, subscriber.SubscriptionID, params)
	if err != nil {
		g.logger.Error("failed to abort cancellation via Stripe API",
			zap.Uint("user_id", userID),
			zap.String("subscription_id", subscriber.SubscriptionID),
			zap.Error(err))
		return fmt.Errorf("failed to abort cancellation: %w", err)
	}

	g.logger.Info("Aborted scheduled cancellation via Stripe API",
		zap.Uint("user_id", userID),
		zap.String("subscription_id", subscriber.SubscriptionID))

	return nil
}

// ReconcileCancellation is a no-op for Stripe as cancellations are handled via webhooks.
// Stripe sends customer.subscription.deleted webhook when the cancellation takes effect.
func (g *StripeGateway) ReconcileCancellation(ctx context.Context, userID uint) error {
	// Stripe handles cancellation finalization via webhooks
	// No action needed here
	return nil
}

// ExecutePause pauses a subscription through Stripe's API.
// Admin can directly pause subscriptions via the Stripe API.
func (g *StripeGateway) ExecutePause(ctx context.Context, userID uint) error {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.ExecutePause")
	defer span.End()

	if g.billing == nil {
		return fmt.Errorf("billing service not configured")
	}

	// Get active subscription
	subscriber, err := g.billing.GetActiveSubscription(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to get active subscription: %w", err)
	}
	if subscriber == nil || subscriber.GatewayType != GatewayID {
		return fmt.Errorf("no active stripe subscription found for user %d", userID)
	}

	if subscriber.SubscriptionID == "" {
		return fmt.Errorf("no stripe subscription ID found for user %d", userID)
	}

	// Pause the subscription via Stripe API
	params := &stripe.SubscriptionUpdateParams{
		PauseCollection: &stripe.SubscriptionUpdatePauseCollectionParams{
			Behavior: stripe.String("mark_uncollectible"),
		},
	}

	_, err = g.stripeClient.V1Subscriptions().Update(ctx, subscriber.SubscriptionID, params)
	if err != nil {
		g.logger.Error("failed to pause subscription via Stripe API",
			zap.Uint("user_id", userID),
			zap.String("subscription_id", subscriber.SubscriptionID),
			zap.Error(err))
		return fmt.Errorf("failed to pause subscription: %w", err)
	}

	g.logger.Info("Paused subscription via Stripe API",
		zap.Uint("user_id", userID),
		zap.String("subscription_id", subscriber.SubscriptionID))

	return nil
}

// ExecuteResume resumes a paused subscription through Stripe's API.
// Admin can directly resume subscriptions via the Stripe API.
func (g *StripeGateway) ExecuteResume(ctx context.Context, userID uint) error {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.ExecuteResume")
	defer span.End()

	if g.billing == nil {
		return fmt.Errorf("billing service not configured")
	}

	subscriber, err := g.getActiveOrPausedSubscription(ctx, userID, true)
	if err != nil {
		return err
	}

	if subscriber.SubscriptionID == "" {
		return fmt.Errorf("no stripe subscription ID found for user %d", userID)
	}

	// Resume the subscription via Stripe API by clearing pause_collection
	// Set PauseCollection to empty struct with empty behavior to clear the pause
	params := &stripe.SubscriptionUpdateParams{
		PauseCollection: &stripe.SubscriptionUpdatePauseCollectionParams{},
	}

	_, err = g.stripeClient.V1Subscriptions().Update(ctx, subscriber.SubscriptionID, params)
	if err != nil {
		g.logger.Error("failed to resume subscription via Stripe API",
			zap.Uint("user_id", userID),
			zap.String("subscription_id", subscriber.SubscriptionID),
			zap.Error(err))
		return fmt.Errorf("failed to resume subscription: %w", err)
	}

	g.logger.Info("Resumed subscription via Stripe API",
		zap.Uint("user_id", userID),
		zap.String("subscription_id", subscriber.SubscriptionID))

	return nil
}

// ExecutePlanChange executes a plan change via Stripe's API.
// Unlike Atlos, Stripe handles proration automatically when updating the subscription item.
// The webhook (invoice.paid) will handle credit/debit issuance based on Stripe's calculation.
// DB changes are managed via webhooks - we only trigger the API call.
func (g *StripeGateway) ExecutePlanChange(
	ctx context.Context,
	userID uint,
	newPeriodID uint,
) (*pluginCore.PlanChangeResult, error) {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.ExecutePlanChange")
	defer span.End()

	// 1. Validate the new pricing plan period
	newPeriod, err := g.pricing.GetPricingPlanPeriod(ctx, newPeriodID)
	if err != nil {
		return nil, fmt.Errorf("failed to get new pricing plan period: %w", err)
	}
	if newPeriod == nil {
		return nil, fmt.Errorf("new pricing plan period not found")
	}

	// 2. Verify the new plan is active
	plan, err := g.pricing.GetPricingPlan(ctx, newPeriod.PricingPlanID)
	if err != nil {
		return nil, fmt.Errorf("failed to get pricing plan: %w", err)
	}
	if plan == nil || !plan.IsActive {
		return nil, fmt.Errorf("new plan is not active")
	}

	// 3. Get the Stripe price ID for the new period
	mapping, err := g.pricing.GetGatewayProductMapping(ctx, newPeriodID, GatewayID)
	if err != nil {
		return nil, fmt.Errorf("failed to get gateway product mapping: %w", err)
	}
	if mapping == nil || mapping.RemotePriceID == "" {
		return nil, fmt.Errorf("no Stripe price ID found for pricing period %d", newPeriodID)
	}

	// 4. Get the current subscription
	currentSub, err := g.billing.GetActiveSubscription(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get current subscription: %w", err)
	}
	if currentSub == nil {
		return nil, fmt.Errorf("no active subscription found")
	}
	if currentSub.GatewayType != GatewayID {
		return nil, fmt.Errorf("active subscription is not from Stripe")
	}
	if currentSub.SubscriptionID == "" {
		return nil, fmt.Errorf("subscription has no Stripe subscription ID")
	}

	// 5. Retrieve subscription from Stripe to get the subscription item ID
	stripeSub, err := g.stripeClient.V1Subscriptions().Retrieve(ctx, currentSub.SubscriptionID, &stripe.SubscriptionRetrieveParams{})
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve Stripe subscription: %w", err)
	}

	if stripeSub.Items == nil || len(stripeSub.Items.Data) == 0 {
		return nil, fmt.Errorf("subscription has no items")
	}
	subscriptionItemID := stripeSub.Items.Data[0].ID

	// 6. Update subscription with new price - Stripe handles proration automatically
	// The webhook (invoice.paid) will process the proration and issue credits/debits
	updateParams := &stripe.SubscriptionUpdateParams{
		Items: []*stripe.SubscriptionUpdateItemParams{
			{
				ID:    stripe.String(subscriptionItemID),
				Price: stripe.String(mapping.RemotePriceID),
			},
		},
		ProrationBehavior: stripe.String("create_prorations"),
	}

	_, err = g.stripeClient.V1Subscriptions().Update(ctx, currentSub.SubscriptionID, updateParams)
	if err != nil {
		g.logger.Error("failed to update subscription via Stripe API",
			zap.Uint("user_id", userID),
			zap.String("subscription_id", currentSub.SubscriptionID),
			zap.Uint("new_period_id", newPeriodID),
			zap.String("new_price_id", mapping.RemotePriceID),
			zap.Error(err))
		return nil, fmt.Errorf("failed to update subscription: %w", err)
	}

	g.logger.Info("Plan change initiated via Stripe API",
		zap.Uint("user_id", userID),
		zap.String("subscription_id", currentSub.SubscriptionID),
		zap.Uint("old_period_id", func() uint {
			if currentSub.PricingPlanPeriodID != nil {
				return *currentSub.PricingPlanPeriodID
			}
			return 0
		}()),
		zap.Uint("new_period_id", newPeriodID),
		zap.String("new_price_id", mapping.RemotePriceID))

	// 7. Return completion - actual credit/debit issuance happens via webhooks
	return &pluginCore.PlanChangeResult{
		Action:        pluginCore.PlanChangeActionComplete,
		EffectiveDate: new(time.Now()),
	}, nil
}

// GetSessionStatus retrieves the current status of a checkout session.
// Implements the SessionStatusProvider interface for embedded checkout return page verification.
func (g *StripeGateway) GetSessionStatus(ctx context.Context, sessionID string) (*pluginCore.SessionStatus, error) {
	ctx, span := core.TraceMethod(ctx, "StripeGateway.GetSessionStatus")
	defer span.End()

	session, err := g.stripeClient.V1CheckoutSessions().Retrieve(ctx, sessionID, &stripe.CheckoutSessionRetrieveParams{
		Expand: []*string{
			stripe.String("customer_details"),
			stripe.String("customer"),
		},
	})

	if err != nil {
		return nil, fmt.Errorf("failed to retrieve checkout session: %w", err)
	}

	status := &pluginCore.SessionStatus{
		SessionID: session.ID,
		Status:    string(session.Status),
	}

	// Extract customer email if available
	if session.CustomerDetails != nil && session.CustomerDetails.Email != "" {
		status.CustomerEmail = string(session.CustomerDetails.Email)
	} else if session.Customer != nil && session.Customer.Email != "" {
		status.CustomerEmail = string(session.Customer.Email)
	}

	// Extract user ID from ClientReferenceID for ownership verification
	if session.ClientReferenceID != "" {
		if userID, err := strconv.ParseUint(session.ClientReferenceID, 10, 64); err == nil {
			status.UserID = uint(userID)
		}
	}

	return status, nil
}
