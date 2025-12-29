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
	"go.lumeweb.com/portal/db"
	"go.lumeweb.com/portal/event"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type BillingServiceDefault struct {
	*core.BaseComponent
	gateways *gateway.Registry
	config   *config.ServiceConfig
}

func (s *BillingServiceDefault) GetConfig() (any, error) {
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
			event.OnBootStartupFuncsCompleted(ctx, func(c core.Context, ctx context.Context) error {
				// Load service configuration
				service.config = core.GetServiceConfig[*config.ServiceConfig](c, pluginCore.BILLING_SERVICE)

				// Register Stripe gateway if webhook secret is configured
				if secret := strings.TrimSpace(service.config.Stripe.WebhookSecret); secret != "" {
					// Get quota service
					quotaSvc := core.GetService[quotaCore.QuotaService](c, quotaCore.QUOTA_SERVICE)
					if quotaSvc == nil {
						return fmt.Errorf("quota service is required for stripe gateway but not available")
					}

					// Get user service
					userSvc := core.GetService[core.UserService](c, core.USER_SERVICE)
					if userSvc == nil {
						return fmt.Errorf("user service is required for stripe gateway but not available")
					}

					if err := service.gateways.Register(ctx, stripe.New(
						service.Logger(),
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

func (s *BillingServiceDefault) GetSignatureHeader(ctx context.Context, gatewayType string) (string, error) {
	ctx, span := core.TraceMethod(ctx, "BillingServiceDefault.GetSignatureHeader")
	defer span.End()

	if s.gateways == nil {
		return "", fmt.Errorf("gateway registry not initialized")
	}
	return s.gateways.GetSignatureHeader(ctx, gatewayType)
}

func (s *BillingServiceDefault) RegisterGateway(ctx context.Context, gateway pluginCore.PaymentGateway) error {
	ctx, span := core.TraceMethod(ctx, "BillingServiceDefault.RegisterGateway")
	defer span.End()

	if s.gateways == nil {
		return fmt.Errorf("gateway registry not initialized")
	}
	return s.gateways.Register(ctx, gateway)
}

func (s *BillingServiceDefault) GetGateway(_ context.Context, gatewayType string) (pluginCore.PaymentGateway, error) {
	if s.gateways == nil {
		return nil, fmt.Errorf("gateway registry not initialized")
	}
	_gateway, exists := s.gateways.Get(gatewayType)
	if !exists {
		return nil, pluginCore.ErrGatewayNotFound
	}
	return _gateway, nil
}

func (s *BillingServiceDefault) ProcessWebhook(ctx context.Context, gatewayType string, signature string, payload []byte) error {
	ctx, span := core.TraceMethod(ctx, "BillingServiceDefault.ProcessWebhook")
	defer span.End()

	if s.gateways == nil {
		return fmt.Errorf("gateway registry not initialized")
	}

	// Extract event ID and type for logging and deduplication
	eventID, err := s.gateways.ExtractEventID(ctx, gatewayType, payload)
	if err != nil {
		return fmt.Errorf("failed to extract event ID: %w", err)
	}

	eventType, err := s.gateways.ExtractEventType(ctx, gatewayType, payload)
	if err != nil {
		return fmt.Errorf("failed to extract event type: %w", err)
	}

	return core.MetricTrack(
		WebhookDuration.WithLabelValues(gatewayType, eventType),
		WebhookProcessed.WithLabelValues(gatewayType, eventType, LabelStatusError),
		func() error {
			// Validate the webhook signature first
			if err := s.gateways.ValidateWebhook(ctx, gatewayType, signature, payload); err != nil {
				return fmt.Errorf("webhook validation failed: %w", err)
			}

			// Claim the event (idempotent insert); if not claimed, skip
			claimed, claimErr := s.claimWebhookEvent(ctx, gatewayType, eventID, eventType, payload)
			if claimErr != nil {
				return fmt.Errorf("failed to claim webhook event: %w", claimErr)
			}
			if !claimed {
				s.Logger().Debug("webhook event already processed, skipping",
					zap.String("event_id", eventID),
					zap.String("gateway_type", gatewayType),
					zap.String("event_type", eventType))
				return nil
			}

			// Handle the webhook
			if err := s.gateways.HandleWebhook(ctx, gatewayType, payload); err != nil {
				// Release the claim so that redeliveries can retry
				if releaseErr := s.releaseWebhookEventClaim(ctx, gatewayType, eventID); releaseErr != nil {
					s.Logger().Error("failed to release webhook event claim",
						zap.Error(releaseErr),
						zap.String("event_id", eventID),
						zap.String("gateway_type", gatewayType))
				}
				return fmt.Errorf("failed to handle webhook: %w", err)
			}

			// Mark the event as processed
			if err := s.markWebhookEventProcessed(ctx, gatewayType, eventID); err != nil {
				s.Logger().Error("failed to mark webhook event as processed",
					zap.Error(err),
					zap.String("event_id", eventID),
					zap.String("gateway_type", gatewayType))
			}

			return nil
		},
	)
}

// claimWebhookEvent tries to insert a row; returns false if it already exists.
func (s *BillingServiceDefault) claimWebhookEvent(ctx context.Context, gatewayType, eventID, eventType string, payload []byte) (bool, error) {
	ctx, span := core.TraceMethod(ctx, "BillingServiceDefault.claimWebhookEvent")
	defer span.End()

	var claimed bool
	err := db.RetryableComponentTransaction(s, ctx, func(tx *gorm.DB) *gorm.DB {
		evt := &models.WebhookEvent{
			GatewayType: gatewayType,
			EventID:     eventID,
			EventType:   eventType,
			// Optional: store payload for observability
			Payload: payload,
			// Leave ProcessedAt zero; mark on success.
		}
		res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(evt)
		claimed = res.RowsAffected == 1
		return res
	})
	return claimed, err
}

func (s *BillingServiceDefault) markWebhookEventProcessed(ctx context.Context, gatewayType, eventID string) error {
	ctx, span := core.TraceMethod(ctx, "BillingServiceDefault.markWebhookEventProcessed")
	defer span.End()

	return db.RetryableComponentTransaction(s, ctx, func(tx *gorm.DB) *gorm.DB {
		return tx.Model(&models.WebhookEvent{}).
			Where("gateway_type = ? AND event_id = ?", gatewayType, eventID).
			Updates(map[string]any{"processed_at": time.Now().UTC()})
	})
}

func (s *BillingServiceDefault) releaseWebhookEventClaim(ctx context.Context, gatewayType, eventID string) error {
	ctx, span := core.TraceMethod(ctx, "BillingServiceDefault.releaseWebhookEventClaim")
	defer span.End()

	return db.RetryableComponentTransaction(s, ctx, func(tx *gorm.DB) *gorm.DB {
		return tx.Model(&models.WebhookEvent{}).
			Where("gateway_type = ? AND event_id = ?", gatewayType, eventID).
			Update("deleted_at", time.Now().UTC())
	})
}

// CreateOrUpdateSubscriber creates or updates a subscriber record
func (s *BillingServiceDefault) CreateOrUpdateSubscriber(ctx context.Context, userID uint, gatewayType, gatewayID string, isActive bool, planID *uint) error {
	ctx, span := core.TraceMethod(ctx, "BillingServiceDefault.CreateOrUpdateSubscriber")
	defer span.End()

	return core.MetricTrack(
		nil,
		SubscriberCreated.WithLabelValues(gatewayType, LabelStatusError),
		func() error {
			return db.RetryableComponentTransaction(s, ctx, func(tx *gorm.DB) *gorm.DB {
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
					return result
				}

				// If we updated a row, we're done
				if result.RowsAffected > 0 {
					return result
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
					return result
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

					return result
				}

				// For any other error, return it
				return result
			})
		},
	)
}

// DeactivateSubscriber deactivates a subscriber
func (s *BillingServiceDefault) DeactivateSubscriber(ctx context.Context, userID uint, gatewayType string) error {
	ctx, span := core.TraceMethod(ctx, "BillingServiceDefault.DeactivateSubscriber")
	defer span.End()

	return core.MetricTrack(
		nil,
		SubscriberDeactivated.WithLabelValues(gatewayType, LabelStatusError),
		func() error {
			return db.RetryableComponentTransaction(s, ctx, func(tx *gorm.DB) *gorm.DB {
				return tx.Model(&models.Subscriber{}).
					Where("user_id = ? AND gateway_type = ?", userID, gatewayType).
					Updates(map[string]any{"is_active": false, "plan_id": nil})
			})
		},
	)
}

// GetActiveSubscriber returns an active subscriber for the given user and gateway
func (s *BillingServiceDefault) GetActiveSubscriber(ctx context.Context, userID uint, gatewayType string) (*pluginCore.Subscriber, error) {
	ctx, span := core.TraceMethod(ctx, "BillingServiceDefault.GetActiveSubscriber")
	defer span.End()

	var subscriber models.Subscriber
	err := db.RetryableComponentTransaction(s, ctx, func(tx *gorm.DB) *gorm.DB {
		return tx.Where("user_id = ? AND gateway_type = ? AND is_active = ?", userID, gatewayType, true).
			First(&subscriber)
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &subscriber, nil
}

// GetSubscriberByGatewayID returns a subscriber by gateway ID and gateway type
func (s *BillingServiceDefault) GetSubscriberByGatewayID(ctx context.Context, gatewayID, gatewayType string) (*pluginCore.Subscriber, error) {
	ctx, span := core.TraceMethod(ctx, "BillingServiceDefault.GetSubscriberByGatewayID")
	defer span.End()

	var subscriber models.Subscriber
	err := db.RetryableComponentTransaction(s, ctx, func(tx *gorm.DB) *gorm.DB {
		return tx.Where("gateway_id = ? AND gateway_type = ?", gatewayID, gatewayType).
			Order("updated_at DESC").
			First(&subscriber)
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &subscriber, nil
}

// IsUserActiveSubscriber checks if a user has an active subscription with any gateway
func (s *BillingServiceDefault) IsUserActiveSubscriber(ctx context.Context, userID uint) (bool, error) {
	ctx, span := core.TraceMethod(ctx, "BillingServiceDefault.IsUserActiveSubscriber")
	defer span.End()

	var count int64
	err := db.RetryableComponentTransaction(s, ctx, func(tx *gorm.DB) *gorm.DB {
		return tx.Model(&models.Subscriber{}).
			Where("user_id = ? AND is_active = ?", userID, true).
			Count(&count)
	})
	return count > 0, err
}

// GetActiveSubscribersByGateway returns all active subscribers for a specific gateway
func (s *BillingServiceDefault) GetActiveSubscribersByGateway(ctx context.Context, gatewayType string) ([]pluginCore.Subscriber, error) {
	ctx, span := core.TraceMethod(ctx, "BillingServiceDefault.GetActiveSubscribersByGateway")
	defer span.End()

	var subscribers []models.Subscriber
	err := db.RetryableComponentTransaction(s, ctx, func(tx *gorm.DB) *gorm.DB {
		return tx.Where("gateway_type = ? AND is_active = ?", gatewayType, true).
			Find(&subscribers)
	})
	if err != nil {
		return nil, err
	}

	// Convert to re-exported type using lo.Map
	result := lo.Map(subscribers, func(sub models.Subscriber, _ int) pluginCore.Subscriber {
		return sub
	})
	return result, nil
}

// GetActiveSubscription returns the first active subscription for a user across all gateways
func (s *BillingServiceDefault) GetActiveSubscription(ctx context.Context, userID uint) (*pluginCore.Subscriber, error) {
	ctx, span := core.TraceMethod(ctx, "BillingServiceDefault.GetActiveSubscription")
	defer span.End()

	var subscriber models.Subscriber
	err := db.RetryableComponentTransaction(s, ctx, func(tx *gorm.DB) *gorm.DB {
		return tx.Where("user_id = ? AND is_active = ?", userID, true).
			First(&subscriber)
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &subscriber, nil
}
