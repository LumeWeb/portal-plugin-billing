package messages

type SubscriptionPlanPeriod string

const (
	SubscriptionPlanPeriodMonth SubscriptionPlanPeriod = "MONTH"
	SubscriptionPlanPeriodYear  SubscriptionPlanPeriod = "YEAR"
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
}

type SubscriptionResponse struct {
	Plan        *SubscriptionPlan `json:"plan"`
	BillingInfo BillingInfo       `json:"billing_info"`
}

type BillingInfo struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	City    string `json:"city"`
	State   string `json:"state"`
	Zip     string `json:"zip"`
	Country string `json:"country"`
}
