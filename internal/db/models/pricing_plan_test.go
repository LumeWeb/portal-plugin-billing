package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPricingPlanBasicCreation(t *testing.T) {
	plan := PricingPlan{
		Name:         "Test Plan",
		Description:  "Test Description",
		FeaturesJSON: `["feature1","feature2"]`,
		Currency:     "USD",
		IsActive:     true,
		IsPublic:     true,
	}

	assert.Equal(t, "Test Plan", plan.Name)
	assert.Equal(t, "Test Description", plan.Description)
	assert.Equal(t, `["feature1","feature2"]`, plan.FeaturesJSON)
	assert.Equal(t, "USD", plan.Currency)
	assert.Equal(t, true, plan.IsActive)
	assert.Equal(t, true, plan.IsPublic)
}

func TestPricingPlanTableNameReturnsCorrectValue(t *testing.T) {
	plan := PricingPlan{}

	tableName := plan.TableName()

	assert.Equal(t, "billing_pricing_plans", tableName)
}
