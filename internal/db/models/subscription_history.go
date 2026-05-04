package models

import (
	"time"

	"gorm.io/gorm"
)

// SubscriptionHistory tracks ended subscriptions for audit and proration calculations
type SubscriptionHistory struct {
	gorm.Model
	UserID              uint       `json:"user_id"`               // Link to users table
	PricingPlanID       uint       `json:"pricing_plan_id"`       // Plan reference
	PricingPlanPeriodID uint       `json:"pricing_plan_period_id"` // Period variant reference
	PaymentGatewayType  string     `json:"payment_gateway_type"`   // "stripe", "atlos", etc.
	BillingPeriodStart  *time.Time `json:"billing_period_start"`   // Current billing cycle start
	BillingPeriodEnd    *time.Time `json:"billing_period_end"`     // Current billing cycle end
	StartedAt           time.Time  `json:"started_at"`             // When subscription started (CreatedAt)
	EndedAt             time.Time  `json:"ended_at"`               // When subscription ended
}

// TableName sets the table name for SubscriptionHistory
func (SubscriptionHistory) TableName() string {
	return "billing_subscription_histories"
}
