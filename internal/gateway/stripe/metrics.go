package stripe

import (
	"github.com/prometheus/client_golang/prometheus"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
)

const (
	MetricCheckoutCompletedTotal            = "checkout_completed_total"
	MetricCheckoutSessionCreated           = "checkout_session_created_total"
	MetricSubscriptionActivated             = "subscription_activated_total"
	MetricSubscriptionDeactivated           = "subscription_deactivated_total"
	MetricSubscriptionUpdated               = "subscription_updated_total"
	MetricCustomerPortalCreated             = "customer_portal_created_total"
	MetricInvoicePaidTotal                  = "invoice_paid_total"
	MetricInvoicePaymentFailedTotal         = "invoice_payment_failed_total"
	MetricInvoicePaymentActionRequiredTotal = "invoice_payment_action_required_total"
)

const (
	LabelStatusSuccess = "success"
	LabelStatusError   = "error"
)

var (
	CheckoutCompleted            *prometheus.CounterVec
	CheckoutSessionCreated       *prometheus.CounterVec
	SubscriptionActivated        *prometheus.CounterVec
	SubscriptionDeactivated      *prometheus.CounterVec
	SubscriptionUpdated          *prometheus.CounterVec
	CustomerPortalCreated        *prometheus.CounterVec
	InvoicePaid                  *prometheus.CounterVec
	InvoicePaymentFailed         *prometheus.CounterVec
	InvoicePaymentActionRequired *prometheus.CounterVec
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
	CheckoutSessionCreated = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      MetricCheckoutSessionCreated,
			Help:      "Total number of checkout sessions created",
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
	InvoicePaid = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      MetricInvoicePaidTotal,
			Subsystem: pluginCore.BILLING_SERVICE,
			Help:      "Total number of invoice payments processed",
		},
		[]string{"status"},
	)
	InvoicePaymentFailed = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      MetricInvoicePaymentFailedTotal,
			Help:      "Total number of invoice payment failures",
		},
		[]string{},
	)
	InvoicePaymentActionRequired = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      MetricInvoicePaymentActionRequiredTotal,
			Help:      "Total number of invoice payments requiring customer action",
		},
		[]string{},
	)
}
