package api

import (
	"fmt"
	"github.com/gorilla/mux"
	"go.lumeweb.com/httputil"
	"go.lumeweb.com/portal-plugin-billing/internal/api/messages"
	"go.lumeweb.com/portal-plugin-billing/service"
	"go.lumeweb.com/portal/config"
	"go.lumeweb.com/portal/core"
	"net/http"
)

var _ core.API = (*API)(nil)

type API struct {
	ctx            core.Context
	logger         *core.Logger
	billingService service.BillingService
}

func NewAPI() (core.API, []core.ContextBuilderOption, error) {
	api := &API{}
	return api, core.ContextOptions(
		core.ContextWithStartupFunc(func(ctx core.Context) error {
			api.ctx = ctx
			api.logger = ctx.APILogger(api)
			api.billingService = core.GetService[service.BillingService](ctx, service.BILLING_SERVICE).(service.BillingService)
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

func (a API) Configure(_ *mux.Router) error {
	accountApi := core.GetAPI("dashboard")
	router := core.GetService[core.HTTPService](a.ctx, core.HTTP_SERVICE).Router()

	domain := fmt.Sprintf("%s.%s", accountApi.Subdomain(), a.ctx.Config().Config().Core.Domain)
	accountRouter := router.Host(domain).Subrouter()

	accountRouter.HandleFunc("/api/account/subscription/plans", a.getPlans).Methods("GET")

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
