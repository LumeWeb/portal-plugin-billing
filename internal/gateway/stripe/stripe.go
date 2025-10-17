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
	var subscription stripe.Subscription
	if err := json.Unmarshal(event.Data.Raw, &subscription); err != nil {
		return err
	}

	// Check if there are any subscription items
	if subscription.Items == nil || len(subscription.Items.Data) == 0 {
		return fmt.Errorf("subscription missing items")
	}

	// Check if the first item's price is nil
	price := subscription.Items.Data[0].Price
	if price == nil {
		return fmt.Errorf("subscription missing item or price")
	}

	// Get user ID from subscription metadata
	userID := ""
	if subscription.Metadata != nil {
		userID = subscription.Metadata[UserIDMetadataKey]
	}
	if userID == "" {
		return fmt.Errorf("subscription metadata missing user_id")
	}

	// Convert userID to uint
	userIDUint, err := strconv.ParseUint(userID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid user_id format: %w", err)
	}

	// Get user by ID
	exists, user, err := g.users.AccountExists(uint(userIDUint))
	if err != nil {
		return fmt.Errorf("failed to check user existence: %w", err)
	}
	if !exists {
		return fmt.Errorf("user with ID %d not found", uint(userIDUint))
	}

	// Get plan ID from price metadata
	planID := ""
	if price.Metadata != nil {
		planID = price.Metadata[PlanIDMetadataKey]
	}
	if planID == "" {
		g.logger.Warn("subscription activated but price metadata missing plan_id",
			zap.String("user_id", userID),
			zap.String("price_id", price.ID),
			zap.String("subscription_id", subscription.ID),
			zap.String("event_id", event.ID))
		return nil
	}

	// Convert planID to uint
	planIDUint, err := strconv.ParseUint(planID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid plan_id format: %w", err)
	}

	// Validate plan exists
	if g.quota == nil {
		return fmt.Errorf("quota service not configured")
	}
	_, err = g.quota.GetQuotaPlan(uint(planIDUint))
	if err != nil {
		return fmt.Errorf("plan with ID %d not found: %w", uint(planIDUint), err)
	}

	// Assign user to quota plan
	if err := g.quota.AssignUserToPlan(user.ID, uint(planIDUint)); err != nil {
		return fmt.Errorf("failed to assign user to plan: %w", err)
	}

	g.logger.Debug("subscription activated - added quota plan",
		zap.String("user_id", userID),
		zap.String("price_id", price.ID),
		zap.String("subscription_id", subscription.ID),
		zap.String("plan_id", planID),
		zap.String("event_id", event.ID),
		zap.Uint("user_db_id", user.ID))

	return nil
}

func (g *StripeGateway) handleSubscriptionDeactivated(ctx context.Context, event stripe.Event) error {
	var subscription stripe.Subscription
	if err := json.Unmarshal(event.Data.Raw, &subscription); err != nil {
		return err
	}

	// Get user ID from subscription metadata
	userID := ""
	if subscription.Metadata != nil {
		userID = subscription.Metadata[UserIDMetadataKey]
	}
	if userID == "" {
		return fmt.Errorf("subscription metadata missing user_id")
	}

	// Convert userID to uint
	userIDUint, err := strconv.ParseUint(userID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid user_id format: %w", err)
	}

	// Get user by ID
	exists, user, err := g.users.AccountExists(uint(userIDUint))
	if err != nil {
		return fmt.Errorf("failed to check user existence: %w", err)
	}
	if !exists {
		return fmt.Errorf("user with ID %d not found", uint(userIDUint))
	}

	// Remove user from their current plan
	if g.quota == nil {
		return fmt.Errorf("quota service not configured")
	}
	if err := g.quota.RemoveUserFromPlan(user.ID); err != nil {
		return fmt.Errorf("failed to remove user from plan: %w", err)
	}

	g.logger.Debug("subscription deactivated - removed quota plan",
		zap.String("user_id", userID),
		zap.String("subscription_id", subscription.ID),
		zap.String("event_id", event.ID),
		zap.Uint("user_db_id", user.ID))

	return nil
}

func (g *StripeGateway) handleSubscriptionUpdated(ctx context.Context, event stripe.Event) error {
	var subscription stripe.Subscription
	if err := json.Unmarshal(event.Data.Raw, &subscription); err != nil {
		return err
	}

	// Check if there are any subscription items
	if subscription.Items == nil || len(subscription.Items.Data) == 0 {
		return fmt.Errorf("subscription missing items")
	}

	// Get the first item's price metadata
	price := subscription.Items.Data[0].Price
	if price == nil {
		return fmt.Errorf("subscription missing item or price")
	}

	// Get plan ID from price metadata
	planID := ""
	if price.Metadata != nil {
		planID = price.Metadata[PlanIDMetadataKey]
	}

	if planID == "" {
		g.logger.Warn("subscription updated but price metadata missing plan_id",
			zap.String("subscription_id", subscription.ID),
			zap.String("price_id", price.ID),
			zap.String("event_id", event.ID))
		return nil
	}

	// Get user ID from subscription metadata
	userID := ""
	if subscription.Metadata != nil {
		userID = subscription.Metadata[UserIDMetadataKey]
	}
	if userID == "" {
		return fmt.Errorf("subscription metadata missing user_id")
	}

	// Convert userID to uint
	userIDUint, err := strconv.ParseUint(userID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid user_id format: %w", err)
	}

	// Get user by ID
	exists, user, err := g.users.AccountExists(uint(userIDUint))
	if err != nil {
		return fmt.Errorf("failed to check user existence: %w", err)
	}
	if !exists {
		return fmt.Errorf("user with ID %d not found", uint(userIDUint))
	}

	g.logger.Debug("updating user quota plan",
		zap.String("user_id", userID),
		zap.String("plan_id", planID),
		zap.Uint("user_db_id", user.ID),
		zap.String("subscription_id", subscription.ID),
		zap.String("price_id", price.ID),
		zap.String("event_id", event.ID),
		zap.Any("event_type", event.Type))

	// Convert planID to uint
	planIDUint, err := strconv.ParseUint(planID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid plan_id format: %w", err)
	}

	// Validate plan exists
	if g.quota == nil {
		return fmt.Errorf("quota service not configured")
	}
	_, err = g.quota.GetQuotaPlan(uint(planIDUint))
	if err != nil {
		return fmt.Errorf("plan with ID %d not found: %w", uint(planIDUint), err)
	}

	// Assign user to new quota plan
	if err := g.quota.AssignUserToPlan(user.ID, uint(planIDUint)); err != nil {
		return fmt.Errorf("failed to assign user to plan: %w", err)
	}

	return nil
}

func (g *StripeGateway) SetQuota(quota quotaCore.QuotaService) {
	g.quota = quota
}

var _ pluginCore.PaymentGateway = (*StripeGateway)(nil)
