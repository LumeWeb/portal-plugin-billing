package core

// SubscriptionChangeType represents the type of subscription change operation
type SubscriptionChangeType string

const (
	// ChangeTypeNewSubscription indicates a new subscription being created
	ChangeTypeNewSubscription SubscriptionChangeType = "new_subscription"
	// ChangeTypeUpgrade indicates an upgrade to a higher-priced plan
	ChangeTypeUpgrade SubscriptionChangeType = "upgrade"
	// ChangeTypeDowngrade indicates a downgrade to a lower-priced plan
	ChangeTypeDowngrade SubscriptionChangeType = "downgrade"
	// ChangeTypeCancel indicates subscription cancellation
	ChangeTypeCancel SubscriptionChangeType = "cancel"
	// ChangeTypeRenewal indicates a subscription renewal
	ChangeTypeRenewal SubscriptionChangeType = "renewal"
)
