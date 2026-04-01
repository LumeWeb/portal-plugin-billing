package billing

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal/core"
	"go.uber.org/zap"
)

const (
	CancellationReconciliationJobSourceID = "billing"
	CancellationReconciliationJobType     = "plugin.billing.cancellation_reconciliation"
)

// CancellationReconciliationJob is a CronJob that reconciles subscription cancellations
// that have been scheduled for a future date. This job runs hourly to check all gateways
// for subscriptions with pending cancellations and triggers appropriate reconciliation handlers.
type CancellationReconciliationJob struct {
	*core.BaseCronJob
}

// NewCancellationReconciliationJob creates a new cancellation reconciliation job
func NewCancellationReconciliationJob() core.CronJob {
	job := &CancellationReconciliationJob{}

	// Initialize BaseCronJob with hourly schedule
	jobID := uuid.New()
	scheduleDef := core.NewCronScheduleDefinition(core.CronScheduleTypeHourly)

	job.BaseCronJob = core.NewBaseCronJob(
		jobID,
		core.JobOriginPlugin,
		CancellationReconciliationJobSourceID,
		"Billing Cancellation Reconciliation",
		scheduleDef,
		nil,
		core.WithExplicitJobType(CancellationReconciliationJobType),
	)

	return job
}

// Run executes the cancellation reconciliation job
func (j *CancellationReconciliationJob) Run(ctx core.Context, eventCtx context.Context) error {
	ctx.Logger().Info("Starting subscription cancellation reconciliation")

	// Get billing service
	billingSvc := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
	if billingSvc == nil {
		return fmt.Errorf("billing service not available")
	}

	// Get gateway registry
	gatewayRegistry := billingSvc.GetRegistry(ctx)
	if gatewayRegistry == nil {
		return fmt.Errorf("gateway registry not available")
	}

	// Get all registered gateways
	gateways := gatewayRegistry.GetAllGateways()

	now := time.Now().UTC()
	totalProcessed := 0
	totalErrors := 0

	// Reconcile cancellations for each gateway
	for gatewayID, gateway := range gateways {
		ctx.Logger().Debug("Processing gateway for cancellation reconciliation",
			zap.String("gateway_type", gatewayID))

		// Try to cast to PaymentGateway (not all gateways may implement all interfaces)
		paymentGateway, ok := gateway.(pluginCore.PaymentGateway)
		if !ok {
			ctx.Logger().Debug("Gateway cannot be cast to PaymentGateway, skipping",
				zap.String("gateway_type", gatewayID))
			continue
		}

		processed, err := reconcileGatewayCancellations(ctx, paymentGateway, now)
		if err != nil {
			ctx.Logger().Error("Failed to reconcile cancellations for gateway",
				zap.String("gateway_type", gatewayID),
				zap.Error(err))
			totalErrors++
			continue
		}

		if processed > 0 {
			ctx.Logger().Info("Gateway reconciled successfully",
				zap.String("gateway_type", gatewayID),
				zap.Int("processed_count", processed))
		}

		totalProcessed += processed
	}

	ctx.Logger().Info("Cancellation reconciliation completed",
		zap.Int("total_processed", totalProcessed),
		zap.Int("total_errors", totalErrors))

	return nil
}

// reconcileGatewayCancellations reconciles pending cancellations for a specific gateway
func reconcileGatewayCancellations(ctx core.Context, gateway pluginCore.PaymentGateway, now time.Time) (int, error) {
	// Check if gateway implements SubscriptionExecutor interface
	executor, err := pluginCore.AsSubscriptionExecutor(gateway)
	if err != nil {
		// Gateway doesn't implement SubscriptionExecutor, skip
		ctx.Logger().Debug("Gateway does not implement SubscriptionExecutor, skipping",
			zap.String("gateway_type", gateway.ID(nil)))
		return 0, nil
	}

	// Get billing service for querying subscribers
	billingSvc := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
	if billingSvc == nil {
		return 0, fmt.Errorf("billing service not available")
	}

	// Query subscribers with pending cancellations
	// These are subscribers with WillCancelAt set to a date in the past or now
	subscribers, err := billingSvc.GetPendingCancellations(ctx, gateway.ID(nil), now)
	if err != nil {
		return 0, fmt.Errorf("failed to get pending cancellations: %w", err)
	}

	processed := 0
	for _, subscriber := range subscribers {
		// Trigger the gateway's reconciliation handler
		ctx.Logger().Debug("Processing pending cancellation for subscriber",
			zap.Uint("user_id", subscriber.UserID),
			zap.String("subscription_id", subscriber.SubscriptionID),
			zap.Time("will_cancel_at", *subscriber.WillCancelAt),
			zap.String("gateway_type", gateway.ID(nil)))

		if err := executor.ReconcileCancellation(ctx, subscriber.UserID); err != nil {
			ctx.Logger().Error("Failed to reconcile cancellation for subscriber",
				zap.Uint("user_id", subscriber.UserID),
				zap.String("subscription_id", subscriber.SubscriptionID),
				zap.String("gateway_type", gateway.ID(nil)),
				zap.Error(err))
			continue
		}

		processed++
		ctx.Logger().Info("Successfully reconciled cancellation for subscriber",
			zap.Uint("user_id", subscriber.UserID),
			zap.String("subscription_id", subscriber.SubscriptionID),
			zap.String("gateway_type", gateway.ID(nil)))
	}

	return processed, nil
}
