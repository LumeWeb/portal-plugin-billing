package tasks

import (
	"go.lumeweb.com/portal-plugin-billing/internal/cron/define"
	"go.lumeweb.com/portal-plugin-billing/internal/service"
	"go.lumeweb.com/portal/core"
)

func CronTaskUserQuotaReconcile(_ *define.CronTaskUserQuotaReconcileArgs, ctx core.Context) error {
	quotaService := ctx.Service(service.QUOTA_SERVICE).(*service.QuotaService)

	return quotaService.Reconcile(ctx)
}
