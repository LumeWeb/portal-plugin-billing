package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	sseServer "github.com/apt304/sse-go/server"
	"github.com/gabriel-vasile/mimetype"
	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus"
	"go.lumeweb.com/httputil"
	"go.lumeweb.com/portal-middleware/auth/jwt"
	mcontext "go.lumeweb.com/portal-middleware/context"
	"go.lumeweb.com/portal-middleware/middleware"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/internal/api/dto"
	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
	billingEvent "go.lumeweb.com/portal-plugin-billing/internal/event"
	"go.lumeweb.com/portal-plugin-billing/internal/gateway"
	billingService "go.lumeweb.com/portal-plugin-billing/internal/service/billing"
	"go.lumeweb.com/portal-plugin-billing/internal/service/pricing"
	"go.lumeweb.com/portal-plugin-billing/internal/x402"
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
	creditService  pluginCore.CreditService
	sseServer      *sseServer.Server
	x402Handler    *x402.Handler
}

var _ core.APIExtension = (*APIExtension)(nil)

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

			// Get and verify the credit service
			if ext.creditService = core.GetService[pluginCore.CreditService](ctx, pluginCore.CREDIT_SERVICE); ext.creditService == nil {
				return fmt.Errorf("credit service not available")
			}

			// Initialize x402 handler with JWT token generator via AuthService
			nonceStore := x402.NewDBNonceStore(ctx.DB())
			userSvc := core.GetService[core.UserService](ctx, core.USER_SERVICE)
			authSvc := core.GetService[core.AuthService](ctx, core.AUTH_SERVICE)
			tokenGen := func(userID uint) (string, error) {
				return authSvc.LoginID(ctx, userID, "", false)
			}
			ext.x402Handler = x402.NewHandler(ext.billingService, ext.creditService, nonceStore, userSvc, tokenGen)

			// Initialize SSE server with apt304/sse-go
			subscriber := sseServer.NewDropOldestSubscriber(sseServer.Options{
				Buffer:            100,              // Store up to 100 events per subscriber
				HeartbeatInterval: 15 * time.Second, // Send heartbeat every 15s to keep connections alive
			})
			ext.sseServer = sseServer.NewServer(sseServer.Config{}, subscriber)

			// Register event listeners for billing events
			ext.registerBillingEventListeners(ctx)

			ctx.Logger().Info("SSE server initialized for billing plugin with heartbeat support")

			return nil
		})), nil
	}
}

// TargetAPI returns the name of the API this extension targets
func (e *APIExtension) TargetAPI() string {
	return "dashboard"
}

// Metrics returns the billing metrics that should be registered on the
// dashboard API's /metrics endpoint.
func (e *APIExtension) Metrics() []prometheus.Collector {
	return mergeBillingMetrics()
}

// mergeBillingMetrics collects metrics from all billing service packages.
func mergeBillingMetrics() []prometheus.Collector {
	return []prometheus.Collector{
		// billing service metrics
		billingService.WebhookProcessed,
		billingService.WebhookDuration,
		billingService.SubscriberCreated,
		billingService.SubscriberUpdated,
		billingService.SubscriberDeactivated,
		billingService.CheckoutUIErrors,
		// pricing sync metrics
		pricing.SyncAttempts,
		pricing.SyncSuccess,
		pricing.SyncFailures,
		pricing.SyncDuration,
		// gateway registry metrics (shared across all gateways)
		gateway.WebhookValidated,
		gateway.WebhookHandled,
		gateway.GatewayRegistered,
		// Gateway-specific metrics (stripe, atlos) are registered
		// automatically during gateway setup via the MetricsProvider
		// interface — see BillingServiceDefault.setupGateways().
	}
}

// Configure is called to set up routes on the API router
func (e *APIExtension) Configure(gRouter router.Router, accessSvc core.AccessService) error {
	// Initialize schema provider for filter/sort support
	schemaProvider := queryutil.NewSchemaProvider()
	creditItemSchema := schemaProvider.ForType(&dto.UserCreditItem{})

	// Create middleware instances once
	authMw := middleware.AuthMiddleware(e.Context(), middleware.WithAuthPurpose(jwt.PurposeLogin))
	// Create auth middleware variant for pricing plans that allows empty authentication
	pricingAuthMw := middleware.AuthMiddleware(e.Context(), middleware.WithAuthPurpose(jwt.PurposeLogin), middleware.WithAuthEmptyAllowed(true))
	accessMw := middleware.AccessMiddleware(e.Context())

	// Define dashboard API billing routes
	dashboardRoutes := router.DefineRoutes(
		// Webhook endpoint
		router.NewRoute(http.MethodPost, gateway.WebhookPathPattern, e.handleWebhook,
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
		// SSE subscription events endpoint
		router.NewRoute(http.MethodGet, "/api/account/billing/subscription/events", e.handleSubscriptionSSE,
			router.WithSwagger(
				router.WithSummary("Subscribe to subscription events via SSE"),
				router.WithDescription("Establishes a Server-Sent Events (SSE) connection for real-time subscription updates including payment completions, subscription activations, and plan changes"),
				router.WithTags("Billing"),
				router.WithSuccessResponse(http.StatusOK, "SSE connection established"),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusUnauthorized, "Authentication required"),
						router.DefineSwaggerErrorResponse(http.StatusInternalServerError, "Failed to establish SSE connection"),
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
		// Abort scheduled cancellation endpoint
		router.NewRoute(http.MethodPost, pluginCore.AbortCancelEndpointPath, e.handleAbortCancellationOperation,
			router.WithSwagger(
				router.WithSummary("Abort scheduled cancellation"),
				router.WithDescription("Cancels a scheduled subscription cancellation, restoring the subscription to active status"),
				router.WithTags("Billing"),
				router.WithSuccessResponse(http.StatusOK, "Scheduled cancellation aborted successfully",
					router.WithJSONContent(dto.ManagementResultResponse{})),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusUnauthorized, "Authentication required"),
						router.DefineSwaggerErrorResponse(http.StatusNotFound, "No active subscription or no scheduled cancellation found"),
						router.DefineSwaggerErrorResponse(http.StatusBadRequest, "Abort is not supported by this gateway"),
						router.DefineSwaggerErrorResponse(http.StatusInternalServerError, "Failed to abort cancellation"),
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
				router.WithRequestBody(dto.ChangePlanRequest{}, "Plan to change to", true),
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
		// Predefined pause operation endpoint
		router.NewRoute(http.MethodPost, pluginCore.PauseEndpointPath, e.handlePauseOperation,
			router.WithSwagger(
				router.WithSummary("Pause subscription"),
				router.WithDescription("Executes the pause operation on the current subscription. Validates that the gateway supports pausing and returns the appropriate action"),
				router.WithTags("Billing"),
				router.WithSuccessResponse(http.StatusOK, "Pause operation completed successfully",
					router.WithJSONContent(dto.ManagementResultResponse{})),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusUnauthorized, "Authentication required"),
						router.DefineSwaggerErrorResponse(http.StatusNotFound, "No active subscription found"),
						router.DefineSwaggerErrorResponse(http.StatusBadRequest, "Pause is not supported by this gateway"),
						router.DefineSwaggerErrorResponse(http.StatusInternalServerError, "Failed to process pause operation"),
					),
				),
			),
			router.WithAccess(core.ACCESS_USER_ROLE),
			router.WithMiddlewares(authMw, accessMw),
			router.WithCors(),
		),
		// Predefined resume operation endpoint
		router.NewRoute(http.MethodPost, pluginCore.ResumeEndpointPath, e.handleResumeOperation,
			router.WithSwagger(
				router.WithSummary("Resume subscription"),
				router.WithDescription("Executes the resume operation on the current subscription. Validates that the gateway supports resuming and returns the appropriate action"),
				router.WithTags("Billing"),
				router.WithSuccessResponse(http.StatusOK, "Resume operation completed successfully",
					router.WithJSONContent(dto.ManagementResultResponse{})),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusUnauthorized, "Authentication required"),
						router.DefineSwaggerErrorResponse(http.StatusNotFound, "No paused subscription found"),
						router.DefineSwaggerErrorResponse(http.StatusBadRequest, "Resume is not supported by this gateway"),
						router.DefineSwaggerErrorResponse(http.StatusInternalServerError, "Failed to process resume operation"),
					),
				),
			),
			router.WithAccess(core.ACCESS_USER_ROLE),
			router.WithMiddlewares(authMw, accessMw),
			router.WithCors(),
		),
		// Customer portal generic access
		router.NewRoute(http.MethodPost, pluginCore.CustomerPortalEndpointPath, e.handleCustomerPortal,
			router.WithSwagger(
				router.WithSummary("Access Customer Portal"),
				router.WithDescription("Returns a URL to access the generic customer portal for managing subscription"),
				router.WithTags("Billing"),
				router.WithSuccessResponse(http.StatusOK, "Customer portal URL returned successfully",
					router.WithJSONContent(dto.ManagementResultResponse{})),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusUnauthorized, "Authentication required"),
						router.DefineSwaggerErrorResponse(http.StatusNotFound, "No subscription found"),
						router.DefineSwaggerErrorResponse(http.StatusInternalServerError, "Failed to get customer portal URL"),
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
					"Returns platform-agnostic UI fragments for checkout. "+
						"Response format varies by gateway but always contains fragments "+
						"of type link, html, script, iframe, modal, button, or form",
				),
				router.WithTags("Billing"),
				router.WithPathParam("planId", "Plan ID", "123"),
				router.WithQueryParam("period_id", "Period ID for the selected pricing period", "1"),
				router.WithQueryParam("gateway", "Payment gateway type (defaults to Stripe if not specified)", "stripe"),
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
		// Checkout session status endpoint (for embedded checkout return page)
		router.NewRoute(http.MethodGet, "/api/account/billing/checkout/session/:sessionId/status", e.handleGetCheckoutSessionStatus,
			router.WithSwagger(
				router.WithSummary("Get Checkout Session Status"),
				router.WithDescription(
					"Returns the status of a checkout session. Used by embedded checkout return pages "+
						"to verify payment completion and retrieve customer information. "+
						"Returns 501 if the gateway does not support session status retrieval.",
				),
				router.WithTags("Billing"),
				router.WithPathParam("sessionId", "Checkout session ID (e.g., Stripe's cs_xxx)", "cs_test_xxx"),
				router.WithQueryParam("gateway", "Payment gateway type (defaults to Stripe if not specified)", "stripe"),
				router.WithSuccessResponse(http.StatusOK, "Session status retrieved",
					router.WithJSONContent(dto.CheckoutSessionStatusResponse{})),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusBadRequest, "Invalid session ID"),
						router.DefineSwaggerErrorResponse(http.StatusNotFound, "Session not found"),
						router.DefineSwaggerErrorResponse(http.StatusNotImplemented, "Gateway does not support session status retrieval"),
						router.DefineSwaggerErrorResponse(http.StatusInternalServerError, "Failed to get session status"),
					),
				),
			),
			router.WithAccess(core.ACCESS_USER_ROLE),
			router.WithMiddlewares(authMw, accessMw),
			router.WithCors(),
		),
		// User balance endpoint
		router.NewRoute(http.MethodGet, "/api/account/billing/balance", e.handleGetBalance,
			router.WithSwagger(
				router.WithSummary("Get Current User's Credit Balance"),
				router.WithDescription("Returns the authenticated user's current credit balance. Positive balance indicates available credits, negative balance indicates outstanding dues."),
				router.WithTags("Billing"),
				router.WithSuccessResponse(http.StatusOK, "Balance retrieved successfully",
					router.WithJSONContent(dto.BalanceResponse{})),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusUnauthorized, "Authentication required"),
						router.DefineSwaggerErrorResponse(http.StatusInternalServerError, "Failed to retrieve balance"),
					),
				),
			),
			router.WithAccess(core.ACCESS_USER_ROLE),
			router.WithMiddlewares(authMw, accessMw),
			router.WithCors(),
		),
		// User credits history endpoint
		router.NewRoute(http.MethodGet, "/api/account/billing/credits", e.handleListUserCredits,
			router.WithSwagger(
				router.WithSummary("Get Current User's Credit History"),
				router.WithDescription("Returns the authenticated user's credit transaction history with support for filtering by transaction type, direction, and date range. Results are paginated."),
				router.WithTags("Billing"),
				router.WithSchema(creditItemSchema),
				router.WithFilterParamsFromSchema(creditItemSchema),
				router.WithSuccessResponse(http.StatusOK, "Credits retrieved successfully",
					router.WithJSONContent(dto.UserCreditsListResponse{})),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusUnauthorized, "Authentication required"),
						router.DefineSwaggerErrorResponse(http.StatusInternalServerError, "Failed to retrieve credit history"),
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
				router.WithSuccessResponse(http.StatusOK, "Gateways retrieved",
					router.WithJSONContent(dto.GatewayListResponse{})),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusInternalServerError, "Gateway registry not initialized"),
					),
				),
			),
			router.WithCors(),
		),
		router.NewRoute(http.MethodGet, "/api/billing/gateways/:id/logo", e.handleGetGatewayLogo,
			router.WithSwagger(
				router.WithSummary("Get Gateway Logo"),
				router.WithDescription("Returns embedded logo image for payment gateway"),
				router.WithTags("Billing"),
				router.WithPathParam("id", "Gateway identifier", "stripe"),
				router.WithSuccessResponse(http.StatusOK, "Gateway logo retrieved"),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusNotFound, "Gateway logo not found"),
					),
				),
			),
			router.WithCors(),
		),
		router.NewRoute(http.MethodGet, "/api/billing/plans", e.handleListPricingPlans,
			router.WithSwagger(
				router.WithSummary("List Pricing Plans"),
				router.WithDescription("Returns pricing plans with their periods for user's effective price line"),
				router.WithTags("Billing"),
				router.WithSuccessResponse(http.StatusOK, "Pricing plans retrieved",
					router.WithJSONContent(dto.PublicPricingPlansListResponse{})),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusBadRequest, "Failed to get pricing plans"),
					),
				),
			),
			router.WithMiddlewares(pricingAuthMw),
			router.WithCors(),
		),
		// Public x402 credits purchase endpoint
		router.NewRoute(http.MethodPost, "/api/billing/credits/purchase", e.handleX402Checkout,
			router.WithSwagger(
				router.WithSummary("Purchase credits via x402"),
				router.WithDescription(
					"Initiates or completes an x402 crypto payment for credits. First call returns 402 Payment Required with a challenge. "+
						"Client pays via ATLOS and then calls again with PAYMENT-SIGNATURE header containing the payload.",
				),
				router.WithTags("Billing"),
				router.WithQueryParam("wallet", "Wallet address for payment", "0xAbC..."),
				router.WithQueryParam("amount", "USD amount to purchase", "5.00"),
				router.WithSuccessResponse(http.StatusOK, "Credit purchased",
					router.WithJSONContent(map[string]interface{}{})),
				router.WithSuccessResponse(http.StatusPaymentRequired, "Payment required",
					router.WithHeader("Payment-Required", "x402 challenge")),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusBadRequest, "Invalid request"),
						router.DefineSwaggerErrorResponse(http.StatusUnauthorized, "Invalid nonce or payment not confirmed"),
						router.DefineSwaggerErrorResponse(http.StatusInternalServerError, "Failed to process payment"),
					),
				),
			),
			router.WithCors(),
		),
	)

	if err := router.RegisterRoutes(gRouter, accessSvc, core.GetAPI(e.TargetAPI()).Subdomain(), publicRoutes); err != nil {
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

	// Fallback to paused subscription so the response includes PausedAt
	if sub == nil {
		sub, err = e.billingService.GetPausedSubscription(c.Request().Context(), userID)
		if err != nil {
			e.Logger().Error("failed to check paused subscription status",
				zap.Uint("user_id", userID),
				zap.Error(err))
			return ctx.Error(NewError(ErrKeySubscriptionCheckFailed, fmt.Errorf("failed to check subscription status")), http.StatusInternalServerError)
		}
	}

	var responseDto dto.SubscriptionStatusResponse
	return httputil.EncodeResponse[*pluginCore.Subscriber](ctx, sub, &responseDto)
}

// handleSubscriptionSSE establishes a Server-Sent Events connection for subscription status updates
func (e *APIExtension) handleSubscriptionSSE(c echo.Context) error {
	ctx := httputil.Context(c)
	userID, ok := e.getUser(ctx)
	if !ok {
		return ctx.Error(NewError(ErrKeyUnauthorized, fmt.Errorf("failed to get user ID")), http.StatusUnauthorized)
	}

	// Get the user-specific topic for billing events
	topic := e.getUserTopic(userID)

	// Set up lifecycle hooks for connection tracking
	hooks := sseServer.LifecycleHooks{
		OnConnect: func(sub sseServer.Subscription) {
			ctx.Logger().Debug("SSE client connected",
				zap.Uint("user_id", userID),
				zap.Strings("topics", sub.Topics))
		},
		OnDisconnect: func(sub sseServer.Subscription) {
			ctx.Logger().Debug("SSE client disconnected",
				zap.Uint("user_id", userID),
				zap.Strings("topics", sub.Topics))
		},
	}

	// Serve SSE connection using apt304/sse-go
	// This handles all SSE protocol details, heartbeats, and graceful connection management
	e.sseServer.ServeHTTP(c.Response(), c.Request(), []string{topic}, hooks)

	return nil
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

	// Parse period ID from query parameter
	periodIDParam := c.QueryParam("period_id")
	periodID, err := strconv.ParseUint(periodIDParam, 10, 64)
	if err != nil || periodIDParam == "" {
		return ctx.Error(NewError(ErrKeyInvalidRequest, fmt.Errorf("invalid period ID")), http.StatusBadRequest)
	}

	// Get gateway type from query param (optional)
	gatewayType := c.QueryParam("gateway")

	// Resolve gateway type (default to stripe if not specified)
	if gatewayType == "" {
		gatewayType = "stripe"
	}

	// Get checkout UI from billing service which includes validation logic
	response, err := e.billingService.GetCheckoutUI(c.Request().Context(), userID, uint(planID), gatewayType, uint(periodID))
	if err != nil {
		e.Logger().Error("failed to get checkout UI",
			zap.Uint("user_id", userID),
			zap.Uint("plan_id", uint(planID)),
			zap.Uint("period_id", uint(periodID)),
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
		zap.Uint("period_id", uint(periodID)),
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

	// Get active gateways (ordered by registration)
	allGateways := registry.GetAllGateways()

	allGateways.Range(func(id string, gateway pluginCore.GatewayIdentity) bool {
		abilities := dto.GatewayAbilities{}
		abilities.FromModel(getPublicAbilities(gateway))

		response = append(response, dto.GatewayPublicInfo{
			ID:          id,
			Name:        gateway.GetName(reqCtx),
			Description: gateway.GetDescription(reqCtx),
			LogoURL:     fmt.Sprintf("/api/billing/gateways/%s/logo", id),
			IsActive:    true,
			Abilities:   abilities,
		})
		return true
	})

	return httputil.EncodeResponse(ctx, &response, &response)
}

// getPublicAbilities returns public abilities for a gateway
// Derived from interface checks - gateways declare capabilities by implementing interfaces
func getPublicAbilities(gateway pluginCore.GatewayIdentity) pluginCore.PublicAbilities {
	return pluginCore.PublicAbilities{
		Checkout:       pluginCore.IsCheckoutProvider(gateway),
		SessionStatus:  pluginCore.IsSessionStatusProvider(gateway),
		CustomerPortal: pluginCore.IsCustomerPortal(gateway),
	}
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

// handleListPricingPlans returns pricing plans with their periods for user
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
			var priceLineID uint

			// If user is not authenticated, use default price line
			if !ok || userID == 0 {
				priceLine, err := e.pricingService.GetDefaultPriceLine(reqCtx)
				if err != nil {
					e.Logger().Error("failed to get default price line", zap.Error(err))
					return nil, 0, fmt.Errorf("failed to retrieve pricing: %w", err)
				}
				priceLineID = priceLine.ID
			} else {
				// Get effective price line for user
				priceLine, err := e.pricingService.GetEffectivePriceLineForUser(reqCtx, userID)
				if err != nil {
					e.Logger().Error("failed to get effective price line", zap.Error(err))
					return nil, 0, fmt.Errorf("failed to retrieve pricing: %w", err)
				}
				priceLineID = priceLine.ID
			}

			// Get pricing plans for the price line
			plans, err := e.pricingService.GetPlansForPriceLine(reqCtx, priceLineID)
			if err != nil {
				e.Logger().Error("failed to get pricing plans", zap.Error(err))
				return nil, 0, fmt.Errorf("failed to retrieve plans: %w", err)
			}

			return plans, int64(len(plans)), nil
		},
		func(plan *models.PricingPlan) dto.PublicPricingPlanResponse {
			var resp dto.PublicPricingPlanResponse
			_ = resp.FromModel(plan)
			// Fetch periods for each plan
			periods, err := e.pricingService.GetPricingPlanPeriods(reqCtx, plan.ID)
			if err != nil {
				e.Logger().Error("failed to get pricing plan periods", zap.Uint("plan_id", plan.ID), zap.Error(err))
			} else if len(periods) > 0 {
				resp.SetPricingPeriods(periods)
			}
			return resp
		},
	)
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

// handleCancelOperation executes the cancel operation.
// This endpoint is called after UI discovers it via POST /management.
// For API mode gateways, it executes directly. For portal mode, it returns redirect URL.
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

	// Check if gateway implements SubscriptionManager (for portal mode)
	manager, ok := gateway.(pluginCore.SubscriptionManager)
	if !ok {
		return ctx.Error(NewError(ErrKeyPaymentGatewayFailed, fmt.Errorf("gateway does not support subscription management")), http.StatusInternalServerError)
	}

	// Get management capabilities to determine mode
	capabilities, err := manager.GetManagementInfo(c.Request().Context(), userID)
	if err != nil {
		e.Logger().Error("failed to get management capabilities",
			zap.Uint("user_id", userID),
			zap.String("gateway_type", sub.GatewayType),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyManagementCapabilitiesFailed, fmt.Errorf("failed to get management capabilities: %w", err)), http.StatusInternalServerError)
	}

	// API mode: execute directly
	if capabilities.ManagementMode == pluginCore.ModeAPI {
		// Verify operation is supported (defense in depth)
		supported, exists := capabilities.Operations[pluginCore.OperationCancel]
		if !exists || !supported {
			return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("cancellation is not supported by this gateway")), http.StatusBadRequest)
		}

		executor, ok := gateway.(pluginCore.CancellationExecutor)
		if !ok {
			return ctx.Error(NewError(ErrKeyPaymentGatewayFailed, fmt.Errorf("gateway does not support cancellation execution")), http.StatusInternalServerError)
		}

		cancelResult, err := executor.ExecuteCancel(c.Request().Context(), userID, false) // Users get scheduled cancellation by default
		if err != nil {
			e.Logger().Error("failed to execute cancellation",
				zap.Uint("user_id", userID),
				zap.String("gateway_type", sub.GatewayType),
				zap.Error(err))
			return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("failed to cancel subscription: %w", err)), http.StatusInternalServerError)
		}

		result := &pluginCore.ManagementResult{
			Action:        pluginCore.ActionShowUI,
			Status:        string(cancelResult.Status),
			EffectiveTime: cancelResult.EffectiveAt,
			CanAbort:      cancelResult.CanAbort,
		}
		response := dto.ManagementResultResponse{}
		if err := response.FromModel(result); err != nil {
			return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("failed to build response: %w", err)), http.StatusInternalServerError)
		}
		return httputil.EncodeResponse(ctx, result, &response)
	}

	// Portal mode: return redirect URL
	result, err := manager.GetManagementURL(c.Request().Context(), userID, pluginCore.OperationCancel)
	if err != nil {
		e.Logger().Error("failed to get cancellation portal URL",
			zap.Uint("user_id", userID),
			zap.String("gateway_type", sub.GatewayType),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("failed to get cancellation URL: %w", err)), http.StatusInternalServerError)
	}

	response := dto.ManagementResultResponse{}
	if err := response.FromModel(result); err != nil {
		e.Logger().Error("failed to build cancellation response",
			zap.Uint("user_id", userID),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("failed to build response: %w", err)), http.StatusInternalServerError)
	}

	return httputil.EncodeResponse(ctx, result, &response)
}

// handleAbortCancellationOperation aborts a scheduled cancellation
// This endpoint is called when a user wants to revert a scheduled cancellation
func (e *APIExtension) handleAbortCancellationOperation(c echo.Context) error {
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

	// Check if there's a scheduled cancellation
	if sub.WillCancelAt == nil {
		return ctx.Error(NewError(ErrKeyNoScheduledCancellation, fmt.Errorf("no scheduled cancellation found")), http.StatusNotFound)
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

	// Check if gateway supports abort operation via Operations
	manager, ok := gateway.(pluginCore.SubscriptionManager)
	if ok {
		capabilities, capErr := manager.GetManagementInfo(c.Request().Context(), userID)
		if capErr == nil {
			supported, exists := capabilities.Operations[pluginCore.OperationCancel]
			if !exists || !supported {
				return ctx.Error(NewError(ErrKeyManagementOperationFailed,
					fmt.Errorf("gateway does not support cancellation operations")), http.StatusBadRequest)
			}
		}
	}

	// Check if gateway implements CancellationExecutor (required for abort)
	executor, ok := gateway.(pluginCore.CancellationExecutor)
	if !ok {
		return ctx.Error(NewError(ErrKeyPaymentGatewayFailed, fmt.Errorf("gateway does not support cancellation execution")), http.StatusInternalServerError)
	}

	// Abort the scheduled cancellation
	if err := executor.AbortCancellation(c.Request().Context(), userID); err != nil {
		e.Logger().Error("failed to abort scheduled cancellation",
			zap.Uint("user_id", userID),
			zap.String("gateway_type", sub.GatewayType),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("failed to abort cancellation: %w", err)), http.StatusInternalServerError)
	}

	// Return success response
	result := &pluginCore.ManagementResult{
		Action:   pluginCore.ActionShowUI,
		Status:   string(pluginCore.CancellationStatusAborted),
		CanAbort: false,
	}
	response := dto.ManagementResultResponse{}
	if err := response.FromModel(result); err != nil {
		return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("failed to build response: %w", err)), http.StatusInternalServerError)
	}

	return httputil.EncodeResponse(ctx, result, &response)
}

// handleChangePlanOperation executes the plan change operation.
// This endpoint is called after UI discovers it via POST /management.
// For API mode gateways, it executes directly. For portal mode, it returns redirect URL.
func (e *APIExtension) handleChangePlanOperation(c echo.Context) error {
	ctx := httputil.Context(c)
	userID, ok := e.getUser(ctx)
	if !ok {
		return ctx.Error(NewError(ErrKeyUnauthorized, fmt.Errorf("failed to get user ID")), http.StatusUnauthorized)
	}

	// Parse request body for period_id (required for API mode)
	var request dto.ChangePlanRequest
	_, valid := httputil.DecodeAndValidateRequest[*dto.ChangePlanRequest, *dto.ChangePlanRequest](ctx, &request)
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

	// Check if gateway implements SubscriptionManager (for portal mode)
	manager, ok := gateway.(pluginCore.SubscriptionManager)
	if !ok {
		return ctx.Error(NewError(ErrKeyPaymentGatewayFailed, fmt.Errorf("gateway does not support subscription management")), http.StatusInternalServerError)
	}

	// Get management capabilities to determine mode
	capabilities, err := manager.GetManagementInfo(c.Request().Context(), userID)
	if err != nil {
		e.Logger().Error("failed to get management capabilities",
			zap.Uint("user_id", userID),
			zap.String("gateway_type", sub.GatewayType),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyManagementCapabilitiesFailed, fmt.Errorf("failed to get management capabilities: %w", err)), http.StatusInternalServerError)
	}

	// API mode: execute directly
	if capabilities.ManagementMode == pluginCore.ModeAPI {
		// Verify operation is supported (defense in depth)
		supported, exists := capabilities.Operations[pluginCore.OperationChangePlan]
		if !exists || !supported {
			return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("plan change is not supported by this gateway")), http.StatusBadRequest)
		}

		executor, ok := gateway.(pluginCore.PlanChangeExecutor)
		if !ok {
			return ctx.Error(NewError(ErrKeyPaymentGatewayFailed, fmt.Errorf("gateway does not support plan change execution")), http.StatusInternalServerError)
		}

		result, err := executor.ExecutePlanChange(c.Request().Context(), userID, request.PeriodID)
		if err != nil {
			e.Logger().Error("failed to execute plan change",
				zap.Uint("user_id", userID),
				zap.Uint("period_id", request.PeriodID),
				zap.String("gateway_type", sub.GatewayType),
				zap.Error(err))
			return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("failed to change plan: %w", err)), http.StatusInternalServerError)
		}

		response := dto.PlanChangeResultResponse{}
		if err := response.FromModel(result); err != nil {
			e.Logger().Error("failed to build plan change response",
				zap.Uint("user_id", userID),
				zap.Error(err))
			return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("failed to build response: %w", err)), http.StatusInternalServerError)
		}

		return httputil.EncodeResponse(ctx, result, &response)
	}

	// Portal mode: return redirect URL
	result, err := manager.GetManagementURL(c.Request().Context(), userID, pluginCore.OperationChangePlan)
	if err != nil {
		e.Logger().Error("failed to get plan change portal URL",
			zap.Uint("user_id", userID),
			zap.String("gateway_type", sub.GatewayType),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyManagementOperationFailed, fmt.Errorf("failed to get plan change URL: %w", err)), http.StatusInternalServerError)
	}

	response := dto.ManagementResultResponse{}
	if err := response.FromModel(result); err != nil {
		e.Logger().Error("failed to build plan change response",
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

// getUserTopic returns the SSE topic name for a specific user
// This allows targeted broadcasting of billing events to a specific user
func (e *APIExtension) getUserTopic(userID uint) string {
	return fmt.Sprintf("user-%d", userID)
}

// registerBillingEventListeners registers the SSE server as a listener to portal billing events
// When billing events occur, they are published to the appropriate user's SSE topic
func (e *APIExtension) registerBillingEventListeners(coreCtx core.Context) {
	// Register listener for payment completed events
	// Note: Use PaymentCompletedEvent (not *PaymentCompletedEvent) to avoid pointer-to-pointer issues
	core.Listen[billingEvent.PaymentCompletedEvent](coreCtx,
		billingEvent.EVENT_PAYMENT_COMPLETED,
		func(ev *core.CoreEvent[billingEvent.PaymentCompletedEvent]) error {
			e.Logger().Debug("received PaymentCompletedEvent",
				zap.Uint("user_id", ev.Data.UserID),
				zap.String("amount", ev.Data.Amount.String()),
				zap.String("gateway", ev.Data.Gateway))
			return e.publishBillingEvent(ev.Data.UserID, billingEvent.SSEEventTypePaymentCompleted, &ev.Data)
		})

	// Register listener for subscription active events
	core.Listen[billingEvent.SubscriptionActiveEvent](coreCtx,
		billingEvent.EVENT_SUBSCRIPTION_ACTIVE,
		func(ev *core.CoreEvent[billingEvent.SubscriptionActiveEvent]) error {
			return e.publishBillingEvent(ev.Data.UserID, billingEvent.SSEEventTypeSubscriptionActive, ev.Data)
		})

	// Register listener for subscription created events
	core.Listen[billingEvent.SubscriptionCreatedEvent](coreCtx,
		billingEvent.EVENT_SUBSCRIPTION_CREATED,
		func(ev *core.CoreEvent[billingEvent.SubscriptionCreatedEvent]) error {
			return e.publishBillingEvent(ev.Data.UserID, billingEvent.SSEEventTypeSubscriptionCreated, ev.Data)
		})

	// Register listener for subscription updated events
	core.Listen[billingEvent.SubscriptionUpdatedEvent](coreCtx,
		billingEvent.EVENT_SUBSCRIPTION_UPDATED,
		func(ev *core.CoreEvent[billingEvent.SubscriptionUpdatedEvent]) error {
			return e.publishBillingEvent(ev.Data.UserID, billingEvent.SSEEventTypeSubscriptionUpdated, ev.Data)
		})

	// Register listener for subscription cancelled events
	core.Listen[billingEvent.SubscriptionCancelledEvent](coreCtx,
		billingEvent.EVENT_SUBSCRIPTION_CANCELLED,
		func(ev *core.CoreEvent[billingEvent.SubscriptionCancelledEvent]) error {
			return e.publishBillingEvent(ev.Data.UserID, billingEvent.SSEEventTypeSubscriptionCancelled, ev.Data)
		})

	// Register listener for plan changed events
	core.Listen[billingEvent.PlanChangedEvent](coreCtx,
		billingEvent.EVENT_PLAN_CHANGED,
		func(ev *core.CoreEvent[billingEvent.PlanChangedEvent]) error {
			return e.publishBillingEvent(ev.Data.UserID, billingEvent.SSEEventTypePlanChanged, ev.Data)
		})

	// Register listener for credit-only plan changes
	core.Listen[billingEvent.PlanChangeCreditOnlyEvent](coreCtx,
		billingEvent.EVENT_PLAN_CHANGE_CREDIT_ONLY,
		func(ev *core.CoreEvent[billingEvent.PlanChangeCreditOnlyEvent]) error {
			return e.publishBillingEvent(ev.Data.UserID, billingEvent.SSEEventTypeCreditOnly, ev.Data)
		})

	// Register listener for zero-amount plan changes
	core.Listen[billingEvent.PlanChangeZeroAmountEvent](coreCtx,
		billingEvent.EVENT_PLAN_CHANGE_ZERO_AMOUNT,
		func(ev *core.CoreEvent[billingEvent.PlanChangeZeroAmountEvent]) error {
			return e.publishBillingEvent(ev.Data.UserID, billingEvent.SSEEventTypeZeroAmount, ev.Data)
		})

	coreCtx.Logger().Info("SSE server registered as listener to billing events")
}

// publishBillingEvent publishes a billing event to a user's SSE topic
// The eventData parameter should be a portal core billing event with JSON tags
func (e *APIExtension) publishBillingEvent(userID uint, eventType string, eventData any) error {
	// Create SSE event wrapper for client consumption
	sseEvent := billingEvent.NewSSEEvent(eventType, eventData)

	// Marshal the wrapper to JSON
	eventJSON, err := json.Marshal(sseEvent)
	if err != nil {
		e.Logger().Error("failed to marshal billing event for SSE",
			zap.Uint("user_id", userID),
			zap.String("event_type", eventType),
			zap.Error(err))
		return err
	}

	// Create SSE event
	topic := e.getUserTopic(userID)
	event := sseServer.Event{
		ID:    fmt.Sprintf("billing-%d-%d", userID, time.Now().UnixNano()),
		Type:  eventType,
		Data:  eventJSON,
		Retry: 3000, // Recommended retry interval in milliseconds
	}

	if err := e.sseServer.Publish(event, topic); err != nil {
		e.Logger().Error("failed to publish billing event to SSE",
			zap.Uint("user_id", userID),
			zap.String("topic", topic),
			zap.String("event_type", eventType),
			zap.Error(err))
		return err
	}

	e.Logger().Debug("published billing event to SSE",
		zap.Uint("user_id", userID),
		zap.String("event_type", eventType),
		zap.String("topic", topic))
	return nil
}

// min helper for string length
func min(b []byte, maxLen int) []byte {
	if len(b) > maxLen {
		return b[:maxLen]
	}
	return b
}

// handleGetCheckoutSessionStatus retrieves the status of a checkout session.
// Used by embedded checkout return pages to verify payment completion.
func (e *APIExtension) handleGetCheckoutSessionStatus(c echo.Context) error {
	ctx := httputil.Context(c)
	userID, ok := e.getUser(ctx)
	if !ok {
		return ctx.Error(NewError(ErrKeyUnauthorized, errors.New("authentication required")), http.StatusUnauthorized)
	}

	sessionID := c.Param("sessionId")
	if sessionID == "" {
		return ctx.Error(NewError(ErrKeyInvalidRequest, errors.New("session ID is required")), http.StatusBadRequest)
	}

	// Get the gateway type from query params (default to Stripe)
	gatewayType := c.QueryParam("gateway")
	if gatewayType == "" {
		gatewayType = "stripe"
	}

	// Look up the gateway via billing service
	g, err := e.billingService.GetGateway(c.Request().Context(), gatewayType)
	if err != nil {
		e.Logger().Error("gateway not found",
			zap.Uint("user_id", userID),
			zap.String("gateway_type", gatewayType),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyGatewayNotFound, err), http.StatusNotFound)
	}

	// Check if gateway implements SessionStatusProvider
	if !pluginCore.IsSessionStatusProvider(g) {
		e.Logger().Debug("gateway does not support session status retrieval",
			zap.Uint("user_id", userID),
			zap.String("gateway_type", gatewayType))
		return ctx.Error(NewError(ErrKeyGatewayNotSupported, errors.New("gateway does not support session status retrieval")), http.StatusNotImplemented)
	}

	// Cast to SessionStatusProvider and retrieve status
	provider, err := pluginCore.AsSessionStatusProvider(g)
	if err != nil {
		e.Logger().Error("failed to cast gateway to SessionStatusProvider",
			zap.Uint("user_id", userID),
			zap.String("gateway_type", gatewayType),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyGatewayNotSupported, err), http.StatusInternalServerError)
	}

	status, err := provider.GetSessionStatus(c.Request().Context(), sessionID)
	if err != nil {
		e.Logger().Error("failed to get session status",
			zap.Uint("user_id", userID),
			zap.String("gateway_type", gatewayType),
			zap.String("session_id", sessionID),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyCheckoutUIGenerationFailed, fmt.Errorf("failed to get session status: %w", err)), http.StatusInternalServerError)
	}

	// Verify session ownership - prevent IDOR attack
	// Deny when UserID is 0 (ownership unverifiable) or when mismatched
	if status.UserID != userID {
		e.Logger().Warn("session ownership mismatch",
			zap.Uint("authenticated_user_id", userID),
			zap.Uint("session_user_id", status.UserID),
			zap.String("session_id", sessionID))
		return ctx.Error(NewError(ErrKeyUnauthorized, errors.New("session not found")), http.StatusNotFound)
	}

	response := dto.CheckoutSessionStatusResponse{}
	if err := response.FromModel(status); err != nil {
		e.Logger().Error("failed to convert session status to DTO",
			zap.Uint("user_id", userID),
			zap.String("gateway_type", gatewayType),
			zap.String("session_id", sessionID),
			zap.Error(err))
		return ctx.Error(NewError(ErrKeyCheckoutUIGenerationFailed, fmt.Errorf("failed to build response: %w", err)), http.StatusInternalServerError)
	}

	return c.JSON(http.StatusOK, response)
}

// handleX402Checkout processes x402 crypto payments.
// First call (no PAYMENT-SIGNATURE) returns 402 with challenge.
// Second call (with PAYMENT-SIGNATURE) confirms payment and issues credits.
func (e *APIExtension) handleX402Checkout(c echo.Context) error {
	return e.x402Handler.HandleCheckout(c)
}
