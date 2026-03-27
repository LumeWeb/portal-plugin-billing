package models

import (
	"gorm.io/gorm"
)

// PriceLine groups multiple PricingPlans together
type PriceLine struct {
	gorm.Model
	Name        string `gorm:"not null"`    // Price line name (e.g. "Enterprise", "Standard")
	Description string `gorm:"not null"`    // Human-readable description
	IsActive    bool   `gorm:"default:true"` // Whether price line is active
	IsDefault   bool   `gorm:"default:false"` // Exactly one line is the default for all users
}

// PriceLinePlan is the junction table linking PricingPlans to PriceLines with ordering
type PriceLinePlan struct {
	PriceLineID uint `gorm:"not null"`   // Foreign key to PriceLine
	PlanID      uint `gorm:"not null"`   // Foreign key to PricingPlan
	Position    int  `gorm:"not null"`   // Defines order: 0=base, 1=first upgrade, 2=second upgrade, etc.
}

// TableName sets the table name for PriceLine
func (PriceLine) TableName() string {
	return "billing_pricelines"
}

// TableName sets the table name for PriceLinePlan
func (PriceLinePlan) TableName() string {
	return "billing_priceline_plans"
}

// PriceLineAssignment is the junction table assigning PriceLines to users
type PriceLineAssignment struct {
	gorm.Model
	PriceLineID uint `gorm:"not null"` // Foreign key to PriceLine
	UserID      uint `gorm:"not null"` // Foreign key to users table
}

// TableName sets the table name for PriceLineAssignment
func (PriceLineAssignment) TableName() string {
	return "billing_priceline_assignments"
}
