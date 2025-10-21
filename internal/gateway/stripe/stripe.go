package stripe

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/stripe/stripe-go/v83"
	"github.com/stripe/stripe-go/v83/webhook"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	quotaCore "go.lumeweb.com/portal-plugin-quota/core"
	"go.lumeweb.com/portal/core"
	"go.lumeweb.com/portal/db/models"
	"go.uber.org/zap"
)

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
	return r.client.V1Subscriptions().Retrieve(ctx, id, params)
}



// Client defines the interface for Stripe client operations
type Client interface {
	V1BillingPortalSessions() BillingPortalSessions
	V1Customers() Customers
	V1Subscriptions() Subscriptions
}

// BillingPortalSessions defines the interface for billing portal sessions operations
type BillingPortalSessions interface {
	Create(ctx context.Context, params *stripe.BillingPortalSessionCreateParams) (*stripe.BillingPortalSession, error)
}

// Customers defines the interface for customer operations
type Customers interface {
	Retrieve(ctx context.Context, id string, params *stripe.CustomerRetrieveParams) (*stripe.Customer, error)
	Update(ctx context.Context, id string, params *stripe.CustomerUpdateParams) (*stripe.Customer, error)
}

// Subscriptions defines the interface for subscription operations
type Subscriptions interface {
	Retrieve(ctx context.Context, id string, params *stripe.SubscriptionRetrieveParams) (*stripe.Subscription, error)
}

// client wraps the stripe.Client to implement Client
type client struct {
	client *stripe.Client
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
	logger         *core.Logger
	endpointSecret string
	secretKey      string
	stripeClient   Client
	quota          quotaCore.QuotaService
	users          core.UserService
	billing        pluginCore.BillingService
	subService     SubscriptionRetriever
	customerService CustomerRetriever
}

func New(logger *core.Logger, endpointSecret string, secretKey string, quota quotaCore.QuotaService, users core.UserService, billing pluginCore.BillingService) *StripeGateway {
	stripeClient := &client{client: stripe.NewClient(secretKey)}
	
	gateway := &StripeGateway{
		logger:         logger,
		endpointSecret: endpointSecret,
		secretKey:      secretKey,
		stripeClient:   stripeClient,
		quota:          quota,
		users:          users,
		billing:        billing,
	}

	gateway.subService = gateway.subscriptionRetriever()
	gateway.customerService = gateway.customerRetriever()

	return gateway
}

// customerRetriever returns a customer retriever instance
func (g *StripeGateway) customerRetriever() CustomerRetriever {
	return &customerRetriever{client: g.stripeClient}
}

// subscriptionRetriever returns a subscription retriever instance
func (g *StripeGateway) subscriptionRetriever() SubscriptionRetriever {
	return &subscriptionRetriever{client: g.stripeClient}
}

func (g *StripeGateway) ID() string {
	return GatewayID
}

func (g *StripeGateway) SignatureHeader() string {
	return "Stripe-Signature"
}

func (g *StripeGateway) ExtractEventID(payload []byte) (string, error) {
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

func (g *StripeGateway) ExtractEventType(payload []byte) (string, error) {
	var event stripe.Event
	if err := json.Unmarshal(payload, &event); err != nil {
		return "", err
	}
	return string(event.Type), nil
}

func (g *StripeGateway) GetCustomerPortalURL(ctx context.Context, userID uint, returnUrl string) (string, error) {
	// Get the subscriber for this user and gateway
	subscriber, err := g.billing.GetActiveSubscription(userID)
	if err != nil {
		return "", fmt.Errorf("failed to get active subscription: %w", err)
	}
	if subscriber == nil || subscriber.GatewayType != GatewayID {
		return "", fmt.Errorf("no active stripe subscription found for user %d", userID)
	}

	// Defensive check: ensure GatewayID is a valid Stripe customer ID
	if subscriber.GatewayID == "" {
		return "", fmt.Errorf("subscriber GatewayID is empty")
	}
	if !strings.HasPrefix(subscriber.GatewayID, CustomerIDPrefix) {
		return "", fmt.Errorf("invalid GatewayID: must be a Stripe customer ID starting with '%s'", CustomerIDPrefix)
	}

	// Create a billing portal session
	params := &stripe.BillingPortalSessionCreateParams{
		Customer:  stripe.String(subscriber.GatewayID),
		ReturnURL: stripe.String(returnUrl),
	}

	sess, err := g.stripeClient.V1BillingPortalSessions().Create(ctx, params)
	if err != nil {
		return "", fmt.Errorf("failed to create billing portal session: %w", err)
	}

	return sess.URL, nil
}

func (g *StripeGateway) ValidateWebhook(ctx context.Context, signature string, payload []byte) error {
	_, err := webhook.ConstructEvent(payload, signature, g.endpointSecret)
	return err
}

func (g *StripeGateway) HandleWebhook(ctx context.Context, payload []byte) error {
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
	return g.handleSubscriptionEvent(ctx, event, g.activateSubscription)
}

func (g *StripeGateway) handleSubscriptionDeactivated(ctx context.Context, event stripe.Event) error {
	return g.handleSubscriptionEvent(ctx, event, g.deactivateSubscription)
}

func (g *StripeGateway) handleSubscriptionUpdated(ctx context.Context, event stripe.Event) error {
	return g.handleSubscriptionEvent(ctx, event, g.handleSubscriptionUpdatedEvent)
}

func (g *StripeGateway) handleSubscriptionUpdatedEvent(ctx context.Context, userID uint, subscription *stripe.Subscription, event stripe.Event) error {
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
}

func (g *StripeGateway) SetQuota(quota quotaCore.QuotaService) {
	g.quota = quota
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
	return r.client.V1Customers().Retrieve(ctx, id, params)
}

// parseUserIDFromCustomerWithFallback attempts to parse user ID from customer metadata,
// and if that fails, fetches the customer from Stripe API and tries again.
func (g *StripeGateway) parseUserIDFromCustomerWithFallback(ctx context.Context, customerID string) (uint, error) {
	// Fetch customer directly from Stripe API
	customer, err := g.customerRetriever().Get(ctx, customerID, nil)
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


// extractUserIDFromSubscription extracts user ID from subscription customer metadata with fallback
func (g *StripeGateway) extractUserIDFromSubscription(ctx context.Context, subscription *stripe.Subscription) (uint, error) {
	userID, err := parseUserIDFromCustomer(subscription.Customer)
	if err != nil {
		// Try fallback if customer metadata is missing
		if subscription.Customer != nil && subscription.Customer.ID != "" {
			userID, err = g.parseUserIDFromCustomerWithFallback(ctx, subscription.Customer.ID)
			if err != nil {
				return 0, err
			}
		} else {
			return 0, err
		}
	}
	return userID, nil
}

// handleSubscriptionEvent is a generic function to handle subscription events
func (g *StripeGateway) handleSubscriptionEvent(ctx context.Context, event stripe.Event, handler func(ctx context.Context, userID uint, subscription *stripe.Subscription, event stripe.Event) error) error {
	subscription, err := g.getExpandedSubscriptionFromEvent(ctx, event)
	if err != nil {
		return err
	}

	userID, err := g.extractUserIDFromSubscription(ctx, subscription)
	if err != nil {
		return err
	}

	return handler(ctx, userID, subscription, event)
}

// getExpandedSubscription retrieves a subscription with expanded product data
func (g *StripeGateway) getExpandedSubscription(ctx context.Context, subscriptionID string) (*stripe.Subscription, error) {
	params := &stripe.SubscriptionRetrieveParams{}
	params.AddExpand("items.data.price.product")
	return g.subService.Get(ctx, subscriptionID, params)
}

// getExpandedSubscriptionFromEvent extracts the subscription ID from a Stripe event and retrieves the expanded subscription
func (g *StripeGateway) getExpandedSubscriptionFromEvent(ctx context.Context, event stripe.Event) (*stripe.Subscription, error) {
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
}

// activateSubscription is a common function to handle subscription activation
// for checkout.session.completed and customer.subscription.resumed events
func (g *StripeGateway) activateSubscription(ctx context.Context, userID uint, subscription *stripe.Subscription, event stripe.Event) error {
	// Validate services
	if err := g.validateServices(); err != nil {
		return err
	}

	// Get and validate user exists
	if _, err := g.getUser(userID); err != nil {
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
}

// activateSubscriptionWithPlanID handles subscription activation with a known plan ID
func (g *StripeGateway) activateSubscriptionWithPlanID(ctx context.Context, userID uint, subscription *stripe.Subscription, event stripe.Event, planID uint) error {
	// Validate services
	if err := g.validateServices(); err != nil {
		return err
	}

	// Get and validate user
	user, err := g.getUser(userID)
	if err != nil {
		return err
	}

	// Validate plan exists
	plan, err := g.quota.GetQuotaPlan(planID)
	if err != nil {
		return fmt.Errorf("plan with ID %d not found: %w", planID, err)
	}
	if plan == nil {
		return fmt.Errorf("plan with ID %d not found", planID)
	}

	// Assign user to quota plan
	if err := g.quota.AssignUserToPlan(user.ID, planID); err != nil {
		return fmt.Errorf("failed to assign user to plan: %w", err)
	}

	// Track subscriber in billing service
	if subscription.Customer == nil {
		return fmt.Errorf("subscription missing customer id")
	}

	if subscription.Customer.ID == "" {
		return fmt.Errorf("subscription missing customer id")
	}

	if err := g.trackSubscriber(user.ID, subscription.Customer.ID, true, &planID); err != nil {
		g.logger.Error("failed to track subscriber",
			zap.Error(err),
			zap.Uint("user_id", userID),
			zap.String("customer_id", subscription.Customer.ID))
	}

	// Update customer metadata with user ID
	g.updateCustomerMetadata(ctx, g.secretKey, subscription.Customer.ID, userID)

	g.logger.Debug("subscription activated - added quota plan",
		zap.Uint("user_id", userID),
		zap.String("subscription_id", subscription.ID),
		zap.Uint("plan_id", planID),
		zap.String("event_id", event.ID),
		zap.Uint("user_db_id", user.ID))

	return nil
}

// deactivateSubscription is a common function to handle subscription deactivation
// for customer.subscription.deleted and customer.subscription.paused events
func (g *StripeGateway) deactivateSubscription(ctx context.Context, userID uint, subscription *stripe.Subscription, event stripe.Event) error {
	// Validate services
	if err := g.validateServices(); err != nil {
		return err
	}

	// Get and validate user
	user, err := g.getUser(userID)
	if err != nil {
		return err
	}

	// Remove user from their current plan
	if err := g.quota.RemoveUserFromPlan(user.ID); err != nil {
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
	if err := g.trackSubscriber(user.ID, subscription.Customer.ID, false, nil); err != nil {
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
func (g *StripeGateway) getUser(userID uint) (*models.User, error) {
	exists, user, err := g.users.AccountExists(userID)
	if err != nil {
		return nil, fmt.Errorf("failed to check user existence: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("user with ID %d not found", userID)
	}
	return user, nil
}

// trackSubscriber handles subscriber tracking in the billing service
func (g *StripeGateway) trackSubscriber(userID uint, gatewayID string, isActive bool, planID *uint) error {
	if g.billing == nil {
		return nil // No billing service configured, nothing to track
	}

	if isActive {
		return g.billing.CreateOrUpdateSubscriber(userID, GatewayID, gatewayID, isActive, planID)
	} else {
		return g.billing.DeactivateSubscriber(userID, GatewayID)
	}
}

// updateCustomerMetadata updates the customer's metadata with the user ID
func (g *StripeGateway) updateCustomerMetadata(ctx context.Context, secretKey string, customerID string, userID uint) {
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

var _ pluginCore.PaymentGateway = (*StripeGateway)(nil)
