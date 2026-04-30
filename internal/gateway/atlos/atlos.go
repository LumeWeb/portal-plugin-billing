package atlos

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/shopspring/decimal"
	"go.lumeweb.com/atlos-sdk"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/internal/config"
	billingEvent "go.lumeweb.com/portal-plugin-billing/internal/event"
	billingModels "go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/portal-plugin-billing/internal/gateway"
	"go.lumeweb.com/portal-plugin-billing/pkg/subscription"
	quotaCore "go.lumeweb.com/portal-plugin-quota/core"
	core "go.lumeweb.com/portal/core"
	"go.lumeweb.com/portal/db/models"
	"go.uber.org/zap"
)

//go:embed templates/*.tpl
var templatesFS embed.FS

//go:embed assets/*.svg
var gatewayLogoFiles embed.FS

const (
	GatewayID             = "atlos"
	paymentButtonTemplate = "paymentButtonScript"

	OrderIDPrefixRegular  = "sub"
	OrderIDSuffixProrated = "prorated"
)

// Setup creates and configures an ATLOS gateway if merchant ID and API key are configured.
// Returns a log message (empty if not configured), the gateway instance (nil if not configured), and an error.
func Setup(opts pluginCore.GatewaySetupOptions, cfg config.AtlosConfig) (string, pluginCore.GatewayIdentity, error) {
	if cfg.MerchantID == "" {
		return "", nil, nil
	}

	if cfg.APIKey == "" {
		return "", nil, nil
	}

	gw := New(opts.Ctx, cfg, opts.HTTP, opts.Quota, opts.User, opts.BillingSvc, opts.PricingSvc, opts.CreditSvc)

	logMsg := fmt.Sprintf("ATLOS gateway registered successfully (merchant_id=%s)", cfg.MerchantID)
	if cfg.Endpoint != "" {
		logMsg = fmt.Sprintf("ATLOS gateway registered successfully (merchant_id=%s, endpoint=%s)", cfg.MerchantID, cfg.Endpoint)
	}
	return logMsg, gw, nil
}

// GenerateOrderID creates a standard order ID for ATLOS checkout.
// Format: sub-{userID}-{periodID} for regular subscriptions
// Example: sub-123-456
func GenerateOrderID(userID uint, periodID uint) string {
	return fmt.Sprintf("%s-%d-%d", OrderIDPrefixRegular, userID, periodID)
}

// GenerateProratedOrderID creates an order ID for prorated plan changes.
// Format: sub-{userID}-{periodID}-prorated
// Example: sub-123-456-prorated
func GenerateProratedOrderID(userID uint, periodID uint) string {
	return fmt.Sprintf("%s-%d-%d-%s", OrderIDPrefixRegular, userID, periodID, OrderIDSuffixProrated)
}

// ParseOrderID extracts userID and periodID from an order ID.
// Supports:
//   - New format: sub-{userID}-{periodID}
//   - Prorated format: sub-{userID}-{periodID}-prorated
//
// Returns true in isProrated if the order ID has the prorated suffix.
func ParseOrderID(orderID string) (uint, uint, bool, error) {
	parts := strings.Split(orderID, "-")
	if len(parts) < 3 {
		return 0, 0, false, fmt.Errorf("invalid order ID format: expected 'sub-{userID}-{periodID}', got: %s", orderID)
	}

	if parts[0] != OrderIDPrefixRegular {
		return 0, 0, false, fmt.Errorf("invalid order ID prefix: expected '%s', got: %s", OrderIDPrefixRegular, parts[0])
	}

	userID, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return 0, 0, false, fmt.Errorf("invalid user ID in order ID: %w", err)
	}

	periodID, err := strconv.ParseUint(parts[2], 10, 64)
	if err != nil {
		return 0, 0, false, fmt.Errorf("invalid period ID in order ID: %w", err)
	}

	isProrated := len(parts) == 4 && parts[3] == OrderIDSuffixProrated

	return uint(userID), uint(periodID), isProrated, nil
}

// atlosPaymentConfigData contains configuration for ATLOS payment widget templates
type atlosPaymentConfigData struct {
	ButtonID         string
	MerchantID       string
	OrderID          string
	Amount           float64
	Currency         string
	UserName         string
	UserEmail        string
	PostbackURL      string
	// Proration fields
	CreditAmount     float64 // For display in UI
	RecurringAmount  float64 // Full recurring amount for future billing
	RecurringUnit    string  // e.g., "month", "year"
	RecurringInterval int     // e.g., 1 for monthly, 1 for yearly
}

// PlanChangeCalculation contains the business logic results for a plan change
type PlanChangeCalculation struct {
	OldPeriod          *billingModels.PricingPlanPeriod
	NewPeriod          *billingModels.PricingPlanPeriod
	ProrationResult    subscription.ProrationResult
	NetAmountDue       decimal.Decimal
	ActionType         PlanChangeActionType
	CreditToIssue      decimal.Decimal
	EffectiveDate      time.Time
	CurrentSub         *billingModels.Subscriber
	NewPlan            *billingModels.PricingPlan
}

// PlanChangeActionType defines the type of action needed for a plan change
type PlanChangeActionType string

const (
	PlanChangeActionCheckoutRequired PlanChangeActionType = "checkout_required"
	PlanChangeActionCreditOnly       PlanChangeActionType = "credit_only"
	PlanChangeActionZeroAmount       PlanChangeActionType = "zero_amount"
)

// Compile-time interface checks for AtlosGateway.
// AtlosGateway supports cancellation and plan changes but NOT pause/resume.
var (
	_ pluginCore.CancellationExecutor = (*AtlosGateway)(nil)
	_ pluginCore.PlanChangeExecutor   = (*AtlosGateway)(nil)
)

// AtlosGateway implements the PaymentGateway interface for ATLOS payment widget
type AtlosGateway struct {
	coreCtx core.Context
	config  config.AtlosConfig
	http    core.HTTPService
	quota   quotaCore.QuotaService
	users   core.UserService
	billing pluginCore.BillingService
	pricing pluginCore.PricingService
	credit  pluginCore.CreditService
}

// New creates a new AtlosGateway instance
func New(
	coreCtx core.Context,
	cfg config.AtlosConfig,
	http core.HTTPService,
	quota quotaCore.QuotaService,
	users core.UserService,
	billing pluginCore.BillingService,
	pricing pluginCore.PricingService,
	credit pluginCore.CreditService,
) *AtlosGateway {
	return &AtlosGateway{
		coreCtx: coreCtx,
		config:  cfg,
		http:    http,
		quota:   quota,
		users:   users,
		billing: billing,
		pricing: pricing,
		credit:  credit,
	}
}

// SetQuota sets the quota service
func (g *AtlosGateway) SetQuota(quota quotaCore.QuotaService) {
	g.quota = quota
}

func (g *AtlosGateway) newAtlosClient() (*atlos.Client, error) {
	opts := make([]atlos.ClientOption, 0, 1)
	if g.config.Endpoint != "" {
		opts = append(opts, atlos.WithEndpoint(g.config.Endpoint))
	}
	return atlos.NewClient(g.config.APIKey, opts...)
}

// cancelSubscription cancels a subscription in ATLOS
// Provides centralized error handling for ATLOS cancellation operations
func (g *AtlosGateway) cancelSubscription(ctx context.Context, subscriptionID string, operation string) error {
	client, err := g.newAtlosClient()
	if err != nil {
		g.coreCtx.Logger().Warn("Failed to create ATLOS client for cancellation",
			zap.Error(err),
			zap.String("subscription_id", subscriptionID),
			zap.String("operation", operation))
		return fmt.Errorf("failed to create ATLOS client: %w", err)
	}
	if err := client.Cancel(ctx, atlos.CancelPostRequest{
		SubscriptionId: &subscriptionID,
	}); err != nil {
		g.coreCtx.Logger().Error("Failed to cancel subscription in ATLOS",
			zap.Error(err),
			zap.String("subscription_id", subscriptionID),
			zap.String("operation", operation))
		return fmt.Errorf("failed to cancel subscription in ATLOS: %w", err)
	}
	return nil
}

// ID returns the gateway identifier
func (g *AtlosGateway) ID(ctx context.Context) string {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.ID")
	defer span.End()

	return GatewayID
}

// SignatureHeader returns the signature header name for webhook verification
func (g *AtlosGateway) SignatureHeader(ctx context.Context) string {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.SignatureHeader")
	defer span.End()

	return atlos.SignatureHeader
}

// ExtractEventID extracts the event ID from a webhook payload
func (g *AtlosGateway) ExtractEventID(ctx context.Context, payload []byte) (string, error) {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.ExtractEventID")
	defer span.End()

	var notification atlos.PostbackNotification
	if err := json.Unmarshal(payload, &notification); err != nil {
		return "", fmt.Errorf("failed to parse postback notification: %w", err)
	}

	if notification.TransactionId == "" {
		return "", fmt.Errorf("empty transaction ID in postback notification")
	}

	return notification.TransactionId, nil
}

// ExtractEventType extracts the event type from a webhook payload
func (g *AtlosGateway) ExtractEventType(ctx context.Context, payload []byte) (string, error) {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.ExtractEventType")
	defer span.End()

	var notification atlos.PostbackNotification
	if err := json.Unmarshal(payload, &notification); err != nil {
		return "", fmt.Errorf("failed to parse postback notification: %w", err)
	}

	// ATLOS postback occurs only on successful payments (Status == 100)
	// The SDK's Validate() method enforces this, so we always return a payment confirmed event
	return "payment.confirmed", nil
}

// GetCustomerPortalURL returns the customer portal URL
func (g *AtlosGateway) GetCustomerPortalURL(ctx context.Context, userID uint, returnUrl string) (string, error) {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.GetCustomerPortalURL")
	defer span.End()

	// ATLOS uses widget-based checkout, customer portal may not be applicable
	return "", fmt.Errorf("customer portal not supported by ATLOS widget")
}

// CreateOrUpdateSubscriber creates or updates a subscriber record
func (g *AtlosGateway) CreateOrUpdateSubscriber(ctx context.Context, userID uint, externalID string, subscriptionID string, isActive bool, pricingPlanPeriodID *uint) error {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.CreateOrUpdateSubscriber")
	defer span.End()

	// Delegate to billing service
	return g.billing.CreateOrUpdateSubscriber(ctx, userID, g.ID(ctx), externalID, subscriptionID, isActive, pricingPlanPeriodID)
}

// DeactivateSubscriber deactivates a subscriber
func (g *AtlosGateway) DeactivateSubscriber(ctx context.Context, userID uint, gatewayType string) error {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.DeactivateSubscriber")
	defer span.End()

	// Delegate to billing service
	return g.billing.DeactivateSubscriber(ctx, userID, g.ID(ctx))
}

// ExecuteCancel schedules a subscription cancellation at the end of the billing period.
// This implements the SubscriptionExecutor interface for API-based cancellation.
// For ATLOS:
//   - If immediate=true: cancels immediately, issues proration credit, deactivates subscriber
//   - If immediate=false: schedules cancellation at the end of the current billing period
// The reconciliation cron job will process scheduled cancellations when WillCancelAt is reached.
// Returns a CancellationResult indicating the cancellation status and whether it can be aborted.
func (g *AtlosGateway) ExecuteCancel(ctx context.Context, userID uint, immediate bool) (*pluginCore.CancellationResult, error) {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.ExecuteCancel")
	defer span.End()

	// Get active subscriber to retrieve the subscription ID
	subscriber, err := g.billing.GetActiveSubscription(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get active subscription: %w", err)
	}
	if subscriber == nil || subscriber.GatewayType != GatewayID {
		return nil, fmt.Errorf("no active Atlas subscription found for user %d", userID)
	}

	// Get the pricing period
	if subscriber.PricingPlanPeriodID == nil {
		return nil, fmt.Errorf("subscriber pricing plan period ID is nil")
	}

	period, err := g.pricing.GetPricingPlanPeriod(ctx, *subscriber.PricingPlanPeriodID)
	if err != nil {
		return nil, fmt.Errorf("failed to get pricing plan period: %w", err)
	}
	if period == nil {
		return nil, fmt.Errorf("pricing plan period not found")
	}

	// Handle edge case where billing period dates might be nil
	if subscriber.BillingPeriodStart == nil || subscriber.BillingPeriodEnd == nil {
		return nil, fmt.Errorf("subscriber billing period dates are nil")
	}

	if immediate {
		return g.executeImmediateCancel(ctx, userID, subscriber, period)
	} else {
		return g.executeScheduledCancel(ctx, userID, subscriber, period)
	}
}

// executeImmediateCancel cancels a subscription immediately.
// Issues proration credit for unused time and deactivates the subscriber.
func (g *AtlosGateway) executeImmediateCancel(ctx context.Context, userID uint, subscriber *billingModels.Subscriber, period *billingModels.PricingPlanPeriod) (*pluginCore.CancellationResult, error) {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.executeImmediateCancel")
	defer span.End()

	if err := g.cancelSubscription(ctx, subscriber.SubscriptionID, "ExecuteCancel-Immediate"); err != nil {
		return nil, err
	}

	// Calculate and issue proration credit for unused time in the billing period
	now := time.Now().UTC()
	oldPrice := subscription.Price{
		Amount:  decimal.NewFromFloat(period.PriceUSD),
		Cadence: subscription.Cadence(period.Cadence),
	}

	cycle := subscription.BillingCycle{
		StartAt: *subscriber.BillingPeriodStart,
		EndAt:   *subscriber.BillingPeriodEnd,
		Cadence: subscription.Cadence(period.Cadence),
	}

	// Clamp proration time to billing cycle boundaries
	if now.After(cycle.EndAt) {
		now = cycle.EndAt
	}
	if now.Before(cycle.StartAt) {
		now = cycle.StartAt
	}

	proratedValue := subscription.UnusedPeriodValue(oldPrice, cycle, now)

	if g.credit != nil && proratedValue.GreaterThan(decimal.Zero) {
		err := g.credit.IssueCreditWithIdempotency(
			ctx,
			uint64(userID),
			pluginCore.TransactionTypeRefund,
			proratedValue,
			pluginCore.ReferenceTypeAtlosPayment,
			fmt.Sprintf("immediate-cancel-%s", subscriber.SubscriptionID),
			"Proration credit for unused subscription period on immediate cancellation",
			0,
		)
		if err != nil {
			g.coreCtx.Logger().Error("failed to issue proration credit for immediate cancellation",
				zap.Error(err),
				zap.Uint("user_id", userID),
				zap.String("subscription_id", subscriber.SubscriptionID))
			// Continue with deactivation even if credit issuance fails
		}

		g.coreCtx.Logger().Info("Proration credit issued for immediate cancellation",
			zap.Uint("user_id", userID),
			zap.String("prorated_amount", proratedValue.String()),
			zap.String("subscription_id", subscriber.SubscriptionID))
	}

	// Deactivate subscriber immediately
	if err := g.billing.DeactivateSubscriber(ctx, userID, GatewayID); err != nil {
		return nil, fmt.Errorf("failed to deactivate subscriber: %w", err)
	}

	// Fire subscription cancelled event
	planID := uint(0)
	if subscriber.PricingPlanPeriodID != nil {
		if p, err := g.pricing.GetPricingPlanPeriod(ctx, *subscriber.PricingPlanPeriodID); err == nil && p != nil {
			planID = p.PricingPlanID
		}
	}

	evt := billingEvent.NewSubscriptionCancelledEvent(
		ctx,
		userID,
		subscriber.SubscriptionID,
		GatewayID,
		planID,
	)
	core.Fire(g.coreCtx, billingEvent.EVENT_SUBSCRIPTION_CANCELLED, evt)

	g.coreCtx.Logger().Info("Subscription cancelled immediately",
		zap.Uint("user_id", userID),
		zap.String("subscription_id", subscriber.SubscriptionID),
		zap.Time("effective_at", now))

	effectiveAt := now
	return &pluginCore.CancellationResult{
		Status:      pluginCore.CancellationStatusImmediate,
		EffectiveAt: &effectiveAt,
		CanAbort:    false, // Immediate cancellation cannot be aborted
	}, nil
}

// executeScheduledCancel schedules a subscription cancellation at the end of the billing period.
// The reconciliation cron job will process the cancellation when WillCancelAt is reached.
func (g *AtlosGateway) executeScheduledCancel(ctx context.Context, userID uint, subscriber *billingModels.Subscriber, period *billingModels.PricingPlanPeriod) (*pluginCore.CancellationResult, error) {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.executeScheduledCancel")
	defer span.End()

	// Cancel in ATLOS (but keep local subscriber active until reconciliation)
	if err := g.cancelSubscription(ctx, subscriber.SubscriptionID, "ExecuteCancel-Scheduled"); err != nil {
		return nil, err
	}

	// Schedule cancellation at the end of the billing period
	cancelAt := *subscriber.BillingPeriodEnd

	g.coreCtx.Logger().Info("Scheduling subscription cancellation at end of billing period",
		zap.Uint("user_id", userID),
		zap.String("subscription_id", subscriber.SubscriptionID),
		zap.Time("cancel_at", cancelAt),
		zap.Time("billing_period_end", *subscriber.BillingPeriodEnd))

	// Update subscriber with scheduled cancellation date
	// We keep the subscriber active until the reconciliation job processes it
	if err := g.billing.CreateOrUpdateSubscriber(
		ctx,
		userID,
		GatewayID,
		subscriber.ExternalID,
		subscriber.SubscriptionID,
		true, // Keep active
		&period.ID,
		pluginCore.WithWillCancelAt(&cancelAt),
		pluginCore.WithBillingPeriodStart(subscriber.BillingPeriodStart),
		pluginCore.WithBillingPeriodEnd(subscriber.BillingPeriodEnd),
	); err != nil {
		return nil, fmt.Errorf("failed to schedule cancellation: %w", err)
	}

	return &pluginCore.CancellationResult{
		Status:      pluginCore.CancellationStatusScheduled,
		EffectiveAt: &cancelAt,
		CanAbort:    true,
	}, nil
}

// ReconcileCancellation handles pending subscription cancellations that were scheduled
// for a future date. This is called by the cancellation reconciliation cron job when
// WillCancelAt has been reached. For ATLOS, we need to:
// 1. Verify the subscriber has a WillCancelAt date that has passed
// 2. Calculate and issue any proration credit for the partial billing period
// 3. Cancel the subscription in ATLOS (fire & forget)
// 4. Deactivate the subscriber locally and mark as cancelled
func (g *AtlosGateway) ReconcileCancellation(ctx context.Context, userID uint) error {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.ReconcileCancellation")
	defer span.End()

	// Get active subscriber
	subscriber, err := g.billing.GetActiveSubscription(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to get active subscription: %w", err)
	}
	if subscriber == nil || subscriber.GatewayType != GatewayID {
		return fmt.Errorf("no active ATLOS subscription found for user %d", userID)
	}

	if subscriber.WillCancelAt == nil {
		return fmt.Errorf("subscriber does not have a scheduled cancellation date")
	}

	now := time.Now().UTC()
	if subscriber.WillCancelAt.After(now) {
		g.coreCtx.Logger().Debug("Scheduled cancellation date is in the future, skipping",
			zap.Uint("user_id", userID),
			zap.Time("will_cancel_at", *subscriber.WillCancelAt),
			zap.Time("now", now))
		return nil
	}

	g.coreCtx.Logger().Info("Reconciling scheduled cancellation",
		zap.Uint("user_id", userID),
		zap.String("subscription_id", subscriber.SubscriptionID),
		zap.Time("will_cancel_at", *subscriber.WillCancelAt))

	// Get the pricing period
	if subscriber.PricingPlanPeriodID == nil {
		return fmt.Errorf("subscriber pricing plan period ID is nil")
	}

	period, err := g.pricing.GetPricingPlanPeriod(ctx, *subscriber.PricingPlanPeriodID)
	if err != nil {
		return fmt.Errorf("failed to get pricing plan period: %w", err)
	}
	if period == nil {
		return fmt.Errorf("pricing plan period not found")
	}

	// Calculate proration credit for the billing period from WillCancelAt to BillingPeriodEnd
	oldPrice := subscription.Price{
		Amount:  decimal.NewFromFloat(period.PriceUSD),
		Cadence: subscription.Cadence(period.Cadence),
	}

	if subscriber.BillingPeriodStart == nil || subscriber.BillingPeriodEnd == nil {
		return fmt.Errorf("subscriber billing period dates are nil")
	}

	cycle := subscription.BillingCycle{
		StartAt: *subscriber.WillCancelAt,
		EndAt:   *subscriber.BillingPeriodEnd,
		Cadence: subscription.Cadence(period.Cadence),
	}

	// Clamp proration time to cycle boundaries
	if now.After(cycle.EndAt) {
		now = cycle.EndAt
	}
	if now.Before(cycle.StartAt) {
		now = cycle.StartAt
	}

	proratedValue := subscription.UnusedPeriodValue(oldPrice, cycle, now)

	if g.credit != nil && proratedValue.GreaterThan(decimal.Zero) {
		err := g.credit.IssueCreditWithIdempotency(
			ctx,
			uint64(userID),
			pluginCore.TransactionTypeRefund,
			proratedValue,
			pluginCore.ReferenceTypeAtlosPayment,
			fmt.Sprintf("cancel-reconcile-%d", subscriber.ID),
			"Proration credit for unused subscription period after cancellation",
			0,
		)
		if err != nil {
			return fmt.Errorf("failed to issue proration credit: %w", err)
		}

		g.coreCtx.Logger().Info("Proration credit issued for scheduled cancellation",
			zap.Uint("user_id", userID),
			zap.String("prorated_amount", proratedValue.String()),
			zap.String("subscription_id", subscriber.SubscriptionID))
	}

	// Cancel in ATLOS
	if err := g.cancelSubscription(ctx, subscriber.SubscriptionID, "ReconcileCancellation"); err != nil {
		return err
	}

	// Deactivate subscriber and mark as cancelled
	if err := g.billing.DeactivateSubscriber(ctx, userID, GatewayID); err != nil {
		return fmt.Errorf("failed to deactivate subscriber: %w", err)
	}

	// Fire subscription cancelled event
	planID := uint(0)
	if subscriber.PricingPlanPeriodID != nil {
		period, err := g.pricing.GetPricingPlanPeriod(ctx, *subscriber.PricingPlanPeriodID)
		if err == nil && period != nil {
			planID = period.PricingPlanID
		}
	}

	evt := billingEvent.NewSubscriptionCancelledEvent(
		ctx,
		userID,
		subscriber.SubscriptionID,
		GatewayID,
		planID,
	)
	core.Fire(g.coreCtx, billingEvent.EVENT_SUBSCRIPTION_CANCELLED, evt)

	g.coreCtx.Logger().Info("Reconciled scheduled cancellation successfully",
		zap.Uint("user_id", userID),
		zap.String("subscription_id", subscriber.SubscriptionID))

	return nil
}

// AbortCancellation cancels a scheduled subscription cancellation, restoring
// the subscription to active status. This implements the SubscriptionExecutor interface.
// Returns an error if no scheduled cancellation exists or if the gateway doesn't support abort.
func (g *AtlosGateway) AbortCancellation(ctx context.Context, userID uint) error {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.AbortCancellation")
	defer span.End()

	// Get active subscriber
	subscriber, err := g.billing.GetActiveSubscription(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to get active subscription: %w", err)
	}
	if subscriber == nil || subscriber.GatewayType != GatewayID {
		return fmt.Errorf("no active ATLOS subscription found for user %d", userID)
	}

	// Verify scheduled cancellation exists
	if subscriber.WillCancelAt == nil {
		return fmt.Errorf("no scheduled cancellation found for user %d", userID)
	}

	g.coreCtx.Logger().Info("Aborting scheduled cancellation",
		zap.Uint("user_id", userID),
		zap.String("subscription_id", subscriber.SubscriptionID),
		zap.Time("was_scheduled_for", *subscriber.WillCancelAt))

	// Clear WillCancelAt to abort the scheduled cancellation
	// The subscription remains active
	if err := g.billing.CreateOrUpdateSubscriber(
		ctx,
		userID,
		GatewayID,
		subscriber.ExternalID,
		subscriber.SubscriptionID,
		true, // Keep active
		subscriber.PricingPlanPeriodID,
		pluginCore.WithBillingPeriodStart(subscriber.BillingPeriodStart),
		pluginCore.WithBillingPeriodEnd(subscriber.BillingPeriodEnd),
		pluginCore.WithClearWillCancelAt(),
	); err != nil {
		return fmt.Errorf("failed to abort scheduled cancellation: %w", err)
	}

	g.coreCtx.Logger().Info("Successfully aborted scheduled cancellation",
		zap.Uint("user_id", userID),
		zap.String("subscription_id", subscriber.SubscriptionID))

	return nil
}

// ExecutePlanChange executes a plan change operation for ATLOS subscriptions.
//
// This implementation uses the new prorated plan change flow:
// 1. Validates the new pricing plan and period
// 2. Gets the current subscription and its period
// 3. Calculates proration between old and new plans
// 4. Determines the appropriate action based on net amount due
//    - CreditOnly: User has net credit, skip checkout, issue credit directly
//    - ZeroAmount: Exact proration match, skip checkout, activate immediately
//    - CheckoutRequired: User owes money, show prorated checkout
// 5. Executes the determined action
//
// The customer pays the prorated difference in a single transaction for better UX.
// The checkout UI shows the prorated price to pay with a credit breakdown.
func (g *AtlosGateway) ExecutePlanChange(
	ctx context.Context,
	userID uint,
	newPeriodID uint,
) (*pluginCore.PlanChangeResult, error) {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.ExecutePlanChange")
	defer span.End()

	// 1. Calculate proration and determine action type (separated business logic)
	calc, err := g.calculatePlanChangeProration(ctx, userID, newPeriodID)
	if err != nil {
		return nil, err
	}

	// 2. Execute based on action type
	switch calc.ActionType {
	case PlanChangeActionCreditOnly:
		return g.handleCreditOnlyPlanChange(ctx, calc)

	case PlanChangeActionZeroAmount:
		return g.handleZeroAmountPlanChange(ctx, calc)

	case PlanChangeActionCheckoutRequired:
		// Generate prorated checkout UI (separated UI logic)
		checkoutUI, err := g.generateProratedCheckoutUI(ctx, calc)
		if err != nil {
			return nil, fmt.Errorf("failed to generate prorated checkout UI: %w", err)
		}

		// Cancel old subscription (will be replaced)
		if err := g.cancelSubscription(ctx, calc.CurrentSub.SubscriptionID, "ExecutePlanChange-CheckoutRequired"); err != nil {
			return nil, err
		}

		// Deactivate old subscriber locally
		if err := g.billing.DeactivateSubscriber(ctx, userID, GatewayID); err != nil {
			return nil, fmt.Errorf("failed to deactivate old subscriber: %w", err)
		}

		g.coreCtx.Logger().Debug("Old subscription deactivated for prorated plan change",
			zap.Uint("user_id", userID),
			zap.String("subscription_id", calc.CurrentSub.SubscriptionID),
			zap.String("net_amount_due", calc.NetAmountDue.String()))

		return &pluginCore.PlanChangeResult{
			Action:        pluginCore.PlanChangeActionCheckoutRequired,
			CheckoutLink:  checkoutUI.SessionID,
			CreditApplied: calc.ProrationResult.UnusedCredit,
			ChargeDue:     calc.NetAmountDue,
			EffectiveDate: &calc.EffectiveDate,
		}, nil

	default:
		return nil, fmt.Errorf("unknown plan change action type: %s", calc.ActionType)
	}
}

// calculatePlanChangeProration calculates the proration for a plan change.
// This is a pure business logic function that can be tested independently.
func (g *AtlosGateway) calculatePlanChangeProration(
	ctx context.Context,
	userID uint,
	newPeriodID uint,
) (*PlanChangeCalculation, error) {
	// 1. Validate and get new pricing period
	newPeriod, err := g.pricing.GetPricingPlanPeriod(ctx, newPeriodID)
	if err != nil {
		return nil, fmt.Errorf("failed to get new pricing plan period: %w", err)
	}
	if newPeriod == nil {
		return nil, fmt.Errorf("new pricing plan period not found")
	}

	// Get the pricing plan to validate it's active
	plan, err := g.pricing.GetPricingPlan(ctx, newPeriod.PricingPlanID)
	if err != nil {
		return nil, fmt.Errorf("failed to get pricing plan: %w", err)
	}
	if plan == nil || !plan.IsActive {
		return nil, fmt.Errorf("new plan is not active")
	}

	// 2. Get current subscriber
	currentSub, err := g.billing.GetActiveSubscription(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get current subscription: %w", err)
	}
	if currentSub == nil {
		return nil, fmt.Errorf("no active subscription found")
	}
	if currentSub.GatewayType != GatewayID {
		return nil, fmt.Errorf("active subscription is not from ATLOS")
	}
	if currentSub.PricingPlanPeriodID == nil {
		return nil, fmt.Errorf("current subscription has no pricing period")
	}

	// 3. Get old pricing period
	oldPeriod, err := g.pricing.GetPricingPlanPeriod(ctx, *currentSub.PricingPlanPeriodID)
	if err != nil {
		return nil, fmt.Errorf("failed to get old pricing plan period: %w", err)
	}
	if oldPeriod == nil {
		return nil, fmt.Errorf("old pricing plan period not found")
	}

	// 4. Calculate proration between old and new plans
	oldPrice := subscription.Price{
		Amount:  decimal.NewFromFloat(oldPeriod.PriceUSD),
		Cadence: subscription.Cadence(oldPeriod.Cadence),
	}
	newPrice := subscription.Price{
		Amount:  decimal.NewFromFloat(newPeriod.PriceUSD),
		Cadence: subscription.Cadence(newPeriod.Cadence),
	}

	// Handle edge case where billing period dates might be nil
	if currentSub.BillingPeriodStart == nil || currentSub.BillingPeriodEnd == nil {
		return nil, fmt.Errorf("subscriber billing period dates are nil")
	}

	oldCycle := subscription.BillingCycle{
		StartAt: *currentSub.BillingPeriodStart,
		EndAt:   *currentSub.BillingPeriodEnd,
		Cadence: subscription.Cadence(oldPeriod.Cadence),
	}

	// Clamp proration timestamp to be within the billing cycle.
	// This handles edge cases where MySQL TIMESTAMP precision loss (nanosecond→microsecond
	// truncation) or timezone conversion (loc=Local in DSN) can cause time.Now().UTC()
	// to appear after BillingPeriodEnd despite the cycle being freshly created.
	prorationTime := time.Now().UTC()
	if prorationTime.After(oldCycle.EndAt) {
		g.coreCtx.Logger().Warn("proration time exceeds billing cycle end, clamping",
			zap.Uint("user_id", userID),
			zap.Time("proration_time", prorationTime),
			zap.Time("cycle_end", oldCycle.EndAt),
			zap.Duration("excess", prorationTime.Sub(oldCycle.EndAt)),
		)
		prorationTime = oldCycle.EndAt
	}
	if prorationTime.Before(oldCycle.StartAt) {
		g.coreCtx.Logger().Warn("proration time precedes billing cycle start, clamping",
			zap.Uint("user_id", userID),
			zap.Time("proration_time", prorationTime),
			zap.Time("cycle_start", oldCycle.StartAt),
			zap.Duration("deficit", oldCycle.StartAt.Sub(prorationTime)),
		)
		prorationTime = oldCycle.StartAt
	}

	prorationResult, err := subscription.ProratedChange(
		oldPrice, newPrice, oldCycle,
		prorationTime,
		subscription.ProrationBehaviorCreateProrations,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate proration: %w", err)
	}

	// 5. Calculate net amount due using subscription package function
	netAmountDue := subscription.NetResult(prorationResult)

	// 6. Determine action type based on net amount
	var actionType PlanChangeActionType
	var creditToIssue decimal.Decimal

	if netAmountDue.LessThan(decimal.Zero) {
		// User has net credit
		actionType = PlanChangeActionCreditOnly
		creditToIssue = netAmountDue.Abs()
	} else if netAmountDue.IsZero() {
		// Exact proration match
		actionType = PlanChangeActionZeroAmount
	} else {
		// User needs to pay
		actionType = PlanChangeActionCheckoutRequired
	}

	g.coreCtx.Logger().Debug("Plan change proration calculated",
		zap.Uint("user_id", userID),
		zap.Uint("old_plan_id", oldPeriod.PricingPlanID),
		zap.Uint("old_period_id", *currentSub.PricingPlanPeriodID),
		zap.Uint("new_period_id", newPeriodID),
		zap.String("credit_due", prorationResult.UnusedCredit.String()),
		zap.String("new_charge", prorationResult.NewCharge.String()),
		zap.String("net_amount_due", netAmountDue.String()),
		zap.String("action_type", string(actionType)),
	)

	return &PlanChangeCalculation{
		OldPeriod:       oldPeriod,
		NewPeriod:       newPeriod,
		ProrationResult: prorationResult,
		NetAmountDue:    netAmountDue,
		ActionType:      actionType,
		CreditToIssue:   creditToIssue,
		EffectiveDate:   prorationResult.EffectiveDate,
		CurrentSub:      currentSub,
		NewPlan:         plan,
	}, nil
}

// handleCreditOnlyPlanChange handles plan changes where user has net credit.
// This method skips the checkout flow and issues credit directly.
func (g *AtlosGateway) handleCreditOnlyPlanChange(
	ctx context.Context,
	calc *PlanChangeCalculation,
) (*pluginCore.PlanChangeResult, error) {
	userID := calc.CurrentSub.UserID

	// 1. Issue net credit to ledger
	if calc.CreditToIssue.GreaterThan(decimal.Zero) {
		err := g.credit.IssueCreditWithIdempotency(
			ctx,
			uint64(userID),
			pluginCore.TransactionTypeRefund,
			calc.CreditToIssue,
			pluginCore.ReferenceTypeAtlosPayment,
			fmt.Sprintf("plan-change-net-credit-%d", calc.CurrentSub.ID),
			"Net credit balance from plan change",
			0,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to issue net credit: %w", err)
		}
		g.coreCtx.Logger().Debug("Net credit issued for plan change",
			zap.Uint("user_id", userID),
			zap.String("credit_amount", calc.CreditToIssue.String()))
	}

	// 2. Cancel old subscription
	if err := g.cancelSubscription(ctx, calc.CurrentSub.SubscriptionID, "handleCreditOnlyPlanChange"); err != nil {
		return nil, err
	}

	// 3. Deactivate old subscriber
	if err := g.billing.DeactivateSubscriber(ctx, userID, GatewayID); err != nil {
		return nil, fmt.Errorf("failed to deactivate old subscriber: %w", err)
	}

	// 4. Calculate first billing cycle for new plan
	firstCycle := subscription.CalculateFirstCycle(
		time.Now().UTC(),
		subscription.Cadence(calc.NewPeriod.Cadence),
	)

	// 5. Activate new subscription (without immediate payment)
	if err := g.billing.CreateOrUpdateSubscriber(
		ctx,
		userID,
		GatewayID,
		"", // Empty external ID - will be set on first payment
		"", // Empty subscription ID - will be set on first payment
		true,
		&calc.NewPeriod.ID,
		pluginCore.WithBillingPeriodStart(&firstCycle.StartAt),
		pluginCore.WithBillingPeriodEnd(&firstCycle.EndAt),
	); err != nil {
		return nil, fmt.Errorf("failed to activate new subscription: %w", err)
	}

	g.coreCtx.Logger().Debug("Credit-only plan change completed",
		zap.Uint("user_id", userID),
		zap.String("credit_issued", calc.CreditToIssue.String()),
		zap.Uint("new_period_id", calc.NewPeriod.ID))

	// Fire plan change credit only event
	oldPlanID := calc.OldPeriod.PricingPlanID
	oldPeriodID := calc.OldPeriod.ID
	billingCycleEnd := time.Time{}
	if calc.CurrentSub.BillingPeriodEnd != nil {
		billingCycleEnd = *calc.CurrentSub.BillingPeriodEnd
	}

	evt := billingEvent.NewPlanChangeCreditOnlyEvent(
		ctx,
		userID,
		calc.CurrentSub.SubscriptionID,
		GatewayID,
		oldPlanID,
		oldPeriodID,
		calc.NewPeriod.PricingPlanID,
		calc.NewPeriod.ID,
		calc.CreditToIssue,
		calc.EffectiveDate,
		billingCycleEnd,
	)
	core.Fire(g.coreCtx, billingEvent.EVENT_PLAN_CHANGE_CREDIT_ONLY, evt)

	return &pluginCore.PlanChangeResult{
		Action:        pluginCore.PlanChangeActionComplete,
		CreditApplied: calc.ProrationResult.UnusedCredit,
		ChargeDue:     calc.NetAmountDue,
		EffectiveDate: &calc.EffectiveDate,
	}, nil
}

// handleZeroAmountPlanChange handles plan changes with exact proration match.
// This method skips checkout and immediately activates the new plan.
func (g *AtlosGateway) handleZeroAmountPlanChange(
	ctx context.Context,
	calc *PlanChangeCalculation,
) (*pluginCore.PlanChangeResult, error) {
	userID := calc.CurrentSub.UserID

	// 1. Cancel old subscription
	if err := g.cancelSubscription(ctx, calc.CurrentSub.SubscriptionID, "handleZeroAmountPlanChange"); err != nil {
		return nil, err
	}

	// 2. Deactivate old subscriber
	if err := g.billing.DeactivateSubscriber(ctx, userID, GatewayID); err != nil {
		return nil, fmt.Errorf("failed to deactivate old subscriber: %w", err)
	}

	// 3. Calculate first billing cycle for new plan
	firstCycle := subscription.CalculateFirstCycle(
		time.Now().UTC(),
		subscription.Cadence(calc.NewPeriod.Cadence),
	)

	// 4. Activate new subscription (with zero due amount)
	if err := g.billing.CreateOrUpdateSubscriber(
		ctx,
		userID,
		GatewayID,
		calc.CurrentSub.ExternalID,
		"", // Will get new subscription ID on first payment
		true,
		&calc.NewPeriod.ID,
		pluginCore.WithBillingPeriodStart(&firstCycle.StartAt),
		pluginCore.WithBillingPeriodEnd(&firstCycle.EndAt),
	); err != nil {
		return nil, fmt.Errorf("failed to activate new subscription: %w", err)
	}

	g.coreCtx.Logger().Debug("Zero-amount plan change completed",
		zap.Uint("user_id", userID),
		zap.Uint("new_period_id", calc.NewPeriod.ID))

	// Fire plan change zero amount event
	oldPlanID := calc.OldPeriod.PricingPlanID
	oldPeriodID := calc.OldPeriod.ID
	billingCycleEnd := time.Time{}
	if calc.CurrentSub.BillingPeriodEnd != nil {
		billingCycleEnd = *calc.CurrentSub.BillingPeriodEnd
	}

	evt := billingEvent.NewPlanChangeZeroAmountEvent(
		ctx,
		userID,
		calc.CurrentSub.SubscriptionID,
		GatewayID,
		oldPlanID,
		oldPeriodID,
		calc.NewPeriod.PricingPlanID,
		calc.NewPeriod.ID,
		calc.ProrationResult.UnusedCredit,
		calc.ProrationResult.NewCharge,
		calc.EffectiveDate,
		billingCycleEnd,
	)
	core.Fire(g.coreCtx, billingEvent.EVENT_PLAN_CHANGE_ZERO_AMOUNT, evt)

	return &pluginCore.PlanChangeResult{
		Action:        pluginCore.PlanChangeActionComplete,
		CreditApplied: calc.ProrationResult.UnusedCredit,
		ChargeDue:     decimal.Zero,
		EffectiveDate: &calc.EffectiveDate,
	}, nil
}

// generateProratedCheckoutUI generates checkout UI with prorated amounts.
// This is UI-specific logic and can be tested separately from business logic.
func (g *AtlosGateway) generateProratedCheckoutUI(
	ctx context.Context,
	calc *PlanChangeCalculation,
) (*pluginCore.CheckoutUIResponse, error) {
	userID := calc.CurrentSub.UserID

	// Get user details
	user, err := g.getUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	// Build script fragment
	scriptFragment, err := g.buildScriptFragment()
	if err != nil {
		return nil, fmt.Errorf("failed to build script fragment: %w", err)
	}

	fragments := []pluginCore.CheckoutUIFragment{scriptFragment}

	userName := fmt.Sprintf("%s %s", user.FirstName, user.LastName)

	// Build button fragment with prorated amount
	orderID := GenerateProratedOrderID(userID, calc.NewPeriod.ID)
	buttonFragment, err := g.buildProratedButtonFragment(
		orderID,
		calc.NewPeriod,
		calc.NewPlan.Currency,
		userName,
		user.Email,
		calc.NetAmountDue,
		calc.ProrationResult.UnusedCredit,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to build prorated button fragment: %w", err)
	}
	fragments = append(fragments, buttonFragment)

	response := &pluginCore.CheckoutUIResponse{
		SessionID: orderID,
		ExpiresAt: time.Now().Add(1 * time.Hour),
		Fragments: fragments,
	}

	g.coreCtx.Logger().Debug("Prorated checkout UI created",
		zap.Uint("user_id", userID),
		zap.Uint("period_id", calc.NewPeriod.ID),
		zap.String("net_amount_due", calc.NetAmountDue.String()))

	return response, nil
}

// ValidateWebhook validates a webhook signature
func (g *AtlosGateway) ValidateWebhook(ctx context.Context, signature string, payload []byte) error {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.ValidateWebhook")
	defer span.End()

	if signature == "" {
		return fmt.Errorf("missing signature header")
	}

	var notification atlos.PostbackNotification
	if err := json.Unmarshal(payload, &notification); err != nil {
		return fmt.Errorf("failed to parse postback notification: %w", err)
	}

	valid, err := notification.VerifySignature(g.config.APIKey, signature)
	if err != nil {
		return fmt.Errorf("signature verification failed: %w", err)
	}
	if !valid {
		return fmt.Errorf("invalid signature")
	}

	return nil
}

// HandleWebhook handles incoming webhook events
func (g *AtlosGateway) HandleWebhook(ctx context.Context, payload []byte) error {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.HandleWebhook")
	defer span.End()

	var notification atlos.PostbackNotification
	if err := json.Unmarshal(payload, &notification); err != nil {
		return fmt.Errorf("failed to parse postback notification: %w", err)
	}

	// Validate the notification structure
	if err := notification.Validate(); err != nil {
		return fmt.Errorf("postback notification validation failed: %w", err)
	}

	// Parse OrderId to extract userID and periodID
	// Format: sub-{userID}-{periodID}
	userID, periodID, _, err := ParseOrderID(notification.OrderId)
	if err != nil {
		return fmt.Errorf("failed to parse order ID: %w", err)
	}

	// Get the pricing plan period and validate its plan
	period, err := g.pricing.GetPricingPlanPeriod(ctx, periodID)
	if err != nil {
		return fmt.Errorf("failed to get pricing plan period: %w", err)
	}
	if period == nil {
		return fmt.Errorf("pricing plan period not found")
	}

	planID := period.PricingPlanID
	planModel, err := g.pricing.GetPricingPlan(ctx, planID)
	if err != nil {
		return fmt.Errorf("failed to get pricing plan: %w", err)
	}
	if planModel == nil {
		return fmt.Errorf("pricing plan not found")
	}
	if !planModel.IsActive {
		return fmt.Errorf("plan is not active")
	}

	// TransactionId is the external account identifier
	// SubscriptionId is the subscription object ID for cancellation
	externalID := notification.TransactionId
	subscriptionID := notification.SubscriptionId

	// Check if this is a new subscription or a renewal
	existingSub, err := g.billing.GetActiveSubscriber(ctx, userID, GatewayID)
	if err != nil {
		return fmt.Errorf("failed to check for existing active subscription: %w", err)
	}
	isNewSubscription := (existingSub == nil || existingSub.GatewayType != GatewayID)

	// Calculate billing cycle based on subscription status
	var billingCycle subscription.BillingCycle
	cadence := subscription.Cadence(period.Cadence)

	if isNewSubscription {
		// New subscription: Calculate first cycle starting from now
		billingCycle = subscription.CalculateFirstCycle(time.Now().UTC(), cadence)
	} else if existingSub.BillingPeriodEnd != nil {
		// Renewal: Calculate next cycle based on existing billing cycle
		billingCycle = subscription.CalculateNextCycle(subscription.BillingCycle{
			StartAt: *existingSub.BillingPeriodStart,
			EndAt:   *existingSub.BillingPeriodEnd,
			Cadence: cadence,
		})
	} else {
		billingCycle = subscription.CalculateFirstCycle(time.Now().UTC(), cadence)
	}

	if g.credit != nil {
		periodPrice := decimal.NewFromFloat(period.PriceUSD)
		err = g.credit.IssueUsageCredit(
			ctx,
			uint64(userID),
			pluginCore.TransactionTypeTime,
			periodPrice,
			notification.TransactionId,
			fmt.Sprintf("Subscription period %s to %s",
				billingCycle.StartAt.Format("2006-01-02"),
				billingCycle.EndAt.Format("2006-01-02")),
			0, // createdBy: 0 for system
		)
		if err != nil {
			g.coreCtx.Logger().Error("failed to debit credit for subscription period",
				zap.Uint("user_id", userID),
				zap.Uint("period_id", periodID),
				zap.Error(err))
			return fmt.Errorf("failed to debit credit for subscription period: %w", err)
		}

		g.coreCtx.Logger().Info("subscription period debit issued successfully",
			zap.Uint("user_id", userID),
			zap.Uint("period_id", periodID),
			zap.String("period_start", billingCycle.StartAt.Format("2006-01-02")),
			zap.String("period_end", billingCycle.EndAt.Format("2006-01-02")),
			zap.String("amount", periodPrice.String()))
	}

	if err := g.billing.CreateOrUpdateSubscriber(ctx, userID, g.ID(ctx), externalID, subscriptionID, true, &periodID,
		pluginCore.WithBillingPeriodStart(&billingCycle.StartAt),
		pluginCore.WithBillingPeriodEnd(&billingCycle.EndAt),
	); err != nil {
		return fmt.Errorf("failed to create or update subscriber: %w", err)
	}

	if g.credit != nil && notification.PaidAmount > 0 {
		g.coreCtx.Logger().Debug("atlos payment has paid amount (USD) - credit integration available",
			zap.Uint("user_id", userID),
			zap.String("transaction_id", notification.TransactionId),
			zap.Float64("paid_amount", notification.PaidAmount),
			zap.String("order_currency", notification.OrderCurrency))

		amountStr := strconv.FormatFloat(notification.PaidAmount, 'f', -1, 64)
		amount, err := decimal.NewFromString(amountStr)
		if err != nil {
			g.coreCtx.Logger().Error("failed to convert ATLOS PaidAmount to decimal",
				zap.Uint("user_id", userID),
				zap.String("transaction_id", notification.TransactionId),
				zap.Float64("paid_amount", notification.PaidAmount),
				zap.String("amountStr", amountStr),
				zap.Error(err))
			return fmt.Errorf("failed to convert ATLOS PaidAmount to decimal: %w", err)
		}

		if err := g.credit.IssueCreditWithIdempotency(
			ctx,
			uint64(userID),
			pluginCore.TransactionTypeCharge,
			amount,
			pluginCore.ReferenceTypeAtlosPayment,
			notification.TransactionId,
			"ATLOS payment completed",
			0, // createdBy: 0 for system
		); err != nil {
			return fmt.Errorf("failed to issue ATLOS payment credit: %w", err)
		}

		g.coreCtx.Logger().Info("ATLOS payment credit issued successfully",
			zap.Uint("user_id", userID),
			zap.String("transaction_id", notification.TransactionId),
			zap.String("amount", amount.String()))
	}

	g.coreCtx.Logger().Debug("ATLOS payment webhook processed successfully",
		zap.Uint("user_id", userID),
		zap.Uint("plan_id", planID),
		zap.String("transaction_id", notification.TransactionId),
		zap.String("order_id", notification.OrderId),
		zap.Float64("crypto_amount", notification.Amount),
		zap.Float64("paid_amount", notification.PaidAmount),
		zap.String("order_currency", notification.OrderCurrency),
		zap.String("asset", notification.Asset),
		zap.String("blockchain", notification.Blockchain),
	)

	return nil
}

// GetName returns the display name for the gateway
func (g *AtlosGateway) GetName(ctx context.Context) string {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.GetName")
	defer span.End()

	return "ATLOS"
}

// GetDescription returns the description for the gateway
func (g *AtlosGateway) GetDescription(ctx context.Context) string {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.GetDescription")
	defer span.End()

	return "Accept crypto payments using the ATLOS payment widget"
}

// GetLogo returns the logo image data for this gateway
func (g *AtlosGateway) GetLogo(ctx context.Context) ([]byte, error) {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.GetLogo")
	defer span.End()

	return gateway.ReadGatewayLogo(GatewayID, gatewayLogoFiles, nil)
}

// GetCheckoutUI returns UI fragments for ATLOS checkout flows
// Returns script and button fragments that load the ATLOS widget and initialize it
func (g *AtlosGateway) GetCheckoutUI(ctx context.Context, userID uint, planID uint, periodID uint) (*pluginCore.CheckoutUIResponse, error) {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.GetCheckoutUI")
	defer span.End()

	return core.MetricTrackResult(
		nil,
		CheckoutUIDisplayed.WithLabelValues(LabelStatusError),
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
				return nil, fmt.Errorf("no pricing periods configured for this plan")
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

			// 4. Get user details
			user, err := g.getUser(ctx, userID)
			if err != nil {
				return nil, fmt.Errorf("failed to get user: %w", err)
			}

			// 5. Build response with script fragment
			scriptFragment, err := g.buildScriptFragment()
			if err != nil {
				return nil, fmt.Errorf("failed to build script fragment: %w", err)
			}

			fragments := []pluginCore.CheckoutUIFragment{scriptFragment}

			userName := fmt.Sprintf("%s %s", user.FirstName, user.LastName)

			// 6. Build a single button fragment for the specified period
			orderID := GenerateOrderID(userID, periodID)
			buttonFragment, err := g.buildButtonFragmentForPeriod(orderID, matchedPeriod, plan.Currency, userName, user.Email)
			if err != nil {
				return nil, fmt.Errorf("failed to build button fragment for period %d: %w", periodID, err)
			}
			fragments = append(fragments, buttonFragment)

			response := &pluginCore.CheckoutUIResponse{
				SessionID: orderID,
				ExpiresAt: time.Now().Add(1 * time.Hour),
				Fragments: fragments,
			}

			g.coreCtx.Logger().Debug("ATLOS checkout UI fragments created",
				zap.Uint("user_id", userID),
				zap.Uint("plan_id", planID),
				zap.Uint("period_id", periodID),
			)

			return response, nil
		},
	)
}

// GetCustomerPortalMetadata returns metadata for ATLOS customer portal
func (g *AtlosGateway) GetCustomerPortalMetadata(ctx context.Context, userID uint) (map[string]interface{}, error) {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.GetCustomerPortalMetadata")
	defer span.End()

	return map[string]any{}, nil
}

// SupportsProductSync returns false - ATLOS does not require product sync
func (g *AtlosGateway) SupportsProductSync() bool {
	return false
}

// SupportsPriceUpdates returns false - ATLOS does not support price updates
func (g *AtlosGateway) SupportsPriceUpdates() bool {
	return false
}

// SupportsPlanDeletion returns false - ATLOS does not support plan deletion
func (g *AtlosGateway) SupportsPlanDeletion() bool {
	return false
}

// RequiredPricingFields returns fields required for pricing plan creation
func (g *AtlosGateway) RequiredPricingFields() []string {
	return []string{}
}

// SyncPlan synchronizes a pricing plan with ATLOS (not supported)
// ATLOS uses widget-based checkout with inline configuration
func (g *AtlosGateway) SyncPlan(ctx context.Context, plan *pluginCore.PricingPlanInfo) (*pluginCore.SyncResult, error) {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.SyncPlan")
	defer span.End()

	return &pluginCore.SyncResult{
		Success: false,
		Error:   fmt.Errorf("ATLOS does not require product synchronization"),
	}, nil
}

// validateServices validates that required services are available
func (g *AtlosGateway) validateServices() error {
	if g.users == nil {
		return fmt.Errorf("user service not configured")
	}
	if g.quota == nil {
		return fmt.Errorf("quota service not configured")
	}
	return nil
}

// getUser retrieves and validates a user exists
func (g *AtlosGateway) getUser(ctx context.Context, userID uint) (*models.User, error) {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.getUser")
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

// buildScriptFragment creates a script fragment that loads the ATLOS JavaScript SDK
func (g *AtlosGateway) buildScriptFragment() (pluginCore.CheckoutUIFragment, error) {
	return pluginCore.CheckoutUIFragment{
		Type:   pluginCore.FragmentTypeScriptURL,
		Script: "https://atlos.io/packages/app/atlos.js",
	}, nil
}

// buildPaymentConfigData creates the configuration data for the ATLOS payment button template
func buildPaymentConfigData(merchantID string, orderID string, period *billingModels.PricingPlanPeriod, currency string, userName string, userEmail string, postbackURL string) atlosPaymentConfigData {
	buttonID := fmt.Sprintf("atlos-pay-btn-%s", orderID)
	return atlosPaymentConfigData{
		ButtonID:    buttonID,
		MerchantID:  merchantID,
		OrderID:     orderID,
		Amount:      period.PriceUSD,
		Currency:    currency,
		UserName:    userName,
		UserEmail:   userEmail,
		PostbackURL: postbackURL,
	}
}

// buildButtonFragmentForPeriod creates a button fragment that initializes and triggers the ATLOS payment widget for a specific pricing period
func (g *AtlosGateway) buildButtonFragmentForPeriod(orderID string, period *billingModels.PricingPlanPeriod, currency string, userName string, userEmail string) (pluginCore.CheckoutUIFragment, error) {
	data := buildPaymentConfigData(g.getMerchantID(), orderID, period, currency, userName, userEmail, g.getPostbackURL())

	tmpl, err := template.New("atlosPaymentConfig").Funcs(template.FuncMap{
		"quote": func(s string) string {
			return fmt.Sprintf("%q", s)
		},
	}).ParseFS(templatesFS, "templates/payment_button.tpl")
	if err != nil {
		return pluginCore.CheckoutUIFragment{}, fmt.Errorf("failed to parse template: %w", err)
	}

	var scriptBuf strings.Builder
	if err := tmpl.ExecuteTemplate(&scriptBuf, paymentButtonTemplate, data); err != nil {
		return pluginCore.CheckoutUIFragment{}, fmt.Errorf("failed to execute template: %w", err)
	}

	buttonHTML := fmt.Sprintf(`<button id="%s">Pay %s %.2f - %s billing</button>`, data.ButtonID, currency, period.PriceUSD, period.Cadence)
	scriptHTML := fmt.Sprintf(`<script>%s</script>`, scriptBuf.String())

	return pluginCore.CheckoutUIFragment{
		Type:   pluginCore.FragmentTypeButton,
		HTML:   buttonHTML,
		Script: scriptHTML,
	}, nil
}

// buildButtonFragment creates a button fragment that initializes and triggers the ATLOS payment widget
func (g *AtlosGateway) buildButtonFragment(orderID string, amount float64, currency string, userName string, userEmail string) (pluginCore.CheckoutUIFragment, error) {
	// Generate unique button ID
	buttonID := fmt.Sprintf("atlos-pay-btn-%s", orderID)

	// Use FuncMap with printf "%q" for proper string quoting
	tmpl, err := template.New("atlosPaymentConfig").Funcs(template.FuncMap{
		"quote": func(s string) string {
			return fmt.Sprintf("%q", s)
		},
	}).ParseFS(templatesFS, "templates/payment_button.tpl")
	if err != nil {
		return pluginCore.CheckoutUIFragment{}, fmt.Errorf("failed to parse template: %w", err)
	}

	data := atlosPaymentConfigData{
		ButtonID:    buttonID,
		MerchantID:  g.getMerchantID(),
		OrderID:     orderID,
		Amount:      amount,
		Currency:    currency,
		UserName:    userName,
		UserEmail:   userEmail,
		PostbackURL: g.getPostbackURL(),
	}

	var scriptBuf strings.Builder
	if err := tmpl.ExecuteTemplate(&scriptBuf, paymentButtonTemplate, data); err != nil {
		return pluginCore.CheckoutUIFragment{}, fmt.Errorf("failed to execute template: %w", err)
	}

	// Build button HTML with unique ID and script with event listener
	buttonHTML := fmt.Sprintf(`<button id="%s">Pay with Crypto</button>`, buttonID)
	scriptHTML := fmt.Sprintf(`<script>%s</script>`, scriptBuf.String())

	return pluginCore.CheckoutUIFragment{
		Type:   pluginCore.FragmentTypeButton,
		HTML:   buttonHTML,
		Script: scriptHTML,
	}, nil
}

// getMerchantID retrieves the ATLOS merchant ID from configuration
func (g *AtlosGateway) getMerchantID() string {
	return g.config.MerchantID
}

// getPostbackURL returns the postback URL for payment notifications
// Uses the HTTP service to build full URL with account subdomain and protocol
func (g *AtlosGateway) getPostbackURL() string {
	secure := g.coreCtx.Config().Config().Core.Secure
	return gateway.BuildAbsoluteURL(g.http, gateway.DashboardPluginID, "/api/billing/webhook/atlos", secure)
}

// buildProratedButtonFragment creates a button fragment for prorated plan changes.
// This UI-specific method can be tested independently from business logic.
func (g *AtlosGateway) buildProratedButtonFragment(
	orderID string,
	period *billingModels.PricingPlanPeriod,
	currency string,
	userName string,
	userEmail string,
	netAmount decimal.Decimal,
	creditAmount decimal.Decimal,
) (pluginCore.CheckoutUIFragment, error) {
	// Generate unique button ID
	buttonID := fmt.Sprintf("atlos-pay-btn-%s", orderID)

	data := atlosPaymentConfigData{
		ButtonID:       buttonID,
		MerchantID:     g.getMerchantID(),
		OrderID:        orderID,
		Amount:         netAmount.InexactFloat64(), // KEY: Prorated net amount
		Currency:       currency,
		UserName:       userName,
		UserEmail:      userEmail,
		PostbackURL:    g.getPostbackURL(),
		CreditAmount:   creditAmount.InexactFloat64(),
		RecurringAmount: period.PriceUSD,
		RecurringUnit:  period.Cadence,
		RecurringInterval: 1,
	}

	tmpl, err := template.New("atlosPaymentConfig").Funcs(template.FuncMap{
		"quote": func(s string) string {
			return fmt.Sprintf("%q", s)
		},
	}).ParseFS(templatesFS, "templates/payment_button.tpl")
	if err != nil {
		return pluginCore.CheckoutUIFragment{}, fmt.Errorf("failed to parse template: %w", err)
	}

	var scriptBuf strings.Builder
	if err := tmpl.ExecuteTemplate(&scriptBuf, paymentButtonTemplate, data); err != nil {
		return pluginCore.CheckoutUIFragment{}, fmt.Errorf("failed to execute template: %w", err)
	}

	// Build button HTML with prorated amount display
	var buttonLabel string
	if creditAmount.GreaterThan(decimal.Zero) {
		buttonLabel = fmt.Sprintf(`Pay %s %.2f - Prorated from %s %s billing`, 
			currency, netAmount.InexactFloat64(), currency, period.Cadence)
	} else {
		buttonLabel = fmt.Sprintf(`Pay %s %.2f - %s billing`, 
			currency, netAmount.InexactFloat64(), period.Cadence)
	}

	buttonHTML := fmt.Sprintf(`<button id="%s">%s</button>`, buttonID, buttonLabel)
	scriptHTML := fmt.Sprintf(`<script>%s</script>`, scriptBuf.String())

	return pluginCore.CheckoutUIFragment{
		Type:   pluginCore.FragmentTypeButton,
		HTML:   buttonHTML,
		Script: scriptHTML,
	}, nil
}

// GetManagementInfo returns management capabilities for operations
func (g *AtlosGateway) GetManagementInfo(ctx context.Context, userID uint) (*pluginCore.ManagementCapabilities, error) {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.GetManagementInfo")
	defer span.End()

	// Atlas supports only API-based operations for both user and admin
	operations := map[pluginCore.ManagementOperation]bool{
		pluginCore.OperationCancel:     true,
		pluginCore.OperationChangePlan: true,
	}

	return &pluginCore.ManagementCapabilities{
		ManagementMode:  pluginCore.ModeAPI,
		Operations:      operations,
		AdminOperations: operations, // Same operations for admin
	}, nil
}

// GetManagementURL returns the appropriate action for a management operation
func (g *AtlosGateway) GetManagementURL(ctx context.Context, userID uint, operation pluginCore.ManagementOperation) (*pluginCore.ManagementResult, error) {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.GetManagementURL")
	defer span.End()

	// Check if user has an active Atlas subscription
	subscriber, err := g.billing.GetActiveSubscription(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get active subscription: %w", err)
	}
	if subscriber == nil || subscriber.GatewayType != GatewayID {
		return nil, fmt.Errorf("no active Atlas subscription found for user %d", userID)
	}

	switch operation {
	case pluginCore.OperationCancel:
		endpoint := pluginCore.GetManagementAPIEndpoint(pluginCore.OperationCancel)
		if endpoint == nil {
			return &pluginCore.ManagementResult{
				Action:       pluginCore.ActionError,
				ErrorMessage: "Operation not configured with a predefined endpoint",
			}, nil
		}
		return &pluginCore.ManagementResult{
			Action:      pluginCore.ActionAPIRequired,
			APIEndpoint: endpoint,
		}, nil

	case pluginCore.OperationChangePlan:
		endpoint := pluginCore.GetManagementAPIEndpoint(pluginCore.OperationChangePlan)
		if endpoint == nil {
			return &pluginCore.ManagementResult{
				Action:       pluginCore.ActionError,
				ErrorMessage: "Operation not configured with a predefined endpoint",
			}, nil
		}
		return &pluginCore.ManagementResult{
			Action:      pluginCore.ActionAPIRequired,
			APIEndpoint: endpoint,
		}, nil

	default:
		return &pluginCore.ManagementResult{
			Action:       pluginCore.ActionUnsupported,
			ErrorMessage: fmt.Sprintf("operation %s is not supported by ATLOS", operation),
		}, nil
	}
}

// parseOrderID parses an order ID and returns userID and periodID.
// Deprecated: Use ParseOrderID instead.
func parseOrderID(orderID string) (uint, uint, error) {
	userID, periodID, _, err := ParseOrderID(orderID)
	return userID, periodID, err
}


