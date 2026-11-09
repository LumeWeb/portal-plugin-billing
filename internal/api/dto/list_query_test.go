package dto

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.lumeweb.com/queryutil"
	"go.lumeweb.com/queryutil/filter"
)

func TestListQueryPolicies_RejectUnsafeFieldsSortsAndTrees(t *testing.T) {
	for _, tc := range []struct {
		name   string
		policy ListQueryPolicy
	}{
		{"plans", PricingPlanQueryPolicy()}, {"lines", PriceLineQueryPolicy()},
		{"periods", PricingPeriodQueryPolicy()}, {"credits", CreditQueryPolicy()},
		{"user credits", UserCreditQueryPolicy()}, {"subscribers", SubscriberQueryPolicy()},
		{"collections", CollectionQueryPolicy()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, field := range []string{"id) OR 1=1 --", "not_a_column", "metadata.secret", "PricingPlan.name", "q"} {
				_, err := tc.policy.Normalize([]queryutil.CrudFilter{queryutil.FieldEqual(field, "1")}, nil)
				require.ErrorIs(t, err, filter.ErrInvalidQuery)
				_, err = tc.policy.Normalize([]queryutil.CrudFilter{queryutil.NewConditionalFilter(filter.LogicalOr, []queryutil.CrudFilter{queryutil.FieldEqual(field, "1")})}, nil)
				require.ErrorIs(t, err, filter.ErrInvalidQuery)
				_, err = tc.policy.Normalize(nil, []queryutil.Sort{{Field: field, Order: filter.OrderAsc}})
				require.ErrorIs(t, err, filter.ErrInvalidQuery)
			}
			for _, sorts := range [][]queryutil.Sort{
				{{Field: "id", Order: filter.SortOrder("asc; SELECT 1")}},
				{{Field: "(SELECT 1)", Order: filter.OrderAsc}},
			} {
				_, err := tc.policy.Normalize(nil, sorts)
				require.ErrorIs(t, err, filter.ErrInvalidQuery)
			}
			cycle := &filter.ConditionalFilter{Operator: filter.LogicalAnd}
			cycle.Filters = []queryutil.CrudFilter{cycle}
			for _, node := range []queryutil.CrudFilter{nil, (*filter.LogicalFilter)(nil), cycle} {
				_, err := tc.policy.Normalize([]queryutil.CrudFilter{node}, nil)
				require.ErrorIs(t, err, filter.ErrInvalidQuery)
			}
		})
	}
}

func TestListQueryPolicies_AllowModelFieldsAndNestedGroups(t *testing.T) {
	for _, tc := range []struct {
		policy ListQueryPolicy
		field  string
		op     filter.Operator
		value  any
	}{
		{PricingPlanQueryPolicy(), "is_public", filter.OpEq, false},
		{PriceLineQueryPolicy(), "name", filter.OpContains, "Standard"},
		{PricingPeriodQueryPolicy(), "price_usd", filter.OpBetween, []any{1, 20}},
		{CreditQueryPolicy(), "reference_id", filter.OpIn, []any{"000001", "true"}},
		{UserCreditQueryPolicy(), "amount", filter.OpGt, 10},
		{UserCreditQueryPolicy(), "created_at", filter.OpGte, "2026-01-01"},
		{SubscriberQueryPolicy(), "subscription_id", filter.OpEq, "000001"},
	} {
		leaf := queryutil.NewLogicalFilter(tc.field, tc.op, tc.value)
		input := []queryutil.CrudFilter{queryutil.NewConditionalFilter(filter.LogicalOr, []queryutil.CrudFilter{leaf})}
		normalized, err := tc.policy.Normalize(input, []queryutil.Sort{{Field: tc.field, Order: filter.OrderDesc}})
		require.NoError(t, err)
		require.Equal(t, tc.value, normalized[0].GetFilters()[0].GetValue())
		require.NotSame(t, leaf, normalized[0].GetFilters()[0])
	}
}

func TestCreditQueryPolicy_UsesScalarValueSemantics(t *testing.T) {
	filters, err := CreditQueryPolicy().Normalize([]queryutil.CrudFilter{queryutil.FieldEqual("reference_id", 1)}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, filters[0].GetValue())

	_, err = CreditQueryPolicy().Normalize([]queryutil.CrudFilter{queryutil.NewLogicalFilter("reference_id", filter.OpIn, []any{})}, nil)
	require.ErrorIs(t, err, filter.ErrInvalidQuery)
}

func TestUserCreditQueryPolicy_ExcludesInternalFields(t *testing.T) {
	for _, field := range []string{"user_id", "reference_id", "reference_type", "metadata", "created_by", "deleted_at"} {
		_, err := UserCreditQueryPolicy().Normalize([]queryutil.CrudFilter{queryutil.FieldEqual(field, "1")}, nil)
		require.ErrorIs(t, err, filter.ErrInvalidQuery)
	}
	_, err := PricingPlanQueryPolicy().Normalize([]queryutil.CrudFilter{queryutil.NewLogicalFilter("is_public", filter.OpContains, "true")}, nil)
	require.ErrorIs(t, err, filter.ErrInvalidQuery)
}
