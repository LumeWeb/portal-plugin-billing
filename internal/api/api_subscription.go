package api

import (
	"fmt"
	"go.lumeweb.com/httputil"
	"go.lumeweb.com/portal-plugin-billing/internal/api/messages"
	"go.lumeweb.com/portal/core"
	"go.lumeweb.com/portal/middleware"
	"go.uber.org/zap"
	"io"
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

	ctx.Encode(&messages.GetPlansResponse{Plans: plans})
}

func (a API) createSubscription(w http.ResponseWriter, r *http.Request) {
	ctx := httputil.Context(r, w)

	user, err := middleware.GetUserFromContext(ctx)

	if err != nil {
		_ = ctx.Error(core.NewAccountError(core.ErrKeyInvalidLogin, nil), http.StatusUnauthorized)
		return
	}

	var createRequest messages.CreateSubscriptionRequest
	if err := ctx.Decode(&createRequest); err != nil {
		_ = ctx.Error(err, http.StatusInternalServerError)
	}

	if createRequest.PlanID == "" {
		_ = ctx.Error(fmt.Errorf("plan is required"), http.StatusBadRequest)
		return
	}

	if err := a.billingService.CreateSubscription(ctx, user, createRequest.PlanID); err != nil {
		_ = ctx.Error(err, http.StatusInternalServerError)
		return
	}

	a.getSubscription(w, r)
}

func (a API) changeSubscription(w http.ResponseWriter, r *http.Request) {
	ctx := httputil.Context(r, w)

	user, err := middleware.GetUserFromContext(ctx)

	if err != nil {
		_ = ctx.Error(core.NewAccountError(core.ErrKeyInvalidLogin, nil), http.StatusUnauthorized)
		return
	}

	var changeRequest messages.UpdateSubscriptionRequest
	if err := ctx.Decode(&changeRequest); err != nil {
		_ = ctx.Error(err, http.StatusInternalServerError)
		return
	}

	if changeRequest.PlanID == "" {
		_ = ctx.Error(fmt.Errorf("plan is required"), http.StatusBadRequest)
		return
	}

	if err := a.billingService.UpdateSubscription(ctx, user, changeRequest.PlanID); err != nil {
		_ = ctx.Error(err, http.StatusInternalServerError)
		return
	}

	a.getSubscription(w, r)
}

func (a API) cancelSubscription(w http.ResponseWriter, r *http.Request) {
	ctx := httputil.Context(r, w)

	user, err := middleware.GetUserFromContext(ctx)
	if err != nil {
		_ = ctx.Error(core.NewAccountError(core.ErrKeyInvalidLogin, nil), http.StatusUnauthorized)
		return
	}

	err = a.billingService.CancelSubscription(ctx, user)
	if err != nil {
		_ = ctx.Error(err, http.StatusInternalServerError)
		return
	}

	ctx.Response.WriteHeader(http.StatusNoContent)
}

func (a *API) handlePaymentWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := httputil.Context(r, w)

	// Read the request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		a.logger.Error("failed to read webhook payload", zap.Error(err))
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	err = a.billingService.HandleWebhook(body)
	if err != nil {
		_ = ctx.Error(err, http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
}
