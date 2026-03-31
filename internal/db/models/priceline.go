package models

import (
	"gorm.io/gorm"
)

// PriceLine groups multiple PricingPlans together
type PriceLine struct {
	gorm.Model
	Name        string    // Price line name (e.g. "Enterprise", "Standard")
	Description string    // Human-readable description
	IsActive    bool   // Whether price line is active
	IsDefault   bool   // Exactly one line is the default for all users

	// Relationships
	PriceLinePlans     []PriceLinePlan     `gorm:"foreignKey:PriceLineID"`
	PriceLineAssignments []PriceLineAssignment `gorm:"foreignKey:PriceLineID"`
}

// PriceLinePlan is the junction table linking PricingPlans to PriceLines with ordering
type PriceLinePlan struct {
	PriceLineID uint   // Foreign key to PriceLine
	PlanID      uint   // Foreign key to PricingPlan
	Position    int    // Defines order: 0=base, 1=first upgrade, 2=second upgrade, etc.

	// Relationships
	PriceLine   *PriceLine   `gorm:"foreignKey:PriceLineID"`
	PricingPlan *PricingPlan `gorm:"foreignKey:PlanID"`
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
	PriceLineID uint // Foreign key to PriceLine
	UserID      uint // Foreign key to users table

	// Relationships
	PriceLine *PriceLine `gorm:"foreignKey:PriceLineID"`
}

// TableName sets the table name for PriceLineAssignment
func (PriceLineAssignment) TableName() string {
	return "billing_priceline_assignments"
}
