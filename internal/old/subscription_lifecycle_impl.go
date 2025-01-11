package old

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/avast/retry-go"
	"github.com/go-openapi/strfmt"
	"github.com/killbill/kbcli/v3/kbmodel"
	"go.lumeweb.com/portal-plugin-billing/internal/client/hyperswitch"
	"go.uber.org/zap"
	"strings"
	"time"
)

type SubscriptionLifecycleImpl struct {
	billingService    *BillingServiceDefault
	hyperswitchClient *hyperswitch.Client
	logger            *zap.Logger
}

func NewSubscriptionLifecycle(billing *BillingServiceDefault, logger *zap.Logger) SubscriptionLifecycle {
	return &SubscriptionLifecycleImpl{
		billingService: billing,
		hyperswitchClient: hyperswitch.NewClient(hyperswitch.ClientConfig{
			BaseURL: billing.cfg.Hyperswitch.APIServer,
			APIKey:  billing.cfg.Hyperswitch.APIKey,
		}, logger),
		logger: logger,
	}
}

func (s *SubscriptionLifecycleImpl) CreateSubscription(ctx context.Context, userID uint, planID string) (*SubscriptionResponse, error) {
	// Validate plan exists
	_, err := s.billingService.getPlanByIdentifier(ctx, planID)
	if err != nil {
		return nil, fmt.Errorf("invalid plan: %w", err)
	}

	// Get account using repository
	acct, err := s.billingService.kbRepo.GetAccountByUserId(ctx, userID)

	if err != nil {
		return nil, fmt.Errorf("failed to get account: %w", err)
	}

	// Create subscription using repository
	resp, err := s.billingService.kbRepo.CreateSubscription(ctx, &kbmodel.Subscription{
		AccountID: acct.Payload.AccountID,
		PlanName:  &planID,
		State:     kbmodel.SubscriptionStatePENDING,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create subscription: %w", err)
	}

	// Parse subscription ID from Location header
	subID, err := parseSubscriptionIDFromLocation(resp.HttpResponse.GetHeader("Location"))
	if err != nil {
		return nil, fmt.Errorf("failed to parse subscription ID: %w", err)
	}

	// Get subscription details using repository
	sub, err := s.billingService.kbRepo.GetSubscription(ctx, strfmt.UUID(subID))
	if err != nil {
		return nil, fmt.Errorf("failed to get subscription: %w", err)
	}

	// Create payment intent with retries
	var intent *PaymentIntent
	err = retry.Do(func() error {
		var err error
		intent, err = s.createPaymentIntent(ctx, sub.Payload)
		if err != nil {
			if strings.Contains(err.Error(), "503") || strings.Contains(err.Error(), "timeout") {
				return ErrRetryable
			}
			// Cleanup subscription on non-retryable failure
			_, _ = s.billingService.CancelSubscription(ctx, userID, nil)
			return err
		}
		return nil
	}, retry.Attempts(3), retry.Delay(1*time.Second))

	if err != nil {
		return nil, fmt.Errorf("failed to create payment intent: %w", err)
	}

	// Set pending flag
	err = s.billingService.setCustomField(ctx, sub.Payload.SubscriptionID, pendingCustomField, "1")
	if err != nil {
		_, _ = s.billingService.CancelSubscription(ctx, userID, nil)
		return nil, fmt.Errorf("failed to set pending status: %w", err)
	}

	return &SubscriptionResponse{
		Subscription:  sub.Payload,
		PaymentIntent: intent,
	}, nil
}

func (s *SubscriptionLifecycleImpl) UpdatePaymentMethod(ctx context.Context, userID uint) (*PaymentMethodResponse, error) {
	// Create $0 authorization intent
	intent, err := s.createAuthIntent(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to create auth intent: %w", err)
	}

	return &PaymentMethodResponse{
		ClientSecret: intent.ClientSecret,
	}, nil
}

func (s *SubscriptionLifecycleImpl) ChangePlan(ctx context.Context, userID uint, newPlanID string) (*PlanChangeResponse, error) {
	// Get account and subscription
	acct, err := s.billingService.kbRepo.GetAccountByUserId(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get account: %w", err)
	}

	bundles, err := s.billingService.kbRepo.GetBundlesByAccountId(ctx, acct.Payload.AccountID)
	if err != nil {
		return nil, fmt.Errorf("failed to get bundles: %w", err)
	}

	sub := findActiveSubscription(bundles.Payload)
	if sub == nil {
		return nil, ErrSubscriptionNotFound
	}

	// Validate new plan exists
	_, err = s.billingService.getPlanByIdentifier(ctx, newPlanID)
	if err != nil {
		return nil, fmt.Errorf("invalid plan: %w", err)
	}

	// Determine change policy
	policy := s.determineChangePolicy(sub, newPlanID)

	// Update subscription plan
	err = s.billingService.kbRepo.UpdateSubscriptionPlan(ctx, sub.SubscriptionID, newPlanID)
	if err != nil {
		return nil, fmt.Errorf("failed to change plan: %w", err)
	}

	// Get updated subscription
	updatedSub, err := s.billingService.kbRepo.GetSubscription(ctx, sub.SubscriptionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get updated subscription: %w", err)
	}

	// Calculate price difference if needed
	var priceChange float64
	if policy == "IMMEDIATE" {
		oldPrice := sub.Prices[0].RecurringPrice
		newPrice := updatedSub.Payload.Prices[0].RecurringPrice
		priceChange = newPrice - oldPrice
	}

	return &PlanChangeResponse{
		Status:       string(updatedSub.Payload.State),
		PriceChange:  priceChange,
		ClientSecret: "", // Empty since no payment is needed for this change
	}, nil
}

func (s *SubscriptionLifecycleImpl) handleStateTransition(ctx context.Context, sub *kbmodel.Subscription, newState kbmodel.SubscriptionStateEnum) error {
	// Determine billing policy based on state
	var billingPolicy *string
	switch newState {
	case kbmodel.SubscriptionStateCANCELLED:
		policy := s.determineCancellationPolicy(sub)
		billingPolicy = &policy
	case kbmodel.SubscriptionStateACTIVE:
		policy := "IMMEDIATE"
		billingPolicy = &policy
	}

	// Update subscription state
	err := s.billingService.kbRepo.UpdateSubscription(ctx, sub.SubscriptionID, &kbmodel.Subscription{
		State:         newState,
		BillingPeriod: (*kbmodel.SubscriptionBillingPeriodEnum)(billingPolicy),
	})
	if err != nil {
		return fmt.Errorf("failed to update subscription state: %w", err)
	}

	s.logger.Info("subscription state transition requested",
		zap.String("subscription_id", sub.SubscriptionID.String()),
		zap.String("requested_state", string(newState)))

	return nil
}

func (s *SubscriptionLifecycleImpl) determineCancellationPolicy(sub *kbmodel.Subscription) string {
	// Default to END_OF_TERM for BASE products
	if sub.ProductCategory == kbmodel.SubscriptionProductCategoryBASE {
		return "END_OF_TERM"
	}

	// Default to IMMEDIATE for ADD_ON products
	if sub.ProductCategory == kbmodel.SubscriptionProductCategoryADDON {
		return "IMMEDIATE"
	}

	// Default fallback
	return "END_OF_TERM"
}

func (s *SubscriptionLifecycleImpl) determineChangePolicy(sub *kbmodel.Subscription, newPlanID string) string {
	// Allow immediate changes during trial
	if sub.PhaseType == kbmodel.SubscriptionPhaseTypeTRIAL {
		return "IMMEDIATE"
	}

	// Default to END_OF_TERM for plan changes
	return "END_OF_TERM"
}

func (s *SubscriptionLifecycleImpl) CancelSubscription(ctx context.Context, userID uint) error {
	// Get account and subscription
	acct, err := s.billingService.kbRepo.GetAccountByUserId(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to get account: %w", err)
	}

	bundles, err := s.billingService.kbRepo.GetBundlesByAccountId(ctx, acct.Payload.AccountID)
	if err != nil {
		return fmt.Errorf("failed to get bundles: %w", err)
	}

	sub := findActiveSubscription(bundles.Payload)
	if sub == nil {
		return ErrSubscriptionNotFound
	}

	// Determine cancellation policy
	policy := s.determineCancellationPolicy(sub)

	// Cancel subscription
	err = s.billingService.kbRepo.CancelSubscription(ctx, sub.SubscriptionID, &policy)
	if err != nil {
		return fmt.Errorf("failed to cancel subscription: %w", err)
	}

	// Transition to CANCELLED state
	err = s.handleStateTransition(ctx, sub, kbmodel.SubscriptionStateCANCELLED)
	if err != nil {
		s.logger.Error("failed to transition subscription state after cancellation",
			zap.Error(err),
			zap.String("subscription_id", sub.SubscriptionID.String()))
		// Don't fail the cancellation for state transition error
	}

	return nil
}

func (s *SubscriptionLifecycleImpl) HandleWebhook(ctx context.Context, event *hyperswitch.WebhookEvent, signature string) error {
	// Verify webhook signature using HMAC-SHA512
	if err := s.verifyWebhookSignature(event, signature); err != nil {
		s.logger.Error("invalid webhook signature",
			zap.Error(err),
			zap.String("event_type", event.Type))
		return ErrInvalidSignature
	}

	s.logger.Info("proxying webhook event to Kill Bill",
		zap.String("type", event.Type),
		zap.String("payment_id", event.Data.PaymentID))

	// Forward webhook to Kill Bill
	// Kill Bill will handle subscription state transitions based on payment status
	err := s.billingService.kbRepo.ProcessWebhook(ctx, event)
	if err != nil {
		s.logger.Error("failed to proxy webhook to Kill Bill",
			zap.Error(err),
			zap.String("event_type", event.Type))
		return err
	}

	return nil
}

func (s *SubscriptionLifecycleImpl) GetSubscriptionStatus(ctx context.Context, userID uint) (*SubscriptionStatus, error) {
	acct, err := s.billingService.kbRepo.GetAccountByUserId(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get account: %w", err)
	}

	bundles, err := s.billingService.kbRepo.GetBundlesByAccountId(ctx, acct.Payload.AccountID)
	if err != nil {
		return nil, fmt.Errorf("failed to get bundles: %w", err)
	}

	sub := findActiveOrPendingSubscription(bundles.Payload)
	if sub == nil {
		return &SubscriptionStatus{
			Status:        "NONE",
			PaymentStatus: "NONE",
		}, nil
	}

	var paymentStatus string
	var currentPeriodEnd time.Time

	if chargedDate := time.Time(sub.ChargedThroughDate); !chargedDate.IsZero() {
		paymentStatus = chargedDate.Format(time.RFC3339)
		currentPeriodEnd = chargedDate
	}

	return &SubscriptionStatus{
		Status:            string(sub.State),
		PaymentStatus:     paymentStatus,
		CurrentPeriodEnd:  currentPeriodEnd,
		CancelAtPeriodEnd: time.Time(sub.BillingEndDate).IsZero(),
	}, nil
}

func (s *SubscriptionLifecycleImpl) createPaymentIntent(ctx context.Context, sub *kbmodel.Subscription) (*PaymentIntent, error) {
	// Get account details
	acct, err := s.billingService.kbRepo.GetAccount(ctx, sub.AccountID)
	if err != nil {
		return nil, err
	}

	// Calculate amount
	amount := float64(0)
	if len(sub.Prices) > 0 {
		amount = sub.Prices[0].RecurringPrice
	}

	// Create payment request
	req := &hyperswitch.PaymentRequest{
		Amount:   amount * 100, // Convert to cents
		Currency: string(acct.Payload.Currency),
		Customer: hyperswitch.Customer{
			ID:    sub.AccountID.String(),
			Name:  acct.Payload.Name,
			Email: acct.Payload.Email,
		},
		Description: fmt.Sprintf("Subscription to %s", *sub.PlanName),
		Metadata: hyperswitch.PaymentMetadata{
			SubscriptionID: sub.SubscriptionID.String(),
			PlanID:         *sub.PlanName,
		},
	}

	// Create payment
	resp, err := s.hyperswitchClient.CreatePayment(ctx, req)
	if err != nil {
		return nil, err
	}

	return &PaymentIntent{
		ID:           resp.PaymentID,
		ClientSecret: resp.ClientSecret,
		Amount:       amount,
		Status:       "pending",
	}, nil
}

func (s *SubscriptionLifecycleImpl) createAuthIntent(ctx context.Context, userID uint) (*PaymentIntent, error) {
	acct, err := s.billingService.kbRepo.GetAccountByUserId(ctx, userID)
	if err != nil {
		return nil, err
	}

	req := &hyperswitch.PaymentRequest{
		Amount:   0,
		Currency: string(acct.Payload.Currency),
		Customer: hyperswitch.Customer{
			ID:    acct.Payload.AccountID.String(),
			Name:  acct.Payload.Name,
			Email: acct.Payload.Email,
		},
		Description: "Payment method authorization",
		PaymentType: "setup_mandate",
	}

	resp, err := s.hyperswitchClient.CreatePayment(ctx, req)
	if err != nil {
		return nil, err
	}

	return &PaymentIntent{
		ID:           resp.PaymentID,
		ClientSecret: resp.ClientSecret,
		Amount:       0,
		Status:       "pending",
	}, nil
}

func (s *SubscriptionLifecycleImpl) createUpgradeIntent(ctx context.Context, userID uint, amount float64) (*PaymentIntent, error) {
	acct, err := s.billingService.kbRepo.GetAccountByUserId(ctx, userID)
	if err != nil {
		return nil, err
	}

	req := &hyperswitch.PaymentRequest{
		Amount:   amount * 100,
		Currency: string(acct.Payload.Currency),
		Customer: hyperswitch.Customer{
			ID:    acct.Payload.AccountID.String(),
			Name:  acct.Payload.Name,
			Email: acct.Payload.Email,
		},
		Description: "Subscription upgrade",
	}

	resp, err := s.hyperswitchClient.CreatePayment(ctx, req)
	if err != nil {
		return nil, err
	}

	return &PaymentIntent{
		ID:           resp.PaymentID,
		ClientSecret: resp.ClientSecret,
		Amount:       amount,
		Status:       "pending",
	}, nil
}

func (s *SubscriptionLifecycleImpl) calculatePriceDifference(ctx context.Context, currentPlanID, newPlanID string) (float64, error) {
	currentPrice, err := s.billingService.getPlanPriceById(ctx, currentPlanID, "EVERGREEN", "USD")
	if err != nil {
		return 0, err
	}

	newPrice, err := s.billingService.getPlanPriceById(ctx, newPlanID, "EVERGREEN", "USD")
	if err != nil {
		return 0, err
	}

	return newPrice - currentPrice, nil
}

func (s *SubscriptionLifecycleImpl) verifyWebhookSignature(event *hyperswitch.WebhookEvent, signature string) error {
	if signature == "" {
		return ErrInvalidSignature
	}

	webhookSecret := s.billingService.cfg.Hyperswitch.WebhookSecret
	if webhookSecret == "" {
		return fmt.Errorf("webhook secret not configured")
	}

	// Create HMAC
	mac := hmac.New(sha256.New, []byte(webhookSecret))
	eventBytes, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}
	mac.Write(eventBytes)
	expectedSignature := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(signature), []byte(expectedSignature)) {
		return ErrInvalidSignature
	}

	return nil
}

func (s *SubscriptionLifecycleImpl) ValidatePaymentMethod(ctx context.Context, paymentMethodID string) error {
	err := s.billingService.kbRepo.ValidatePaymentMethod(ctx, strfmt.UUID(paymentMethodID))
	if err != nil {
		return ErrPaymentMethodInvalid
	}
	return nil
}

func (s *SubscriptionLifecycleImpl) DeletePaymentMethod(ctx context.Context, userID uint, paymentMethodID string) error {
	// Get account
	acct, err := s.billingService.kbRepo.GetAccountByUserId(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to get account: %w", err)
	}

	// Delete payment method from KillBill
	err = s.billingService.kbRepo.DeletePaymentMethod(ctx, acct.Payload.AccountID, strfmt.UUID(paymentMethodID), true)
	if err != nil {
		return fmt.Errorf("failed to delete payment method: %w", err)
	}

	return nil
}
