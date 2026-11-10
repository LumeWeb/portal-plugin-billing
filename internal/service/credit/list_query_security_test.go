package credit

import (
	"testing"

	"github.com/stretchr/testify/require"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
	"go.lumeweb.com/queryutil"
	"go.lumeweb.com/queryutil/filter"
)

func TestCreditListing_RejectsUnsafeDirectQueries(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		svc := getCreditService(ctx)
		for _, tc := range []struct {
			filters []queryutil.CrudFilter
			sorts   []queryutil.Sort
		}{
			{filters: []queryutil.CrudFilter{queryutil.FieldEqual("id) OR 1=1 --", "1")}},
			{filters: []queryutil.CrudFilter{queryutil.NewConditionalFilter(filter.LogicalOr, []queryutil.CrudFilter{queryutil.FieldEqual("metadata.secret", "x")})}},
			{filters: []queryutil.CrudFilter{(*filter.LogicalFilter)(nil)}},
			{sorts: []queryutil.Sort{{Field: "(SELECT 1)", Order: filter.OrderAsc}}},
			{sorts: []queryutil.Sort{{Field: "id", Order: filter.SortOrder("asc; SELECT 1")}}},
		} {
			rows, total, err := svc.ListCredits(ctx, tc.filters, tc.sorts, queryutil.DefaultPagination)
			require.ErrorIs(tb, err, filter.ErrInvalidQuery)
			require.Empty(tb, rows)
			require.Zero(tb, total)
		}
	}, getCreditServiceTestOptions())
}

func TestCreditListing_DeletionModes(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		repo := getCreditRepo(ctx)
		active, deleted := createTestCreditForList(), createTestCreditForList()
		require.NoError(tb, repo.CreateCredit(ctx, active))
		require.NoError(tb, repo.CreateCredit(ctx, deleted))
		require.NoError(tb, repo.SoftDeleteCredit(ctx, deleted.ID))
		rows, total, err := repo.ListCredits(ctx, nil, nil, queryutil.DefaultPagination)
		require.NoError(tb, err)
		require.EqualValues(tb, 1, total)
		require.Equal(tb, active.ID, rows[0].ID)
		rows, total, err = repo.ListCredits(ctx, nil, nil, queryutil.DefaultPagination, pluginCore.WithIncludeDeleted(true))
		require.NoError(tb, err)
		require.EqualValues(tb, 2, total)
		require.Len(tb, rows, 2)
		rows, total, err = repo.ListCredits(ctx, []queryutil.CrudFilter{queryutil.FieldIsNotNull("deleted_at")}, nil, queryutil.DefaultPagination, pluginCore.WithIncludeDeleted(true))
		require.NoError(tb, err)
		require.EqualValues(tb, 1, total)
		require.Equal(tb, deleted.ID, rows[0].ID)
	}, getCreditServiceTestOptions())
}
