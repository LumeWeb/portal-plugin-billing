package service

import (
	"context"
	"fmt"
	"github.com/killbill/kbcli/v3/kbmodel"
	"github.com/samber/lo"
	"go.lumeweb.com/portal-plugin-billing/internal/api/messages"
	"go.uber.org/zap"
	"sync"
	"time"
)

// SubscriptionManager defines the interface for subscription management operations
type SubscriptionManager interface {
	// GetSubscription retrieves a subscription for a user
	GetSubscription(ctx context.Context, userID uint) (*messages.SubscriptionResponse, error)

	// InvalidateCache removes the cached subscription for a user
	InvalidateCache(userID uint)

	// UpdateSubscription changes a user's subscription
	UpdateSubscription(ctx context.Context, userID uint, planID string) error

	// CancelSubscription cancels a user's subscription
	CancelSubscription(ctx context.Context, userID uint, req *messages.CancellationRequest) (*messages.CancellationResponse, error)

	// GetPlans retrieves available subscription plans
	GetPlans(ctx context.Context) ([]*messages.SubscriptionPlan, error)

	// UpdateBillingInfo updates a user's billing information
	UpdateBillingInfo(ctx context.Context, userID uint, billingInfo *messages.BillingInfo) error

	// RequestPaymentMethodChange initiates a payment method change
	RequestPaymentMethodChange(ctx context.Context, userID uint) (*messages.RequestPaymentMethodChangeResponse, error)

	// UpdatePaymentMethod updates the payment method for a user
	UpdatePaymentMethod(ctx context.Context, userID uint, paymentMethodID string) error
}

// SubscriptionManagerDefault handles subscription-related operations and caching
type SubscriptionManagerDefault struct {
	billingService *BillingServiceDefault
	logger         *zap.Logger
	cache          sync.Map // map[uint]*cachedSubscription
}

type cachedSubscription struct {
	subscription *messages.SubscriptionResponse
	expiresAt    time.Time
}

const cacheDuration = 5 * time.Minute

// NewSubscriptionManager creates a new subscription manager
func NewSubscriptionManager(billingService *BillingServiceDefault, logger *zap.Logger) SubscriptionManager {
	return &SubscriptionManagerDefault{
		billingService: billingService,
		logger:         logger.Named("subscription-manager"),
	}
}

// GetSubscription retrieves a subscription, using cache if available
func (sm *SubscriptionManagerDefault) GetSubscription(ctx context.Context, userID uint) (*messages.SubscriptionResponse, error) {
	// Check cache first
	if cached, ok := sm.cache.Load(userID); ok {
		cachedSub := cached.(*cachedSubscription)
		if time.Now().Before(cachedSub.expiresAt) {
			sm.logger.Debug("subscription cache hit",
				zap.Uint("user_id", userID))
			return cachedSub.subscription, nil
		}
		sm.logger.Debug("subscription cache expired",
			zap.Uint("user_id", userID))
		sm.cache.Delete(userID)
	}

	if !sm.billingService.enabled() {
		return nil, nil
	}

	if !sm.billingService.paidEnabled() {
		freePlan := sm.billingService.getFreePlan()
		resp := &messages.SubscriptionResponse{
			Plan:        freePlan,
			BillingInfo: messages.BillingInfo{},
			PaymentInfo: messages.PaymentInfo{},
		}
		sm.cacheSubscription(userID, resp)
		return resp, nil
	}

	// Get fresh subscription data
	err := sm.billingService.CreateCustomerById(ctx, userID)
	if err != nil {
		return nil, err
	}

	acct, err := sm.billingService.kbRepo.GetAccountByUserId(ctx, userID)
	if err != nil {
		return nil, err
	}

	bundles, err := sm.billingService.kbRepo.GetBundlesByAccountId(ctx, acct.Payload.AccountID)
	if err != nil {
		return nil, err
	}

	var subPlan *messages.SubscriptionPlan
	var paymentID string
	var clientSecret string
	var paymentExpires time.Time
	publishableKey := sm.billingService.cfg.Hyperswitch.PublishableKey

	sub := findActiveOrPendingSubscription(bundles.Payload)

	if sub != nil {
		plan, err := sm.billingService.getPlanByIdentifier(ctx, *sub.PlanName)
		if err != nil {
			return nil, err
		}

		planName, err := sm.billingService.getPlanNameById(ctx, *sub.PlanName)
		if err != nil {
			return nil, err
		}

		prices := lo.Filter(sub.Prices, func(price *kbmodel.PhasePrice, _ int) bool {
			return kbmodel.SubscriptionPhaseTypeEnum(price.PhaseType) == sub.PhaseType
		})

		if len(prices) > 0 {
			subPlan = &messages.SubscriptionPlan{
				Name:       planName,
				Price:      prices[0].RecurringPrice,
				Status:     remoteSubscriptionStatusToLocal(sub.State),
				Identifier: plan.Identifier,
				Period:     remoteSubscriptionPhaseToLocal(kbmodel.SubscriptionPhaseTypeEnum(*sub.BillingPeriod)),
				Storage:    plan.Storage,
				Upload:     plan.Upload,
				Download:   plan.Download,
				StartDate:  &sub.StartDate,
			}

			cfState, err := sm.billingService.getCustomField(ctx, sub.SubscriptionID, pendingCustomField)
			if err != nil {
				return nil, err
			}

			if sub.State == kbmodel.SubscriptionStatePENDING || (cfState != nil && *cfState.Value == "1") {
				// Get the client secret
				_paymentID, err := sm.billingService.getCustomField(ctx, sub.SubscriptionID, paymentIdCustomField)
				if err != nil {
					return nil, err
				}

				if _paymentID != nil {
					_clientSecret, created, err := sm.billingService.fetchClientSecret(ctx, *_paymentID.Value)
					if err != nil {
						return nil, err
					}

					paymentID = *_paymentID.Value
					paymentExpires = created.Add(15 * time.Minute)
					clientSecret = _clientSecret
					subPlan.Status = messages.SubscriptionPlanStatusPending
				}
			}
		}
	}

	if subPlan == nil {
		subPlan = sm.billingService.getFreePlan()
	}

	resp := &messages.SubscriptionResponse{
		Plan: subPlan,
		BillingInfo: messages.BillingInfo{
			Name:    acct.Payload.Name,
			Address: acct.Payload.Address1,
			City:    acct.Payload.City,
			State:   acct.Payload.State,
			Zip:     acct.Payload.PostalCode,
			Country: acct.Payload.Country,
		},
		PaymentInfo: messages.PaymentInfo{
			PaymentID:      paymentID,
			PaymentExpires: paymentExpires,
			ClientSecret:   clientSecret,
			PublishableKey: publishableKey,
		},
	}

	sm.cacheSubscription(userID, resp)
	return resp, nil
}

func (sm *SubscriptionManagerDefault) cacheSubscription(userID uint, subscription *messages.SubscriptionResponse) {
	sm.cache.Store(userID, &cachedSubscription{
		subscription: subscription,
		expiresAt:    time.Now().Add(cacheDuration),
	})
	sm.logger.Debug("cached subscription", zap.Uint("user_id", userID))
}

// InvalidateCache removes the cached subscription for a user
func (sm *SubscriptionManagerDefault) InvalidateCache(userID uint) {
	sm.cache.Delete(userID)
	sm.logger.Debug("invalidated subscription cache",
		zap.Uint("user_id", userID))
}

// UpdateSubscription changes a user's subscription and invalidates cache
func (sm *SubscriptionManagerDefault) UpdateSubscription(ctx context.Context, userID uint, planID string) error {
	err := sm.billingService.ChangeSubscription(ctx, userID, planID)
	if err != nil {
		return fmt.Errorf("failed to change subscription: %w", err)
	}

	sm.InvalidateCache(userID)
	return nil
}

// CancelSubscription cancels a user's subscription
func (sm *SubscriptionManagerDefault) CancelSubscription(ctx context.Context, userID uint, req *messages.CancellationRequest) (*messages.CancellationResponse, error) {
	resp, err := sm.billingService.CancelSubscription(ctx, userID, req)
	if err != nil {
		return nil, fmt.Errorf("failed to cancel subscription: %w", err)
	}

	sm.InvalidateCache(userID)
	return resp, nil
}

// GetPlans retrieves available subscription plans
func (sm *SubscriptionManagerDefault) GetPlans(ctx context.Context) ([]*messages.SubscriptionPlan, error) {
	return sm.billingService.GetPlans(ctx)
}

// UpdateBillingInfo updates a user's billing information
func (sm *SubscriptionManagerDefault) UpdateBillingInfo(ctx context.Context, userID uint, billingInfo *messages.BillingInfo) error {
	err := sm.billingService.UpdateBillingInfo(ctx, userID, billingInfo)
	if err != nil {
		return fmt.Errorf("failed to update billing info: %w", err)
	}

	sm.InvalidateCache(userID)
	return nil
}

// RequestPaymentMethodChange initiates a payment method change
func (sm *SubscriptionManagerDefault) RequestPaymentMethodChange(ctx context.Context, userID uint) (*messages.RequestPaymentMethodChangeResponse, error) {
	response, err := sm.billingService.RequestPaymentMethodChange(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to request payment method change: %w", err)
	}

	sm.InvalidateCache(userID)
	return response, nil
}

// UpdatePaymentMethod updates the payment method for a user
func (sm *SubscriptionManagerDefault) UpdatePaymentMethod(ctx context.Context, userID uint, paymentMethodID string) error {
	if !sm.billingService.enabled() || !sm.billingService.paidEnabled() {
		return nil
	}

	// Verify the payment method exists and is valid
	if err := sm.billingService.verifyPaymentMethod(ctx, paymentMethodID); err != nil {
		return fmt.Errorf("invalid payment method: %w", err)
	}

	// Get account
	acct, err := sm.billingService.kbRepo.GetAccountByUserId(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to get account: %w", err)
	}

	// Set as default payment method
	_, err = sm.billingService.kbRepo.CreatePaymentMethod(ctx, acct.Payload.AccountID, &kbmodel.PaymentMethod{
		PluginName: paymentMethodPluginName,
		PluginInfo: &kbmodel.PaymentMethodPluginDetail{
			IsDefaultPaymentMethod: true,
			Properties: []*kbmodel.PluginProperty{
				{
					Key:   "mandateId",
					Value: paymentMethodID,
				},
			},
		},
		IsDefault: true,
	}, true)
	if err != nil {
		return fmt.Errorf("failed to create payment method: %w", err)
	}

	// Clean up old payment methods
	err = sm.billingService.kbRepo.RefreshPaymentMethods(ctx, acct.Payload.AccountID)
	if err != nil {
		sm.logger.Error("failed to refresh payment methods", zap.Error(err))
		// Don't fail the update for cleanup errors
	}

	sm.InvalidateCache(userID)
	return nil
}
