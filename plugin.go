package billing

import (
	"go.lumeweb.com/portal-plugin-billing/build"
	"go.lumeweb.com/portal-plugin-billing/internal"
	"go.lumeweb.com/portal-plugin-billing/internal/api"
	"go.lumeweb.com/portal-plugin-billing/internal/db/migrations"
	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/portal-plugin-billing/internal/service/billing"
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
