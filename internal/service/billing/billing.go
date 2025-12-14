package billing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/samber/lo"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/internal/config"
	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/portal-plugin-billing/internal/gateway"
	"go.lumeweb.com/portal-plugin-billing/internal/gateway/stripe"
	quotaCore "go.lumeweb.com/portal-plugin-quota/core"
	"go.lumeweb.com/portal/core"
	"go.lumeweb.com/portal/event"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type BillingServiceDefault struct {
	db       *gorm.DB
	ctx      core.Context
	logger   *core.Logger
	gateways *gateway.Registry
	config   *config.ServiceConfig
}

func (s *BillingServiceDefault) Config() (any, error) {
	return &config.ServiceConfig{}, nil
}

var _ pluginCore.BillingService = (*BillingServiceDefault)(nil)

// NewBillingService creates a new billing service with default registry
func NewBillingService() (core.Service, []core.ContextBuilderOption, error) {
	return NewBillingServiceWithRegistry(gateway.GetRegistry())
}

// NewBillingServiceWithRegistry creates a new billing service with custom registry
// Useful for testing
func NewBillingServiceWithRegistry(registry *gateway.Registry) (core.Service, []core.ContextBuilderOption, error) {
	if registry == nil {
		return nil, nil, fmt.Errorf("gateway registry is nil")
	}
	service := &BillingServiceDefault{
		gateways: registry,
	}

	return service, core.ContextOptions(
		core.ContextWithStartupFunc(func(ctx core.Context) error {
			service.ctx = ctx
			service.logger = ctx.ServiceLogger(service)
			service.db = ctx.DB()

			event.OnBootStartupFuncsCompleted(ctx, func(ctx core.Context) error {
				// Load service configuration
				service.config = core.GetServiceConfig[*config.ServiceConfig](ctx, pluginCore.BILLING_SERVICE)

				// Register Stripe gateway if webhook secret is configured
				if secret := strings.TrimSpace(service.config.Stripe.WebhookSecret); secret != "" {
					// Get quota service
					quotaSvc := core.GetService[quotaCore.QuotaService](ctx, quotaCore.QUOTA_SERVICE)
					if quotaSvc == nil {
						return fmt.Errorf("quota service is required for stripe gateway but not available")
					}

					// Get user service
					userSvc := core.GetService[core.UserService](ctx, core.USER_SERVICE)
					if userSvc == nil {
						return fmt.Errorf("user service is required for stripe gateway but not available")
					}

					if err := service.gateways.Register(stripe.New(
						service.logger,
						secret,
						service.config.Stripe.SecretKey,
						quotaSvc,
						userSvc,
						service,
					)); err != nil {
						return fmt.Errorf("failed to register stripe gateway: %w", err)
					}
				}

				return nil
			})

			return nil
		}),
	), nil
}

func (s *BillingServiceDefault) ID() string {
	return pluginCore.BILLING_SERVICE
}

func (s *BillingServiceDefault) GetSignatureHeader(gatewayType string) (string, error) {
	if s.gateways == nil {
		return "", fmt.Errorf("gateway registry not initialized")
	}
	return s.gateways.GetSignatureHeader(gatewayType)
}

func (s *BillingServiceDefault) RegisterGateway(gateway pluginCore.PaymentGateway) error {
	if s.gateways == nil {
		return fmt.Errorf("gateway registry not initialized")
	}
	return s.gateways.Register(gateway)
}

func (s *BillingServiceDefault) GetGateway(gatewayType string) (pluginCore.PaymentGateway, error) {
	if s.gateways == nil {
		return nil, fmt.Errorf("gateway registry not initialized")
	}
	gateway, exists := s.gateways.Get(gatewayType)
	if !exists {
		return nil, pluginCore.ErrGatewayNotFound
	}
	return gateway, nil
}

func (s *BillingServiceDefault) ProcessWebhook(ctx context.Context, gatewayType string, signature string, payload []byte) error {
	if s.gateways == nil {
		return fmt.Errorf("gateway registry not initialized")
	}

	// Validate the webhook signature first
	if err := s.gateways.ValidateWebhook(ctx, gatewayType, signature, payload); err != nil {
		return fmt.Errorf("webhook validation failed: %w", err)
	}

	// Extract event ID and type for logging and deduplication
	eventID, err := s.gateways.ExtractEventID(gatewayType, payload)
	if err != nil {
		return fmt.Errorf("failed to extract event ID: %w", err)
	}

	eventType, err := s.gateways.ExtractEventType(gatewayType, payload)
	if err != nil {
		return fmt.Errorf("failed to extract event type: %w", err)
	}

	// Claim the event (idempotent insert); if not claimed, skip
	claimed, claimErr := s.claimWebhookEvent(gatewayType, eventID, eventType, payload)
	if claimErr != nil {
		return fmt.Errorf("failed to claim webhook event: %w", claimErr)
	}
	if !claimed {
		s.logger.Debug("webhook event already processed, skipping",
			zap.String("event_id", eventID),
			zap.String("gateway_type", gatewayType),
			zap.String("event_type", eventType))
		return nil
	}

	// Handle the webhook
	if err := s.gateways.HandleWebhook(ctx, gatewayType, payload); err != nil {
		// Release the claim so that redeliveries can retry
		if releaseErr := s.releaseWebhookEventClaim(gatewayType, eventID); releaseErr != nil {
			s.logger.Error("failed to release webhook event claim",
				zap.Error(releaseErr),
				zap.String("event_id", eventID),
				zap.String("gateway_type", gatewayType))
		}
		return fmt.Errorf("failed to handle webhook: %w", err)
	}

	// Mark the event as processed
	if err := s.markWebhookEventProcessed(gatewayType, eventID); err != nil {
		s.logger.Error("failed to mark webhook event as processed",
			zap.Error(err),
			zap.String("event_id", eventID),
			zap.String("gateway_type", gatewayType))
	}

	return nil
}

// claimWebhookEvent tries to insert a row; returns false if it already exists.
func (s *BillingServiceDefault) claimWebhookEvent(gatewayType, eventID, eventType string, payload []byte) (bool, error) {
	evt := &models.WebhookEvent{
		GatewayType: gatewayType,
		EventID:     eventID,
		EventType:   eventType,
		// Optional: store payload for observability
		Payload: payload,
		// Leave ProcessedAt zero; mark on success.
	}
	res := s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(evt)
	if res.Error != nil {
		return false, res.Error
	}
	// RowsAffected == 0 => duplicate (already claimed/processed)
	return res.RowsAffected == 1, nil
}

func (s *BillingServiceDefault) markWebhookEventProcessed(gatewayType, eventID string) error {
	return s.db.Model(&models.WebhookEvent{}).
		Where("gateway_type = ? AND event_id = ?", gatewayType, eventID).
		Updates(map[string]any{"processed_at": time.Now().UTC()}).Error
}

func (s *BillingServiceDefault) releaseWebhookEventClaim(gatewayType, eventID string) error {
	return s.db.Model(&models.WebhookEvent{}).
		Where("gateway_type = ? AND event_id = ?", gatewayType, eventID).
		Update("deleted_at", time.Now().UTC()).Error
}

// CreateOrUpdateSubscriber creates or updates a subscriber record
func (s *BillingServiceDefault) CreateOrUpdateSubscriber(userID uint, gatewayType, gatewayID string, isActive bool, planID *uint) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		// First try to update existing record (including soft-deleted ones)
		result := tx.Unscoped().Model(&models.Subscriber{}).
			Where("user_id = ? AND gateway_type = ?", userID, gatewayType).
			Updates(map[string]any{
				"gateway_id": gatewayID,
				"is_active":  isActive,
				"plan_id":    planID,
				"deleted_at": nil,
			})

		if result.Error != nil {
			return result.Error
		}

		// If we updated a row, we're done
		if result.RowsAffected > 0 {
			return nil
		}

		// No existing row found, try to insert
		sub := models.Subscriber{
			UserID:      userID,
			GatewayType: gatewayType,
			GatewayID:   gatewayID,
			IsActive:    isActive,
			PlanID:      planID,
		}

		result = tx.Create(&sub)
		if result.Error == nil {
			return nil
		}

		// If insert failed due to unique constraint violation, retry update
		// This handles the race condition where another goroutine inserted between our update and create
		if strings.Contains(result.Error.Error(), "UNIQUE constraint failed") {
			result = tx.Unscoped().Model(&models.Subscriber{}).
				Where("user_id = ? AND gateway_type = ?", userID, gatewayType).
				Updates(map[string]any{
					"gateway_id": gatewayID,
					"is_active":  isActive,
					"plan_id":    planID,
					"deleted_at": nil,
				})

			return result.Error
		}

		// For any other error, return it
		return result.Error
	})
}

// DeactivateSubscriber deactivates a subscriber
func (s *BillingServiceDefault) DeactivateSubscriber(userID uint, gatewayType string) error {
	return s.db.Model(&models.Subscriber{}).
		Where("user_id = ? AND gateway_type = ?", userID, gatewayType).
		Updates(map[string]any{"is_active": false, "plan_id": nil}).Error
}

// GetActiveSubscriber returns an active subscriber for the given user and gateway
func (s *BillingServiceDefault) GetActiveSubscriber(userID uint, gatewayType string) (*pluginCore.Subscriber, error) {
	var subscriber models.Subscriber
	err := s.db.Where("user_id = ? AND gateway_type = ? AND is_active = ?", userID, gatewayType, true).
		First(&subscriber).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return (*pluginCore.Subscriber)(&subscriber), nil
}

// GetSubscriberByGatewayID returns a subscriber by gateway ID and gateway type
func (s *BillingServiceDefault) GetSubscriberByGatewayID(gatewayID, gatewayType string) (*pluginCore.Subscriber, error) {
	var subscriber models.Subscriber
	err := s.db.Where("gateway_id = ? AND gateway_type = ?", gatewayID, gatewayType).
		Order("updated_at DESC").
		First(&subscriber).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return (*pluginCore.Subscriber)(&subscriber), nil
}

// IsUserActiveSubscriber checks if a user has an active subscription with any gateway
func (s *BillingServiceDefault) IsUserActiveSubscriber(userID uint) (bool, error) {
	var count int64
	err := s.db.Model(&models.Subscriber{}).
		Where("user_id = ? AND is_active = ?", userID, true).
		Count(&count).Error
	return count > 0, err
}

// GetActiveSubscribersByGateway returns all active subscribers for a specific gateway
func (s *BillingServiceDefault) GetActiveSubscribersByGateway(gatewayType string) ([]pluginCore.Subscriber, error) {
	var subscribers []models.Subscriber
	err := s.db.Where("gateway_type = ? AND is_active = ?", gatewayType, true).
		Find(&subscribers).Error
	if err != nil {
		return nil, err
	}

	// Convert to re-exported type using lo.Map
	result := lo.Map(subscribers, func(sub models.Subscriber, _ int) pluginCore.Subscriber {
		return pluginCore.Subscriber(sub)
	})
	return result, nil
}

// GetActiveSubscription returns the first active subscription for a user across all gateways
func (s *BillingServiceDefault) GetActiveSubscription(userID uint) (*pluginCore.Subscriber, error) {
	var subscriber models.Subscriber
	err := s.db.Where("user_id = ? AND is_active = ?", userID, true).
		First(&subscriber).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return (*pluginCore.Subscriber)(&subscriber), nil
}
