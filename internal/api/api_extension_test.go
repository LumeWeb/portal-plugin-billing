package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	billingEvent "go.lumeweb.com/portal-plugin-billing/internal/event"
	"go.lumeweb.com/portal-plugin-billing/internal"
	"go.lumeweb.com/portal-plugin-billing/internal/api/dto"
	pluginConfig "go.lumeweb.com/portal-plugin-billing/internal/config"
	"go.lumeweb.com/portal-plugin-billing/internal/gateway"
	"go.lumeweb.com/portal-plugin-billing/internal/service/pricing"
	"go.lumeweb.com/portal/core"
	coreTesting "go.lumeweb.com/portal/core/testing"
	"gorm.io/gorm"

	"go.lumeweb.com/queryutil"

	internalModels "go.lumeweb.com/portal-plugin-billing/internal/db/models"
)







// mockGatewayWithSession is a composite mock that implements both PaymentGateway and SessionStatusProvider
type mockGatewayWithSession struct {
	*pluginCore.MockPaymentGateway
	*pluginCore.MockSessionStatusProvider
}

// Ensure mockGatewayWithSession implements both interfaces
var _ pluginCore.PaymentGateway = (*mockGatewayWithSession)(nil)
var _ pluginCore.SessionStatusProvider = (*mockGatewayWithSession)(nil)

// testSetup holds common test dependencies
type testSetup struct {
	billingSvc *pluginCore.MockBillingService
	userSvc    *coreTesting.MockUserService
	creditSvc  *pluginCore.MockCreditService
	router     http.Handler
}

// setupTest creates common test dependencies
func setupTest(ctx coreTesting.TestContext) *testSetup {
	return &testSetup{
		billingSvc: core.GetService[*pluginCore.MockBillingService](ctx, pluginCore.BILLING_SERVICE),
		userSvc:    core.GetService[*coreTesting.MockUserService](ctx, core.USER_SERVICE),
		creditSvc:  core.GetService[*pluginCore.MockCreditService](ctx, pluginCore.CREDIT_SERVICE),
		router:     ctx.Router(),
	}
}

// createAuthenticatedRequest creates an authenticated HTTP request with a valid JWT token
func (ts *testSetup) createAuthenticatedRequest(ctx coreTesting.TestContext, method, url string, body []byte, userID string) (*http.Request, error) {
	// Create a test user ID (default to 1 if not specified)
	userIDUint := uint(1)
	if userID != "" {
		if id, err := strconv.ParseUint(userID, 10, 32); err == nil {
			userIDUint = uint(id)
		}
	}

	// Generate a JWT token directly without setting up LoginPassword expectations
	// The CreateTestLoginToken function creates a valid JWT token for testing
	userIDStr := strconv.Itoa(int(userIDUint))
	token := coreTesting.CreateTestLoginToken(ctx.T(), ctx, userIDStr)

	req := ctx.NewAPIRequest(method, url, body)
	req.Header.Set("Authorization", "Bearer "+token)

	return req, nil
}

// createMockSubscriber creates a mock subscriber with the given parameters
func createMockSubscriber(userID uint, gatewayType, externalID string, isActive bool, pricingPlanPeriodID *uint) *pluginCore.Subscriber {
	return &pluginCore.Subscriber{
		UserID:              userID,
		GatewayType:         gatewayType,
		ExternalID:          externalID,
		SubscriptionID:      "",
		IsActive:            isActive,
		PricingPlanPeriodID: pricingPlanPeriodID,
	}
}

// assertSubscriptionStatus verifies subscription status response
func assertSubscriptionStatus(t coreTesting.TB, response dto.SubscriptionStatusResponse, expectedSubscribed bool, expectedGateway string, expectedPricingPlanPeriodID *uint) {
	assert.Equal(t, expectedSubscribed, response.IsSubscribed)
	assert.Equal(t, expectedGateway, response.GatewayType)
	assert.Equal(t, expectedPricingPlanPeriodID, response.PricingPlanPeriodID)
}

func TestMain(m *testing.M) {
	// Base test setup without global API extensions.
	// Individual tests should call getUserAPITestOptions() or getAdminAPITestOptions()
	// as the third argument to RunTestCase.
	coreTesting.WithOptions(m,
		// Base configuration without API extensions
		coreTesting.WithServiceConfig(internal.PLUGIN_NAME, pluginCore.BILLING_SERVICE, &pluginConfig.ServiceConfig{}),
	)
}

func TestHandleWebhook_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock expectations
		ts.billingSvc.EXPECT().GetSignatureHeader(mock.Anything, "stripe").Return("Stripe-Signature", nil).Once()

		// Create a test webhook payload for a checkout.session.completed event
		webhookPayload := `{
			"id": "evt_test_webhook",
			"type": "checkout.session.completed",
			"data": {
				"object": {
					"id": "cs_test_session",
					"object": "checkout.session",
					"mode": "subscription",
					"client_reference_id": "1",
					"subscription": {
						"id": "sub_test_subscription"
					}
				}
			}
		}`

		ts.billingSvc.EXPECT().ProcessWebhook(mock.Anything, "stripe", "test_sig", []byte(webhookPayload)).
			Return(nil).Once()

		// Create request
		req := ctx.NewAPIRequest("POST", "/api/account/billing/webhooks/stripe", []byte(webhookPayload))
		req.Header.Set("Stripe-Signature", "test_sig")
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusNoContent, w.Code)

	}, getUserAPITestOptions())
}

func TestHandlePauseOperation_Success_APIBased(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock active subscription
		planID := uint(42)
		mockSubscriber := createMockSubscriber(1, "stripe", "sub_123", true, &planID)
		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(mockSubscriber, nil).Once()

		// Mock gateway
		mockGateway := pluginCore.NewMockPaymentGateway(tb)
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "stripe").Return(mockGateway, nil).Once()

		// Mock management capabilities - API mode with pause support
		capabilities := &pluginCore.ManagementCapabilities{
			ManagementMode: pluginCore.ModeAPI,
			Operations: map[pluginCore.ManagementOperation]bool{
				pluginCore.OperationPause: true,
			},
		}
		mockGateway.EXPECT().GetManagementInfo(mock.Anything, uint(1)).Return(capabilities, nil).Once()

		// ExecutePause is called directly
		mockGateway.EXPECT().ExecutePause(mock.Anything, uint(1)).Return(nil).Once()

		// Create authenticated request
		req, err := ts.createAuthenticatedRequest(ctx, "POST", "/api/account/billing/pause", nil, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		var response dto.ManagementResultResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(tb, err)

		assert.Equal(tb, pluginCore.ActionShowUI, response.Action)
		assert.Equal(tb, "paused", response.Status)

	}, getUserAPITestOptions())
}

func TestHandlePauseOperation_Success_PortalRedirect(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock active subscription
		planID := uint(42)
		mockSubscriber := createMockSubscriber(1, "stripe", "sub_123", true, &planID)
		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(mockSubscriber, nil).Once()

		// Mock gateway
		mockGateway := pluginCore.NewMockPaymentGateway(tb)
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "stripe").Return(mockGateway, nil).Once()

		// Mock management capabilities - Portal mode
		capabilities := &pluginCore.ManagementCapabilities{
			ManagementMode: pluginCore.ModePortal,
			Operations: map[pluginCore.ManagementOperation]bool{
				pluginCore.OperationPause: true,
			},
		}
		mockGateway.EXPECT().GetManagementInfo(mock.Anything, uint(1)).Return(capabilities, nil).Once()

		// Mock management result - redirect to portal
		managementResult := &pluginCore.ManagementResult{
			Action: pluginCore.ActionRedirect,
			URL:    "https://dashboard.stripe.com/customer/portal/session_pause",
		}
		mockGateway.EXPECT().GetManagementURL(mock.Anything, uint(1), pluginCore.OperationPause).Return(managementResult, nil).Once()

		// Create authenticated request
		req, err := ts.createAuthenticatedRequest(ctx, "POST", "/api/account/billing/pause", nil, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		var response dto.ManagementResultResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(tb, err)

		assert.Equal(tb, pluginCore.ActionRedirect, response.Action)
		assert.Equal(tb, "https://dashboard.stripe.com/customer/portal/session_pause", response.URL)

	}, getUserAPITestOptions())
}

func TestHandlePauseOperation_NotSupported(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock active subscription
		planID := uint(42)
		mockSubscriber := createMockSubscriber(1, "atlos", "sub_123", true, &planID)
		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(mockSubscriber, nil).Once()

		// Mock gateway
		mockGateway := pluginCore.NewMockPaymentGateway(tb)
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "atlos").Return(mockGateway, nil).Once()

		// Mock management capabilities - pause not supported
		capabilities := &pluginCore.ManagementCapabilities{
			ManagementMode: pluginCore.ModeAPI,
			Operations: map[pluginCore.ManagementOperation]bool{
				pluginCore.OperationPause: false,
			},
		}
		mockGateway.EXPECT().GetManagementInfo(mock.Anything, uint(1)).Return(capabilities, nil).Once()

		// Create authenticated request
		req, err := ts.createAuthenticatedRequest(ctx, "POST", "/api/account/billing/pause", nil, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return 400 Bad Request
		assert.Equal(tb, http.StatusBadRequest, w.Code)

		requireErrorResponse(tb, w, "ManagementOperationFailed")

	}, getUserAPITestOptions())
}

func TestHandleResumeOperation_Success_APIBased(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock active subscription
		planID := uint(42)
		mockSubscriber := createMockSubscriber(1, "stripe", "sub_123", true, &planID)
		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(mockSubscriber, nil).Once()

		// Mock gateway
		mockGateway := pluginCore.NewMockPaymentGateway(tb)
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "stripe").Return(mockGateway, nil).Once()

		// Mock management capabilities - API mode with resume support
		capabilities := &pluginCore.ManagementCapabilities{
			ManagementMode: pluginCore.ModeAPI,
			Operations: map[pluginCore.ManagementOperation]bool{
				pluginCore.OperationResume: true,
			},
		}
		mockGateway.EXPECT().GetManagementInfo(mock.Anything, uint(1)).Return(capabilities, nil).Once()

		// ExecuteResume is called directly
		mockGateway.EXPECT().ExecuteResume(mock.Anything, uint(1)).Return(nil).Once()

		// Create authenticated request
		req, err := ts.createAuthenticatedRequest(ctx, "POST", "/api/account/billing/resume", nil, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		var response dto.ManagementResultResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(tb, err)

		assert.Equal(tb, pluginCore.ActionShowUI, response.Action)
		assert.Equal(tb, "resumed", response.Status)

	}, getUserAPITestOptions())
}

func TestHandleResumeOperation_Success_PortalRedirect(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock active subscription
		planID := uint(42)
		mockSubscriber := createMockSubscriber(1, "stripe", "sub_123", true, &planID)
		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(mockSubscriber, nil).Once()

		// Mock gateway
		mockGateway := pluginCore.NewMockPaymentGateway(tb)
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "stripe").Return(mockGateway, nil).Once()

		// Mock management capabilities - Portal mode
		capabilities := &pluginCore.ManagementCapabilities{
			ManagementMode: pluginCore.ModePortal,
			Operations: map[pluginCore.ManagementOperation]bool{
				pluginCore.OperationResume: true,
			},
		}
		mockGateway.EXPECT().GetManagementInfo(mock.Anything, uint(1)).Return(capabilities, nil).Once()

		// Mock management result - redirect to portal
		managementResult := &pluginCore.ManagementResult{
			Action: pluginCore.ActionRedirect,
			URL:    "https://dashboard.stripe.com/customer/portal/session_resume",
		}
		mockGateway.EXPECT().GetManagementURL(mock.Anything, uint(1), pluginCore.OperationResume).Return(managementResult, nil).Once()

		// Create authenticated request
		req, err := ts.createAuthenticatedRequest(ctx, "POST", "/api/account/billing/resume", nil, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		var response dto.ManagementResultResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(tb, err)

		assert.Equal(tb, pluginCore.ActionRedirect, response.Action)
		assert.Equal(tb, "https://dashboard.stripe.com/customer/portal/session_resume", response.URL)

	}, getUserAPITestOptions())
}

func TestHandleResumeOperation_NotSupported(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock active subscription
		planID := uint(42)
		mockSubscriber := createMockSubscriber(1, "atlos", "sub_123", true, &planID)
		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(mockSubscriber, nil).Once()

		// Mock gateway
		mockGateway := pluginCore.NewMockPaymentGateway(tb)
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "atlos").Return(mockGateway, nil).Once()

		// Mock management capabilities - resume not supported
		capabilities := &pluginCore.ManagementCapabilities{
			ManagementMode: pluginCore.ModeAPI,
			Operations: map[pluginCore.ManagementOperation]bool{
				pluginCore.OperationResume: false,
			},
		}
		mockGateway.EXPECT().GetManagementInfo(mock.Anything, uint(1)).Return(capabilities, nil).Once()

		// Create authenticated request
		req, err := ts.createAuthenticatedRequest(ctx, "POST", "/api/account/billing/resume", nil, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return 400 Bad Request
		assert.Equal(tb, http.StatusBadRequest, w.Code)

		requireErrorResponse(tb, w, "ManagementOperationFailed")

	}, getUserAPITestOptions())
}

// Customer Portal Tests

func TestHandleCustomerPortal_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock active subscription
		planID := uint(42)
		mockSubscriber := createMockSubscriber(1, "stripe", "cus_123", true, &planID)
		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(mockSubscriber, nil).Once()

		// Mock gateway
		mockGateway := pluginCore.NewMockPaymentGateway(tb)
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "stripe").Return(mockGateway, nil).Once()

		// Mock management result - redirect to portal
		managementResult := &pluginCore.ManagementResult{
			Action: pluginCore.ActionRedirect,
			URL:    "https://billing.stripe.com/session/abc123",
		}
		mockGateway.EXPECT().GetManagementURL(mock.Anything, uint(1), pluginCore.OperationCustomerPortal).Return(managementResult, nil).Once()

		// Create authenticated request
		req, err := ts.createAuthenticatedRequest(ctx, "POST", "/api/account/billing/customer-portal", nil, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		var response dto.ManagementResultResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(tb, err)

		assert.Equal(tb, pluginCore.ActionRedirect, response.Action)
		assert.Equal(tb, "https://billing.stripe.com/session/abc123", response.URL)

	}, getUserAPITestOptions())
}

func TestHandleCustomerPortal_PausedSubscription(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// No active subscription
		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(nil, nil).Once()

		// Mock paused subscription
		planID := uint(42)
		mockSubscriber := createMockSubscriber(1, "stripe", "cus_123", false, &planID)
		ts.billingSvc.EXPECT().GetPausedSubscription(mock.Anything, uint(1)).Return(mockSubscriber, nil).Once()

		// Mock gateway
		mockGateway := pluginCore.NewMockPaymentGateway(tb)
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "stripe").Return(mockGateway, nil).Once()

		// Mock management result - redirect to portal
		managementResult := &pluginCore.ManagementResult{
			Action: pluginCore.ActionRedirect,
			URL:    "https://billing.stripe.com/session/paused_sub",
		}
		mockGateway.EXPECT().GetManagementURL(mock.Anything, uint(1), pluginCore.OperationCustomerPortal).Return(managementResult, nil).Once()

		// Create authenticated request
		req, err := ts.createAuthenticatedRequest(ctx, "POST", "/api/account/billing/customer-portal", nil, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		var response dto.ManagementResultResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(tb, err)

		assert.Equal(tb, pluginCore.ActionRedirect, response.Action)
		assert.Equal(tb, "https://billing.stripe.com/session/paused_sub", response.URL)

	}, getUserAPITestOptions())
}

func TestHandleCustomerPortal_NoSubscription(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// No active subscription
		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(nil, nil).Once()

		// No paused subscription
		ts.billingSvc.EXPECT().GetPausedSubscription(mock.Anything, uint(1)).Return(nil, nil).Once()

		// Create authenticated request
		req, err := ts.createAuthenticatedRequest(ctx, "POST", "/api/account/billing/customer-portal", nil, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return 404 Not Found
		assert.Equal(tb, http.StatusNotFound, w.Code)

		requireErrorResponse(tb, w, "NoActiveSubscription")

	}, getUserAPITestOptions())
}

func TestHandleCustomerPortal_GetManagementURLError(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock active subscription
		planID := uint(42)
		mockSubscriber := createMockSubscriber(1, "stripe", "cus_123", true, &planID)
		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(mockSubscriber, nil).Once()

		// Mock gateway
		mockGateway := pluginCore.NewMockPaymentGateway(tb)
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "stripe").Return(mockGateway, nil).Once()

		// Mock error from GetManagementURL
		mockGateway.EXPECT().GetManagementURL(mock.Anything, uint(1), pluginCore.OperationCustomerPortal).Return(nil, fmt.Errorf("portal session creation failed")).Once()

		// Create authenticated request
		req, err := ts.createAuthenticatedRequest(ctx, "POST", "/api/account/billing/customer-portal", nil, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return 500 Internal Server Error
		assert.Equal(tb, http.StatusInternalServerError, w.Code)

		requireErrorResponse(tb, w, "ManagementOperationFailed")

	}, getUserAPITestOptions())
}

// SSE API Tests

// sseTestHelper provides utilities for SSE testing
type sseTestHelper struct {
	tb        coreTesting.TB
	ctx       coreTesting.TestContext
	setup     *testSetup
	userToken string
}

// sseConnection manages an active SSE connection for testing
type sseConnection struct {
	req       *http.Request
	rec       *httptest.ResponseRecorder
	done      chan bool
	cancel    context.CancelFunc
	closed    bool
	tb        coreTesting.TB
}

// StartSSEConnection creates and starts an SSE connection in a goroutine
// Returns a connection struct that can be used to wait for results or cancel the connection
func (h *sseTestHelper) StartSSEConnection() *sseConnection {
	
	// Create a cancelable context for the request
	reqCtx, cancel := context.WithCancel(context.Background())
	
	// Create the request with the cancelable context
	req := h.ctx.NewAPIRequest("GET", "/api/account/billing/subscription/events", nil)
	req = req.WithContext(reqCtx)
	req.Header.Set("Authorization", "Bearer "+h.userToken)
	
	rec := httptest.NewRecorder()
	
	conn := &sseConnection{
		req:    req,
		rec:    rec,
		done:   make(chan bool, 1),
		cancel: cancel,
		closed: false,
		tb:     h.tb,
	}
	
	go func() {
		h.setup.router.ServeHTTP(rec, req)
		conn.done <- true
	}()
	
	return conn
}

// WaitForConnection waits for the SSE connection to establish
func (conn *sseConnection) WaitForConnection() {
	const connectionDelay = 100 * time.Millisecond
	time.Sleep(connectionDelay)
}

// WaitForEvent waits for an event to be delivered
func (conn *sseConnection) WaitForEvent() {
	const eventDelay = 200 * time.Millisecond
	time.Sleep(eventDelay)
}

// Cancel closes the SSE connection by canceling the request context
func (conn *sseConnection) Cancel() {
	if conn.closed {
		return
	}
	
	conn.cancel()
}

// WaitForClose waits for the SSE connection goroutine to complete
// Returns true if connection closed within timeout, false otherwise
func (conn *sseConnection) WaitForClose(timeout time.Duration) bool {
	select {
	case <-conn.done:
		conn.closed = true
		return true
	case <-time.After(timeout):
		conn.tb.Errorf("WaitForClose: Connection did not close within %v", timeout)
		preview := conn.rec.Body.String()
		if len(preview) > 500 {
			preview = preview[:500]
		}
		return false
	}
}

// GetResponseBody returns the full response body received so far
func (conn *sseConnection) GetResponseBody() string {
	body := conn.rec.Body.String()
	return body
}

// Helper method to fire a PaymentCompletedEvent
func (h *sseTestHelper) FirePaymentCompletedEvent(amount decimal.Decimal, gateway, invoiceID, externalID string) {
	paymentEvent := billingEvent.NewPaymentCompletedEvent(
		context.Background(),
		1, // Always user ID 1 for the helper
		amount,
		gateway,
		invoiceID,
		externalID,
	)
	
	// Fire[P any](ctx Context, eventName string, data *P)
	// If P = PaymentCompletedEvent, then data *P = *PaymentCompletedEvent
	// NewPaymentCompletedEvent returns *PaymentCompletedEvent, so pass it directly
	core.Fire(h.ctx, billingEvent.EVENT_PAYMENT_COMPLETED, paymentEvent)
}

// setupSSETest creates a helper for SSE testing
func setupSSETest(tb coreTesting.TB, ctx coreTesting.TestContext) *sseTestHelper {
	ts := setupTest(ctx)
	
	ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)
	
	token := coreTesting.CreateTestLoginToken(ctx.T(), ctx, "1")
	
	return &sseTestHelper{
		tb:        tb,
		ctx:       ctx,
		setup:     ts,
		userToken: token,
	}
}

func TestHandleSubscriptionSSE_Unauthenticated(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Create unauthenticated SSE request
		req := ctx.NewAPIRequest("GET", "/api/account/billing/subscription/events", nil)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return unauthorized
		assert.Equal(tb, http.StatusUnauthorized, w.Code)

	}, getUserAPITestOptions())
}

func TestHandleSubscriptionSSE_ConnectionEstablished(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		helper := setupSSETest(tb, ctx)

		conn := helper.StartSSEConnection()

		conn.WaitForConnection()

		// Note: SSE headers are written immediately when connection establishes
		// We can check them before canceling the connection

		// Verify SSE response headers
		assert.Equal(tb, http.StatusOK, conn.rec.Code)
		assert.Contains(tb, conn.rec.Header().Get("Content-Type"), "text/event-stream")
		assert.Equal(tb, "no-cache", conn.rec.Header().Get("Cache-Control"))
		assert.Equal(tb, "keep-alive", conn.rec.Header().Get("Connection"))

		conn.Cancel()

		conn.WaitForClose(2 * time.Second)

	}, getUserAPITestOptions())
}

func TestHandleSubscriptionSSE_PaymentCompletedEvent(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		helper := setupSSETest(tb, ctx)

		conn := helper.StartSSEConnection()

		conn.WaitForConnection()

		amount, _ := decimal.NewFromString("10.99")
		helper.FirePaymentCompletedEvent(amount, "stripe", "inv_123", "pi_456")

		// Wait longer for first event (buffering may take time)
		time.Sleep(300 * time.Millisecond)

		conn.Cancel()

		if !conn.WaitForClose(2 * time.Second) {
			return // Test already failed in WaitForClose
		}

		// Verify SSE event was sent
		responseBody := conn.GetResponseBody()

		// Should contain the event type
		assert.Contains(tb, responseBody, "event: payment.completed")

		// Should contain event data
		assert.Contains(tb, responseBody, `"amount":"10.99"`)
		assert.Contains(tb, responseBody, `"gateway":"stripe"`)
		assert.Contains(tb, responseBody, `"invoice_id":"inv_123"`)

		// Verify JSON structure
		assert.Contains(tb, responseBody, `"type":"payment.completed"`)
		assert.Contains(tb, responseBody, `"data":{`)

	}, getUserAPITestOptions())
}

func TestHandleSubscriptionSSE_UserTopicIsolation(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock user account validation for user 1
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Create SSE request for user 1 with cancelable context
		token1 := coreTesting.CreateTestLoginToken(ctx.T(), ctx, "1")
		req1Ctx, req1Cancel := context.WithCancel(context.Background())
		req1 := ctx.NewAPIRequest("GET", "/api/account/billing/subscription/events", nil)
		req1 = req1.WithContext(req1Ctx)
		req1.Header.Set("Authorization", "Bearer "+token1)

		rec1 := httptest.NewRecorder()

		// Start SSE connection for user 1
		connDone1 := make(chan bool, 1)
		go func() {
			ts.router.ServeHTTP(rec1, req1)
			connDone1 <- true
		}()

		// Wait for connection
		time.Sleep(100 * time.Millisecond)

		// Fire event for user 2 (should not reach user 1)
		amount, _ := decimal.NewFromString("15.00")
		event := billingEvent.NewPaymentCompletedEvent(
			context.Background(),
			2, // Different user ID
			amount,
			"stripe",
			"inv_user2",
			"pi_user2",
		)
		core.Fire(ctx, billingEvent.EVENT_PAYMENT_COMPLETED, event)

		// Wait
		time.Sleep(200 * time.Millisecond)

		// Fire event for user 1
		event1 := billingEvent.NewPaymentCompletedEvent(
			context.Background(),
			1,
			amount,
			"stripe",
			"inv_user1",
			"pi_user1",
		)
		core.Fire(ctx, billingEvent.EVENT_PAYMENT_COMPLETED, event1)

		// Wait for event
		time.Sleep(200 * time.Millisecond)

		// Cancel the SSE connection
		req1Cancel()

		select {
		case <-connDone1:
		case <-time.After(2 * time.Second):
			tb.Error("Connection did not close")
		}

		// Verify user 1 only received their own events
		responseBody := rec1.Body.String()

		// Should contain user 1's event
		assert.Contains(tb, responseBody, `"invoice_id":"inv_user1"`)

		// Should NOT contain user 2's event
		assert.NotContains(tb, responseBody, `"invoice_id":"inv_user2"`)

	}, getUserAPITestOptions())
}

func TestHandleSubscriptionSSE_EventFieldsExcludedFromSSE(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		helper := setupSSETest(tb, ctx)

		conn := helper.StartSSEConnection()

		conn.WaitForConnection()

		amount, _ := decimal.NewFromString("25.00")
		helper.FirePaymentCompletedEvent(amount, "stripe", "inv_test", "pi_test")

		conn.WaitForEvent()

		conn.Cancel()

		if !conn.WaitForClose(2 * time.Second) {
			return // Test already failed in WaitForClose
		}

		// Verify UserID field is excluded from JSON
		responseBody := conn.GetResponseBody()

		// Should NOT contain user_id field
		assert.NotContains(tb, responseBody, `"user_id"`)

		// Should contain other fields
		assert.Contains(tb, responseBody, `"invoice_id":"inv_test"`)
		assert.Contains(tb, responseBody, `"gateway":"stripe"`)

	}, getUserAPITestOptions())
}

func TestHandleWebhook_InvalidGateway(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock expectations
		ts.billingSvc.EXPECT().GetSignatureHeader(mock.Anything, "invalid").Return("", pluginCore.ErrGatewayNotFound).Once()

		// Create request
		req := ctx.NewAPIRequest("POST", "/api/account/billing/webhooks/invalid", []byte(`{"test":"payload"}`))
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusNotFound, w.Code)

	}, getUserAPITestOptions())
}

func TestHandleSubscriptionStatus_ActiveSubscription(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock the billing service to return an active subscription
		planID := uint(42)
		mockSubscriber := createMockSubscriber(1, "stripe", "cus_123", true, &planID)
		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(mockSubscriber, nil)

		// Create authenticated request
		req, err := ts.createAuthenticatedRequest(ctx, "GET", "/api/account/billing/subscription", nil, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		// Parse response using DTO
		var response dto.SubscriptionStatusResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(tb, err)

		assertSubscriptionStatus(tb, response, true, "stripe", &planID)

	}, getUserAPITestOptions())
}

func TestHandleSubscriptionStatus_NoActiveSubscription(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock the billing service to return no active subscription
		// This covers both scenarios: no subscription exists and inactive subscriptions
		// (GetActiveSubscription only returns active subscriptions)
		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return((*pluginCore.Subscriber)(nil), nil)
		ts.billingSvc.EXPECT().GetPausedSubscription(mock.Anything, uint(1)).Return((*pluginCore.Subscriber)(nil), nil)

		// Create authenticated request
		req, err := ts.createAuthenticatedRequest(ctx, "GET", "/api/account/billing/subscription", nil, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		// Parse response using DTO
		var response dto.SubscriptionStatusResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(tb, err)

		// Both no subscription and inactive subscription scenarios should return the same response
		assertSubscriptionStatus(tb, response, false, "", nil)
	}, getUserAPITestOptions())
}

func TestHandleSubscriptionStatus_PausedSubscription(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		planID := uint(42)
		pausedAt := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
		pausedSub := &pluginCore.Subscriber{
			UserID:              1,
			GatewayType:         "stripe",
			ExternalID:          "cus_123",
			SubscriptionID:      "sub_paused",
			IsActive:            false,
			PricingPlanPeriodID: &planID,
			PausedAt:            &pausedAt,
		}
		pausedSub.CreatedAt = time.Now()
		pausedSub.UpdatedAt = time.Now()

		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(nil, nil).Once()
		ts.billingSvc.EXPECT().GetPausedSubscription(mock.Anything, uint(1)).Return(pausedSub, nil).Once()

		req, err := ts.createAuthenticatedRequest(ctx, "GET", "/api/account/billing/subscription", nil, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()
		ts.router.ServeHTTP(w, req)

		assert.Equal(tb, http.StatusOK, w.Code)

		var response dto.SubscriptionStatusResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(tb, err)

		assert.False(tb, response.IsSubscribed)
		assert.Equal(tb, "stripe", response.GatewayType)
		assert.Equal(tb, planID, *response.PricingPlanPeriodID)
		assert.NotNil(tb, response.PausedAt)
		assert.Equal(tb, pausedAt, *response.PausedAt)
	}, getUserAPITestOptions())
}

func TestHandleSubscriptionStatus_NoSubscriptionAtAll(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(nil, nil).Once()
		ts.billingSvc.EXPECT().GetPausedSubscription(mock.Anything, uint(1)).Return(nil, nil).Once()

		req, err := ts.createAuthenticatedRequest(ctx, "GET", "/api/account/billing/subscription", nil, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()
		ts.router.ServeHTTP(w, req)

		assert.Equal(tb, http.StatusOK, w.Code)

		var response dto.SubscriptionStatusResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(tb, err)

		assertSubscriptionStatus(tb, response, false, "", nil)
	}, getUserAPITestOptions())
}

func TestHandleSubscriptionStatus_MultipleGateways(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock the billing service to return an active subscription (could be any gateway)
		planID := uint(99)
		mockSubscriber := createMockSubscriber(1, "paypal", "cus_456", true, &planID)
		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(mockSubscriber, nil)

		// Create authenticated request
		req, err := ts.createAuthenticatedRequest(ctx, "GET", "/api/account/billing/subscription", nil, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		// Parse response using DTO
		var response dto.SubscriptionStatusResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(tb, err)

		// Should return the mocked subscription
		assertSubscriptionStatus(tb, response, true, "paypal", &planID)
	}, getUserAPITestOptions())
}

func TestHandleSubscriptionStatus_Unauthorized(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Create unauthenticated request (no auth header)
		req := ctx.NewAPIRequest("GET", "/api/account/billing/subscription", nil)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return unauthorized
		assert.Equal(tb, http.StatusUnauthorized, w.Code)
	}, getUserAPITestOptions())
}

// mockFullGateway embeds MockPaymentGateway (which satisfies CheckoutProvider and CustomerPortal)
// plus MockSessionStatusProvider to satisfy all public ability interfaces.
type mockFullGateway struct {
	*pluginCore.MockPaymentGateway
	*pluginCore.MockSessionStatusProvider
}

func newMockFullGateway(t *testing.T) *mockFullGateway {
	return &mockFullGateway{
		MockPaymentGateway:        pluginCore.NewMockPaymentGateway(t),
		MockSessionStatusProvider: pluginCore.NewMockSessionStatusProvider(t),
	}
}

func TestHandleGetGateways_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Create a new registry for testing
		mockRegistry := gateway.NewRegistry()
		mockGateway1 := newMockFullGateway(t)
		mockGateway1.MockPaymentGateway.EXPECT().ID(mock.Anything).Return("stripe").Once()
		mockGateway1.MockPaymentGateway.EXPECT().GetName(mock.Anything).Return("Stripe").Once()
		mockGateway1.MockPaymentGateway.EXPECT().GetDescription(mock.Anything).Return("Industry-leading payment processor").Once()
		mockGateway2 := pluginCore.NewMockPaymentGateway(t)
		mockGateway2.EXPECT().ID(mock.Anything).Return("paypal").Once()
		mockGateway2.EXPECT().GetName(mock.Anything).Return("PayPal").Once()
		mockGateway2.EXPECT().GetDescription(mock.Anything).Return("Fast and secure payments").Once()

		// Register gateways manually for test
		ctxForReg := context.Background()
		err := mockRegistry.Register(ctxForReg, mockGateway1)
		assert.NoError(tb, err)
		err = mockRegistry.Register(ctxForReg, mockGateway2)
		assert.NoError(tb, err)

		ts.billingSvc.EXPECT().GetRegistry(mock.Anything).Return(mockRegistry).Once()

		// Create request
		req := ctx.NewAPIRequest("GET", "/api/billing/gateways", nil)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		// Parse response
		var response dto.GatewayListResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(tb, err)
		assert.Len(tb, response, 2)

		// Verify gateways by ID (order not guaranteed)
		gatewayMap := make(map[string]dto.GatewayPublicInfo)
		for _, gateway := range response {
			gatewayMap[gateway.ID] = gateway
		}

		// Verify stripe gateway
		stripeGateway, exists := gatewayMap["stripe"]
		assert.True(tb, exists)
		assert.Equal(tb, "Stripe", stripeGateway.Name)
		assert.Equal(tb, "Industry-leading payment processor", stripeGateway.Description)
		assert.Equal(tb, "/api/billing/gateways/stripe/logo", stripeGateway.LogoURL)
		assert.True(tb, stripeGateway.IsActive)
		assert.True(tb, stripeGateway.Abilities.Checkout)
		assert.True(tb, stripeGateway.Abilities.SessionStatus)
		assert.True(tb, stripeGateway.Abilities.CustomerPortal)

		// Verify paypal gateway
		paypalGateway, exists := gatewayMap["paypal"]
		assert.True(tb, exists)
		assert.Equal(tb, "PayPal", paypalGateway.Name)
		assert.Equal(tb, "Fast and secure payments", paypalGateway.Description)
		assert.Equal(tb, "/api/billing/gateways/paypal/logo", paypalGateway.LogoURL)
		assert.True(tb, paypalGateway.IsActive)
	}, getUserAPITestOptions())
}

func TestHandleGetGateways_EmptyRegistry(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Create empty registry for testing
		mockRegistry := gateway.NewRegistry()

		ts.billingSvc.EXPECT().GetRegistry(mock.Anything).Return(mockRegistry).Once()

		// Create request
		req := ctx.NewAPIRequest("GET", "/api/billing/gateways", nil)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		// Parse response
		var response dto.GatewayListResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(tb, err)
		assert.Len(tb, response, 0)
	}, getUserAPITestOptions())
}

func TestHandleGetGateways_RegistryNil(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock nil registry
		ts.billingSvc.EXPECT().GetRegistry(mock.Anything).Return(pluginCore.GatewayRegistry(nil)).Once()

		// Create request
		req := ctx.NewAPIRequest("GET", "/api/billing/gateways", nil)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return internal server error
		assert.Equal(tb, http.StatusInternalServerError, w.Code)

		requireErrorResponse(tb, w, "GatewayRegistryNotInitialized")
	}, getUserAPITestOptions())
}

func TestHandleGetGatewayLogo_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Create mock gateway
		mockGateway := pricing.NewMockMockablePaymentGateway(tb)

		// Mock getting gateway from billing service
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "stripe").
			Return(mockGateway, nil).
			Once()

		// Mock getting logo from gateway
		logoData := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<svg width="100" height="100" xmlns="http://www.w3.org/2000/svg">
  <rect width="100" height="100" fill="red"/>
</svg>`)
		mockGateway.EXPECT().GetLogo(mock.Anything).
			Return(logoData, nil).
			Once()

		// Create request for stripe gateway
		req := ctx.NewAPIRequest("GET", "/api/billing/gateways/stripe/logo", nil)

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return OK with logo data
		assert.Equal(tb, http.StatusOK, w.Code)
		assert.True(tb, len(w.Body.Bytes()) > 0)
		assert.Contains(tb, w.Header().Get("Content-Type"), "svg")
	}, getUserAPITestOptions())
}

func TestHandleGetGatewayLogo_NotFound(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock gateway not found
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "nonexistent").
			Return(nil, pluginCore.ErrGatewayNotFound).
			Once()

		// Create request for non-existent gateway
		req := ctx.NewAPIRequest("GET", "/api/billing/gateways/nonexistent/logo", nil)

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return not found
		assert.Equal(tb, http.StatusNotFound, w.Code)
	}, getUserAPITestOptions())
}

func TestHandleGetGatewayLogo_GetLogoError(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Create mock gateway
		mockGateway := pricing.NewMockMockablePaymentGateway(tb)

		// Mock getting gateway from billing service
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "stripe").
			Return(mockGateway, nil).
			Once()

		// Mock GetLogo returning an error
		mockGateway.EXPECT().GetLogo(mock.Anything).
			Return(nil, fmt.Errorf("logo file not found")).
			Once()

		// Create request for stripe gateway
		req := ctx.NewAPIRequest("GET", "/api/billing/gateways/stripe/logo", nil)

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return not found
		assert.Equal(tb, http.StatusNotFound, w.Code)
	}, getUserAPITestOptions())
}

func TestHandleGetGatewayLogo_PNGContentType(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Create mock gateway
		mockGateway := pricing.NewMockMockablePaymentGateway(tb)

		// Mock getting gateway from billing service
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "stripe").
			Return(mockGateway, nil).
			Once()

		// Mock getting a PNG logo from gateway
		// PNG magic bytes: 89 50 4E 47 0D 0A 1A 0A
		logoData := []byte{
			0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
			// Minimal PNG
			0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
			// ... more PNG data would follow
		}
		mockGateway.EXPECT().GetLogo(mock.Anything).
			Return(logoData, nil).
			Once()

		// Create request for stripe gateway
		req := ctx.NewAPIRequest("GET", "/api/billing/gateways/stripe/logo", nil)

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return OK and PNG content type
		assert.Equal(tb, http.StatusOK, w.Code)
		assert.True(tb, len(w.Body.Bytes()) > 0)
		assert.Contains(tb, w.Header().Get("Content-Type"), "png")
	}, getUserAPITestOptions())
}

func TestHandleGetGatewayLogo_ContentTypeFallback_Unknown(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Create mock gateway
		mockGateway := pricing.NewMockMockablePaymentGateway(tb)

		// Mock getting gateway from billing service
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "stripe").
			Return(mockGateway, nil).
			Once()

		// Mock getting content that mimetype detects as text/plain
		logoData := []byte("not-a-real-image-type")
		mockGateway.EXPECT().GetLogo(mock.Anything).
			Return(logoData, nil).
			Once()

		// Create request for stripe gateway
		req := ctx.NewAPIRequest("GET", "/api/billing/gateways/stripe/logo", nil)

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return OK, mimetype will detect as text/plain
		assert.Equal(tb, http.StatusOK, w.Code)
		assert.Contains(tb, w.Header().Get("Content-Type"), "text/plain")
	}, getUserAPITestOptions())
}

func TestHandleGetGatewayLogo_EmptyData(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Create mock gateway
		mockGateway := pricing.NewMockMockablePaymentGateway(tb)

		// Mock getting gateway from billing service
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "stripe").
			Return(mockGateway, nil).
			Once()

		// Mock getting empty logo data
		logoData := []byte{}
		mockGateway.EXPECT().GetLogo(mock.Anything).
			Return(logoData, nil).
			Once()

		// Create request for stripe gateway
		req := ctx.NewAPIRequest("GET", "/api/billing/gateways/stripe/logo", nil)

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return OK even with empty data
		assert.Equal(tb, http.StatusOK, w.Code)
	}, getUserAPITestOptions())
}

func TestHandleListPricingPlans_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Get pricing service
		pricingSvc := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		// Mock effective price line
		priceLine := &internalModels.PriceLine{
			Model: gorm.Model{ID: 100},
			Name:  "Default Price Line",
		}
		pricingSvc.EXPECT().GetEffectivePriceLineForUser(mock.Anything, uint(1)).Return(priceLine, nil).Once()

		// Mock pricing plan periods for plan 1
		plan1Periods := []*internalModels.PricingPlanPeriod{
			{
				Model:         gorm.Model{ID: 10},
				PricingPlanID: 1,
				Cadence:       "monthly",
				PriceUSD:      10.99,
				QuotaPlanID:   100,
			},
			{
				Model:         gorm.Model{ID: 11},
				PricingPlanID: 1,
				Cadence:       "yearly",
				PriceUSD:      99.99,
				QuotaPlanID:   100,
			},
		}
		pricingSvc.EXPECT().GetPricingPlanPeriods(mock.Anything, uint(1)).Return(plan1Periods, nil).Once()

		// Mock pricing plan periods for plan 2
		plan2Periods := []*internalModels.PricingPlanPeriod{
			{
				Model:         gorm.Model{ID: 20},
				PricingPlanID: 2,
				Cadence:       "monthly",
				PriceUSD:      29.99,
				QuotaPlanID:   200,
			},
		}
		pricingSvc.EXPECT().GetPricingPlanPeriods(mock.Anything, uint(2)).Return(plan2Periods, nil).Once()

		// Mock pricing plans
		plans := []*internalModels.PricingPlan{
			{
				Model:        gorm.Model{ID: 1},
				Name:         "Basic Plan",
				Description:  "Entry level plan",
				Currency:     "USD",
				IsActive:     true,
				IsPublic:     true,
			},
			{
				Model:        gorm.Model{ID: 2},
				Name:         "Pro Plan",
				Description:  "Professional plan",
				Currency:     "USD",
				IsActive:     true,
				IsPublic:     true,
			},
		}
		pricingSvc.EXPECT().GetPlansForPriceLine(mock.Anything, uint(100)).Return(plans, nil).Once()

		// Create authenticated request
		req, requestErr := ts.createAuthenticatedRequest(ctx, "GET", "/api/billing/plans", nil, "1")
		assert.NoError(tb, requestErr, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		// Parse response
		var response queryutil.Response[[]dto.PublicPricingPlanResponse]
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(tb, err)
		assert.Len(tb, response.Data, 2)

		// Verify plan 1
		assert.Equal(tb, uint(1), response.Data[0].ID)
		assert.Equal(tb, "Basic Plan", response.Data[0].Name)
		assert.Equal(tb, "Entry level plan", response.Data[0].Description)
		assert.Equal(tb, "USD", response.Data[0].Currency)
		assert.Len(tb, response.Data[0].PricingPeriods, 2)
		assert.Equal(tb, uint(10), response.Data[0].PricingPeriods[0].ID)
		assert.Equal(tb, "monthly", response.Data[0].PricingPeriods[0].Cadence)
		assert.Equal(tb, 10.99, response.Data[0].PricingPeriods[0].PriceUSD)
		assert.Equal(tb, uint(11), response.Data[0].PricingPeriods[1].ID)
		assert.Equal(tb, "yearly", response.Data[0].PricingPeriods[1].Cadence)

		// Verify plan 2
		assert.Equal(tb, uint(2), response.Data[1].ID)
		assert.Equal(tb, "Pro Plan", response.Data[1].Name)
		assert.Len(tb, response.Data[1].PricingPeriods, 1)
		assert.Equal(tb, uint(20), response.Data[1].PricingPeriods[0].ID)
		assert.Equal(tb, "monthly", response.Data[1].PricingPeriods[0].Cadence)
		assert.Equal(tb, 29.99, response.Data[1].PricingPeriods[0].PriceUSD)

	}, getUserAPITestOptions())
}

func TestHandleListPricingPlans_Unauthenticated(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Get pricing service
		pricingSvc := core.GetService[*pluginCore.MockPricingService](ctx, pluginCore.PRICING_SERVICE)

		// Mock default price line
		priceLine := &internalModels.PriceLine{
			Model: gorm.Model{ID: 999},
			Name:  "Default Price Line",
		}
		pricingSvc.EXPECT().GetDefaultPriceLine(mock.Anything).Return(priceLine, nil).Once()

		// Mock pricing plan periods
		planPeriods := []*internalModels.PricingPlanPeriod{
			{
				Model:         gorm.Model{ID: 10},
				PricingPlanID: 1,
				Cadence:       "monthly",
				PriceUSD:      10.99,
				QuotaPlanID:   100,
			},
		}
		pricingSvc.EXPECT().GetPricingPlanPeriods(mock.Anything, uint(1)).Return(planPeriods, nil).Once()

		// Mock pricing plans
		plans := []*internalModels.PricingPlan{
			{
				Model:        gorm.Model{ID: 1},
				Name:         "Basic Plan",
				Description:  "Entry level plan",
				Currency:     "USD",
				IsActive:     true,
				IsPublic:     true,
			},
		}
		pricingSvc.EXPECT().GetPlansForPriceLine(mock.Anything, uint(999)).Return(plans, nil).Once()

		// Create unauthenticated request (no auth header)
		req := ctx.NewAPIRequest("GET", "/api/billing/plans", nil)

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return OK with default plans
		assert.Equal(tb, http.StatusOK, w.Code)

		// Parse response
		var response queryutil.Response[[]dto.PublicPricingPlanResponse]
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(tb, err)
		assert.Len(tb, response.Data, 1)

		// Verify plan details
		assert.Equal(tb, uint(1), response.Data[0].ID)
		assert.Equal(tb, "Basic Plan", response.Data[0].Name)
		assert.Len(tb, response.Data[0].PricingPeriods, 1)
		assert.Equal(tb, uint(10), response.Data[0].PricingPeriods[0].ID)
		assert.Equal(tb, "monthly", response.Data[0].PricingPeriods[0].Cadence)

	}, getUserAPITestOptions())
}

// Checkout UI Tests

func TestHandleGetCheckoutUI_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		planID := uint(42)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock GetCheckoutUI response
		checkoutResponse := &pluginCore.CheckoutUIResponse{
			SessionID: "sess_123",
			ExpiresAt: createTestExpirationTime(),
			Metadata: map[string]any{
				"plan_id": planID,
			},
			Fragments: []pluginCore.CheckoutUIFragment{
				{
					Type: pluginCore.FragmentTypeLink,
					Link: "https://checkout.stripe.com/pay/sess_123",
				},
			},
		}

		// Mock GetCheckoutUI on billing service
		ts.billingSvc.EXPECT().GetCheckoutUI(mock.Anything, uint(1), planID, "stripe", uint(1)).
			Return(checkoutResponse, nil).Once()

		// Create authenticated request
		req := ctx.NewAPIRequest("GET", "/api/account/billing/checkout/ui/"+strconv.Itoa(int(planID))+"?period_id=1", nil)
		token := coreTesting.CreateTestLoginToken(ctx.T(), ctx, "1")
		req.Header.Set("Authorization", "Bearer "+token)

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		var response pluginCore.CheckoutUIResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(tb, err)

		assert.Equal(tb, checkoutResponse.SessionID, response.SessionID)
		// JSON numbers are unmarshaled as float64
		assert.Equal(tb, float64(planID), response.Metadata["plan_id"])
		assert.Len(tb, response.Fragments, 1)
		assert.Equal(tb, pluginCore.FragmentTypeLink, response.Fragments[0].Type)
	}, getUserAPITestOptions())
}

func TestHandleGetCheckoutUI_WithCustomGateway(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		planID := uint(99)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock checkout UI with custom gateway
		checkoutResponse := &pluginCore.CheckoutUIResponse{
			SessionID: "sess_custom",
			Fragments: []pluginCore.CheckoutUIFragment{
				{
					Type:   pluginCore.FragmentTypeScript,
					Script: "https://checkout.example.com/sdk.js",
				},
			},
		}

		// Mock GetCheckoutUI on billing service
		ts.billingSvc.EXPECT().GetCheckoutUI(mock.Anything, uint(1), planID, "paypal", uint(1)).
			Return(checkoutResponse, nil).Once()

		// Create request with gateway query parameter (note: parameter name is "gateway", not "gateway_type")
		req := ctx.NewAPIRequest("GET", "/api/account/billing/checkout/ui/99?gateway=paypal&period_id=1", nil)
		token := coreTesting.CreateTestLoginToken(ctx.T(), ctx, "1")
		req.Header.Set("Authorization", "Bearer "+token)

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		var response pluginCore.CheckoutUIResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(tb, err)

		assert.Equal(tb, "sess_custom", response.SessionID)
		assert.Equal(tb, pluginCore.FragmentTypeScript, response.Fragments[0].Type)
	}, getUserAPITestOptions())
}

func TestHandleGetCheckoutUI_Unauthorized(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		planID := uint(42)

		// Create unauthenticated request
		req := ctx.NewAPIRequest("GET", "/api/account/billing/checkout/ui/"+strconv.Itoa(int(planID))+"?period_id=1", nil)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusUnauthorized, w.Code)
	}, getUserAPITestOptions())
}

func TestHandleGetCheckoutUI_InvalidPlanID(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		token := coreTesting.CreateTestLoginToken(ctx.T(), ctx, "1")
		req := ctx.NewAPIRequest("GET", "/api/account/billing/checkout/ui/invalid", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusBadRequest, w.Code)

		requireErrorResponse(tb, w, "InvalidPlanId")
	}, getUserAPITestOptions())
}

func TestHandleGetCheckoutUI_GetCheckoutUIError(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		planID := uint(42)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock GetCheckoutUI to return an error
		ts.billingSvc.EXPECT().GetCheckoutUI(mock.Anything, uint(1), planID, "stripe", uint(1)).
			Return(nil, assert.AnError).Once()

		// Create authenticated request
		req := ctx.NewAPIRequest("GET", "/api/account/billing/checkout/ui/"+strconv.Itoa(int(planID))+"?period_id=1", nil)
		token := coreTesting.CreateTestLoginToken(ctx.T(), ctx, "1")
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusInternalServerError, w.Code)

		var errResponse map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &errResponse)
		require.NoError(tb, err)
	}, getUserAPITestOptions())
}

func TestHandleGetCheckoutUI_GatewayNotFound(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		planID := uint(42)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock GetCheckoutUI to return gateway not found error
		ts.billingSvc.EXPECT().GetCheckoutUI(mock.Anything, uint(1), planID, "nonexistent", uint(1)).
			Return(nil, pluginCore.ErrGatewayNotFound).Once()

		// Create authenticated request - use "gateway" query param, not "gateway_type"
		req := ctx.NewAPIRequest("GET", "/api/account/billing/checkout/ui/"+strconv.Itoa(int(planID))+"?gateway=nonexistent&period_id=1", nil)
		token := coreTesting.CreateTestLoginToken(ctx.T(), ctx, "1")
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - GetGateway errors return 500 Internal Server Error
		assert.Equal(tb, http.StatusInternalServerError, w.Code)

		requireErrorResponse(tb, w, "CheckoutUiGenerationFailed")
	}, getUserAPITestOptions())
}

func TestHandleGetCheckoutUI_UserAlreadySubscribed(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		planID := uint(42)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock GetCheckoutUI to return error about active subscription
		ts.billingSvc.EXPECT().GetCheckoutUI(mock.Anything, uint(1), planID, "stripe", uint(1)).
			Return(nil, fmt.Errorf("user already has an active subscription")).Once()

		// Create authenticated request
		req := ctx.NewAPIRequest("GET", "/api/account/billing/checkout/ui/"+strconv.Itoa(int(planID))+"?period_id=1", nil)
		token := coreTesting.CreateTestLoginToken(ctx.T(), ctx, "1")
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return 409 Conflict due to active subscription
		assert.Equal(tb, http.StatusConflict, w.Code)
	}, getUserAPITestOptions())
}

func TestHandleGetCheckoutUI_RequestBodyParsing(t *testing.T) {
	// This test name is misleading - endpoint uses GET with path params, not POST with body
	// Renaming to match actual implementation
	t.Run("ValidPlanIDInPath", func(t *testing.T) {
		coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
			ts := setupTest(ctx)

			planID := uint(42)

			// Mock user account validation
			ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

			checkoutResponse := &pluginCore.CheckoutUIResponse{
				Fragments: []pluginCore.CheckoutUIFragment{
					{Type: pluginCore.FragmentTypeLink},
				},
			}

			// Mock GetCheckoutUI
			ts.billingSvc.EXPECT().GetCheckoutUI(mock.Anything, uint(1), planID, "stripe", uint(1)).
				Return(checkoutResponse, nil).Once()

			// Create authenticated request
			req := ctx.NewAPIRequest("GET", "/api/account/billing/checkout/ui/"+strconv.Itoa(int(planID))+"?period_id=1", nil)
			token := coreTesting.CreateTestLoginToken(ctx.T(), ctx, "1")
			req.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()

			// Execute
			ts.router.ServeHTTP(w, req)

			// Verify
			assert.Equal(tb, http.StatusOK, w.Code)
		}, getUserAPITestOptions())
	})
}

// Helper function for checkout tests

func createTestExpirationTime() time.Time {
	return time.Now().Add(30 * time.Minute)
}

// Checkout Session Status Tests

func TestHandleGetCheckoutSessionStatus_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		sessionID := "cs_test_123"
		customerEmail := "test@example.com"
		userID := uint(1)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, userID).Return(true, nil, nil)

		// Mock gateway - use composite mock for PaymentGateway + SessionStatusProvider
		mockGateway := pluginCore.NewMockPaymentGateway(t)
		mockSessionProvider := pluginCore.NewMockSessionStatusProvider(t)

		// Create a composite mock that implements both interfaces
		compositeMock := &mockGatewayWithSession{
			MockPaymentGateway:     mockGateway,
			MockSessionStatusProvider: mockSessionProvider,
		}
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "stripe").Return(compositeMock, nil).Once()

		// Mock GetSessionStatus response with matching UserID
		sessionStatus := &pluginCore.SessionStatus{
			SessionID:     sessionID,
			Status:        "complete",
			CustomerEmail: customerEmail,
			UserID:        userID, // Matches authenticated user
		}
		mockSessionProvider.EXPECT().GetSessionStatus(mock.Anything, sessionID).Return(sessionStatus, nil).Once()

		// Create authenticated request
		req := ctx.NewAPIRequest("GET", "/api/account/billing/checkout/session/"+sessionID+"/status", nil)
		token := coreTesting.CreateTestLoginToken(ctx.T(), ctx, "1")
		req.Header.Set("Authorization", "Bearer "+token)

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		var response dto.CheckoutSessionStatusResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(tb, err)

		assert.Equal(tb, sessionID, response.SessionID)
		assert.Equal(tb, "complete", response.Status)
		assert.Equal(tb, customerEmail, response.CustomerEmail)
		assert.Equal(tb, userID, response.UserID)
	}, getUserAPITestOptions())
}

func TestHandleGetCheckoutSessionStatus_GatewayNotFound(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		sessionID := "cs_test_789"

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock gateway not found
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "nonexistent").
			Return(nil, pluginCore.ErrGatewayNotFound).Once()

		// Create authenticated request with non-existent gateway
		req := ctx.NewAPIRequest("GET", "/api/account/billing/checkout/session/"+sessionID+"/status?gateway=nonexistent", nil)
		token := coreTesting.CreateTestLoginToken(ctx.T(), ctx, "1")
		req.Header.Set("Authorization", "Bearer "+token)

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return 404 Not Found
		assert.Equal(tb, http.StatusNotFound, w.Code)
	}, getUserAPITestOptions())
}

func TestHandleGetCheckoutSessionStatus_Unauthorized(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		sessionID := "cs_test_123"

		// Create request without authentication
		req := ctx.NewAPIRequest("GET", "/api/account/billing/checkout/session/"+sessionID+"/status", nil)

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return 401 Unauthorized
		assert.Equal(tb, http.StatusUnauthorized, w.Code)
	}, getUserAPITestOptions())
}

func TestHandleGetCheckoutSessionStatus_OwnershipMismatch(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		sessionID := "cs_test_123"
		authenticatedUserID := uint(1)
		sessionUserID := uint(999) // Different user - simulating IDOR attack
		customerEmail := "other@example.com"

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, authenticatedUserID).Return(true, nil, nil)

		// Mock gateway composite (PaymentGateway + SessionStatusProvider)
		mockGateway := pluginCore.NewMockPaymentGateway(t)
		mockSessionProvider := pluginCore.NewMockSessionStatusProvider(t)
		compositeMock := &mockGatewayWithSession{
			MockPaymentGateway:        mockGateway,
			MockSessionStatusProvider: mockSessionProvider,
		}
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "stripe").Return(compositeMock, nil).Once()

		// Mock GetSessionStatus returning a session with different UserID
		sessionStatus := &pluginCore.SessionStatus{
			SessionID:     sessionID,
			Status:        "complete",
			CustomerEmail: customerEmail,
			UserID:        sessionUserID, // Belongs to a different user!
		}
		mockSessionProvider.EXPECT().GetSessionStatus(mock.Anything, sessionID).Return(sessionStatus, nil).Once()

		// Create authenticated request as user 1
		req := ctx.NewAPIRequest("GET", "/api/account/billing/checkout/session/"+sessionID+"/status", nil)
		token := coreTesting.CreateTestLoginToken(ctx.T(), ctx, "1")
		req.Header.Set("Authorization", "Bearer "+token)

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return 404 Not Found (obscures the fact that session exists but belongs to another user)
		assert.Equal(tb, http.StatusNotFound, w.Code)
	}, getUserAPITestOptions())
}

func TestHandleGetCheckoutSessionStatus_UnverifiableOwnership(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		sessionID := "cs_test_123"
		authenticatedUserID := uint(1)
		customerEmail := "test@example.com"

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, authenticatedUserID).Return(true, nil, nil)

		// Mock gateway composite (PaymentGateway + SessionStatusProvider)
		mockGateway := pluginCore.NewMockPaymentGateway(t)
		mockSessionProvider := pluginCore.NewMockSessionStatusProvider(t)
		compositeMock := &mockGatewayWithSession{
			MockPaymentGateway:        mockGateway,
			MockSessionStatusProvider: mockSessionProvider,
		}
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "stripe").Return(compositeMock, nil).Once()

		// Mock GetSessionStatus returning a session with UserID = 0 (unverifiable)
		sessionStatus := &pluginCore.SessionStatus{
			SessionID:     sessionID,
			Status:        "complete",
			CustomerEmail: customerEmail,
			UserID:        0, // Missing ClientReferenceID - ownership unverifiable!
		}
		mockSessionProvider.EXPECT().GetSessionStatus(mock.Anything, sessionID).Return(sessionStatus, nil).Once()

		// Create authenticated request as user 1
		req := ctx.NewAPIRequest("GET", "/api/account/billing/checkout/session/"+sessionID+"/status", nil)
		token := coreTesting.CreateTestLoginToken(ctx.T(), ctx, "1")
		req.Header.Set("Authorization", "Bearer "+token)

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return 404 Not Found (deny by default when ownership unverifiable)
		assert.Equal(tb, http.StatusNotFound, w.Code)
	}, getUserAPITestOptions())
}

// Management operation tests

func TestHandleCancelOperation_Success_APIBased(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock active subscription
		planID := uint(42)
		mockSubscriber := createMockSubscriber(1, "atlos", "sub_123", true, &planID)
		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(mockSubscriber, nil).Once()

		// Mock gateway
		mockGateway := pluginCore.NewMockPaymentGateway(t)
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "atlos").Return(mockGateway, nil).Once()

		// Mock management capabilities - Atlas supports cancellation (API mode)
		capabilities := &pluginCore.ManagementCapabilities{
			ManagementMode: pluginCore.ModeAPI,
			Operations: map[pluginCore.ManagementOperation]bool{
				pluginCore.OperationCancel:     true,
				pluginCore.OperationChangePlan: false,
			},
		}
		mockGateway.EXPECT().GetManagementInfo(mock.Anything, uint(1)).Return(capabilities, nil).Once()

		// API mode: ExecuteCancel is called directly (no GetManagementURL)
		cancelResult := &pluginCore.CancellationResult{
			Status:      pluginCore.CancellationStatusScheduled,
			EffectiveAt: nil,
			CanAbort:    true,
		}
		mockGateway.EXPECT().ExecuteCancel(mock.Anything, uint(1), false).Return(cancelResult, nil).Once()

		// Create authenticated request
		req, err := ts.createAuthenticatedRequest(ctx, "POST", "/api/account/billing/cancel", nil, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		var response dto.ManagementResultResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(tb, err)

		// After successful execution, action is "show_ui" (success)
		assert.Equal(tb, pluginCore.ActionShowUI, response.Action)

	}, getUserAPITestOptions())
}

func TestHandleCancelOperation_Success_PortalRedirect(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock active subscription
		planID := uint(42)
		mockSubscriber := createMockSubscriber(1, "stripe", "sub_123", true, &planID)
		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(mockSubscriber, nil).Once()

		// Mock gateway
		mockGateway := pluginCore.NewMockPaymentGateway(t)
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "stripe").Return(mockGateway, nil).Once()

		// Mock management capabilities - Stripe supports cancellation
		capabilities := &pluginCore.ManagementCapabilities{
			ManagementMode: pluginCore.ModePortal,
			Operations: map[pluginCore.ManagementOperation]bool{
				pluginCore.OperationCancel:     true,
				pluginCore.OperationChangePlan: true,
			},
		}
		mockGateway.EXPECT().GetManagementInfo(mock.Anything, uint(1)).Return(capabilities, nil).Once()

		// Mock management result - redirect to portal
		managementResult := &pluginCore.ManagementResult{
			Action: pluginCore.ActionRedirect,
			URL:    "https://dashboard.stripe.com/customer/portal/session_123",
		}
		mockGateway.EXPECT().GetManagementURL(mock.Anything, uint(1), pluginCore.OperationCancel).Return(managementResult, nil).Once()

		// Create authenticated request
		req, err := ts.createAuthenticatedRequest(ctx, "POST", "/api/account/billing/cancel", nil, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		var response dto.ManagementResultResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(tb, err)

		assert.Equal(tb, pluginCore.ActionRedirect, response.Action)
		assert.Equal(tb, "https://dashboard.stripe.com/customer/portal/session_123", response.URL)

	}, getUserAPITestOptions())
}

func TestHandleCancelOperation_NotSupported(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock active subscription
		planID := uint(42)
		mockSubscriber := createMockSubscriber(1, "atlos", "sub_123", true, &planID)
		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(mockSubscriber, nil).Once()

		// Mock gateway
		mockGateway := pluginCore.NewMockPaymentGateway(t)
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "atlos").Return(mockGateway, nil).Once()

		// Mock management capabilities - Atlas doesn't support cancellation in this scenario
		capabilities := &pluginCore.ManagementCapabilities{
			ManagementMode: pluginCore.ModeAPI,
			Operations: map[pluginCore.ManagementOperation]bool{
				pluginCore.OperationCancel:     false,
				pluginCore.OperationChangePlan: false,
			},
		}
		mockGateway.EXPECT().GetManagementInfo(mock.Anything, uint(1)).Return(capabilities, nil).Once()

		// Create authenticated request
		req, err := ts.createAuthenticatedRequest(ctx, "POST", "/api/account/billing/cancel", nil, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return 400 Bad Request
		assert.Equal(tb, http.StatusBadRequest, w.Code)

		requireErrorResponse(tb, w, "ManagementOperationFailed")

	}, getUserAPITestOptions())
}

func TestHandleCancelOperation_NoActiveSubscription(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock no active subscription
		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(nil, nil).Once()

		// Create authenticated request
		req, err := ts.createAuthenticatedRequest(ctx, "POST", "/api/account/billing/cancel", nil, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return 404 Not Found
		assert.Equal(tb, http.StatusNotFound, w.Code)

	}, getUserAPITestOptions())
}

func TestHandleCancelOperation_Unauthorized(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Create unauthenticated request
		req := ctx.NewAPIRequest("POST", "/api/account/billing/cancel", nil)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusUnauthorized, w.Code)
	}, getUserAPITestOptions())
}

func TestHandleCancelOperation_GatewayNotSubscriptionManager(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock active subscription
		planID := uint(42)
		mockSubscriber := createMockSubscriber(1, "basic-gateway", "sub_123", true, &planID)
		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(mockSubscriber, nil).Once()

		// Mock gateway that doesn't implement SubscriptionManager (just GatewayIdentity)
		mockGateway := pluginCore.NewMockGatewayIdentity(tb)
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "basic-gateway").Return(mockGateway, nil).Once()

		// Create authenticated request
		req, err := ts.createAuthenticatedRequest(ctx, "POST", "/api/account/billing/cancel", nil, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusInternalServerError, w.Code)

	}, getUserAPITestOptions())
}

func TestHandleAbortCancellationOperation_Success(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock subscriber with scheduled cancellation
		cancelAt := time.Now().Add(24 * time.Hour)
		planID := uint(42)
		mockSubscriber := &pluginCore.Subscriber{
			UserID:              1,
			GatewayType:         "atlos",
			ExternalID:          "ext_123",
			SubscriptionID:      "sub_123",
			IsActive:            true,
			PricingPlanPeriodID: &planID,
			WillCancelAt:        &cancelAt,
		}
		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(mockSubscriber, nil).Once()

		// Mock gateway
		mockGateway := pluginCore.NewMockPaymentGateway(t)
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "atlos").Return(mockGateway, nil).Once()

		// Mock capability check
		mockGateway.EXPECT().GetManagementInfo(mock.Anything, uint(1)).Return(&pluginCore.ManagementCapabilities{
			ManagementMode: pluginCore.ModeAPI,
			Operations: map[pluginCore.ManagementOperation]bool{
				pluginCore.OperationCancel: true,
			},
		}, nil).Once()

		// Mock abort cancellation
		mockGateway.EXPECT().AbortCancellation(mock.Anything, uint(1)).Return(nil).Once()

		// Create authenticated request
		req, err := ts.createAuthenticatedRequest(ctx, "POST", "/api/account/billing/cancel/abort", nil, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		var response dto.ManagementResultResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(tb, err)

		assert.Equal(tb, pluginCore.ActionShowUI, response.Action)
		assert.Equal(tb, "aborted", response.Status)
		assert.False(tb, response.CanAbort)

	}, getUserAPITestOptions())
}

func TestHandleAbortCancellationOperation_NoScheduledCancellation(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock subscriber without scheduled cancellation
		planID := uint(42)
		mockSubscriber := createMockSubscriber(1, "atlos", "sub_123", true, &planID)
		// WillCancelAt is nil by default
		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(mockSubscriber, nil).Once()

		// Create authenticated request
		req, err := ts.createAuthenticatedRequest(ctx, "POST", "/api/account/billing/cancel/abort", nil, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return not found
		assert.Equal(tb, http.StatusNotFound, w.Code)

	}, getUserAPITestOptions())
}

func TestHandleAbortCancellationOperation_NoActiveSubscription(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock no active subscription
		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(nil, nil).Once()

		// Create authenticated request
		req, err := ts.createAuthenticatedRequest(ctx, "POST", "/api/account/billing/cancel/abort", nil, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return not found
		assert.Equal(tb, http.StatusNotFound, w.Code)

	}, getUserAPITestOptions())
}

func TestHandleChangePlanOperation_Success_APIBased(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock active subscription
		planID := uint(42)
		mockSubscriber := createMockSubscriber(1, "atlos", "sub_123", true, &planID)
		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(mockSubscriber, nil).Once()

		// Mock gateway
		mockGateway := pluginCore.NewMockPaymentGateway(t)
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "atlos").Return(mockGateway, nil).Once()

		// Mock management capabilities - Atlas supports plan change (API mode)
		capabilities := &pluginCore.ManagementCapabilities{
			ManagementMode: pluginCore.ModeAPI,
			Operations: map[pluginCore.ManagementOperation]bool{
				pluginCore.OperationCancel:     true,
				pluginCore.OperationChangePlan: true,
			},
		}
		mockGateway.EXPECT().GetManagementInfo(mock.Anything, uint(1)).Return(capabilities, nil).Once()

		// API mode: ExecutePlanChange is called directly (no GetManagementURL)
		newPeriodID := uint(99)
		planChangeResult := &pluginCore.PlanChangeResult{
			Action:       pluginCore.PlanChangeActionCheckoutRequired,
			CheckoutLink: "123-period99",
		}
		mockGateway.EXPECT().ExecutePlanChange(mock.Anything, uint(1), newPeriodID).Return(planChangeResult, nil).Once()

		// Create authenticated request with period_id
		requestBody := dto.ChangePlanRequest{PeriodID: newPeriodID}
		bodyBytes, err := json.Marshal(requestBody)
		require.NoError(tb, err)
		req, err := ts.createAuthenticatedRequest(ctx, "POST", "/api/account/billing/change-plan", bodyBytes, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		var response dto.PlanChangeResultResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(tb, err)

		assert.Equal(tb, string(pluginCore.PlanChangeActionCheckoutRequired), response.Action)
		assert.Equal(tb, "123-period99", response.CheckoutLink)

	}, getUserAPITestOptions())
}

func TestHandleChangePlanOperation_Success_PortalRedirect(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock active subscription
		planID := uint(42)
		mockSubscriber := createMockSubscriber(1, "stripe", "sub_123", true, &planID)
		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(mockSubscriber, nil).Once()

		// Mock gateway
		mockGateway := pluginCore.NewMockPaymentGateway(t)
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "stripe").Return(mockGateway, nil).Once()

		// Mock management capabilities - Stripe supports plan change
		capabilities := &pluginCore.ManagementCapabilities{
			ManagementMode: pluginCore.ModePortal,
			Operations: map[pluginCore.ManagementOperation]bool{
				pluginCore.OperationCancel:     true,
				pluginCore.OperationChangePlan: true,
			},
		}
		mockGateway.EXPECT().GetManagementInfo(mock.Anything, uint(1)).Return(capabilities, nil).Once()

		// Mock management result - redirect to portal
		managementResult := &pluginCore.ManagementResult{
			Action: pluginCore.ActionRedirect,
			URL:    "https://dashboard.stripe.com/customer/portal/session_456",
		}
		mockGateway.EXPECT().GetManagementURL(mock.Anything, uint(1), pluginCore.OperationChangePlan).Return(managementResult, nil).Once()

		// Create authenticated request with request body
		requestBody := dto.ChangePlanRequest{PeriodID: uint(99)}
		bodyBytes, _ := json.Marshal(requestBody)
		req, err := ts.createAuthenticatedRequest(ctx, "POST", "/api/account/billing/change-plan", bodyBytes, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusOK, w.Code)

		var response dto.ManagementResultResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(tb, err)

		assert.Equal(tb, pluginCore.ActionRedirect, response.Action)
		assert.Equal(tb, "https://dashboard.stripe.com/customer/portal/session_456", response.URL)

	}, getUserAPITestOptions())
}

func TestHandleChangePlanOperation_NotSupported(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock active subscription
		planID := uint(42)
		mockSubscriber := createMockSubscriber(1, "atlos", "sub_123", true, &planID)
		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(mockSubscriber, nil).Once()

		// Mock gateway
		mockGateway := pluginCore.NewMockPaymentGateway(t)
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "atlos").Return(mockGateway, nil).Once()

		// Mock management capabilities - Atlas doesn't support plan change in this scenario
		capabilities := &pluginCore.ManagementCapabilities{
			ManagementMode: pluginCore.ModeAPI,
			Operations: map[pluginCore.ManagementOperation]bool{
				pluginCore.OperationCancel:     true,
				pluginCore.OperationChangePlan: false,
			},
		}
		mockGateway.EXPECT().GetManagementInfo(mock.Anything, uint(1)).Return(capabilities, nil).Once()

		// Create authenticated request with request body
		requestBody := dto.ChangePlanRequest{PeriodID: uint(99)}
		bodyBytes, _ := json.Marshal(requestBody)
		req, err := ts.createAuthenticatedRequest(ctx, "POST", "/api/account/billing/change-plan", bodyBytes, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return 400 Bad Request
		assert.Equal(tb, http.StatusBadRequest, w.Code)

		requireErrorResponse(tb, w, "ManagementOperationFailed")

	}, getUserAPITestOptions())
}

func TestHandleChangePlanOperation_NoActiveSubscription(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock no active subscription
		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(nil, nil).Once()

		// Create authenticated request with request body
		requestBody := dto.ChangePlanRequest{PeriodID: uint(99)}
		bodyBytes, _ := json.Marshal(requestBody)
		req, err := ts.createAuthenticatedRequest(ctx, "POST", "/api/account/billing/change-plan", bodyBytes, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify - should return 404 Not Found
		assert.Equal(tb, http.StatusNotFound, w.Code)

	}, getUserAPITestOptions())
}

func TestHandleChangePlanOperation_Unauthorized(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Create unauthenticated request
		req := ctx.NewAPIRequest("POST", "/api/account/billing/change-plan", nil)
		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusUnauthorized, w.Code)

	}, getUserAPITestOptions())
}

func TestHandleChangePlanOperation_GatewayNotSubscriptionManager(t *testing.T) {
	coreTesting.RunTestCase(t, func(tb coreTesting.TB, ctx coreTesting.TestContext) {
		ts := setupTest(ctx)

		// Mock user account validation
		ts.userSvc.EXPECT().AccountExists(mock.Anything, uint(1)).Return(true, nil, nil)

		// Mock active subscription
		planID := uint(42)
		mockSubscriber := createMockSubscriber(1, "basic-gateway", "sub_123", true, &planID)
		ts.billingSvc.EXPECT().GetActiveSubscription(mock.Anything, uint(1)).Return(mockSubscriber, nil).Once()

		// Mock gateway that doesn't implement SubscriptionManager (just GatewayIdentity)
		mockGateway := pluginCore.NewMockGatewayIdentity(tb)
		ts.billingSvc.EXPECT().GetGateway(mock.Anything, "basic-gateway").Return(mockGateway, nil).Once()

		// Create authenticated request with request body
		requestBody := dto.ChangePlanRequest{PeriodID: uint(99)}
		bodyBytes, _ := json.Marshal(requestBody)
		req, err := ts.createAuthenticatedRequest(ctx, "POST", "/api/account/billing/change-plan", bodyBytes, "1")
		assert.NoError(tb, err, "Failed to create authenticated request")

		w := httptest.NewRecorder()

		// Execute
		ts.router.ServeHTTP(w, req)

		// Verify
		assert.Equal(tb, http.StatusInternalServerError, w.Code)

	}, getUserAPITestOptions())
}

// --- Regression: x402Limiter is goroutine-safe (no duplicate limiters per IP) ---

func TestRegression_X402Limiter_ConcurrentAllowNoRace(t *testing.T) {
	limiter := newX402Limiter(100, 1) // 100 rps, burst 1

	// Fire 50 concurrent requests from the same IP.
	// With the mutex fix, only one limiter is created and the burst is
	// respected. Without the fix, multiple limiters with full burst
	// could be created, allowing more than 1 immediate Allow().
	var allowed int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if limiter.allow("10.0.0.1") {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	// With burst=1, at most 1 should be allowed immediately.
	// Without the mutex, multiple limiters could allow more.
	assert.Equal(t, 1, allowed, "burst=1 should only allow 1 concurrent request, got %d", allowed)
}

func TestRegression_X402Limiter_DoesNotPanicUnderConcurrentLoad(t *testing.T) {
	limiter := newX402Limiter(1000, 10)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			ip := fmt.Sprintf("10.0.0.%d", n%5) // 5 distinct IPs
			for j := 0; j < 10; j++ {
				limiter.allow(ip)
			}
		}(i)
	}
	wg.Wait()
	// Test passes if no panic or race detected.
}
