package billing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/samber/lo"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/internal"
	"go.lumeweb.com/portal-plugin-billing/internal/config"
	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/portal-plugin-billing/internal/gateway"
	"go.lumeweb.com/portal-plugin-billing/internal/gateway/atlos"
	"go.lumeweb.com/portal-plugin-billing/internal/gateway/stripe"
	quotaCore "go.lumeweb.com/portal-plugin-quota/core"
	"go.lumeweb.com/portal/core"
	"go.lumeweb.com/portal/db"
	"go.lumeweb.com/portal/event"
	"go.lumeweb.com/queryutil"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type BillingServiceDefault struct {
	*core.BaseComponent
	gateways       *gateway.Registry
	config         *config.ServiceConfig
	pricingService pluginCore.PricingService
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
				return service.registerGateways(c, ctx)
			})

			return nil
		}),
	), nil
}

// gatewaySetupFunc is a function that sets up a gateway
type gatewaySetupFunc func() (string, pluginCore.GatewayIdentity, error)

// gatewaySetup holds the setup configuration for a gateway
type gatewaySetup struct {
	name string
	fn   gatewaySetupFunc
}

// registerGateways registers all configured payment gateways
func (s *BillingServiceDefault) registerGateways(c core.Context, ctx context.Context) error {
	s.config = core.GetServiceConfig[*config.ServiceConfig](c, pluginCore.BILLING_SERVICE)

	s.pricingService = core.GetService[pluginCore.PricingService](c, pluginCore.PRICING_SERVICE)
	if s.pricingService == nil {
		return fmt.Errorf("pricing service is required but not available")
	}

	opts := pluginCore.GatewaySetupOptions{
		Logger:     s.Logger(),
		Ctx:        c,
		Context:    ctx,
		BillingSvc: s,
		PricingSvc: s.pricingService,
		HTTP:       core.GetService[core.HTTPService](c, core.HTTP_SERVICE),
		Quota:      core.GetService[quotaCore.QuotaService](c, quotaCore.QUOTA_SERVICE),
		User:       core.GetService[core.UserService](c, core.USER_SERVICE),
		CreditSvc:  core.GetService[pluginCore.CreditService](c, pluginCore.CREDIT_SERVICE),
	}

	return s.setupGateways(ctx, opts)
}

func (s *BillingServiceDefault) setupGateways(ctx context.Context, opts pluginCore.GatewaySetupOptions) error {
	setups := s.getGatewaySetups(opts)

	for _, setup := range setups {
		msg, gw, err := setup.fn()
		if err != nil {
			return fmt.Errorf("failed to setup %s gateway: %w", setup.name, err)
		}
		if gw != nil {
			if err := s.gateways.Register(ctx, gw); err != nil {
				return fmt.Errorf("failed to register %s gateway: %w", setup.name, err)
			}

			// If the gateway implements MetricsProvider, register its metrics
			// with the plugin's prometheus registry automatically.
			if metricsProvider, ok := gw.(pluginCore.MetricsProvider); ok {
				if err := core.RegisterPluginMetrics(internal.PLUGIN_NAME, metricsProvider.Metrics()); err != nil {
					opts.Logger.Error("Failed to register gateway metrics",
						zap.String("gateway", setup.name),
						zap.Error(err))
				}
			}
		}
		if msg != "" {
			s.Logger().Info(msg)
		}
	}

	return nil
}

func (s *BillingServiceDefault) getGatewaySetups(opts pluginCore.GatewaySetupOptions) []gatewaySetup {
	return []gatewaySetup{
		{
			name: "stripe",
			fn: func() (string, pluginCore.GatewayIdentity, error) {
				return stripe.Setup(opts, s.config)
			},
		},
		{
			name: "atlos",
			fn: func() (string, pluginCore.GatewayIdentity, error) {
				return atlos.Setup(opts, s.config.Atlos)
			},
		},
	}
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

func (s *BillingServiceDefault) GetGateway(_ context.Context, gatewayType string) (pluginCore.GatewayIdentity, error) {
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
			if err := s.gateways.ValidateWebhook(ctx, gatewayType, signature, payload); err != nil {
				return fmt.Errorf("webhook validation failed: %w", err)
			}

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

			if err := s.gateways.HandleWebhook(ctx, gatewayType, payload); err != nil {
				if releaseErr := s.releaseWebhookEventClaim(ctx, gatewayType, eventID); releaseErr != nil {
					s.Logger().Error("failed to release webhook event claim",
						zap.Error(releaseErr),
						zap.String("event_id", eventID),
						zap.String("gateway_type", gatewayType))
				}
				return fmt.Errorf("failed to handle webhook: %w", err)
			}

			if err := s.markWebhookEventProcessed(ctx, gatewayType, eventID); err != nil {
				return fmt.Errorf("failed to mark webhook event as processed: %w", err)
			}

			return nil
		},
	)
}

func (s *BillingServiceDefault) claimWebhookEvent(ctx context.Context, gatewayType, eventID, eventType string, payload []byte) (bool, error) {
	ctx, span := core.TraceMethod(ctx, "BillingServiceDefault.claimWebhookEvent")
	defer span.End()

	var claimed bool
	err := db.RetryableComponentTransaction(s, ctx, func(tx *gorm.DB) *gorm.DB {
		evt := &models.WebhookEvent{
			GatewayType: gatewayType,
			EventID:     eventID,
			EventType:   eventType,
			Payload:     payload,
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

func (s *BillingServiceDefault) CreateOrUpdateSubscriber(ctx context.Context, userID uint, gatewayType, externalID, subscriptionID string, isActive bool, pricingPlanPeriodID *uint, opts ...pluginCore.SubscriberOption) error {
	ctx, span := core.TraceMethod(ctx, "BillingServiceDefault.CreateOrUpdateSubscriber")
	defer span.End()

	// Apply subscriber options
	subOptions := pluginCore.ApplySubscriberOptions(opts...)

	return core.MetricTrack(
		nil,
		SubscriberCreated.WithLabelValues(gatewayType, LabelStatusError),
		func() error {
			// Validate pricing plan period if provided
			if pricingPlanPeriodID != nil {
				period, err := s.pricingService.GetPricingPlanPeriod(ctx, *pricingPlanPeriodID)
				if err != nil {
					return fmt.Errorf("failed to validate pricing plan period: %w", err)
				}
				if period == nil {
					return fmt.Errorf("pricing plan period with ID %d not found", *pricingPlanPeriodID)
				}
			}

			// Prepare updates map with option fields if provided
			updates := map[string]any{
				"external_id":            externalID,
				"subscription_id":        subscriptionID,
				"is_active":              isActive,
				"pricing_plan_period_id": pricingPlanPeriodID,
				"deleted_at":             nil,
			}
			if subOptions.BillingPeriodStart != nil {
				updates["billing_period_start"] = *subOptions.BillingPeriodStart
			}
			if subOptions.BillingPeriodEnd != nil {
				updates["billing_period_end"] = *subOptions.BillingPeriodEnd
			}
			if subOptions.ClearWillCancelAt {
				updates["will_cancel_at"] = nil
			} else if subOptions.WillCancelAt != nil {
				updates["will_cancel_at"] = *subOptions.WillCancelAt
			}

			return db.RetryableComponentTransaction(s, ctx, func(tx *gorm.DB) *gorm.DB {
				result := tx.Unscoped().Model(&models.Subscriber{}).
					Where("user_id = ? AND gateway_type = ?", userID, gatewayType).
					Updates(updates)

				if result.Error != nil {
					return result
				}

				if result.RowsAffected > 0 {
					return result
				}

				sub := models.Subscriber{
					UserID:              userID,
					GatewayType:         gatewayType,
					ExternalID:          externalID,
					SubscriptionID:      subscriptionID,
					IsActive:            isActive,
					PricingPlanPeriodID: pricingPlanPeriodID,
					BillingPeriodStart:  subOptions.BillingPeriodStart,
					BillingPeriodEnd:    subOptions.BillingPeriodEnd,
				}
				// Only set WillCancelAt if not clearing it
				if !subOptions.ClearWillCancelAt && subOptions.WillCancelAt != nil {
					sub.WillCancelAt = subOptions.WillCancelAt
				}

				result = tx.Create(&sub)
				if result.Error == nil {
					return result
				}

				if strings.Contains(result.Error.Error(), "UNIQUE constraint failed") {
					result = tx.Unscoped().Model(&models.Subscriber{}).
						Where("user_id = ? AND gateway_type = ?", userID, gatewayType).
						Updates(updates)

					return result
				}

				return result
			})
		},
	)
}

func (s *BillingServiceDefault) DeactivateSubscriber(ctx context.Context, userID uint, gatewayType string) error {
	ctx, span := core.TraceMethod(ctx, "BillingServiceDefault.DeactivateSubscriber")
	defer span.End()

	return core.MetricTrack(
		nil,
		SubscriberDeactivated.WithLabelValues(gatewayType, LabelStatusError),
		func() error {
			return db.RetryableComponentTransaction(s, ctx, func(tx *gorm.DB) *gorm.DB {
				// Get the current subscriber before deactivating to save to history
				var subscriber models.Subscriber
				err := tx.Where("user_id = ? AND gateway_type = ?", userID, gatewayType).
					Preload("PricingPlanPeriod").
					First(&subscriber).Error
				if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
					tx.Error = err
					return tx
				}

				// If subscriber exists with valid data, create history record
				if err == nil && subscriber.PricingPlanPeriodID != nil && subscriber.PricingPlanPeriod != nil {
					history := models.SubscriptionHistory{
						UserID:              subscriber.UserID,
						PricingPlanID:       subscriber.PricingPlanPeriod.PricingPlanID,
						PricingPlanPeriodID: *subscriber.PricingPlanPeriodID,
						PaymentGatewayType:  subscriber.GatewayType,
						BillingPeriodStart:  subscriber.BillingPeriodStart,
						BillingPeriodEnd:    subscriber.BillingPeriodEnd,
						StartedAt:           subscriber.CreatedAt,
						EndedAt:             time.Now().UTC(),
					}
					if err := tx.Create(&history).Error; err != nil {
						tx.Error = err
						return tx
					}
				}

				// Deactivate the subscriber
				return tx.Model(&models.Subscriber{}).
					Where("user_id = ? AND gateway_type = ?", userID, gatewayType).
					Updates(map[string]any{"is_active": false, "pricing_plan_period_id": nil, "paused_at": nil})
			})
		},
	)
}

func (s *BillingServiceDefault) PauseSubscriber(ctx context.Context, userID uint, gatewayType string) error {
	ctx, span := core.TraceMethod(ctx, "BillingServiceDefault.PauseSubscriber")
	defer span.End()

	return core.MetricTrack(
		nil,
		SubscriberDeactivated.WithLabelValues(gatewayType, LabelStatusError),
		func() error {
			now := time.Now().UTC()
			return db.RetryableComponentTransaction(s, ctx, func(tx *gorm.DB) *gorm.DB {
				return tx.Model(&models.Subscriber{}).
					Where("user_id = ? AND gateway_type = ?", userID, gatewayType).
					Updates(map[string]any{"is_active": false, "paused_at": now})
			})
		},
	)
}

func (s *BillingServiceDefault) ResumeSubscriber(ctx context.Context, userID uint, gatewayType string) error {
	ctx, span := core.TraceMethod(ctx, "BillingServiceDefault.ResumeSubscriber")
	defer span.End()

	return core.MetricTrack(
		nil,
		SubscriberCreated.WithLabelValues(gatewayType, LabelStatusError),
		func() error {
			return db.RetryableComponentTransaction(s, ctx, func(tx *gorm.DB) *gorm.DB {
				return tx.Model(&models.Subscriber{}).
					Where("user_id = ? AND gateway_type = ?", userID, gatewayType).
					Updates(map[string]any{"is_active": true, "paused_at": nil})
			})
		},
	)
}

func (s *BillingServiceDefault) GetActiveSubscriber(ctx context.Context, userID uint, gatewayType string) (*pluginCore.Subscriber, error) {
	ctx, span := core.TraceMethod(ctx, "BillingServiceDefault.GetActiveSubscriber")
	defer span.End()

	var subscriber models.Subscriber
	err := db.RetryableComponentTransaction(s, ctx, func(tx *gorm.DB) *gorm.DB {
		return tx.Preload("PricingPlanPeriod").Where("user_id = ? AND gateway_type = ? AND is_active = ?", userID, gatewayType, true).
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

func (s *BillingServiceDefault) GetSubscriberByExternalID(ctx context.Context, externalID, gatewayType string) (*pluginCore.Subscriber, error) {
	ctx, span := core.TraceMethod(ctx, "BillingServiceDefault.GetSubscriberByExternalID")
	defer span.End()

	var subscriber models.Subscriber
	err := db.RetryableComponentTransaction(s, ctx, func(tx *gorm.DB) *gorm.DB {
		return tx.Where("external_id = ? AND gateway_type = ?", externalID, gatewayType).
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

func (s *BillingServiceDefault) GetSubscriberBySubscriptionID(ctx context.Context, subscriptionID, gatewayType string) (*pluginCore.Subscriber, error) {
	ctx, span := core.TraceMethod(ctx, "BillingServiceDefault.GetSubscriberBySubscriptionID")
	defer span.End()

	var subscriber models.Subscriber
	err := db.RetryableComponentTransaction(s, ctx, func(tx *gorm.DB) *gorm.DB {
		return tx.Where("subscription_id = ? AND gateway_type = ?", subscriptionID, gatewayType).
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

func (s *BillingServiceDefault) GetActiveSubscribersByGateway(ctx context.Context, gatewayType string) ([]pluginCore.Subscriber, error) {
	ctx, span := core.TraceMethod(ctx, "BillingServiceDefault.GetActiveSubscribersByGateway")
	defer span.End()

	var subscribers []models.Subscriber
	err := db.RetryableComponentTransaction(s, ctx, func(tx *gorm.DB) *gorm.DB {
		return tx.Preload("PricingPlanPeriod").Where("gateway_type = ? AND is_active = ?", gatewayType, true).
			Find(&subscribers)
	})
	if err != nil {
		return nil, err
	}

	result := lo.Map(subscribers, func(sub models.Subscriber, _ int) pluginCore.Subscriber {
		return sub
	})
	return result, nil
}

func (s *BillingServiceDefault) GetActiveSubscription(ctx context.Context, userID uint) (*pluginCore.Subscriber, error) {
	ctx, span := core.TraceMethod(ctx, "BillingServiceDefault.GetActiveSubscription")
	defer span.End()

	var subscriber models.Subscriber
	err := db.RetryableComponentTransaction(s, ctx, func(tx *gorm.DB) *gorm.DB {
		return tx.Preload("PricingPlanPeriod").Where("user_id = ? AND is_active = ?", userID, true).
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

func (s *BillingServiceDefault) GetPausedSubscription(ctx context.Context, userID uint) (*pluginCore.Subscriber, error) {
	ctx, span := core.TraceMethod(ctx, "BillingServiceDefault.GetPausedSubscription")
	defer span.End()

	var subscriber models.Subscriber
	err := db.RetryableComponentTransaction(s, ctx, func(tx *gorm.DB) *gorm.DB {
		return tx.Preload("PricingPlanPeriod").
			Where("user_id = ? AND is_active = ? AND paused_at IS NOT NULL AND cancelled_at IS NULL", userID, false).
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

func (s *BillingServiceDefault) GetRegistry(ctx context.Context) pluginCore.GatewayRegistry {
	ctx, span := core.TraceMethod(ctx, "BillingServiceDefault.GetRegistry")
	defer span.End()

	return s.gateways
}

func (s *BillingServiceDefault) GetPendingCancellations(ctx context.Context, gatewayType string, now time.Time) ([]pluginCore.Subscriber, error) {
	ctx, span := core.TraceMethod(ctx, "BillingServiceDefault.GetPendingCancellations")
	defer span.End()

	var subscribers []models.Subscriber
	err := db.RetryableComponentTransaction(s, ctx, func(tx *gorm.DB) *gorm.DB {
		return tx.Preload("PricingPlanPeriod").
			Where("gateway_type = ? AND is_active = ? AND will_cancel_at IS NOT NULL AND will_cancel_at <= ?", gatewayType, true, now).
			Find(&subscribers)
	})
	if err != nil {
		return nil, err
	}

	result := lo.Map(subscribers, func(sub models.Subscriber, _ int) pluginCore.Subscriber {
		return sub
	})
	return result, nil
}

func (s *BillingServiceDefault) GetSubscriberByID(ctx context.Context, id uint) (*pluginCore.Subscriber, error) {
	ctx, span := core.TraceMethod(ctx, "BillingServiceDefault.GetSubscriberByID")
	defer span.End()

	var subscriber models.Subscriber
	err := db.RetryableComponentTransaction(s, ctx, func(tx *gorm.DB) *gorm.DB {
		return tx.Preload("PricingPlanPeriod").First(&subscriber, id)
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	result := subscriber
	return &result, nil
}

func (s *BillingServiceDefault) GetSubscribersByUserID(ctx context.Context, userID uint) ([]pluginCore.Subscriber, error) {
	ctx, span := core.TraceMethod(ctx, "BillingServiceDefault.GetSubscribersByUserID")
	defer span.End()

	var subscribers []models.Subscriber
	err := db.RetryableComponentTransaction(s, ctx, func(tx *gorm.DB) *gorm.DB {
		return tx.Preload("PricingPlanPeriod").Where("user_id = ?", userID).Find(&subscribers)
	})
	if err != nil {
		return nil, err
	}

	result := lo.Map(subscribers, func(sub models.Subscriber, _ int) pluginCore.Subscriber {
		return sub
	})
	return result, nil
}

func (s *BillingServiceDefault) GetSubscriberByUserAndPeriod(ctx context.Context, userID uint, periodID uint) (*pluginCore.Subscriber, error) {
	ctx, span := core.TraceMethod(ctx, "BillingServiceDefault.GetSubscriberByUserAndPeriod")
	defer span.End()

	var subscriber models.Subscriber
	err := db.RetryableComponentTransaction(s, ctx, func(tx *gorm.DB) *gorm.DB {
		return tx.Preload("PricingPlanPeriod").
			Where("user_id = ? AND pricing_plan_period_id = ?", userID, periodID).
			First(&subscriber)
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	result := subscriber
	return &result, nil
}

func (s *BillingServiceDefault) GetSubscriptionHistoryByUserAndPeriod(ctx context.Context, userID uint, periodID uint) (*models.SubscriptionHistory, error) {
	ctx, span := core.TraceMethod(ctx, "BillingServiceDefault.GetSubscriptionHistoryByUserAndPeriod")
	defer span.End()

	var history models.SubscriptionHistory
	err := db.RetryableComponentTransaction(s, ctx, func(tx *gorm.DB) *gorm.DB {
		return tx.Where("user_id = ? AND pricing_plan_period_id = ?", userID, periodID).
			Order("ended_at DESC").
			First(&history)
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	return &history, nil
}

func (s *BillingServiceDefault) ListSubscribers(ctx context.Context, filters []queryutil.CrudFilter, sorts []queryutil.Sort, pagination queryutil.Pagination) ([]pluginCore.Subscriber, int64, error) {
	ctx, span := core.TraceMethod(ctx, "BillingServiceDefault.ListSubscribers")
	defer span.End()

	var subscribers []models.Subscriber
	var total int64

	err := db.RetryableComponentTransaction(s, ctx, func(tx *gorm.DB) *gorm.DB {
		query := tx.Model(&models.Subscriber{})

		// Apply filters using queryutil helper
		query = queryutil.ApplyFilters(query, filters, nil)

		// Get total count
		if err := query.Count(&total).Error; err != nil {
			return tx
		}

		// Apply sorts using queryutil helper
		query = queryutil.ApplySort(query, sorts)

		// Apply pagination using queryutil helper
		query = queryutil.ApplyPagination(query, pagination)

		return query.Preload("PricingPlanPeriod").Find(&subscribers)
	})
	if err != nil {
		return nil, 0, err
	}

	result := lo.Map(subscribers, func(sub models.Subscriber, _ int) pluginCore.Subscriber {
		return sub
	})
	return result, total, nil
}

// UpdateSubscriberPlan updates a subscriber's pricing plan period in the database
// This is used for database-only plan changes when the gateway doesn't support backend plan changes
func (s *BillingServiceDefault) UpdateSubscriberPlan(ctx context.Context, userID uint, gatewayType string, newPeriodID uint) (*pluginCore.PlanChangeResult, error) {
	ctx, span := core.TraceMethod(ctx, "BillingServiceDefault.UpdateSubscriberPlan")
	defer span.End()

	// Get the new pricing plan period to verify it exists and get plan info
	newPeriod, err := s.pricingService.GetPricingPlanPeriod(ctx, newPeriodID)
	if err != nil {
		return nil, fmt.Errorf("failed to get new pricing plan period: %w", err)
	}
	if newPeriod == nil {
		return nil, fmt.Errorf("pricing plan period not found: %d", newPeriodID)
	}

	// Get the pricing plan to validate it's active
	plan, err := s.pricingService.GetPricingPlan(ctx, newPeriod.PricingPlanID)
	if err != nil {
		return nil, fmt.Errorf("failed to get pricing plan: %w", err)
	}
	if plan == nil || !plan.IsActive {
		return nil, fmt.Errorf("new plan is not active")
	}

	// Find the active subscriber for this user and gateway
	var subscriber models.Subscriber
	err = db.RetryableComponentTransaction(s, ctx, func(tx *gorm.DB) *gorm.DB {
		return tx.Where("user_id = ? AND gateway_type = ? AND is_active = ?", userID, gatewayType, true).First(&subscriber)
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("no active subscription found for user %d", userID)
		}
		return nil, fmt.Errorf("failed to find active subscriber: %w", err)
	}

	// Store the old period ID for the result
	oldPeriodID := uint(0)
	if subscriber.PricingPlanPeriodID != nil {
		oldPeriodID = *subscriber.PricingPlanPeriodID
	}

	// Update the subscriber's pricing plan period
	now := time.Now()
	err = db.RetryableComponentTransaction(s, ctx, func(tx *gorm.DB) *gorm.DB {
		return tx.Model(&subscriber).Updates(map[string]any{
			"pricing_plan_period_id": newPeriodID,
			"previous_plan_id":       oldPeriodID,
			"updated_at":             now,
		})
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update subscriber plan: %w", err)
	}

	// Build the result
	result := &pluginCore.PlanChangeResult{
		Action:        pluginCore.PlanChangeActionComplete,
		EffectiveDate: &now,
	}

	return result, nil
}
