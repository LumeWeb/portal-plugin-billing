package billing

import (
	"context"
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

	// Check if event was already processed
	if s.isWebhookEventProcessed(eventID) {
		s.logger.Debug("webhook event already processed, skipping",
			zap.String("event_id", eventID),
			zap.String("gateway_type", gatewayType),
			zap.String("event_type", eventType))
		return nil
	}

	// Handle the webhook
	if err := s.gateways.HandleWebhook(ctx, gatewayType, payload); err != nil {
		return fmt.Errorf("failed to handle webhook: %w", err)
	}

	// Log the processed webhook event
	if err := s.logWebhookEvent(gatewayType, eventID, eventType, payload); err != nil {
		s.logger.Error("failed to log webhook event",
			zap.Error(err),
			zap.String("event_id", eventID),
			zap.String("gateway_type", gatewayType))
	}

	return nil
}

// isWebhookEventProcessed checks if a webhook event has already been processed
func (s *BillingServiceDefault) isWebhookEventProcessed(eventID string) bool {
	var count int64
	s.db.Model(&models.WebhookEvent{}).Where("event_id = ?", eventID).Count(&count)
	return count > 0
}

// logWebhookEvent logs webhook events to prevent duplicate processing
func (s *BillingServiceDefault) logWebhookEvent(gatewayType, eventID, eventType string, payload []byte) error {
	event := &models.WebhookEvent{
		GatewayType: gatewayType,
		EventID:     eventID,
		EventType:   eventType,
		ProcessedAt: time.Now(),
		Payload:     payload,
	}

	if err := s.db.Create(event).Error; err != nil {
		return fmt.Errorf("failed to log webhook event: %w", err)
	}

	s.logger.Debug("webhook event processed",
		zap.String("event_id", eventID),
		zap.String("gateway_type", gatewayType),
		zap.String("event_type", eventType))

	return nil
}
