package models

import (
	"gorm.io/gorm"
)

// PricingPlanPeriod stores pricing plan variations for different billing cadences
type PricingPlanPeriod struct {
	gorm.Model
	PricingPlanID uint    // Foreign key to PricingPlan
	Cadence       string  // Billing cadence: monthly, yearly, quarterly, weekly
	PriceUSD      float64 // Price in USD
	QuotaPlanID   uint    // External quota plan ID from portal-plugin-quota service
	RollingDays   *int    // Optional rolling days window for quota calculations

	// Relationships
	PricingPlan *PricingPlan `gorm:"foreignKey:PricingPlanID"`
}

// TableName sets the table name for PricingPlanPeriod
func (PricingPlanPeriod) TableName() string {
	return "billing_pricing_plan_periods"
}
