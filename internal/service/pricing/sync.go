package pricing

import (
	"context"
	"fmt"
	"time"

	"github.com/avast/retry-go"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/portal/core"
	"go.uber.org/zap"
)

// SyncManager handles synchronization of pricing plans with payment gateways
type SyncManager struct {
	pricingSvc pluginCore.PricingService
	billingSvc pluginCore.BillingService
	ctx        core.Context
}

// NewSyncManager creates a new sync manager with a core context
func NewSyncManager(pricingSvc pluginCore.PricingService, billingSvc pluginCore.BillingService, ctx core.Context) *SyncManager {
	return &SyncManager{
		pricingSvc: pricingSvc,
		billingSvc: billingSvc,
		ctx:        ctx,
	}
}

// SyncGatewayPlanResults represents synchronization results for all gateways
type SyncGatewayPlanResults struct {
	PlanID        uint
	TotalGateways int
	SuccessCount  int
	FailureCount  int
	Results       map[string]*pluginCore.SyncResult
	Errors        map[string]error
}

// SyncPricingPlan synchronizes a pricing plan with all payment gateways
func (s *SyncManager) SyncPricingPlan(ctx context.Context, planID uint) (*SyncGatewayPlanResults, error) {
	ctx, span := core.TraceMethod(ctx, "SyncManager.SyncPricingPlan")
	defer span.End()

	s.ctx.Logger().Info("starting sync of pricing plan", zap.Uint("plan_id", planID))

	plan, err := s.pricingSvc.GetPricingPlan(ctx, planID)
	if err != nil {
		s.ctx.Logger().Error("failed to get pricing plan", zap.Error(err))
		return nil, err
	}

	registry := s.billingSvc.GetRegistry(ctx)
	if registry == nil {
		return nil, fmt.Errorf("gateway registry not available")
	}

	allGateways := registry.GetAllGateways()

	s.ctx.Logger().Debug("syncing plan to gateways",
		zap.Uint("plan_id", planID),
		zap.Int("gateway_count", len(allGateways)))

	results := &SyncGatewayPlanResults{
		PlanID:        planID,
		TotalGateways: len(allGateways),
		Results:       make(map[string]*pluginCore.SyncResult),
		Errors:        make(map[string]error),
	}

	for gatewayID, gateway := range allGateways {
		syncResult, syncErr := s.syncGatewayAttempt(ctx, gateway, plan, gatewayID)

		if syncErr != nil {
			results.FailureCount++
			results.Errors[gatewayID] = syncErr
		} else if syncResult != nil && syncResult.Success && syncResult.Error == nil {
			results.SuccessCount++
			results.Results[gatewayID] = syncResult
		} else {
			results.FailureCount++
		}
	}

	s.ctx.Logger().Info("sync completed",
		zap.Uint("plan_id", planID),
		zap.Int("success_count", results.SuccessCount),
		zap.Int("failure_count", results.FailureCount),
		zap.Int("total_gateways", results.TotalGateways))

	return results, nil
}

// syncGatewayAttempt wraps syncGatewayPlan with metrics tracking using retry-go for transient failures
func (s *SyncManager) syncGatewayAttempt(
	ctx context.Context,
	gateway pluginCore.PaymentGateway,
	plan *models.PricingPlan,
	gatewayID string,
) (*pluginCore.SyncResult, error) {
	syncResult, syncErr := core.MetricTrackResult(
		SyncDuration.WithLabelValues(gatewayID),
		SyncFailures.WithLabelValues(gatewayID),
		func() (*pluginCore.SyncResult, error) {
			SyncAttempts.WithLabelValues(gatewayID).Inc()

			var result *pluginCore.SyncResult

			// Retry on transient failures with exponential backoff
			attemptErr := retry.Do(
				func() error {
					var syncErr error
					result, syncErr = s.syncGatewayPlan(ctx, gateway, plan, gatewayID)
					return syncErr
				},
				retry.Context(ctx),
				retry.Attempts(3),
				retry.DelayType(retry.BackOffDelay),
				retry.MaxJitter(time.Second),
				retry.LastErrorOnly(true),
			)

			// Only increment success if the sync was actually successful (gateway supports sync and succeeded)
			if attemptErr == nil && result != nil && result.Success && result.Error == nil {
				SyncSuccess.WithLabelValues(gatewayID).Inc()
			}

			return result, attemptErr
		},
	)

	return syncResult, syncErr
}

// syncGatewayPlan synchronizes a pricing plan with a specific gateway
func (s *SyncManager) syncGatewayPlan(
	ctx context.Context,
	gateway pluginCore.PaymentGateway,
	plan *models.PricingPlan,
	gatewayID string,
) (*pluginCore.SyncResult, error) {
	ctx, span := core.TraceMethod(ctx, "SyncManager.syncGatewayPlan")
	defer span.End()

	capabilities, err := pluginCore.AsGatewayCapabilities(gateway)
	if err != nil {
		SyncFailures.WithLabelValues(gatewayID).Inc()
		return nil, fmt.Errorf("gateway %s does not implement GatewayCapabilities", gatewayID)
	}

	if !capabilities.SupportsProductSync() {
		s.ctx.Logger().Debug("skipping gateway that doesn't support product sync",
			zap.String("gateway", gatewayID))
		return &pluginCore.SyncResult{
			Success: false,
			Error:   fmt.Errorf("gateway doesn't support sync"),
		}, nil
	}

	s.ctx.Logger().Debug("syncing pricing plan to gateway",
		zap.String("gateway", gatewayID),
		zap.Uint("plan_id", plan.ID),
		zap.String("plan_name", plan.Name))

	planInfo := &pluginCore.PricingPlanInfo{
		ID:              plan.ID,
		Name:            plan.Name,
		Description:     plan.Description,
		Currency:        plan.Currency,
		MonthlyPriceUSD: plan.MonthlyPriceUSD,
		YearlyPriceUSD:  plan.YearlyPriceUSD,
		IsActive:        plan.IsActive,
		IsPublic:        plan.IsPublic,
	}

	syncGateway, syncErr := pluginCore.AsGatewaySync(gateway)
	if syncErr != nil {
		return nil, fmt.Errorf("gateway %s does not implement GatewaySync", gatewayID)
	}

	syncResult, err := syncGateway.SyncPlan(ctx, planInfo)
	if err != nil {
		s.ctx.Logger().Error("sync failed for gateway",
			zap.String("gateway", gatewayID),
			zap.Error(err))
		return nil, err
	}

	s.ctx.Logger().Debug("sync completed successfully",
		zap.String("gateway", gatewayID),
		zap.String("product_id", syncResult.ProductID))

	return syncResult, nil
}




