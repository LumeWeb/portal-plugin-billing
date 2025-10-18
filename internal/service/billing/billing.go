package billing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/internal/config"
	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/portal-plugin-billing/internal/gateway"
	"go.lumeweb.com/portal-plugin-billing/internal/gateway/stripe"
	quotaCore "go.lumeweb.com/portal-plugin-quota/core"
	"go.lumeweb.com/portal/core"
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
					quotaSvc,
					userSvc,
					service,
				)); err != nil {
					return fmt.Errorf("failed to register stripe gateway: %w", err)
				}
			}

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
	// First try to find existing subscriber (including soft deleted)
	var existing models.Subscriber
	err := s.db.Unscoped().Where("user_id = ? AND gateway_type = ?", userID, gatewayType).First(&existing).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Create new subscriber
			subscriber := &models.Subscriber{
				UserID:      userID,
				GatewayType: gatewayType,
				GatewayID:   gatewayID,
				IsActive:    isActive,
				PlanID:      planID,
			}
			return s.db.Create(subscriber).Error
		}
		return err
	}

	// Update existing subscriber (restore if soft deleted)
	existing.GatewayID = gatewayID
	existing.IsActive = isActive
	existing.PlanID = planID
	existing.DeletedAt = gorm.DeletedAt{} // Restore if soft deleted

	return s.db.Save(&existing).Error
}

// DeactivateSubscriber deactivates a subscriber
func (s *BillingServiceDefault) DeactivateSubscriber(userID uint, gatewayType string) error {
	return s.db.Model(&models.Subscriber{}).
		Where("user_id = ? AND gateway_type = ?", userID, gatewayType).
		Updates(map[string]any{"is_active": false, "plan_id": nil}).Error
}

// GetActiveSubscriber returns an active subscriber for the given user and gateway
func (s *BillingServiceDefault) GetActiveSubscriber(userID uint, gatewayType string) (*models.Subscriber, error) {
	var subscriber models.Subscriber
	err := s.db.Where("user_id = ? AND gateway_type = ? AND is_active = ?", userID, gatewayType, true).
		First(&subscriber).Error
	if err != nil {
		return nil, err
	}
	return &subscriber, nil
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
func (s *BillingServiceDefault) GetActiveSubscribersByGateway(gatewayType string) ([]models.Subscriber, error) {
	var subscribers []models.Subscriber
	err := s.db.Where("gateway_type = ? AND is_active = ?", gatewayType, true).
		Find(&subscribers).Error
	return subscribers, err
}

// GetActiveSubscription returns the first active subscription for a user across all gateways
func (s *BillingServiceDefault) GetActiveSubscription(userID uint) (*models.Subscriber, error) {
	var subscriber models.Subscriber
	err := s.db.Where("user_id = ? AND is_active = ?", userID, true).
		First(&subscriber).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &subscriber, nil
}
