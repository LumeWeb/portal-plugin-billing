package pricing

import (
	"github.com/prometheus/client_golang/prometheus"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
)

const (
	MetricSyncAttempts = "sync_attempts_total"
	MetricSyncSuccess  = "sync_success_total"
	MetricSyncFailures = "sync_failures_total"
	MetricSyncDuration = "sync_duration_seconds"
)

var (
	SyncAttempts *prometheus.CounterVec
	SyncSuccess  *prometheus.CounterVec
	SyncFailures *prometheus.CounterVec
	SyncDuration *prometheus.HistogramVec
)

func init() {
	SyncAttempts = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      MetricSyncAttempts,
			Subsystem: pluginCore.BILLING_SERVICE,
			Help:      "Total number of pricing plan sync attempts",
		},
		[]string{"gateway"},
	)
	SyncSuccess = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      MetricSyncSuccess,
			Subsystem: pluginCore.BILLING_SERVICE,
			Help:      "Total number of successful pricing plan syncs",
		},
		[]string{"gateway"},
	)
	SyncFailures = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      MetricSyncFailures,
			Subsystem: pluginCore.BILLING_SERVICE,
			Help:      "Total number of failed pricing plan syncs",
		},
		[]string{"gateway"},
	)
	SyncDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:      MetricSyncDuration,
			Subsystem: pluginCore.BILLING_SERVICE,
			Help:      "Duration of pricing plan sync operations",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"gateway"},
	)
}
