package billing

import (
	"fmt"
	"go.lumeweb.com/portal-plugin-billing/build"
	"go.lumeweb.com/portal-plugin-billing/internal"
	"go.lumeweb.com/portal-plugin-billing/internal/api"
	"go.lumeweb.com/portal-plugin-billing/internal/config"
	"go.lumeweb.com/portal-plugin-billing/internal/db/migrations"
	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/portal-plugin-billing/internal/service/billing"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal/core"
)

func init() {
	core.RegisterPlugin(core.PluginInfo{
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
	})
}
