package dto

import (
	"time"

	"go.lumeweb.com/httputil"
	billingModels "go.lumeweb.com/portal-plugin-billing/internal/db/models"
)

var (
	_ httputil.DTOResponse[*billingModels.Subscriber] = (*SubscriptionStatusResponse)(nil)
)

// SubscriptionStatusResponse represents the subscription status response
type SubscriptionStatusResponse struct {
	IsSubscribed bool      `json:"is_subscribed"`
	GatewayType  string    `json:"gateway_type,omitempty"`
	PlanID       *uint     `json:"plan_id,omitempty"`
	CreatedAt    time.Time `json:"created_at,omitempty"`
	UpdatedAt    time.Time `json:"updated_at,omitempty"`
}

// FromModel converts a Subscriber model to SubscriptionStatusResponse
func (r *SubscriptionStatusResponse) FromModel(subscriber *billingModels.Subscriber) error {
	if subscriber == nil {
		r.IsSubscribed = false
		return nil
	}

	r.IsSubscribed = subscriber.IsActive
	r.GatewayType = subscriber.GatewayType
	r.PlanID = subscriber.PlanID
	r.CreatedAt = subscriber.CreatedAt
	r.UpdatedAt = subscriber.UpdatedAt

	return nil
}
