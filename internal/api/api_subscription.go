package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go.lumeweb.com/httputil"
	"go.lumeweb.com/portal-plugin-billing/internal/api/messages"
	pluginConfig "go.lumeweb.com/portal-plugin-billing/internal/config"
	"go.lumeweb.com/portal-plugin-billing/internal/hyperswitch"
	"go.lumeweb.com/portal-plugin-billing/internal/service"
	"go.lumeweb.com/portal/core"
	"go.lumeweb.com/portal/middleware"
	"go.uber.org/zap"
	"io"
	"net/http"
)

const (
	hyperswitchSignatureHeader = "x-webhook-signature-512"
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
	// Restore the body for later use
	r.Body = io.NopCloser(bytes.NewBuffer(body))

	// Get and verify the webhook signature
	signature := r.Header.Get(hyperswitchSignatureHeader)
	if signature == "" {
		a.logger.Error("missing signature header")
		_ = ctx.Error(fmt.Errorf("missing signature header"), http.StatusUnauthorized)
		return
	}

	cfg := a.ctx.Config().GetService(service.BILLING_SERVICE).(*pluginConfig.BillingConfig)
	if err := verifyWebhookSignature(body, signature, cfg.Hyperswitch.WebhookSecret); err != nil {
		a.logger.Error("signature verification failed",
			zap.Error(err),
			zap.String("signature", signature))
		_ = ctx.Error(fmt.Errorf("Invalid signature: %w", err), http.StatusUnauthorized)
		return
	}

	// Parse the webhook payload
	var event hyperswitch.WebhookEvent
	if err := json.Unmarshal(body, &event); err != nil {
		a.logger.Error("failed to parse webhook payload", zap.Error(err))
		_ = ctx.Error(fmt.Errorf("Invalid request body: %w", err), http.StatusBadRequest)
		return
	}

	err = a.billingService.HandleWebhook(body)
	if err != nil {
		_ = ctx.Error(err, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func verifyWebhookSignature(payload []byte, signature string, secretKey string) error {
	// Generate HMAC-SHA512 signature using the raw payload
	mac := hmac.New(sha512.New, []byte(secretKey))
	mac.Write(payload)
	expectedSignature := hex.EncodeToString(mac.Sum(nil))

	// Compare signatures using constant-time comparison
	if !hmac.Equal([]byte(signature), []byte(expectedSignature)) {
		return fmt.Errorf("signature mismatch")
	}
	return nil
}
