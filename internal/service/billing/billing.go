package billing

import (
	"context"
	"fmt"

	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/internal/config"
	"go.lumeweb.com/portal-plugin-billing/internal/gateway"
	"go.lumeweb.com/portal-plugin-billing/internal/gateway/stripe"
	quotaCore "go.lumeweb.com/portal-plugin-quota/core"
	"go.lumeweb.com/portal/core"
)

type BillingServiceDefault struct {
	ctx      core.Context
	logger   *core.Logger
	gateways *gateway.Registry
	config   *config.ServiceConfig
	quota    quotaCore.QuotaService
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
	service := &BillingServiceDefault{
		gateways: registry,
	}

	return service, core.ContextOptions(
		core.ContextWithStartupFunc(func(ctx core.Context) error {
			service.ctx = ctx
			service.logger = ctx.ServiceLogger(service)

			// Load service configuration
			service.config = core.GetServiceConfig[*config.ServiceConfig](ctx, pluginCore.BILLING_SERVICE)

			// Get quota service early
			service.quota = core.GetService[quotaCore.QuotaService](ctx, quotaCore.QUOTA_SERVICE)
			if service.quota == nil {
				return fmt.Errorf("quota service is required but not available")
			}

			// Register Stripe gateway if webhook secret is configured
			if service.config.Stripe.WebhookSecret != "" {
				if err := service.gateways.Register(stripe.New(
					ctx.Logger(),
					service.config.Stripe.WebhookSecret,
					service.quota,
					core.GetService[core.UserService](ctx, core.USER_SERVICE),
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
	gw, exists := s.gateways.Get(gatewayType)
	if !exists {
		return "", fmt.Errorf("%w: %s", pluginCore.ErrGatewayNotFound, gatewayType)
	}
	return gw.SignatureHeader(), nil
}

func (s *BillingServiceDefault) ProcessWebhook(ctx context.Context, gatewayType string, signature string, payload []byte) error {
	if s.gateways == nil {
		return fmt.Errorf("gateway registry not initialized")
	}
	// Get the gateway by type
	gw, exists := s.gateways.Get(gatewayType)
	if !exists {
		return fmt.Errorf("%w: %s", pluginCore.ErrGatewayNotFound, gatewayType)
	}

	// Validate the webhook signature
	if err := gw.ValidateWebhook(ctx, signature, payload); err != nil {
		return fmt.Errorf("webhook validation failed: %w", err)
	}

	// Handle the webhook
	if err := gw.HandleWebhook(ctx, payload); err != nil {
		return fmt.Errorf("failed to handle webhook: %w", err)
	}

	return nil
}
