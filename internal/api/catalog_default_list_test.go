package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/internal/api/dto"
	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/portal/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
	"go.lumeweb.com/queryutil"
)

func TestPublicCatalogWithoutPaginationReturnsAllVisiblePlans(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		pricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)
		line := &models.PriceLine{}
		line.ID = 1
		plans := make([]*models.PricingPlan, 0, 13)
		for id := uint(1); id <= 13; id++ {
			plan := &models.PricingPlan{Name: "Offer", IsActive: true, IsPublic: true}
			plan.ID = id
			if id == 2 {
				plan.IsPublic = false
			}
			plans = append(plans, plan)
		}
		pricing.EXPECT().GetDefaultPriceLine(mock.Anything).Return(line, nil).Once()
		pricing.EXPECT().GetPlansForPriceLine(mock.Anything, uint(1)).Return(plans, nil).Once()
		visibleIDs := []uint{1, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13}
		pricing.EXPECT().GetPricingPlanPeriodsForPlans(mock.Anything, visibleIDs).Return([]*models.PricingPlanPeriod{
			{PricingPlanID: 1, Cadence: "monthly", PriceUSD: 10},
			{PricingPlanID: 13, Cadence: "yearly", PriceUSD: 100},
		}, nil).Once()

		w := httptest.NewRecorder()
		ctx.Router().ServeHTTP(w, ctx.NewAPIRequest(http.MethodGet, "/api/billing/plans", nil))
		require.Equal(tb, http.StatusOK, w.Code, w.Body.String())
		var response queryutil.Response[[]dto.PublicPricingPlanResponse]
		require.NoError(tb, json.Unmarshal(w.Body.Bytes(), &response))
		require.EqualValues(tb, 12, response.Total)
		require.Len(tb, response.Data, 12)
		require.EqualValues(tb, 1, response.Data[0].ID)
		require.EqualValues(tb, 3, response.Data[1].ID)
		require.EqualValues(tb, 13, response.Data[11].ID)
		require.Len(tb, response.Data[0].PricingPeriods, 1)
		require.Equal(tb, "monthly", response.Data[0].PricingPeriods[0].Cadence)
		require.Empty(tb, response.Data[1].PricingPeriods)
		require.Len(tb, response.Data[11].PricingPeriods, 1)
		require.Equal(tb, "yearly", response.Data[11].PricingPeriods[0].Cadence)
		require.Equal(tb, "pricing_plans 0-11/12", w.Header().Get("Content-Range"))
		require.Equal(tb, "12", w.Header().Get("X-Total-Count"))
	}, getUserAPITestOptions())
}
