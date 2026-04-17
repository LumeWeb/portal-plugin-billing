package atlos

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.lumeweb.com/atlos-sdk"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	pluginConfig "go.lumeweb.com/portal-plugin-billing/internal/config"
	billingModels "go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/portal-plugin-billing/pkg/subscription"
	quotaCore "go.lumeweb.com/portal-plugin-quota/core"
	core "go.lumeweb.com/portal/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
	portalModels "go.lumeweb.com/portal/db/models"
	"gorm.io/gorm"
)

const (
	// Test constants for commonly used values
	TestUserID         = uint(123)
	TestPlanID         = uint(1)
	TestMerchantID     = "merchant_test_123"
	TestTransactionID  = "txn_test_123"
	TestSubscriptionID = "sub_test_123"
)

var TestAPISecret string

func TestMain(m *testing.M) {
	// Set test API secret for tests
	TestAPISecret = "api_secret_test_123"

	coreTesting.WithOptions(m,
		coreTesting.WithMockServiceFactory(quotaCore.QUOTA_SERVICE, quotaCore.NewMockQuotaService, &quotaCore.QuotaConfig{}),
		coreTesting.WithMockServiceFactory(pluginCore.BILLING_SERVICE, pluginCore.NewMockBillingService, &pluginConfig.ServiceConfig{}),
		coreTesting.WithMockServiceFactory(pluginCore.PRICING_SERVICE, pluginCore.NewMockPricingService, coreTesting.NewConfigBuilder().Build()),
		coreTesting.WithMockServiceFactory(pluginCore.CREDIT_SERVICE, pluginCore.NewMockCreditService, coreTesting.NewConfigBuilder().Build()),
	)
}

func TestAtlosGateway_ID(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx.Logger(), ctx, TestAPISecret, TestMerchantID, nil, nil, nil, nil, nil, nil)
	assert.Equal(t, GatewayID, gw.ID(context.Background()))
}

func TestAtlosGateway_SignatureHeader(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx.Logger(), ctx, TestAPISecret, TestMerchantID, nil, nil, nil, nil, nil, nil)
	assert.Equal(t, atlos.ApiSecretHeader, gw.SignatureHeader(context.Background()))
}

func TestAtlosGateway_GetName(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx.Logger(), ctx, TestAPISecret, TestMerchantID, nil, nil, nil, nil, nil, nil)
	assert.Equal(t, "ATLOS", gw.GetName(context.Background()))
}

func TestAtlosGateway_GetDescription(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx.Logger(), ctx, TestAPISecret, TestMerchantID, nil, nil, nil, nil, nil, nil)
	assert.Equal(t, "Accept crypto payments using the ATLOS payment widget", gw.GetDescription(context.Background()))
}

func TestAtlosGateway_SupportsProductSync(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx.Logger(), ctx, TestAPISecret, TestMerchantID, nil, nil, nil, nil, nil, nil)
	assert.False(t, gw.SupportsProductSync())
}

func TestAtlosGateway_SyncPlan(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx.Logger(), ctx, TestAPISecret, TestMerchantID, nil, nil, nil, nil, nil, nil)

	plan := &pluginCore.PricingPlanInfo{
		ID:   TestPlanID,
		Name: "Test Plan",
	}

	result, err := gw.SyncPlan(context.Background(), plan)
	assert.NoError(t, err)
	assert.False(t, result.Success)
	assert.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "does not require product synchronization")
}

func TestAtlosGateway_GetCustomerPortalURL(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx.Logger(), ctx, TestAPISecret, TestMerchantID, nil, nil, nil, nil, nil, nil)

	url, err := gw.GetCustomerPortalURL(context.Background(), TestUserID, "https://example.com/return")
	assert.Error(t, err)
	assert.Empty(t, url)
	assert.Contains(t, err.Error(), "customer portal not supported")
}

func TestAtlosGateway_GetCustomerPortalMetadata(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx.Logger(), ctx, TestAPISecret, TestMerchantID, nil, nil, nil, nil, nil, nil)

	metadata, err := gw.GetCustomerPortalMetadata(context.Background(), TestUserID)
	assert.NoError(t, err)
	assert.NotNil(t, metadata)
	assert.Empty(t, metadata)
}

func TestAtlosGateway_ExtractEventID(t *testing.T) {
	tests := []struct {
		name        string
		payload     []byte
		expectedID  string
		expectError bool
	}{
		{
			name: "valid postback notification",
			payload: func() []byte {
				notification := atlos.PostbackNotification{
					TransactionId: TestTransactionID,
					OrderId:       "123-plan1",
					Amount:        10.0,
					Status:        100,
				}
				payload, _ := json.Marshal(notification)
				return payload
			}(),
			expectedID: TestTransactionID,
		},
		{
			name:        "invalid json payload",
			payload:     []byte("invalid json"),
			expectError: true,
		},
		{
			name: "empty transaction ID",
			payload: func() []byte {
				notification := atlos.PostbackNotification{
					TransactionId: "",
					OrderId:       "123-plan1",
					Amount:        10.0,
					Status:        100,
				}
				payload, _ := json.Marshal(notification)
				return payload
			}(),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := coreTesting.NewTestContext(t)
			gw := New(ctx.Logger(), ctx, TestAPISecret, TestMerchantID, nil, nil, nil, nil, nil, nil)

			eventID, err := gw.ExtractEventID(context.Background(), tt.payload)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedID, eventID)
			}
		})
	}
}

func TestAtlosGateway_ExtractEventType(t *testing.T) {
	tests := []struct {
		name         string
		payload      []byte
		expectedType string
		expectError  bool
	}{
		{
			name: "valid postback notification",
			payload: func() []byte {
				notification := atlos.PostbackNotification{
					TransactionId: TestTransactionID,
					OrderId:       "123-plan1",
					Amount:        10.0,
					Status:        100,
				}
				payload, _ := json.Marshal(notification)
				return payload
			}(),
			expectedType: "payment.confirmed",
		},
		{
			name:        "invalid json payload",
			payload:     []byte("invalid json"),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := coreTesting.NewTestContext(t)
			gw := New(ctx.Logger(), ctx, TestAPISecret, TestMerchantID, nil, nil, nil, nil, nil, nil)

			eventType, err := gw.ExtractEventType(context.Background(), tt.payload)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedType, eventType)
			}
		})
	}
}

func TestAtlosGateway_ValidateWebhook(t *testing.T) {
	tests := []struct {
		name        string
		signature   string
		payload     []byte
		expectError bool
	}{
		{
			name: "valid signature",
			payload: func() []byte {
				notification := atlos.CreateTestPostback(TestMerchantID)
				payload, _ := json.Marshal(notification)
				return payload
			}(),
			signature: func() string {
				notification := atlos.CreateTestPostback(TestMerchantID)
				valid, _ := notification.VerifySignature(TestAPISecret, "")
				if valid {
					return ""
				}
				return "test_signature"
			}(),
			expectError: true,
		},
		{
			name: "missing signature",
			payload: func() []byte {
				notification := atlos.CreateTestPostback(TestMerchantID)
				payload, _ := json.Marshal(notification)
				return payload
			}(),
			signature:   "",
			expectError: true,
		},
		{
			name:        "invalid json payload",
			payload:     []byte("invalid json"),
			signature:   "test_signature",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := coreTesting.NewTestContext(t)
			gw := New(ctx.Logger(), ctx, TestAPISecret, TestMerchantID, nil, nil, nil, nil, nil, nil)

			err := gw.ValidateWebhook(context.Background(), tt.signature, tt.payload)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestAtlosGateway_HandleWebhook_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota := core.GetService[*quotaCore.MockQuotaService](ctx, quotaCore.QUOTA_SERVICE)
		mockUsers := core.GetService[*coreTesting.MockUserService](ctx, core.USER_SERVICE)
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)
		mockCredit := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)

		userID := uint(456)
		periodID := uint(2)
		orderID := "456-period2"

		notification := atlos.CreateTestPostback(TestMerchantID)
		notification.OrderId = orderID
		notification.TransactionId = TestTransactionID
		notification.SubscriptionId = TestSubscriptionID
		notification.PaidAmount = 10.0
		payload, _ := json.Marshal(notification)

		// Pricing plan period
		period := &billingModels.PricingPlanPeriod{
			Model:        gorm.Model{ID: periodID},
			PricingPlanID: 1,
			Cadence:       "monthly",
			PriceUSD:      10.0,
		}

		// Pricing plan for validation
		pricingPlan := &billingModels.PricingPlan{
			Model:       gorm.Model{ID: 1},
			Name:        "Test Plan",
			Description: "Test Description",
			IsActive:    true,
		}

		// GetPricingPlanPeriod is called twice (validation + subscriber check)
		mockPricing.EXPECT().GetPricingPlanPeriod(mock.AnythingOfType("*context.valueCtx"), periodID).Return(period, nil)
		mockPricing.EXPECT().GetPricingPlanPeriod(mock.AnythingOfType("*context.valueCtx"), periodID).Return(period, nil)
		mockPricing.EXPECT().GetPricingPlan(mock.AnythingOfType("*context.valueCtx"), uint(1)).Return(pricingPlan, nil)

		// No existing subscriber (new subscription)
		mockBilling.EXPECT().GetActiveSubscriber(mock.AnythingOfType("*context.valueCtx"), userID, GatewayID).Return(nil, nil)

		// Calculate expected billing cycle
		cadence := subscription.Cadence(period.Cadence)
		billingCycle := subscription.CalculateFirstCycle(time.Now().UTC(), cadence)

		// Debit credit for subscription period
		referenceID := TestTransactionID
		description := "Subscription period " + billingCycle.StartAt.Format("2006-01-02") + " to " + billingCycle.EndAt.Format("2006-01-02")
		mockCredit.EXPECT().IssueUsageCredit(
			mock.AnythingOfType("*context.valueCtx"),
			uint64(userID),
			pluginCore.TransactionTypeTime,
			mock.AnythingOfType("decimal.Decimal"), // Use AnythingOfType since decimal representation may differ
			referenceID,
			description,
			uint64(0),
		).Return(nil)

		// Create or update subscriber with billing period options
		mockBilling.EXPECT().CreateOrUpdateSubscriber(
			mock.Anything,
			userID,
			GatewayID,
			TestTransactionID,
			TestSubscriptionID,
			true,
			&periodID,
			mock.Anything, mock.Anything, // Two SubscriberOption args: WithBillingPeriodStart and WithBillingPeriodEnd
		).Return(nil)

		// Issue payment credit
		mockCredit.EXPECT().IssueCreditWithIdempotency(
			mock.AnythingOfType("*context.valueCtx"),
			uint64(userID),
			pluginCore.TransactionTypeCharge,
			mock.AnythingOfType("decimal.Decimal"), // Use AnythingOfType since decimal representation may differ
			pluginCore.ReferenceTypeAtlosPayment,
			TestTransactionID,
			"ATLOS payment completed",
			uint64(0),
		).Return(nil)

		gw := New(ctx.Logger(), ctx, TestAPISecret, TestMerchantID, nil, mockQuota, mockUsers, mockBilling, mockPricing, mockCredit)
		err := gw.HandleWebhook(context.Background(), payload)

		assert.NoError(t, err)
	})
}

func TestAtlosGateway_HandleWebhook_InvalidPayload(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx.Logger(), ctx, TestAPISecret, TestMerchantID, nil, nil, nil, nil, nil, nil)

	err := gw.HandleWebhook(context.Background(), []byte("invalid json"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse postback notification")
}

func TestAtlosGateway_HandleWebhook_InvalidOrderID(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		notification := atlos.CreateTestPostback(TestMerchantID)
		notification.OrderId = "invalid-order-id"
		payload, _ := json.Marshal(notification)

		gw := New(ctx.Logger(), ctx, TestAPISecret, TestMerchantID, nil, nil, nil, nil, mockPricing, nil)
		err := gw.HandleWebhook(context.Background(), payload)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse order ID")
	})
}

func TestAtlosGateway_HandleWebhook_PlanNotFound(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		notification := atlos.CreateTestPostback(TestMerchantID)
		notification.OrderId = "123-plan999"
		payload, _ := json.Marshal(notification)

		mockPricing.EXPECT().GetPricingPlan(mock.Anything, uint(999)).Return(nil, nil)

		gw := New(ctx.Logger(), ctx, TestAPISecret, TestMerchantID, nil, nil, nil, nil, mockPricing, nil)
		err := gw.HandleWebhook(context.Background(), payload)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "pricing plan not found")
	})
}

func TestAtlosGateway_HandleWebhook_PlanNotActive(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		planID := uint(3)
		notification := atlos.CreateTestPostback(TestMerchantID)
		notification.OrderId = "123-plan3"
		payload, _ := json.Marshal(notification)

		pricingPlan := &billingModels.PricingPlan{
			Model:       gorm.Model{ID: planID},
			Name:        "Test Plan",
			Description: "Test Description",
			IsActive:    false,
		}
		mockPricing.EXPECT().GetPricingPlan(mock.Anything, planID).Return(pricingPlan, nil)

		gw := New(ctx.Logger(), ctx, TestAPISecret, TestMerchantID, nil, nil, nil, nil, mockPricing, nil)
		err := gw.HandleWebhook(context.Background(), payload)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "plan is not active")
	})
}

func TestAtlosGateway_GetCheckoutUI_UserNotFound(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota := core.GetService[*quotaCore.MockQuotaService](ctx, quotaCore.QUOTA_SERVICE)
		mockUsers := core.GetService[*coreTesting.MockUserService](ctx, core.USER_SERVICE)
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		pricingPlan := &billingModels.PricingPlan{
			Model:       gorm.Model{ID: TestPlanID},
			Name:        "Test Plan",
			Description: "Test Description",
			IsActive:    true,
		}
		period := &billingModels.PricingPlanPeriod{
			Model:        gorm.Model{ID: 1},
			PricingPlanID: TestPlanID,
			Cadence:       "monthly",
			PriceUSD:      10.0,
		}

		mockPricing.EXPECT().GetPricingPlan(mock.Anything, TestPlanID).Return(pricingPlan, nil)
		mockPricing.EXPECT().GetPricingPlanPeriods(mock.Anything, TestPlanID).Return([]*billingModels.PricingPlanPeriod{period}, nil)
		mockUsers.EXPECT().AccountExists(mock.Anything, TestUserID).Return(false, nil, nil)

		gw := New(ctx.Logger(), ctx, TestAPISecret, TestMerchantID, nil, mockQuota, mockUsers, nil, mockPricing, nil)
		_, err := gw.GetCheckoutUI(context.Background(), TestUserID, TestPlanID, 1)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "user with ID")
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestBuildPaymentConfigData(t *testing.T) {
	period := &billingModels.PricingPlanPeriod{
		Model:        gorm.Model{ID: 2},
		PricingPlanID: 1,
		Cadence:       "monthly",
		PriceUSD:      15.99,
	}
	userName := "Test User"
	userEmail := "test@example.com"
	merchantID := "merchant_123"
	orderID := "456-period2"
	postbackURL := "https://example.com/api/billing/webhook/atlos"
	currency := "USD"

	data := buildPaymentConfigData(merchantID, orderID, period, currency, userName, userEmail, postbackURL)

	assert.Equal(t, "atlos-pay-btn-456-period2", data.ButtonID, "button ID should be prefixed with atlos-pay-btn- and order ID")
	assert.Equal(t, merchantID, data.MerchantID, "merchant ID should match")
	assert.Equal(t, orderID, data.OrderID, "order ID should match")
	assert.Equal(t, 15.99, data.Amount, "amount should match period price")
	assert.Equal(t, currency, data.Currency, "currency should match")
	assert.Equal(t, userName, data.UserName, "user name should match")
	assert.Equal(t, userEmail, data.UserEmail, "user email should match")
	assert.Equal(t, postbackURL, data.PostbackURL, "postback URL should match")
}

func TestAtlosGateway_GetCheckoutUI_PeriodNotFound(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota := core.GetService[*quotaCore.MockQuotaService](ctx, quotaCore.QUOTA_SERVICE)
		mockUsers := core.GetService[*coreTesting.MockUserService](ctx, core.USER_SERVICE)
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		pricingPlan := &billingModels.PricingPlan{
			Model:       gorm.Model{ID: TestPlanID},
			Name:        "Test Plan",
			Description: "Test Description",
			IsActive:    true,
			Currency:    "USD",
		}
		period := &billingModels.PricingPlanPeriod{
			Model:        gorm.Model{ID: 1},
			PricingPlanID: TestPlanID,
			Cadence:       "monthly",
			PriceUSD:      10.0,
		}

		mockPricing.EXPECT().GetPricingPlan(mock.Anything, TestPlanID).Return(pricingPlan, nil)
		mockPricing.EXPECT().GetPricingPlanPeriods(mock.Anything, TestPlanID).Return([]*billingModels.PricingPlanPeriod{period}, nil)

		gw := New(ctx.Logger(), ctx, TestAPISecret, TestMerchantID, nil, mockQuota, mockUsers, nil, mockPricing, nil)
		_, err := gw.GetCheckoutUI(context.Background(), TestUserID, TestPlanID, 999)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "period 999 not found for plan")
	})
}

func TestAtlosGateway_GetCheckoutUI_MissingUserService(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx.Logger(), ctx, TestAPISecret, TestMerchantID, nil, nil, nil, nil, nil, nil)

	_, err := gw.GetCheckoutUI(context.Background(), TestUserID, TestPlanID, 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "user service not configured")
}

func TestAtlosGateway_GetCheckoutUI_PlanNotFound(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota := core.GetService[*quotaCore.MockQuotaService](ctx, quotaCore.QUOTA_SERVICE)
		mockUsers := core.GetService[*coreTesting.MockUserService](ctx, core.USER_SERVICE)
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		mockPricing.EXPECT().GetPricingPlan(mock.Anything, TestPlanID).Return(nil, nil)

		gw := New(ctx.Logger(), ctx, TestAPISecret, TestMerchantID, nil, mockQuota, mockUsers, nil, mockPricing, nil)
		_, err := gw.GetCheckoutUI(context.Background(), TestUserID, TestPlanID, 1)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "pricing plan not found")
	})
}

func TestAtlosGateway_GetCheckoutUI_PlanNotActive(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota := core.GetService[*quotaCore.MockQuotaService](ctx, quotaCore.QUOTA_SERVICE)
		mockUsers := core.GetService[*coreTesting.MockUserService](ctx, core.USER_SERVICE)
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		pricingPlan := &billingModels.PricingPlan{
			Model:       gorm.Model{ID: TestPlanID},
			Name:        "Test Plan",
			Description: "Test Description",
			IsActive:    false,
		}
		mockPricing.EXPECT().GetPricingPlan(mock.Anything, TestPlanID).Return(pricingPlan, nil)

		gw := New(ctx.Logger(), ctx, TestAPISecret, TestMerchantID, nil, mockQuota, mockUsers, nil, mockPricing, nil)
		_, err := gw.GetCheckoutUI(context.Background(), TestUserID, TestPlanID, 1)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "plan is not active")
	})
}

func TestAtlosGateway_GetCheckoutUI_NoPricingPeriods(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota := core.GetService[*quotaCore.MockQuotaService](ctx, quotaCore.QUOTA_SERVICE)
		mockUsers := core.GetService[*coreTesting.MockUserService](ctx, core.USER_SERVICE)
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		pricingPlan := &billingModels.PricingPlan{
			Model:       gorm.Model{ID: TestPlanID},
			Name:        "Test Plan",
			Description: "Test Description",
			IsActive:    true,
		}
		mockPricing.EXPECT().GetPricingPlan(mock.Anything, TestPlanID).Return(pricingPlan, nil)
		mockPricing.EXPECT().GetPricingPlanPeriods(mock.Anything, TestPlanID).Return([]*billingModels.PricingPlanPeriod{}, nil)

		gw := New(ctx.Logger(), ctx, TestAPISecret, TestMerchantID, nil, mockQuota, mockUsers, nil, mockPricing, nil)
		_, err := gw.GetCheckoutUI(context.Background(), TestUserID, TestPlanID, 1)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no pricing periods configured")
	})
}

func TestAtlosGateway_ExecuteCancel_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		userID := uint(999)
		periodID := uint(5)

		// Set billing period dates (1 month ago to 1 month from now)
		start := time.Now().AddDate(0, -1, 0).UTC()
		end := time.Now().AddDate(0, 1, 0).UTC()

		mockSubscriber := &pluginCore.Subscriber{
			UserID:              userID,
			GatewayType:         GatewayID,
			ExternalID:          TestTransactionID,
			SubscriptionID:      TestSubscriptionID,
			IsActive:            true,
			PricingPlanPeriodID: &periodID,
			BillingPeriodStart:  &start,
			BillingPeriodEnd:    &end,
		}

		// Pricing plan period
		period := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: periodID},
			PricingPlanID: 1,
			Cadence:       "monthly",
			PriceUSD:      10.0,
		}

		// Mock expectations - ExecuteCancel now schedules cancellation at end of billing period (default)
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(mockSubscriber, nil)
		mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, periodID).Return(period, nil)

		// Schedule cancellation with WillCancelAt option
		mockBilling.EXPECT().CreateOrUpdateSubscriber(
			mock.Anything,
			userID,
			GatewayID,
			TestTransactionID,
			TestSubscriptionID,
			true,
			&periodID,
			mock.Anything, // WithBillingPeriodStart option
			mock.Anything, // WithBillingPeriodEnd option
			mock.Anything, // WithWillCancelAt option
		).Return(nil)

		gw := New(ctx.Logger(), ctx, TestAPISecret, TestMerchantID, nil, nil, nil, mockBilling, mockPricing, nil)
		result, err := gw.ExecuteCancel(context.Background(), userID, false) // false = scheduled cancellation

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, pluginCore.CancellationStatusScheduled, result.Status)
		assert.NotNil(t, result.EffectiveAt)
		assert.True(t, result.CanAbort)
	})
}

func TestAtlosGateway_ExecuteCancel_NoActiveSubscription(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, TestUserID).Return(nil, nil)

		gw := New(ctx.Logger(), ctx, TestAPISecret, TestMerchantID, nil, nil, nil, mockBilling, nil, nil)
		result, err := gw.ExecuteCancel(context.Background(), TestUserID, false)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "no active Atlas subscription found")
	})
}

func TestAtlosGateway_ExecuteCancel_WrongGateway(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		planID := uint(6)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:         TestUserID,
			GatewayType:    "stripe",
			ExternalID:     TestTransactionID,
			SubscriptionID: TestSubscriptionID,
			IsActive:       true,
			PricingPlanPeriodID: &planID,
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, TestUserID).Return(mockSubscriber, nil)

		gw := New(ctx.Logger(), ctx, TestAPISecret, TestMerchantID, nil, nil, nil, mockBilling, nil, nil)
		result, err := gw.ExecuteCancel(context.Background(), TestUserID, false)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "no active Atlas subscription found")
	})
}

func TestAtlosGateway_ExecuteCancel_Immediate(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)
		mockCredit := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)

		userID := uint(999)
		periodID := uint(5)

		// Set billing period dates (1 month ago to 1 month from now)
		start := time.Now().AddDate(0, -1, 0).UTC()
		end := time.Now().AddDate(0, 1, 0).UTC()

		mockSubscriber := &pluginCore.Subscriber{
			UserID:              userID,
			GatewayType:         GatewayID,
			ExternalID:          TestTransactionID,
			SubscriptionID:      TestSubscriptionID,
			IsActive:            true,
			PricingPlanPeriodID: &periodID,
			BillingPeriodStart:  &start,
			BillingPeriodEnd:    &end,
		}

		// Pricing plan period
		period := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: periodID},
			PricingPlanID: 1,
			Cadence:       "monthly",
			PriceUSD:      10.0,
		}

		// Mock expectations - immediate cancellation
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(mockSubscriber, nil)
		mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, periodID).Return(period, nil)

		// Expect proration credit to be issued (for unused time)
		mockCredit.EXPECT().IssueCreditWithIdempotency(
			mock.Anything,
			uint64(userID),
			pluginCore.TransactionTypeRefund,
			mock.MatchedBy(func(amount decimal.Decimal) bool {
				return amount.GreaterThan(decimal.Zero) // Should be positive proration
			}),
			pluginCore.ReferenceTypeAtlosPayment,
			"immediate-cancel-"+TestSubscriptionID,
			"Proration credit for unused subscription period on immediate cancellation",
			uint64(0),
		).Return(nil)

		// Expect subscriber to be deactivated
		mockBilling.EXPECT().DeactivateSubscriber(mock.Anything, userID, GatewayID).Return(nil)

		gw := New(ctx.Logger(), ctx, TestAPISecret, TestMerchantID, nil, nil, nil, mockBilling, mockPricing, mockCredit)
		result, err := gw.ExecuteCancel(context.Background(), userID, true) // true = immediate cancellation

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, pluginCore.CancellationStatusImmediate, result.Status)
		assert.NotNil(t, result.EffectiveAt)
		assert.False(t, result.CanAbort) // Immediate cancellation cannot be aborted
	})
}

func TestAtlosGateway_ExecuteCancel_Immediate_ZeroProration(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)
		mockCredit := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)

		userID := uint(999)
		periodID := uint(5)

		// Set billing period dates (at the very end of the period, minimal unused time)
		start := time.Now().AddDate(0, 0, -30).UTC()
		end := time.Now().Add(time.Hour).UTC()

		mockSubscriber := &pluginCore.Subscriber{
			UserID:              userID,
			GatewayType:         GatewayID,
			ExternalID:          TestTransactionID,
			SubscriptionID:      TestSubscriptionID,
			IsActive:            true,
			PricingPlanPeriodID: &periodID,
			BillingPeriodStart:  &start,
			BillingPeriodEnd:    &end,
		}

		// Pricing plan period
		period := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: periodID},
			PricingPlanID: 1,
			Cadence:       "monthly",
			PriceUSD:      10.0,
		}

		// Mock expectations - immediate cancellation
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(mockSubscriber, nil)
		mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, periodID).Return(period, nil)

		// Expect subscriber to be deactivated
		mockBilling.EXPECT().DeactivateSubscriber(mock.Anything, userID, GatewayID).Return(nil)

		gw := New(ctx.Logger(), ctx, TestAPISecret, TestMerchantID, nil, nil, nil, mockBilling, mockPricing, mockCredit)
		result, err := gw.ExecuteCancel(context.Background(), userID, true) // true = immediate cancellation

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, pluginCore.CancellationStatusImmediate, result.Status)
	})
}

func TestAtlosGateway_AbortCancellation_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		userD := uint(999)
		periodID := uint(5)
		cancelAt := time.Now().Add(24 * time.Hour).UTC()
		start := time.Now().AddDate(0, -1, 0).UTC()
		end := time.Now().AddDate(0, 1, 0).UTC()

		mockSubscriber := &pluginCore.Subscriber{
			UserID:              userD,
			GatewayType:         GatewayID,
			ExternalID:          TestTransactionID,
			SubscriptionID:      TestSubscriptionID,
			IsActive:            true,
			PricingPlanPeriodID: &periodID,
			BillingPeriodStart:  &start,
			BillingPeriodEnd:    &end,
			WillCancelAt:        &cancelAt,
		}

		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userD).Return(mockSubscriber, nil)

		// Expect CreateOrUpdateSubscriber with ClearWillCancelAt option
		mockBilling.EXPECT().CreateOrUpdateSubscriber(
			mock.Anything,
			userD,
			GatewayID,
			TestTransactionID,
			TestSubscriptionID,
			true,
			&periodID,
			mock.Anything, // WithBillingPeriodStart option
			mock.Anything, // WithBillingPeriodEnd option
			mock.Anything, // WithClearWillCancelAt option
		).Return(nil)

		gw := New(ctx.Logger(), ctx, TestAPISecret, TestMerchantID, nil, nil, nil, mockBilling, nil, nil)
		err := gw.AbortCancellation(context.Background(), userD)

		assert.NoError(t, err)
	})
}

func TestAtlosGateway_AbortCancellation_NoScheduledCancellation(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		userD := uint(999)
		periodID := uint(5)

		mockSubscriber := &pluginCore.Subscriber{
			UserID:              userD,
			GatewayType:         GatewayID,
			ExternalID:          TestTransactionID,
			SubscriptionID:      TestSubscriptionID,
			IsActive:            true,
			PricingPlanPeriodID: &periodID,
			WillCancelAt:        nil, // No scheduled cancellation
		}

		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userD).Return(mockSubscriber, nil)

		gw := New(ctx.Logger(), ctx, TestAPISecret, TestMerchantID, nil, nil, nil, mockBilling, nil, nil)
		err := gw.AbortCancellation(context.Background(), userD)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no scheduled cancellation found")
	})
}

func TestAtlosGateway_AbortCancellation_NoActiveSubscription(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, TestUserID).Return(nil, nil)

		gw := New(ctx.Logger(), ctx, TestAPISecret, TestMerchantID, nil, nil, nil, mockBilling, nil, nil)
		err := gw.AbortCancellation(context.Background(), TestUserID)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no active ATLOS subscription found")
	})
}

func TestAtlosGateway_GetManagementInfo(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx.Logger(), ctx, TestAPISecret, TestMerchantID, nil, nil, nil, nil, nil, nil)

	info, err := gw.GetManagementInfo(context.Background(), TestUserID)
	assert.NoError(t, err)
	assert.NotNil(t, info)
	assert.Equal(t, pluginCore.ModeAPI, info.ManagementMode)
	assert.True(t, info.Operations[pluginCore.OperationCancel])
	assert.True(t, info.Operations[pluginCore.OperationChangePlan])
}

func TestAtlosGateway_GetManagementURL_Cancel(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		planID := uint(7)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:         TestUserID,
			GatewayType:    GatewayID,
			ExternalID:     TestTransactionID,
			SubscriptionID: TestSubscriptionID,
			IsActive:       true,
			PricingPlanPeriodID: &planID,
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, TestUserID).Return(mockSubscriber, nil)

		gw := New(ctx.Logger(), ctx, TestAPISecret, TestMerchantID, nil, nil, nil, mockBilling, nil, nil)
		result, err := gw.GetManagementURL(context.Background(), TestUserID, pluginCore.OperationCancel)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, pluginCore.ActionAPIRequired, result.Action)
		assert.NotNil(t, result.APIEndpoint)
	})
}

func TestAtlosGateway_GetManagementURL_ChangePlan(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		planID := uint(8)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:         TestUserID,
			GatewayType:    GatewayID,
			ExternalID:     TestTransactionID,
			SubscriptionID: TestSubscriptionID,
			IsActive:       true,
			PricingPlanPeriodID: &planID,
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, TestUserID).Return(mockSubscriber, nil)

		gw := New(ctx.Logger(), ctx, TestAPISecret, TestMerchantID, nil, nil, nil, mockBilling, nil, nil)
		result, err := gw.GetManagementURL(context.Background(), TestUserID, pluginCore.OperationChangePlan)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, pluginCore.ActionAPIRequired, result.Action)
		assert.NotNil(t, result.APIEndpoint)
		assert.Equal(t, pluginCore.ChangePlanEndpointPath, result.APIEndpoint.Path)
		assert.Equal(t, "POST", result.APIEndpoint.Method)
	})
}

func TestAtlosGateway_GetManagementURL_NoActiveSubscription(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, TestUserID).Return(nil, nil)

		gw := New(ctx.Logger(), ctx, TestAPISecret, TestMerchantID, nil, nil, nil, mockBilling, nil, nil)
		_, err := gw.GetManagementURL(context.Background(), TestUserID, pluginCore.OperationCancel)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no active Atlas subscription found")
	})
}

func TestAtlosGateway_GetManagementURL_UnsupportedOperation(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		planID := uint(9)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:         TestUserID,
			GatewayType:    GatewayID,
			ExternalID:     TestTransactionID,
			SubscriptionID: TestSubscriptionID,
			IsActive:       true,
			PricingPlanPeriodID: &planID,
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, TestUserID).Return(mockSubscriber, nil)

		gw := New(ctx.Logger(), ctx, TestAPISecret, TestMerchantID, nil, nil, nil, mockBilling, nil, nil)
		result, err := gw.GetManagementURL(context.Background(), TestUserID, pluginCore.ManagementOperation("unsupported"))

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, pluginCore.ActionUnsupported, result.Action)
		assert.Contains(t, result.ErrorMessage, "is not supported by ATLOS")
	})
}


func TestSetup(t *testing.T) {
	tests := []struct {
		name         string
		merchantID   string
		apiSecret    string
		expectGW     bool
		expectLogMsg bool
	}{
		{
			name:         "both configured",
			merchantID:   TestMerchantID,
			apiSecret:    TestAPISecret,
			expectGW:     true,
			expectLogMsg: true,
		},
		{
			name:         "missing merchant ID",
			merchantID:   "",
			apiSecret:    TestAPISecret,
			expectGW:     false,
			expectLogMsg: false,
		},
		{
			name:         "missing API secret",
			merchantID:   TestMerchantID,
			apiSecret:    "",
			expectGW:     false,
			expectLogMsg: false,
		},
		{
			name:         "both missing",
			merchantID:   "",
			apiSecret:    "",
			expectGW:     false,
			expectLogMsg: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := coreTesting.NewTestContext(t)
			opts := pluginCore.GatewaySetupOptions{
				Logger: ctx.Logger(),
			}

			logMsg, gw, err := Setup(opts, tt.apiSecret, tt.merchantID)
			assert.NoError(t, err)

			if tt.expectGW {
				assert.NotNil(t, gw)
				assert.NotEmpty(t, logMsg)
				assert.Contains(t, logMsg, "ATLOS gateway registered successfully")
			} else {
				assert.Nil(t, gw)
				assert.Empty(t, logMsg)
			}
		})
	}
}

func TestAtlosGateway_SetQuota(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx.Logger(), ctx, TestAPISecret, TestMerchantID, nil, nil, nil, nil, nil, nil)

	mockQuota := &quotaCore.MockQuotaService{}
	gw.SetQuota(mockQuota)

	assert.Equal(t, mockQuota, gw.quota)
}

func TestAtlosGateway_CreateOrUpdateSubscriber(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		planID := uint(10)
		mockBilling.On("CreateOrUpdateSubscriber",
			mock.AnythingOfType("*context.valueCtx"),
			TestUserID,
			GatewayID,
			TestTransactionID,
			TestSubscriptionID,
			true,
			&planID,
			mock.Anything, // Variadic SubscriberOption args
		).Return(nil)

		gw := New(ctx.Logger(), ctx, TestAPISecret, TestMerchantID, nil, nil, nil, mockBilling, nil, nil)
		err := gw.CreateOrUpdateSubscriber(context.Background(), TestUserID, TestTransactionID, TestSubscriptionID, true, &planID)

		assert.NoError(t, err)
	})
}

func TestAtlosGateway_DeactivateSubscriber(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		mockBilling.EXPECT().DeactivateSubscriber(mock.Anything, TestUserID, GatewayID).Return(nil)

		gw := New(ctx.Logger(), ctx, TestAPISecret, TestMerchantID, nil, nil, nil, mockBilling, nil, nil)
		err := gw.DeactivateSubscriber(context.Background(), TestUserID, GatewayID)

		assert.NoError(t, err)
	})
}

func TestAtlosGateway_GetLogo(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx.Logger(), ctx, TestAPISecret, TestMerchantID, nil, nil, nil, nil, nil, nil)

	logo, err := gw.GetLogo(context.Background())
	assert.NoError(t, err)
	assert.NotNil(t, logo)
	assert.NotEmpty(t, logo)
}

func createTestUser(id uint) *portalModels.User {
	return &portalModels.User{
		Model:     gorm.Model{ID: id},
		FirstName: "Test",
		LastName:  "User",
		Email:     "test@example.com",
	}
}
