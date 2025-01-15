package messages

import (
	"time"
)

// Core types
type (
	PlanPeriod string
	PlanStatus string
)

const (
	PeriodMonthly PlanPeriod = "MONTHLY"
	PeriodYearly  PlanPeriod = "YEARLY"

	StatusActive   PlanStatus = "ACTIVE"
	StatusPending  PlanStatus = "PENDING"
	StatusCanceled PlanStatus = "CANCELED"
)

// Plan structures
type Plan struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Period    PlanPeriod `json:"period"`
	Price     float64    `json:"price"`
	IsFree    bool       `json:"is_free"`
	Resources Resources  `json:"resources"`
}

type Resources struct {
	Storage  uint64 `json:"storage"`  // In bytes
	Upload   uint64 `json:"upload"`   // In bytes
	Download uint64 `json:"download"` // In bytes
}

// Subscription structures
type Subscription struct {
	ID            string     `json:"id"`
	Plan          *Plan      `json:"plan"`
	Status        PlanStatus `json:"status"`
	CurrentPeriod Period     `json:"current_period"`
	Billing       *Billing   `json:"billing,omitempty"`
	Payment       *Payment   `json:"payment,omitempty"`
}

type Period struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// Payment structures
type Payment struct {
	ClientSecret   string    `json:"client_secret,omitempty"`
	PublishableKey string    `json:"publishable_key,omitempty"`
	ExpiresAt      time.Time `json:"expires_at,omitempty"`
}

// Billing structures
type Billing struct {
	Name         string  `json:"name"`
	Organization string  `json:"organization,omitempty"`
	Address      Address `json:"address"`
}

type Address struct {
	Line1             string `json:"line1"`
	Line2             string `json:"line2,omitempty"`
	City              string `json:"city"`
	State             string `json:"state"`
	PostalCode        string `json:"postal_code"`
	Country           string `json:"country"`
	DependentLocality string `json:"dependent_locality,omitempty"`
	SortingCode       string `json:"sorting_code,omitempty"`
}

// Location reference data
type Location struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type Country struct {
	Location
	SupportedFields []string `json:"supported_fields,omitempty"`
}

// API Requests
type CreateSubscriptionRequest struct {
	PlanID string `json:"plan_id"`
}

type UpdateSubscriptionRequest struct {
	PlanID string `json:"plan_id"`
}

type UpdateBillingRequest struct {
	Billing
}

type UpdatePaymentRequest struct {
	PaymentMethodID string `json:"payment_method_id"`
}

type CancelSubscriptionRequest struct {
	Reason    string `json:"reason"`
	EndOfTerm bool   `json:"end_of_term"`
}

// API Responses
type GetPlansResponse struct {
	Plans []*Plan `json:"plans"`
}

type SubscriptionResponse struct {
	Subscription Subscription `json:"subscription"`
}

type ErrorResponse struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Details map[string]string `json:"details,omitempty"`
}

// Usage structures
type Usage struct {
	Current Resources    `json:"current"`
	History []UsagePoint `json:"history,omitempty"`
}

type UsagePoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     uint64    `json:"value"`
}
