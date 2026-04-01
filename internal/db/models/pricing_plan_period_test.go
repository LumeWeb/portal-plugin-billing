package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPricingPlanPeriod_TableName(t *testing.T) {
	period := &PricingPlanPeriod{}
	assert.Equal(t, "billing_pricing_plan_periods", period.TableName())
}

func TestPricingPlanPeriod_Fields(t *testing.T) {
	rollingDays := 30
	period := &PricingPlanPeriod{
		// gorm.Model fields: ID, CreatedAt, UpdatedAt, DeletedAt
		PricingPlanID: 1,
		Cadence:       "monthly",
		PriceUSD:      29.99,
		QuotaPlanID:   100,
		RollingDays:   &rollingDays,
	}

	// Verify all struct fields
	assert.Equal(t, uint(1), period.PricingPlanID)
	assert.Equal(t, "monthly", period.Cadence)
	assert.Equal(t, 29.99, period.PriceUSD)
	assert.Equal(t, uint(100), period.QuotaPlanID)
	assert.NotNil(t, period.RollingDays)
	assert.Equal(t, 30, *period.RollingDays)

	// Verify gorm.Model fields exist
	assert.NotNil(t, period.ID)
	assert.NotNil(t, period.CreatedAt)
	assert.NotNil(t, period.UpdatedAt)
	assert.NotNil(t, period.DeletedAt)
}

func TestPricingPlanPeriod_RollingDaysNilable(t *testing.T) {
	period := &PricingPlanPeriod{
		PricingPlanID: 1,
		Cadence:       "monthly",
		PriceUSD:      29.99,
		// Leave RollingDays nil
	}

	assert.Nil(t, period.RollingDays)
}
