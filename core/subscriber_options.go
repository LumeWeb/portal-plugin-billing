package core

import "time"

// SubscriberOption is a function that configures subscriber options
type SubscriberOption func(*SubscriberOptions)

// SubscriberOptions holds optional subscriber configuration
type SubscriberOptions struct {
	BillingPeriodStart  *time.Time
	BillingPeriodEnd    *time.Time
	WillCancelAt        *time.Time
	ClearWillCancelAt   bool // Flag to clear WillCancelAt (set to nil)
}

// WithBillingPeriodStart sets the billing period start date
func WithBillingPeriodStart(start *time.Time) SubscriberOption {
	return func(opts *SubscriberOptions) {
		opts.BillingPeriodStart = start
	}
}

// WithBillingPeriodEnd sets the billing period end date
func WithBillingPeriodEnd(end *time.Time) SubscriberOption {
	return func(opts *SubscriberOptions) {
		opts.BillingPeriodEnd = end
	}
}

// WithWillCancelAt sets the scheduled cancellation date
func WithWillCancelAt(cancelAt *time.Time) SubscriberOption {
	return func(opts *SubscriberOptions) {
		opts.WillCancelAt = cancelAt
		opts.ClearWillCancelAt = false // Explicit setting takes precedence
	}
}

// WithClearWillCancelAt clears the scheduled cancellation date
func WithClearWillCancelAt() SubscriberOption {
	return func(opts *SubscriberOptions) {
		opts.ClearWillCancelAt = true
		opts.WillCancelAt = nil // Ensure it's nil
	}
}

// ApplySubscriberOptions applies all options and returns the configured options struct
func ApplySubscriberOptions(opts ...SubscriberOption) SubscriberOptions {
	var options SubscriberOptions
	for _, opt := range opts {
		opt(&options)
	}
	return options
}
