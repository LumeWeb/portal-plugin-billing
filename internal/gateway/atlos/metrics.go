package atlos

import (
	"github.com/prometheus/client_golang/prometheus"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
)

const (
	MetricCheckoutUIDisplayedTotal = "checkout_ui_displayed_total"
	MetricPaymentInitiatedTotal    = "payment_initiated_total"
)

const (
	LabelStatusSuccess = "success"
	LabelStatusError   = "error"
)

var (
	CheckoutUIDisplayed *prometheus.CounterVec
	PaymentInitiated    *prometheus.CounterVec
)

func init() {
	CheckoutUIDisplayed = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      MetricCheckoutUIDisplayedTotal,
			Subsystem: pluginCore.BILLING_SERVICE,
			Help:      "Total number of ATLOS checkout UI fragments displayed",
		},
		[]string{"status"},
	)
	PaymentInitiated = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      MetricPaymentInitiatedTotal,
			Subsystem: pluginCore.BILLING_SERVICE,
			Help:      "Total number of ATLOS payments initiated",
		},
		[]string{"status"},
	)
	prometheus.MustRegister(CheckoutUIDisplayed, PaymentInitiated)
}
