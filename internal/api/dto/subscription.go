package dto

import (
	"time"

	"go.lumeweb.com/httputil"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	z "github.com/Oudwins/zog"
)

var (
	_ httputil.DTOResponse[*pluginCore.Subscriber] = (*SubscriptionStatusResponse)(nil)
	_ httputil.DTOResponse[*pluginCore.Subscriber] = (*SubscriberResponse)(nil)
)

// SubscriptionStatusResponse represents the subscription status response
type SubscriptionStatusResponse struct {
	IsSubscribed        bool       `json:"is_subscribed"`
	GatewayType         string     `json:"gateway_type,omitempty"`
	PricingPlanPeriodID *uint      `json:"pricing_plan_period_id,omitempty"`
	CreatedAt           *time.Time `json:"created_at,omitempty"`
	UpdatedAt           *time.Time `json:"updated_at,omitempty"`
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
	r.PricingPlanPeriodID = subscriber.PricingPlanPeriodID
	r.CreatedAt = &subscriber.CreatedAt
	r.UpdatedAt = &subscriber.UpdatedAt

	return nil
}

// SubscriberItem represents a subscriber item for list responses with minimal fields
type SubscriberItem struct {
	ID                  uint        `json:"id"`
	UserID              uint        `json:"user_id"`
	GatewayType         string      `json:"gateway_type"`
	ExternalID          string      `json:"external_id"`
	SubscriptionID      string      `json:"subscription_id"`
	IsActive            bool        `json:"is_active"`
	PricingPlanPeriodID *uint       `json:"pricing_plan_period_id,omitempty"`
	BillingPeriodStart  *time.Time  `json:"billing_period_start,omitempty"`
	BillingPeriodEnd    *time.Time  `json:"billing_period_end,omitempty"`
	PaymentStatus       string      `json:"payment_status,omitempty"`
	WillCancelAt        *time.Time  `json:"will_cancel_at,omitempty"`
	CancelledAt         *time.Time  `json:"cancelled_at,omitempty"`
	PreviousPlanID      *uint       `json:"previous_plan_id,omitempty"`
	CreatedAt           time.Time   `json:"created_at"`
	UpdatedAt           time.Time   `json:"updated_at"`
}

// SubscribersListResponse represents a paginated list of subscribers
type SubscribersListResponse struct {
	Results []SubscriberItem `json:"results"`
	Total   int64            `json:"total"`
}

// FromModel converts a Subscriber model to SubscriberItem
func (r *SubscriberItem) FromModel(subscriber *pluginCore.Subscriber) error {
	if subscriber == nil {
		return nil
	}

	r.ID = subscriber.ID
	r.UserID = subscriber.UserID
	r.GatewayType = subscriber.GatewayType
	r.ExternalID = subscriber.ExternalID
	r.SubscriptionID = subscriber.SubscriptionID
	r.IsActive = subscriber.IsActive
	r.PricingPlanPeriodID = subscriber.PricingPlanPeriodID
	r.BillingPeriodStart = subscriber.BillingPeriodStart
	r.BillingPeriodEnd = subscriber.BillingPeriodEnd
	r.PaymentStatus = subscriber.PaymentStatus
	r.WillCancelAt = subscriber.WillCancelAt
	r.CancelledAt = subscriber.CancelledAt
	r.PreviousPlanID = subscriber.PreviousPlanID
	r.CreatedAt = subscriber.CreatedAt
	r.UpdatedAt = subscriber.UpdatedAt

	return nil
}


// SubscriberCreateRequest represents a request to create a subscriber
type SubscriberCreateRequest struct {
	UserID              uint       `json:"user_id"`
	GatewayType         string     `json:"gateway_type" validate:"required"`
	ExternalID          string     `json:"external_id" validate:"required"`
	SubscriptionID      string     `json:"subscription_id"`
	IsActive            bool       `json:"is_active"`
	PricingPlanPeriodID *uint      `json:"pricing_plan_period_id"`
	BillingPeriodStart  *time.Time `json:"billing_period_start,omitempty"`
	BillingPeriodEnd    *time.Time `json:"billing_period_end,omitempty"`
	PaymentStatus       string     `json:"payment_status,omitempty"`
	WillCancelAt        *time.Time `json:"will_cancel_at,omitempty"`
	CancelledAt         *time.Time `json:"cancelled_at,omitempty"`
}

// Schema returns the validation schema for SubscriberCreateRequest
func (r SubscriberCreateRequest) Schema() *z.StructSchema {
	return z.Struct(z.Shape{
		"UserID":              z.UintLike[uint]().Required(),
		"GatewayType":         z.String().Required().Min(1).Max(255),
		"ExternalID":          z.String().Required().Min(1).Max(255),
		"SubscriptionID":      z.String().Min(1).Max(255),
		"IsActive":            z.Bool().Required(),
		"PricingPlanPeriodID": z.Ptr(z.UintLike[uint]()),
		"PaymentStatus":       z.String().Min(1).Max(50),
	})
}

// SubscriberUpdateRequest represents a request to update a subscriber
type SubscriberUpdateRequest struct {
	GatewayType         *string     `json:"gateway_type,omitempty"`
	ExternalID          *string     `json:"external_id,omitempty"`
	SubscriptionID      *string     `json:"subscription_id,omitempty"`
	IsActive            *bool       `json:"is_active,omitempty"`
	PricingPlanPeriodID *uint       `json:"pricing_plan_period_id,omitempty"`
	BillingPeriodStart  *time.Time  `json:"billing_period_start,omitempty"`
	BillingPeriodEnd    *time.Time  `json:"billing_period_end,omitempty"`
	PaymentStatus       *string     `json:"payment_status,omitempty"`
	WillCancelAt        *time.Time  `json:"will_cancel_at,omitempty"`
	CancelledAt         *time.Time  `json:"cancelled_at,omitempty"`
	PreviousPlanID      *uint       `json:"previous_plan_id,omitempty"`
}

// Schema returns the validation schema for SubscriberUpdateRequest
func (r SubscriberUpdateRequest) Schema() *z.StructSchema {
	return z.Struct(z.Shape{
		"GatewayType":         z.String().Min(1).Max(255),
		"ExternalID":          z.String().Min(1).Max(255),
		"SubscriptionID":      z.String().Min(1).Max(255),
		"IsActive":            z.Bool(),
		"PricingPlanPeriodID": z.Ptr(z.UintLike[uint]()),
		"PaymentStatus":       z.String().Min(1).Max(50),
		"PreviousPlanID":      z.Ptr(z.UintLike[uint]()),
	})
}

// SubscriberResponse represents a detailed subscriber response
type SubscriberResponse struct {
	ID                  uint        `json:"id"`
	UserID              uint        `json:"user_id"`
	GatewayType         string      `json:"gateway_type"`
	ExternalID          string      `json:"external_id"`
	SubscriptionID      string      `json:"subscription_id"`
	IsActive            bool        `json:"is_active"`
	PricingPlanPeriodID *uint       `json:"pricing_plan_period_id,omitempty"`
	BillingPeriodStart  *time.Time  `json:"billing_period_start,omitempty"`
	BillingPeriodEnd    *time.Time  `json:"billing_period_end,omitempty"`
	PaymentStatus       string      `json:"payment_status,omitempty"`
	WillCancelAt        *time.Time  `json:"will_cancel_at,omitempty"`
	CancelledAt         *time.Time  `json:"cancelled_at,omitempty"`
	PreviousPlanID      *uint       `json:"previous_plan_id,omitempty"`
	CreatedAt           time.Time   `json:"created_at"`
	UpdatedAt           time.Time   `json:"updated_at"`
}

// FromModel converts a Subscriber model to SubscriberResponse
func (r *SubscriberResponse) FromModel(subscriber *pluginCore.Subscriber) error {
	// Reset to zero to avoid stale fields when DTO is reused
	*r = SubscriberResponse{}
	if subscriber == nil {
		return nil
	}

	r.ID = subscriber.ID
	r.UserID = subscriber.UserID
	r.GatewayType = subscriber.GatewayType
	r.ExternalID = subscriber.ExternalID
	r.SubscriptionID = subscriber.SubscriptionID
	r.IsActive = subscriber.IsActive
	r.PricingPlanPeriodID = subscriber.PricingPlanPeriodID
	r.BillingPeriodStart = subscriber.BillingPeriodStart
	r.BillingPeriodEnd = subscriber.BillingPeriodEnd
	r.PaymentStatus = subscriber.PaymentStatus
	r.WillCancelAt = subscriber.WillCancelAt
	r.CancelledAt = subscriber.CancelledAt
	r.PreviousPlanID = subscriber.PreviousPlanID
	r.CreatedAt = subscriber.CreatedAt
	r.UpdatedAt = subscriber.UpdatedAt

	return nil
}
