package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/internal/api/dto"
	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/portal-plugin-billing/internal/service/pricing"
	"go.lumeweb.com/portal/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
	"gorm.io/gorm"
)

func TestHandleChangeUserPlan_DoesNotApplyPublicEligibility(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupAdminTest(ctx)
		billing := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		periodID := uint(1)
		billing.EXPECT().GetActiveSubscription(mock.Anything, uint(7)).Return(createMockSubscriber(7, "atlos", "sub_7", true, &periodID), nil).Once()
		gateway := pluginCore.NewMockPaymentGateway(t)
		billing.EXPECT().GetGateway(mock.Anything, "atlos").Return(gateway, nil).Once()
		gateway.EXPECT().GetManagementInfo(mock.Anything, uint(7)).Return(&pluginCore.ManagementCapabilities{
			ManagementMode:  pluginCore.ModeAPI,
			AdminOperations: map[pluginCore.ManagementOperation]bool{pluginCore.OperationChangePlan: true},
		}, nil).Once()
		gateway.EXPECT().ExecutePlanChange(mock.Anything, uint(7), uint(99)).Return(&pluginCore.PlanChangeResult{Action: pluginCore.PlanChangeActionComplete}, nil).Once()
		req, err := ts.createAuthenticatedRequest(ctx, http.MethodPost, "/api/billing/users/7/subscriptions/change-plan", []byte(`{"period_id":99}`), "1")
		require.NoError(tb, err)
		w := httptest.NewRecorder()
		ts.router.ServeHTTP(w, req)
		require.Equal(tb, http.StatusOK, w.Code, w.Body.String())
		ts.pricingSvc.AssertNotCalled(t, "GetPricingPlanPeriod", mock.Anything, mock.Anything)
		ts.pricingSvc.AssertNotCalled(t, "GetPricingPlan", mock.Anything, mock.Anything)
	}, getAdminAPITestOptions())
}

func TestHandleChangePlanOperation_ValidatesSelfServiceTarget(t *testing.T) {
	for _, tt := range []struct {
		name      string
		period    *models.PricingPlanPeriod
		periodErr error
		plan      *models.PricingPlan
		planErr   error
		wantCode  int
	}{
		{name: "private", period: &models.PricingPlanPeriod{PricingPlanID: 2}, plan: &models.PricingPlan{Name: "Private offer", IsActive: true}, wantCode: http.StatusBadRequest},
		{name: "inactive", period: &models.PricingPlanPeriod{PricingPlanID: 2}, plan: &models.PricingPlan{IsPublic: true}, wantCode: http.StatusBadRequest},
		{name: "missing period", periodErr: pricing.ErrPricingPlanPeriodNotFound, wantCode: http.StatusBadRequest},
		{name: "nil period", wantCode: http.StatusBadRequest},
		{name: "missing parent", period: &models.PricingPlanPeriod{PricingPlanID: 2}, planErr: pricing.ErrPricingPlanNotFound, wantCode: http.StatusBadRequest},
		{name: "nil parent", period: &models.PricingPlanPeriod{PricingPlanID: 2}, wantCode: http.StatusBadRequest},
		{name: "period lookup failure", periodErr: errors.New("private database details"), wantCode: http.StatusInternalServerError},
		{name: "plan lookup failure", period: &models.PricingPlanPeriod{PricingPlanID: 2}, planErr: errors.New("private database details"), wantCode: http.StatusInternalServerError},
		{name: "public active", period: &models.PricingPlanPeriod{PricingPlanID: 2}, plan: &models.PricingPlan{Model: gorm.Model{ID: 2}, IsActive: true, IsPublic: true}, wantCode: http.StatusOK},
	} {
		t.Run(tt.name, func(t *testing.T) {
			coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
				ts := setupTest(ctx)
				ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)
				oldPeriodID := uint(1)
				ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(createMockSubscriber(1, "atlos", "sub_123", true, &oldPeriodID), nil).Once()
				gateway := pluginCore.NewMockPaymentGateway(t)
				ts.billingSvc.EXPECT().GetGateway(mock.Anything, "atlos").Return(gateway, nil).Once()
				gateway.EXPECT().GetManagementInfo(mock.Anything, uint(1)).Return(&pluginCore.ManagementCapabilities{
					ManagementMode: pluginCore.ModeAPI,
					Operations:     map[pluginCore.ManagementOperation]bool{pluginCore.OperationChangePlan: true},
				}, nil).Once()
				pricingSvc := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)
				pricingSvc.EXPECT().GetPricingPlanPeriod(mock.Anything, uint(99)).Return(tt.period, tt.periodErr).Maybe()
				if tt.period != nil && tt.periodErr == nil {
					pricingSvc.EXPECT().GetPricingPlan(mock.Anything, uint(2)).Return(tt.plan, tt.planErr).Maybe()
				}
				gateway.EXPECT().ExecutePlanChange(mock.Anything, uint(1), uint(99)).Return(&pluginCore.PlanChangeResult{Action: pluginCore.PlanChangeActionComplete}, nil).Maybe()

				body, err := json.Marshal(dto.ChangePlanRequest{PeriodID: 99})
				require.NoError(tb, err)
				req, err := ts.createAuthenticatedRequest(ctx, http.MethodPost, "/api/account/billing/change-plan", body, "1")
				require.NoError(tb, err)
				w := httptest.NewRecorder()
				ts.router.ServeHTTP(w, req)
				assert.Equal(tb, tt.wantCode, w.Code, w.Body.String())
				if tt.wantCode != http.StatusOK {
					gateway.AssertNotCalled(t, "ExecutePlanChange", mock.Anything, mock.Anything, mock.Anything)
					assert.NotContains(tb, w.Body.String(), "Private offer")
					assert.NotContains(tb, w.Body.String(), "private database details")
				} else {
					gateway.AssertNumberOfCalls(t, "ExecutePlanChange", 1)
				}
			}, getUserAPITestOptions())
		})
	}
}
