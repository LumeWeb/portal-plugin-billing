package dto

import (
	"time"

	z "github.com/Oudwins/zog"
	"go.lumeweb.com/httputil"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
)

var (
	_ httputil.DTOResponse[*pluginCore.Subscriber] = (*SubscriptionStatusResponse)(nil)
	_ httputil.DTOValidator                        = (*CustomerPortalRequest)(nil)
	_ httputil.DTORequest[*CustomerPortalRequest]  = (*CustomerPortalRequest)(nil)
)

// SubscriptionStatusResponse represents the subscription status response
type SubscriptionStatusResponse struct {
	IsSubscribed bool       `json:"is_subscribed"`
	GatewayType  string     `json:"gateway_type,omitempty"`
	PlanID       *uint      `json:"plan_id,omitempty"`
	CreatedAt    *time.Time `json:"created_at,omitempty"`
	UpdatedAt    *time.Time `json:"updated_at,omitempty"`
}

// FromModel converts a Subscriber model to SubscriptionStatusResponse
func (r *SubscriptionStatusResponse) FromModel(subscriber *pluginCore.Subscriber) error {
	// Reset to zero to avoid stale fields when DTO is reused
	*r = SubscriptionStatusResponse{}
	if subscriber == nil {
		return nil
	}

	if !subscriber.IsActive {
		return nil
	}

	r.IsSubscribed = true
	r.GatewayType = subscriber.GatewayType
	r.PlanID = subscriber.PlanID
	r.CreatedAt = &subscriber.CreatedAt
	r.UpdatedAt = &subscriber.UpdatedAt

	return nil
}

// CustomerPortalRequest represents the request to create a customer portal session
type CustomerPortalRequest struct {
	ReturnURL string `json:"return_url"`
}

func (r *CustomerPortalRequest) Schema() *z.StructSchema {
	return z.Struct(z.Shape{
		"ReturnURL": z.String().Required().URL(),
	})
}

func (r *CustomerPortalRequest) ToModel() (*CustomerPortalRequest, error) {
	return r, nil
}

// CustomerPortalResponse represents the customer portal URL response
type CustomerPortalResponse struct {
	URL string `json:"url"`
}
