package models

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGatewayProductMappingHasPricingPlanPeriodID(t *testing.T) {
	now := time.Now()
	mapping := GatewayProductMapping{
		GatewayType:         "stripe",
		RemoteProductID:     "prod_123",
		PricingPlanPeriodID: &[]uint{1}[0],
		PortalConfigurationID: &[]string{"config_abc"}[0],
		SyncStatus:          "synced",
		LastSyncedAt:        &now,
		ErrorMessage:        "",
		Retries:             0,
	}

	assert.Equal(t, uint(1), *mapping.PricingPlanPeriodID)
	assert.Equal(t, "stripe", mapping.GatewayType)
	assert.Equal(t, "prod_123", mapping.RemoteProductID)
	assert.Equal(t, "synced", mapping.SyncStatus)
}

func TestGatewayProductMappingTableName(t *testing.T) {
	mapping := GatewayProductMapping{}

	tableName := mapping.TableName()

	assert.Equal(t, "billing_gateway_product_mappings", tableName)
}

func TestGatewayProductMappingBeforeCreate(t *testing.T) {
	mapping := GatewayProductMapping{
		GatewayType:         "stripe",
		RemoteProductID:     "prod_123",
		PricingPlanPeriodID: &[]uint{1}[0],
	}

	err := mapping.BeforeCreate(nil)

	assert.NoError(t, err)
	assert.Equal(t, "pending", mapping.SyncStatus)
}
