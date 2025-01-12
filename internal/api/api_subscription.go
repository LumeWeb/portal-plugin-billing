package api

import (
	"fmt"
	"go.lumeweb.com/httputil"
	"go.lumeweb.com/portal-plugin-billing/internal/api/messages"
	"go.lumeweb.com/portal/core"
	"go.lumeweb.com/portal/middleware"
	"net/http"
)

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

func (a API) getPlans(w http.ResponseWriter, r *http.Request) {
	ctx := httputil.Context(r, w)

	plans, err := a.billingService.GetPlans(ctx)

	if err != nil {
		_ = ctx.Error(err, http.StatusInternalServerError)
		return
	}

	ctx.Encode(&messages.SubscriptionPlansResponse{Plans: plans})
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

	if err := a.billingService.UpdateSubscription(ctx, user, changeRequest.Plan); err != nil {
		_ = ctx.Error(err, http.StatusInternalServerError)
		return
	}
}
