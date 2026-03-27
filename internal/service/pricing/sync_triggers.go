package pricing

import (
	"context"
	"fmt"

	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal/core"
)

const (
	syncPricingPlanJobName = "sync_pricing_plan"
)


// triggerPlanSync queues a sync job for a pricing plan
func triggerPlanSync(cronService core.CronService, ctx context.Context, planID uint) error {
	// Build the job type identifier that matches CronService's registration
	// Format: plugin.{pluginID}.{jobName}
	jobType := core.GetCronJobIdentifier(core.JobOriginPlugin, fmt.Sprintf("%s.%s", pluginCore.BILLING_SERVICE, syncPricingPlanJobName))

	// Create job instance
	job, err := cronService.JobFactory().CreateJob(ctx, jobType)
	if err != nil {
		return fmt.Errorf("failed to create job instance: %w", err)
	}

	// Set planID as args
	job.SetArgs(planID)

	// Register the job for execution
	if err = cronService.RegisterJob(ctx, job, nil); err != nil {
		return fmt.Errorf("failed to register sync job: %w", err)
	}

	return nil
}
