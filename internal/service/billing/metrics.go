package billing

import (
	"github.com/prometheus/client_golang/prometheus"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
)

const (
	MetricWebhookProcessedTotal = "webhook_processed_total"
	MetricWebhookDuration       = "webhook_duration_seconds"
	MetricSubscriberCreated     = "subscriber_created_total"
	MetricSubscriberUpdated     = "subscriber_updated_total"
	MetricSubscriberDeactivated = "subscriber_deactivated_total"
)

const (
	LabelStatusSuccess = "success"
	LabelStatusError   = "error"
)

var (
	WebhookProcessed      *prometheus.CounterVec
	WebhookDuration       *prometheus.HistogramVec
	SubscriberCreated     *prometheus.CounterVec
	SubscriberUpdated     *prometheus.CounterVec
	SubscriberDeactivated *prometheus.CounterVec
)

func init() {
	WebhookProcessed = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      MetricWebhookProcessedTotal,
			Subsystem: pluginCore.BILLING_SERVICE,
			Help:      "Total number of webhooks processed",
		},
		[]string{"gateway_type", "event_type", "status"},
	)
	WebhookDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:      MetricWebhookDuration,
			Subsystem: pluginCore.BILLING_SERVICE,
			Help:      "Duration of webhook processing",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"gateway_type", "event_type"},
	)
	SubscriberCreated = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      MetricSubscriberCreated,
			Subsystem: pluginCore.BILLING_SERVICE,
			Help:      "Total number of subscribers created",
		},
		[]string{"gateway_type", "status"},
	)
	SubscriberUpdated = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      MetricSubscriberUpdated,
			Subsystem: pluginCore.BILLING_SERVICE,
			Help:      "Total number of subscribers updated",
		},
		[]string{"gateway_type", "status"},
	)
	SubscriberDeactivated = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      MetricSubscriberDeactivated,
			Subsystem: pluginCore.BILLING_SERVICE,
			Help:      "Total number of subscribers deactivated",
		},
		[]string{"gateway_type", "status"},
	)
}

func GetCollectors() []prometheus.Collector {
	return []prometheus.Collector{
		WebhookProcessed,
		WebhookDuration,
		SubscriberCreated,
		SubscriberUpdated,
		SubscriberDeactivated,
	}
}
