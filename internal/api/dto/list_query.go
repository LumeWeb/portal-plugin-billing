package dto

import "go.lumeweb.com/queryutil/filter"

// Bound filter work at both HTTP and direct service boundaries.
const maxListFilterNodes = 64

// ListQueryPolicy owns the allowed database fields and HTTP string parsing rules.
// Normalize must also run at the database boundary to protect non-HTTP callers.
type ListQueryPolicy struct {
	filter.QueryPolicy
	StringFields []string
}

// HTTPPolicyWithReservedNodes limits client filters while leaving room for
// trusted scope filters appended before the service applies the full policy.
func (p ListQueryPolicy) HTTPPolicyWithReservedNodes(reserved int) filter.QueryPolicy {
	policy := p.QueryPolicy
	policy.MaxFilterNodes -= reserved
	return policy
}

func PricingPlanQueryPolicy() ListQueryPolicy {
	return listQueryPolicy(
		[]string{"id", "created_at", "updated_at"},
		[]string{"name", "description", "currency"},
		[]string{"is_active", "is_public"},
	)
}

func PriceLineQueryPolicy() ListQueryPolicy {
	return listQueryPolicy(
		[]string{"id", "created_at", "updated_at"},
		[]string{"name", "description"},
		[]string{"is_active", "is_default"},
	)
}

func PricingPeriodQueryPolicy() ListQueryPolicy {
	return listQueryPolicy(
		[]string{"id", "pricing_plan_id", "price_usd", "quota_plan_id", "rolling_days", "created_at", "updated_at"},
		[]string{"cadence"}, nil,
	)
}

func UserCreditQueryPolicy() ListQueryPolicy {
	return listQueryPolicy(
		[]string{"amount", "created_at"}, []string{"id", "type", "direction", "description"}, nil,
	)
}

func CreditQueryPolicy() ListQueryPolicy {
	return listQueryPolicy(
		[]string{"user_id", "amount", "created_by", "created_at", "updated_at", "deleted_at"},
		[]string{"id", "type", "direction", "description", "reference_id", "reference_type"}, nil,
	)
}

func SubscriberQueryPolicy() ListQueryPolicy {
	return listQueryPolicy(
		[]string{"id", "user_id", "pricing_plan_period_id", "previous_plan_id", "created_at", "updated_at", "billing_period_start", "billing_period_end", "will_cancel_at", "cancelled_at", "paused_at"},
		[]string{"gateway_type", "external_id", "subscription_id", "payment_status"},
		[]string{"is_active"},
	)
}

// CollectionQueryPolicy rejects filters and sorts on position-ordered or fixed
// collections whose backing operations do not support those query controls.
func CollectionQueryPolicy() ListQueryPolicy {
	return ListQueryPolicy{QueryPolicy: filter.QueryPolicy{MaxFilterNodes: maxListFilterNodes}}
}

func listQueryPolicy(comparisons, strings, booleans []string) ListQueryPolicy {
	fields := filter.AllScalarFieldRules(strings...)
	for field, rule := range filter.ComparableFieldRules(comparisons...) {
		rule.Operators = append(rule.Operators, filter.OpNull, filter.OpNnull)
		fields[field] = rule
	}
	for _, field := range booleans {
		fields[field] = filter.FieldRule{Operators: []filter.Operator{filter.OpEq, filter.OpNe}}
	}
	sorts := append(append(append([]string(nil), strings...), comparisons...), booleans...)
	return ListQueryPolicy{
		QueryPolicy:  filter.QueryPolicy{Fields: fields, SortFields: sorts, MaxFilterNodes: maxListFilterNodes},
		StringFields: strings,
	}
}
