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

	// Verify the webhook signature
	signature := r.Header.Get(hyperswitchSignatureHeader)
	cfg := a.ctx.Config().GetService(service.BILLING_SERVICE).(*pluginConfig.BillingConfig)

	// Verify raw payload signature
	if err := verifyRawSignature(body, signature, cfg.Hyperswitch.WebhookSecret); err != nil {
		a.logger.Error("raw signature verification failed",
			zap.Error(err),
			zap.String("signature", signature))

		_ = ctx.Error(fmt.Errorf("Invalid signature: %w", err), http.StatusUnauthorized)
		return
	}

	// Verify JSON-encoded signature
	if err := verifyJSONSignature(body, signature, cfg.Hyperswitch.WebhookSecret); err != nil {
		a.logger.Error("JSON signature verification failed",
			zap.Error(err),
			zap.String("signature", signature))
		_ = ctx.Error(fmt.Errorf("Invalid signature: %w", err), http.StatusUnauthorized)
		return
	}

	// Parse the webhook payload
	var event hyperswitch.WebhookEvent
	if err := json.Unmarshal(body, &event); err != nil {
		a.logger.Error("failed to parse webhook payload", zap.Error(err))
		_ = ctx.Error(fmt.Errorf("Invalid request body: %w", err), http.StatusUnauthorized)
		return
	}

	err = a.billingService.HandleWebhook(body)
	if err != nil {
		_ = ctx.Error(err, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func verifyRawSignature(payload []byte, signature string, secretKey string) error {
	mac := hmac.New(sha512.New, []byte(secretKey))
	mac.Write(payload)
	expectedSignature := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(signature), []byte(expectedSignature)) {
		return fmt.Errorf("raw signature mismatch")
	}
	return nil
}

// verifyJSONSignature verifies the signature using JSON-encoded payload
func verifyJSONSignature(payload []byte, signature string, secretKey string) error {
	// Parse and re-encode to get canonical JSON
	var jsonData interface{}
	if err := json.Unmarshal(payload, &jsonData); err != nil {
		return fmt.Errorf("invalid JSON payload: %w", err)
	}

	canonicalJSON, err := json.Marshal(jsonData)
	if err != nil {
		return fmt.Errorf("failed to re-encode JSON: %w", err)
	}

	mac := hmac.New(sha512.New, []byte(secretKey))
	mac.Write(canonicalJSON)
	expectedSignature := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(signature), []byte(expectedSignature)) {
		return fmt.Errorf("JSON signature mismatch")
	}
	return nil
}
