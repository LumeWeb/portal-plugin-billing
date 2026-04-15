package core

import (
	"context"

	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/portal/core"
	"go.lumeweb.com/queryutil"
)

// PRICING_SERVICE is the service ID for PricingService
const PRICING_SERVICE = "billing.pricing"

// PricingService handles pricing plan and price line management
type PricingService interface {
	core.Service
	core.Configurable

	// CreatePricingPlan creates a new pricing plan
	CreatePricingPlan(ctx context.Context, plan *models.PricingPlan) error

	// UpdatePricingPlan updates an existing pricing plan
	UpdatePricingPlan(ctx context.Context, id uint, plan *models.PricingPlan) error

	// DeletePricingPlan deletes a pricing plan (soft delete)
	DeletePricingPlan(ctx context.Context, id uint) error

	// GetPricingPlan retrieves a pricing plan by ID
	GetPricingPlan(ctx context.Context, id uint) (*models.PricingPlan, error)

	// GetPricingPlans retrieves pricing plans with filters, sorting, and pagination
	GetPricingPlans(ctx context.Context, userID uint, filters []queryutil.CrudFilter, sorts []queryutil.Sort, pagination queryutil.Pagination) ([]*models.PricingPlan, int64, error)

	// CreatePriceLine creates a new price line
	CreatePriceLine(ctx context.Context, line *models.PriceLine) error

	// UpdatePriceLine updates a price line
	UpdatePriceLine(ctx context.Context, id uint, line *models.PriceLine) error

	// GetPriceLines retrieves price lines with filters, sorting, and pagination
	GetPriceLines(ctx context.Context, userID uint, filters []queryutil.CrudFilter, sorts []queryutil.Sort, pagination queryutil.Pagination) ([]*models.PriceLine, int64, error)

	// GetPriceLine retrieves a single price line by ID
	GetPriceLine(ctx context.Context, id uint) (*models.PriceLine, error)

	// DeletePriceLine deletes a price line (soft delete)
	DeletePriceLine(ctx context.Context, id uint) error

	// AddPlanToPriceLine adds a pricing plan to a price line with a position
	AddPlanToPriceLine(ctx context.Context, priceLineID, planID uint, position int) error

	// RemovePlanFromPriceLine removes a pricing plan from a price line
	RemovePlanFromPriceLine(ctx context.Context, priceLineID, planID uint) error

	// UpdatePlanPosition updates the position of a plan within a price line
	UpdatePlanPosition(ctx context.Context, priceLineID, planID uint, newPosition int) error

	// GetPriceLinePlans returns all PriceLinePlan associations for a price line with plan details
	GetPriceLinePlans(ctx context.Context, priceLineID uint) ([]*models.PriceLinePlan, error)

	// AssignPriceLineToUser assigns a price line to a user
	AssignPriceLineToUser(ctx context.Context, userID, priceLineID uint) error

	// GetEffectivePriceLineForUser returns the price line for a user (assigned or default)
	GetEffectivePriceLineForUser(ctx context.Context, userID uint) (*models.PriceLine, error)

	// GetDefaultPriceLine returns the default price line
	GetDefaultPriceLine(ctx context.Context) (*models.PriceLine, error)

	// GetUpgradeDowngradePlans returns upgrade (>Position) and downgrade (<Position) options
	GetUpgradeDowngradePlans(ctx context.Context, currentPlanID uint, priceLineID uint) (*UpgradeDowngradePaths, error)

	// GetPlansForPriceLine returns all plans for a price line ordered by position
	GetPlansForPriceLine(ctx context.Context, priceLineID uint) ([]*models.PricingPlan, error)

	// CreateGatewayProductMapping creates a new mapping between a pricing plan and gateway product IDs
	CreateGatewayProductMapping(ctx context.Context, mapping *models.GatewayProductMapping) error

	// UpdateGatewayProductMapping updates an existing gateway product mapping
	UpdateGatewayProductMapping(ctx context.Context, id uint, mapping *models.GatewayProductMapping) error

	// GetGatewayProductMapping retrieves a mapping by plan ID and gateway type
	GetGatewayProductMapping(ctx context.Context, planID uint, gatewayType string) (*models.GatewayProductMapping, error)

	// GetGatewayProductMappingsByPlan retrieves all gateway mappings for a pricing plan
	GetGatewayProductMappingsByPlan(ctx context.Context, planID uint) ([]*models.GatewayProductMapping, error)

	// UpdateGatewaySyncStatus updates the sync status and timestamps for a mapping
	UpdateGatewaySyncStatus(ctx context.Context, planID uint, gatewayType string, syncResult SyncResult) error

	// RecordGatewaySyncError records an error and increments retry count for a mapping
	RecordGatewaySyncError(ctx context.Context, planID uint, gatewayType string, syncErr error) error

	// DeleteGatewayProductMapping deletes a gateway product mapping
	DeleteGatewayProductMapping(ctx context.Context, id uint) error

	// GetPendingSyncMappings retrieves all mappings with pending or error status for retry
	GetPendingSyncMappings(ctx context.Context, gatewayType string) ([]*models.GatewayProductMapping, error)

	// GetPriceLinesForPlan returns the price line plan associations for a given plan ID
	GetPriceLinesForPlan(ctx context.Context, planID uint) ([]*models.PriceLinePlan, error)

	// CreatePricingPlanPeriod creates a new pricing plan period
	CreatePricingPlanPeriod(ctx context.Context, period *models.PricingPlanPeriod) error

	// UpdatePricingPlanPeriod updates an existing pricing plan period
	UpdatePricingPlanPeriod(ctx context.Context, id uint, period *models.PricingPlanPeriod) error

	// DeletePricingPlanPeriod deletes a pricing plan period (soft delete)
	DeletePricingPlanPeriod(ctx context.Context, id uint) error

	// GetPricingPlanPeriod retrieves a pricing plan period by ID
	GetPricingPlanPeriod(ctx context.Context, id uint) (*models.PricingPlanPeriod, error)

	// GetPricingPlanPeriods retrieves all pricing plan periods for a given pricing plan
	GetPricingPlanPeriods(ctx context.Context, planID uint) ([]*models.PricingPlanPeriod, error)

	// GetPricingPlanPeriodsWithFilter retrieves pricing plan periods with filters, sorting, and pagination
	GetPricingPlanPeriodsWithFilter(ctx context.Context, filters []queryutil.CrudFilter, sorts []queryutil.Sort, pagination queryutil.Pagination) ([]*models.PricingPlanPeriod, int64, error)
}

// UpgradeDowngradePaths contains upgrade and downgrade plan options
type UpgradeDowngradePaths struct {
	Upgrades   []*models.PricingPlan
	Downgrades []*models.PricingPlan
}
