package api

import (
	"encoding/json"
	"fmt"
	"github.com/go-openapi/strfmt"
	"github.com/killbill/kbcli/v3/kbclient/subscription"
	"go.lumeweb.com/portal-plugin-billing/internal/api/messages"
	"go.lumeweb.com/portal/core"
	"go.uber.org/zap"
	"io"
	"net/http"
)

func (a *API) handleWebhook(w http.ResponseWriter, r *http.Request) {
    // TODO: Use KillBill API client to handle webhook
    // Pseudocode:
    // 1. Parse webhook payload
    // 2. Call appropriate KillBill API method based on webhook type
    // 3. Return appropriate response
    
    // Example:
    // params := &webhook.ProcessHyperswitchWebhookParams{
    //     Body: payload,
    //     XKillbillCreatedBy: "system",
    // }
    // _, err := a.billingService.api.Webhook.ProcessHyperswitchWebhook(r.Context(), params)
    
    http.Error(w, "Not implemented", http.StatusNotImplemented)
}

func (a *API) handlePaymentSucceeded(ctx core.Context, event *messages.WebhookEvent) error {
	// Extract subscription ID from metadata
	subscriptionID := event.Data.Metadata["subscription_id"]
	if subscriptionID == "" {
		return fmt.Errorf("missing subscription_id in metadata")
	}

	// Update subscription status
	_, err := a.billingService.(*service.BillingServiceDefault).api.Subscription.UpdateSubscription(ctx, &subscription.UpdateSubscriptionParams{
		SubscriptionID: strfmt.UUID(subscriptionID),
	})
	if err != nil {
		return fmt.Errorf("failed to update subscription: %w", err)
	}

	return nil
}

func (a *API) handlePaymentFailed(ctx core.Context, event *messages.WebhookEvent) error {
	// Extract subscription ID from metadata
	subscriptionID := event.Data.Metadata["subscription_id"]
	if subscriptionID == "" {
		return fmt.Errorf("missing subscription_id in metadata")
	}

	// Cancel the subscription
	_, err := a.billingService.(*service.BillingServiceDefault).api.Subscription.CancelSubscriptionPlan(ctx, &subscription.CancelSubscriptionPlanParams{
		SubscriptionID: strfmt.UUID(subscriptionID),
	})
	if err != nil {
		return fmt.Errorf("failed to cancel subscription: %w", err)
	}

	return nil
}

func (a *API) verifyWebhookSignature(r *http.Request, signature string) bool {
	if signature == "" {
		a.logger.Warn("Missing webhook signature")
		return false
	}

	// Get webhook secret from config
	webhookSecret := a.billingService.(*service.BillingServiceDefault).cfg.Hyperswitch.WebhookSecret
	if webhookSecret == "" {
		a.logger.Error("Webhook secret not configured")
		return false
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		a.logger.Error("Failed to read request body for signature verification", zap.Error(err))
		return false
	}

	// Important: Restore request body for later use
	r.Body = io.NopCloser(bytes.NewBuffer(body))

	// Verify HMAC signature
	expectedMAC := hmac.New(sha256.New, []byte(webhookSecret))
	expectedMAC.Write(body)
	expectedSig := hex.EncodeToString(expectedMAC.Sum(nil))

	return hmac.Equal([]byte(signature), []byte(expectedSig))
}
var ErrRetryable = errors.New("retryable error")


func (a *API) handlePaymentExpired(ctx context.Context, event *messages.WebhookEvent) error {
	subscriptionID := event.Data.Metadata["subscription_id"]
	if subscriptionID == "" {
		return fmt.Errorf("missing subscription_id in metadata")
	}

	// Clear the payment ID custom field
	err := a.billingService.(*service.BillingServiceDefault).deleteCustomField(ctx, strfmt.UUID(subscriptionID), "payment_id")
	if err != nil {
		return fmt.Errorf("failed to clear payment ID: %w", err)
	}

	return nil
}

func (a *API) handlePaymentCancelled(ctx context.Context, event *messages.WebhookEvent) error {
	subscriptionID := event.Data.Metadata["subscription_id"]
	if subscriptionID == "" {
		return fmt.Errorf("missing subscription_id in metadata")
	}

	// Get subscription details
	sub, err := a.billingService.(*service.BillingServiceDefault).api.Subscription.GetSubscription(ctx, &subscription.GetSubscriptionParams{
		SubscriptionID: strfmt.UUID(subscriptionID),
	})
	if err != nil {
		return fmt.Errorf("failed to get subscription: %w", err)
	}

	// Only handle active subscriptions
	if sub.Payload.State != kbmodel.SubscriptionStateACTIVE {
		a.logger.Info("ignoring cancellation for non-active subscription",
			zap.String("subscription_id", subscriptionID),
			zap.String("state", string(sub.Payload.State)))
		return nil
	}

	// Cancel subscription immediately
	policy := kbmodel.SubscriptionCancellationPolicyIMMEDIATE
	_, err = a.billingService.(*service.BillingServiceDefault).api.Subscription.CancelSubscriptionPlan(ctx, &subscription.CancelSubscriptionPlanParams{
		SubscriptionID: strfmt.UUID(subscriptionID),
		Body: &kbmodel.Subscription{
			CancelPolicy: &policy,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to cancel subscription: %w", err)
	}

	// Store cancellation reason
	err = a.billingService.(*service.BillingServiceDefault).setCustomField(ctx, strfmt.UUID(subscriptionID), "cancellation_reason", "payment_cancelled")
	if err != nil {
		a.logger.Error("failed to store cancellation reason",
			zap.Error(err),
			zap.String("subscription_id", subscriptionID))
		// Don't fail the webhook for this
	}

	a.logger.Info("subscription cancelled due to payment cancellation",
		zap.String("subscription_id", subscriptionID))

	return nil
}

func (a *API) handleRefundSucceeded(ctx context.Context, event *messages.WebhookEvent) error {
	subscriptionID := event.Data.Metadata["subscription_id"]
	if subscriptionID == "" {
		return fmt.Errorf("missing subscription_id in metadata")
	}

	// Update subscription status to reflect refund
	_, err := a.billingService.(*service.BillingServiceDefault).api.Subscription.UpdateSubscription(ctx, &subscription.UpdateSubscriptionParams{
		SubscriptionID: strfmt.UUID(subscriptionID),
		Body: &kbmodel.Subscription{
			BillingEndDate: strfmt.DateTime(time.Now()),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to update subscription: %w", err)
	}

	return nil
}
