package gateway

import (
	"github.com/prometheus/client_golang/prometheus"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
)

const (
	MetricWebhookValidatedTotal = "webhook_validated_total"
	MetricWebhookHandledTotal   = "webhook_handled_total"
	MetricGatewayRegistered     = "gateway_registered_total"
)

const (
	LabelStatusSuccess = "success"
	LabelStatusError   = "error"
)

var (
	WebhookValidated  *prometheus.CounterVec
	WebhookHandled    *prometheus.CounterVec
	GatewayRegistered *prometheus.CounterVec
)

func init() {
	WebhookValidated = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      MetricWebhookValidatedTotal,
			Subsystem: pluginCore.BILLING_SERVICE,
			Help:      "Total number of webhooks validated",
		},
		[]string{"gateway_type", "status"},
	)
	WebhookHandled = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      MetricWebhookHandledTotal,
			Subsystem: pluginCore.BILLING_SERVICE,
			Help:      "Total number of webhooks handled",
		},
		[]string{"gateway_type", "status"},
	)
	GatewayRegistered = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      MetricGatewayRegistered,
			Subsystem: pluginCore.BILLING_SERVICE,
			Help:      "Total number of gateways registered",
		},
		[]string{"gateway_type", "status"},
	)
}

func GetCollectors() []prometheus.Collector {
	return []prometheus.Collector{
		WebhookValidated,
		WebhookHandled,
		GatewayRegistered,
	}
}
