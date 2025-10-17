package stripe

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/stripe/stripe-go/v83"
	"github.com/stripe/stripe-go/v83/webhook"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	quotaCore "go.lumeweb.com/portal-plugin-quota/core"
	"go.lumeweb.com/portal/core"
	"go.uber.org/zap"
)

const (
	GatewayID                    = "stripe"
	EventTypeSubscriptionCreated = "customer.subscription.created"
	EventTypeSubscriptionDeleted = "customer.subscription.deleted"
	EventTypeSubscriptionPaused  = "customer.subscription.paused"
	EventTypeSubscriptionResumed = "customer.subscription.resumed"
	EventTypeSubscriptionUpdated = "customer.subscription.updated"
	PlanIDMetadataKey            = "plan_id"
	UserIDMetadataKey            = "user_id"
)

type StripeGateway struct {
	logger         *core.Logger
	endpointSecret string
	quota          quotaCore.QuotaService
	users          core.UserService
}

func New(logger *core.Logger, endpointSecret string, quota quotaCore.QuotaService, users core.UserService) *StripeGateway {
	return &StripeGateway{
		logger:         logger,
		endpointSecret: endpointSecret,
		quota:          quota,
		users:          users,
	}
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

	if event.Request != nil {
		return event.Request.IdempotencyKey, nil
	}

	return event.ID, nil
}

func (g *StripeGateway) ExtractEventType(payload []byte) (string, error) {
	var event stripe.Event
	if err := json.Unmarshal(payload, &event); err != nil {
		return "", err
	}
	return string(event.Type), nil
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
	case EventTypeSubscriptionCreated, EventTypeSubscriptionResumed:
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
	subscription, err := g.validateSubscriptionEvent(event, true)
	if err != nil {
		return err
	}

	userID, err := parseUserID(subscription.Metadata)
	if err != nil {
		return err
	}

	if g.users == nil {
		return fmt.Errorf("user service not configured")
	}

	// Get user by ID
	exists, user, err := g.users.AccountExists(userID)
	if err != nil {
		return fmt.Errorf("failed to check user existence: %w", err)
	}
	if !exists {
		return fmt.Errorf("user with ID %d not found", userID)
	}

	price, planID, hasPlan, err := findFirstPlanPrice(subscription)
	if err != nil {
		return err
	}
	if price == nil {
		return fmt.Errorf("subscription missing item or price")
	}

	if !hasPlan {
		g.logger.Warn("subscription activated but price metadata missing plan_id",
			zap.Uint("user_id", userID),
			zap.String("price_id", price.ID),
			zap.String("subscription_id", subscription.ID),
			zap.String("event_id", event.ID))
		return nil
	}

	// Validate plan exists
	if g.quota == nil {
		return fmt.Errorf("quota service not configured")
	}
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

	g.logger.Debug("subscription activated - added quota plan",
		zap.Uint("user_id", userID),
		zap.String("price_id", price.ID),
		zap.String("subscription_id", subscription.ID),
		zap.Uint("plan_id", planID),
		zap.String("event_id", event.ID),
		zap.Uint("user_db_id", user.ID))

	return nil
}

func (g *StripeGateway) handleSubscriptionDeactivated(ctx context.Context, event stripe.Event) error {
	subscription, err := g.validateSubscriptionEvent(event, false)
	if err != nil {
		return err
	}

	userID, err := parseUserID(subscription.Metadata)
	if err != nil {
		return err
	}

	if g.users == nil {
		return fmt.Errorf("user service not configured")
	}

	// Get user by ID
	exists, user, err := g.users.AccountExists(userID)
	if err != nil {
		return fmt.Errorf("failed to check user existence: %w", err)
	}
	if !exists {
		return fmt.Errorf("user with ID %d not found", userID)
	}

	// Remove user from their current plan
	if g.quota == nil {
		return fmt.Errorf("quota service not configured")
	}
	if err := g.quota.RemoveUserFromPlan(user.ID); err != nil {
		return fmt.Errorf("failed to remove user from plan: %w", err)
	}

	g.logger.Debug("subscription deactivated - removed quota plan",
		zap.Uint("user_id", userID),
		zap.String("subscription_id", subscription.ID),
		zap.String("event_id", event.ID),
		zap.Uint("user_db_id", user.ID))

	return nil
}

func (g *StripeGateway) handleSubscriptionUpdated(ctx context.Context, event stripe.Event) error {
	subscription, err := g.validateSubscriptionEvent(event, true)
	if err != nil {
		return err
	}

	price, planID, hasPlan, err := findFirstPlanPrice(subscription)
	if err != nil {
		return err
	}

	if !hasPlan {
		g.logger.Warn("subscription updated but price metadata missing plan_id",
			zap.String("subscription_id", subscription.ID),
			zap.String("price_id", price.ID),
			zap.String("event_id", event.ID))
		return nil
	}

	userID, err := parseUserID(subscription.Metadata)
	if err != nil {
		return err
	}

	if g.users == nil {
		return fmt.Errorf("user service not configured")
	}

	// Get user by ID
	exists, user, err := g.users.AccountExists(userID)
	if err != nil {
		return fmt.Errorf("failed to check user existence: %w", err)
	}
	if !exists {
		return fmt.Errorf("user with ID %d not found", userID)
	}

	g.logger.Debug("updating user quota plan",
		zap.Uint("user_id", userID),
		zap.Uint("plan_id", planID),
		zap.Uint("user_db_id", user.ID),
		zap.String("subscription_id", subscription.ID),
		zap.String("price_id", price.ID),
		zap.String("event_id", event.ID),
		zap.Any("event_type", event.Type))

	// Validate plan exists
	if g.quota == nil {
		return fmt.Errorf("quota service not configured")
	}
	plan, err := g.quota.GetQuotaPlan(planID)
	if err != nil {
		return fmt.Errorf("plan with ID %d not found: %w", planID, err)
	}
	if plan == nil {
		return fmt.Errorf("plan with ID %d not found", planID)
	}

	// Assign user to new quota plan
	if err := g.quota.AssignUserToPlan(user.ID, planID); err != nil {
		return fmt.Errorf("failed to assign user to plan: %w", err)
	}

	return nil
}

func (g *StripeGateway) SetQuota(quota quotaCore.QuotaService) {
	g.quota = quota
}

// Helper function to parse user ID from metadata
func parseUserID(meta map[string]string) (uint, error) {
	userID := ""
	if meta != nil {
		userID = meta[UserIDMetadataKey]
	}
	if userID == "" {
		return 0, fmt.Errorf("subscription metadata missing user_id")
	}

	userIDUint, err := strconv.ParseUint(userID, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid user_id format: %w", err)
	}

	return uint(userIDUint), nil
}

// Helper function to extract plan ID from price metadata
func extractPlanID(price *stripe.Price) (uint, bool, error) {
	planID := ""
	if price.Metadata != nil {
		planID = price.Metadata[PlanIDMetadataKey]
	}

	if planID == "" {
		return 0, false, nil
	}

	planIDUint, err := strconv.ParseUint(planID, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("invalid plan_id format: %w", err)
	}

	return uint(planIDUint), true, nil
}

// Helper function to validate subscription event data
func (g *StripeGateway) validateSubscriptionEvent(event stripe.Event, requireItems bool) (*stripe.Subscription, error) {
	if event.Data == nil {
		return nil, fmt.Errorf("event data is nil")
	}

	if len(event.Data.Raw) == 0 {
		return nil, fmt.Errorf("event data raw payload is empty")
	}

	var subscription stripe.Subscription
	if err := json.Unmarshal(event.Data.Raw, &subscription); err != nil {
		return nil, err
	}

	// Check if there are any subscription items when required
	if requireItems && (subscription.Items == nil || len(subscription.Items.Data) == 0) {
		return nil, fmt.Errorf("subscription missing items")
	}

	return &subscription, nil
}

func findFirstPlanPrice(sub *stripe.Subscription) (*stripe.Price, uint, bool, error) {
	if sub.Items == nil || len(sub.Items.Data) == 0 {
		return nil, 0, false, nil
	}
	for _, it := range sub.Items.Data {
		if it == nil || it.Price == nil {
			continue
		}
		pid, ok, err := extractPlanID(it.Price)
		if err != nil {
			return nil, 0, false, err
		}
		if ok {
			return it.Price, pid, true, nil
		}
	}

	// Safe fallback: find first non-nil item with non-nil Price
	for _, it := range sub.Items.Data {
		if it != nil && it.Price != nil {
			return it.Price, 0, false, nil
		}
	}

	// If no valid item found, return nil values
	return nil, 0, false, nil
}

var _ pluginCore.PaymentGateway = (*StripeGateway)(nil)
