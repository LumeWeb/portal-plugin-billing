package pricing

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal/core"
)

const (
	SyncPricingPlanJobSourceID = "billing"
	SyncPricingPlanJobType     = "plugin.billing.sync_pricing_plan"
)

// SyncPricingPlanJob is a CronJob that synchronizes pricing plans with payment gateways
type SyncPricingPlanJob struct {
	*core.BaseCronJob
}

// NewSyncPricingPlanJob creates a new sync pricing plan job
func NewSyncPricingPlanJob() core.CronJob {
	job := &SyncPricingPlanJob{}

	// Initialize BaseCronJob with default values
	jobID := uuid.New()
	scheduleDef := core.NewCronScheduleDefinition(core.CronScheduleTypeOnce)

	job.BaseCronJob = core.NewBaseCronJob(
		jobID,
		core.JobOriginPlugin,
		SyncPricingPlanJobSourceID,
		"Billing Pricing Plan Sync",
		scheduleDef,
		nil,
		core.WithExplicitJobType(SyncPricingPlanJobType),
	)

	return job
}

// Run executes the sync job
func (j *SyncPricingPlanJob) Run(ctx core.Context, eventCtx context.Context) error {
	var planID uint

	rargs := j.Args()

	// args represents the pricing plan ID to sync
	switch v := rargs.(type) {
	case uint:
		planID = v
	case float64:
		planID = uint(v)
	default:
		return fmt.Errorf("invalid job arguments type, expected uint got %T", j.Args())
	}

	// Get services
	pricingSvc := core.GetService[pluginCore.PricingService](ctx, pluginCore.PRICING_SERVICE)
	if pricingSvc == nil {
		return fmt.Errorf("pricing service not available")
	}

	billingSvc := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
	if billingSvc == nil {
		return fmt.Errorf("billing service not available")
	}

	// Create sync manager and execute
	syncManager := NewSyncManager(pricingSvc, billingSvc, ctx)
	_, err := syncManager.SyncPricingPlan(eventCtx, planID)
	return err
}
