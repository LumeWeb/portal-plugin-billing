package pricing

import (
	"context"
	"errors"
	"fmt"
	"time"

	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/internal/config"
	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/portal/core"
	"go.lumeweb.com/portal/db"
	"go.lumeweb.com/queryutil"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Predefined errors
var (
	ErrPricingPlanNameRequired        = errors.New("pricing plan name is required")
	ErrPricingPlanDescriptionRequired = errors.New("pricing plan description is required")
	ErrPriceLineNameRequired          = errors.New("price line name is required")
	ErrPriceLineDescriptionRequired   = errors.New("price line description is required")
	ErrGatewayTypeRequired            = errors.New("gateway type is required")
	ErrPricingPlanNotFound            = "pricing plan with ID %d not found"
	ErrDefaultPriceLineNotFound       = "default price line not found"
)

type PricingServiceDefault struct {
	*core.BaseComponent
	config      *config.ServiceConfig
	logger      *core.Logger
	cronService core.CronService
}

func NewPricingService() (core.Service, []core.ContextBuilderOption, error) {
	service := &PricingServiceDefault{}

	return service, core.ContextOptions(
		core.ContextWithStartupFunc(func(ctx core.Context) error {
			service.config = core.GetServiceConfig[*config.ServiceConfig](ctx, pluginCore.BILLING_SERVICE)
			service.logger = ctx.NamedLogger("billing.pricing_service")
			service.cronService = core.GetService[core.CronService](ctx, core.CRON_SERVICE)
			return nil
		}),
	), nil
}

func (s *PricingServiceDefault) ID() string {
	return pluginCore.PRICING_SERVICE
}

func (s *PricingServiceDefault) GetConfig() (any, error) {
	return &config.ServiceConfig{}, nil
}

// CreatePricingPlan creates a new pricing plan
func (s *PricingServiceDefault) CreatePricingPlan(ctx context.Context, plan *models.PricingPlan) error {
	if plan.Name == "" {
		return ErrPricingPlanNameRequired
	}
	if plan.Description == "" {
		return ErrPricingPlanDescriptionRequired
	}

	err := s.withTracedTransaction(ctx, "CreatePricingPlan", func(tx *gorm.DB) error {
		return tx.Create(plan).Error
	})

	if err == nil {
		s.triggerSyncWithLogging(ctx, plan.ID, "plan")
	}

	return err
}

// UpdatePricingPlan updates an existing pricing plan
func (s *PricingServiceDefault) UpdatePricingPlan(ctx context.Context, id uint, plan *models.PricingPlan) error {
	result, err := s.withTracedTransactionResult(ctx, "UpdatePricingPlan", func(tx *gorm.DB) *gorm.DB {
		return tx.Model(&models.PricingPlan{}).
			Where("id = ?", id).
			Updates(plan)
	})

	if err != nil {
		return err
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf(ErrPricingPlanNotFound, id)
	}

	s.triggerSyncWithLogging(ctx, id, "plan")
	return nil
}

// DeletePricingPlan deletes a pricing plan (soft delete via GORM)
func (s *PricingServiceDefault) DeletePricingPlan(ctx context.Context, id uint) error {
	return s.deleteEntity(ctx, "DeletePricingPlan", &models.PricingPlan{}, id)
}

// GetPricingPlan retrieves a pricing plan by ID
func (s *PricingServiceDefault) GetPricingPlan(ctx context.Context, id uint) (*models.PricingPlan, error) {
	var plan models.PricingPlan
	err := s.withTracedTransaction(ctx, "GetPricingPlan", func(tx *gorm.DB) error {
		return tx.First(&plan, id).Error
	})

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf(ErrPricingPlanNotFound, id)
		}
		return nil, err
	}

	return &plan, nil
}

// GetPricingPlans retrieves pricing plans with filters, sorting, and pagination
func (s *PricingServiceDefault) GetPricingPlans(ctx context.Context, userID uint, filters []queryutil.CrudFilter, sorts []queryutil.Sort, pagination queryutil.Pagination) ([]*models.PricingPlan, int64, error) {
	var plans []*models.PricingPlan
	total := s.paginatedQuery(ctx, "GetPricingPlans", &models.PricingPlan{}, filters, sorts, pagination, &plans, func(err error) {
		s.logger.Error("failed to count pricing plans", zap.Error(err))
	})
	return plans, total, nil
}

// CreatePriceLine creates a new price line
func (s *PricingServiceDefault) CreatePriceLine(ctx context.Context, line *models.PriceLine) error {
	if line.Name == "" {
		return ErrPriceLineNameRequired
	}
	if line.Description == "" {
		return ErrPriceLineDescriptionRequired
	}

	if line.IsDefault {
		var count int64
		err := s.withTracedTransaction(ctx, "CreatePriceLine-CountDefault", func(tx *gorm.DB) error {
			return tx.Model(&models.PriceLine{}).Where("is_default = ?", true).Count(&count).Error
		})
		if err != nil {
			return err
		}
		if count > 0 {
			return errors.New("a default price line already exists")
		}
	}

	return s.withTracedTransaction(ctx, "CreatePriceLine", func(tx *gorm.DB) error {
		return tx.Create(line).Error
	})
}

// UpdatePriceLine updates a price line
func (s *PricingServiceDefault) UpdatePriceLine(ctx context.Context, id uint, line *models.PriceLine) error {
	if line.IsDefault {
		var count int64
		err := s.withTracedTransaction(ctx, "UpdatePriceLine-CountDefault", func(tx *gorm.DB) error {
			return tx.Model(&models.PriceLine{}).Where("is_default = ? AND id != ?", true, id).
				Count(&count).Error
		})
		if err != nil {
			return err
		}
		if count > 0 {
			return errors.New("a default price line already exists")
		}
	}

	return s.withTracedTransaction(ctx, "UpdatePriceLine", func(tx *gorm.DB) error {
		return tx.Model(&models.PriceLine{}).Where("id = ?", id).Updates(line).Error
	})
}

// DeletePriceLine deletes a price line (soft delete)
func (s *PricingServiceDefault) DeletePriceLine(ctx context.Context, id uint) error {
	return s.deleteEntity(ctx, "DeletePriceLine", &models.PriceLine{}, id)
}

// AddPlanToPriceLine adds a pricing plan to a price line with a position
func (s *PricingServiceDefault) AddPlanToPriceLine(ctx context.Context, priceLineID, planID uint, position int) error {
	return s.withTracedTransaction(ctx, "AddPlanToPriceLine", func(tx *gorm.DB) error {
		var count int64
		result := tx.Model(&models.PriceLinePlan{}).
			Where("price_line_id = ? AND plan_id = ?", priceLineID, planID).
			Count(&count)
		if result.Error != nil {
			return result.Error
		}
		if count > 0 {
			return nil
		}

		priceLinePlan := &models.PriceLinePlan{
			PriceLineID: priceLineID,
			PlanID:      planID,
			Position:    position,
		}

		return tx.Create(priceLinePlan).Error
	})
}

// RemovePlanFromPriceLine removes a pricing plan from a price line and reorders remaining
func (s *PricingServiceDefault) RemovePlanFromPriceLine(ctx context.Context, priceLineID, planID uint) error {
	return s.withTracedTransaction(ctx, "RemovePlanFromPriceLine", func(tx *gorm.DB) error {
		result := tx.Exec("DELETE FROM billing_priceline_plans WHERE price_line_id = ? AND plan_id = ?", priceLineID, planID)
		if result.Error != nil {
			return result.Error
		}

		var plans []models.PriceLinePlan
		result = tx.Where("price_line_id = ?", priceLineID).
			Order("position ASC").
			Find(&plans)
		if result.Error != nil {
			return result.Error
		}

		for i := range plans {
			result = tx.Exec("UPDATE billing_priceline_plans SET position = ? WHERE price_line_id = ? AND plan_id = ?", i, plans[i].PriceLineID, plans[i].PlanID)
			if result.Error != nil {
				return result.Error
			}
		}

		return nil
	})
}

// AssignPriceLineToUser assigns a price line to a user using upsert
func (s *PricingServiceDefault) AssignPriceLineToUser(ctx context.Context, userID, priceLineID uint) error {
	return s.withTracedTransaction(ctx, "AssignPriceLineToUser", func(tx *gorm.DB) error {
		var existing models.PriceLineAssignment
		result := tx.Where("user_id = ?", userID).First(&existing)

		if result.Error == nil {
			existing.PriceLineID = priceLineID
			return tx.Save(&existing).Error
		}

		if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return result.Error
		}

		assignment := &models.PriceLineAssignment{
			PriceLineID: priceLineID,
			UserID:      userID,
		}
		return tx.Create(assignment).Error
	})
}

// GetEffectivePriceLineForUser returns the price line for a user (assigned or default)
func (s *PricingServiceDefault) GetEffectivePriceLineForUser(ctx context.Context, userID uint) (*models.PriceLine, error) {
	var assignment models.PriceLineAssignment
	assignmentErr := s.withTracedTransaction(ctx, "GetEffectivePriceLineForUser-Assignment", func(tx *gorm.DB) error {
		return tx.Where("user_id = ?", userID).First(&assignment).Error
	})

	if assignmentErr == nil && assignment.PriceLineID != 0 {
		var line models.PriceLine
		err := s.withTracedTransaction(ctx, "GetEffectivePriceLineForUser-Line", func(tx *gorm.DB) error {
			return tx.First(&line, assignment.PriceLineID).Error
		})
		if err == nil {
			return &line, nil
		}
	}

	var line models.PriceLine
	err := s.withTracedTransaction(ctx, "GetEffectivePriceLineForUser-Default", func(tx *gorm.DB) error {
		return tx.Where("is_default = ?", true).First(&line).Error
	})

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(ErrDefaultPriceLineNotFound)
		}
		return nil, err
	}

	return &line, nil
}

// GetDefaultPriceLine returns the default price line
func (s *PricingServiceDefault) GetDefaultPriceLine(ctx context.Context) (*models.PriceLine, error) {
	var line models.PriceLine
	err := s.withTracedTransaction(ctx, "GetDefaultPriceLine", func(tx *gorm.DB) error {
		return tx.Where("is_default = ?", true).First(&line).Error
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(ErrDefaultPriceLineNotFound)
		}
		return nil, err
	}
	return &line, nil
}

// GetPriceLines retrieves price lines with filters, sorting, and pagination
func (s *PricingServiceDefault) GetPriceLines(ctx context.Context, userID uint, filters []queryutil.CrudFilter, sorts []queryutil.Sort, pagination queryutil.Pagination) ([]*models.PriceLine, int64, error) {
	var lines []*models.PriceLine
	total := s.paginatedQuery(ctx, "GetPriceLines", &models.PriceLine{}, filters, sorts, pagination, &lines, func(err error) {
		s.logger.Error("failed to count price lines", zap.Error(err))
	})
	return lines, total, nil
}

// GetUpgradeDowngradePlans returns upgrade (>Position) and downgrade (<Position) options
func (s *PricingServiceDefault) GetUpgradeDowngradePlans(ctx context.Context, currentPlanID uint, priceLineID uint) (*pluginCore.UpgradeDowngradePaths, error) {
	var currentPlan models.PriceLinePlan
	if err := s.withTracedTransaction(ctx, "GetUpgradeDowngradePlans-Current", func(tx *gorm.DB) error {
		return tx.Where("price_line_id = ? AND plan_id = ?", priceLineID, currentPlanID).
			First(&currentPlan).Error
	}); err != nil {
		return nil, fmt.Errorf("plan not found in this price line")
	}

	var plans []models.PriceLinePlan
	if err := s.withTracedTransaction(ctx, "GetUpgradeDowngradePlans-All", func(tx *gorm.DB) error {
		return tx.Where("price_line_id = ?", priceLineID).
			Order("position ASC").
			Find(&plans).Error
	}); err != nil {
		return nil, err
	}

	paths := &pluginCore.UpgradeDowngradePaths{
		Upgrades:   []*models.PricingPlan{},
		Downgrades: []*models.PricingPlan{},
	}

	planIDs := make([]uint, 0, len(plans))
	for _, plan := range plans {
		if plan.PlanID != currentPlan.PlanID {
			planIDs = append(planIDs, plan.PlanID)
		}
	}

	planMap := make(map[uint]*models.PricingPlan)
	if len(planIDs) > 0 {
		var fetchedPlans []*models.PricingPlan
		err := s.withTracedTransaction(ctx, "GetUpgradeDowngradePlans-FetchPlans", func(tx *gorm.DB) error {
			return tx.Where("id IN ?", planIDs).Find(&fetchedPlans).Error
		})
		if err != nil {
			return nil, err
		}

		for _, p := range fetchedPlans {
			planMap[p.ID] = p
		}
	}

	for _, plan := range plans {
		if pricingPlan, ok := planMap[plan.PlanID]; ok {
			if plan.Position > currentPlan.Position {
				paths.Upgrades = append(paths.Upgrades, pricingPlan)
			} else if plan.Position < currentPlan.Position {
				paths.Downgrades = append(paths.Downgrades, pricingPlan)
			}
		}
	}

	return paths, nil
}

// GetPlansForPriceLine returns all plans for a price line ordered by position
func (s *PricingServiceDefault) GetPlansForPriceLine(ctx context.Context, priceLineID uint) ([]*models.PricingPlan, error) {
	var priceLinePlans []models.PriceLinePlan
	err := s.withTracedTransaction(ctx, "GetPlansForPriceLine-Query", func(tx *gorm.DB) error {
		return tx.Where("price_line_id = ?", priceLineID).
			Order("position ASC").
			Find(&priceLinePlans).Error
	})
	if err != nil {
		return nil, err
	}

	planIDs := make([]uint, 0, len(priceLinePlans))
	for _, plan := range priceLinePlans {
		planIDs = append(planIDs, plan.PlanID)
	}

	plans := make([]*models.PricingPlan, 0, len(priceLinePlans))
	if len(planIDs) > 0 {
		var fetchedPlans []*models.PricingPlan
		err := s.withTracedTransaction(ctx, "GetPlansForPriceLine-FetchPlans", func(tx *gorm.DB) error {
			return tx.Where("id IN ?", planIDs).Find(&fetchedPlans).Error
		})
		if err != nil {
			return nil, err
		}

		planMap := make(map[uint]*models.PricingPlan)
		for _, p := range fetchedPlans {
			planMap[p.ID] = p
		}

		for _, plp := range priceLinePlans {
			if plan, ok := planMap[plp.PlanID]; ok {
				plans = append(plans, plan)
			}
		}
	}

	return plans, nil
}

// CreateGatewayProductMapping creates a new mapping between a pricing plan and gateway product IDs
func (s *PricingServiceDefault) CreateGatewayProductMapping(ctx context.Context, mapping *models.GatewayProductMapping) error {
	if mapping.GatewayType == "" {
		return ErrGatewayTypeRequired
	}

	return s.withTracedTransaction(ctx, "CreateGatewayProductMapping", func(tx *gorm.DB) error {
		return tx.Create(mapping).Error
	})
}

// UpdateGatewayProductMapping updates an existing gateway product mapping
func (s *PricingServiceDefault) UpdateGatewayProductMapping(ctx context.Context, id uint, mapping *models.GatewayProductMapping) error {
	return s.withTracedTransaction(ctx, "UpdateGatewayProductMapping", func(tx *gorm.DB) error {
		return tx.Model(&models.GatewayProductMapping{}).
			Where("id = ?", id).
			Updates(mapping).Error
	})
}

// GetGatewayProductMapping retrieves a mapping by plan ID and gateway type
func (s *PricingServiceDefault) GetGatewayProductMapping(ctx context.Context, planID uint, gatewayType string) (*models.GatewayProductMapping, error) {
	var mapping models.GatewayProductMapping
	err := s.withTracedTransaction(ctx, "GetGatewayProductMapping", func(tx *gorm.DB) error {
		return tx.Where("plan_id = ? AND gateway_type = ?", planID, gatewayType).
			First(&mapping).Error
	})

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("gateway product mapping for plan %d and gateway %s not found", planID, gatewayType)
		}
		return nil, err
	}

	return &mapping, nil
}

// GetGatewayProductMappingsByPlan retrieves all gateway mappings for a pricing plan
func (s *PricingServiceDefault) GetGatewayProductMappingsByPlan(ctx context.Context, planID uint) ([]*models.GatewayProductMapping, error) {
	var mappings []*models.GatewayProductMapping
	err := s.withTracedTransaction(ctx, "GetGatewayProductMappingsByPlan", func(tx *gorm.DB) error {
		return tx.Where("plan_id = ?", planID).
			Find(&mappings).Error
	})

	if err != nil {
		return nil, err
	}

	return mappings, nil
}

// UpdateGatewaySyncStatus updates the sync status and timestamps for a mapping
func (s *PricingServiceDefault) UpdateGatewaySyncStatus(ctx context.Context, planID uint, gatewayType string, syncResult pluginCore.SyncResult) error {
	now := time.Now()

	err := s.withTracedTransaction(ctx, "UpdateGatewaySyncStatus", func(tx *gorm.DB) error {
		updates := map[string]any{
			"remote_product_id":       syncResult.ProductID,
			"remote_monthly_price_id": syncResult.MonthlyPriceID,
			"remote_yearly_price_id":  syncResult.YearlyPriceID,
			"sync_status":             "synced",
			"last_synced_at":          &now,
			"error_message":           "",
			"retries":                 0,
		}

		return tx.Model(&models.GatewayProductMapping{}).
			Where("plan_id = ? AND gateway_type = ?", planID, gatewayType).
			Updates(updates).Error
	})

	if err == nil {
		s.triggerSyncWithLogging(ctx, planID, "gateway status update")
	}

	return err
}

// RecordGatewaySyncError records an error and increments retry count for a mapping
func (s *PricingServiceDefault) RecordGatewaySyncError(ctx context.Context, planID uint, gatewayType string, syncErr error) error {
	s.logger.Error("recording gateway sync error",
		zap.Uint("plan_id", planID),
		zap.String("gateway_type", gatewayType),
		zap.Error(syncErr))

	currentRetries := 0
	var mapping models.GatewayProductMapping

	err := s.withTracedTransaction(ctx, "RecordGatewaySyncError-Query", func(tx *gorm.DB) error {
		return tx.Where("plan_id = ? AND gateway_type = ?", planID, gatewayType).
			First(&mapping).Error
	})

	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	if err == nil {
		currentRetries = mapping.Retries
	}

	now := time.Now()

	return s.withTracedTransaction(ctx, "RecordGatewaySyncError-Update", func(tx *gorm.DB) error {
		updates := map[string]any{
			"sync_status":    "error",
			"error_message":  syncErr.Error(),
			"last_synced_at": &now,
			"retries":        currentRetries + 1,
		}

		return tx.Model(&models.GatewayProductMapping{}).
			Where("plan_id = ? AND gateway_type = ?", planID, gatewayType).
			Updates(updates).Error
	})
}

// DeleteGatewayProductMapping deletes a gateway product mapping
func (s *PricingServiceDefault) DeleteGatewayProductMapping(ctx context.Context, id uint) error {
	return s.deleteEntity(ctx, "DeleteGatewayProductMapping", &models.GatewayProductMapping{}, id)
}

// GetPendingSyncMappings retrieves all mappings with pending or error status for retry
func (s *PricingServiceDefault) GetPendingSyncMappings(ctx context.Context, gatewayType string) ([]*models.GatewayProductMapping, error) {
	var mappings []*models.GatewayProductMapping
	err := s.withTracedTransaction(ctx, "GetPendingSyncMappings", func(tx *gorm.DB) error {
		q := tx.Model(&models.GatewayProductMapping{}).
			Where("sync_status IN ?", []string{"pending", "error"})

		if gatewayType != "" {
			q = q.Where("gateway_type = ?", gatewayType)
		}

		q = q.Order("retries ASC, created_at DESC")

		return q.Find(&mappings).Error
	})

	if err != nil {
		return nil, err
	}

	return mappings, nil
}

// GetPriceLinesForPlan returns the price line plan associations for a given plan ID
func (s *PricingServiceDefault) GetPriceLinesForPlan(ctx context.Context, planID uint) ([]*models.PriceLinePlan, error) {
	var priceLinePlans []*models.PriceLinePlan
	err := s.withTracedTransaction(ctx, "GetPriceLinesForPlan", func(tx *gorm.DB) error {
		return tx.Where("plan_id = ?", planID).Find(&priceLinePlans).Error
	})
	if err != nil {
		return nil, err
	}

	return priceLinePlans, nil
}

// Helper methods for DRY

// withTracedTransaction combines context tracing and retryable transaction
func (s *PricingServiceDefault) withTracedTransaction(ctx context.Context, methodName string, fn func(tx *gorm.DB) error) error {
	ctx, span := core.TraceMethod(ctx, "PricingServiceDefault."+methodName)
	defer span.End()

	return db.RetryableComponentTransaction(s, ctx, func(tx *gorm.DB) *gorm.DB {
		if err := fn(tx.WithContext(ctx)); err != nil {
			_ = tx.AddError(err)
		}
		return tx
	})
}

// withTracedTransactionResult is a version that returns the GORM result for checking RowsAffected
func (s *PricingServiceDefault) withTracedTransactionResult(ctx context.Context, methodName string, fn func(tx *gorm.DB) *gorm.DB) (*gorm.DB, error) {
	ctx, span := core.TraceMethod(ctx, "PricingServiceDefault."+methodName)
	defer span.End()

	var result *gorm.DB
	err := db.RetryableComponentTransaction(s, ctx, func(tx *gorm.DB) *gorm.DB {
		result = fn(tx.WithContext(ctx))
		return result
	})

	return result, err
}

// paginatedQuery handles the common pattern for paginated queries
func (s *PricingServiceDefault) paginatedQuery(ctx context.Context, methodName string, model any, filters []queryutil.CrudFilter, sorts []queryutil.Sort, pagination queryutil.Pagination, results any, countErrorHandler func(error)) int64 {
	ctx, span := core.TraceMethod(ctx, "PricingServiceDefault."+methodName)
	defer span.End()

	var total int64
	_ = db.RetryableComponentTransaction(s, ctx, func(tx *gorm.DB) *gorm.DB {
		q := tx.Model(model)

		q = queryutil.ApplyFilters(q, filters, nil)
		q = queryutil.ApplySort(q, sorts)

		if err := q.Count(&total).Error; err != nil && countErrorHandler != nil {
			countErrorHandler(err)
		}

		q = queryutil.ApplyPagination(q, pagination)

		return q.Find(results)
	})

	return total
}

// deleteEntity handles the common delete operation pattern
func (s *PricingServiceDefault) deleteEntity(ctx context.Context, methodName string, model any, id uint) error {
	ctx, span := core.TraceMethod(ctx, "PricingServiceDefault."+methodName)
	defer span.End()

	return db.RetryableComponentTransaction(s, ctx, func(tx *gorm.DB) *gorm.DB {
		return tx.Delete(model, id)
	})
}

// fetchPricingPlan is a helper to fetch a pricing plan by ID
func (s *PricingServiceDefault) fetchPricingPlan(ctx context.Context, planID uint) (*models.PricingPlan, error) {
	var plan models.PricingPlan
	err := s.withTracedTransaction(ctx, "fetchPricingPlan", func(tx *gorm.DB) error {
		return tx.First(&plan, planID).Error
	})

	if err != nil {
		return nil, err
	}

	return &plan, nil
}

// triggerSyncWithLogging is a helper for triggering sync with consistent logging
func (s *PricingServiceDefault) triggerSyncWithLogging(ctx context.Context, planID uint, reason string) {
	if err := triggerPlanSync(s.cronService, ctx, planID); err != nil {
		s.logger.Warn("failed to trigger plan sync",
			zap.Uint("plan_id", planID),
			zap.String("reason", reason),
			zap.Error(err))
	}
}

var _ pluginCore.PricingService = (*PricingServiceDefault)(nil)
