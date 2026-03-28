package models

import (
	"gorm.io/gorm"
)

// Subscriber represents a user with an active subscription
type Subscriber struct {
	gorm.Model
	UserID        uint   `gorm:"uniqueIndex:idx_user_gateway;not null"` // Link to users table
	GatewayType   string `gorm:"uniqueIndex:idx_user_gateway;not null"` // "stripe", "paypal", etc.
	ExternalID    string `gorm:"not null"`                              // External account identifier in gateway
	SubscriptionID string                                                // Gateway subscription object ID (e.g., sub_123 for Stripe)
	IsActive      bool   `gorm:"default:false"`                         // Active subscription flag
	PlanID        *uint  // Optional: current plan reference
}

// TableName sets the table name for Subscriber
func (Subscriber) TableName() string {
	return "billing_subscribers"
}
