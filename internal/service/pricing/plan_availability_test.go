package pricing

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
)

func TestPricingService_UpdatePricingPlan_PreservesOmittedFlags(t *testing.T) {
	for _, tt := range []struct {
		name       string
		active     bool
		public     bool
		update     *pluginCore.PricingPlanUpdate
		wantActive bool
		wantPublic bool
	}{
		{name: "omit both true flags", active: true, public: true, update: &pluginCore.PricingPlanUpdate{Name: "Renamed"}, wantActive: true, wantPublic: true},
		{name: "omit both false flags", update: &pluginCore.PricingPlanUpdate{Name: "Renamed"}},
		{name: "hide only", active: true, public: true, update: &pluginCore.PricingPlanUpdate{IsPublic: new(false)}, wantActive: true},
		{name: "deactivate only", active: true, public: true, update: &pluginCore.PricingPlanUpdate{IsActive: new(false)}, wantPublic: true},
		{name: "clear features", active: true, public: true, update: &pluginCore.PricingPlanUpdate{FeaturesJSON: new("")}, wantActive: true, wantPublic: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
				service := setupPricingTestContext(tb, ctx, true)
				plan := createTestPricingPlan()
				plan.IsActive, plan.IsPublic = tt.active, tt.public
				require.NoError(tb, service.CreatePricingPlan(context.Background(), plan))
				require.NoError(tb, service.UpdatePricingPlan(context.Background(), plan.ID, tt.update))
				stored, err := service.GetPricingPlan(context.Background(), plan.ID)
				require.NoError(tb, err)
				assert.Equal(tb, tt.wantActive, stored.IsActive)
				assert.Equal(tb, tt.wantPublic, stored.IsPublic)
				if tt.update.FeaturesJSON != nil {
					require.NotNil(tb, stored.FeaturesJSON)
					assert.Empty(tb, *stored.FeaturesJSON)
				}
			}, getPricingTestOptions())
		})
	}
}

func TestPricingService_UpdatePricingPlan_CanRevokeAvailability(t *testing.T) {
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		service := setupPricingTestContext(tb, ctx, true)
		plan := createAndVerifyPlan(t, service, createTestPricingPlan())
		require.NoError(tb, service.UpdatePricingPlan(context.Background(), plan.ID, &pluginCore.PricingPlanUpdate{
			Name: "Retired plan", IsActive: new(false), IsPublic: new(false),
		}))
		stored, err := service.GetPricingPlan(context.Background(), plan.ID)
		require.NoError(tb, err)
		assert.False(tb, stored.IsActive)
		assert.False(tb, stored.IsPublic)
		assert.Equal(tb, "Retired plan", stored.Name)

		require.NoError(tb, service.UpdatePricingPlan(context.Background(), plan.ID, &pluginCore.PricingPlanUpdate{
			IsActive: new(true), IsPublic: new(true),
		}))
		stored, err = service.GetPricingPlan(context.Background(), plan.ID)
		require.NoError(tb, err)
		assert.True(tb, stored.IsActive)
		assert.True(tb, stored.IsPublic)
	}, getPricingTestOptions())
}
