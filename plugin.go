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
	billing "go.lumeweb.com/portal-plugin-billing/internal/service/billing"
	"go.lumeweb.com/portal-plugin-billing/internal/service/credit"
	"go.lumeweb.com/portal-plugin-billing/internal/service/pricing"
	"go.lumeweb.com/portal/core"
	portal_plugin_billing "go.lumeweb.com/web/go/portal-plugin-billing"
)

const PRICING_SERVICE = "billing.pricing"

func GetPluginInfo() core.PluginInfo {
	return core.PluginInfo{
		ID:      internal.PLUGIN_NAME,
		Version: build.GetInfo(),
		Depends:    []string{"dashboard", "quota"},
		Services: func() ([]core.ServiceInfo, error) {
			return []core.ServiceInfo{
				{ID: internal.PLUGIN_NAME, Factory: billing.NewBillingService, Depends: []string{PRICING_SERVICE, pluginCore.CREDIT_SERVICE}},
				{ID: PRICING_SERVICE, Factory: pricing.NewPricingService},
				{ID: pluginCore.CREDIT_SERVICE, Factory: credit.NewCreditService},
			}, nil
		},
		APIExtensions: func(ctx core.Context) ([]core.APIExtensionFactory, error) {
			return []core.APIExtensionFactory{
				api.NewAPIExtension(),
				api.NewAdminExtension(),
			}, nil
		},
		Meta: func(ctx core.Context, builder core.PortalMetaBuilder) error {
			cfg := core.GetServiceConfig[*config.ServiceConfig](ctx, pluginCore.BILLING_SERVICE)
			plugin, err := builder.Plugin(internal.PLUGIN_NAME)
			if err != nil {
				return fmt.Errorf("failed to get plugin meta builder for billing: %w", err)
			}

			plugin.AddMeta("stripe_publishable_key", cfg.Stripe.PublishableKey)

			return nil
		},
		Models: []any{
			&models.WebhookEvent{},
			&models.Subscriber{},
			&models.PricingPlan{},
			&models.PriceLine{},
			&models.PriceLinePlan{},
			&models.PriceLineAssignment{},
			&models.GatewayProductMapping{},
			&models.CreditModel{},
			&models.CreditActiveView{},
			&models.CreditsBalanceView{},
			&models.PricingPlanPeriod{},
			&models.SubscriptionHistory{},
		},
		Migrations: core.DBMigration{
			core.DB_TYPE_MYSQL:  migrations.GetMySQL(),
			core.DB_TYPE_SQLITE: migrations.GetSQLite(),
		},
		CronJobs: []core.PluginCronJob{
			{
				Name: "sync_pricing_plan",
				Factory: func() (core.CronJob, error) {
					return pricing.NewSyncPricingPlanJob(), nil
				},
			},
			{
				Name: "cancellation_reconciliation",
				Factory: func() (core.CronJob, error) {
					return billing.NewCancellationReconciliationJob(), nil
				},
			},
		},
		Metrics:    mergeMetrics(),
		WebBundles: core.NewWebBundles(core.NewWebBundle(portal_plugin_billing.GetFS(), core.WithWebBundleTargetApps("dashboard"))),
	}
}

func mergeMetrics() []prometheus.Collector {
	return []prometheus.Collector{
		// billing service metrics
		billing.WebhookProcessed,
		billing.WebhookDuration,
		billing.SubscriberCreated,
		billing.SubscriberUpdated,
		billing.SubscriberDeactivated,
		billing.CheckoutUIErrors,
		// pricing sync metrics
		pricing.SyncAttempts,
		pricing.SyncSuccess,
		pricing.SyncFailures,
		pricing.SyncDuration,
		// gateway registry metrics (shared across all gateways)
		gateway.WebhookValidated,
		gateway.WebhookHandled,
		gateway.GatewayRegistered,
		// Gateway-specific metrics (stripe, atlos) are registered
		// automatically during gateway setup via the MetricsProvider
		// interface — see BillingServiceDefault.setupGateways().
	}
}

func init() {
	core.RegisterPlugin(GetPluginInfo())
}
