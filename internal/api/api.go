package api

import (
	"errors"
	"fmt"
	"github.com/Boostport/address"
	"github.com/gorilla/mux"
	"github.com/hashicorp/go-multierror"
	"github.com/samber/lo"
	"go.lumeweb.com/httputil"
	"go.lumeweb.com/portal-plugin-billing/internal/api/messages"
	"go.lumeweb.com/portal-plugin-billing/service"
	"go.lumeweb.com/portal/config"
	"go.lumeweb.com/portal/core"
	"go.lumeweb.com/portal/middleware"
	"net/http"
	"sort"
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
		{mainRouter, "/api/account/subscription/billing", "POST", a.updateBilling, []mux.MiddlewareFunc{authMw, accessMw}, core.ACCESS_USER_ROLE},
		{mainRouter, "/api/account/subscription/change", "POST", a.changeSubscription, []mux.MiddlewareFunc{authMw, accessMw}, core.ACCESS_USER_ROLE},
		{mainRouter, "/api/account/subscription/connect", "POST", a.connectSubscription, []mux.MiddlewareFunc{authMw, accessMw}, core.ACCESS_USER_ROLE},
		{mainRouter, "/api/account/subscription/ephemeral-key", "POST", a.generateEphemeralKey, []mux.MiddlewareFunc{authMw, accessMw}, core.ACCESS_USER_ROLE},
		{mainRouter, "/api/account/subscription/request-payment-method-change", "POST", a.requestPaymentMethodChange, []mux.MiddlewareFunc{authMw, accessMw}, core.ACCESS_USER_ROLE},
		{mainRouter, "/api/account/subscription/cancel", "POST", a.cancelSubscription, []mux.MiddlewareFunc{authMw, accessMw}, core.ACCESS_USER_ROLE},
		{mainRouter, "/api/account/subscription/billing/countries", "GET", a.listBillingCountries, []mux.MiddlewareFunc{authMw, accessMw}, core.ACCESS_USER_ROLE},
		{mainRouter, "/api/account/subscription/billing/states", "GET", a.listBillingStates, []mux.MiddlewareFunc{authMw, accessMw}, core.ACCESS_USER_ROLE},
		{mainRouter, "/api/account/subscription/billing/cities", "GET", a.listBillingCities, []mux.MiddlewareFunc{authMw, accessMw}, core.ACCESS_USER_ROLE},
		{accountRouter, "/api/account/subscription/plans", "GET", a.getPlans, nil, ""},
		{accountRouter, "/api/account/usage/current", "GET", a.getCurrentUsage, []mux.MiddlewareFunc{authMw, accessMw}, core.ACCESS_USER_ROLE},
		{accountRouter, "/api/account/usage/history/upload", "GET", a.getUploadUsageHistory, []mux.MiddlewareFunc{authMw, accessMw}, core.ACCESS_USER_ROLE},
		{accountRouter, "/api/account/usage/history/download", "GET", a.getDownloadUsageHistory, []mux.MiddlewareFunc{authMw, accessMw}, core.ACCESS_USER_ROLE},
		{accountRouter, "/api/account/usage/history/storage", "GET", a.getStorageUsageHistory, []mux.MiddlewareFunc{authMw, accessMw}, core.ACCESS_USER_ROLE},
	}

	// Register routes
	for _, route := range routes {
		r := route.Router.HandleFunc(route.Path, route.Handler).Methods(route.Method, "OPTIONS")
		r.Use(route.Middlewares...)

		if err := accessSvc.RegisterRoute(a.Subdomain(), route.Path, route.Method, route.Access); err != nil {
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

func (a API) getPlans(w http.ResponseWriter, r *http.Request) {
	ctx := httputil.Context(r, w)

	plans, err := a.billingService.GetPlans(ctx)

	if err != nil {
		_ = ctx.Error(err, http.StatusInternalServerError)
		return
	}

	ctx.Encode(&messages.SubscriptionPlansResponse{Plans: plans})
}

func (a API) getSubscription(w http.ResponseWriter, r *http.Request) {
	ctx := httputil.Context(r, w)

	user, err := middleware.GetUserFromContext(ctx)

	if err != nil {
		_ = ctx.Error(core.NewAccountError(core.ErrKeyInvalidLogin, nil), http.StatusUnauthorized)
		return
	}

	subscription, err := a.billingService.GetSubscription(ctx, user)

	if err != nil {
		_ = ctx.Error(err, http.StatusInternalServerError)
		return
	}

	ctx.Encode(subscription)
}

func (a API) changeSubscription(w http.ResponseWriter, r *http.Request) {
	ctx := httputil.Context(r, w)

	user, err := middleware.GetUserFromContext(ctx)

	if err != nil {
		_ = ctx.Error(core.NewAccountError(core.ErrKeyInvalidLogin, nil), http.StatusUnauthorized)
		return
	}

	var changeRequest messages.SubscriptionChangeRequest
	if err := ctx.Decode(&changeRequest); err != nil {
		_ = ctx.Error(err, http.StatusInternalServerError)
		return
	}

	if changeRequest.Plan == "" {
		_ = ctx.Error(fmt.Errorf("plan is required"), http.StatusBadRequest)
		return
	}

	if err := a.billingService.ChangeSubscription(ctx, user, changeRequest.Plan); err != nil {
		_ = ctx.Error(err, http.StatusInternalServerError)
		return
	}
}

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

	if err := a.billingService.UpdateBillingInfo(ctx, user, &billingInfo); err != nil {
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

func (a API) connectSubscription(w http.ResponseWriter, r *http.Request) {
	ctx := httputil.Context(r, w)

	user, err := middleware.GetUserFromContext(ctx)

	if err != nil {
		_ = ctx.Error(core.NewAccountError(core.ErrKeyInvalidLogin, nil), http.StatusUnauthorized)
		return
	}

	var connectRequest messages.SubscriptionConnectRequest
	if err := ctx.Decode(&connectRequest); err != nil {
		_ = ctx.Error(err, http.StatusInternalServerError)
		return
	}

	if connectRequest.PaymentMethodID == "" {
		_ = ctx.Error(fmt.Errorf("payment_method_id is required"), http.StatusBadRequest)
		return
	}

	if err := a.billingService.ConnectSubscription(ctx, user, connectRequest.PaymentMethodID); err != nil {
		_ = ctx.Error(err, http.StatusInternalServerError)
		return
	}
}

func (a API) generateEphemeralKey(w http.ResponseWriter, r *http.Request) {
	ctx := httputil.Context(r, w)

	user, err := middleware.GetUserFromContext(ctx)

	if err != nil {
		_ = ctx.Error(core.NewAccountError(core.ErrKeyInvalidLogin, nil), http.StatusUnauthorized)
		return
	}

	key, err := a.billingService.GenerateEphemeralKey(ctx, user)

	if err != nil {
		_ = ctx.Error(err, http.StatusInternalServerError)
		return
	}

	ctx.Encode(key)
}

func (a API) requestPaymentMethodChange(w http.ResponseWriter, r *http.Request) {
	ctx := httputil.Context(r, w)

	user, err := middleware.GetUserFromContext(ctx)

	if err != nil {
		_ = ctx.Error(core.NewAccountError(core.ErrKeyInvalidLogin, nil), http.StatusUnauthorized)
		return
	}

	response, err := a.billingService.RequestPaymentMethodChange(ctx, user)

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

	if err := a.billingService.CancelSubscription(ctx, user); err != nil {
		_ = ctx.Error(err, http.StatusInternalServerError)
		return
	}
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

func (a API) listBillingCountries(w http.ResponseWriter, r *http.Request) {
	ctx := httputil.Context(r, w)

	countries := address.ListCountries("en")

	countriesResponse := lo.Map(countries, func(item address.CountryListItem, _ int) *messages.ListBillingCountriesResponseItem {
		countryData := address.GetCountry(item.Code)
		return &messages.ListBillingCountriesResponseItem{
			Code: item.Code,
			Name: item.Name,
			SupportedEntities: lo.Map(countryData.Allowed, func(entity address.Field, _ int) string {
				return entity.Key()
			}),
		}
	})

	ctx.Encode(countriesResponse)
}

func (a API) listBillingStates(w http.ResponseWriter, r *http.Request) {
	ctx := httputil.Context(r, w)

	var country string

	err := ctx.DecodeForm("country", &country)
	if err != nil {
		_ = ctx.Error(err, http.StatusBadRequest)
		return
	}

	countryData := address.GetCountry(country)

	states := lo.Reduce(
		lo.Keys(countryData.AdministrativeAreas),
		func(acc []*messages.ListBillingStatesResponseItem, key string, _ int) []*messages.ListBillingStatesResponseItem {
			areas := countryData.AdministrativeAreas[key]
			for _, area := range areas {
				acc = append(acc, &messages.ListBillingStatesResponseItem{
					Code: area.ID,
					Name: area.Name,
				})
			}
			return acc
		},
		[]*messages.ListBillingStatesResponseItem{},
	)

	ctx.Encode(states)
}

func (a API) listBillingCities(w http.ResponseWriter, r *http.Request) {
	ctx := httputil.Context(r, w)

	var country, state string

	err := ctx.DecodeForm("country", &country)
	if err != nil {
		_ = ctx.Error(err, http.StatusBadRequest)
		return
	}

	err = ctx.DecodeForm("state", &state)
	if err != nil {
		_ = ctx.Error(err, http.StatusBadRequest)
		return
	}

	countryData := address.GetCountry(country)

	cities := lo.Reduce(
		lo.Values(countryData.AdministrativeAreas),
		func(acc []*messages.ListBillingCitiesResponseItem, areas []address.AdministrativeAreaData, _ int) []*messages.ListBillingCitiesResponseItem {
			matchingArea, found := lo.Find(areas, func(area address.AdministrativeAreaData) bool {
				return area.ID == state
			})

			if found {
				return append(acc, lo.Map(matchingArea.Localities, func(locality address.LocalityData, _ int) *messages.ListBillingCitiesResponseItem {
					return &messages.ListBillingCitiesResponseItem{
						Code: locality.ID,
						Name: locality.Name,
					}
				})...)
			}

			return acc
		},
		[]*messages.ListBillingCitiesResponseItem{},
	)

	// Sort cities alphabetically by name
	sort.Slice(cities, func(i, j int) bool {
		return cities[i].Name < cities[j].Name
	})

	ctx.Encode(cities)
}
