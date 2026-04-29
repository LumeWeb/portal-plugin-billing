package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"go.lumeweb.com/httputil"
	"go.lumeweb.com/portal-middleware/auth/jwt"
	"go.lumeweb.com/portal-middleware/middleware"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/internal/api/dto"
	"go.lumeweb.com/portal-plugin-billing/internal/service/pricing"
	_ "go.lumeweb.com/portal-plugin-billing/internal/api/dto"
	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/portal-plugin-billing/pkg/ledger"
	router "go.lumeweb.com/portal-router"
	"go.lumeweb.com/portal/config"
	"go.lumeweb.com/portal/core"
	"go.lumeweb.com/queryutil"
	queryutilHttp "go.lumeweb.com/queryutil/http"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// AdminExtension extends the Admin API with billing management functionality
type AdminExtension struct {
	*core.BaseComponent
	config         config.Manager
	pricingService pluginCore.PricingService
	creditService  pluginCore.CreditService
	billingService pluginCore.BillingService
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

			ext.creditService = core.GetService[pluginCore.CreditService](ctx, pluginCore.CREDIT_SERVICE)
			if ext.creditService == nil {
				return fmt.Errorf("credit service not available")
			}

			ext.billingService = core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
			if ext.billingService == nil {
				return fmt.Errorf("billing service not available")
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
	// Initialize schema provider for filter/sort support
	schemaProvider := queryutil.NewSchemaProvider()
	creditItemSchema := schemaProvider.ForType(&dto.CreditItem{})
	subscriberItemSchema := schemaProvider.ForType(&dto.SubscriberItem{})

	// Create middleware instances for admin access
	authMw := middleware.AuthMiddleware(e.Context(), middleware.WithAuthPurpose(jwt.PurposeLogin))
	accessMw := middleware.AccessMiddleware(e.Context())

	// Define admin billing routes (all require admin authentication)
	routes := router.DefineRoutes(
		router.NewRoute(http.MethodPost, "/api/billing/pricing-plans/sync-all", e.handleSyncAllPricingPlans,
			router.WithSwagger(
				router.WithSummary("Sync All Pricing Plans to Gateways"),
				router.WithDescription("Triggers synchronization of all pricing plans with payment gateways and returns results"),
				router.WithTags("Billing Admin"),
				router.WithSuccessResponse(http.StatusOK, "Sync results"),
			),
			router.WithAccess(core.ACCESS_ADMIN_ROLE),
			router.WithMiddlewares(authMw, accessMw),
		),
		router.NewRoute(http.MethodPost, "/api/billing/plans/:id/sync", e.handleSyncPricingPlan,
			router.WithSwagger(
				router.WithSummary("Sync Pricing Plan to Gateway"),
				router.WithDescription("Triggers immediate synchronization of pricing plan with payment gateway"),
				router.WithTags("Billing Admin"),
				router.WithPathParam("id", "Plan ID", "123"),
				router.WithSuccessResponse(http.StatusOK, "Sync task queued"),
			),
			router.WithAccess(core.ACCESS_ADMIN_ROLE),
			router.WithMiddlewares(authMw, accessMw),
		),
		router.NewRoute(http.MethodPost, "/api/billing/pricing-plans", e.handleCreatePricingPlan,
			router.WithSwagger(
				router.WithoutDefaultSuccessResponse(),
				router.WithSummary("Create Pricing Plan"),
				router.WithDescription("Creates a new pricing plan and queues gateway sync"),
				router.WithTags("Billing Admin"),
				router.WithRequestBody(dto.PricingPlanCreateRequest{}, "Pricing plan creation request", true),
				router.WithSuccessResponse(http.StatusCreated, "",
					router.WithJSONContent(dto.PricingPlanResponse{})),
			),
			router.WithAccess(core.ACCESS_ADMIN_ROLE),
			router.WithMiddlewares(authMw, accessMw),
		),
		router.NewRoute(http.MethodPut, "/api/billing/pricing-plans/:id", e.handleUpdatePricingPlan,
			router.WithSwagger(
				router.WithSummary("Update Pricing Plan"),
				router.WithTags("Billing Admin"),
				router.WithPathParam("id", "Pricing Plan ID", "123"),
				router.WithRequestBody(dto.PricingPlanUpdateRequest{}, "Pricing plan update request", true),
				router.WithSuccessResponse(http.StatusOK, "",
					router.WithJSONContent(dto.PricingPlanResponse{})),
			),
			router.WithAccess(core.ACCESS_ADMIN_ROLE),
			router.WithMiddlewares(authMw, accessMw),
		),
		router.NewRoute(http.MethodDelete, "/api/billing/pricing-plans/:id", e.handleDeletePricingPlan,
			router.WithSwagger(
				router.WithoutDefaultSuccessResponse(),
				router.WithSummary("Delete Pricing Plan"),
				router.WithTags("Billing Admin"),
				router.WithPathParam("id", "Pricing Plan ID", "123"),
				router.WithSuccessResponse(http.StatusNoContent, ""),
			),
			router.WithAccess(core.ACCESS_ADMIN_ROLE),
			router.WithMiddlewares(authMw, accessMw),
		),
		router.NewRoute(http.MethodGet, "/api/billing/pricing-plans", e.handleListPricingPlans,
			router.WithSwagger(
				router.WithSummary("List Pricing Plans"),
				router.WithDescription("Retrieves all pricing plans with filtering, sorting, and pagination support"),
				router.WithTags("Billing Admin"),
				router.WithSuccessResponse(http.StatusOK, "",
					router.WithJSONContent(dto.PricingPlansListResponse{})),
			),
			router.WithAccess(core.ACCESS_ADMIN_ROLE),
			router.WithMiddlewares(authMw, accessMw),
		),
		router.NewRoute(http.MethodGet, "/api/billing/pricing-plans/:id", e.handleGetPricingPlan,
			router.WithSwagger(
				router.WithSummary("Get Pricing Plan"),
				router.WithDescription("Retrieves a specific pricing plan by ID with its pricing periods"),
				router.WithTags("Billing Admin"),
				router.WithPathParam("id", "Pricing Plan ID", "123"),
				router.WithSuccessResponse(http.StatusOK, "",
					router.WithJSONContent(dto.PricingPlanResponse{})),
			),
			router.WithAccess(core.ACCESS_ADMIN_ROLE),
			router.WithMiddlewares(authMw, accessMw),
		),
		router.NewRoute(http.MethodPost, "/api/billing/price-lines", e.handleCreatePriceLine,
			router.WithSwagger(
				router.WithoutDefaultSuccessResponse(),
				router.WithSummary("Create Price Line"),
				router.WithDescription("Creates a new price line"),
				router.WithTags("Billing Admin"),
				router.WithRequestBody(dto.PriceLineCreateRequest{}, "Price line creation request", true),
				router.WithSuccessResponse(http.StatusCreated, "",
					router.WithJSONContent(dto.PriceLineResponse{})),
			),
			router.WithAccess(core.ACCESS_ADMIN_ROLE),
			router.WithMiddlewares(authMw, accessMw),
		),
		router.NewRoute(http.MethodPut, "/api/billing/price-lines/:id", e.handleUpdatePriceLine,
			router.WithSwagger(
				router.WithSummary("Update Price Line"),
				router.WithTags("Billing Admin"),
				router.WithPathParam("id", "Price Line ID", "123"),
				router.WithRequestBody(dto.PriceLineUpdateRequest{}, "Price line update request", true),
				router.WithSuccessResponse(http.StatusOK, "",
					router.WithJSONContent(dto.PriceLineResponse{})),
			),
			router.WithAccess(core.ACCESS_ADMIN_ROLE),
			router.WithMiddlewares(authMw, accessMw),
		),
		router.NewRoute(http.MethodDelete, "/api/billing/price-lines/:id", e.handleDeletePriceLine,
			router.WithSwagger(
				router.WithoutDefaultSuccessResponse(),
				router.WithSummary("Delete Price Line"),
				router.WithTags("Billing Admin"),
				router.WithPathParam("id", "Price Line ID", "123"),
				router.WithSuccessResponse(http.StatusNoContent, ""),
			),
			router.WithAccess(core.ACCESS_ADMIN_ROLE),
			router.WithMiddlewares(authMw, accessMw),
		),
		router.NewRoute(http.MethodGet, "/api/billing/price-lines", e.handleListPriceLines,
			router.WithSwagger(
				router.WithSummary("List Price Lines"),
				router.WithDescription("Retrieves all price lines with filtering, sorting, and pagination support"),
				router.WithTags("Billing Admin"),
				router.WithSuccessResponse(http.StatusOK, "",
					router.WithJSONContent(dto.PriceLinesListResponse{})),
			),
			router.WithAccess(core.ACCESS_ADMIN_ROLE),
			router.WithMiddlewares(authMw, accessMw),
		),
		router.NewRoute(http.MethodGet, "/api/billing/price-lines/:id", e.handleGetPriceLine,
			router.WithSwagger(
				router.WithSummary("Get Price Line"),
				router.WithDescription("Retrieves a specific price line by ID with its associated plans"),
				router.WithTags("Billing Admin"),
				router.WithPathParam("id", "Price Line ID", "123"),
				router.WithSuccessResponse(http.StatusOK, "",
					router.WithJSONContent(dto.PriceLineDetailResponse{})),
			),
			router.WithAccess(core.ACCESS_ADMIN_ROLE),
			router.WithMiddlewares(authMw, accessMw),
		),
		router.NewRoute(http.MethodPost, "/api/billing/price-lines/:id/plan", e.handleAddPlanToPriceLine,
			router.WithSwagger(
				router.WithoutDefaultSuccessResponse(),
				router.WithSummary("Add Plan to Price Line"),
				router.WithDescription("Adds a pricing plan to a price line with a specified position"),
				router.WithTags("Billing Admin"),
				router.WithPathParam("id", "Price Line ID", "123"),
				router.WithRequestBody(dto.AddPlanToPriceLineRequest{}, "Plan to add with position", true),
				router.WithSuccessResponse(http.StatusOK, "Plan added to price line",
					router.WithJSONContent(dto.PriceLineDetailResponse{})),
			),
			router.WithAccess(core.ACCESS_ADMIN_ROLE),
			router.WithMiddlewares(authMw, accessMw),
		),
		router.NewRoute(http.MethodPut, "/api/billing/price-lines/:id/plans/:planId", e.handleUpdatePlanPosition,
			router.WithSwagger(
				router.WithSummary("Update Plan Position"),
				router.WithDescription("Updates the position of a plan within a price line, reordering other plans as needed"),
				router.WithTags("Billing Admin"),
				router.WithPathParam("id", "Price Line ID", "123"),
				router.WithPathParam("planId", "Plan ID", "456"),
				router.WithRequestBody(dto.UpdatePlanPositionRequest{}, "New position for the plan", true),
				router.WithSuccessResponse(http.StatusOK, "Plan position updated",
					router.WithJSONContent(dto.PriceLineDetailResponse{})),
			),
			router.WithAccess(core.ACCESS_ADMIN_ROLE),
			router.WithMiddlewares(authMw, accessMw),
		),
		router.NewRoute(http.MethodDelete, "/api/billing/price-lines/:id/plans/:planId", e.handleRemovePlanFromPriceLine,
			router.WithSwagger(
				router.WithoutDefaultSuccessResponse(),
				router.WithSummary("Remove Plan from Price Line"),
				router.WithDescription("Removes a pricing plan from a price line and reorders remaining plans"),
				router.WithTags("Billing Admin"),
				router.WithPathParam("id", "Price Line ID", "123"),
				router.WithPathParam("planId", "Plan ID", "456"),
				router.WithSuccessResponse(http.StatusNoContent, "Plan removed from price line"),
			),
			router.WithAccess(core.ACCESS_ADMIN_ROLE),
			router.WithMiddlewares(authMw, accessMw),
		),
		router.NewRoute(http.MethodPost, "/api/billing/pricing-plan-periods", e.handleCreatePricingPlanPeriod,
			router.WithSwagger(
				router.WithoutDefaultSuccessResponse(),
				router.WithSummary("Create Pricing Plan Period"),
				router.WithDescription("Creates a new pricing plan period with billing cadence information"),
				router.WithTags("Billing Admin"),
				router.WithRequestBody(dto.PricingPlanPeriodCreateRequest{}, "Pricing plan period creation request", true),
				router.WithSuccessResponse(http.StatusCreated, "",
					router.WithJSONContent(dto.PricingPlanPeriodDTO{})),
			),
			router.WithAccess(core.ACCESS_ADMIN_ROLE),
			router.WithMiddlewares(authMw, accessMw),
		),
		router.NewRoute(http.MethodPut, "/api/billing/pricing-plan-periods/:id", e.handleUpdatePricingPlanPeriod,
			router.WithSwagger(
				router.WithSummary("Update Pricing Plan Period"),
				router.WithDescription("Updates an existing pricing plan period"),
				router.WithTags("Billing Admin"),
				router.WithPathParam("id", "Pricing Plan Period ID", "123"),
				router.WithRequestBody(dto.PricingPlanPeriodUpdateRequest{}, "Pricing plan period update request", true),
				router.WithSuccessResponse(http.StatusOK, "",
					router.WithJSONContent(dto.PricingPlanPeriodDTO{})),
			),
			router.WithAccess(core.ACCESS_ADMIN_ROLE),
			router.WithMiddlewares(authMw, accessMw),
		),
		router.NewRoute(http.MethodDelete, "/api/billing/pricing-plan-periods/:id", e.handleDeletePricingPlanPeriod,
			router.WithSwagger(
				router.WithoutDefaultSuccessResponse(),
				router.WithSummary("Delete Pricing Plan Period"),
				router.WithDescription("Deletes a pricing plan period (soft delete)"),
				router.WithTags("Billing Admin"),
				router.WithPathParam("id", "Pricing Plan Period ID", "123"),
				router.WithSuccessResponse(http.StatusNoContent, ""),
			),
			router.WithAccess(core.ACCESS_ADMIN_ROLE),
			router.WithMiddlewares(authMw, accessMw),
		),
		router.NewRoute(http.MethodGet, "/api/billing/pricing-plan-periods", e.handleListPricingPlanPeriods,
			router.WithSwagger(
				router.WithSummary("List Pricing Plan Periods"),
				router.WithDescription("Retrieves all pricing plan periods with filtering, sorting, and pagination support"),
				router.WithTags("Billing Admin"),
				router.WithSuccessResponse(http.StatusOK, "",
					router.WithJSONContent(dto.PricingPlanPeriodsListResponse{})),
			),
			router.WithAccess(core.ACCESS_ADMIN_ROLE),
			router.WithMiddlewares(authMw, accessMw),
		),
		router.NewRoute(http.MethodGet, "/api/billing/pricing-plan-periods/:id", e.handleGetPricingPlanPeriod,
			router.WithSwagger(
				router.WithSummary("Get Pricing Plan Period"),
				router.WithDescription("Retrieves a specific pricing plan period by ID"),
				router.WithTags("Billing Admin"),
				router.WithPathParam("id", "Pricing Plan Period ID", "123"),
				router.WithSuccessResponse(http.StatusOK, "",
					router.WithJSONContent(dto.PricingPlanPeriodDTO{})),
			),
			router.WithAccess(core.ACCESS_ADMIN_ROLE),
			router.WithMiddlewares(authMw, accessMw),
		),
		router.NewRoute(http.MethodPost, "/api/billing/credits", e.handleCreateCredit,
			router.WithSwagger(
				router.WithoutDefaultSuccessResponse(),
				router.WithSummary("Create Credit"),
				router.WithDescription("Creates a new credit entry. Returns 201 for new credits or 200 if credit already exists (idempotent via reference_id)"),
				router.WithTags("Billing Admin"),
				router.WithRequestBody(dto.CreditCreateRequest{}, "Credit creation request", true),
				router.WithSuccessResponse(http.StatusCreated, "Credit created successfully",
					router.WithJSONContent(dto.CreditResponse{})),
				router.WithSuccessResponse(http.StatusOK, "Credit already exists (idempotent)",
					router.WithJSONContent(dto.CreditResponse{})),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusUnauthorized, "Authentication required"),
						router.DefineSwaggerErrorResponse(http.StatusForbidden, "Insufficient permissions"),
						router.DefineSwaggerErrorResponse(http.StatusBadRequest, "Invalid request"),
						router.DefineSwaggerErrorResponse(http.StatusInternalServerError, "Server error"),
					),
				),
			),
			router.WithAccess(core.ACCESS_ADMIN_ROLE),
			router.WithMiddlewares(authMw, accessMw),
		),
		router.NewRoute(http.MethodGet, "/api/billing/credits/:id", e.handleGetCredit,
			router.WithSwagger(
				router.WithSummary("Get Credit"),
				router.WithDescription("Retrieves a credit by ID"),
				router.WithTags("Billing Admin"),
				router.WithPathParam("id", "Credit ID", "uuid"),
				router.WithSuccessResponse(http.StatusOK, "Credit retrieved successfully",
					router.WithJSONContent(dto.CreditResponse{})),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusUnauthorized, "Authentication required"),
						router.DefineSwaggerErrorResponse(http.StatusForbidden, "Insufficient permissions"),
						router.DefineSwaggerErrorResponse(http.StatusBadRequest, "Invalid request"),
						router.DefineSwaggerErrorResponse(http.StatusNotFound, "Resource not found"),
						router.DefineSwaggerErrorResponse(http.StatusInternalServerError, "Server error"),
					),
				),
			),
			router.WithAccess(core.ACCESS_ADMIN_ROLE),
			router.WithMiddlewares(authMw, accessMw),
		),
		router.NewRoute(http.MethodGet, "/api/billing/credits", e.handleListCredits,
			router.WithSwagger(
				router.WithSummary("List Credits"),
				router.WithDescription("Retrieves all credits with filtering, sorting, and pagination support"),
				router.WithTags("Billing Admin"),
				router.WithSchema(creditItemSchema),
				router.WithFilterParamsFromSchema(creditItemSchema),
				router.WithSuccessResponse(http.StatusOK, "Credits retrieved successfully",
					router.WithJSONContent(dto.CreditsListResponse{})),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusUnauthorized, "Authentication required"),
						router.DefineSwaggerErrorResponse(http.StatusForbidden, "Insufficient permissions"),
						router.DefineSwaggerErrorResponse(http.StatusBadRequest, "Invalid request"),
						router.DefineSwaggerErrorResponse(http.StatusInternalServerError, "Server error"),
					),
				),
			),
			router.WithAccess(core.ACCESS_ADMIN_ROLE),
			router.WithMiddlewares(authMw, accessMw),
		),
		router.NewRoute(http.MethodDelete, "/api/billing/credits/:id", e.handleDeleteCredit,
			router.WithSwagger(
				router.WithoutDefaultSuccessResponse(),
				router.WithSummary("Delete Credit"),
				router.WithDescription("Soft deletes a credit entry"),
				router.WithTags("Billing Admin"),
				router.WithPathParam("id", "Credit ID", "uuid"),
				router.WithSuccessResponse(http.StatusNoContent, "Credit deleted successfully"),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusUnauthorized, "Authentication required"),
						router.DefineSwaggerErrorResponse(http.StatusForbidden, "Insufficient permissions"),
						router.DefineSwaggerErrorResponse(http.StatusBadRequest, "Invalid request"),
						router.DefineSwaggerErrorResponse(http.StatusNotFound, "Resource not found"),
						router.DefineSwaggerErrorResponse(http.StatusInternalServerError, "Server error"),
					),
				),
			),
			router.WithAccess(core.ACCESS_ADMIN_ROLE),
			router.WithMiddlewares(authMw, accessMw),
		),
		router.NewRoute(http.MethodPost, "/api/billing/credits/:id/restore", e.handleRestoreCredit,
			router.WithSwagger(
				router.WithSummary("Restore Credit"),
				router.WithDescription("Restores a soft-deleted credit"),
				router.WithTags("Billing Admin"),
				router.WithPathParam("id", "Credit ID", "uuid"),
				router.WithSuccessResponse(http.StatusOK, "Credit restored successfully",
					router.WithJSONContent(dto.CreditResponse{})),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusUnauthorized, "Authentication required"),
						router.DefineSwaggerErrorResponse(http.StatusForbidden, "Insufficient permissions"),
						router.DefineSwaggerErrorResponse(http.StatusBadRequest, "Invalid request"),
						router.DefineSwaggerErrorResponse(http.StatusNotFound, "Resource not found"),
						router.DefineSwaggerErrorResponse(http.StatusInternalServerError, "Server error"),
					),
				),
			),
			router.WithAccess(core.ACCESS_ADMIN_ROLE),
			router.WithMiddlewares(authMw, accessMw),
		),
		router.NewRoute(http.MethodGet, "/api/billing/users/:userId/deleted-credits", e.handleListDeletedCredits,
			router.WithSwagger(
				router.WithSummary("List Deleted Credits"),
				router.WithDescription("Retrieves soft-deleted credits for a user"),
				router.WithTags("Billing Admin"),
				router.WithPathParam("userId", "User ID", "123"),
				router.WithSchema(creditItemSchema),
				router.WithFilterParamsFromSchema(creditItemSchema),
				router.WithSuccessResponse(http.StatusOK, "Deleted credits retrieved successfully",
					router.WithJSONContent(dto.DeletedCreditsListResponse{})),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusUnauthorized, "Authentication required"),
						router.DefineSwaggerErrorResponse(http.StatusForbidden, "Insufficient permissions"),
						router.DefineSwaggerErrorResponse(http.StatusBadRequest, "Invalid request"),
						router.DefineSwaggerErrorResponse(http.StatusInternalServerError, "Server error"),
					),
				),
			),
			router.WithAccess(core.ACCESS_ADMIN_ROLE),
			router.WithMiddlewares(authMw, accessMw),
		),
		router.NewRoute(http.MethodGet, "/api/billing/users/:userId/balance", e.handleGetUserBalance,
			router.WithSwagger(
				router.WithSummary("Get User Balance"),
				router.WithDescription("Retrieves the current balance for a user"),
				router.WithTags("Billing Admin"),
				router.WithPathParam("userId", "User ID", "123"),
				router.WithSuccessResponse(http.StatusOK, "Balance retrieved successfully",
					router.WithJSONContent(dto.BalanceResponse{})),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusUnauthorized, "Authentication required"),
						router.DefineSwaggerErrorResponse(http.StatusForbidden, "Insufficient permissions"),
						router.DefineSwaggerErrorResponse(http.StatusBadRequest, "Invalid request"),
						router.DefineSwaggerErrorResponse(http.StatusNotFound, "Resource not found"),
						router.DefineSwaggerErrorResponse(http.StatusInternalServerError, "Server error"),
					),
				),
			),
			router.WithAccess(core.ACCESS_ADMIN_ROLE),
			router.WithMiddlewares(authMw, accessMw),
		),
		router.NewRoute(http.MethodPost, "/api/billing/credits/purge", e.handlePurgeCredits,
			router.WithSwagger(
				router.WithSummary("Purge Credits"),
				router.WithDescription("Permanently removes soft-deleted credits older than specified duration"),
				router.WithTags("Billing Admin"),
				router.WithRequestBody(dto.CreditPurgeRequest{}, "Purge request with duration", true),
				router.WithSuccessResponse(http.StatusOK, "Credits purged successfully",
					router.WithJSONContent(dto.CreditPurgeResponse{PurgedCount: 0})),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusUnauthorized, "Authentication required"),
						router.DefineSwaggerErrorResponse(http.StatusForbidden, "Insufficient permissions"),
						router.DefineSwaggerErrorResponse(http.StatusBadRequest, "Invalid request"),
						router.DefineSwaggerErrorResponse(http.StatusInternalServerError, "Server error"),
					),
				),
			),
			router.WithAccess(core.ACCESS_ADMIN_ROLE),
			router.WithMiddlewares(authMw, accessMw),
		),
		router.NewRoute(http.MethodGet, "/api/billing/subscribers", e.handleListSubscribers,
			router.WithSwagger(
				router.WithSummary("List Subscribers"),
				router.WithDescription("Retrieves all subscribers with filtering, sorting, and pagination support"),
				router.WithTags("Billing Admin"),
				router.WithSchema(subscriberItemSchema),
				router.WithFilterParamsFromSchema(subscriberItemSchema),
				router.WithSuccessResponse(http.StatusOK, "Subscribers retrieved successfully",
					router.WithJSONContent(dto.SubscribersListResponse{})),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusUnauthorized, "Authentication required"),
						router.DefineSwaggerErrorResponse(http.StatusForbidden, "Insufficient permissions"),
						router.DefineSwaggerErrorResponse(http.StatusBadRequest, "Invalid request"),
						router.DefineSwaggerErrorResponse(http.StatusInternalServerError, "Server error"),
					),
				),
			),
			router.WithAccess(core.ACCESS_ADMIN_ROLE),
			router.WithMiddlewares(authMw, accessMw),
		),
		router.NewRoute(http.MethodGet, "/api/billing/subscribers/:id", e.handleGetSubscriber,
			router.WithSwagger(
				router.WithSummary("Get Subscriber"),
				router.WithDescription("Retrieves a specific subscriber by ID"),
				router.WithTags("Billing Admin"),
				router.WithPathParam("id", "Subscriber ID", "123"),
				router.WithSuccessResponse(http.StatusOK, "Subscriber retrieved successfully",
					router.WithJSONContent(dto.SubscriberResponse{})),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusUnauthorized, "Authentication required"),
						router.DefineSwaggerErrorResponse(http.StatusForbidden, "Insufficient permissions"),
						router.DefineSwaggerErrorResponse(http.StatusBadRequest, "Invalid request"),
						router.DefineSwaggerErrorResponse(http.StatusNotFound, "Resource not found"),
						router.DefineSwaggerErrorResponse(http.StatusInternalServerError, "Server error"),
					),
				),
			),
			router.WithAccess(core.ACCESS_ADMIN_ROLE),
			router.WithMiddlewares(authMw, accessMw),
		),
		router.NewRoute(http.MethodGet, "/api/billing/users/:userId/subscribers", e.handleGetUserSubscribers,
			router.WithSwagger(
				router.WithSummary("Get User Subscribers"),
				router.WithDescription("Retrieves subscribers for a specific user across all gateways"),
				router.WithTags("Billing Admin"),
				router.WithPathParam("userId", "User ID", "123"),
				router.WithSuccessResponse(http.StatusOK, "User subscribers retrieved successfully",
					router.WithJSONContent(dto.SubscribersListResponse{})),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusUnauthorized, "Authentication required"),
						router.DefineSwaggerErrorResponse(http.StatusForbidden, "Insufficient permissions"),
						router.DefineSwaggerErrorResponse(http.StatusBadRequest, "Invalid request"),
						router.DefineSwaggerErrorResponse(http.StatusInternalServerError, "Server error"),
					),
				),
			),
			router.WithAccess(core.ACCESS_ADMIN_ROLE),
			router.WithMiddlewares(authMw, accessMw),
		),
		router.NewRoute(http.MethodPost, "/api/billing/users/:userId/subscriptions/cancel", e.handleCancelUserSubscription,
			router.WithSwagger(
				router.WithoutDefaultSuccessResponse(),
				router.WithSummary("Cancel User Subscription"),
				router.WithDescription("Cancels a user's active subscription. Supports two modes: 'gateway' (delegates to payment gateway) or 'database' (cancels locally only)"),
				router.WithTags("Billing Admin"),
				router.WithPathParam("userId", "User ID", "123"),
				router.WithRequestBody(dto.AdminCancelSubscriptionRequest{}, "Cancellation request with mode", true),
				router.WithSuccessResponse(http.StatusOK, "Subscription cancelled successfully",
					router.WithJSONContent(dto.ManagementResultResponse{})),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusUnauthorized, "Authentication required"),
						router.DefineSwaggerErrorResponse(http.StatusForbidden, "Insufficient permissions"),
						router.DefineSwaggerErrorResponse(http.StatusBadRequest, "Invalid request"),
						router.DefineSwaggerErrorResponse(http.StatusNotFound, "Resource not found"),
						router.DefineSwaggerErrorResponse(http.StatusInternalServerError, "Server error"),
					),
				),
			),
			router.WithAccess(core.ACCESS_ADMIN_ROLE),
			router.WithMiddlewares(authMw, accessMw),
		),
		router.NewRoute(http.MethodPost, "/api/billing/users/:userId/subscriptions/cancel/abort", e.handleAbortCancellation,
			router.WithSwagger(
				router.WithoutDefaultSuccessResponse(),
				router.WithSummary("Abort Scheduled Cancellation"),
				router.WithDescription("Cancels a scheduled subscription cancellation, restoring the subscription to active status"),
				router.WithTags("Billing Admin"),
				router.WithPathParam("userId", "User ID", "123"),
				router.WithSuccessResponse(http.StatusOK, "Scheduled cancellation aborted successfully",
					router.WithJSONContent(dto.ManagementResultResponse{})),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusUnauthorized, "Authentication required"),
						router.DefineSwaggerErrorResponse(http.StatusForbidden, "Insufficient permissions"),
						router.DefineSwaggerErrorResponse(http.StatusBadRequest, "Invalid request"),
						router.DefineSwaggerErrorResponse(http.StatusNotFound, "No scheduled cancellation found"),
						router.DefineSwaggerErrorResponse(http.StatusInternalServerError, "Server error"),
					),
				),
			),
			router.WithAccess(core.ACCESS_ADMIN_ROLE),
			router.WithMiddlewares(authMw, accessMw),
		),
		router.NewRoute(http.MethodPost, "/api/billing/users/:userId/subscriptions/pause", e.handlePauseUserSubscription,
			router.WithSwagger(
				router.WithoutDefaultSuccessResponse(),
				router.WithSummary("Pause User Subscription"),
				router.WithDescription("Pauses a user's active subscription through the payment gateway"),
				router.WithTags("Billing Admin"),
				router.WithPathParam("userId", "User ID", "123"),
				router.WithSuccessResponse(http.StatusOK, "Subscription paused successfully",
					router.WithJSONContent(dto.ManagementResultResponse{})),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusUnauthorized, "Authentication required"),
						router.DefineSwaggerErrorResponse(http.StatusForbidden, "Insufficient permissions"),
						router.DefineSwaggerErrorResponse(http.StatusBadRequest, "Invalid request"),
						router.DefineSwaggerErrorResponse(http.StatusNotFound, "No active subscription found"),
						router.DefineSwaggerErrorResponse(http.StatusInternalServerError, "Server error"),
					),
				),
			),
			router.WithAccess(core.ACCESS_ADMIN_ROLE),
			router.WithMiddlewares(authMw, accessMw),
		),
		router.NewRoute(http.MethodPost, "/api/billing/users/:userId/subscriptions/resume", e.handleResumeUserSubscription,
			router.WithSwagger(
				router.WithoutDefaultSuccessResponse(),
				router.WithSummary("Resume User Subscription"),
				router.WithDescription("Resumes a user's paused subscription through the payment gateway"),
				router.WithTags("Billing Admin"),
				router.WithPathParam("userId", "User ID", "123"),
				router.WithSuccessResponse(http.StatusOK, "Subscription resumed successfully",
					router.WithJSONContent(dto.ManagementResultResponse{})),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusUnauthorized, "Authentication required"),
						router.DefineSwaggerErrorResponse(http.StatusForbidden, "Insufficient permissions"),
						router.DefineSwaggerErrorResponse(http.StatusBadRequest, "Invalid request"),
						router.DefineSwaggerErrorResponse(http.StatusNotFound, "No paused subscription found"),
						router.DefineSwaggerErrorResponse(http.StatusInternalServerError, "Server error"),
					),
				),
			),
			router.WithAccess(core.ACCESS_ADMIN_ROLE),
			router.WithMiddlewares(authMw, accessMw),
		),
		router.NewRoute(http.MethodPost, "/api/billing/users/:userId/subscriptions/change-plan", e.handleChangeUserPlan,
			router.WithSwagger(
				router.WithoutDefaultSuccessResponse(),
				router.WithSummary("Change User Plan"),
				router.WithDescription("Changes a user's subscription plan"),
				router.WithTags("Billing Admin"),
				router.WithPathParam("userId", "User ID", "123"),
				router.WithRequestBody(dto.AdminChangePlanRequest{}, "Plan change request", true),
				router.WithSuccessResponse(http.StatusOK, "Plan changed successfully",
					router.WithJSONContent(dto.PlanChangeResultResponse{})),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusUnauthorized, "Authentication required"),
						router.DefineSwaggerErrorResponse(http.StatusForbidden, "Insufficient permissions"),
						router.DefineSwaggerErrorResponse(http.StatusBadRequest, "Invalid request"),
						router.DefineSwaggerErrorResponse(http.StatusNotFound, "Resource not found"),
						router.DefineSwaggerErrorResponse(http.StatusInternalServerError, "Server error"),
					),
				),
			),
			router.WithAccess(core.ACCESS_ADMIN_ROLE),
			router.WithMiddlewares(authMw, accessMw),
		),
		router.NewRoute(http.MethodGet, "/api/billing/gateways/:gatewayId/subscribers", e.handleListGatewaySubscribers,
			router.WithSwagger(
				router.WithSummary("List Gateway Subscribers"),
				router.WithDescription("Retrieves active subscribers for a specific payment gateway"),
				router.WithTags("Billing Admin"),
				router.WithPathParam("gatewayId", "Gateway ID", "stripe"),
				router.WithSuccessResponse(http.StatusOK, "Gateway subscribers retrieved successfully",
					router.WithJSONContent(dto.SubscribersListResponse{})),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusUnauthorized, "Authentication required"),
						router.DefineSwaggerErrorResponse(http.StatusForbidden, "Insufficient permissions"),
						router.DefineSwaggerErrorResponse(http.StatusBadRequest, "Invalid request"),
						router.DefineSwaggerErrorResponse(http.StatusInternalServerError, "Server error"),
					),
				),
			),
			router.WithAccess(core.ACCESS_ADMIN_ROLE),
			router.WithMiddlewares(authMw, accessMw),
		),
	)

	apiGroup := core.GetAPI(e.TargetAPI()).Subdomain()
	if err := router.RegisterRoutes(gRouter, accessSvc, apiGroup, routes); err != nil {
		return err
	}

	return nil
}

// ID returns the service ID
func (e *AdminExtension) ID() string {
	return "billing.admin_extension"
}

// handleSyncPricingPlan triggers immediate sync for a pricing plan
func (e *AdminExtension) handleSyncPricingPlan(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return ctx.Error(NewError(ErrKeyInvalidPlanID, fmt.Errorf("invalid id: %w", err)), http.StatusBadRequest)
	}

	syncManager := pricing.NewSyncManager(e.pricingService, e.billingService, e.Context())

	result, err := syncManager.SyncPricingPlan(reqCtx, uint(id))
	if err != nil {
		return ctx.Error(NewError(ErrKeyPricingPlanFetchFailed, fmt.Errorf("failed to sync plan: %w", err)), http.StatusInternalServerError)
	}

	var resp dto.PricingPlanSyncResponse
	return httputil.EncodeResponse(ctx, result, &resp)
}

// handleSyncAllPricingPlans triggers sync for all pricing plans and returns results
func (e *AdminExtension) handleSyncAllPricingPlans(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	syncManager := pricing.NewSyncManager(e.pricingService, e.billingService, e.Context())

	plans, _, err := e.pricingService.GetPricingPlans(reqCtx, 0, nil, nil, queryutil.XXLargePagination)
	if err != nil {
		return ctx.Error(NewError(ErrKeyPricingPlansSyncAllFailed, fmt.Errorf("failed to fetch plans: %w", err)), http.StatusInternalServerError)
	}

	planResults := make([]*pricing.SyncGatewayPlanResults, 0, len(plans))

	for _, plan := range plans {
		syncResult, syncErr := syncManager.SyncPricingPlan(reqCtx, plan.ID)
		if syncErr != nil {
			e.Logger().Error("failed to sync pricing plan",
				zap.Uint("plan_id", plan.ID),
				zap.Error(syncErr))
			planResults = append(planResults, &pricing.SyncGatewayPlanResults{
				PlanID:       plan.ID,
				Errors:       map[string]error{"sync": syncErr},
				FailureCount: 1,
			})
			continue
		}

		planResults = append(planResults, syncResult)
	}

	result := &dto.SyncAllResult{
		PlanResults: planResults,
	}

	var resp dto.PricingPlanSyncAllResponse
	return httputil.EncodeResponse(ctx, result, &resp)
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

	// Auto-link to price line if specified
	if req.PriceLineID != nil {
		position := 0
		if req.Position != nil {
			position = *req.Position
		} else {
			// Auto-calculate position (append to end)
			existingPlans, err := e.pricingService.GetPriceLinePlans(reqCtx, *req.PriceLineID)
			if err != nil {
				e.Logger().Warn("failed to get price line plans for position calculation",
					zap.Uint("price_line_id", *req.PriceLineID),
					zap.Error(err))
			}
			position = len(existingPlans)
		}

		if err := e.pricingService.AddPlanToPriceLine(reqCtx, *req.PriceLineID, plan.ID, position); err != nil {
			e.Logger().Warn("failed to auto-link plan to price line",
				zap.Uint("price_line_id", *req.PriceLineID),
				zap.Uint("plan_id", plan.ID),
				zap.Int("position", position),
				zap.Error(err))
			// Don't fail the request - plan was created successfully
		}
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

// handleGetPricingPlan retrieves a specific pricing plan by ID with its periods
func (e *AdminExtension) handleGetPricingPlan(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return ctx.Error(NewError(ErrKeyInvalidPlanID, fmt.Errorf("invalid id: %w", err)), http.StatusBadRequest)
	}

	// Fetch the plan with periods preloaded
	plan, err := e.pricingService.GetPricingPlan(reqCtx, uint(id))
	if err != nil {
		if errors.Is(err, pricing.ErrPricingPlanNotFound) {
			return ctx.Error(NewError(ErrKeyPricingPlanNotFound, fmt.Errorf("pricing plan with ID %d not found", id)), http.StatusNotFound)
		}
		e.Logger().Error("failed to fetch pricing plan", zap.Error(err))
		return ctx.Error(NewError(ErrKeyPricingPlanFetchFailed, fmt.Errorf("failed to fetch pricing plan: %w", err)), http.StatusInternalServerError)
	}

	var resp dto.PricingPlanResponse
	_ = resp.FromModel(plan)

	// Fetch and populate pricing periods
	periods, err := e.pricingService.GetPricingPlanPeriods(reqCtx, plan.ID)
	if err != nil {
		e.Logger().Error("failed to fetch pricing periods for plan",
			zap.Uint("plan_id", plan.ID),
			zap.Error(err))
	} else {
		resp.SetPricingPeriods(periods)
	}

	return ctx.JSON(http.StatusOK, resp)
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

			// Fetch and populate pricing periods
			periods, err := e.pricingService.GetPricingPlanPeriods(reqCtx, plan.ID)
			if err != nil {
				e.Logger().Error("failed to fetch pricing periods for plan",
					zap.Uint("plan_id", plan.ID),
					zap.Error(err))
			} else {
				resp.SetPricingPeriods(periods)
			}

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

// handleGetPriceLine retrieves a single price line by ID with its plans
func (e *AdminExtension) handleGetPriceLine(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	priceLineID, err := parsePriceLineID(c)
	if err != nil {
		return ctx.Error(NewError(ErrKeyInvalidPriceLineID, err), http.StatusBadRequest)
	}

	priceLine, err := e.pricingService.GetPriceLine(reqCtx, priceLineID)
	if err != nil {
		return ctx.Error(NewError(ErrKeyPriceLineNotFound, err), http.StatusNotFound)
	}

	// Fetch plans for the price line
	priceLinePlans, err := e.pricingService.GetPriceLinePlans(reqCtx, priceLineID)
	if err != nil {
		e.Logger().Error("failed to get price line plans", zap.Uint("price_line_id", priceLineID), zap.Error(err))
		return ctx.Error(NewError(ErrKeyPriceLinePlanListFailed, fmt.Errorf("failed to retrieve plans for price line: %w", err)), http.StatusInternalServerError)
	}

	return buildPriceLineDetailResponse(ctx, priceLine, priceLinePlans)
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

// handleAddPlanToPriceLine adds a plan to a price line
func (e *AdminExtension) handleAddPlanToPriceLine(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	priceLineID, err := parsePriceLineID(c)
	if err != nil {
		return ctx.Error(NewError(ErrKeyInvalidPriceLineID, err), http.StatusBadRequest)
	}

	// Parse and validate request body
	var req dto.AddPlanToPriceLineRequest
	if _, ok := httputil.DecodeAndValidateRequest(ctx, &req); !ok {
		return nil
	}

	// Verify price line exists
	priceLine, err := e.pricingService.GetPriceLine(reqCtx, priceLineID)
	if err != nil {
		return ctx.Error(NewError(ErrKeyPriceLineNotFound, err), http.StatusNotFound)
	}

	// Verify plan exists
	_, err = e.pricingService.GetPricingPlan(reqCtx, req.PlanID)
	if err != nil {
		return ctx.Error(NewError(ErrKeyPricingPlanNotFound, err), http.StatusNotFound)
	}

	// Add plan to price line
	position := 0
	if req.Position != nil {
		position = *req.Position
	} else {
		// Auto-calculate position (append to end)
		existingPlans, err := e.pricingService.GetPriceLinePlans(reqCtx, priceLineID)
		if err != nil {
			e.Logger().Warn("failed to get price line plans for position calculation",
				zap.Uint("price_line_id", priceLineID),
				zap.Error(err))
		}
		position = len(existingPlans)
	}
	if err := e.pricingService.AddPlanToPriceLine(reqCtx, priceLineID, req.PlanID, position); err != nil {
		e.Logger().Error("failed to add plan to price line",
			zap.Uint("price_line_id", priceLineID),
			zap.Uint("plan_id", req.PlanID),
			zap.Int("position", position),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyPriceLinePlanAddFailed, fmt.Errorf("failed to add plan to price line: %w", err)), http.StatusInternalServerError)
	}

	// Fetch updated plans
	priceLinePlans, err := e.pricingService.GetPriceLinePlans(reqCtx, priceLineID)
	if err != nil {
		e.Logger().Error("failed to get price line plans", zap.Uint("price_line_id", priceLineID), zap.Error(err))
		return ctx.Error(NewError(ErrKeyPriceLinePlanAddFailed, fmt.Errorf("failed to retrieve updated price line: %w", err)), http.StatusInternalServerError)
	}

	return buildPriceLineDetailResponse(ctx, priceLine, priceLinePlans)
}

// handleUpdatePlanPosition updates a plan's position within a price line
func (e *AdminExtension) handleUpdatePlanPosition(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	priceLineID, err := parsePriceLineID(c)
	if err != nil {
		return ctx.Error(NewError(ErrKeyInvalidPriceLineID, err), http.StatusBadRequest)
	}

	planID, err := parsePlanID(c)
	if err != nil {
		return ctx.Error(NewError(ErrKeyInvalidPlanID, err), http.StatusBadRequest)
	}

	// Parse and validate request body
	var req dto.UpdatePlanPositionRequest
	if _, ok := httputil.DecodeAndValidateRequest(ctx, &req); !ok {
		return nil
	}

	// Verify price line exists
	priceLine, err := e.pricingService.GetPriceLine(reqCtx, priceLineID)
	if err != nil {
		return ctx.Error(NewError(ErrKeyPriceLineNotFound, err), http.StatusNotFound)
	}

	// Update plan position
	if req.Position == nil {
		return ctx.Error(NewError(ErrKeyInvalidRequest, fmt.Errorf("position is required")), http.StatusBadRequest)
	}
	if err := e.pricingService.UpdatePlanPosition(reqCtx, priceLineID, planID, *req.Position); err != nil {
		e.Logger().Error("failed to update plan position",
			zap.Uint("price_line_id", priceLineID),
			zap.Uint("plan_id", planID),
			zap.Int("position", *req.Position),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyPriceLinePlanUpdateFailed, fmt.Errorf("failed to update plan position: %w", err)), http.StatusInternalServerError)
	}

	// Fetch updated plans
	priceLinePlans, err := e.pricingService.GetPriceLinePlans(reqCtx, priceLineID)
	if err != nil {
		e.Logger().Error("failed to get price line plans", zap.Uint("price_line_id", priceLineID), zap.Error(err))
		return ctx.Error(NewError(ErrKeyPriceLinePlanUpdateFailed, fmt.Errorf("failed to retrieve updated price line: %w", err)), http.StatusInternalServerError)
	}

	return buildPriceLineDetailResponse(ctx, priceLine, priceLinePlans)
}

// handleRemovePlanFromPriceLine removes a plan from a price line
func (e *AdminExtension) handleRemovePlanFromPriceLine(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	priceLineID, err := parsePriceLineID(c)
	if err != nil {
		return ctx.Error(NewError(ErrKeyInvalidPriceLineID, err), http.StatusBadRequest)
	}

	planID, err := parsePlanID(c)
	if err != nil {
		return ctx.Error(NewError(ErrKeyInvalidPlanID, err), http.StatusBadRequest)
	}

	// Remove plan from price line
	if err := e.pricingService.RemovePlanFromPriceLine(reqCtx, priceLineID, planID); err != nil {
		e.Logger().Error("failed to remove plan from price line",
			zap.Uint("price_line_id", priceLineID),
			zap.Uint("plan_id", planID),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyPriceLinePlanRemoveFailed, fmt.Errorf("failed to remove plan from price line: %w", err)), http.StatusInternalServerError)
	}

	return ctx.NoContent(http.StatusNoContent)
}

// handleCreateCredit creates a new credit entry
func (e *AdminExtension) handleCreateCredit(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	// Parse and validate request body using DTO
	var req dto.CreditCreateRequest
	if _, ok := httputil.DecodeAndValidateRequest(ctx, &req); !ok {
		return nil
	}

	// Convert DTO to ledger Credit
	amount := decimal.Zero
	if req.Amount != nil {
		parsedAmount, err := decimal.NewFromString(*req.Amount)
		if err != nil {
			return ctx.Error(NewError(ErrKeyInvalidRequest, fmt.Errorf("invalid amount format: %w", err)), http.StatusBadRequest)
		}
		amount = parsedAmount
	}
	// For idempotency, if reference_id and reference_type are provided, use IssueCreditWithIdempotency
	var credit *ledger.Credit
	var newlyCreated bool

	if req.ReferenceID != "" && req.ReferenceType != "" {
		// Check for existing credit first to determine status code
		existingCredits, err := e.creditService.GetCreditsByReference(reqCtx, req.ReferenceID, req.ReferenceType)
		if err != nil {
			e.Logger().Error("failed to check existing credits", zap.Error(err))
			return ctx.Error(NewError(ErrKeyCreditCreateFailed, fmt.Errorf("failed to check existing credits: %w", err)), http.StatusInternalServerError)
		}

		if len(existingCredits) > 0 {
			// Existing found, return it with 200
			credit = &existingCredits[0]
			newlyCreated = false
		} else {
			// Issue credit with idempotency via service layer
			if err := e.creditService.IssueCreditWithIdempotency(
				reqCtx,
				req.UserID,
				req.TransactionType,
				amount,
				req.ReferenceType,
				req.ReferenceID,
				req.Description,
				0, // createdBy: TODO Get from authenticated admin user
			); err != nil {
				e.Logger().Error("failed to create credit", zap.Error(err))
				return ctx.Error(NewError(ErrKeyCreditCreateFailed, fmt.Errorf("failed to create credit: %w", err)), http.StatusInternalServerError)
			}

			// Retrieve the created credit for response
			existingCredits, err = e.creditService.GetCreditsByReference(reqCtx, req.ReferenceID, req.ReferenceType)
			if err != nil || len(existingCredits) == 0 {
				e.Logger().Error("failed to retrieve created credit", zap.Error(err))
				return ctx.Error(NewError(ErrKeyCreditCreateFailed, fmt.Errorf("failed to retrieve created credit: %w", err)), http.StatusInternalServerError)
			}
			credit = &existingCredits[0]
			newlyCreated = true
		}
	} else {
		// Create credit without reference-based idempotency
		credit = &ledger.Credit{
			ID:            uuid.New(),
			UserID:        req.UserID,
			Amount:        amount,
			Type:          req.TransactionType,
			Direction:     req.Direction,
			Description:   req.Description,
			ReferenceID:   req.ReferenceID,
			ReferenceType: req.ReferenceType,
			CreatedBy:     0, // TODO: Get from authenticated admin user
		}

		if err := e.creditService.CreateCredit(reqCtx, credit); err != nil {
			e.Logger().Error("failed to create credit", zap.Error(err))
			return ctx.Error(NewError(ErrKeyCreditCreateFailed, fmt.Errorf("failed to create credit: %w", err)), http.StatusInternalServerError)
		}
		newlyCreated = true
	}

	// Set status code based on whether credit was newly created
	statusCode := http.StatusCreated
	if !newlyCreated {
		statusCode = http.StatusOK
	}
	ctx.Response().Before(func() {
		ctx.Response().Status = statusCode
	})

	// ConvertCredit to CreditModel
	creditModel := convertCreditToModel(credit)
	var resp dto.CreditResponse
	return httputil.EncodeResponse(ctx, creditModel, &resp)
}

// handleGetCredit retrieves a credit by ID
func (e *AdminExtension) handleGetCredit(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return ctx.Error(NewError(ErrKeyInvalidIdentifier, fmt.Errorf("invalid credit id: %w", err)), http.StatusBadRequest)
	}

	credit, err := e.creditService.GetCredit(reqCtx, id)
	if err != nil {
		e.Logger().Error("failed to get credit", zap.Error(err))
		return ctx.Error(NewError(ErrKeyCreditNotFound, fmt.Errorf("credit not found: %w", err)), http.StatusNotFound)
	}

	if credit == nil {
		return ctx.Error(NewError(ErrKeyCreditNotFound, fmt.Errorf("credit not found")), http.StatusNotFound)
	}

	creditModel := convertCreditToModel(credit)
	var resp dto.CreditResponse
	return httputil.EncodeResponse(ctx, creditModel, &resp)
}

// handleListCredits lists all credits with filtering
func (e *AdminExtension) handleListCredits(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	return queryutilHttp.ProcessListRequest(
		c.Response(),
		c.Request(),
		"credits",
		func(filters []queryutil.CrudFilter, sorts []queryutil.Sort, pagination queryutil.Pagination) ([]*models.CreditModel, int64, error) {
			// Get credits from service
			credits, total, err := e.creditService.ListCredits(reqCtx, filters, sorts, pagination)
			if err != nil {
				e.Logger().Error("failed to get credits", zap.Error(err))
				return nil, 0, err
			}

			// Convert ledger.Credit to CreditModel for response
			creditModels := lo.Map(credits, func(credit ledger.Credit, _ int) *models.CreditModel {
				return convertCreditToModel(&credit)
			})

			return creditModels, total, nil
		},
		func(credit *models.CreditModel) dto.CreditResponse {
			var resp dto.CreditResponse
			_ = resp.FromModel(credit)
			return resp
		},
	)
}

// handleDeleteCredit soft deletes a credit entry
func (e *AdminExtension) handleDeleteCredit(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return ctx.Error(NewError(ErrKeyInvalidIdentifier, fmt.Errorf("invalid credit id: %w", err)), http.StatusBadRequest)
	}

	if err := e.creditService.SoftDeleteCredit(reqCtx, id); err != nil {
		e.Logger().Error("failed to delete credit", zap.Error(err))
		return ctx.Error(NewError(ErrKeyCreditDeleteFailed, fmt.Errorf("failed to delete credit: %w", err)), http.StatusInternalServerError)
	}

	return ctx.NoContent(http.StatusNoContent)
}

// handleRestoreCredit restores a soft-deleted credit
func (e *AdminExtension) handleRestoreCredit(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return ctx.Error(NewError(ErrKeyInvalidIdentifier, fmt.Errorf("invalid credit id: %w", err)), http.StatusBadRequest)
	}

	if err := e.creditService.RestoreCredit(reqCtx, id); err != nil {
		e.Logger().Error("failed to restore credit", zap.Error(err))
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ctx.Error(NewError(ErrKeyCreditNotFound, fmt.Errorf("credit not found: %w", err)), http.StatusNotFound)
		}
		return ctx.Error(NewError(ErrKeyCreditRestoreFailed, fmt.Errorf("failed to restore credit: %w", err)), http.StatusInternalServerError)
	}

	credit, err := e.creditService.GetCredit(reqCtx, id)
	if err != nil {
		e.Logger().Error("failed to get credit after restore", zap.Error(err))
		return ctx.Error(NewError(ErrKeyCreditNotFound, fmt.Errorf("failed to retrieve restored credit: %w", err)), http.StatusInternalServerError)
	}

	creditModel := convertCreditToModel(credit)
	var resp dto.CreditResponse
	return httputil.EncodeResponse(ctx, creditModel, &resp)
}

// handleListDeletedCredits lists soft-deleted credits for a user
func (e *AdminExtension) handleListDeletedCredits(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	userIDStr := c.Param("userId")
	userID, err := strconv.ParseUint(userIDStr, 10, 64)
	if err != nil {
		return ctx.Error(NewError(ErrKeyInvalidIdentifier, fmt.Errorf("invalid user id: %w", err)), http.StatusBadRequest)
	}

	return queryutilHttp.ProcessListRequest(
		c.Response(),
		c.Request(),
		"deleted_credits",
		func(filters []queryutil.CrudFilter, sorts []queryutil.Sort, pagination queryutil.Pagination) ([]*models.CreditModel, int64, error) {
			userFilter := queryutil.FieldEqual("user_id", userID)
			filters = append([]queryutil.CrudFilter{userFilter}, filters...)

			credits, total, err := e.creditService.ListCredits(reqCtx, filters, sorts, pagination, pluginCore.WithIncludeDeleted(true))
			if err != nil {
				return nil, 0, err
			}

			result := make([]*models.CreditModel, len(credits))
			for i, credit := range credits {
				model := convertCreditToModel(&credit)
				result[i] = model
			}
			return result, total, nil
		},
		func(credit *models.CreditModel) dto.CreditItem {
			return dto.CreditItem{
				ID:              credit.ID,
				UserID:          credit.UserID,
				Amount:          credit.Amount,
				TransactionType: credit.Type,
				Direction:       credit.Direction,
				Description:     credit.Description,
				CreatedBy:       credit.CreatedBy,
				CreatedAt:       credit.CreatedAt,
				UpdatedAt:       credit.UpdatedAt,
				DeletedAt:       credit.DeletedAt,
			}
		},
	)
}

// handleGetUserBalance retrieves a user's current balance
func (e *AdminExtension) handleGetUserBalance(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	userIDStr := c.Param("userId")
	userID, err := strconv.ParseUint(userIDStr, 10, 64)
	if err != nil {
		return ctx.Error(NewError(ErrKeyInvalidIdentifier, fmt.Errorf("invalid user id: %w", err)), http.StatusBadRequest)
	}

	balance, err := e.creditService.GetUserBalance(reqCtx, userID)
	if err != nil {
		e.Logger().Error("failed to get user balance", zap.Error(err))
		return ctx.Error(NewError(ErrKeyCreditNotFound, fmt.Errorf("failed to get user balance: %w", err)), http.StatusInternalServerError)
	}

	// Create balance view model for proper DTO conversion
	balanceView := &models.CreditsBalanceView{
		UserID:  userID,
		Balance: balance,
	}

	var resp dto.BalanceResponse
	return httputil.EncodeResponse(ctx, balanceView, &resp)
}

// handlePurgeCredits permanently removes soft-deleted credits older than specified duration
func (e *AdminExtension) handlePurgeCredits(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	// Parse and validate request body
	var req dto.CreditPurgeRequest
	if _, ok := httputil.DecodeAndValidateRequest(ctx, &req); !ok {
		return nil
	}

	// Parse duration
	duration, err := time.ParseDuration(req.OlderThan)
	if err != nil {
		e.Logger().Error("failed to parse duration", zap.Error(err))
		return ctx.Error(NewError(ErrKeyInvalidRequest, fmt.Errorf("invalid duration format: %w", err)), http.StatusBadRequest)
	}

	count, err := e.creditService.PurgeDeletedCredits(reqCtx, duration)
	if err != nil {
		e.Logger().Error("failed to purge credits", zap.Error(err))
		return ctx.Error(NewError(ErrKeyCreditDeleteFailed, fmt.Errorf("failed to purge credits: %w", err)), http.StatusInternalServerError)
	}

	result := &dto.PurgeResult{Count: count}
	var resp dto.CreditPurgeResponse
	return httputil.EncodeResponse(ctx, result, &resp)
}

// handleCreatePricingPlanPeriod creates a new pricing plan period
func (e *AdminExtension) handleCreatePricingPlanPeriod(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	// Parse and validate request body using DTO
	var req dto.PricingPlanPeriodCreateRequest
	if _, ok := httputil.DecodeAndValidateRequest(ctx, &req); !ok {
		return nil
	}

	// Convert DTO to model
	period, err := req.ToModel()
	if err != nil {
		e.Logger().Error("failed to convert pricing plan period request", zap.Error(err))
		return ctx.Error(NewError(ErrKeyInvalidRequest, fmt.Errorf("invalid request: %w", err)), http.StatusBadRequest)
	}

	// Create period
	if err := e.pricingService.CreatePricingPlanPeriod(reqCtx, period); err != nil {
		e.Logger().Error("failed to create pricing plan period", zap.Error(err))
		return ctx.Error(NewError(ErrKeyPricingPeriodCreateFailed, fmt.Errorf("failed to create pricing plan period: %w", err)), http.StatusInternalServerError)
	}

	ctx.Response().Before(func() {
		ctx.Response().Status = http.StatusCreated
	})

	var resp dto.PricingPlanPeriodDTO
	_ = resp.FromModel(period)
	return ctx.JSON(http.StatusCreated, resp)
}

// handleUpdatePricingPlanPeriod updates an existing pricing plan period
func (e *AdminExtension) handleUpdatePricingPlanPeriod(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return ctx.Error(NewError(ErrKeyInvalidPlanID, fmt.Errorf("invalid id: %w", err)), http.StatusBadRequest)
	}

	// Parse and validate request body using DTO
	var req dto.PricingPlanPeriodUpdateRequest
	if _, ok := httputil.DecodeAndValidateRequest(ctx, &req); !ok {
		return nil
	}

	// Convert DTO to model
	period, err := req.ToModel()
	if err != nil {
		e.Logger().Error("failed to convert pricing plan period request", zap.Error(err))
		return ctx.Error(NewError(ErrKeyInvalidRequest, fmt.Errorf("invalid request: %w", err)), http.StatusBadRequest)
	}

	// Update period
	if err := e.pricingService.UpdatePricingPlanPeriod(reqCtx, uint(id), period); err != nil {
		e.Logger().Error("failed to update pricing plan period", zap.Error(err))
		return ctx.Error(NewError(ErrKeyPricingPeriodUpdateFailed, fmt.Errorf("failed to update pricing plan period: %w", err)), http.StatusInternalServerError)
	}

	var resp dto.PricingPlanPeriodDTO
	updatedPeriod, err := e.pricingService.GetPricingPlanPeriod(reqCtx, uint(id))
	if err != nil {
		return ctx.Error(NewError(ErrKeyPricingPeriodNotFound, fmt.Errorf("failed to retrieve updated period: %w", err)), http.StatusInternalServerError)
	}
	_ = resp.FromModel(updatedPeriod)
	return ctx.JSON(http.StatusOK, resp)
}

// handleDeletePricingPlanPeriod deletes a pricing plan period
func (e *AdminExtension) handleDeletePricingPlanPeriod(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return ctx.Error(NewError(ErrKeyInvalidPlanID, fmt.Errorf("invalid id: %w", err)), http.StatusBadRequest)
	}

	// Delete period
	if err := e.pricingService.DeletePricingPlanPeriod(reqCtx, uint(id)); err != nil {
		e.Logger().Error("failed to delete pricing plan period", zap.Error(err))
		return ctx.Error(NewError(ErrKeyPricingPeriodDeleteFailed, fmt.Errorf("failed to delete pricing plan period: %w", err)), http.StatusInternalServerError)
	}

	return ctx.NoContent(http.StatusNoContent)
}

// handleListPricingPlanPeriods lists all pricing plan periods with filtering and pagination
func (e *AdminExtension) handleListPricingPlanPeriods(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	return queryutilHttp.ProcessListRequest(
		c.Response(),
		c.Request(),
		"pricing_plan_periods",
		func(filters []queryutil.CrudFilter, sorts []queryutil.Sort, pagination queryutil.Pagination) ([]*models.PricingPlanPeriod, int64, error) {
			return e.pricingService.GetPricingPlanPeriodsWithFilter(reqCtx, filters, sorts, pagination)
		},
		func(period *models.PricingPlanPeriod) dto.PricingPlanPeriodDTO {
			var dto dto.PricingPlanPeriodDTO
			_ = dto.FromModel(period)
			return dto
		},
	)
}

// handleGetPricingPlanPeriod retrieves a specific pricing plan period by ID
func (e *AdminExtension) handleGetPricingPlanPeriod(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return ctx.Error(NewError(ErrKeyInvalidPlanID, fmt.Errorf("invalid id: %w", err)), http.StatusBadRequest)
	}

	period, err := e.pricingService.GetPricingPlanPeriod(reqCtx, uint(id))
	if err != nil {
		return ctx.Error(NewError(ErrKeyPricingPeriodNotFound, fmt.Errorf("pricing plan period with ID %d not found", id)), http.StatusNotFound)
	}

	var resp dto.PricingPlanPeriodDTO
	_ = resp.FromModel(period)
	return ctx.JSON(http.StatusOK, resp)
}

// handleListSubscribers lists all subscribers with filtering
func (e *AdminExtension) handleListSubscribers(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	return queryutilHttp.ProcessListRequest(
		c.Response(),
		c.Request(),
		"billing_subscribers",
		func(filters []queryutil.CrudFilter, sorts []queryutil.Sort, pagination queryutil.Pagination) ([]*models.Subscriber, int64, error) {
			subscribers, total, err := e.billingService.ListSubscribers(reqCtx, filters, sorts, pagination)
			if err != nil {
				return nil, 0, err
			}

			// Convert to []*models.Subscriber for queryutil compatibility
			result := lo.Map(subscribers, func(sub pluginCore.Subscriber, _ int) *models.Subscriber {
				s := models.Subscriber(sub)
				return &s
			})

			return result, total, nil
		},
		func(subscriber *models.Subscriber) dto.SubscriberItem {
			var resp dto.SubscriberItem
			_ = resp.FromModel((*pluginCore.Subscriber)(subscriber))
			return resp
		},
	)
}

// handleGetSubscriber retrieves a specific subscriber by ID
func (e *AdminExtension) handleGetSubscriber(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return ctx.Error(NewError(ErrKeyInvalidIdentifier, fmt.Errorf("invalid subscriber id: %w", err)), http.StatusBadRequest)
	}

	subscriber, err := e.billingService.GetSubscriberByID(reqCtx, uint(id))
	if err != nil {
		e.Logger().Error("failed to get subscriber", zap.Uint("id", uint(id)), zap.Error(err))
		return ctx.Error(NewError(ErrKeySubscriptionCheckFailed, fmt.Errorf("failed to get subscriber: %w", err)), http.StatusInternalServerError)
	}
	if subscriber == nil {
		return ctx.Error(NewError(ErrKeyNoActiveSubscription, fmt.Errorf("subscriber not found")), http.StatusNotFound)
	}

	var resp dto.SubscriberResponse
	return httputil.EncodeResponse(ctx, subscriber, &resp)
}

// handleGetUserSubscribers retrieves subscribers for a specific user
func (e *AdminExtension) handleGetUserSubscribers(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	userIDStr := c.Param("userId")
	userID, err := strconv.ParseUint(userIDStr, 10, 64)
	if err != nil {
		return ctx.Error(NewError(ErrKeyInvalidIdentifier, fmt.Errorf("invalid user id: %w", err)), http.StatusBadRequest)
	}

	return queryutilHttp.ProcessListRequest(
		c.Response(),
		c.Request(),
		"billing_subscribers",
		func(filters []queryutil.CrudFilter, sorts []queryutil.Sort, pagination queryutil.Pagination) ([]*models.Subscriber, int64, error) {
			// Get subscribers from service
			subscribers, err := e.billingService.GetSubscribersByUserID(reqCtx, uint(userID))
			if err != nil {
				e.Logger().Error("failed to get user subscribers", zap.Uint("user_id", uint(userID)), zap.Error(err))
				return nil, 0, err
			}

			// Convert to models for response
			result := lo.Map(subscribers, func(sub pluginCore.Subscriber, _ int) *models.Subscriber {
				s := models.Subscriber(sub)
				return &s
			})

			return result, int64(len(result)), nil
		},
		func(subscriber *models.Subscriber) dto.SubscriberItem {
			var resp dto.SubscriberItem
			_ = resp.FromModel((*pluginCore.Subscriber)(subscriber))
			return resp
		},
	)
}

// handleCancelUserSubscription cancels a user's active subscription
func (e *AdminExtension) handleCancelUserSubscription(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	userIDStr := c.Param("userId")
	userID, err := strconv.ParseUint(userIDStr, 10, 64)
	if err != nil {
		return ctx.Error(NewError(ErrKeyInvalidIdentifier, fmt.Errorf("invalid user id: %w", err)), http.StatusBadRequest)
	}

	// Parse request body for cancellation mode
	var request dto.AdminCancelSubscriptionRequest
	_, valid := httputil.DecodeAndValidateRequest[*dto.AdminCancelSubscriptionRequest, *dto.AdminCancelSubscriptionRequest](ctx, &request)
	if !valid {
		return nil
	}

	// Get active subscription for the user
	sub, err := e.billingService.GetActiveSubscription(reqCtx, uint(userID))
	if err != nil {
		e.Logger().Error("failed to get active subscription",
			zap.Uint("user_id", uint(userID)),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeySubscriptionCheckFailed, fmt.Errorf("failed to get active subscription: %w", err)), http.StatusInternalServerError)
	}

	if sub == nil {
		return ctx.Error(NewError(ErrKeyNoActiveSubscription, fmt.Errorf("no active subscription found")), http.StatusNotFound)
	}

	// Handle database-only cancellation
	isDatabaseMode := request.Mode != nil && *request.Mode == dto.CancellationModeDatabase
	if isDatabaseMode {
		// Cancel directly in the database
		if err := e.billingService.DeactivateSubscriber(reqCtx, uint(userID), sub.GatewayType); err != nil {
			e.Logger().Error("failed to cancel subscription in database",
				zap.Uint("user_id", uint(userID)),
				zap.String("gateway_type", sub.GatewayType),
				zap.Error(err))
			return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("failed to cancel subscription: %w", err)), http.StatusInternalServerError)
		}

		// Return success response with completed status
		now := time.Now()
		result := &pluginCore.ManagementResult{
			Action:        pluginCore.ActionAPIRequired,
			Status:        string(pluginCore.CancellationStatusCompleted),
			EffectiveTime: &now,
			CanAbort:      false,
		}
		response := dto.ManagementResultResponse{}
		if err := response.FromModel(result); err != nil {
			return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("failed to build response: %w", err)), http.StatusInternalServerError)
		}

		return httputil.EncodeResponse(ctx, result, &response)
	}

	// Handle gateway delegation (default or explicit gateway mode)
	gateway, err := e.billingService.GetGateway(reqCtx, sub.GatewayType)
	if err != nil {
		e.Logger().Error("failed to get payment gateway",
			zap.Uint("user_id", uint(userID)),
			zap.String("gateway_type", sub.GatewayType),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyPaymentGatewayFailed, fmt.Errorf("failed to get payment gateway")), http.StatusInternalServerError)
	}

	// Check if gateway implements SubscriptionManager
	manager, ok := gateway.(pluginCore.SubscriptionManager)
	if !ok {
		return ctx.Error(NewError(ErrKeyPaymentGatewayFailed, fmt.Errorf("gateway does not support subscription management")), http.StatusInternalServerError)
	}

	// Get management capabilities to check admin operations
	capabilities, err := manager.GetManagementInfo(reqCtx, uint(userID))
	if err != nil {
		e.Logger().Error("failed to get management capabilities",
			zap.Uint("user_id", uint(userID)),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyManagementCapabilitiesFailed, fmt.Errorf("failed to get management capabilities: %w", err)), http.StatusInternalServerError)
	}

	// Check if gateway supports admin backend cancellation
	supported, exists := capabilities.AdminOperations[pluginCore.OperationCancel]
	if !exists || !supported {
		return ctx.Error(NewError(ErrKeyManagementOperationFailed, 
			fmt.Errorf("gateway does not support backend cancellation; use mode='database' for local-only cancellation")), 
			http.StatusBadRequest)
	}

	// Gateway supports backend cancellation - execute it
	executor, ok := gateway.(pluginCore.CancellationExecutor)
	if !ok {
		return ctx.Error(NewError(ErrKeyPaymentGatewayFailed, fmt.Errorf("gateway reports admin cancel support but does not implement CancellationExecutor")), http.StatusInternalServerError)
	}

	// Determine if cancellation should be immediate or scheduled (default to scheduled)
	immediate := request.Immediate != nil && *request.Immediate

	cancelResult, err := executor.ExecuteCancel(reqCtx, uint(userID), immediate)
	if err != nil {
		e.Logger().Error("failed to cancel subscription",
			zap.Uint("user_id", uint(userID)),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("failed to cancel subscription: %w", err)), http.StatusInternalServerError)
	}

	result := &pluginCore.ManagementResult{
		Action:        pluginCore.ActionAPIRequired,
		Status:        string(cancelResult.Status),
		EffectiveTime: cancelResult.EffectiveAt,
		CanAbort:      cancelResult.CanAbort,
	}

	response := dto.ManagementResultResponse{}
	if err := response.FromModel(result); err != nil {
		e.Logger().Error("failed to build cancellation response",
			zap.Uint("user_id", uint(userID)),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("failed to build response: %w", err)), http.StatusInternalServerError)
	}

	return httputil.EncodeResponse(ctx, result, &response)
}

// handleChangeUserPlan changes a user's subscription plan
func (e *AdminExtension) handleChangeUserPlan(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	userIDStr := c.Param("userId")
	userID, err := strconv.ParseUint(userIDStr, 10, 64)
	if err != nil {
		return ctx.Error(NewError(ErrKeyInvalidIdentifier, fmt.Errorf("invalid user id: %w", err)), http.StatusBadRequest)
	}

	// Parse request body for period_id
	var request dto.AdminChangePlanRequest
	_, valid := httputil.DecodeAndValidateRequest[*dto.AdminChangePlanRequest, *dto.AdminChangePlanRequest](ctx, &request)
	if !valid {
		return nil
	}

	// Get active subscription for the user
	sub, err := e.billingService.GetActiveSubscription(reqCtx, uint(userID))
	if err != nil {
		e.Logger().Error("failed to get active subscription",
			zap.Uint("user_id", uint(userID)),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeySubscriptionCheckFailed, fmt.Errorf("failed to get active subscription: %w", err)), http.StatusInternalServerError)
	}

	if sub == nil {
		return ctx.Error(NewError(ErrKeyNoActiveSubscription, fmt.Errorf("no active subscription found")), http.StatusNotFound)
	}

	// Get the gateway for this subscription
	gateway, err := e.billingService.GetGateway(reqCtx, sub.GatewayType)
	if err != nil {
		e.Logger().Error("failed to get payment gateway",
			zap.Uint("user_id", uint(userID)),
			zap.String("gateway_type", sub.GatewayType),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyPaymentGatewayFailed, fmt.Errorf("failed to get payment gateway")), http.StatusInternalServerError)
	}

	// Check if gateway implements SubscriptionManager
	manager, ok := gateway.(pluginCore.SubscriptionManager)
	if !ok {
		return ctx.Error(NewError(ErrKeyPaymentGatewayFailed, fmt.Errorf("gateway does not support subscription management")), http.StatusInternalServerError)
	}

	// Get management capabilities to check admin operations
	capabilities, err := manager.GetManagementInfo(reqCtx, uint(userID))
	if err != nil {
		e.Logger().Error("failed to get management capabilities",
			zap.Uint("user_id", uint(userID)),
			zap.String("gateway_type", sub.GatewayType),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyManagementCapabilitiesFailed, fmt.Errorf("failed to get management capabilities: %w", err)), http.StatusInternalServerError)
	}

	// Check if gateway supports admin backend plan change
	supported, exists := capabilities.AdminOperations[pluginCore.OperationChangePlan]
	if !exists || !supported {
		return ctx.Error(NewError(ErrKeyManagementOperationFailed,
			fmt.Errorf("gateway does not support backend plan change; use mode='database' for local-only changes")),
			http.StatusBadRequest)
	}

	// Gateway supports backend plan change - execute it
	executor, ok := gateway.(pluginCore.PlanChangeExecutor)
	if !ok {
		return ctx.Error(NewError(ErrKeyPaymentGatewayFailed, fmt.Errorf("gateway reports admin plan change support but does not implement PlanChangeExecutor")), http.StatusInternalServerError)
	}

	result, err := executor.ExecutePlanChange(reqCtx, uint(userID), request.PeriodID)
	if err != nil {
		e.Logger().Error("failed to execute plan change",
			zap.Uint("user_id", uint(userID)),
			zap.Uint("period_id", request.PeriodID),
			zap.String("gateway_type", sub.GatewayType),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("failed to change plan: %w", err)), http.StatusInternalServerError)
	}

	response := dto.PlanChangeResultResponse{}
	if err := response.FromModel(result); err != nil {
		e.Logger().Error("failed to build plan change response",
			zap.Uint("user_id", uint(userID)),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("failed to build response: %w", err)), http.StatusInternalServerError)
	}

	return httputil.EncodeResponse(ctx, result, &response)
}

// handlePauseUserSubscription pauses a user's subscription
func (e *AdminExtension) handlePauseUserSubscription(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	userIDStr := c.Param("userId")
	userID, err := strconv.ParseUint(userIDStr, 10, 64)
	if err != nil {
		return ctx.Error(NewError(ErrKeyInvalidIdentifier, fmt.Errorf("invalid user id: %w", err)), http.StatusBadRequest)
	}

	// Get active subscription for the user
	sub, err := e.billingService.GetActiveSubscription(reqCtx, uint(userID))
	if err != nil {
		e.Logger().Error("failed to get active subscription",
			zap.Uint("user_id", uint(userID)),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeySubscriptionCheckFailed, fmt.Errorf("failed to get active subscription: %w", err)), http.StatusInternalServerError)
	}

	if sub == nil {
		return ctx.Error(NewError(ErrKeyNoActiveSubscription, fmt.Errorf("no active subscription found")), http.StatusNotFound)
	}

	// Get the gateway for this subscription
	gateway, err := e.billingService.GetGateway(reqCtx, sub.GatewayType)
	if err != nil {
		e.Logger().Error("failed to get payment gateway",
			zap.Uint("user_id", uint(userID)),
			zap.String("gateway_type", sub.GatewayType),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyPaymentGatewayFailed, fmt.Errorf("failed to get payment gateway")), http.StatusInternalServerError)
	}

	// Check if gateway implements SubscriptionManager
	manager, ok := gateway.(pluginCore.SubscriptionManager)
	if !ok {
		return ctx.Error(NewError(ErrKeyPaymentGatewayFailed, fmt.Errorf("gateway does not support subscription management")), http.StatusInternalServerError)
	}

	// Get management capabilities to check admin operations
	capabilities, err := manager.GetManagementInfo(reqCtx, uint(userID))
	if err != nil {
		e.Logger().Error("failed to get management capabilities",
			zap.Uint("user_id", uint(userID)),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyManagementCapabilitiesFailed, fmt.Errorf("failed to get management capabilities: %w", err)), http.StatusInternalServerError)
	}

	// Check if gateway supports admin backend pause
	supported, exists := capabilities.AdminOperations[pluginCore.OperationPause]
	if !exists || !supported {
		return ctx.Error(NewError(ErrKeyManagementOperationFailed,
			fmt.Errorf("gateway does not support backend pause")),
			http.StatusBadRequest)
	}

	// Gateway supports backend pause - execute it
	executor, ok := gateway.(pluginCore.PauseResumeExecutor)
	if !ok {
		return ctx.Error(NewError(ErrKeyPaymentGatewayFailed, fmt.Errorf("gateway reports admin pause support but does not implement PauseResumeExecutor")), http.StatusInternalServerError)
	}

	if err := executor.ExecutePause(reqCtx, uint(userID)); err != nil {
		e.Logger().Error("failed to pause subscription",
			zap.Uint("user_id", uint(userID)),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("failed to pause subscription: %w", err)), http.StatusInternalServerError)
	}

	result := &pluginCore.ManagementResult{
		Action: pluginCore.ActionAPIRequired,
		Status: "paused",
	}

	response := dto.ManagementResultResponse{}
	if err := response.FromModel(result); err != nil {
		e.Logger().Error("failed to build pause response",
			zap.Uint("user_id", uint(userID)),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("failed to build response: %w", err)), http.StatusInternalServerError)
	}

	return httputil.EncodeResponse(ctx, result, &response)
}

// handleResumeUserSubscription resumes a user's paused subscription
func (e *AdminExtension) handleResumeUserSubscription(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	userIDStr := c.Param("userId")
	userID, err := strconv.ParseUint(userIDStr, 10, 64)
	if err != nil {
		return ctx.Error(NewError(ErrKeyInvalidIdentifier, fmt.Errorf("invalid user id: %w", err)), http.StatusBadRequest)
	}

	// Get active subscription for the user (allows paused subscriptions)
	sub, err := e.billingService.GetActiveSubscription(reqCtx, uint(userID))
	if err != nil {
		e.Logger().Error("failed to get active subscription",
			zap.Uint("user_id", uint(userID)),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeySubscriptionCheckFailed, fmt.Errorf("failed to get active subscription: %w", err)), http.StatusInternalServerError)
	}

	if sub == nil {
		return ctx.Error(NewError(ErrKeyNoActiveSubscription, fmt.Errorf("no active subscription found")), http.StatusNotFound)
	}

	// Get the gateway for this subscription
	gateway, err := e.billingService.GetGateway(reqCtx, sub.GatewayType)
	if err != nil {
		e.Logger().Error("failed to get payment gateway",
			zap.Uint("user_id", uint(userID)),
			zap.String("gateway_type", sub.GatewayType),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyPaymentGatewayFailed, fmt.Errorf("failed to get payment gateway")), http.StatusInternalServerError)
	}

	// Check if gateway implements SubscriptionManager
	manager, ok := gateway.(pluginCore.SubscriptionManager)
	if !ok {
		return ctx.Error(NewError(ErrKeyPaymentGatewayFailed, fmt.Errorf("gateway does not support subscription management")), http.StatusInternalServerError)
	}

	// Get management capabilities to check admin operations
	capabilities, err := manager.GetManagementInfo(reqCtx, uint(userID))
	if err != nil {
		e.Logger().Error("failed to get management capabilities",
			zap.Uint("user_id", uint(userID)),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyManagementCapabilitiesFailed, fmt.Errorf("failed to get management capabilities: %w", err)), http.StatusInternalServerError)
	}

	// Check if gateway supports admin backend resume
	supported, exists := capabilities.AdminOperations[pluginCore.OperationResume]
	if !exists || !supported {
		return ctx.Error(NewError(ErrKeyManagementOperationFailed,
			fmt.Errorf("gateway does not support backend resume")),
			http.StatusBadRequest)
	}

	// Gateway supports backend resume - execute it
	executor, ok := gateway.(pluginCore.PauseResumeExecutor)
	if !ok {
		return ctx.Error(NewError(ErrKeyPaymentGatewayFailed, fmt.Errorf("gateway reports admin resume support but does not implement PauseResumeExecutor")), http.StatusInternalServerError)
	}

	if err := executor.ExecuteResume(reqCtx, uint(userID)); err != nil {
		e.Logger().Error("failed to resume subscription",
			zap.Uint("user_id", uint(userID)),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("failed to resume subscription: %w", err)), http.StatusInternalServerError)
	}

	result := &pluginCore.ManagementResult{
		Action: pluginCore.ActionAPIRequired,
		Status: "resumed",
	}

	response := dto.ManagementResultResponse{}
	if err := response.FromModel(result); err != nil {
		e.Logger().Error("failed to build resume response",
			zap.Uint("user_id", uint(userID)),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("failed to build response: %w", err)), http.StatusInternalServerError)
	}

	return httputil.EncodeResponse(ctx, result, &response)
}

// handleListGatewaySubscribers lists active subscribers for a specific gateway
func (e *AdminExtension) handleListGatewaySubscribers(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	gatewayId := c.Param("gatewayId")
	if gatewayId == "" {
		return ctx.Error(NewError(ErrKeyInvalidRequest, fmt.Errorf("gateway ID is required")), http.StatusBadRequest)
	}

	return queryutilHttp.ProcessListRequest(
		c.Response(),
		c.Request(),
		"billing_subscribers",
		func(filters []queryutil.CrudFilter, sorts []queryutil.Sort, pagination queryutil.Pagination) ([]*models.Subscriber, int64, error) {
			// Get subscribers from service
			subscribers, err := e.billingService.GetActiveSubscribersByGateway(reqCtx, gatewayId)
			if err != nil {
				e.Logger().Error("failed to get gateway subscribers",
					zap.String("gateway_id", gatewayId),
					zap.Error(err))
				return nil, 0, err
			}

			// Convert to models for response
			result := lo.Map(subscribers, func(sub pluginCore.Subscriber, _ int) *models.Subscriber {
				s := models.Subscriber(sub)
				return &s
			})

			return result, int64(len(result)), nil
		},
		func(subscriber *models.Subscriber) dto.SubscriberItem {
			var resp dto.SubscriberItem
			_ = resp.FromModel((*pluginCore.Subscriber)(subscriber))
			return resp
		},
	)
}

// handleAbortCancellation aborts a scheduled subscription cancellation
func (e *AdminExtension) handleAbortCancellation(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	userIDStr := c.Param("userId")
	userID, err := strconv.ParseUint(userIDStr, 10, 64)
	if err != nil {
		return ctx.Error(NewError(ErrKeyInvalidIdentifier, fmt.Errorf("invalid user id: %w", err)), http.StatusBadRequest)
	}

	// Get active subscription for the user
	sub, err := e.billingService.GetActiveSubscription(reqCtx, uint(userID))
	if err != nil {
		e.Logger().Error("failed to get active subscription",
			zap.Uint("user_id", uint(userID)),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeySubscriptionCheckFailed, fmt.Errorf("failed to get active subscription: %w", err)), http.StatusInternalServerError)
	}

	if sub == nil {
		return ctx.Error(NewError(ErrKeyNoActiveSubscription, fmt.Errorf("no active subscription found")), http.StatusNotFound)
	}

	// Check if there's a scheduled cancellation
	if sub.WillCancelAt == nil {
		return ctx.Error(NewError(ErrKeyNoActiveSubscription, fmt.Errorf("no scheduled cancellation found")), http.StatusNotFound)
	}

	// Get the gateway for this subscription
	gateway, err := e.billingService.GetGateway(reqCtx, sub.GatewayType)
	if err != nil {
		e.Logger().Error("failed to get payment gateway",
			zap.Uint("user_id", uint(userID)),
			zap.String("gateway_type", sub.GatewayType),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyPaymentGatewayFailed, fmt.Errorf("failed to get payment gateway")), http.StatusInternalServerError)
	}

	// Check if gateway supports admin backend cancellation via AdminOperations
	manager, ok := gateway.(pluginCore.SubscriptionManager)
	if !ok {
		return ctx.Error(NewError(ErrKeyPaymentGatewayFailed, fmt.Errorf("gateway does not support subscription management")), http.StatusInternalServerError)
	}

	capabilities, err := manager.GetManagementInfo(reqCtx, uint(userID))
	if err != nil {
		e.Logger().Error("failed to get management capabilities",
			zap.Uint("user_id", uint(userID)),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyManagementCapabilitiesFailed, fmt.Errorf("failed to get management capabilities: %w", err)), http.StatusInternalServerError)
	}

	supported, exists := capabilities.AdminOperations[pluginCore.OperationCancel]
	if !exists || !supported {
		return ctx.Error(NewError(ErrKeyManagementOperationFailed,
			fmt.Errorf("gateway does not support admin backend cancellation")), http.StatusBadRequest)
	}

	// Gateway supports admin cancellation - get executor
	executor, ok := gateway.(pluginCore.CancellationExecutor)
	if !ok {
		return ctx.Error(NewError(ErrKeyPaymentGatewayFailed, fmt.Errorf("gateway does not implement CancellationExecutor")), http.StatusInternalServerError)
	}

	// Abort the scheduled cancellation
	if err := executor.AbortCancellation(reqCtx, uint(userID)); err != nil {
		e.Logger().Error("failed to abort scheduled cancellation",
			zap.Uint("user_id", uint(userID)),
			zap.String("gateway_type", sub.GatewayType),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("failed to abort cancellation: %w", err)), http.StatusInternalServerError)
	}

	// Return success response
	result := &pluginCore.ManagementResult{
		Action:   pluginCore.ActionAPIRequired,
		Status:   string(pluginCore.CancellationStatusAborted),
		CanAbort: false,
	}
	response := dto.ManagementResultResponse{}
	if err := response.FromModel(result); err != nil {
		return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("failed to build response: %w", err)), http.StatusInternalServerError)
	}

	return httputil.EncodeResponse(ctx, result, &response)
}

// Helper methods for price line plan management

// parsePriceLineID parses and validates a price line ID from route parameter
func parsePriceLineID(c echo.Context) (uint, error) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid price line id: %w", err)
	}
	return uint(id), nil
}

// parsePlanID parses and validates a plan ID from route parameter
func parsePlanID(c echo.Context) (uint, error) {
	idStr := c.Param("planId")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid plan id: %w", err)
	}
	return uint(id), nil
}

// buildPriceLineDetailResponse builds a PriceLineDetailResponse and returns it via EncodeResponse
func buildPriceLineDetailResponse(ctx httputil.RequestContext, priceLine *models.PriceLine, priceLinePlans []*models.PriceLinePlan) error {
	var resp dto.PriceLineDetailResponse
	model := &dto.PriceLineDetail{PriceLine: priceLine, Plans: priceLinePlans}
	return httputil.EncodeResponse(ctx, model, &resp)
}
