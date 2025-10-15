package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/labstack/echo/v4"
	"go.lumeweb.com/httputil"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	router "go.lumeweb.com/portal-router"
	"go.lumeweb.com/portal/core"
	"go.uber.org/zap"
)

// Read the full request body with size limit (1 MiB)
const maxWebhookPayload = 1 << 20 // 1 MiB

// APIExtension extends the API with billing functionality
type APIExtension struct {
	ctx            core.Context
	logger         *core.Logger
	billingService pluginCore.BillingService
}

// NewAPIExtension creates a new API extension for billing
func NewAPIExtension() core.APIExtensionFactory {
	return func() (core.APIExtension, []core.ContextBuilderOption, error) {
		ext := &APIExtension{}

		return ext, core.ContextOptions(core.ContextWithStartupFunc(func(ctx core.Context) error {
			ext.ctx = ctx
			ext.logger = ctx.NamedLogger("billing.api_extension")

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
	// Create a subrouter for billing
	billingRouter, err := gRouter.Group("/api/account/billing")
	if err != nil {
		return err
	}

	// Register all route handlers
	if err = e.registerWebhookHandlers(billingRouter, accessSvc); err != nil {
		return err
	}

	return nil
}

// registerWebhookHandlers sets up the webhook routes
func (e *APIExtension) registerWebhookHandlers(gRouter router.Router, accessSvc core.AccessService) error {
	routes := router.DefineRoutes(
		router.NewRoute(http.MethodPost, "/webhooks/:gatewayType", e.handleWebhook,
			router.WithSwagger(
				router.WithSummary("Process payment gateway webhook"),
				router.WithDescription("Handles incoming webhooks from payment gateways such as Stripe, PayPal, etc."),
				router.WithTags("Billing"),
				router.WithPathParam("gatewayType", "Type of payment gateway (e.g., stripe, paypal)", "stripe"),
				router.WithRequestBody(map[string]interface{}{}, "Raw webhook payload from the payment gateway", false),
				router.WithSuccessResponse(http.StatusNoContent, "Webhook processed successfully"),
				router.WithErrorResponses(
					router.DefineSwaggerErrorResponses(
						router.DefineSwaggerErrorResponse(http.StatusBadRequest, "Invalid gateway type or webhook validation failed"),
					),
				),
			),
		),
	)

	return router.RegisterRoutes(gRouter, accessSvc, core.GetAPI(e.TargetAPI()).Subdomain(), routes)
}

// handleWebhook processes incoming webhook requests from payment gateways
func (e *APIExtension) handleWebhook(c echo.Context) error {
	ctx := httputil.Context(c)

	gatewayType := c.Param("gatewayType")

	if gatewayType == "" {
		return ctx.Error(fmt.Errorf("gateway type is required"), http.StatusBadRequest)
	}

	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, maxWebhookPayload)
	payload, err := io.ReadAll(c.Request().Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			e.logger.Warn("webhook payload too large",
				zap.String("gateway", gatewayType),
				zap.Int64("max_size", maxWebhookPayload))
			return ctx.Error(fmt.Errorf("payload too large"), http.StatusRequestEntityTooLarge)
		}
		e.logger.Error("failed to read webhook payload",
			zap.String("gateway", gatewayType),
			zap.Error(err))
		return ctx.Error(fmt.Errorf("failed to read webhook payload"), http.StatusBadRequest)
	}
	defer func() {
		if err := c.Request().Body.Close(); err != nil {
			e.logger.Error("failed to close request body",
				zap.String("gateway", gatewayType),
				zap.Error(err))
		}
	}()

	// Get signature header name from billing service
	sigHeader, err := e.billingService.GetSignatureHeader(gatewayType)
	if err != nil {
		return ctx.Error(fmt.Errorf("failed to get signature header: %w", err), http.StatusBadRequest)
	}

	// Get signature from header
	signature := c.Request().Header.Get(sigHeader)
	if signature == "" {
		return ctx.Error(fmt.Errorf("missing %s header", sigHeader), http.StatusBadRequest)
	}

	// Process the webhook through the billing service
	if err := e.billingService.ProcessWebhook(c.Request().Context(), gatewayType, signature, payload); err != nil {
		e.logger.Error("failed to process webhook",
			zap.String("gateway", gatewayType),
			zap.Error(err))
		return ctx.Error(fmt.Errorf("failed to process webhook: %w", err), http.StatusBadRequest)
	}

	return ctx.NoContent(http.StatusNoContent)
}
