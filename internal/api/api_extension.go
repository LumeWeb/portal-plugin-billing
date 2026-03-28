package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gabriel-vasile/mimetype"
	"github.com/labstack/echo/v4"
	"go.lumeweb.com/httputil"
	"go.lumeweb.com/portal-middleware/auth/jwt"
	mcontext "go.lumeweb.com/portal-middleware/context"
	"go.lumeweb.com/portal-middleware/middleware"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/internal/api/dto"
	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
	router "go.lumeweb.com/portal-router"
	"go.lumeweb.com/portal/config"
	"go.lumeweb.com/portal/core"
	"go.lumeweb.com/queryutil"
	queryutilHttp "go.lumeweb.com/queryutil/http"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Read the full request body with size limit (1 MiB)
const maxWebhookPayload = 1 << 20 // 1 MiB

const defaultLogoContentType = "image/svg+xml"

// APIExtension extends the API with billing functionality
type APIExtension struct {
	*core.BaseComponent
	config         config.Manager
	db             *gorm.DB
	pricingService pluginCore.PricingService
	billingService pluginCore.BillingService
}

// NewAPIExtension creates a new API extension for billing
func NewAPIExtension() core.APIExtensionFactory {
	return func() (core.APIExtension, []core.ContextBuilderOption, error) {
		ext := &APIExtension{}

		return ext, core.ContextOptions(core.ContextWithStartupFunc(func(ctx core.Context) error {
			// Get config from context
			ext.config = ctx.Config()

			// Get and verify required services
			ext.pricingService = core.GetService[pluginCore.PricingService](ctx, pluginCore.PRICING_SERVICE)
			if ext.pricingService == nil {
				return fmt.Errorf("pricing service not available")
			}

			// Get and verify the billing service
			if ext.billingService = core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE); ext.billingService == nil {
				return fmt.Errorf("billing service not available")
			}

			return nil
		})), nil
	}
}

// TargetAPI returns the name of the API this extension targets
func (e *APIExtension) TargetAPI() string {
	return "dashboard"
}

// Configure is called to set up routes on the API router
func (e *APIExtension) Configure(gRouter router.Router, accessSvc core.AccessService) error {
	// Create middleware instances once
	authMw := middleware.AuthMiddleware(e.Context(), middleware.WithAuthPurpose(jwt.PurposeLogin))
	// Create auth middleware variant for pricing plans that allows empty authentication
	pricingAuthMw := middleware.AuthMiddleware(e.Context(), middleware.WithAuthPurpose(jwt.PurposeLogin), middleware.WithAuthEmptyAllowed(true))
	accessMw := middleware.AccessMiddleware(e.Context())

	// Define dashboard API billing routes
	dashboardRoutes := router.DefineRoutes(
		// Webhook endpoint
		router.NewRoute(http.MethodPost, "/api/account/billing/webhooks/:gatewayType", e.handleWebhook,
			router.WithSwagger(
				router.WithSummary("Process payment gateway webhook"),
				router.WithDescription("Handles incoming webhooks from payment gateways such as Stripe, PayPal, etc."),
				router.WithTags("Billing"),
				router.WithPathParam("gatewayType", "Type of payment gateway (e.g., stripe, paypal)", "stripe"),
				router.WithRequestBody(map[string]any{}, "Raw webhook payload from the payment gateway", true),
				router.WithSuccessResponse(http.StatusNoContent, "Webhook processed successfully"),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusBadRequest, "Invalid gateway type, missing signature header, or webhook processing failed"),
						router.DefineSwaggerErrorResponse(http.StatusRequestEntityTooLarge, "Payload too large"),
						router.DefineSwaggerErrorResponse(http.StatusNotFound, "Gateway not found"),
					),
				),
			),
			router.WithAccess(""),
		),
		// Subscription status endpoint
		router.NewRoute(http.MethodGet, "/api/account/billing/subscription", e.handleSubscriptionStatus,
			router.WithSwagger(
				router.WithSummary("Get subscription status"),
				router.WithDescription("Returns the subscription status for the authenticated user"),
				router.WithTags("Billing"),
				router.WithSuccessResponse(http.StatusOK, "Subscription status retrieved successfully",
					router.WithJSONContent(dto.SubscriptionStatusResponse{})),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusUnauthorized, "Authentication required"),
						router.DefineSwaggerErrorResponse(http.StatusInternalServerError, "Failed to check subscription status"),
					),
				),
			),
			router.WithAccess(core.ACCESS_USER_ROLE),
			router.WithMiddlewares(authMw, accessMw),
			router.WithCors(),
		),
		// Subscription management capabilities endpoint
		router.NewRoute(http.MethodGet, "/api/account/billing/management/capabilities", e.handleGetManagementCapabilities,
			router.WithSwagger(
				router.WithSummary("Get subscription management capabilities"),
				router.WithDescription("Returns the subscription management capabilities for the current user's gateway"),
				router.WithTags("Billing"),
				router.WithSuccessResponse(http.StatusOK, "Management capabilities retrieved successfully",
					router.WithJSONContent(dto.ManagementCapabilitiesResponse{})),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusUnauthorized, "Authentication required"),
						router.DefineSwaggerErrorResponse(http.StatusNotFound, "No active subscription found"),
						router.DefineSwaggerErrorResponse(http.StatusInternalServerError, "Failed to retrieve management capabilities"),
					),
				),
			),
			router.WithAccess(core.ACCESS_USER_ROLE),
			router.WithMiddlewares(authMw, accessMw),
			router.WithCors(),
		),
		// Subscription management operation endpoint
		router.NewRoute(http.MethodPost, "/api/account/billing/management", e.handleManagementOperation,
			router.WithSwagger(
				router.WithSummary("Get subscription management operation details"),
				router.WithDescription("Returns the action and configuration for a specific management operation"),
				router.WithTags("Billing"),
				router.WithRequestBody(dto.ManagementRequest{}, "Operation to perform", true),
				router.WithSuccessResponse(http.StatusOK, "Management operation details retrieved successfully",
					router.WithJSONContent(dto.ManagementResultResponse{})),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusUnauthorized, "Authentication required"),
						router.DefineSwaggerErrorResponse(http.StatusNotFound, "No active subscription found"),
						router.DefineSwaggerErrorResponse(http.StatusBadRequest, "Invalid request"),
						router.DefineSwaggerErrorResponse(http.StatusInternalServerError, "Failed to get management operation details"),
					),
				),
			),
			router.WithAccess(core.ACCESS_USER_ROLE),
			router.WithMiddlewares(authMw, accessMw),
			router.WithCors(),
		),
		// Predefined cancel operation endpoint
		router.NewRoute(http.MethodPost, pluginCore.CancelEndpointPath, e.handleCancelOperation,
			router.WithSwagger(
				router.WithSummary("Cancel subscription"),
				router.WithDescription("Executes the cancel operation on the current subscription. Validates that the gateway supports cancellation and returns the appropriate action"),
				router.WithTags("Billing"),
				router.WithSuccessResponse(http.StatusOK, "Cancel operation details retrieved successfully",
					router.WithJSONContent(dto.ManagementResultResponse{})),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusUnauthorized, "Authentication required"),
						router.DefineSwaggerErrorResponse(http.StatusNotFound, "No active subscription found"),
						router.DefineSwaggerErrorResponse(http.StatusBadRequest, "Cancellation is not supported by this gateway"),
						router.DefineSwaggerErrorResponse(http.StatusInternalServerError, "Failed to process cancel operation"),
					),
				),
			),
			router.WithAccess(core.ACCESS_USER_ROLE),
			router.WithMiddlewares(authMw, accessMw),
			router.WithCors(),
		),
		// Predefined change-plan operation endpoint
		router.NewRoute(http.MethodPost, pluginCore.ChangePlanEndpointPath, e.handleChangePlanOperation,
			router.WithSwagger(
				router.WithSummary("Change subscription plan"),
				router.WithDescription("Executes the change plan operation on the current subscription. Validates that the gateway supports plan changes and returns the appropriate action"),
				router.WithTags("Billing"),
				router.WithSuccessResponse(http.StatusOK, "Change plan operation details retrieved successfully",
					router.WithJSONContent(dto.ManagementResultResponse{})),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusUnauthorized, "Authentication required"),
						router.DefineSwaggerErrorResponse(http.StatusNotFound, "No active subscription found"),
						router.DefineSwaggerErrorResponse(http.StatusBadRequest, "Plan change is not supported by this gateway"),
						router.DefineSwaggerErrorResponse(http.StatusInternalServerError, "Failed to process change plan operation"),
					),
				),
			),
			router.WithAccess(core.ACCESS_USER_ROLE),
			router.WithMiddlewares(authMw, accessMw),
			router.WithCors(),
		),
		// Checkout UI endpoint
		router.NewRoute(http.MethodGet, "/api/account/billing/checkout/ui/:planId", e.handleGetCheckoutUI,
			router.WithSwagger(
				router.WithSummary("Get Checkout UI Fragments"),
				router.WithDescription(
					"Returns platform-agnostic UI fragments for checkout. " +
						"Response format varies by gateway but always contains fragments " +
						"of type link, html, script, iframe, modal, button, or form",
				),
				router.WithTags("Billing"),
				router.WithPathParam("planId", "Plan ID", "123"),
				router.WithSuccessResponse(http.StatusOK, "Checkout UI fragments retrieved",
					router.WithJSONContent(pluginCore.CheckoutUIResponse{})),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusBadRequest, "Invalid request or plan not available"),
						router.DefineSwaggerErrorResponse(http.StatusConflict, "User already has an active subscription"),
						router.DefineSwaggerErrorResponse(http.StatusInternalServerError, "Failed to get checkout UI"),
					),
				),
			),
			router.WithAccess(core.ACCESS_USER_ROLE),
			router.WithMiddlewares(authMw, accessMw),
			router.WithCors(),
		),
	)

	// Register dashboard routes
	if err := router.RegisterRoutes(gRouter, accessSvc, core.GetAPI(e.TargetAPI()).Subdomain(), dashboardRoutes); err != nil {
		return err
	}

	// Define public billing routes
	publicRoutes := router.DefineRoutes(
		router.NewRoute(http.MethodGet, "/api/billing/gateways", e.handleGetGateways,
			router.WithSwagger(
				router.WithSummary("List Available Payment Gateways"),
				router.WithDescription("Returns list of available payment gateways with metadata"),
				router.WithTags("Billing"),
				router.WithSuccessResponse(http.StatusOK, "Gateways retrieved"),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusInternalServerError, "Gateway registry not initialized"),
					),
				),
			)),
		router.NewRoute(http.MethodGet, "/api/billing/gateways/:id/logo", e.handleGetGatewayLogo,
			router.WithSwagger(
				router.WithSummary("Get Gateway Logo"),
				router.WithDescription("Returns embedded logo image for payment gateway"),
				router.WithTags("Billing"),
				router.WithPathParam("id", "Gateway identifier", "stripe"),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusNotFound, "Gateway logo not found"),
					),
				),
			)),
		router.NewRoute(http.MethodGet, "/api/billing/plans", e.handleListPricingPlans,
			router.WithSwagger(
				router.WithSummary("List Pricing Plans"),
				router.WithDescription("Returns pricing plans for user's effective price line"),
				router.WithTags("Billing"),
				router.WithSuccessResponse(http.StatusOK, "Pricing plans retrieved"),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusBadRequest, "Failed to get pricing plans"),
					),
				),
			),
			router.WithMiddlewares(pricingAuthMw),
		),
		router.NewRoute(http.MethodGet, "/api/billing/plans/:id", e.handleGetPricingPlanDetail,
			router.WithSwagger(
				router.WithSummary("Get Pricing Plan Detail"),
				router.WithDescription("Returns detailed information for a pricing plan"),
				router.WithTags("Billing"),
				router.WithPathParam("id", "Plan ID", "123"),
				router.WithSuccessResponse(http.StatusOK, "Plan details retrieved"),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusBadRequest, "Invalid plan ID"),
						router.DefineSwaggerErrorResponse(http.StatusNotFound, "Pricing plan not found"),
					),
				),
			),
			router.WithMiddlewares(pricingAuthMw),
		),
	)

	apiGroup := "billing"
	if err := router.RegisterRoutes(gRouter, accessSvc, apiGroup, publicRoutes); err != nil {
		return err
	}

	return nil
}

// ID returns the service ID
func (e *APIExtension) ID() string {
	return pluginCore.BILLING_SERVICE
}

// Config returns the config manager
func (e *APIExtension) Config() config.Manager {
	return e.config
}

// handleSubscriptionStatus returns the subscription status for the authenticated user
func (e *APIExtension) handleSubscriptionStatus(c echo.Context) error {
	ctx := httputil.Context(c)
	userID, ok := e.getUser(ctx)
	if !ok {
		return ctx.Error(NewError(ErrKeyUnauthorized, fmt.Errorf("failed to get user ID")), http.StatusUnauthorized)
	}

	// Get active subscription if any
	sub, err := e.billingService.GetActiveSubscription(c.Request().Context(), userID)
	if err != nil {
		e.Logger().Error("failed to check subscription status",
			zap.Uint("user_id", userID),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeySubscriptionCheckFailed, fmt.Errorf("failed to check subscription status")), http.StatusInternalServerError)
	}

	var responseDto dto.SubscriptionStatusResponse
	return httputil.EncodeResponse[*pluginCore.Subscriber](ctx, sub, &responseDto)
}

// handleGetCheckoutUI returns checkout UI fragments for a plan
func (e *APIExtension) handleGetCheckoutUI(c echo.Context) error {
	ctx := httputil.Context(c)
	userID, ok := e.getUser(ctx)
	if !ok {
		return ctx.Error(NewError(ErrKeyUnauthorized, fmt.Errorf("failed to get user ID")), http.StatusUnauthorized)
	}

	// Parse plan ID
	planIDParam := c.Param("planId")
	planID, err := strconv.ParseUint(planIDParam, 10, 64)
	if err != nil {
		return ctx.Error(NewError(ErrKeyInvalidPlanID, fmt.Errorf("invalid plan ID")), http.StatusBadRequest)
	}

	// Get gateway type from query param (optional)
	gatewayType := c.QueryParam("gateway")

	// Resolve gateway type (default to stripe if not specified)
	if gatewayType == "" {
		gatewayType = "stripe"
	}

	// Get checkout UI from billing service which includes validation logic
	response, err := e.billingService.GetCheckoutUI(c.Request().Context(), userID, uint(planID), gatewayType)
	if err != nil {
		e.Logger().Error("failed to get checkout UI",
			zap.Uint("user_id", userID),
			zap.Uint("plan_id", uint(planID)),
			zap.String("gateway_type", gatewayType),
			zap.Error(err))

		// Map specific errors to appropriate HTTP status codes
		if strings.Contains(err.Error(), "already has an active subscription") {
			return ctx.Error(NewError(ErrKeyCheckoutSubscriptionActive, err), http.StatusConflict)
		}

		return ctx.Error(NewError(ErrKeyCheckoutUIGenerationFailed, err), http.StatusInternalServerError)
	}

	e.Logger().Debug("checkout UI retrieved",
		zap.Uint("user_id", userID),
		zap.Uint("plan_id", uint(planID)),
		zap.String("gateway_type", gatewayType),
	)

	return c.JSON(http.StatusOK, response)
}

// handleWebhook processes incoming webhook requests from payment gateways
func (e *APIExtension) handleWebhook(c echo.Context) error {
	ctx := httputil.Context(c)

	gatewayType := c.Param("gatewayType")

	if gatewayType == "" {
		return ctx.Error(NewError(ErrKeyGatewayTypeRequired, fmt.Errorf("gateway type is required")), http.StatusBadRequest)
	}

	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, maxWebhookPayload)
	defer func() {
		if err := c.Request().Body.Close(); err != nil {
			e.Logger().Error("failed to close request body",
				zap.String("gateway", gatewayType),
				zap.Error(err))
		}
	}()

	payload, err := io.ReadAll(c.Request().Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			e.Logger().Warn("webhook payload too large",
				zap.String("gateway", gatewayType),
				zap.Int64("max_size", maxWebhookPayload))
			return ctx.Error(NewError(ErrKeyPayloadTooLarge, fmt.Errorf("payload too large")), http.StatusRequestEntityTooLarge)
		}
		e.Logger().Error("failed to read webhook payload",
			zap.String("gateway", gatewayType),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyWebhookPayloadReadFailed, fmt.Errorf("failed to read webhook")), http.StatusBadRequest)
	}

	// Get signature header name from billing service
	sigHeader, err := e.billingService.GetSignatureHeader(c.Request().Context(), gatewayType)
	if err != nil {
		if errors.Is(err, pluginCore.ErrGatewayNotFound) {
			return ctx.Error(NewError(ErrKeyGatewayNotFound, fmt.Errorf("failed to get signature header: %w", err)), http.StatusNotFound)
		}
		return ctx.Error(NewError(ErrKeySignatureHeaderFailed, fmt.Errorf("failed to get signature header: %w", err)), http.StatusBadRequest)
	}

	// Get signature from header
	signature := c.Request().Header.Get(sigHeader)
	if signature == "" {
		return ctx.Error(NewError(ErrKeyMissingSignatureHeader, fmt.Errorf("missing %s header", sigHeader)), http.StatusBadRequest)
	}

	// Process the webhook through the billing service
	if err = e.billingService.ProcessWebhook(c.Request().Context(), gatewayType, signature, payload); err != nil {
		e.Logger().Error("failed to process webhook",
			zap.String("gateway", gatewayType),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyWebhookProcessFailed, fmt.Errorf("failed to process webhook: %w", err)), http.StatusBadRequest)
	}

	return ctx.NoContent(http.StatusNoContent)
}

// handleGetGateways returns list of available payment gateways
func (e *APIExtension) handleGetGateways(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	// Get registry from billing service
	registry := e.billingService.GetRegistry(reqCtx)
	if registry == nil {
		return ctx.Error(NewError(ErrKeyGatewayRegistryNotInitialized, fmt.Errorf("gateway registry not initialized")), http.StatusInternalServerError)
	}

	// Query registry for all gateways and their metadata
	response := dto.GatewayListResponse{}

	// Get active gateways
	allGateways := registry.GetAllGateways()

	for id, gateway := range allGateways {
		response = append(response, dto.GatewayPublicInfo{
			ID:          id,
			Name:        gateway.GetName(reqCtx),
			Description: gateway.GetDescription(reqCtx),
			LogoURL:     fmt.Sprintf("/api/billing/gateways/%s/logo", id),
			IsActive:    true,
		})
	}

	return httputil.EncodeResponse(ctx, &response, &response)
}

// handleGetGatewayLogo returns embedded logo for gateway
func (e *APIExtension) handleGetGatewayLogo(c echo.Context) error {
	reqCtx := c.Request().Context()
	ctx := httputil.Context(c)
	gatewayID := c.Param("id")

	// Get gateway from billing service
	gateway, err := e.billingService.GetGateway(reqCtx, gatewayID)
	if err != nil {
		return ctx.Error(NewError(ErrKeyGatewayNotFound, fmt.Errorf("gateway %s not found: %w", gatewayID, err)), http.StatusNotFound)
	}

	// Get logo bytes from gateway
	logoData, err := gateway.GetLogo(reqCtx)
	if err != nil {
		return ctx.Error(NewError(ErrKeyGatewayLogoNotFound, fmt.Errorf("failed to get logo from gateway %s: %w", gatewayID, err)), http.StatusNotFound)
	}

	// Detect content type using mimetype
	mime := mimetype.Detect(logoData)
	contentType := defaultLogoContentType // default to SVG
	if mime != nil {
		contentType = mime.String()
	}

	c.Response().Header().Set("Content-Type", contentType)
	c.Response().Header().Set("Cache-Control", "public, max-age=86400") // Cache for 1 day
	return c.Blob(http.StatusOK, contentType, logoData)
}

// handleListPricingPlans returns pricing plans for user
func (e *APIExtension) handleListPricingPlans(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	// Get authenticated user ID from context (may be empty if not authenticated)
	userID, ok := e.getUserIDFromContext(c)

	return queryutilHttp.ProcessListRequest(
		c.Response(),
		c.Request(),
		"pricing_plans",
		func(filters []queryutil.CrudFilter, sorts []queryutil.Sort, pagination queryutil.Pagination) ([]*models.PricingPlan, int64, error) {
			// If user is not authenticated, return default price line plans
			if !ok || userID == 0 {
				priceLine, err := e.pricingService.GetDefaultPriceLine(reqCtx)
				if err != nil {
					e.Logger().Error("failed to get default price line", zap.Error(err))
					return nil, 0, fmt.Errorf("failed to retrieve pricing: %w", err)
				}
				plans, err := e.pricingService.GetPlansForPriceLine(reqCtx, priceLine.ID)
				if err != nil {
					e.Logger().Error("failed to get pricing plans", zap.Error(err))
					return nil, 0, fmt.Errorf("failed to retrieve plans: %w", err)
				}
				return plans, int64(len(plans)), nil
			}

			// Get effective price line for user
			priceLine, err := e.pricingService.GetEffectivePriceLineForUser(reqCtx, userID)
			if err != nil {
				e.Logger().Error("failed to get effective price line", zap.Error(err))
				return nil, 0, fmt.Errorf("failed to retrieve pricing: %w", err)
			}

			// Get pricing plans for the price line with positions
			plans, err := e.pricingService.GetPlansForPriceLine(reqCtx, priceLine.ID)
			if err != nil {
				e.Logger().Error("failed to get pricing plans", zap.Error(err))
				return nil, 0, fmt.Errorf("failed to retrieve plans: %w", err)
			}

			return plans, int64(len(plans)), nil
		},
		func(plan *models.PricingPlan) dto.PricingPlanResponse {
			var resp dto.PricingPlanResponse
			_ = resp.FromModel(plan)
			return resp
		},
	)
}

// handleGetPricingPlanDetail returns detailed plan information
func (e *APIExtension) handleGetPricingPlanDetail(c echo.Context) error {
	ctx := httputil.Context(c)
	reqCtx := ctx.Context.Request().Context()

	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return ctx.Error(NewError(ErrKeyInvalidPlanID, fmt.Errorf("invalid plan ID format: %w", err)), http.StatusBadRequest)
	}

	plan, err := e.pricingService.GetPricingPlan(reqCtx, uint(id))
	if err != nil {
		return ctx.Error(NewError(ErrKeyPricingPlanNotFound, fmt.Errorf("pricing plan with ID %d not found", id)), http.StatusNotFound)
	}

	var resp dto.PricingPlanResponse
	return httputil.EncodeResponse(ctx, plan, &resp)
}

// handleGetManagementCapabilities returns the subscription management capabilities
func (e *APIExtension) handleGetManagementCapabilities(c echo.Context) error {
	ctx := httputil.Context(c)
	userID, ok := e.getUser(ctx)
	if !ok {
		return ctx.Error(NewError(ErrKeyUnauthorized, fmt.Errorf("failed to get user ID")), http.StatusUnauthorized)
	}

	// Get active subscription to determine gateway
	sub, err := e.billingService.GetActiveSubscription(c.Request().Context(), userID)
	if err != nil {
		e.Logger().Error("failed to check subscription status",
			zap.Uint("user_id", userID),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeySubscriptionCheckFailed, fmt.Errorf("failed to check subscription status")), http.StatusInternalServerError)
	}

	if sub == nil {
		return ctx.Error(NewError(ErrKeyNoActiveSubscription, fmt.Errorf("no active subscription found")), http.StatusNotFound)
	}

	// Get the gateway for this subscription
	gateway, err := e.billingService.GetGateway(c.Request().Context(), sub.GatewayType)
	if err != nil {
		e.Logger().Error("failed to get payment gateway",
			zap.Uint("user_id", userID),
			zap.String("gateway_type", sub.GatewayType),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyPaymentGatewayFailed, fmt.Errorf("failed to get payment gateway")), http.StatusInternalServerError)
	}

	// Check if gateway implements SubscriptionManager
	manager, ok := gateway.(pluginCore.SubscriptionManager)
	if !ok {
		return ctx.Error(NewError(ErrKeyPaymentGatewayFailed, fmt.Errorf("gateway does not support subscription management")), http.StatusInternalServerError)
	}

	// Get management capabilities
	capabilities, err := manager.GetManagementInfo(c.Request().Context(), userID)
	if err != nil {
		e.Logger().Error("failed to get management capabilities",
			zap.Uint("user_id", userID),
			zap.String("gateway_type", sub.GatewayType),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyManagementCapabilitiesFailed, fmt.Errorf("failed to get management capabilities: %w", err)), http.StatusInternalServerError)
	}

	var response dto.ManagementCapabilitiesResponse
	return httputil.EncodeResponse(ctx, capabilities, &response)
}

// handleManagementOperation returns the action and configuration for a management operation
func (e *APIExtension) handleManagementOperation(c echo.Context) error {
	ctx := httputil.Context(c)
	userID, ok := e.getUser(ctx)
	if !ok {
		return ctx.Error(NewError(ErrKeyUnauthorized, fmt.Errorf("failed to get user ID")), http.StatusUnauthorized)
	}

	// Parse and validate request body
	var request dto.ManagementRequest
	_, valid := httputil.DecodeAndValidateRequest[*dto.ManagementRequest, *dto.ManagementRequest](ctx, &request)
	if !valid {
		return nil // Error handled by DecodeAndValidateRequest
	}

	// Get active subscription to determine gateway
	sub, err := e.billingService.GetActiveSubscription(c.Request().Context(), userID)
	if err != nil {
		e.Logger().Error("failed to check subscription status",
			zap.Uint("user_id", userID),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeySubscriptionCheckFailed, fmt.Errorf("failed to check subscription status")), http.StatusInternalServerError)
	}

	if sub == nil {
		return ctx.Error(NewError(ErrKeyNoActiveSubscription, fmt.Errorf("no active subscription found")), http.StatusNotFound)
	}

	// Get the gateway for this subscription
	gateway, err := e.billingService.GetGateway(c.Request().Context(), sub.GatewayType)
	if err != nil {
		e.Logger().Error("failed to get payment gateway",
			zap.Uint("user_id", userID),
			zap.String("gateway_type", sub.GatewayType),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyPaymentGatewayFailed, fmt.Errorf("failed to get payment gateway")), http.StatusInternalServerError)
	}

	// Check if gateway implements SubscriptionManager
	manager, ok := gateway.(pluginCore.SubscriptionManager)
	if !ok {
		return ctx.Error(NewError(ErrKeyPaymentGatewayFailed, fmt.Errorf("gateway does not support subscription management")), http.StatusInternalServerError)
	}

	// Get the operation from the request
	operation, err := request.GetOperation()
	if err != nil {
		return ctx.Error(NewError(ErrKeyInvalidRequest, fmt.Errorf("invalid operation: %w", err)), http.StatusBadRequest)
	}

	// Get management capabilities to check if operation is supported
	capabilities, err := manager.GetManagementInfo(c.Request().Context(), userID)
	if err != nil {
		e.Logger().Error("failed to get management capabilities",
			zap.Uint("user_id", userID),
			zap.String("gateway_type", sub.GatewayType),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyManagementCapabilitiesFailed, fmt.Errorf("failed to get management capabilities: %w", err)), http.StatusInternalServerError)
	}

	// Check if the requested operation is supported
	supported, exists := capabilities.Operations[*operation]
	if !exists || !supported {
		return ctx.Error(NewError(ErrKeyManagementOperationFailed,
			fmt.Errorf("%s is not supported by this gateway", *operation)), http.StatusBadRequest)
	}

	// Get management result
	result, err := manager.GetManagementURL(c.Request().Context(), userID, *operation)
	if err != nil {
		e.Logger().Error("failed to get management operation details",
			zap.Uint("user_id", userID),
			zap.String("gateway_type", sub.GatewayType),
			zap.String("operation", string(*operation)),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("failed to get management operation details: %w", err)), http.StatusInternalServerError)
	}

	// Build response
	response := dto.ManagementResultResponse{}
	if err := response.FromModel(result); err != nil {
		e.Logger().Error("failed to build management operation response",
			zap.Uint("user_id", userID),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("failed to build response: %w", err)), http.StatusInternalServerError)
	}

	return httputil.EncodeResponse(ctx, result, &response)
}


// getUser extracts the user ID from the request context
func (e *APIExtension) getUser(ctx httputil.RequestContext) (uint, bool) {
	user, err := mcontext.GetUserID(ctx.Context)
	if err != nil {
		return 0, false
	}
	return user, true
}

// handleCancelOperation handles the predefined cancel operation
func (e *APIExtension) handleCancelOperation(c echo.Context) error {
	ctx := httputil.Context(c)
	userID, ok := e.getUser(ctx)
	if !ok {
		return ctx.Error(NewError(ErrKeyUnauthorized, fmt.Errorf("failed to get user ID")), http.StatusUnauthorized)
	}

	// Get active subscription to determine gateway
	sub, err := e.billingService.GetActiveSubscription(c.Request().Context(), userID)
	if err != nil {
		e.Logger().Error("failed to check subscription status",
			zap.Uint("user_id", userID),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeySubscriptionCheckFailed, fmt.Errorf("failed to check subscription status")), http.StatusInternalServerError)
	}

	if sub == nil {
		return ctx.Error(NewError(ErrKeyNoActiveSubscription, fmt.Errorf("no active subscription found")), http.StatusNotFound)
	}

	// Get the gateway for this subscription
	gateway, err := e.billingService.GetGateway(c.Request().Context(), sub.GatewayType)
	if err != nil {
		e.Logger().Error("failed to get payment gateway",
			zap.Uint("user_id", userID),
			zap.String("gateway_type", sub.GatewayType),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyPaymentGatewayFailed, fmt.Errorf("failed to get payment gateway")), http.StatusInternalServerError)
	}

	// Check if gateway implements SubscriptionManager
	manager, ok := gateway.(pluginCore.SubscriptionManager)
	if !ok {
		return ctx.Error(NewError(ErrKeyPaymentGatewayFailed, fmt.Errorf("gateway does not support subscription management")), http.StatusInternalServerError)
	}

	// Get management capabilities to check if operation is supported
	capabilities, err := manager.GetManagementInfo(c.Request().Context(), userID)
	if err != nil {
		e.Logger().Error("failed to get management capabilities",
			zap.Uint("user_id", userID),
			zap.String("gateway_type", sub.GatewayType),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyManagementCapabilitiesFailed, fmt.Errorf("failed to get management capabilities: %w", err)), http.StatusInternalServerError)
	}

	// Check if cancellation is supported
	supported, exists := capabilities.Operations[pluginCore.OperationCancel]
	if !exists || !supported {
		return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("cancellation is not supported by this gateway")), http.StatusBadRequest)
	}

	// Get management result for cancellation
	result, err := manager.GetManagementURL(c.Request().Context(), userID, pluginCore.OperationCancel)
	if err != nil {
		e.Logger().Error("failed to get cancellation operation details",
			zap.Uint("user_id", userID),
			zap.String("gateway_type", sub.GatewayType),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("failed to get cancellation operation details: %w", err)), http.StatusInternalServerError)
	}

	// For API-based gateways, execute the cancel operation directly
	if capabilities.ManagementMode == pluginCore.ModeAPI && result.Action == pluginCore.ActionAPIRequired {
		executor, ok := gateway.(pluginCore.SubscriptionExecutor)
		if !ok {
			return ctx.Error(NewError(ErrKeyPaymentGatewayFailed, fmt.Errorf("gateway does not support subscription execution")), http.StatusInternalServerError)
		}

		if err := executor.ExecuteCancel(c.Request().Context(), userID); err != nil {
			e.Logger().Error("failed to execute cancellation",
				zap.Uint("user_id", userID),
				zap.String("gateway_type", sub.GatewayType),
				zap.Error(err))
			return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("failed to cancel subscription: %w", err)), http.StatusInternalServerError)
		}

		// Return success response
		successResult := &pluginCore.ManagementResult{
			Action:              pluginCore.ActionShowUI,
			RequiresConfirmation: false,
		}
		response := dto.ManagementResultResponse{}
		if err := response.FromModel(successResult); err != nil {
			e.Logger().Error("failed to build cancellation success response",
				zap.Uint("user_id", userID),
				zap.Error(err))
			return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("failed to build response: %w", err)), http.StatusInternalServerError)
		}
		return httputil.EncodeResponse(ctx, successResult, &response)
	}

	// Build response for non-API gateways (redirect, portal, etc.)
	response := dto.ManagementResultResponse{}
	if err := response.FromModel(result); err != nil {
		e.Logger().Error("failed to build cancellation operation response",
			zap.Uint("user_id", userID),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("failed to build response: %w", err)), http.StatusInternalServerError)
	}

	return httputil.EncodeResponse(ctx, result, &response)
}

// handleChangePlanOperation handles the predefined change-plan operation
func (e *APIExtension) handleChangePlanOperation(c echo.Context) error {
	ctx := httputil.Context(c)
	userID, ok := e.getUser(ctx)
	if !ok {
		return ctx.Error(NewError(ErrKeyUnauthorized, fmt.Errorf("failed to get user ID")), http.StatusUnauthorized)
	}

	// Get active subscription to determine gateway
	sub, err := e.billingService.GetActiveSubscription(c.Request().Context(), userID)
	if err != nil {
		e.Logger().Error("failed to check subscription status",
			zap.Uint("user_id", userID),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeySubscriptionCheckFailed, fmt.Errorf("failed to check subscription status")), http.StatusInternalServerError)
	}

	if sub == nil {
		return ctx.Error(NewError(ErrKeyNoActiveSubscription, fmt.Errorf("no active subscription found")), http.StatusNotFound)
	}

	// Get the gateway for this subscription
	gateway, err := e.billingService.GetGateway(c.Request().Context(), sub.GatewayType)
	if err != nil {
		e.Logger().Error("failed to get payment gateway",
			zap.Uint("user_id", userID),
			zap.String("gateway_type", sub.GatewayType),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyPaymentGatewayFailed, fmt.Errorf("failed to get payment gateway")), http.StatusInternalServerError)
	}

	// Check if gateway implements SubscriptionManager
	manager, ok := gateway.(pluginCore.SubscriptionManager)
	if !ok {
		return ctx.Error(NewError(ErrKeyPaymentGatewayFailed, fmt.Errorf("gateway does not support subscription management")), http.StatusInternalServerError)
	}

	// Get management capabilities to check if operation is supported
	capabilities, err := manager.GetManagementInfo(c.Request().Context(), userID)
	if err != nil {
		e.Logger().Error("failed to get management capabilities",
			zap.Uint("user_id", userID),
			zap.String("gateway_type", sub.GatewayType),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyManagementCapabilitiesFailed, fmt.Errorf("failed to get management capabilities: %w", err)), http.StatusInternalServerError)
	}

	// Check if plan change is supported
	supported, exists := capabilities.Operations[pluginCore.OperationChangePlan]
	if !exists || !supported {
		return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("plan change is not supported by this gateway")), http.StatusBadRequest)
	}

	// Get management result for plan change
	result, err := manager.GetManagementURL(c.Request().Context(), userID, pluginCore.OperationChangePlan)
	if err != nil {
		e.Logger().Error("failed to get plan change operation details",
			zap.Uint("user_id", userID),
			zap.String("gateway_type", sub.GatewayType),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("failed to get plan change operation details: %w", err)), http.StatusInternalServerError)
	}

	// Build response
	response := dto.ManagementResultResponse{}
	if err := response.FromModel(result); err != nil {
		e.Logger().Error("failed to build plan change operation response",
			zap.Uint("user_id", userID),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("failed to build response: %w", err)), http.StatusInternalServerError)
	}

	return httputil.EncodeResponse(ctx, result, &response)
}

// getUserIDFromContext retrieves the authenticated user ID from the echo context
func (e *APIExtension) getUserIDFromContext(c echo.Context) (uint, bool) {
	userID, err := mcontext.GetUserID(c)
	if err != nil {
		return 0, false
	}
	return userID, true
}
