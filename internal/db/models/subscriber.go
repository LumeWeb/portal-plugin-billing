package models

import (
	"time"

	"gorm.io/gorm"
)

// Subscriber represents a user with an active subscription
type Subscriber struct {
	gorm.Model
	UserID               uint        `json:"user_id"`                                       // Link to users table
	GatewayType          string      `json:"gateway_type"`                           // "stripe", "paypal", etc.
	ExternalID           string      `json:"external_id"`                                      // External account identifier in gateway
	SubscriptionID       string      `json:"subscription_id"`                                                    // Gateway subscription object ID (e.g., sub_123 for Stripe)
	IsActive             bool        `json:"is_active"`                                                // Active subscription flag
	PricingPlanPeriodID  *uint       `json:"pricing_plan_period_id"`                           // Current billing period variant reference

	// Relationships
	PricingPlanPeriod    *PricingPlanPeriod `gorm:"foreignKey:PricingPlanPeriodID"`

	// Billing and Payment Information
	BillingPeriodStart *time.Time `json:"billing_period_start"`                   // Start date of current billing cycle
	BillingPeriodEnd   *time.Time `json:"billing_period_end"`                        // End date of current billing cycle
	PaymentStatus      string      `json:"payment_status"`               // Payment transaction status ("pending", "succeeded", "failed", "processing")

	// Cancellation Information
	WillCancelAt *time.Time `json:"will_cancel_at"`      // When subscription will be automatically cancelled
	CancelledAt  *time.Time `json:"cancelled_at"`        // When subscription was cancelled

	// Plan Change Tracking
	PreviousPlanID *uint `json:"previous_plan_id"` // Previous plan reference after plan change
}

// TableName sets the table name for Subscriber
func (Subscriber) TableName() string {
	return "billing_subscribers"
}
