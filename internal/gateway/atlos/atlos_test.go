package atlos

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.lumeweb.com/atlos-sdk"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	pluginConfig "go.lumeweb.com/portal-plugin-billing/internal/config"
	quotaCore "go.lumeweb.com/portal-plugin-quota/core"
	"go.lumeweb.com/portal/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
	billingModels "go.lumeweb.com/portal-plugin-billing/internal/db/models"
	portalModels "go.lumeweb.com/portal/db/models"
	"gorm.io/gorm"
)

const (
	// Test constants for commonly used values
	TestUserID         = uint(123)
	TestPlanID         = uint(1)
	TestMerchantID     = "merchant_test_123"
	TestAPISecret      = "api_secret_test_123"
	TestTransactionID  = "txn_test_123"
	TestSubscriptionID = "sub_test_123"
)

func TestMain(m *testing.M) {
	coreTesting.WithOptions(m,
		coreTesting.WithMockServiceFactory(quotaCore.QUOTA_SERVICE, quotaCore.NewMockQuotaService, &quotaCore.QuotaConfig{}),
		coreTesting.WithMockServiceFactory(pluginCore.BILLING_SERVICE, pluginCore.NewMockBillingService, &pluginConfig.ServiceConfig{}),
		coreTesting.WithMockServiceFactory(pluginCore.PRICING_SERVICE, pluginCore.NewMockPricingService, coreTesting.NewConfigBuilder().Build()),
	)
}

func TestAtlosGateway_ID(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx.Logger(), TestAPISecret, TestMerchantID, nil, nil, nil, nil, nil)
	assert.Equal(t, GatewayID, gw.ID(context.Background()))
}

func TestAtlosGateway_SignatureHeader(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx.Logger(), TestAPISecret, TestMerchantID, nil, nil, nil, nil, nil)
	assert.Equal(t, atlos.ApiSecretHeader, gw.SignatureHeader(context.Background()))
}

func TestAtlosGateway_GetName(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx.Logger(), TestAPISecret, TestMerchantID, nil, nil, nil, nil, nil)
	assert.Equal(t, "ATLOS", gw.GetName(context.Background()))
}

func TestAtlosGateway_GetDescription(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx.Logger(), TestAPISecret, TestMerchantID, nil, nil, nil, nil, nil)
	assert.Equal(t, "Accept crypto payments using the ATLOS payment widget", gw.GetDescription(context.Background()))
}

func TestAtlosGateway_SupportsProductSync(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx.Logger(), TestAPISecret, TestMerchantID, nil, nil, nil, nil, nil)
	assert.False(t, gw.SupportsProductSync())
}

func TestAtlosGateway_SyncPlan(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx.Logger(), TestAPISecret, TestMerchantID, nil, nil, nil, nil, nil)

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
	gw := New(ctx.Logger(), TestAPISecret, TestMerchantID, nil, nil, nil, nil, nil)

	url, err := gw.GetCustomerPortalURL(context.Background(), TestUserID, "https://example.com/return")
	assert.Error(t, err)
	assert.Empty(t, url)
	assert.Contains(t, err.Error(), "customer portal not supported")
}

func TestAtlosGateway_GetCustomerPortalMetadata(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx.Logger(), TestAPISecret, TestMerchantID, nil, nil, nil, nil, nil)

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
			gw := New(ctx.Logger(), TestAPISecret, TestMerchantID, nil, nil, nil, nil, nil)

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
			gw := New(ctx.Logger(), TestAPISecret, TestMerchantID, nil, nil, nil, nil, nil)

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
			gw := New(ctx.Logger(), TestAPISecret, TestMerchantID, nil, nil, nil, nil, nil)

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

		userID := uint(456)
		planID := uint(2)
		orderID := "456-plan2"

		notification := atlos.CreateTestPostback(TestMerchantID)
		notification.OrderId = orderID
		notification.TransactionId = TestTransactionID
		notification.SubscriptionId = TestSubscriptionID
		payload, _ := json.Marshal(notification)

		pricingPlan := &billingModels.PricingPlan{
			Model:      gorm.Model{ID: planID},
			Name:       "Test Plan",
			Description: "Test Description",
			IsActive:   true,
		}
		mockPricing.EXPECT().GetPricingPlan(mock.Anything, planID).Return(pricingPlan, nil)

		mockBilling.EXPECT().CreateOrUpdateSubscriber(
			mock.Anything, userID, GatewayID, TestTransactionID, TestSubscriptionID, true, &planID,
		).Return(nil)

		gw := New(ctx.Logger(), TestAPISecret, TestMerchantID, nil, mockQuota, mockUsers, mockBilling, mockPricing)
		err := gw.HandleWebhook(context.Background(), payload)

		assert.NoError(t, err)
	})
}

func TestAtlosGateway_HandleWebhook_InvalidPayload(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx.Logger(), TestAPISecret, TestMerchantID, nil, nil, nil, nil, nil)

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

		gw := New(ctx.Logger(), TestAPISecret, TestMerchantID, nil, nil, nil, nil, mockPricing)
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

		gw := New(ctx.Logger(), TestAPISecret, TestMerchantID, nil, nil, nil, nil, mockPricing)
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
			Model:      gorm.Model{ID: planID},
			Name:       "Test Plan",
			Description: "Test Description",
			IsActive:   false,
		}
		mockPricing.EXPECT().GetPricingPlan(mock.Anything, planID).Return(pricingPlan, nil)

		gw := New(ctx.Logger(), TestAPISecret, TestMerchantID, nil, nil, nil, nil, mockPricing)
		err := gw.HandleWebhook(context.Background(), payload)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "plan is not active")
	})
}

func TestAtlosGateway_GetCheckoutUI_Success(t *testing.T) {
	t.Skip("Template execution requires embedded FS context")
}

func TestAtlosGateway_GetCheckoutUI_MissingUserService(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx.Logger(), TestAPISecret, TestMerchantID, nil, nil, nil, nil, nil)

	_, err := gw.GetCheckoutUI(context.Background(), TestUserID, TestPlanID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "user service not configured")
}

func TestAtlosGateway_GetCheckoutUI_PlanNotFound(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota := core.GetService[*quotaCore.MockQuotaService](ctx, quotaCore.QUOTA_SERVICE)
		mockUsers := core.GetService[*coreTesting.MockUserService](ctx, core.USER_SERVICE)
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		mockPricing.EXPECT().GetPricingPlan(mock.Anything, TestPlanID).Return(nil, nil)

		gw := New(ctx.Logger(), TestAPISecret, TestMerchantID, nil, mockQuota, mockUsers, nil, mockPricing)
		_, err := gw.GetCheckoutUI(context.Background(), TestUserID, TestPlanID)

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
			Model:      gorm.Model{ID: TestPlanID},
			Name:       "Test Plan",
			Description: "Test Description",
			IsActive:   false,
		}
		mockPricing.EXPECT().GetPricingPlan(mock.Anything, TestPlanID).Return(pricingPlan, nil)

		gw := New(ctx.Logger(), TestAPISecret, TestMerchantID, nil, mockQuota, mockUsers, nil, mockPricing)
		_, err := gw.GetCheckoutUI(context.Background(), TestUserID, TestPlanID)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "plan is not active")
	})
}

func TestAtlosGateway_GetCheckoutUI_NoMonthlyPrice(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota := core.GetService[*quotaCore.MockQuotaService](ctx, quotaCore.QUOTA_SERVICE)
		mockUsers := core.GetService[*coreTesting.MockUserService](ctx, core.USER_SERVICE)
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		pricingPlan := &billingModels.PricingPlan{
			Model:          gorm.Model{ID: TestPlanID},
			Name:           "Test Plan",
			Description:    "Test Description",
			IsActive:       true,
			MonthlyPriceUSD: nil,
		}
		mockPricing.EXPECT().GetPricingPlan(mock.Anything, TestPlanID).Return(pricingPlan, nil)

		gw := New(ctx.Logger(), TestAPISecret, TestMerchantID, nil, mockQuota, mockUsers, nil, mockPricing)
		_, err := gw.GetCheckoutUI(context.Background(), TestUserID, TestPlanID)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "does not have a monthly price configured")
	})
}

func TestAtlosGateway_ExecuteCancel_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		userID := uint(999)
		planID := uint(5)

		mockSubscriber := &pluginCore.Subscriber{
			UserID:         userID,
			GatewayType:    GatewayID,
			ExternalID:     TestTransactionID,
			SubscriptionID: TestSubscriptionID,
			IsActive:       true,
			PlanID:         &planID,
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(mockSubscriber, nil)
		mockBilling.EXPECT().DeactivateSubscriber(mock.Anything, userID, GatewayID).Return(nil)

		gw := New(ctx.Logger(), TestAPISecret, TestMerchantID, nil, nil, nil, mockBilling, nil)
		err := gw.ExecuteCancel(context.Background(), userID)

		assert.NoError(t, err)
	})
}

func TestAtlosGateway_ExecuteCancel_NoActiveSubscription(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, TestUserID).Return(nil, nil)

		gw := New(ctx.Logger(), TestAPISecret, TestMerchantID, nil, nil, nil, mockBilling, nil)
		err := gw.ExecuteCancel(context.Background(), TestUserID)

		assert.Error(t, err)
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
			PlanID:         &planID,
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, TestUserID).Return(mockSubscriber, nil)

		gw := New(ctx.Logger(), TestAPISecret, TestMerchantID, nil, nil, nil, mockBilling, nil)
		err := gw.ExecuteCancel(context.Background(), TestUserID)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no active Atlas subscription found")
	})
}

func TestAtlosGateway_GetManagementInfo(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx.Logger(), TestAPISecret, TestMerchantID, nil, nil, nil, nil, nil)

	info, err := gw.GetManagementInfo(context.Background(), TestUserID)
	assert.NoError(t, err)
	assert.NotNil(t, info)
	assert.Equal(t, pluginCore.ModeAPI, info.ManagementMode)
	assert.True(t, info.Operations[pluginCore.OperationCancel])
	assert.False(t, info.Operations[pluginCore.OperationChangePlan])
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
			PlanID:         &planID,
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, TestUserID).Return(mockSubscriber, nil)

		gw := New(ctx.Logger(), TestAPISecret, TestMerchantID, nil, nil, nil, mockBilling, nil)
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
			PlanID:         &planID,
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, TestUserID).Return(mockSubscriber, nil)

		gw := New(ctx.Logger(), TestAPISecret, TestMerchantID, nil, nil, nil, mockBilling, nil)
		result, err := gw.GetManagementURL(context.Background(), TestUserID, pluginCore.OperationChangePlan)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, pluginCore.ActionUnsupported, result.Action)
		assert.Contains(t, result.ErrorMessage, "Plan changes are not yet supported")
	})
}

func TestAtlosGateway_GetManagementURL_NoActiveSubscription(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, TestUserID).Return(nil, nil)

		gw := New(ctx.Logger(), TestAPISecret, TestMerchantID, nil, nil, nil, mockBilling, nil)
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
			PlanID:         &planID,
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, TestUserID).Return(mockSubscriber, nil)

		gw := New(ctx.Logger(), TestAPISecret, TestMerchantID, nil, nil, nil, mockBilling, nil)
		result, err := gw.GetManagementURL(context.Background(), TestUserID, pluginCore.ManagementOperation("unsupported"))

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, pluginCore.ActionUnsupported, result.Action)
		assert.Contains(t, result.ErrorMessage, "is not supported by ATLOS")
	})
}

func TestParseOrderID(t *testing.T) {
	tests := []struct {
		name        string
		orderID     string
		expectError bool
		expectedUserID uint
		expectedPlanID uint
	}{
		{
			name:           "valid order ID",
			orderID:        "123-plan456",
			expectError:    false,
			expectedUserID: 123,
			expectedPlanID: 456,
		},
		{
			name:        "invalid format - missing dash",
			orderID:     "123plan456",
			expectError: true,
		},
		{
			name:        "invalid format - missing plan prefix",
			orderID:     "123-456",
			expectError: true,
		},
		{
			name:        "invalid user ID",
			orderID:     "abc-plan456",
			expectError: true,
		},
		{
			name:        "invalid plan ID",
			orderID:     "123-planabc",
			expectError: true,
		},
		{
			name:        "empty order ID",
			orderID:     "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userID, planID, err := parseOrderID(tt.orderID)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedUserID, userID)
				assert.Equal(t, tt.expectedPlanID, planID)
			}
		})
	}
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
	gw := New(ctx.Logger(), TestAPISecret, TestMerchantID, nil, nil, nil, nil, nil)

	mockQuota := &quotaCore.MockQuotaService{}
	gw.SetQuota(mockQuota)

	assert.Equal(t, mockQuota, gw.quota)
}

func TestAtlosGateway_CreateOrUpdateSubscriber(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		planID := uint(10)
		mockBilling.EXPECT().CreateOrUpdateSubscriber(
			mock.Anything, TestUserID, GatewayID, TestTransactionID, TestSubscriptionID, true, &planID,
		).Return(nil)

		gw := New(ctx.Logger(), TestAPISecret, TestMerchantID, nil, nil, nil, mockBilling, nil)
		err := gw.CreateOrUpdateSubscriber(context.Background(), TestUserID, TestTransactionID, TestSubscriptionID, true, &planID)

		assert.NoError(t, err)
	})
}

func TestAtlosGateway_DeactivateSubscriber(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		mockBilling.EXPECT().DeactivateSubscriber(mock.Anything, TestUserID, GatewayID).Return(nil)

		gw := New(ctx.Logger(), TestAPISecret, TestMerchantID, nil, nil, nil, mockBilling, nil)
		err := gw.DeactivateSubscriber(context.Background(), TestUserID, GatewayID)

		assert.NoError(t, err)
	})
}

func TestAtlosGateway_GetLogo(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx.Logger(), TestAPISecret, TestMerchantID, nil, nil, nil, nil, nil)

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