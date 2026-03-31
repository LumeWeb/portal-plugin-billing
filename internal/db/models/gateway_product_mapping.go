package models

import (
	"time"

	"gorm.io/gorm"
)

// GatewayProductMapping stores gateway-specific product IDs for pricing plan periods.
// This junction table allows tracking remote product/price IDs for gateways that maintain
// their own product catalogs (e.g., Stripe products, PayPal plan IDs).
// It enables bidirectional sync and maintains proper state for each gateway.
type GatewayProductMapping struct {
	gorm.Model

	// PricingPlanPeriodID identifies the specific billing period variant
	PricingPlanPeriodID *uint
	PricingPlanPeriod   *PricingPlanPeriod `gorm:"foreignKey:PricingPlanPeriodID"`

	// GatewayType identifies the payment gateway (e.g., "stripe", "paypal")
	GatewayType string

	// RemoteProductID is the gateway's unique product identifier
	RemoteProductID string

	// RemotePriceID is the gateway's price ID for the specific pricing plan period
	RemotePriceID string

	// PortalConfigurationID stores the billing portal configuration ID for this plan
	// Used for Stripe customer portal to control upgrade/downgrade paths
	PortalConfigurationID *string

	// SyncStatus tracks the synchronization state
	SyncStatus string // pending, synced, error

	// LastSyncedAt records when the mapping was last synchronized with the gateway
	LastSyncedAt *time.Time

	// ErrorMessage captures any sync errors for debugging
	ErrorMessage string

	// Retries counts how many sync attempts have been made
	Retries int
}

// TableName sets the table name for GatewayProductMapping
func (GatewayProductMapping) TableName() string {
	return "billing_gateway_product_mappings"
}

// BeforeCreate hook to ensure valid sync status
func (g *GatewayProductMapping) BeforeCreate(tx *gorm.DB) error {
	if g.SyncStatus == "" {
		g.SyncStatus = "pending"
	}
	return nil
}
