package atlos

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/atlos-sdk"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	pluginConfig "go.lumeweb.com/portal-plugin-billing/internal/config"
	billingModels "go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/portal-plugin-billing/internal/gateway"
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

// mustGenerateOrderID generates an HMAC-signed order ID using the provided gateway.
// Panics on error for test convenience. The gateway must have been created with
// a context that has a valid identity for HMAC derivation.
func mustGenerateOrderID(tb testing.TB, gw *AtlosGateway, userID, periodID uint) string {
	if tb != nil {
		tb.Helper()
	}
	orderID, err := gw.GenerateOrderID(userID, periodID)
	if err != nil {
		panic(fmt.Sprintf("failed to generate order ID: %v", err))
	}
	return orderID
}

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
	gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, nil, nil, nil)
	assert.Equal(t, GatewayID, gw.ID(context.Background()))
}

func TestAtlosGateway_SignatureHeader(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, nil, nil, nil)
	assert.Equal(t, atlos.SignatureHeader, gw.SignatureHeader(context.Background()))
}

func TestAtlosGateway_GetName(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, nil, nil, nil)
	assert.Equal(t, "ATLOS", gw.GetName(context.Background()))
}

func TestAtlosGateway_GetDescription(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, nil, nil, nil)
	assert.Equal(t, "Accept crypto payments using the ATLOS payment widget", gw.GetDescription(context.Background()))
}

func TestAtlosGateway_SupportsProductSync(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, nil, nil, nil)
	assert.False(t, gw.SupportsProductSync())
}

func TestAtlosGateway_SyncPlan(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, nil, nil, nil)

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
	gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, nil, nil, nil)

	url, err := gw.GetCustomerPortalURL(context.Background(), TestUserID, "https://example.com/return")
	assert.Error(t, err)
	assert.Empty(t, url)
	assert.Contains(t, err.Error(), "customer portal not supported")
}

func TestAtlosGateway_GetCustomerPortalMetadata(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, nil, nil, nil)

	metadata, err := gw.GetCustomerPortalMetadata(context.Background(), TestUserID)
	assert.NoError(t, err)
	assert.NotNil(t, metadata)
	assert.Empty(t, metadata)
}

func TestAtlosGateway_ExtractEventID(t *testing.T) {
	tests := []struct {
		name         string
		setupPayload func(*AtlosGateway) ([]byte, string)
		expectError  bool
	}{
		{
			name: "valid postback notification",
			setupPayload: func(gw *AtlosGateway) ([]byte, string) {
				notification := atlos.PostbackNotification{
					TransactionId: TestTransactionID,
					OrderId:       mustGenerateOrderID(nil, gw, 123, 1),
					Amount:        10.0,
					Status:        100,
				}
				payload, _ := json.Marshal(notification)
				return payload, TestTransactionID
			},
			expectError: false,
		},
		{
			name: "empty transaction ID",
			setupPayload: func(gw *AtlosGateway) ([]byte, string) {
				notification := atlos.PostbackNotification{
					TransactionId: "",
					OrderId:       mustGenerateOrderID(nil, gw, 123, 1),
					Amount:        10.0,
					Status:        100,
				}
				payload, _ := json.Marshal(notification)
				return payload, ""
			},
			expectError: true,
		},
		{
			name: "invalid json payload",
			setupPayload: func(gw *AtlosGateway) ([]byte, string) {
				return []byte("invalid json"), ""
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
				gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, nil, nil, nil)
				payload, expectedID := tt.setupPayload(gw)

				eventID, err := gw.ExtractEventID(context.Background(), payload)
				if tt.expectError {
					assert.Error(t, err)
				} else {
					assert.NoError(t, err)
					assert.Equal(t, expectedID, eventID)
				}
			})
		})
	}
}

func TestAtlosGateway_ExtractEventType(t *testing.T) {
	tests := []struct {
		name         string
		setupPayload func(*AtlosGateway) ([]byte, string)
		expectError  bool
	}{
		{
			name: "valid postback notification",
			setupPayload: func(gw *AtlosGateway) ([]byte, string) {
				notification := atlos.PostbackNotification{
					TransactionId: TestTransactionID,
					OrderId:       mustGenerateOrderID(nil, gw, 123, 1),
					Amount:        10.0,
					Status:        100,
				}
				payload, _ := json.Marshal(notification)
				return payload, "payment.confirmed"
			},
			expectError: false,
		},
		{
			name: "invalid json payload",
			setupPayload: func(gw *AtlosGateway) ([]byte, string) {
				return []byte("invalid json"), ""
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
				gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, nil, nil, nil)
				payload, expectedType := tt.setupPayload(gw)

				eventType, err := gw.ExtractEventType(context.Background(), payload)
				if tt.expectError {
					assert.Error(t, err)
				} else {
					assert.NoError(t, err)
					assert.Equal(t, expectedType, eventType)
				}
			})
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
				notification := atlos.CreateTestPostback(TestMerchantID, atlos.WithDefaults())
				payload, _ := json.Marshal(notification)
				return payload
			}(),
			signature: func() string {
				notification := atlos.CreateTestPostback(TestMerchantID, atlos.WithDefaults())
				payload, _ := json.Marshal(notification)
				h := hmac.New(sha256.New, []byte(TestAPISecret))
				h.Write(payload)
				return base64.StdEncoding.EncodeToString(h.Sum(nil))
			}(),
			expectError: false,
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
			gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, nil, nil, nil)

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
		gwForOrder := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, nil, nil, nil)
		orderID, err := gwForOrder.GenerateOrderID(userID, periodID)
		require.NoError(t, err)

		notification := atlos.CreateTestPostback(TestMerchantID)
		notification.OrderId = orderID
		notification.TransactionId = TestTransactionID
		notification.SubscriptionId = TestSubscriptionID
		notification.OrderAmount = 10.0
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

		handlerGw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, mockQuota, mockUsers, mockBilling, mockPricing, mockCredit)
		err = handlerGw.HandleWebhook(context.Background(), payload)

		assert.NoError(t, err)
	})
}

func TestAtlosGateway_ExecutePlanChange_SamePeriod_ReturnsError(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		userID := uint(1)
		currentPeriodID := uint(20)

		start := time.Now().AddDate(0, -1, 0).UTC()
		end := time.Now().AddDate(0, 1, 0).UTC()

		mockSubscriber := &pluginCore.Subscriber{
			UserID:              userID,
			GatewayType:         GatewayID,
			ExternalID:          TestTransactionID,
			SubscriptionID:      TestSubscriptionID,
			IsActive:            true,
			PricingPlanPeriodID: &currentPeriodID,
			BillingPeriodStart:  &start,
			BillingPeriodEnd:    &end,
		}

		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(mockSubscriber, nil)

		gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, mockBilling, mockPricing, nil)
		result, err := gw.ExecutePlanChange(context.Background(), userID, currentPeriodID)

		assert.Error(tb, err)
		assert.Nil(tb, result)
		assert.Contains(tb, err.Error(), "already on the requested plan period")
	})
}

func TestAtlosGateway_ExecutePlanChange_CreditOnly_Downgrade_RoundedAmounts(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)
		mockCredit := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)

		userID := uint(1)
		oldPeriodID := uint(20)
		newPeriodID := uint(21)

		start := time.Now().AddDate(0, -1, 0).UTC()
		end := time.Now().AddDate(0, 1, 0).UTC()

		mockSubscriber := &pluginCore.Subscriber{
			UserID:              userID,
			GatewayType:         GatewayID,
			ExternalID:          TestTransactionID,
			SubscriptionID:      TestSubscriptionID,
			IsActive:            true,
			PricingPlanPeriodID: &oldPeriodID,
			BillingPeriodStart:  &start,
			BillingPeriodEnd:    &end,
		}

		oldPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: oldPeriodID},
			PricingPlanID: 1,
			Cadence:       "monthly",
			PriceUSD:      2.0,
		}
		newPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: newPeriodID},
			PricingPlanID: 2,
			Cadence:       "monthly",
			PriceUSD:      1.0,
		}
		newPlan := &billingModels.PricingPlan{
			Model:       gorm.Model{ID: 2},
			Name:        "Budget $1",
			Description: "Budget",
			IsActive:    true,
		}

		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(mockSubscriber, nil)
		mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, newPeriodID).Return(newPeriod, nil)
		mockPricing.EXPECT().GetPricingPlan(mock.Anything, uint(2)).Return(newPlan, nil)
		mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, oldPeriodID).Return(oldPeriod, nil)

		mockCredit.EXPECT().IssueCreditWithIdempotency(
			mock.Anything,
			uint64(userID),
			pluginCore.TransactionTypeRefund,
			mock.MatchedBy(func(amount decimal.Decimal) bool {
				s := amount.String()
				return len(s)-strings.Index(s, ".")-1 <= 2
			}),
			pluginCore.ReferenceTypeAtlosPayment,
			mock.Anything,
			mock.Anything,
			mock.Anything,
		).Return(nil)

		mockBilling.EXPECT().DeactivateSubscriber(mock.Anything, userID, GatewayID).Return(nil)
		mockBilling.EXPECT().CreateOrUpdateSubscriber(
			mock.Anything,
			userID,
			GatewayID,
			mock.Anything,
			mock.Anything,
			true,
			&newPeriodID,
			mock.Anything,
			mock.Anything,
		).Return(nil)

		gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, mockBilling, mockPricing, mockCredit)
		result, err := gw.ExecutePlanChange(context.Background(), userID, newPeriodID)

		require.NoError(tb, err)
		require.NotNil(tb, result)
		assert.Equal(tb, pluginCore.PlanChangeActionComplete, result.Action)

		creditStr := result.CreditApplied.String()
		if strings.Contains(creditStr, ".") {
			decimalPlaces := len(creditStr) - strings.Index(creditStr, ".") - 1
			assert.LessOrEqual(tb, decimalPlaces, 2, "CreditApplied should be rounded to 2 decimal places, got %s", creditStr)
		}

		chargeStr := result.ChargeDue.String()
		if strings.Contains(chargeStr, ".") {
			decimalPlaces := len(chargeStr) - strings.Index(chargeStr, ".") - 1
			assert.LessOrEqual(tb, decimalPlaces, 2, "ChargeDue should be rounded to 2 decimal places, got %s", chargeStr)
		}
	})
}

func TestAtlosGateway_ExecutePlanChange_CreditOnly_StableReferenceID(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)
		mockCredit := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)

		userID := uint(1)
		oldPeriodID := uint(20)
		newPeriodID := uint(21)

		start := time.Now().AddDate(0, -1, 0).UTC()
		end := time.Now().AddDate(0, 1, 0).UTC()

		mockSubscriber := &pluginCore.Subscriber{
			UserID:              userID,
			GatewayType:         GatewayID,
			ExternalID:          TestTransactionID,
			SubscriptionID:      TestSubscriptionID,
			IsActive:            true,
			PricingPlanPeriodID: &oldPeriodID,
			BillingPeriodStart:  &start,
			BillingPeriodEnd:    &end,
		}

		oldPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: oldPeriodID},
			PricingPlanID: 1,
			Cadence:       "monthly",
			PriceUSD:      2.0,
		}
		newPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: newPeriodID},
			PricingPlanID: 2,
			Cadence:       "monthly",
			PriceUSD:      1.0,
		}
		newPlan := &billingModels.PricingPlan{
			Model:       gorm.Model{ID: 2},
			Name:        "Budget $1",
			Description: "Budget",
			IsActive:    true,
		}

		expectedRefID := fmt.Sprintf("plan-change-credit-%d-%d", oldPeriodID, newPeriodID)

		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(mockSubscriber, nil)
		mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, newPeriodID).Return(newPeriod, nil)
		mockPricing.EXPECT().GetPricingPlan(mock.Anything, uint(2)).Return(newPlan, nil)
		mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, oldPeriodID).Return(oldPeriod, nil)

		mockCredit.EXPECT().IssueCreditWithIdempotency(
			mock.Anything,
			uint64(userID),
			pluginCore.TransactionTypeRefund,
			mock.Anything,
			pluginCore.ReferenceTypeAtlosPayment,
			expectedRefID,
			mock.Anything,
			mock.Anything,
		).Return(nil)

		mockBilling.EXPECT().DeactivateSubscriber(mock.Anything, userID, GatewayID).Return(nil)
		mockBilling.EXPECT().CreateOrUpdateSubscriber(
			mock.Anything,
			userID,
			GatewayID,
			mock.Anything,
			mock.Anything,
			true,
			&newPeriodID,
			mock.Anything,
			mock.Anything,
		).Return(nil)

		gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, mockBilling, mockPricing, mockCredit)
		result, err := gw.ExecutePlanChange(context.Background(), userID, newPeriodID)

		require.NoError(tb, err)
		require.NotNil(tb, result)
	})
}

func TestAtlosGateway_HandleWebhook_InvalidPayload(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, nil, nil, nil)

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

		gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, nil, mockPricing, nil)
		err := gw.HandleWebhook(context.Background(), payload)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse order ID")
	})
}

func TestAtlosGateway_HandleWebhook_PeriodNotFound(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		periodID := uint(999)
		gwForOrder := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, nil, nil, nil)
		orderID, _ := gwForOrder.GenerateOrderID(123, periodID)
		notification := atlos.CreateTestPostback(TestMerchantID)
		notification.OrderId = orderID
		payload, _ := json.Marshal(notification)

		mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, periodID).Return(nil, nil)

		gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, nil, mockPricing, nil)
		err := gw.HandleWebhook(context.Background(), payload)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "pricing plan period not found")
	})
}

func TestAtlosGateway_HandleWebhook_PlanNotActive(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		periodID := uint(3)
		planID := uint(2)
		gwForOrder := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, nil, nil, nil)
		orderID, _ := gwForOrder.GenerateOrderID(123, periodID)
		notification := atlos.CreateTestPostback(TestMerchantID)
		notification.OrderId = orderID
		payload, _ := json.Marshal(notification)

		period := &billingModels.PricingPlanPeriod{
			Model: gorm.Model{ID: periodID},
			PricingPlanID: planID,
			Cadence:       "monthly",
			PriceUSD:      10.0,
		}
		pricingPlan := &billingModels.PricingPlan{
			Model:       gorm.Model{ID: planID},
			Name:        "Test Plan",
			Description: "Test Description",
			IsActive:    false,
		}
		mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, periodID).Return(period, nil)
		mockPricing.EXPECT().GetPricingPlan(mock.Anything, planID).Return(pricingPlan, nil)

		gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, nil, mockPricing, nil)
		err := gw.HandleWebhook(context.Background(), payload)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "plan is not active")
	})
}

func TestAtlosGateway_HandleWebhook_AmountMismatch_CreditsWithoutSubscription(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)
		mockCredit := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		userID := uint(456)
		periodID := uint(2)
		gwForOrder := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, nil, nil, nil)
		orderID, err := gwForOrder.GenerateOrderID(userID, periodID)
		require.NoError(t, err)

		notification := atlos.CreateTestPostback(TestMerchantID)
		notification.OrderId = orderID
		notification.TransactionId = TestTransactionID
		notification.SubscriptionId = TestSubscriptionID
		notification.OrderAmount = 5.0 // Tampered in widget config
		notification.PaidAmount = 5.0  // ATLOS-calculated fiat from actual crypto paid
		payload, _ := json.Marshal(notification)

		// Plan price is $10 but PaidAmount is $5 (underpayment via tampered widget)
		period := &billingModels.PricingPlanPeriod{
			Model:        gorm.Model{ID: periodID},
			PricingPlanID: 1,
			Cadence:       "monthly",
			PriceUSD:      10.0,
		}
		pricingPlan := &billingModels.PricingPlan{
			Model:       gorm.Model{ID: 1},
			Name:        "Test Plan",
			Description: "Test Description",
			IsActive:    true,
		}

		mockPricing.EXPECT().GetPricingPlanPeriod(mock.AnythingOfType("*context.valueCtx"), periodID).Return(period, nil)
		mockPricing.EXPECT().GetPricingPlan(mock.AnythingOfType("*context.valueCtx"), uint(1)).Return(pricingPlan, nil)

		// Credit is issued for the paid amount, but NO subscription is created
		mockCredit.EXPECT().IssueCreditWithIdempotency(
			mock.AnythingOfType("*context.valueCtx"),
			uint64(userID),
			pluginCore.TransactionTypeCharge,
			mock.AnythingOfType("decimal.Decimal"),
			pluginCore.ReferenceTypeAtlosPayment,
			TestTransactionID,
			mock.AnythingOfType("string"),
			uint64(0),
		).Return(nil)

		// Billing should NOT be called — no subscription created
		mockBilling.EXPECT().GetActiveSubscriber(mock.Anything, mock.Anything, mock.Anything).Maybe()
		mockBilling.EXPECT().CreateOrUpdateSubscriber(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()

		handlerGw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, mockBilling, mockPricing, mockCredit)
		err = handlerGw.HandleWebhook(context.Background(), payload)

		assert.NoError(t, err)
	})
}

// TestAtlosGateway_HandleWebhook_ProratedUpgrade_WithCredit_MaliciousMismatch verifies that
// a prorated upgrade with existing credit applied correctly rejects a malicious payment that
// doesn't match the expected checkout amount. The scenario:
//   - Full proration = $2.00 (upgrade from $1/mo to $5/mo at midpoint)
//   - User has $0.50 credit → checkout amount sent to ATLOS = $1.50
//   - Malicious actor sends PaidAmount = $1.00 (underpayment relative to $1.50 expected)
//   - Handler should: credit the user for the paid amount, NOT create a subscription,
//     and NOT debit the existing credit (since the price match failed).
func TestAtlosGateway_HandleWebhook_ProratedUpgrade_WithCredit_MaliciousMismatch(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)
		mockCredit := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)

		userID := uint(1)
		oldPeriodID := uint(20)
		newPeriodID := uint(21)
		oldPlanID := uint(17)
		newPlanID := uint(18)

		gwForOrder := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, nil, nil, nil)
		orderID, err := gwForOrder.GenerateProratedOrderID(userID, oldPeriodID, newPeriodID)
		require.NoError(t, err)

		// Use 15 days ago (exactly halfway through 30-day monthly billing period) to get
		// a prorated amount that's exactly representable in float64:
		// - Remaining credit = $0.50 (exactly)
		// - New plan prorated charge = $2.50 (exactly)
		// - Net charge = $2.00 (exactly representable)
		now := time.Now().UTC()
		fifteenDaysAgo := now.Add(-15 * 24 * time.Hour)
		billingPeriodStart := fifteenDaysAgo
		billingPeriodEnd := subscription.CalculateFirstCycle(billingPeriodStart, subscription.CadenceMonthly).EndAt
		endedAt := now

		// Calculate expected proration using same logic as recalculateProrationAmount
		oldCycle := subscription.BillingCycle{
			StartAt: billingPeriodStart,
			EndAt:   billingPeriodEnd,
			Cadence: subscription.CadenceMonthly,
		}
		oldPrice := subscription.Price{Amount: decimal.NewFromFloat(1.00), Cadence: subscription.CadenceMonthly}
		newPrice := subscription.Price{Amount: decimal.NewFromFloat(5.00), Cadence: subscription.CadenceMonthly}
		prorationResult, err := subscription.ProratedChange(oldPrice, newPrice, oldCycle, endedAt, subscription.ProrationBehaviorCreateProrations)
		require.NoError(t, err)
		expectedProration := subscription.NetResult(prorationResult)

		prorationFloat, _ := expectedProration.Float64()

		// Simulate: user had $0.50 credit, checkout was $1.50, but malicious webhook
		// sends PaidAmount = $1.00 (underpayment). The handler should infer $0.50 credit
		// from user balance, compute expectedPrice = $2.00 - $0.50 = $1.50, then detect
		// that PaidAmount ($1.00) does NOT match expectedPrice ($1.50).
		userCreditBalance := decimal.NewFromFloat(0.50)
		maliciousPaidAmount := 1.00 // Should be $1.50 after credit deduction

		notification := atlos.CreateTestPostback(TestMerchantID)
		notification.OrderId = orderID
		notification.TransactionId = TestTransactionID
		notification.SubscriptionId = TestSubscriptionID
		notification.OrderAmount = prorationFloat
		notification.PaidAmount = maliciousPaidAmount
		payload, _ := json.Marshal(notification)

		oldPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: oldPeriodID},
			PricingPlanID: oldPlanID,
			Cadence:       "monthly",
			PriceUSD:      1.00,
		}
		newPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: newPeriodID},
			PricingPlanID: newPlanID,
			Cadence:       "monthly",
			PriceUSD:      5.00,
		}
		newPlan := &billingModels.PricingPlan{
			Model:       gorm.Model{ID: newPlanID},
			Name:        "Premium Plan",
			Description: "Premium Description",
			IsActive:    true,
		}

		history := &billingModels.SubscriptionHistory{
			UserID:              userID,
			PricingPlanID:       oldPlanID,
			PricingPlanPeriodID: oldPeriodID,
			PaymentGatewayType:  GatewayID,
			BillingPeriodStart:  &billingPeriodStart,
			BillingPeriodEnd:    &billingPeriodEnd,
			StartedAt:           fifteenDaysAgo,
			EndedAt:             endedAt,
		}

		mockPricing.EXPECT().GetPricingPlanPeriod(mock.AnythingOfType("*context.valueCtx"), newPeriodID).Return(newPeriod, nil)
		mockPricing.EXPECT().GetPricingPlan(mock.AnythingOfType("*context.valueCtx"), newPlanID).Return(newPlan, nil)
		mockBilling.EXPECT().GetSubscriptionHistoryByUserAndPeriod(mock.AnythingOfType("*context.valueCtx"), userID, oldPeriodID).Return(history, nil)
		mockPricing.EXPECT().GetPricingPlanPeriod(mock.AnythingOfType("*context.valueCtx"), oldPeriodID).Return(oldPeriod, nil)
		mockPricing.EXPECT().GetPricingPlanPeriod(mock.AnythingOfType("*context.valueCtx"), newPeriodID).Return(newPeriod, nil)

		// Handler will check user balance to infer credit applied
		mockCredit.EXPECT().GetUserBalance(mock.AnythingOfType("*context.valueCtx"), uint64(userID)).Return(userCreditBalance, nil)

		// Price mismatch: PaidAmount ($1.00) != expectedPrice ($1.50) after credit deduction.
		// Handler credits the paid amount but does NOT create subscription and does NOT debit credit.
		mockCredit.EXPECT().IssueCreditWithIdempotency(
			mock.AnythingOfType("*context.valueCtx"),
			uint64(userID),
			pluginCore.TransactionTypeCharge,
			mock.MatchedBy(func(amount decimal.Decimal) bool {
				return amount.Equal(decimal.NewFromFloat(1.00))
			}),
			pluginCore.ReferenceTypeAtlosPayment,
			TestTransactionID,
			mock.AnythingOfType("string"),
			uint64(0),
		).Return(nil)

		// Billing should NOT be called — no subscription created for mismatched payment
		mockBilling.EXPECT().GetActiveSubscriber(mock.Anything, mock.Anything, mock.Anything).Maybe()
		mockBilling.EXPECT().CreateOrUpdateSubscriber(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()

		// IssueUsageCredit (debit) should NOT be called — credit is NOT debited on mismatch
		mockCredit.EXPECT().IssueUsageCredit(
			mock.Anything, mock.Anything, mock.Anything, mock.Anything,
			mock.Anything, mock.Anything, mock.Anything,
		).Maybe()

		handlerGw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, mockBilling, mockPricing, mockCredit)
		err = handlerGw.HandleWebhook(context.Background(), payload)

		assert.NoError(t, err)
	})
}

func TestAtlosGateway_HandleWebhook_AmountMismatch_ZeroPaidAmount(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)
		mockCredit := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)

		userID := uint(456)
		periodID := uint(2)
		gwForOrder := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, nil, nil, nil)
		orderID, _ := gwForOrder.GenerateOrderID(userID, periodID)

		notification := atlos.CreateTestPostback(TestMerchantID)
		notification.OrderId = orderID
		notification.TransactionId = TestTransactionID
		notification.SubscriptionId = TestSubscriptionID
		notification.OrderAmount = 5.0 // Tampered
		notification.PaidAmount = 0.0  // Nothing paid
		payload, _ := json.Marshal(notification)

		period := &billingModels.PricingPlanPeriod{
			Model:        gorm.Model{ID: periodID},
			PricingPlanID: 1,
			Cadence:       "monthly",
			PriceUSD:      10.0,
		}
		pricingPlan := &billingModels.PricingPlan{
			Model:       gorm.Model{ID: 1},
			Name:        "Test Plan",
			Description: "Test Description",
			IsActive:    true,
		}

		mockPricing.EXPECT().GetPricingPlanPeriod(mock.AnythingOfType("*context.valueCtx"), periodID).Return(period, nil)
		mockPricing.EXPECT().GetPricingPlan(mock.AnythingOfType("*context.valueCtx"), uint(1)).Return(pricingPlan, nil)

		// No credit issued when PaidAmount is 0
		mockCredit.EXPECT().IssueCreditWithIdempotency(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()

		gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, nil, mockPricing, mockCredit)
		err := gw.HandleWebhook(context.Background(), payload)

		assert.NoError(t, err)
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

		gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, mockQuota, mockUsers, nil, mockPricing, nil)
		_, err := gw.GetCheckoutUI(context.Background(), TestUserID, TestPlanID, 1)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "user with ID")
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestBuildPaymentConfigData(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		period := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: 2},
			PricingPlanID: 1,
			Cadence:       "monthly",
			PriceUSD:      15.99,
		}
		userName := "Test User"
		userEmail := "test@example.com"
		merchantID := "merchant_123"
		gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, nil, nil, nil)
		orderID, _ := gw.GenerateOrderID(456, 2)
		postbackURL := "https://example.com" + gateway.WebhookPath + "/atlos"
		currency := "USD"

		data, err := buildPaymentConfigData(merchantID, orderID, period, currency, userName, userEmail, postbackURL)

	assert.NoError(t, err, "buildPaymentConfigData should not error")
	assert.Equal(t, "atlos-pay-btn-"+orderID, data.ButtonID, "button ID should be prefixed with atlos-pay-btn- and order ID")
	assert.NotEmpty(t, data.ConfigJSON, "config JSON should not be empty")

	// Verify the JSON contains expected fields
	var config map[string]interface{}
	err = json.Unmarshal([]byte(data.ConfigJSON), &config)
	assert.NoError(t, err, "config JSON should unmarshal")
	assert.Equal(t, merchantID, config["merchantId"], "merchant ID should match")
	assert.Equal(t, orderID, config["orderId"], "order ID should match")
	assert.Equal(t, 15.99, config["orderAmount"], "order amount should match period price")
	assert.Equal(t, currency, config["orderCurrency"], "currency should match")
	assert.Equal(t, userName, config["userName"], "user name should match")
	assert.Equal(t, userEmail, config["userEmail"], "user email should match")
	assert.Equal(t, postbackURL, config["postbackUrl"], "postback URL should match")

		// Verify subscription has proper unit value (3 = atlos.RECURRENCE_MONTH)
		subscription := config["subscription"].([]interface{})[0].(map[string]interface{})
		assert.Equal(t, 15.99, subscription["amount"], "subscription amount should match period price")
		assert.Equal(t, float64(3), subscription["unit"], "subscription unit should be RECURRENCE_MONTH (3)")
		assert.Equal(t, float64(1), subscription["interval"], "subscription interval should be 1")

		// Verify UI settings are in the config
		assert.Equal(t, "en", config["language"], "language should be 'en'")
		assert.Equal(t, "light", config["theme"], "theme should be 'light'")
	})
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

		gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, mockQuota, mockUsers, nil, mockPricing, nil)
		_, err := gw.GetCheckoutUI(context.Background(), TestUserID, TestPlanID, 999)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "period 999 not found for plan")
	})
}

func TestAtlosGateway_GetCheckoutUI_MissingUserService(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, nil, nil, nil)

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

		gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, mockQuota, mockUsers, nil, mockPricing, nil)
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

		gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, mockQuota, mockUsers, nil, mockPricing, nil)
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

		gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, mockQuota, mockUsers, nil, mockPricing, nil)
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

		gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, mockBilling, mockPricing, nil)
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

		gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, mockBilling, nil, nil)
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

		gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, mockBilling, nil, nil)
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

		gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, mockBilling, mockPricing, mockCredit)
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

		gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, mockBilling, mockPricing, mockCredit)
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

		gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, mockBilling, nil, nil)
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

		gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, mockBilling, nil, nil)
		err := gw.AbortCancellation(context.Background(), userD)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no scheduled cancellation found")
	})
}

func TestAtlosGateway_AbortCancellation_NoActiveSubscription(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, TestUserID).Return(nil, nil)

		gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, mockBilling, nil, nil)
		err := gw.AbortCancellation(context.Background(), TestUserID)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no active ATLOS subscription found")
	})
}

func TestAtlosGateway_GetManagementInfo(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, nil, nil, nil)

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

		gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, mockBilling, nil, nil)
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

		gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, mockBilling, nil, nil)
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

		gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, mockBilling, nil, nil)
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

		gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, mockBilling, nil, nil)
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

			logMsg, gw, err := Setup(opts, pluginConfig.AtlosConfig{APIKey: tt.apiSecret, MerchantID: tt.merchantID})
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
	gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, nil, nil, nil)

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

		gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, mockBilling, nil, nil)
		err := gw.CreateOrUpdateSubscriber(context.Background(), TestUserID, TestTransactionID, TestSubscriptionID, true, &planID)

		assert.NoError(t, err)
	})
}

func TestAtlosGateway_DeactivateSubscriber(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		mockBilling.EXPECT().DeactivateSubscriber(mock.Anything, TestUserID, GatewayID).Return(nil)

		gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, mockBilling, nil, nil)
		err := gw.DeactivateSubscriber(context.Background(), TestUserID, GatewayID)

		assert.NoError(t, err)
	})
}

func TestAtlosGateway_GetLogo(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, nil, nil, nil)

	logo, err := gw.GetLogo(context.Background())
	assert.NoError(t, err, "GetLogo should not return an error - check that assets/atlos.svg exists in the embedded filesystem")
	assert.NotNil(t, logo)
	assert.True(t, len(logo) > 0, "logo should contain valid SVG data")
}

func createTestUser(id uint) *portalModels.User {
	return &portalModels.User{
		Model:     gorm.Model{ID: id},
		FirstName: "Test",
		LastName:  "User",
		Email:     "test@example.com",
	}
}

// ATLOS Pause/Resume Tests - ATLOS does not support pause/resume

func TestAtlosGateway_GetManagementInfo_PauseResumeNotSupported(t *testing.T) {
	ctx, _ := coreTesting.NewTestContext(t)
	gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, nil, nil, nil)

	info, err := gw.GetManagementInfo(context.Background(), TestUserID)
	assert.NoError(t, err)
	assert.NotNil(t, info)

	// Pause and resume should not be in operations map (not supported)
	_, hasPause := info.Operations[pluginCore.OperationPause]
	_, hasResume := info.Operations[pluginCore.OperationResume]
	assert.False(t, hasPause)
	assert.False(t, hasResume)
}

func TestAtlosGateway_GetManagementURL_PauseUnsupported(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		planID := uint(10)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:              TestUserID,
			GatewayType:         GatewayID,
			ExternalID:          TestTransactionID,
			SubscriptionID:      TestSubscriptionID,
			IsActive:            true,
			PricingPlanPeriodID: &planID,
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, TestUserID).Return(mockSubscriber, nil)

		gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, mockBilling, nil, nil)
		result, err := gw.GetManagementURL(context.Background(), TestUserID, pluginCore.OperationPause)

		assert.NoError(tb, err)
		assert.NotNil(tb, result)
		assert.Equal(tb, pluginCore.ActionUnsupported, result.Action)
		assert.Contains(tb, result.ErrorMessage, "pause")
	})
}

func TestAtlosGateway_GetManagementURL_ResumeUnsupported(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		planID := uint(11)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:              TestUserID,
			GatewayType:         GatewayID,
			ExternalID:          TestTransactionID,
			SubscriptionID:      TestSubscriptionID,
			IsActive:            true,
			PricingPlanPeriodID: &planID,
		}
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, TestUserID).Return(mockSubscriber, nil)

		gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, mockBilling, nil, nil)
		result, err := gw.GetManagementURL(context.Background(), TestUserID, pluginCore.OperationResume)

		assert.NoError(tb, err)
		assert.NotNil(tb, result)
		assert.Equal(tb, pluginCore.ActionUnsupported, result.Action)
		assert.Contains(tb, result.ErrorMessage, "resume")
	})
}

// ExecutePlanChange tests

func TestAtlosGateway_ExecutePlanChange_CheckoutRequired_WithFragments(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockQuota := core.GetService[*quotaCore.MockQuotaService](ctx, quotaCore.QUOTA_SERVICE)
		mockUsers := core.GetService[*coreTesting.MockUserService](ctx, core.USER_SERVICE)
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)
		mockCredit := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)

		userID := uint(999)
		currentPeriodID := uint(5)
		newPeriodID := uint(6)

		// Billing period dates
		start := time.Now().AddDate(0, -1, 0).UTC()
		end := time.Now().AddDate(0, 1, 0).UTC()

		mockSubscriber := &pluginCore.Subscriber{
			UserID:              userID,
			GatewayType:         GatewayID,
			ExternalID:          TestTransactionID,
			SubscriptionID:      TestSubscriptionID,
			IsActive:            true,
			PricingPlanPeriodID: &currentPeriodID,
			BillingPeriodStart:  &start,
			BillingPeriodEnd:    &end,
		}

		// Pricing plan periods
		currentPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: currentPeriodID},
			PricingPlanID: 1,
			Cadence:       "monthly",
			PriceUSD:      10.0,
		}
		newPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: newPeriodID},
			PricingPlanID: 2,
			Cadence:       "monthly",
			PriceUSD:      25.0,
		}

		// Pricing plans
		newPlan := &billingModels.PricingPlan{
			Model:       gorm.Model{ID: 2},
			Name:        "Premium Plan",
			Description: "Premium Description",
			IsActive:    true,
		}

		// Get subscriber (checked first now)
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, userID).Return(mockSubscriber, nil)

		// Get new period and plan
		mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, newPeriodID).Return(newPeriod, nil)
		mockPricing.EXPECT().GetPricingPlan(mock.Anything, uint(2)).Return(newPlan, nil)

		// Get current period for proration calculation
		mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, currentPeriodID).Return(currentPeriod, nil)

		// Check existing credit balance — user has no credit, so checkout is still required
		mockCredit.EXPECT().GetUserBalance(mock.Anything, uint64(userID)).Return(decimal.Zero, nil)

		// User lookup for checkout UI generation
		mockUsers.EXPECT().AccountExists(mock.Anything, userID).Return(true, &portalModels.User{
			Model: gorm.Model{ID: userID},
			Email: "test@example.com",
		}, nil)

		// Cancel old subscription via ATLOS client - no direct mock, it's done via cancelSubscription method

		// Deactivate old subscriber
		mockBilling.EXPECT().DeactivateSubscriber(mock.Anything, userID, GatewayID).Return(nil)

		gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, mockQuota, mockUsers, mockBilling, mockPricing, mockCredit)
		result, err := gw.ExecutePlanChange(context.Background(), userID, newPeriodID)

		assert.NoError(tb, err)
		assert.NotNil(tb, result)
		assert.Equal(tb, pluginCore.PlanChangeActionCheckoutRequired, result.Action)
		assert.NotEmpty(tb, result.CheckoutLink)
		assert.True(tb, result.CreditApplied.GreaterThan(decimal.Zero))
		assert.True(tb, result.ChargeDue.GreaterThan(decimal.Zero))
		assert.NotNil(tb, result.EffectiveDate)
		assert.NotEmpty(tb, result.Fragments)

		// Verify fragments are present
		scriptFound := false
		buttonFound := false
		for _, frag := range result.Fragments {
			if frag.Type == pluginCore.FragmentTypeScript {
				scriptFound = true
				assert.NotEmpty(tb, frag.Script)
			}
			if frag.Type == pluginCore.FragmentTypeButton {
				buttonFound = true
				assert.NotEmpty(tb, frag.HTML)
			}
		}
		assert.True(tb, scriptFound, "script fragment should be present")
		assert.True(tb, buttonFound, "button fragment should be present")
	})
}

func TestAtlosGateway_ExecutePlanChange_NoActiveSubscription(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		// No active subscription
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, TestUserID).Return(nil, nil)

		gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, mockBilling, nil, nil)
		result, err := gw.ExecutePlanChange(context.Background(), TestUserID, uint(100))

		assert.Error(tb, err)
		assert.Nil(tb, result)
		assert.Contains(tb, err.Error(), "no active subscription found")
	})
}

func TestAtlosGateway_ExecutePlanChange_WrongGateway(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)

		mockSubscriber := &pluginCore.Subscriber{
			UserID:              TestUserID,
			GatewayType:         "stripe",
			ExternalID:          TestTransactionID,
			SubscriptionID:      TestSubscriptionID,
			IsActive:            true,
			PricingPlanPeriodID: func() *uint { v := uint(5); return &v }(),
		}

		// Get subscriber but it's on Stripe, not ATLOS
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, TestUserID).Return(mockSubscriber, nil)

		gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, mockBilling, nil, nil)
		result, err := gw.ExecutePlanChange(context.Background(), TestUserID, uint(100))

		assert.Error(tb, err)
		assert.Nil(tb, result)
		assert.Contains(tb, err.Error(), "not from ATLOS")
	})
}

func TestAtlosGateway_ExecutePlanChange_PeriodNotFound(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		currentPeriodID := uint(5)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:              TestUserID,
			GatewayType:         GatewayID,
			ExternalID:          TestTransactionID,
			SubscriptionID:      TestSubscriptionID,
			IsActive:            true,
			PricingPlanPeriodID: &currentPeriodID,
		}

		// Get subscriber
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, TestUserID).Return(mockSubscriber, nil)

		// New period not found
		mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, uint(999)).Return(nil, nil)

		gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, mockBilling, mockPricing, nil)
		result, err := gw.ExecutePlanChange(context.Background(), TestUserID, uint(999))

		assert.Error(tb, err)
		assert.Nil(tb, result)
		assert.Contains(tb, err.Error(), "not found")
	})
}

func TestAtlosGateway_ExecutePlanChange_PlanNotActive(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		currentPeriodID := uint(5)
		newPeriodID := uint(6)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:              TestUserID,
			GatewayType:         GatewayID,
			ExternalID:          TestTransactionID,
			SubscriptionID:      TestSubscriptionID,
			IsActive:            true,
			PricingPlanPeriodID: &currentPeriodID,
		}

		newPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: newPeriodID},
			PricingPlanID: 2,
			Cadence:       "monthly",
			PriceUSD:      25.0,
		}

		inactivePlan := &billingModels.PricingPlan{
			Model:       gorm.Model{ID: 2},
			Name:        "Premium Plan",
			Description: "Premium Description",
			IsActive:    false,
		}

		// Get subscriber
		mockBilling.EXPECT().GetActiveSubscription(mock.Anything, TestUserID).Return(mockSubscriber, nil)

		// Get new period and plan (but it's inactive)
		mockPricing.EXPECT().GetPricingPlanPeriod(mock.Anything, newPeriodID).Return(newPeriod, nil)
		mockPricing.EXPECT().GetPricingPlan(mock.Anything, uint(2)).Return(inactivePlan, nil)

		gw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, mockBilling, mockPricing, nil)
		result, err := gw.ExecutePlanChange(context.Background(), TestUserID, newPeriodID)

		assert.Error(tb, err)
		assert.Nil(tb, result)
		assert.Contains(tb, err.Error(), "not active")
	})
}

// TestAtlosGateway_HandleWebhook_ProratedUpgrade_MonthlyDollarPlan tests the prorated
// upgrade webhook flow: user signs up for $1/mo, then upgrades to $5/mo within 30 minutes.
// After the fix, recalculateProrationAmount uses EndedAt as prorationTime, producing
// the correct proration amount that matches PaidAmount, allowing subscription creation.
func TestAtlosGateway_HandleWebhook_ProratedUpgrade_MonthlyDollarPlan(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)
		mockCredit := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)

		userID := uint(1)
		oldPeriodID := uint(20)
		newPeriodID := uint(21)
		oldPlanID := uint(17)
		newPlanID := uint(18)

		gwForOrder := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, nil, nil, nil)
		orderID, err := gwForOrder.GenerateProratedOrderID(userID, oldPeriodID, newPeriodID)
		require.NoError(t, err)

		// Use 15 days ago (exactly halfway through 30-day monthly billing period) to get
		// a prorated amount that's exactly representable in float64:
		// - Remaining credit = $0.50 (exactly)
		// - New plan prorated charge = $2.50 (exactly)
		// - Net charge = $2.00 (exactly representable)
		now := time.Now().UTC()
		fifteenDaysAgo := now.Add(-15 * 24 * time.Hour)
		billingPeriodStart := fifteenDaysAgo
		billingPeriodEnd := subscription.CalculateFirstCycle(billingPeriodStart, subscription.CadenceMonthly).EndAt
		endedAt := now

		// Calculate expected proration using same logic as recalculateProrationAmount
		oldCycle := subscription.BillingCycle{
			StartAt: billingPeriodStart,
			EndAt:   billingPeriodEnd,
			Cadence: subscription.CadenceMonthly,
		}
		oldPrice := subscription.Price{Amount: decimal.NewFromFloat(1.00), Cadence: subscription.CadenceMonthly}
		newPrice := subscription.Price{Amount: decimal.NewFromFloat(5.00), Cadence: subscription.CadenceMonthly}
		prorationResult, err := subscription.ProratedChange(oldPrice, newPrice, oldCycle, endedAt, subscription.ProrationBehaviorCreateProrations)
		require.NoError(t, err)
		expectedProration := subscription.NetResult(prorationResult)

		prorationFloat, _ := expectedProration.Float64()

		notification := atlos.CreateTestPostback(TestMerchantID)
		notification.OrderId = orderID
		notification.TransactionId = TestTransactionID
		notification.SubscriptionId = TestSubscriptionID
		notification.OrderAmount = prorationFloat
		notification.PaidAmount = prorationFloat
		payload, _ := json.Marshal(notification)

		oldPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: oldPeriodID},
			PricingPlanID: oldPlanID,
			Cadence:       "monthly",
			PriceUSD:      1.00,
		}
		newPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: newPeriodID},
			PricingPlanID: newPlanID,
			Cadence:       "monthly",
			PriceUSD:      5.00,
		}
		newPlan := &billingModels.PricingPlan{
			Model:       gorm.Model{ID: newPlanID},
			Name:        "Premium Plan",
			Description: "Premium Description",
			IsActive:    true,
		}

		history := &billingModels.SubscriptionHistory{
			UserID:              userID,
			PricingPlanID:       oldPlanID,
			PricingPlanPeriodID: oldPeriodID,
			PaymentGatewayType:  GatewayID,
			BillingPeriodStart:  &billingPeriodStart,
			BillingPeriodEnd:    &billingPeriodEnd,
			StartedAt:           fifteenDaysAgo,
			EndedAt:             endedAt,
		}

		mockPricing.EXPECT().GetPricingPlanPeriod(mock.AnythingOfType("*context.valueCtx"), newPeriodID).Return(newPeriod, nil)
		mockPricing.EXPECT().GetPricingPlan(mock.AnythingOfType("*context.valueCtx"), newPlanID).Return(newPlan, nil)
		mockBilling.EXPECT().GetSubscriptionHistoryByUserAndPeriod(mock.AnythingOfType("*context.valueCtx"), userID, oldPeriodID).Return(history, nil)
		mockPricing.EXPECT().GetPricingPlanPeriod(mock.AnythingOfType("*context.valueCtx"), oldPeriodID).Return(oldPeriod, nil)
		mockPricing.EXPECT().GetPricingPlanPeriod(mock.AnythingOfType("*context.valueCtx"), newPeriodID).Return(newPeriod, nil)

		// After fix: expectedPrice matches PaidAmount → normal subscription flow
		mockBilling.EXPECT().GetActiveSubscriber(mock.AnythingOfType("*context.valueCtx"), userID, GatewayID).Return(nil, nil)

		// Usage credit (debit) for subscription period
		mockCredit.EXPECT().IssueUsageCredit(
			mock.AnythingOfType("*context.valueCtx"),
			uint64(userID),
			pluginCore.TransactionTypeTime,
			mock.MatchedBy(func(amount decimal.Decimal) bool {
				return amount.Equal(decimal.NewFromFloat(5.00))
			}),
			TestTransactionID,
			mock.AnythingOfType("string"),
			uint64(0),
		).Return(nil)

		// Create subscriber
		mockBilling.EXPECT().CreateOrUpdateSubscriber(
			mock.Anything,
			userID,
			GatewayID,
			TestTransactionID,
			TestSubscriptionID,
			true,
			&newPeriodID,
			mock.Anything,
			mock.Anything,
		).Return(nil)

		// Payment credit
		mockCredit.EXPECT().IssueCreditWithIdempotency(
			mock.AnythingOfType("*context.valueCtx"),
			uint64(userID),
			pluginCore.TransactionTypeCharge,
			mock.MatchedBy(func(amount decimal.Decimal) bool {
				// Matches the expected proration amount ($2.00) after float64 round-trip
				return amount.Equal(decimal.NewFromFloat(2.00))
			}),
			pluginCore.ReferenceTypeAtlosPayment,
			TestTransactionID,
			"ATLOS payment completed",
			uint64(0),
		).Return(nil)

		handlerGw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, mockBilling, mockPricing, mockCredit)
		err = handlerGw.HandleWebhook(context.Background(), payload)

		assert.NoError(t, err)
	})
}



// TestAtlosGateway_HandleWebhook_ProratedCancel_MonthlyDollarPlan tests a user who
// signs up for $1/mo, upgrades to $5/mo (prorated), then cancels.
// This test verifies the upgrade webhook portion uses the correct proration time.
func TestAtlosGateway_HandleWebhook_ProratedCancel_MonthlyDollarPlan(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)
		mockCredit := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)

		userID := uint(1)
		oldPeriodID := uint(20)
		newPeriodID := uint(21)
		oldPlanID := uint(17)
		newPlanID := uint(18)

		gwForOrder := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, nil, nil, nil)
		orderID, err := gwForOrder.GenerateProratedOrderID(userID, oldPeriodID, newPeriodID)
		require.NoError(t, err)

		// Use 15 days ago (exactly halfway through 30-day monthly billing period) to get
		// a prorated amount that's exactly representable in float64:
		// - Remaining credit = $0.50 (exactly)
		// - New plan prorated charge = $2.50 (exactly)
		// - Net charge = $2.00 (exactly representable)
		now := time.Now().UTC()
		fifteenDaysAgo := now.Add(-15 * 24 * time.Hour)
		billingPeriodStart := fifteenDaysAgo
		billingPeriodEnd := subscription.CalculateFirstCycle(billingPeriodStart, subscription.CadenceMonthly).EndAt
		endedAt := now

		// Calculate expected proration using same logic as recalculateProrationAmount
		oldCycle := subscription.BillingCycle{
			StartAt: billingPeriodStart,
			EndAt:   billingPeriodEnd,
			Cadence: subscription.CadenceMonthly,
		}
		oldPrice := subscription.Price{Amount: decimal.NewFromFloat(1.00), Cadence: subscription.CadenceMonthly}
		newPrice := subscription.Price{Amount: decimal.NewFromFloat(5.00), Cadence: subscription.CadenceMonthly}
		prorationResult, err := subscription.ProratedChange(oldPrice, newPrice, oldCycle, endedAt, subscription.ProrationBehaviorCreateProrations)
		require.NoError(t, err)
		expectedProration := subscription.NetResult(prorationResult)

		prorationFloat, _ := expectedProration.Float64()

		notification := atlos.CreateTestPostback(TestMerchantID)
		notification.OrderId = orderID
		notification.TransactionId = TestTransactionID
		notification.SubscriptionId = TestSubscriptionID
		notification.OrderAmount = prorationFloat
		notification.PaidAmount = prorationFloat
		payload, _ := json.Marshal(notification)

		oldPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: oldPeriodID},
			PricingPlanID: oldPlanID,
			Cadence:       "monthly",
			PriceUSD:      1.00,
		}
		newPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: newPeriodID},
			PricingPlanID: newPlanID,
			Cadence:       "monthly",
			PriceUSD:      5.00,
		}
		newPlan := &billingModels.PricingPlan{
			Model:       gorm.Model{ID: newPlanID},
			Name:        "Premium Plan",
			Description: "Premium Description",
			IsActive:    true,
		}
		history := &billingModels.SubscriptionHistory{
			UserID:              userID,
			PricingPlanID:       oldPlanID,
			PricingPlanPeriodID: oldPeriodID,
			PaymentGatewayType:  GatewayID,
			BillingPeriodStart:  &billingPeriodStart,
			BillingPeriodEnd:    &billingPeriodEnd,
			StartedAt:           fifteenDaysAgo,
			EndedAt:             endedAt,
		}

		mockPricing.EXPECT().GetPricingPlanPeriod(mock.AnythingOfType("*context.valueCtx"), newPeriodID).Return(newPeriod, nil)
		mockPricing.EXPECT().GetPricingPlan(mock.AnythingOfType("*context.valueCtx"), newPlanID).Return(newPlan, nil)
		mockBilling.EXPECT().GetSubscriptionHistoryByUserAndPeriod(mock.AnythingOfType("*context.valueCtx"), userID, oldPeriodID).Return(history, nil)
		mockPricing.EXPECT().GetPricingPlanPeriod(mock.AnythingOfType("*context.valueCtx"), oldPeriodID).Return(oldPeriod, nil)
		mockPricing.EXPECT().GetPricingPlanPeriod(mock.AnythingOfType("*context.valueCtx"), newPeriodID).Return(newPeriod, nil)

		// After fix: expectedPrice matches PaidAmount → normal subscription flow
		mockBilling.EXPECT().GetActiveSubscriber(mock.AnythingOfType("*context.valueCtx"), userID, GatewayID).Return(nil, nil)

		mockCredit.EXPECT().IssueUsageCredit(
			mock.AnythingOfType("*context.valueCtx"),
			uint64(userID),
			pluginCore.TransactionTypeTime,
			mock.MatchedBy(func(amount decimal.Decimal) bool {
				return amount.Equal(decimal.NewFromFloat(5.00))
			}),
			TestTransactionID,
			mock.AnythingOfType("string"),
			uint64(0),
		).Return(nil)

		mockBilling.EXPECT().CreateOrUpdateSubscriber(
			mock.Anything,
			userID,
			GatewayID,
			TestTransactionID,
			TestSubscriptionID,
			true,
			&newPeriodID,
			mock.Anything,
			mock.Anything,
		).Return(nil)

		mockCredit.EXPECT().IssueCreditWithIdempotency(
			mock.AnythingOfType("*context.valueCtx"),
			uint64(userID),
			pluginCore.TransactionTypeCharge,
			mock.MatchedBy(func(amount decimal.Decimal) bool {
				// The net proration should be exactly $2.00 (paid $2.50 for new plan - $0.50 credit remaining)
				return amount.Equal(decimal.NewFromFloat(2.00))
			}),
			pluginCore.ReferenceTypeAtlosPayment,
			TestTransactionID,
			"ATLOS payment completed",
			uint64(0),
		).Return(nil)

		handlerGw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, mockBilling, mockPricing, mockCredit)
		err = handlerGw.HandleWebhook(context.Background(), payload)

		assert.NoError(t, err)
	})
}


// TestAtlosGateway_HandleWebhook_ProratedDowngrade_MonthlyDollarPlan verifies the downgrade
// scenario where a downgrade produces a net credit. Since ATLOS PaidAmount is always positive
// but the expectedPrice is negative (net credit), the handler credits the user for $0
// and does NOT create a subscription — which is correct because downgrade credits are issued
// via handleCreditOnlyPlanChange, not checkout txns.
// This test verifies that the proration calculation function returns the correct (negative) value.
func TestAtlosGateway_HandleWebhook_ProratedDowngrade_MonthlyDollarPlan(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		mockBilling := core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE)
		mockPricing := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)
		mockCredit := core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE)

		userID := uint(1)
		oldPeriodID := uint(22)
		newPeriodID := uint(21)
		oldPlanID := uint(19)
		newPlanID := uint(18)

		gwForOrder := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, nil, nil, nil)
		orderID, err := gwForOrder.GenerateProratedOrderID(userID, oldPeriodID, newPeriodID)
		require.NoError(t, err)

		// Use 15 days ago (exactly halfway through 30-day monthly billing period) to get
		// predictable proration values
		now := time.Now().UTC()
		fifteenDaysAgo := now.Add(-15 * 24 * time.Hour)
		billingPeriodStart := fifteenDaysAgo
		billingPeriodEnd := subscription.CalculateFirstCycle(billingPeriodStart, subscription.CadenceMonthly).EndAt
		endedAt := now

		// Calculate expected proration using same logic as recalculateProrationAmount
		oldCycle := subscription.BillingCycle{
			StartAt: billingPeriodStart,
			EndAt:   billingPeriodEnd,
			Cadence: subscription.CadenceMonthly,
		}
		oldPrice := subscription.Price{Amount: decimal.NewFromFloat(10.00), Cadence: subscription.CadenceMonthly}
		newPrice := subscription.Price{Amount: decimal.NewFromFloat(5.00), Cadence: subscription.CadenceMonthly}
		prorationResult, err := subscription.ProratedChange(oldPrice, newPrice, oldCycle, endedAt, subscription.ProrationBehaviorCreateProrations)
		require.NoError(t, err)
		expectedProration := subscription.NetResult(prorationResult)

		// Downgrade produces negative net result (net credit to user)
		// Expected price will be negative, PaidAmount is 0 (no payment made for credit-only)
		require.True(t, expectedProration.IsNegative(), "downgrade should produce negative net result")

		notification := atlos.CreateTestPostback(TestMerchantID)
		notification.OrderId = orderID
		notification.TransactionId = TestTransactionID
		notification.SubscriptionId = TestSubscriptionID
		notification.OrderAmount = 0
		notification.PaidAmount = 0 // No payment for pure credit case
		payload, _ := json.Marshal(notification)

		oldPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: oldPeriodID},
			PricingPlanID: oldPlanID,
			Cadence:       "monthly",
			PriceUSD:      10.00,
		}
		newPeriod := &billingModels.PricingPlanPeriod{
			Model:         gorm.Model{ID: newPeriodID},
			PricingPlanID: newPlanID,
			Cadence:       "monthly",
			PriceUSD:      5.00,
		}
		newPlan := &billingModels.PricingPlan{
			Model:       gorm.Model{ID: newPlanID},
			Name:        "Standard Plan",
			Description: "Standard Description",
			IsActive:    true,
		}
		history := &billingModels.SubscriptionHistory{
			UserID:              userID,
			PricingPlanID:       oldPlanID,
			PricingPlanPeriodID: oldPeriodID,
			PaymentGatewayType:  GatewayID,
			BillingPeriodStart:  &billingPeriodStart,
			BillingPeriodEnd:    &billingPeriodEnd,
			StartedAt:           fifteenDaysAgo,
			EndedAt:             endedAt,
		}

		mockPricing.EXPECT().GetPricingPlanPeriod(mock.AnythingOfType("*context.valueCtx"), newPeriodID).Return(newPeriod, nil)
		mockPricing.EXPECT().GetPricingPlan(mock.AnythingOfType("*context.valueCtx"), newPlanID).Return(newPlan, nil)
		mockBilling.EXPECT().GetSubscriptionHistoryByUserAndPeriod(mock.AnythingOfType("*context.valueCtx"), userID, oldPeriodID).Return(history, nil)
		mockPricing.EXPECT().GetPricingPlanPeriod(mock.AnythingOfType("*context.valueCtx"), oldPeriodID).Return(oldPeriod, nil)
		mockPricing.EXPECT().GetPricingPlanPeriod(mock.AnythingOfType("*context.valueCtx"), newPeriodID).Return(newPeriod, nil)

		// PaidAmount (0) != expectedPrice (negative), so it takes the credit-only path
		// No subscription created, no credit issued (since PaidAmount is 0)
		mockBilling.EXPECT().GetActiveSubscriber(mock.Anything, mock.Anything, mock.Anything).Maybe()
		mockBilling.EXPECT().CreateOrUpdateSubscriber(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
		mockCredit.EXPECT().IssueCreditWithIdempotency(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()

		handlerGw := New(ctx, pluginConfig.AtlosConfig{APIKey: TestAPISecret, MerchantID: TestMerchantID}, nil, nil, nil, mockBilling, mockPricing, mockCredit)
		err = handlerGw.HandleWebhook(context.Background(), payload)

		assert.NoError(t, err)
	})
}
