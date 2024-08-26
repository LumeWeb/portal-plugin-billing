package messages

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
}
type SubscriptionResponse struct {
	Plan        *SubscriptionPlan `json:"plan"`
	BillingInfo BillingInfo       `json:"billing_info"`
	PaymentInfo PaymentInfo       `json:"payment_info"`
}

type BillingInfo struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	City    string `json:"city"`
	State   string `json:"state"`
	Zip     string `json:"zip"`
	Country string `json:"country"`
}

type PaymentInfo struct {
	PaymentID      string `json:"payment_id,omitempty"`
	ClientSecret   string `json:"client_secret,omitempty"`
	PublishableKey string `json:"publishable_key,omitempty"`
}

type SubscriptionChangeRequest struct {
	Plan string `json:"plan"`
}

type SubscriptionChangeResponse struct {
	Plan *SubscriptionPlan `json:"plan"`
}
