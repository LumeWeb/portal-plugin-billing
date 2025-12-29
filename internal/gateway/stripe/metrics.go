package stripe

import (
	"github.com/prometheus/client_golang/prometheus"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
)

const (
	MetricCheckoutCompletedTotal  = "checkout_completed_total"
	MetricSubscriptionActivated   = "subscription_activated_total"
	MetricSubscriptionDeactivated = "subscription_deactivated_total"
	MetricSubscriptionUpdated     = "subscription_updated_total"
	MetricCustomerPortalCreated   = "customer_portal_created_total"
)

const (
	LabelStatusSuccess = "success"
	LabelStatusError   = "error"
)

var (
	CheckoutCompleted       *prometheus.CounterVec
	SubscriptionActivated   *prometheus.CounterVec
	SubscriptionDeactivated *prometheus.CounterVec
	SubscriptionUpdated     *prometheus.CounterVec
	CustomerPortalCreated   *prometheus.CounterVec
)

func init() {
	CheckoutCompleted = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      MetricCheckoutCompletedTotal,
			Subsystem: pluginCore.BILLING_SERVICE,
			Help:      "Total number of checkout sessions completed",
		},
		[]string{"status"},
	)
	SubscriptionActivated = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      MetricSubscriptionActivated,
			Subsystem: pluginCore.BILLING_SERVICE,
			Help:      "Total number of subscriptions activated",
		},
		[]string{"status"},
	)
	SubscriptionDeactivated = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      MetricSubscriptionDeactivated,
			Subsystem: pluginCore.BILLING_SERVICE,
			Help:      "Total number of subscriptions deactivated",
		},
		[]string{"status"},
	)
	SubscriptionUpdated = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      MetricSubscriptionUpdated,
			Subsystem: pluginCore.BILLING_SERVICE,
			Help:      "Total number of subscriptions updated",
		},
		[]string{"status"},
	)
	CustomerPortalCreated = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      MetricCustomerPortalCreated,
			Subsystem: pluginCore.BILLING_SERVICE,
			Help:      "Total number of customer portal sessions created",
		},
		[]string{"status"},
	)
}
