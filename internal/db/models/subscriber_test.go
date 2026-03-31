package models

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSubscriberHasPricingPlanPeriodID(t *testing.T) {
	now := time.Now()
	planPeriodID := uint(1)
	subscriber := Subscriber{
		UserID:              1,
		GatewayType:         "stripe",
		ExternalID:          "ext_123",
		SubscriptionID:      "sub_123",
		IsActive:            true,
		PricingPlanPeriodID: &planPeriodID,
		BillingPeriodStart:  &now,
		BillingPeriodEnd:    &now,
		PaymentStatus:       "succeeded",
	}

	assert.Equal(t, uint(1), *subscriber.PricingPlanPeriodID)
	assert.Equal(t, "stripe", subscriber.GatewayType)
	assert.Equal(t, true, subscriber.IsActive)
}

func TestSubscriberTableName(t *testing.T) {
	subscriber := Subscriber{}

	tableName := subscriber.TableName()

	assert.Equal(t, "billing_subscribers", tableName)
}

func TestSubscriberHasPreviousPlanID(t *testing.T) {
	// Test that Subscriber has PreviousPlanID field (not PreviousPricingPlanPeriodID)
	// This field should be kept until migration strategy is determined
	previousPlanID := uint(2)
	subscriber := Subscriber{
		UserID:          1,
		GatewayType:     "stripe",
		ExternalID:      "ext_123",
		SubscriptionID:  "sub_123",
		IsActive:        true,
		PreviousPlanID:  &previousPlanID,
	}

	assert.Equal(t, uint(2), *subscriber.PreviousPlanID)
}
