package models

import (
	"gorm.io/gorm"
)


type PricingPlan struct {
	gorm.Model
	Name             string                             `gorm:"not null"`                      // Plan name (e.g. "Pro Plan")
	Description      string                             `gorm:"not null"`                      // Human-readable description
	FeaturesJSON     string                             `gorm:"type:text"`                      // JSON array of feature strings
	MonthlyPriceUSD  *float64                           // Monthly price in USD (nullable)
	YearlyPriceUSD   *float64                           // Yearly price in USD (nullable, inferred from monthly if nil)
	QuotaPlanID      *uint                              // Link to quota plan (nullable)
	Currency  string `gorm:"default:'USD'"` // Currency code, defaults to USD
	IsActive bool   `gorm:"default:true"`  // Whether plan is active
	IsPublic         bool                               `gorm:"default:false"`                  // Whether plan appears in price lines
}

// TableName sets the table name for PricingPlan
func (PricingPlan) TableName() string {
	return "billing_pricing_plans"
}
