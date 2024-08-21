package billing

import (
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
	})
}
