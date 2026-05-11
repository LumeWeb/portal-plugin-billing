package models

import (
	"gorm.io/gorm"
)


type PricingPlan struct {
	gorm.Model
	Name         string  // Plan name (e.g. "Pro Plan")
	Description  string  // Human-readable description
	FeaturesJSON *string // JSON array of feature strings, nil means not set for updates
	Currency     string  // Currency code, defaults to USD
	IsActive     bool    // Whether plan is active
	IsPublic     bool    // Whether plan appears in price lines

	// Relationships
	Periods []PricingPlanPeriod `gorm:"foreignKey:PricingPlanID"`
}

// TableName sets the table name for PricingPlan
func (PricingPlan) TableName() string {
	return "billing_pricing_plans"
}
