package dto

import (
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
)

// CheckoutUIRequest represents a request for checkout UI fragments
type CheckoutUIRequest struct {
	PlanID      uint   `json:"plan_id" validate:"required"`
	PeriodID    uint   `json:"period_id" validate:"required"`
	GatewayType string `json:"gateway_type,omitempty"` // e.g., "stripe", "paypal"
}

// CheckoutSessionStatusResponse represents the status of a checkout session.
// Used by embedded checkout return pages to verify payment completion.
type CheckoutSessionStatusResponse struct {
	SessionID     string `json:"session_id"`     // Gateway session identifier (e.g., cs_xxx for Stripe)
	Status        string `json:"status"`         // Session status: 'open', 'complete', or 'expired'
	CustomerEmail string `json:"customer_email"` // Customer email if available
	UserID        uint   `json:"user_id"`        // User ID from ClientReferenceID (for verification)
}

// FromModel converts core.SessionStatus to CheckoutSessionStatusResponse.
func (r *CheckoutSessionStatusResponse) FromModel(source *pluginCore.SessionStatus) error {
	if source == nil {
		return nil
	}

	*r = CheckoutSessionStatusResponse{
		SessionID:     source.SessionID,
		Status:        source.Status,
		CustomerEmail: source.CustomerEmail,
		UserID:        source.UserID,
	}

	return nil
}
