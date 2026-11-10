package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/internal"
	"go.lumeweb.com/portal-plugin-billing/internal/api/dto"
	pluginConfig "go.lumeweb.com/portal-plugin-billing/internal/config"
	"go.lumeweb.com/portal-plugin-billing/internal/db/migrations"
	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/portal-plugin-billing/internal/service/pricing"
	"go.lumeweb.com/portal/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
	"go.lumeweb.com/queryutil"
	"gorm.io/gorm"
)

func TestHandleListPricingPlans_DatabaseVisibility(t *testing.T) {
	options := coreTesting.CombineOptions(
		coreTesting.WithAPIExtension(NewAPIExtension()),
		coreTesting.NewMockPluginBuilder(internal.PLUGIN_NAME).
			WithMigrations(core.DBMigration{core.DB_TYPE_SQLITE: migrations.GetSQLite()}).
			WithService(pluginCore.PRICING_SERVICE, pricing.NewPricingService).
			WithMockServiceFactory(pluginCore.BILLING_SERVICE, pluginCore.NewMockBillingService).
			WithMockServiceFactory(pluginCore.CREDIT_SERVICE, pluginCore.NewMockCreditService).
			WithServiceConfig(pluginCore.BILLING_SERVICE, &pluginConfig.ServiceConfig{}).
			BuilderOption(),
	)
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		line := &models.PriceLine{Name: "Default", Description: "Default offers", IsDefault: true, IsActive: true}
		require.NoError(tb, ctx.DB().Create(line).Error)
		plans := []*models.PricingPlan{
			{Name: "Visible", IsActive: true, IsPublic: true},
			{Name: "Private", IsActive: true},
			{Name: "Inactive", IsPublic: true},
		}
		for i, plan := range plans {
			plan.FeaturesJSON = new(`[]`)
			require.NoError(tb, ctx.DB().Create(plan).Error)
			require.NoError(tb, ctx.DB().Create(&models.PriceLinePlan{PriceLineID: line.ID, PlanID: plan.ID, Position: i}).Error)
			require.NoError(tb, ctx.DB().Create(&models.PricingPlanPeriod{PricingPlanID: plan.ID, Cadence: "monthly", PriceUSD: 10}).Error)
		}
		svc := core.GetService[pluginCore.PricingService](ctx, pluginCore.PRICING_SERVICE)
		raw, err := svc.GetPlansForPriceLine(context.Background(), line.ID)
		require.NoError(tb, err)
		require.Len(tb, raw, 3, "internal queries must retain hidden records")

		w := httptest.NewRecorder()
		ctx.Router().ServeHTTP(w, ctx.NewAPIRequest(http.MethodGet, "/api/billing/plans", nil))
		require.Equal(tb, http.StatusOK, w.Code, w.Body.String())
		var response queryutil.Response[[]dto.PublicPricingPlanResponse]
		require.NoError(tb, json.Unmarshal(w.Body.Bytes(), &response))
		require.Len(tb, response.Data, 1)
		assert.Equal(tb, int64(1), response.Total)
		assert.Equal(tb, plans[0].ID, response.Data[0].ID)
		require.Len(tb, response.Data[0].PricingPeriods, 1)
		assert.Equal(tb, 10.0, response.Data[0].PricingPeriods[0].PriceUSD)
	}, options)
}

func TestHandleListPricingPlans_ExcludesUnavailablePlans(t *testing.T) {
	for _, authenticated := range []bool{false, true} {
		for _, visible := range []bool{false, true} {
			name := "anonymous"
			if authenticated {
				name = "authenticated"
			}
			if visible {
				name += "/mixed"
			} else {
				name += "/all_hidden"
			}
			t.Run(name, func(t *testing.T) {
				coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
					ts := setupTest(ctx)
					pricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)
					line := &models.PriceLine{Model: gorm.Model{ID: 100}}
					if authenticated {
						pricing.EXPECT().GetEffectivePriceLineForUser(mock.Anything, uint(1)).Return(line, nil).Once()
					} else {
						pricing.EXPECT().GetDefaultPriceLine(mock.Anything).Return(line, nil).Once()
					}
					plans := []*models.PricingPlan{
						{Model: gorm.Model{ID: 2}, Name: "Private offer", IsActive: true},
						{Model: gorm.Model{ID: 3}, Name: "Retired offer", IsPublic: true},
					}
					var wantIDs []uint
					if visible {
						plans = append(plans, &models.PricingPlan{Model: gorm.Model{ID: 1}, Name: "Public offer", IsActive: true, IsPublic: true})
						wantIDs = []uint{1}
						pricing.EXPECT().GetPricingPlanPeriodsForPlans(mock.Anything, []uint{1}).Return([]*models.PricingPlanPeriod{{Model: gorm.Model{ID: 10}, PricingPlanID: 1, PriceUSD: 20}}, nil).Once()
					}
					pricing.EXPECT().GetPlansForPriceLine(mock.Anything, uint(100)).Return(plans, nil).Once()
					// Hidden records must neither be serialized nor have their periods loaded.

					req := ctx.NewAPIRequest(http.MethodGet, "/api/billing/plans", nil)
					if authenticated {
						var err error
						req, err = ts.createAuthenticatedRequest(ctx, http.MethodGet, "/api/billing/plans", nil, "1")
						require.NoError(tb, err)
					}
					w := httptest.NewRecorder()
					ts.router.ServeHTTP(w, req)
					require.Equal(tb, http.StatusOK, w.Code, w.Body.String())
					var response queryutil.Response[[]dto.PublicPricingPlanResponse]
					require.NoError(tb, json.Unmarshal(w.Body.Bytes(), &response))
					var ids []uint
					for _, plan := range response.Data {
						ids = append(ids, plan.ID)
					}
					assert.Equal(tb, wantIDs, ids)
					assert.Equal(tb, int64(len(wantIDs)), response.Total)
					assert.NotContains(tb, w.Body.String(), "Private offer")
					assert.NotContains(tb, w.Body.String(), "Retired offer")
					pricing.AssertNotCalled(t, "GetPricingPlanPeriods", mock.Anything, uint(2))
					pricing.AssertNotCalled(t, "GetPricingPlanPeriods", mock.Anything, uint(3))
				}, getUserAPITestOptions())
			})
		}
	}
}
