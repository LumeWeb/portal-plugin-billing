package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
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
	"go.lumeweb.com/portal/core/testing/mocks"
)

func TestAdminUpdatePricingPlan_PersistsAvailability(t *testing.T) {
	options := coreTesting.CombineOptions(
		coreTesting.WithAPIExtension(NewAdminExtension()),
		coreTesting.NewMockPluginBuilder(internal.PLUGIN_NAME).
			WithMigrations(core.DBMigration{core.DB_TYPE_SQLITE: migrations.GetSQLite()}).
			WithService(pluginCore.PRICING_SERVICE, pricing.NewPricingService).
			WithMockServiceFactory(pluginCore.BILLING_SERVICE, pluginCore.NewMockBillingService).
			WithMockServiceFactory(pluginCore.CREDIT_SERVICE, pluginCore.NewMockCreditService).
			WithServiceConfig(pluginCore.BILLING_SERVICE, &pluginConfig.ServiceConfig{}).
			BuilderOption(),
	)
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		cron := core.GetService[*mocks.MockCronService](ctx, core.CRON_SERVICE)
		factory := mocks.NewMockCronJobFactory(t)
		job := mocks.NewMockCronJob(t)
		cron.EXPECT().JobFactory().Return(factory)
		factory.EXPECT().CreateJob(mock.Anything, mock.Anything).Return(job, nil)
		job.EXPECT().SetArgs(mock.Anything)
		cron.EXPECT().RegisterJob(mock.Anything, job, mock.Anything).Return(nil)

		plan := &models.PricingPlan{Name: "Offer", Description: "Offer details", Currency: "USD", FeaturesJSON: new(`[]`), IsActive: true, IsPublic: true}
		require.NoError(tb, ctx.DB().Create(plan).Error)
		userSvc := core.GetService[*coreTesting.MockUserService](ctx, core.USER_SERVICE)
		userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)
		token := coreTesting.CreateTestLoginToken(t, ctx, "1")
		url := "/api/billing/pricing-plans/" + strconv.Itoa(int(plan.ID))
		for _, tt := range []struct {
			body       string
			wantActive bool
			wantPublic bool
		}{
			{body: `{"is_active":false,"is_public":false}`},
			{body: `{"name":"Renamed"}`},
			{body: `{"is_active":true}`, wantActive: true},
			{body: `{"is_public":true}`, wantActive: true, wantPublic: true},
		} {
			req := ctx.NewAPIRequest(http.MethodPut, url, []byte(tt.body))
			req.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			ctx.Router().ServeHTTP(w, req)
			require.Equal(tb, http.StatusOK, w.Code, w.Body.String())
			var response dto.PricingPlanResponse
			require.NoError(tb, json.Unmarshal(w.Body.Bytes(), &response))
			assert.Equal(tb, tt.wantActive, response.IsActive)
			assert.Equal(tb, tt.wantPublic, response.IsPublic)
			stored, err := core.GetService[pluginCore.PricingService](ctx, pluginCore.PRICING_SERVICE).GetPricingPlan(context.Background(), plan.ID)
			require.NoError(tb, err)
			assert.Equal(tb, tt.wantActive, stored.IsActive)
			assert.Equal(tb, tt.wantPublic, stored.IsPublic)
		}
	}, options)
}
