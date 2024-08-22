package billing

import (
	"go.lumeweb.com/portal-plugin-billing/internal/cron"
	pluginDb "go.lumeweb.com/portal-plugin-billing/internal/db"
	"go.lumeweb.com/portal-plugin-billing/internal/service"
	"go.lumeweb.com/portal/core"
)

const pluginName = "billing"

func init() {
	core.RegisterPlugin(core.PluginInfo{
		ID: pluginName,
		Services: func() ([]core.ServiceInfo, error) {
			return []core.ServiceInfo{
				{
					ID:      service.BILLING_SERVICE,
					Factory: service.NewBillingService,
				},
				{
					ID:      service.QUOTA_SERVICE,
					Factory: service.NewQuotaService,
					Depends: []string{core.PIN_SERVICE, core.METADATA_SERVICE, service.BILLING_SERVICE},
				},
			}, nil
		},
		Models: []any{
			&pluginDb.Download{},
			&pluginDb.Upload{},
			&pluginDb.UserQuota{},
			&pluginDb.Plan{},
		},
		Cron: func() core.CronFactory {
			return func(ctx core.Context) (core.Cronable, error) {
				return cron.NewCron(ctx), nil
			}
		},
	})
}
