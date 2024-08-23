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
	ID       uint                   `json:"id"`
	Name     string                 `json:"name"`
	Price    float64                `json:"price"`
	Period   SubscriptionPlanPeriod `json:"period"`
	Storage  uint64                 `json:"storage"`
	Upload   uint64                 `json:"upload"`
	Download uint64                 `json:"download"`
}
