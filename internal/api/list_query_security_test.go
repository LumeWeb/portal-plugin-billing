package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/internal"
	"go.lumeweb.com/portal-plugin-billing/internal/api/dto"
	pluginConfig "go.lumeweb.com/portal-plugin-billing/internal/config"
	"go.lumeweb.com/portal-plugin-billing/internal/db/migrations"
	"go.lumeweb.com/portal-plugin-billing/internal/db/models"
	creditService "go.lumeweb.com/portal-plugin-billing/internal/service/credit"
	"go.lumeweb.com/portal/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
	"go.lumeweb.com/queryutil"
	"go.lumeweb.com/queryutil/filter"
	"gorm.io/gorm"
)

func TestListUserCredits_RejectsUnsafeQueriesAndExcludesDeleted(t *testing.T) {
	options := coreTesting.CombineOptions(
		coreTesting.WithAPIExtension(NewAPIExtension()),
		coreTesting.NewMockPluginBuilder(internal.PLUGIN_NAME).
			WithMigrations(core.DBMigration{core.DB_TYPE_SQLITE: migrations.GetSQLite()}).
			WithService(pluginCore.CREDIT_SERVICE, creditService.NewCreditService).
			WithMockServiceFactory(pluginCore.BILLING_SERVICE, pluginCore.NewMockBillingService).
			WithMockServiceFactory(pluginCore.PRICING_SERVICE, pluginCore.NewMockPricingService).
			WithServiceConfig(pluginCore.BILLING_SERVICE, &pluginConfig.ServiceConfig{}).
			WithServiceConfig(pluginCore.PRICING_SERVICE, &pluginConfig.ServiceConfig{}).
			BuilderOption(),
	)
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		userSvc := core.GetService[*coreTesting.MockUserService](ctx, core.USER_SERVICE)
		userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)
		token := coreTesting.CreateTestLoginToken(t, ctx, "1")
		ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
		for i, userID := range []uint64{1, 1, 2} {
			amount := int64(i + 1)
			description := "000001"
			if i == 1 {
				description = "a+b%2F"
			}
			if userID == 2 {
				amount = 777
				description = "other account private credit"
			}
			require.NoError(tb, ctx.DB().Create(&models.CreditModel{
				ID: ids[i], UserID: userID, Amount: decimal.NewFromInt(amount), Type: "charge", Direction: "credit", Description: description,
			}).Error)
		}
		request := func(query url.Values, wantStatus int) queryutil.Response[[]dto.UserCreditItem] {
			req := ctx.NewAPIRequest(http.MethodGet, "/api/account/billing/credits?"+query.Encode(), nil)
			req.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			ctx.Router().ServeHTTP(w, req)
			require.Equal(tb, wantStatus, w.Code, w.Body.String())
			var response queryutil.Response[[]dto.UserCreditItem]
			if wantStatus == http.StatusOK {
				require.NoError(tb, json.Unmarshal(w.Body.Bytes(), &response))
			} else {
				t.Logf("error response: %s", w.Body.String())
			}
			return response
		}
		for i, description := range []string{"000001", "a+b%2F"} {
			filtered := request(url.Values{"filters[description][eq]": {description}}, http.StatusOK)
			require.Len(tb, filtered.Data, 1)
			require.Equal(tb, ids[i], filtered.Data[0].ID)
		}
		nested := request(url.Values{"filters[or][0][description][in]": {"000001,true"}}, http.StatusOK)
		require.Len(tb, nested.Data, 1)
		require.Equal(tb, ids[0], nested.Data[0].ID)
		baseline := request(url.Values{}, http.StatusOK)
		require.EqualValues(tb, 2, baseline.Total)
		require.Len(tb, baseline.Data, 2)
		request(url.Values{"filters[user_id][eq]": {"2"}}, http.StatusBadRequest)
		request(url.Values{"filters[id) OR 1=1 --][eq]": {"1"}}, http.StatusBadRequest)
		request(url.Values{"filters[or][0][id) OR 1=1 --][eq]": {"1"}}, http.StatusBadRequest)

		for _, guess := range []string{"777", "778"} {
			request(url.Values{"_sort": {"CASE WHEN (SELECT amount FROM billing_credits WHERE user_id = 2 LIMIT 1) = " + guess + " THEN amount ELSE -amount END"}}, http.StatusBadRequest)
		}
		request(url.Values{"filters[not_a_credit_column][eq]": {"1"}}, http.StatusBadRequest)
		allowed := request(url.Values{"filters[or][0][amount][eq]": {"1"}, "filters[or][1][amount][eq]": {"777"}, "_sort": {"amount"}}, http.StatusOK)
		require.EqualValues(tb, 1, allowed.Total)
		require.Equal(tb, ids[0], allowed.Data[0].ID)

		now := time.Now()
		require.NoError(tb, ctx.DB().Model(&models.CreditModel{}).Where("id = ?", ids[0]).Update("deleted_at", &now).Error)
		deletedVisible := request(url.Values{}, http.StatusOK)
		require.EqualValues(tb, 1, deletedVisible.Total)
		require.Equal(tb, ids[1], deletedVisible.Data[0].ID)
	}, options)
}

func TestListDeletedCredits_ExcludesActiveRows(t *testing.T) {
	options := coreTesting.CombineOptions(
		coreTesting.WithAPIExtension(NewAdminExtension()),
		coreTesting.NewMockPluginBuilder(internal.PLUGIN_NAME).
			WithMigrations(core.DBMigration{core.DB_TYPE_SQLITE: migrations.GetSQLite()}).
			WithService(pluginCore.CREDIT_SERVICE, creditService.NewCreditService).
			WithMockServiceFactory(pluginCore.BILLING_SERVICE, pluginCore.NewMockBillingService).
			WithMockServiceFactory(pluginCore.PRICING_SERVICE, pluginCore.NewMockPricingService).
			WithServiceConfig(pluginCore.BILLING_SERVICE, &pluginConfig.ServiceConfig{}).
			WithServiceConfig(pluginCore.PRICING_SERVICE, &pluginConfig.ServiceConfig{}).
			BuilderOption(),
	)
	coreTesting.RunTestCaseWithDB(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		userSvc := core.GetService[*coreTesting.MockUserService](ctx, core.USER_SERVICE)
		userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)
		now := time.Now()
		for _, deletedAt := range []*time.Time{nil, &now} {
			require.NoError(tb, ctx.DB().Create(&models.CreditModel{
				ID: uuid.New(), UserID: 1, Amount: decimal.NewFromInt(1), Type: "charge", Direction: "credit", DeletedAt: deletedAt,
			}).Error)
		}
		req := ctx.NewAPIRequest(http.MethodGet, "/api/billing/users/1/deleted-credits", nil)
		req.Header.Set("Authorization", "Bearer "+coreTesting.CreateTestLoginToken(t, ctx, "1"))
		w := httptest.NewRecorder()
		ctx.Router().ServeHTTP(w, req)
		require.Equal(tb, http.StatusOK, w.Code, w.Body.String())
		var response queryutil.Response[[]dto.CreditItem]
		require.NoError(tb, json.Unmarshal(w.Body.Bytes(), &response))
		require.EqualValues(tb, 1, response.Total)
		require.Len(tb, response.Data, 1)
		require.NotNil(tb, response.Data[0].DeletedAt)
	}, options)
}

func TestListPricingPlans_PaginatesBeforeLoadingPeriods(t *testing.T) {
	for _, tc := range []struct {
		start  int
		wantID uint
	}{{0, 1}, {1, 2}, {2, 3}, {3, 0}, {100, 0}} {
		t.Run(fmt.Sprint(tc.start), func(t *testing.T) {
			coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
				svc := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)
				line := &models.PriceLine{}
				line.ID = 1
				plans := []*models.PricingPlan{}
				for i := 1; i <= 3; i++ {
					plan := &models.PricingPlan{Name: "Visible", IsActive: true, IsPublic: true}
					plan.ID = uint(i)
					plans = append(plans, plan)
				}
				svc.EXPECT().GetDefaultPriceLine(mock.Anything).Return(line, nil).Once()
				svc.EXPECT().GetPlansForPriceLine(mock.Anything, uint(1)).Return(plans, nil).Once()
				wantLen := 0
				if tc.wantID != 0 {
					wantLen = 1
					svc.EXPECT().GetPricingPlanPeriods(mock.Anything, tc.wantID).Return([]*models.PricingPlanPeriod{}, nil).Once()
				}
				w := httptest.NewRecorder()
				ctx.Router().ServeHTTP(w, ctx.NewAPIRequest(http.MethodGet, fmt.Sprintf("/api/billing/plans?_start=%d&_end=%d", tc.start, tc.start+1), nil))
				require.Equal(tb, http.StatusOK, w.Code, w.Body.String())
				var response queryutil.Response[[]dto.PublicPricingPlanResponse]
				require.NoError(tb, json.Unmarshal(w.Body.Bytes(), &response))
				require.EqualValues(tb, 3, response.Total)
				require.Len(tb, response.Data, wantLen)
				if wantLen != 0 {
					require.Equal(tb, tc.wantID, response.Data[0].ID)
					require.Equal(tb, fmt.Sprintf("pricing_plans %d-%d/3", tc.start, tc.start), w.Header().Get("Content-Range"))
				}
				require.Equal(tb, fmt.Sprint(wantLen), w.Result().Header.Get("X-Total-Count"))
			}, getUserAPITestOptions())
		})
	}
}

func TestBillingListings_RejectExcessiveFilterWidthBeforeService(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		core.GetService[*coreTesting.MockUserService](ctx, core.USER_SERVICE).EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)
		pricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)
		pricing.EXPECT().GetPricingPlans(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, int64(0), nil).Maybe()

		query := url.Values{}
		for i := 0; i < 65; i++ {
			query.Set(fmt.Sprintf("filters[or][%d][id][eq]", i), "1")
		}
		req := ctx.NewAPIRequest(http.MethodGet, "/api/billing/pricing-plans?"+query.Encode(), nil)
		req.Header.Set("Authorization", "Bearer "+coreTesting.CreateTestLoginToken(t, ctx, "1"))
		w := httptest.NewRecorder()
		ctx.Router().ServeHTTP(w, req)
		require.Equal(tb, http.StatusBadRequest, w.Code, w.Body.String())
		pricing.AssertNotCalled(tb, "GetPricingPlans", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	}, getAdminAPITestOptions())
}

func TestBillingListings_RejectUnsafeQueriesBeforeService(t *testing.T) {
	for _, tc := range []struct {
		route string
		admin bool
	}{
		{"/api/account/billing/credits", false}, {"/api/billing/plans", false},
		{"/api/billing/pricing-plans", true}, {"/api/billing/price-lines", true},
		{"/api/billing/credits", true}, {"/api/billing/users/1/deleted-credits", true},
		{"/api/billing/pricing-plan-periods", true}, {"/api/billing/subscribers", true},
		{"/api/billing/users/1/subscribers", true}, {"/api/billing/gateways/stripe/subscribers", true},
	} {
		t.Run(tc.route, func(t *testing.T) {
			opts := getUserAPITestOptions()
			if tc.admin {
				opts = getAdminAPITestOptions()
			}
			coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
				userSvc := core.GetService[*coreTesting.MockUserService](ctx, core.USER_SERVICE)
				if tc.route != "/api/billing/plans" {
					userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)
				}
				token := coreTesting.CreateTestLoginToken(t, ctx, "1")
				for _, query := range []string{
					url.Values{"filters[id) OR 1=1 --][eq]": {"1"}}.Encode(),
					url.Values{"_sort": {"(SELECT 1)"}}.Encode(),
					"_start=-1", "_end=101",
					"filters[amount][eq]=%zz", "filters%zz=value",
				} {
					req := ctx.NewAPIRequest(http.MethodGet, tc.route+"?"+query, nil)
					req.Header.Set("Authorization", "Bearer "+token)
					w := httptest.NewRecorder()
					ctx.Router().ServeHTTP(w, req)
					require.Equal(tb, http.StatusBadRequest, w.Code, w.Body.String())
				}
			}, opts)
		})
	}
}

func TestBillingListings_DoNotExposeServiceErrors(t *testing.T) {
	for _, tc := range []struct {
		route string
		admin bool
	}{
		{"/api/account/billing/credits", false}, {"/api/billing/plans", false},
		{"/api/billing/pricing-plans", true}, {"/api/billing/price-lines", true},
		{"/api/billing/credits", true}, {"/api/billing/users/1/deleted-credits", true},
		{"/api/billing/pricing-plan-periods", true}, {"/api/billing/subscribers", true},
		{"/api/billing/users/1/subscribers", true}, {"/api/billing/gateways/stripe/subscribers", true},
	} {
		failures := []struct {
			err    error
			status int
		}{{errors.New("private database details"), http.StatusInternalServerError}}
		if tc.route == "/api/billing/credits" {
			failures = append(failures,
				struct {
					err    error
					status int
				}{fmt.Errorf("private database details: %w", filter.ErrInvalidQuery), http.StatusBadRequest},
				struct {
					err    error
					status int
				}{fmt.Errorf("private database details: %w", gorm.ErrRecordNotFound), http.StatusNotFound})
		}
		for _, failure := range failures {
			t.Run(tc.route+"/"+fmt.Sprint(failure.status), func(t *testing.T) {
				opts := getUserAPITestOptions()
				if tc.admin {
					opts = getAdminAPITestOptions()
				}
				coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
					if tc.route != "/api/billing/plans" {
						core.GetService[*coreTesting.MockUserService](ctx, core.USER_SERVICE).EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)
					}
					pricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)
					billing := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
					credit := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)
					switch tc.route {
					case "/api/billing/plans":
						pricing.EXPECT().GetDefaultPriceLine(mock.Anything).Return(nil, failure.err).Once()
					case "/api/billing/pricing-plans":
						pricing.EXPECT().GetPricingPlans(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, int64(0), failure.err).Once()
					case "/api/billing/price-lines":
						pricing.EXPECT().GetPriceLines(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, int64(0), failure.err).Once()
					case "/api/billing/pricing-plan-periods":
						pricing.EXPECT().GetPricingPlanPeriodsWithFilter(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, int64(0), failure.err).Once()
					case "/api/billing/subscribers", "/api/billing/users/1/subscribers", "/api/billing/gateways/stripe/subscribers":
						billing.EXPECT().ListSubscribers(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, int64(0), failure.err).Once()
					case "/api/billing/users/1/deleted-credits":
						credit.EXPECT().ListCredits(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, int64(0), failure.err).Once()
					default:
						credit.EXPECT().ListCredits(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, int64(0), failure.err).Once()
					}
					req := ctx.NewAPIRequest(http.MethodGet, tc.route, nil)
					if tc.route != "/api/billing/plans" {
						req.Header.Set("Authorization", "Bearer "+coreTesting.CreateTestLoginToken(t, ctx, "1"))
					}
					w := httptest.NewRecorder()
					ctx.Router().ServeHTTP(w, req)
					require.Equal(tb, failure.status, w.Code, w.Body.String())
					require.NotContains(tb, w.Body.String(), "private database details")
				}, opts)
			})
		}
	}
}
