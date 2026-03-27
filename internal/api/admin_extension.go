package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
	"go.lumeweb.com/httputil"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/internal/api/dto"
	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/portal/config"
	"go.lumeweb.com/portal/core"
	router "go.lumeweb.com/portal-router"
	"go.lumeweb.com/queryutil"
	queryutilHttp "go.lumeweb.com/queryutil/http"
	"go.uber.org/zap"
	_ "go.lumeweb.com/portal-plugin-billing/internal/api/dto"
)

// AdminExtension extends the Admin API with billing management functionality
type AdminExtension struct {
	*core.BaseComponent
	config         config.Manager
	pricingService pluginCore.PricingService
}

// NewAdminExtension creates a new Admin API extension for billing
func NewAdminExtension() core.APIExtensionFactory {
	return func() (core.APIExtension, []core.ContextBuilderOption, error) {
		ext := &AdminExtension{}

		return ext, core.ContextOptions(core.ContextWithStartupFunc(func(ctx core.Context) error {
			// Get config from context
			ext.config = ctx.Config()

			// Get and verify required services
			ext.pricingService = core.GetService[pluginCore.PricingService](ctx, pluginCore.PRICING_SERVICE)
			if ext.pricingService == nil {
				return fmt.Errorf("pricing service not available")
			}

			return nil
		})), nil
	}
}

// TargetAPI returns the name of the API this extension targets
func (e *AdminExtension) TargetAPI() string {
	return "admin"
}

// Configure is called to set up routes on the admin API router
func (e *AdminExtension) Configure(gRouter router.Router, accessSvc core.AccessService) error {
	// Define admin billing routes
	routes := router.DefineRoutes(
		router.NewRoute(http.MethodPost, "/api/billing/plans/:id/sync", e.handleSyncPricingPlan,
			router.WithSwagger(
				router.WithSummary("Sync Pricing Plan to Gateway"),
				router.WithDescription("Triggers immediate synchronization of pricing plan with payment gateway"),
				router.WithTags("Billing Admin"),
				router.WithPathParam("id", "Plan ID", "123"),
				router.WithSuccessResponse(http.StatusOK, "Sync task queued"),
			)),
		router.NewRoute(http.MethodPost, "/api/billing/pricing-plans", e.handleCreatePricingPlan,
			router.WithSwagger(
				router.WithSummary("Create Pricing Plan"),
				router.WithDescription("Creates a new pricing plan and queues gateway sync"),
				router.WithTags("Billing Admin"),
			)),
		router.NewRoute(http.MethodPut, "/api/billing/pricing-plans/:id", e.handleUpdatePricingPlan,
			router.WithSwagger(
				router.WithSummary("Update Pricing Plan"),
				router.WithTags("Billing Admin"),
			)),
		router.NewRoute(http.MethodDelete, "/api/billing/pricing-plans/:id", e.handleDeletePricingPlan,
			router.WithSwagger(
				router.WithSummary("Delete Pricing Plan"),
				router.WithTags("Billing Admin"),
			)),
		router.NewRoute(http.MethodGet, "/api/billing/pricing-plans", e.handleListPricingPlans,
			router.WithSwagger(
				router.WithSummary("List Pricing Plans"),
				router.WithDescription("Retrieves all pricing plans with filtering, sorting, and pagination support"),
				router.WithTags("Billing Admin"),
			)),
		router.NewRoute(http.MethodPost, "/api/billing/price-lines", e.handleCreatePriceLine,
			router.WithSwagger(
				router.WithSummary("Create Price Line"),
				router.WithDescription("Creates a new price line"),
				router.WithTags("Billing Admin"),
			)),
		router.NewRoute(http.MethodPut, "/api/billing/price-lines/:id", e.handleUpdatePriceLine,
			router.WithSwagger(
				router.WithSummary("Update Price Line"),
				router.WithTags("Billing Admin"),
			)),
		router.NewRoute(http.MethodDelete, "/api/billing/price-lines/:id", e.handleDeletePriceLine,
			router.WithSwagger(
				router.WithSummary("Delete Price Line"),
				router.WithTags("Billing Admin"),
			)),
		router.NewRoute(http.MethodGet, "/api/billing/price-lines", e.handleListPriceLines,
			router.WithSwagger(
				router.WithSummary("List Price Lines"),
				router.WithDescription("Retrieves all price lines with filtering, sorting, and pagination support"),
				router.WithTags("Billing Admin"),
			)),
	)

	apiGroup := "billing"
	if err := router.RegisterRoutes(gRouter, accessSvc, apiGroup, routes); err != nil {
		return err
	}

	return nil
}

// ID returns the service ID
func (e *AdminExtension) ID() string {
	return "billing.admin_extension"
}

// handleSyncPricingPlan triggers immediate sync for pricing plan
func (e *AdminExtension) handleSyncPricingPlan(c echo.Context) error {
	ctx := httputil.Context(c)

	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return ctx.Error(NewError(ErrKeyInvalidPlanID, fmt.Errorf("invalid id: %w", err)), http.StatusBadRequest)
	}

	return ctx.JSON(http.StatusAccepted, map[string]interface{}{
		"status":   "queued",
		"plan_id":  id,
		"job_type": "sync_pricing_plan",
	})
}

// handleCreatePricingPlan creates a new pricing plan
func (e *AdminExtension) handleCreatePricingPlan(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	// Parse and validate request body using DTO
	var req dto.PricingPlanCreateRequest
	if _, ok := httputil.DecodeAndValidateRequest(ctx, &req); !ok {
		return nil
	}

	// Convert DTO to model
	plan, err := req.ToModel()
	if err != nil {
		e.Logger().Error("failed to convert pricing plan request", zap.Error(err))
		return ctx.Error(NewError(ErrKeyInvalidRequest, fmt.Errorf("invalid request: %w", err)), http.StatusBadRequest)
	}

	// Create plan
	if err := e.pricingService.CreatePricingPlan(reqCtx, plan); err != nil {
		e.Logger().Error("failed to create pricing plan", zap.Error(err))
		return ctx.Error(NewError(ErrKeyPricingPlanCreateFailed, fmt.Errorf("failed to create pricing plan: %w", err)), http.StatusInternalServerError)
	}

	ctx.Response().Before(func() {
		ctx.Response().Status = http.StatusCreated
	})

	var resp dto.PricingPlanResponse
	return httputil.EncodeResponse(ctx, plan, &resp)
}

// handleUpdatePricingPlan updates a pricing plan
func (e *AdminExtension) handleUpdatePricingPlan(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return ctx.Error(NewError(ErrKeyInvalidPlanID, fmt.Errorf("invalid id: %w", err)), http.StatusBadRequest)
	}

	// Parse and validate request body using DTO
	var req dto.PricingPlanUpdateRequest
	if _, ok := httputil.DecodeAndValidateRequest(ctx, &req); !ok {
		return nil
	}

	// Convert DTO to model
	plan, err := req.ToModel()
	if err != nil {
		e.Logger().Error("failed to convert pricing plan request", zap.Error(err))
		return ctx.Error(NewError(ErrKeyInvalidRequest, fmt.Errorf("invalid request: %w", err)), http.StatusBadRequest)
	}

	// Update plan
	if err := e.pricingService.UpdatePricingPlan(reqCtx, uint(id), plan); err != nil {
		e.Logger().Error("failed to update pricing plan", zap.Error(err))
		return ctx.Error(NewError(ErrKeyPricingPlanUpdateFailed, fmt.Errorf("failed to update pricing plan: %w", err)), http.StatusInternalServerError)
	}

	var resp dto.PricingPlanResponse
	return httputil.EncodeResponse(ctx, plan, &resp)
}

// handleDeletePricingPlan deletes a pricing plan
func (e *AdminExtension) handleDeletePricingPlan(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return ctx.Error(NewError(ErrKeyInvalidPlanID, fmt.Errorf("invalid id: %w", err)), http.StatusBadRequest)
	}

	// Delete plan
	if err := e.pricingService.DeletePricingPlan(reqCtx, uint(id)); err != nil {
		e.Logger().Error("failed to delete pricing plan", zap.Error(err))
		return ctx.Error(NewError(ErrKeyPricingPlanDeleteFailed, fmt.Errorf("failed to delete pricing plan: %w", err)), http.StatusInternalServerError)
	}

	return ctx.NoContent(http.StatusNoContent)
}

// handleListPricingPlans lists all pricing plans with filtering
func (e *AdminExtension) handleListPricingPlans(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	return queryutilHttp.ProcessListRequest(
		c.Response(),
		c.Request(),
		"pricing_plans",
		func(filters []queryutil.CrudFilter, sorts []queryutil.Sort, pagination queryutil.Pagination) ([]*models.PricingPlan, int64, error) {
			return e.pricingService.GetPricingPlans(reqCtx, 0, filters, sorts, pagination)
		},
		func(plan *models.PricingPlan) dto.PricingPlanResponse {
			var resp dto.PricingPlanResponse
			_ = resp.FromModel(plan)
			return resp
		},
	)
}

// handleListPriceLines lists all price lines with filtering
func (e *AdminExtension) handleListPriceLines(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	return queryutilHttp.ProcessListRequest(
		c.Response(),
		c.Request(),
		"price_lines",
		func(filters []queryutil.CrudFilter, sorts []queryutil.Sort, pagination queryutil.Pagination) ([]*models.PriceLine, int64, error) {
			return e.pricingService.GetPriceLines(reqCtx, 0, filters, sorts, pagination)
		},
		func(line *models.PriceLine) dto.PriceLineResponse {
			var resp dto.PriceLineResponse
			_ = resp.FromModel(line)
			return resp
		},
	)
}

// handleCreatePriceLine creates a new price line
func (e *AdminExtension) handleCreatePriceLine(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	// Parse and validate request body using DTO
	var req dto.PriceLineCreateRequest
	if _, ok := httputil.DecodeAndValidateRequest(ctx, &req); !ok {
		return nil
	}

	// Convert DTO to model
	priceLine, err := req.ToModel()
	if err != nil {
		e.Logger().Error("failed to convert price line request", zap.Error(err))
		return ctx.Error(NewError(ErrKeyInvalidRequest, fmt.Errorf("invalid request: %w", err)), http.StatusBadRequest)
	}

	// Create price line
	if err := e.pricingService.CreatePriceLine(reqCtx, priceLine); err != nil {
		e.Logger().Error("failed to create price line", zap.Error(err))
		return ctx.Error(NewError(ErrKeyPriceLineCreateFailed, fmt.Errorf("failed to create price line: %w", err)), http.StatusInternalServerError)
	}

	ctx.Response().Before(func() {
		ctx.Response().Status = http.StatusCreated
	})

	var resp dto.PriceLineResponse
	return httputil.EncodeResponse(ctx, priceLine, &resp)
}

// handleUpdatePriceLine updates a price line
func (e *AdminExtension) handleUpdatePriceLine(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return ctx.Error(NewError(ErrKeyInvalidPriceLineID, fmt.Errorf("invalid id: %w", err)), http.StatusBadRequest)
	}

	// Parse and validate request body using DTO
	var req dto.PriceLineUpdateRequest
	if _, ok := httputil.DecodeAndValidateRequest(ctx, &req); !ok {
		return nil
	}

	// Convert DTO to model
	priceLine, err := req.ToModel()
	if err != nil {
		e.Logger().Error("failed to convert price line request", zap.Error(err))
		return ctx.Error(NewError(ErrKeyInvalidRequest, fmt.Errorf("invalid request: %w", err)), http.StatusBadRequest)
	}

	// Update price line
	if err := e.pricingService.UpdatePriceLine(reqCtx, uint(id), priceLine); err != nil {
		e.Logger().Error("failed to update price line", zap.Error(err))
		return ctx.Error(NewError(ErrKeyPriceLineUpdateFailed, fmt.Errorf("failed to update price line: %w", err)), http.StatusInternalServerError)
	}

	var resp dto.PriceLineResponse
	return httputil.EncodeResponse(ctx, priceLine, &resp)
}

// handleDeletePriceLine deletes a price line
func (e *AdminExtension) handleDeletePriceLine(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return ctx.Error(NewError(ErrKeyInvalidPriceLineID, fmt.Errorf("invalid id: %w", err)), http.StatusBadRequest)
	}

	// Delete price line
	if err := e.pricingService.DeletePriceLine(reqCtx, uint(id)); err != nil {
		e.Logger().Error("failed to delete price line", zap.Error(err))
		return ctx.Error(NewError(ErrKeyPriceLineDeleteFailed, fmt.Errorf("failed to delete price line: %w", err)), http.StatusInternalServerError)
	}

	return ctx.NoContent(http.StatusNoContent)
}
