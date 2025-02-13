package messages

import (
	"github.com/go-openapi/strfmt"
	"time"
)

type (
	SubscriptionPlanPeriod string
	SubscriptionPlanStatus string
)

const (
	SubscriptionPlanPeriodMonth SubscriptionPlanPeriod = "MONTH"
	SubscriptionPlanPeriodYear  SubscriptionPlanPeriod = "YEAR"
)

const (
	SubscriptionPlanStatusActive  SubscriptionPlanStatus = "ACTIVE"
	SubscriptionPlanStatusPending SubscriptionPlanStatus = "PENDING"
)

type SubscriptionPlansResponse struct {
	Plans []*SubscriptionPlan `json:"plans"`
}

type SubscriptionPlan struct {
	Name       string                 `json:"name"`
	Identifier string                 `json:"identifier"`
	Price      float64                `json:"price"`
	Period     SubscriptionPlanPeriod `json:"period"`
	Storage    uint64                 `json:"storage"`
	Upload     uint64                 `json:"upload"`
	Download   uint64                 `json:"download"`
	Status     SubscriptionPlanStatus `json:"status"`
	StartDate  *strfmt.DateTime       `json:"start_date,omitempty"`
	IsFree     bool                   `json:"is_free,omitempty"`
}
type SubscriptionResponse struct {
	Plan        *SubscriptionPlan `json:"plan"`
	BillingInfo BillingInfo       `json:"billing_info"`
	PaymentInfo PaymentInfo       `json:"payment_info"`
}

type BillingInfo struct {
	Name              string `json:"name"`
	Organization      string `json:"organization"`
	Address           string `json:"address"`
	DependentLocality string `json:"dependent_locality"`
	SortingCode       string `json:"sorting_code"`
	City              string `json:"city"`
	State             string `json:"state"`
	Zip               string `json:"zip"`
	Country           string `json:"country"`
}

type PaymentInfo struct {
	PaymentID      string    `json:"payment_id,omitempty"`
	PaymentExpires time.Time `json:"payment_expires,omitempty"`
	ClientSecret   string    `json:"client_secret,omitempty"`
	PublishableKey string    `json:"publishable_key,omitempty"`
}

type SubscriptionChangeRequest struct {
	Plan string `json:"plan"`
}

type SubscriptionChangeResponse struct {
	Plan *SubscriptionPlan `json:"plan"`
}

type SubscriptionConnectRequest struct {
	PaymentMethodID string `json:"payment_method_id"`
}

type EphemeralKeyResponse struct {
	Key string `json:"key"`
}

type RequestPaymentMethodChangeResponse struct {
	ClientSecret string `json:"client_secret"`
}

type UsageData struct {
	Date  time.Time `json:"date"`
	Usage uint64    `json:"usage"`
}

type CurrentUsageResponse struct {
	Upload   uint64 `json:"upload"`
	Download uint64 `json:"download"`
	Storage  uint64 `json:"storage"`
}

type ListBillingCountriesResponse = []*ListBillingCountriesResponseItem

type ListBillingCountriesResponseItem struct {
	Name              string   `json:"name"`
	Code              string   `json:"code"`
	SupportedEntities []string `json:"supported_entities,omitempty"`
}

type ListBillingStatesResponse = []*ListBillingStatesResponseItem

type ListBillingStatesResponseItem struct {
	Name string `json:"name"`
	Code string `json:"code"`
}

type ListBillingCitiesResponse = []*ListBillingCitiesResponseItem

type ListBillingCitiesResponseItem struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type UpdateBillingInfoResponseError struct {
	Errors []*UpdateBillingInfoResponseErrorItem `json:"errors"`
}

type UpdateBillingInfoResponseErrorItem struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}
