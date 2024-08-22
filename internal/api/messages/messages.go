package messages

type SubscriptionPlansResponse struct {
	Plans []SubscriptionPlan `json:"plans"`
}

type SubscriptionPlan struct {
	ID       uint    `json:"id"`
	Name     string  `json:"name"`
	Price    float64 `json:"price"`
	Storage  uint64  `json:"storage"`
	Upload   uint64  `json:"upload"`
	Download uint64  `json:"download"`
}
