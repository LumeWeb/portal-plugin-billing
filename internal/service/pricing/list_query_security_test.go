package pricing

import (
	"testing"

	"github.com/stretchr/testify/require"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
	"go.lumeweb.com/queryutil"
	"go.lumeweb.com/queryutil/filter"
)

func TestPricingLists_RejectUnknownFields(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		svc := core.GetService[pluginCore.PricingService](ctx, pluginCore.PRICING_SERVICE)
		for _, query := range []struct {
			filters []queryutil.CrudFilter
			sorts   []queryutil.Sort
		}{
			{filters: []queryutil.CrudFilter{queryutil.FieldEqual("not_a_pricing_column", 1)}},
			{filters: []queryutil.CrudFilter{queryutil.NewConditionalFilter(filter.LogicalOr, []queryutil.CrudFilter{queryutil.FieldEqual("id) OR 1=1 --", 1)})}},
			{filters: []queryutil.CrudFilter{(*filter.LogicalFilter)(nil)}},
			{sorts: []queryutil.Sort{{Field: "(SELECT 1)", Order: filter.OrderAsc}}},
			{sorts: []queryutil.Sort{{Field: "id", Order: filter.SortOrder("asc; SELECT 1")}}},
		} {
			plans, total, err := svc.GetPricingPlans(ctx, 0, query.filters, query.sorts, queryutil.DefaultPagination)
			require.ErrorIs(tb, err, filter.ErrInvalidQuery)
			require.Empty(tb, plans)
			require.Zero(tb, total)
			lines, total, err := svc.GetPriceLines(ctx, 0, query.filters, query.sorts, queryutil.DefaultPagination)
			require.ErrorIs(tb, err, filter.ErrInvalidQuery)
			require.Empty(tb, lines)
			require.Zero(tb, total)
			periods, total, err := svc.GetPricingPlanPeriodsWithFilter(ctx, query.filters, query.sorts, queryutil.DefaultPagination)
			require.ErrorIs(tb, err, filter.ErrInvalidQuery)
			require.Empty(tb, periods)
			require.Zero(tb, total)
		}
	}, getPricingTestOptions())
}
func TestPricingLists_PropagateDatabaseErrors(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		svc := core.GetService[pluginCore.PricingService](ctx, pluginCore.PRICING_SERVICE)
		for _, table := range []string{"billing_pricing_plans", "billing_pricelines", "billing_pricing_plan_periods"} {
			require.NoError(tb, ctx.DB().Exec("DROP TABLE "+table).Error)
		}
		_, _, err := svc.GetPricingPlans(ctx, 0, nil, nil, queryutil.DefaultPagination)
		require.Error(tb, err)
		_, _, err = svc.GetPriceLines(ctx, 0, nil, nil, queryutil.DefaultPagination)
		require.Error(tb, err)
		_, _, err = svc.GetPricingPlanPeriodsWithFilter(ctx, nil, nil, queryutil.DefaultPagination)
		require.Error(tb, err)
	}, getPricingTestOptions())
}
