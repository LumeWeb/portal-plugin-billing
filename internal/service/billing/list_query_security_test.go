package billing

import (
	"testing"

	"github.com/stretchr/testify/require"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
	"go.lumeweb.com/queryutil"
	"go.lumeweb.com/queryutil/filter"
)

func TestListSubscribers_RejectsUnknownFields(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		svc := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		for _, query := range []struct {
			filters []queryutil.CrudFilter
			sorts   []queryutil.Sort
		}{
			{filters: []queryutil.CrudFilter{queryutil.FieldEqual("not_a_subscriber_column", 1)}},
			{filters: []queryutil.CrudFilter{queryutil.NewConditionalFilter(filter.LogicalOr, []queryutil.CrudFilter{queryutil.FieldEqual("id) OR 1=1 --", 1)})}},
			{filters: []queryutil.CrudFilter{(*filter.LogicalFilter)(nil)}},
			{sorts: []queryutil.Sort{{Field: "(SELECT 1)", Order: filter.OrderAsc}}},
			{sorts: []queryutil.Sort{{Field: "id", Order: filter.SortOrder("asc; SELECT 1")}}},
		} {
			rows, total, err := svc.ListSubscribers(ctx, query.filters, query.sorts, queryutil.DefaultPagination)
			require.ErrorIs(tb, err, filter.ErrInvalidQuery)
			require.Empty(tb, rows)
			require.Zero(tb, total)
		}
	}, getBillingTestOptions())
}
func TestListSubscribers_PropagatesCountErrors(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		svc := core.GetService[pluginCore.BillingService](ctx, pluginCore.BILLING_SERVICE)
		require.NoError(tb, ctx.DB().Exec("DROP TABLE billing_subscribers").Error)
		_, _, err := svc.ListSubscribers(ctx, nil, nil, queryutil.DefaultPagination)
		require.Error(tb, err)
	}, getBillingTestOptions())
}
