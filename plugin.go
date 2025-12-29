package billing

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"go.lumeweb.com/portal-plugin-billing/build"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/internal"
	"go.lumeweb.com/portal-plugin-billing/internal/api"
	"go.lumeweb.com/portal-plugin-billing/internal/config"
	"go.lumeweb.com/portal-plugin-billing/internal/db/migrations"
	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/portal-plugin-billing/internal/gateway"
	"go.lumeweb.com/portal-plugin-billing/internal/gateway/stripe"
	"go.lumeweb.com/portal-plugin-billing/internal/service/billing"
	"go.lumeweb.com/portal/core"
)

func GetPluginInfo() core.PluginInfo {
	return core.PluginInfo{
		ID:      internal.PLUGIN_NAME,
		Version: build.GetInfo(),
		Depends: []string{"quota"},
		Services: func() ([]core.ServiceInfo, error) {
			return []core.ServiceInfo{
				{ID: internal.PLUGIN_NAME, Factory: billing.NewBillingService},
			}, nil
		},
		APIExtensions: func(ctx core.Context) ([]core.APIExtensionFactory, error) {
			return []core.APIExtensionFactory{
				api.NewAPIExtension(),
			}, nil
		},
		Meta: func(ctx core.Context, builder core.PortalMetaBuilder) error {
			cfg := core.GetServiceConfig[*config.ServiceConfig](ctx, pluginCore.BILLING_SERVICE)
			plugin, err := builder.Plugin(internal.PLUGIN_NAME)
			if err != nil {
				return fmt.Errorf("failed to get plugin meta builder for billing: %w", err)
			}

			plugin.AddMeta("stripe_publishable_key", cfg.Stripe.PublishableKey)
			plugin.AddMeta("stripe_pricing_table_id", cfg.Stripe.PricingTableID)

			return nil
		},
		Models: []any{
			&models.WebhookEvent{},
			&models.Subscriber{},
		},
		Migrations: core.DBMigration{
			core.DB_TYPE_MYSQL:  migrations.GetMySQL(),
			core.DB_TYPE_SQLITE: migrations.GetSQLite(),
		},
		Metrics: mergeMetrics(),
	}
}

func mergeMetrics() []prometheus.Collector {
	var collectors []prometheus.Collector
	collectors = append(collectors, billing.GetCollectors()...)
	collectors = append(collectors, gateway.GetCollectors()...)
	collectors = append(collectors, stripe.GetCollectors()...)
	return collectors
}

func init() {
	core.RegisterPlugin(GetPluginInfo())
}
