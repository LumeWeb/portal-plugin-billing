package billing

import (
	"go.lumeweb.com/portal-plugin-billing/internal/api"
	pluginConfig "go.lumeweb.com/portal-plugin-billing/internal/config"
	"go.lumeweb.com/portal-plugin-billing/internal/cron"
	pluginDb "go.lumeweb.com/portal-plugin-billing/internal/db"
	"go.lumeweb.com/portal-plugin-billing/internal/service"
	"go.lumeweb.com/portal/core"
)

const pluginName = "billing"

func init() {
	core.RegisterPlugin(core.PluginInfo{
		ID: pluginName,
		Meta: func(context core.Context, builder core.PortalMetaBuilder) error {
			quotaConfig := context.Config().GetService(service.QUOTA_SERVICE).(*pluginConfig.QuotaConfig)
			billingConfig := context.Config().GetService(service.BILLING_SERVICE).(*pluginConfig.BillingConfig)

			if quotaConfig.Enabled {
				builder.AddFeatureFlag("quota", true)
			}

			if billingConfig.Enabled {
				builder.AddFeatureFlag("billing", true)

				if billingConfig.PaidPlansEnabled {
					builder.AddFeatureFlag("paid_billing", true)
				}

				if billingConfig.FreeStorage > 0 || billingConfig.FreeDownload > 0 || billingConfig.FreeUpload > 0 {
					builder.AddFeatureFlag("free_plan", true)
				}
			}

			return nil
		},
		API: func() (core.API, []core.ContextBuilderOption, error) {
			return api.NewAPI()
		},
		Services: func() ([]core.ServiceInfo, error) {
			return []core.ServiceInfo{
				{
					ID:      service.BILLING_SERVICE,
					Factory: service.NewBillingService,
				},
				{
					ID:      service.QUOTA_SERVICE,
					Factory: service.NewQuotaService,
					Depends: []string{core.PIN_SERVICE, core.UPLOAD_SERVICE, service.BILLING_SERVICE},
				},
			}, nil
		},
		Models: []any{
			&pluginDb.UserDownload{},
			&pluginDb.UserUpload{},
			&pluginDb.UserStorage{},
			&pluginDb.UserBandwidthQuota{},
			&pluginDb.UserStorageQuota{},
			&pluginDb.Plan{},
		},
		Cron: func() core.CronFactory {
			return func(ctx core.Context) (core.Cronable, error) {
				return cron.NewCron(ctx), nil
			}
		},
	})
}
