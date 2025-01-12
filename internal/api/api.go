package api

import (
	"fmt"
	"github.com/gorilla/mux"
	"go.lumeweb.com/portal-plugin-billing/service"
	"go.lumeweb.com/portal/config"
	"go.lumeweb.com/portal/core"
	"go.lumeweb.com/portal/middleware"
	"net/http"
)

var _ core.API = (*API)(nil)

const defaultUsageHistoryPeriod = 30

type API struct {
	ctx            core.Context
	logger         *core.Logger
	billingService service.BillingService
	quotaService   service.QuotaService
}

func NewAPI() (core.API, []core.ContextBuilderOption, error) {
	api := &API{}
	return api, core.ContextOptions(
		core.ContextWithStartupFunc(func(ctx core.Context) error {
			api.ctx = ctx
			api.logger = ctx.APILogger(api)
			api.billingService = core.GetService[service.BillingService](ctx, service.BILLING_SERVICE).(service.BillingService)
			api.quotaService = core.GetService[service.QuotaService](ctx, service.QUOTA_SERVICE).(service.QuotaService)
			return nil
		}),
	), nil
}

func (a API) Name() string {
	return service.BILLING_SERVICE
}

func (a API) Subdomain() string {
	return service.BILLING_SERVICE
}

func (a *API) Configure(_ *mux.Router, accessSvc core.AccessService) error {
	accountApi := core.GetAPI("dashboard")
	httpService := core.GetService[core.HTTPService](a.ctx, core.HTTP_SERVICE)

	// Middleware setup
	corsHandler := middleware.CorsMiddleware(nil)
	authMw := middleware.AuthMiddleware(middleware.AuthMiddlewareOptions{
		Context: a.ctx,
		Purpose: core.JWTPurposeLogin,
	})
	accessMw := middleware.AccessMiddleware(a.ctx)

	// Setup routers
	mainRouter := httpService.Router()
	domain := fmt.Sprintf("%s.%s", accountApi.Subdomain(), a.ctx.Config().Config().Core.Domain)
	accountRouter := mainRouter.Host(domain).Subrouter()

	mainRouter.Use(corsHandler)
	accountRouter.Use(corsHandler)

	// Define routes
	routes := []struct {
		Router      *mux.Router
		Path        string
		Method      string
		Handler     http.HandlerFunc
		Middlewares []mux.MiddlewareFunc
		Access      string
	}{
		{mainRouter, "/api/account/subscription", "GET", a.getSubscription, []mux.MiddlewareFunc{authMw, accessMw}, core.ACCESS_USER_ROLE},
		//{mainRouter, "/api/account/subscription/billing", "POST", a.updateBilling, []mux.MiddlewareFunc{authMw, accessMw}, core.ACCESS_USER_ROLE},
		{mainRouter, "/api/account/subscription/plan", "PUT", a.changeSubscription, []mux.MiddlewareFunc{authMw, accessMw}, core.ACCESS_USER_ROLE},
		//{mainRouter, "/api/account/subscription/request-payment-method-change", "POST", a.requestPaymentMethodChange, []mux.MiddlewareFunc{authMw, accessMw}, core.ACCESS_USER_ROLE},
		//{mainRouter, "/api/account/subscription/update-payment-method", "POST", a.updatePaymentMethod, []mux.MiddlewareFunc{authMw, accessMw}, core.ACCESS_USER_ROLE},
		//{mainRouter, "/api/account/subscription/cancel", "POST", a.cancelSubscription, []mux.MiddlewareFunc{authMw, accessMw}, core.ACCESS_USER_ROLE},
		{mainRouter, "/api/account/subscription/billing/countries", "GET", a.listBillingCountries, []mux.MiddlewareFunc{authMw, accessMw}, core.ACCESS_USER_ROLE},
		{mainRouter, "/api/account/subscription/billing/states", "GET", a.listBillingStates, []mux.MiddlewareFunc{authMw, accessMw}, core.ACCESS_USER_ROLE},
		{mainRouter, "/api/account/subscription/billing/cities", "GET", a.listBillingCities, []mux.MiddlewareFunc{authMw, accessMw}, core.ACCESS_USER_ROLE},
		{accountRouter, "/api/account/subscription/plans", "GET", a.getPlans, nil, ""},
		//{accountRouter, "/api/account/usage/current", "GET", a.getCurrentUsage, []mux.MiddlewareFunc{authMw, accessMw}, core.ACCESS_USER_ROLE},
		//{accountRouter, "/api/account/usage/history/upload", "GET", a.getUploadUsageHistory, []mux.MiddlewareFunc{authMw, accessMw}, core.ACCESS_USER_ROLE},
		//{accountRouter, "/api/account/usage/history/download", "GET", a.getDownloadUsageHistory, []mux.MiddlewareFunc{authMw, accessMw}, core.ACCESS_USER_ROLE},
		//{accountRouter, "/api/account/usage/history/storage", "GET", a.getStorageUsageHistory, []mux.MiddlewareFunc{authMw, accessMw}, core.ACCESS_USER_ROLE},
		//{mainRouter, "/api/account/webhook/payment", "POST", a.handlePaymentWebhook, nil, ""},
	}

	// Register routes
	for _, route := range routes {
		r := route.Router.HandleFunc(route.Path, route.Handler).Methods(route.Method, "OPTIONS")
		r.Use(route.Middlewares...)

		if err := accessSvc.RegisterRoute(accountApi.Subdomain(), route.Path, route.Method, route.Access); err != nil {
			return fmt.Errorf("failed to register route %s %s: %w", route.Method, route.Path, err)
		}
	}

	return nil
}

func (a API) AuthTokenName() string {
	return core.AUTH_TOKEN_NAME
}

func (a API) Config() config.APIConfig {
	return nil
}

/*




func (a API) updateBilling(w http.ResponseWriter, r *http.Request) {
	ctx := httputil.Context(r, w)

	user, err := middleware.GetUserFromContext(ctx)

	if err != nil {
		_ = ctx.Error(core.NewAccountError(core.ErrKeyInvalidLogin, nil), http.StatusUnauthorized)
		return
	}

	var billingInfo messages.BillingInfo
	if err := ctx.Decode(&billingInfo); err != nil {
		_ = ctx.Error(err, http.StatusInternalServerError)
		return
	}

	if err := a.billingService.GetSubscriptionManager().UpdateBillingInfo(ctx, user, &billingInfo); err != nil {
		errs :=
			make([]*messages.UpdateBillingInfoResponseErrorItem, 0)
		if merr, ok := errors.Unwrap(err).(*multierror.Error); ok {
			for _, subErr := range merr.Errors {
				switch {
				case errors.Is(subErr, address.ErrInvalidCountryCode):
					errs = append(errs, &messages.UpdateBillingInfoResponseErrorItem{
						Field:   "country",
						Message: subErr.Error(),
					})
				case errors.Is(subErr, address.ErrInvalidAdministrativeArea):
					errs = append(errs, &messages.UpdateBillingInfoResponseErrorItem{
						Field:   "state",
						Message: subErr.Error(),
					})
				case errors.Is(subErr, address.ErrInvalidLocality):
					errs = append(errs, &messages.UpdateBillingInfoResponseErrorItem{
						Field:   "city",
						Message: subErr.Error(),
					})
				case errors.Is(subErr, address.ErrInvalidPostCode):
					errs = append(errs, &messages.UpdateBillingInfoResponseErrorItem{
						Field:   "zip",
						Message: subErr.Error(),
					})
				}
			}

			ctx.Response.WriteHeader(http.StatusBadRequest)
			ctx.Encode(&messages.UpdateBillingInfoResponseError{Errors: errs})
			return
		}

		_ = ctx.Error(err, http.StatusInternalServerError)
	}
	return
}

func (a API) updatePaymentMethod(w http.ResponseWriter, r *http.Request) {
	ctx := httputil.Context(r, w)

	user, err := middleware.GetUserFromContext(ctx)
	if err != nil {
		_ = ctx.Error(core.NewAccountError(core.ErrKeyInvalidLogin, nil), http.StatusUnauthorized)
		return
	}

	var updateRequest messages.UpdatePaymentMethodRequest
	if err := ctx.Decode(&updateRequest); err != nil {
		_ = ctx.Error(err, http.StatusBadRequest)
		return
	}

	if updateRequest.PaymentMethodID == "" {
		_ = ctx.Error(fmt.Errorf("payment_method_id is required"), http.StatusBadRequest)
		return
	}

	if err := a.billingService.GetSubscriptionManager().UpdatePaymentMethod(ctx, user, updateRequest.PaymentMethodID); err != nil {
		_ = ctx.Error(err, http.StatusInternalServerError)
		return
	}
}

func (a API) requestPaymentMethodChange(w http.ResponseWriter, r *http.Request) {
	ctx := httputil.Context(r, w)

	user, err := middleware.GetUserFromContext(ctx)

	if err != nil {
		_ = ctx.Error(core.NewAccountError(core.ErrKeyInvalidLogin, nil), http.StatusUnauthorized)
		return
	}

	response, err := a.billingService.GetSubscriptionManager().RequestPaymentMethodChange(ctx, user)

	if err != nil {
		_ = ctx.Error(err, http.StatusInternalServerError)
		return
	}

	ctx.Encode(response)
}

func (a API) cancelSubscription(w http.ResponseWriter, r *http.Request) {
	ctx := httputil.Context(r, w)

	user, err := middleware.GetUserFromContext(ctx)
	if err != nil {
		_ = ctx.Error(core.NewAccountError(core.ErrKeyInvalidLogin, nil), http.StatusUnauthorized)
		return
	}

	var req messages.CancellationRequest
	if err := ctx.Decode(&req); err != nil {
		_ = ctx.Error(err, http.StatusBadRequest)
		return
	}

	// Validate reason is provided
	if req.Reason == "" {
		_ = ctx.Error(fmt.Errorf("cancellation reason is required"), http.StatusBadRequest)
		return
	}

	resp, err := a.billingService.GetSubscriptionManager().CancelSubscription(ctx, user, &req)
	if err != nil {
		_ = ctx.Error(err, http.StatusInternalServerError)
		return
	}

	ctx.Encode(resp)
}
func (a API) getUploadUsageHistory(w http.ResponseWriter, r *http.Request) {
	a.getUsageHistory(w, r, a.quotaService.GetUploadUsageHistory)
}

func (a API) getDownloadUsageHistory(w http.ResponseWriter, r *http.Request) {
	a.getUsageHistory(w, r, a.quotaService.GetDownloadUsageHistory)
}

func (a API) getStorageUsageHistory(w http.ResponseWriter, r *http.Request) {
	a.getUsageHistory(w, r, a.quotaService.GetStorageUsageHistory)
}

func (a API) handlePaymentWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := httputil.Context(r, w)

	// Read and parse the webhook payload
	var event hyperswitch.WebhookEvent
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		a.logger.Error("failed to decode webhook payload", zap.Error(err))
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Forward the webhook to KillBill through the subscription manager
	err := a.billingService.GetLifeCycle().HandleWebhook(ctx, &event, r.Header.Get("Hyperswitch-Signature"))
	if err != nil {
		a.logger.Error("failed to process webhook", zap.Error(err))
		http.Error(w, "Webhook processing failed", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (a API) getUsageHistory(w http.ResponseWriter, r *http.Request, getHistoryFunc func(uint, int) ([]*messages.UsageData, error)) {
	ctx := httputil.Context(r, w)

	user, err := middleware.GetUserFromContext(ctx)
	if err != nil {
		acctErr := core.NewAccountError(core.ErrKeyInvalidLogin, nil)
		_ = ctx.Error(acctErr, acctErr.HttpStatus())
		return
	}

	period := defaultUsageHistoryPeriod

	err = ctx.DecodeForm("period", &period)
	if err != nil {
		_ = ctx.Error(err, http.StatusBadRequest)
		return
	}

	usageHistory, err := getHistoryFunc(user, period)
	if err != nil {
		_ = ctx.Error(err, http.StatusInternalServerError)
		return
	}

	ctx.Encode(usageHistory)
}

func (a API) getCurrentUsage(w http.ResponseWriter, r *http.Request) {
	ctx := httputil.Context(r, w)

	user, err := middleware.GetUserFromContext(ctx)
	if err != nil {
		acctErr := core.NewAccountError(core.ErrKeyInvalidLogin, nil)
		_ = ctx.Error(acctErr, acctErr.HttpStatus())
		return
	}

	usage, err := a.quotaService.GetCurrentUsage(user)
	if err != nil {
		_ = ctx.Error(err, http.StatusInternalServerError)
		return
	}

	ctx.Encode(usage)
}
*/
