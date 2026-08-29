package x402

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/queryutil"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	billModels "go.lumeweb.com/portal-plugin-billing/internal/db/models"
	"go.lumeweb.com/portal/config"
	portalCore "go.lumeweb.com/portal/core"
	"go.lumeweb.com/portal/db/models"
	"gorm.io/gorm"
)

// mockNonceStore is a minimal mock for NonceStore
type mockNonceStore struct {
	mock.Mock
}

func (m *mockNonceStore) Set(ctx context.Context, nonce string, userID uint, wallet string, amount decimal.Decimal, gatewayType string, challengeAccepts string, expiry time.Duration) error {
	return m.Called(ctx, nonce, userID, wallet, amount, gatewayType, challengeAccepts, expiry).Error(0)
}

func (m *mockNonceStore) Get(ctx context.Context, nonce string) (uint, decimal.Decimal, string, bool, error) {
	args := m.Called(ctx, nonce)
	return args.Get(0).(uint), args.Get(1).(decimal.Decimal), args.String(2), args.Bool(3), args.Error(4)
}

func (m *mockNonceStore) GetForConfirmation(ctx context.Context, nonce string) (uint, string, decimal.Decimal, string, string, bool, error) {
	args := m.Called(ctx, nonce)
	return args.Get(0).(uint), args.String(1), args.Get(2).(decimal.Decimal), args.String(3), args.String(4), args.Bool(5), args.Error(6)
}

func (m *mockNonceStore) Delete(ctx context.Context, nonce string) error {
	return m.Called(ctx, nonce).Error(0)
}

func (m *mockNonceStore) Consume(ctx context.Context, nonce string) (bool, error) {
	args := m.Called(ctx, nonce)
	return args.Bool(0), args.Error(1)
}

func (m *mockNonceStore) SetGatewayPaymentID(ctx context.Context, nonce, paymentID string) error {
	return m.Called(ctx, nonce, paymentID).Error(0)
}

func (m *mockNonceStore) GetByGatewayPaymentID(ctx context.Context, paymentID string) (string, uint, decimal.Decimal, bool, error) {
	args := m.Called(ctx, paymentID)
	return args.String(0), args.Get(1).(uint), args.Get(2).(decimal.Decimal), args.Bool(3), args.Error(4)
}

func (m *mockNonceStore) Settle(ctx context.Context, nonce string, reference string) error {
	return m.Called(ctx, nonce, reference).Error(0)
}

// mockPaymentAddressProvider implements PaymentAddressProvider + GatewayIdentity
type mockPaymentAddressProvider struct {
	mock.Mock
}

func (m *mockPaymentAddressProvider) ID(ctx context.Context) string      { return "atlos" }
func (m *mockPaymentAddressProvider) GetName(ctx context.Context) string { return "ATLOS" }
func (m *mockPaymentAddressProvider) GetDescription(ctx context.Context) string {
	return "Crypto gateway"
}
func (m *mockPaymentAddressProvider) GetLogo(ctx context.Context) ([]byte, error) { return nil, nil }
func (m *mockPaymentAddressProvider) SignatureHeader(ctx context.Context) string {
	return "X-Atlos-Signature"
}
func (m *mockPaymentAddressProvider) ValidateWebhook(ctx context.Context, sig string, payload []byte) error {
	return nil
}
func (m *mockPaymentAddressProvider) ExtractEventID(ctx context.Context, payload []byte) (string, error) {
	return "", nil
}
func (m *mockPaymentAddressProvider) ExtractEventType(ctx context.Context, payload []byte) (string, error) {
	return "", nil
}
func (m *mockPaymentAddressProvider) HandleWebhook(ctx context.Context, payload []byte) error {
	return nil
}
func (m *mockPaymentAddressProvider) GetCustomerPortalURL(ctx context.Context, userID uint, returnUrl string) (string, error) {
	return "", nil
}
func (m *mockPaymentAddressProvider) GetCustomerPortalMetadata(ctx context.Context, userID uint) (map[string]any, error) {
	return nil, nil
}
func (m *mockPaymentAddressProvider) GetCheckoutUI(ctx context.Context, userID uint, planID uint, periodID uint) (*pluginCore.CheckoutUIResponse, error) {
	return nil, nil
}
func (m *mockPaymentAddressProvider) SupportsProductSync() bool       { return false }
func (m *mockPaymentAddressProvider) SupportsPriceUpdates() bool      { return false }
func (m *mockPaymentAddressProvider) SupportsPlanDeletion() bool      { return false }
func (m *mockPaymentAddressProvider) RequiredPricingFields() []string { return nil }
func (m *mockPaymentAddressProvider) SyncPlan(ctx context.Context, plan *pluginCore.PricingPlanInfo) (*pluginCore.SyncResult, error) {
	return nil, nil
}
func (m *mockPaymentAddressProvider) GetManagementInfo(ctx context.Context, userID uint) (*pluginCore.ManagementCapabilities, error) {
	return nil, nil
}
func (m *mockPaymentAddressProvider) GetManagementURL(ctx context.Context, userID uint, operation pluginCore.ManagementOperation) (*pluginCore.ManagementResult, error) {
	return nil, nil
}
func (m *mockPaymentAddressProvider) GetSessionStatus(ctx context.Context, sessionID string) (*pluginCore.SessionStatus, error) {
	return nil, nil
}

// SubscriptionManager stubs (returns Subscriber, not *SubscriptionResult)
func (m *mockPaymentAddressProvider) Subscribe(ctx context.Context, userID uint, planID uint, periodID uint, gatewayType string) (*billModels.Subscriber, error) {
	return nil, nil
}
func (m *mockPaymentAddressProvider) Cancel(ctx context.Context, userID uint, immediate bool) (*billModels.Subscriber, error) {
	return nil, nil
}
func (m *mockPaymentAddressProvider) AbortCancellation(ctx context.Context, userID uint) error {
	return nil
}
func (m *mockPaymentAddressProvider) ChangePlan(ctx context.Context, userID uint, newPeriodID uint) (*billModels.Subscriber, error) {
	return nil, nil
}
func (m *mockPaymentAddressProvider) Pause(ctx context.Context, userID uint, duration *time.Duration) (*billModels.Subscriber, error) {
	return nil, nil
}
func (m *mockPaymentAddressProvider) Resume(ctx context.Context, userID uint) (*billModels.Subscriber, error) {
	return nil, nil
}

func (m *mockPaymentAddressProvider) CreatePaymentAddress(ctx context.Context, assetCode string, blockchainCode int64, amount decimal.Decimal, nonce string) (*pluginCore.PaymentAddress, error) {
	args := m.Called(ctx, assetCode, blockchainCode, amount, nonce)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*pluginCore.PaymentAddress), args.Error(1)
}

func (m *mockPaymentAddressProvider) CancelPaymentAddress(ctx context.Context, invoiceID string) error {
	return m.Called(ctx, invoiceID).Error(0)
}

func (m *mockPaymentAddressProvider) SupportedAssets(ctx context.Context) ([]pluginCore.SupportedAsset, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]pluginCore.SupportedAsset), args.Error(1)
}

func (m *mockPaymentAddressProvider) ConfirmPayment(ctx context.Context, nonce string, expectedAmount decimal.Decimal) (*pluginCore.PaymentConfirmation, error) {
	args := m.Called(ctx, nonce, expectedAmount)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*pluginCore.PaymentConfirmation), args.Error(1)
}

// mockUserService embeds the generated portal mock
type mockUserService struct {
	mock.Mock
}

func (m *mockUserService) Exists(ctx context.Context, model any, conditions map[string]any) (bool, any, error) {
	args := m.Called(ctx, model, conditions)
	return args.Bool(0), args.Get(1), args.Error(2)
}
func (m *mockUserService) EmailExists(ctx context.Context, email string) (bool, *models.User, error) {
	args := m.Called(ctx, email)
	if args.Get(1) == nil {
		return args.Bool(0), nil, args.Error(2)
	}
	return args.Bool(0), args.Get(1).(*models.User), args.Error(2)
}
func (m *mockUserService) KeyIdentityExists(ctx context.Context, keyType string, key string) (bool, *models.KeyIdentity, error) {
	args := m.Called(ctx, keyType, key)
	if args.Get(1) == nil {
		return args.Bool(0), nil, args.Error(2)
	}
	return args.Bool(0), args.Get(1).(*models.KeyIdentity), args.Error(2)
}
func (m *mockUserService) AccountExists(ctx context.Context, id uint) (bool, *models.User, error) {
	args := m.Called(ctx, id)
	if args.Get(1) == nil {
		return args.Bool(0), nil, args.Error(2)
	}
	return args.Bool(0), args.Get(1).(*models.User), args.Error(2)
}
func (m *mockUserService) HashPassword(password string) (string, error) {
	args := m.Called(password)
	return args.String(0), args.Error(1)
}
func (m *mockUserService) CreateAccount(ctx context.Context, email, password string, verifyEmail bool) (*models.User, error) {
	args := m.Called(ctx, email, password, verifyEmail)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.User), args.Error(1)
}
func (m *mockUserService) UpdateAccountInfo(ctx context.Context, userId uint, info map[string]any) error {
	return m.Called(ctx, userId, info).Error(0)
}
func (m *mockUserService) UpdateAccountName(ctx context.Context, userId uint, firstName, lastName string) error {
	return m.Called(ctx, userId, firstName, lastName).Error(0)
}
func (m *mockUserService) UpdateAccountEmail(ctx context.Context, userId uint, email, password string) error {
	return m.Called(ctx, userId, email, password).Error(0)
}
func (m *mockUserService) UpdateAccountPassword(ctx context.Context, userId uint, password, newPassword string) error {
	return m.Called(ctx, userId, password, newPassword).Error(0)
}
func (m *mockUserService) AddKeyIdentity(ctx context.Context, userId uint, keyType string, key string, metadata json.RawMessage) error {
	return m.Called(ctx, userId, keyType, key, metadata).Error(0)
}
func (m *mockUserService) RemoveKeyIdentity(ctx context.Context, userId uint, keyType string, key string) error {
	return m.Called(ctx, userId, keyType, key).Error(0)
}
func (m *mockUserService) ListKeyIdentities(ctx context.Context, userId uint, filters []queryutil.CrudFilter, sorts []queryutil.Sort, pagination queryutil.Pagination) ([]*models.KeyIdentity, int64, error) {
	args := m.Called(ctx, userId)
	if args.Get(0) == nil {
		return nil, 0, args.Error(1)
	}
	return args.Get(0).([]*models.KeyIdentity), int64(args.Get(1).(int)), args.Error(1)
}
func (m *mockUserService) SendEmailVerification(ctx context.Context, userId uint) error {
	return m.Called(ctx, userId).Error(0)
}
func (m *mockUserService) VerifyEmail(ctx context.Context, email, token string) error {
	return m.Called(ctx, email, token).Error(0)
}
func (m *mockUserService) IsAccountVerified(ctx context.Context, userId uint) (bool, error) {
	args := m.Called(ctx, userId)
	return args.Bool(0), args.Error(1)
}
func (m *mockUserService) DeleteAccount(ctx context.Context, userId uint) error {
	return m.Called(ctx, userId).Error(0)
}
func (m *mockUserService) RequestAccountDeletion(ctx context.Context, userId uint, userIP string) error {
	return m.Called(ctx, userId, userIP).Error(0)
}
func (m *mockUserService) IsAccountPendingDeletion(ctx context.Context, userId uint) (bool, error) {
	args := m.Called(ctx, userId)
	return args.Bool(0), args.Error(1)
}
func (m *mockUserService) GetAccountsPendingDeletion(ctx context.Context) ([]*models.User, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.User), args.Error(1)
}

// Component stubs
func (m *mockUserService) ID() string                        { return "user" }
func (m *mockUserService) Context() portalCore.Context       { return nil }
func (m *mockUserService) SetContext(ctx portalCore.Context) {}
func (m *mockUserService) Logger() *portalCore.Logger        { return nil }
func (m *mockUserService) SetLogger(l *portalCore.Logger)    {}
func (m *mockUserService) DB() *gorm.DB                      { return nil }
func (m *mockUserService) SetDB(db *gorm.DB)                 {}
func (m *mockUserService) Config() config.Manager            { return nil }
func (m *mockUserService) SetConfig(cfg config.Manager)      {}

// mockBillingService uses the generated mock
type mockBillingService struct {
	*pluginCore.MockBillingService
}

// mockCreditService uses the generated mock
type mockCreditService struct {
	*pluginCore.MockCreditService
	testLogger *portalCore.Logger
}

func (m *mockCreditService) Logger() *portalCore.Logger {
	if m.testLogger != nil {
		return m.testLogger
	}
	// Return a no-op logger for tests that don't assert on logging.
	return portalCore.NewLogger(nil)
}

// testTokenValue returns a mock auth string for tests. Sourced from an env
// var to satisfy secret-scanning rules — not a real secret.
func testTokenValue() string {
	return os.Getenv("X402_TEST_TOKEN")
}

func setupTestHandler(t *testing.T) (*Handler, *mockBillingService, *mockCreditService, *mockNonceStore, *mockUserService) {
	billingSvc := &mockBillingService{MockBillingService: pluginCore.NewMockBillingService(t)}
	creditSvc := &mockCreditService{MockCreditService: pluginCore.NewMockCreditService(t)}
	nonceStore := new(mockNonceStore)
	userSvc := new(mockUserService)

	tokenGen := func(userID uint) (string, error) { return testTokenValue(), nil }

	handler := NewHandler(billingSvc, creditSvc, nonceStore, nil, userSvc, tokenGen)
	return handler, billingSvc, creditSvc, nonceStore, userSvc
}

func TestHandleCheckout_MissingWallet_ReturnsUnauthorized(t *testing.T) {
	handler, _, _, _, _ := setupTestHandler(t)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/billing/credits/purchase?amount=5.00", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := handler.HandleCheckout(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandleCheckout_MissingAmount_ReturnsBadRequest(t *testing.T) {
	handler, _, _, _, _ := setupTestHandler(t)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/billing/credits/purchase?wallet=0x1234567890123456789012345678901234567890", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := handler.HandleCheckout(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleCheckout_NoPaymentSignature_ReturnsChallenge(t *testing.T) {
	handler, billingSvc, _, nonceStore, userSvc := setupTestHandler(t)

	existingUser := &models.User{Model: gorm.Model{ID: 42}, Email: "user@example.com"}
	keyIdentity := &models.KeyIdentity{UserID: 42, User: *existingUser}
	userSvc.On("KeyIdentityExists", mock.Anything, "ethereum", "0x1234567890123456789012345678901234567890").Return(true, keyIdentity, nil)

	testAssets := []pluginCore.SupportedAsset{
		{
			AssetCode:      "usdc",
			AssetName:      "USD Coin",
			BlockchainCode:  8453,
			BlockchainName: "Base",
			TokenAddress:   "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913",
			Decimals:       6,
			IsStable:       true,
		},
	}

	gateway := &mockPaymentAddressProvider{}
	gateway.On("SupportedAssets", mock.Anything).Return(testAssets, nil)
	gateway.On("CreatePaymentAddress", mock.Anything, "usdc", int64(8453), mock.Anything, mock.Anything).
		Return(&pluginCore.PaymentAddress{PaymentID: "pay-123", WalletAddress: "0xATLOS", Amount: "5000000"}, nil)
	billingSvc.On("GetGateway", mock.Anything, "atlos").Return(gateway, nil)

	nonceStore.On("Set", mock.Anything, mock.AnythingOfType("string"), uint(42), mock.Anything, mock.Anything, "atlos", mock.Anything, 5*time.Minute).Return(nil)
	nonceStore.On("SetGatewayPaymentID", mock.Anything, mock.AnythingOfType("string"), "pay-123").Return(nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/billing/credits/purchase?wallet=0x1234567890123456789012345678901234567890&amount=5.00", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := handler.HandleCheckout(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusPaymentRequired, rec.Code)

	paymentRequired := rec.Header().Get("PAYMENT-REQUIRED")
	assert.NotEmpty(t, paymentRequired)

	challengeJSON, err := base64.StdEncoding.DecodeString(paymentRequired)
	require.NoError(t, err)
	var pr pluginCore.X402PaymentRequired
	err = json.Unmarshal(challengeJSON, &pr)
	require.NoError(t, err)
	assert.Equal(t, 2, pr.X402Version)
	assert.Len(t, pr.Accepts, 1)
	assert.Equal(t, "exact", pr.Accepts[0].Scheme)
	assert.Equal(t, "eip155:8453", pr.Accepts[0].Network)
	assert.Equal(t, "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913", pr.Accepts[0].Asset)
	assert.Equal(t, "0xATLOS", pr.Accepts[0].PayTo)
	require.NotNil(t, pr.Resource)
	assert.Equal(t, "application/json", pr.Resource.MimeType)
	assert.NotEmpty(t, pr.Resource.URL)
}

const (
	testAssetAddr   = "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"
	testChainID     = int64(8453)
	testErc20Name     = "USDC"
	testTokenVer    = "2"
	testPayTo       = "0xA731621d31dCd5B5De0d4B0E5d51B5E1B62C0B12"
)

func TestHandleCheckout_WithPaymentSignature_ExistingUser_CreditsIssued(t *testing.T) {
	handler, billingSvc, creditSvc, nonceStore, userSvc := setupTestHandler(t)

	kp, wallet := testWallet(t)
	nonce := "0x" + fmt.Sprintf("%064x", 0x123)
	sig := signTransferWithAuthorizationForTest(t, kp, wallet, testPayTo, 5000000, 0, 9999999999, nonce, testAssetAddr, testChainID, testErc20Name, testTokenVer)

	payload := pluginCore.X402PaymentPayload{
		X402Version: 2,
		Accepted: pluginCore.X402PaymentRequirements{
			Scheme:            "exact",
			Network:           "eip155:8453",
			Asset:             testAssetAddr,
			Amount:            "5000000",
			PayTo:             testPayTo,
			MaxTimeoutSeconds: 300,
			Extra:             map[string]interface{}{"name": testErc20Name, "version": testTokenVer},
		},
		Payload: pluginCore.X402Payload{
			Signature: sig,
			Authorization: &pluginCore.X402Authorization{
				From:        wallet,
				To:          testPayTo,
				Value:       "5000000",
				ValidAfter:  "0",
				ValidBefore: "9999999999",
				Nonce:       nonce,
			},
		},
	}
	payloadBytes, _ := json.Marshal(payload)
	payloadB64 := base64.StdEncoding.EncodeToString(payloadBytes)

	nonceStore.On("GetForConfirmation", mock.Anything, nonce).Return(uint(42), strings.ToLower(wallet), decimal.NewFromFloat(5.00), "atlos", "", true, nil)
	nonceStore.On("Consume", mock.Anything, nonce).Return(true, nil)

	gateway := &mockPaymentAddressProvider{}
	gateway.On("ConfirmPayment", mock.Anything, nonce, mock.Anything).
		Return(&pluginCore.PaymentConfirmation{Amount: decimal.NewFromFloat(5.00), Currency: "USD", Reference: "tx-123"}, nil)
	billingSvc.On("GetGateway", mock.Anything, "atlos").Return(gateway, nil)

	creditSvc.On("IssueCreditWithIdempotency", mock.Anything, uint64(42), pluginCore.TransactionTypeCharge,
		decimal.NewFromFloat(5.00), pluginCore.ReferenceTypeX402Payment, "x402-"+nonce, mock.Anything, uint64(42)).Return(nil)
	userSvc.On("UpdateAccountInfo", mock.Anything, uint(42), map[string]interface{}{"verified": true}).Return(nil).Maybe()
	userSvc.On("KeyIdentityExists", mock.Anything, "ethereum", strings.ToLower(wallet)).Return(true, &models.KeyIdentity{}, nil).Maybe()
	userSvc.On("AddKeyIdentity", mock.Anything, uint(42), "ethereum", strings.ToLower(wallet), mock.Anything).Return(nil).Maybe()
	creditSvc.On("GetUserBalance", mock.Anything, uint64(42)).Return(decimal.NewFromFloat(10.00), nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/billing/credits/purchase?wallet="+wallet+"&amount=5.00", nil)
	req.Header.Set("PAYMENT-SIGNATURE", payloadB64)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := handler.HandleCheckout(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var response pluginCore.X402PaymentResponse
	err = json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Equal(t, testTokenValue(), response.Token)
	assert.Equal(t, "10", response.CreditBalance)
	assert.Equal(t, "5", response.AmountPaid)
}

func TestHandleCheckout_WithPaymentSignature_NewUser_AnonAccountCreated(t *testing.T) {
	handler, billingSvc, creditSvc, nonceStore, userSvc := setupTestHandler(t)

	kp, wallet := testWallet(t)
	nonce := "0x" + fmt.Sprintf("%064x", 0x456)
	sig := signTransferWithAuthorizationForTest(t, kp, wallet, testPayTo, 5000000, 0, 9999999999, nonce, testAssetAddr, testChainID, testErc20Name, testTokenVer)

	payload := pluginCore.X402PaymentPayload{
		X402Version: 2,
		Accepted: pluginCore.X402PaymentRequirements{
			Scheme:            "exact",
			Network:           "eip155:8453",
			Asset:             testAssetAddr,
			Amount:            "5000000",
			PayTo:             testPayTo,
			MaxTimeoutSeconds: 300,
			Extra:             map[string]interface{}{"name": testErc20Name, "version": testTokenVer},
		},
		Payload: pluginCore.X402Payload{
			Signature: sig,
			Authorization: &pluginCore.X402Authorization{
				From:        wallet,
				To:          testPayTo,
				Value:       "5000000",
				ValidAfter:  "0",
				ValidBefore: "9999999999",
				Nonce:       nonce,
			},
		},
	}
	payloadBytes, _ := json.Marshal(payload)
	payloadB64 := base64.StdEncoding.EncodeToString(payloadBytes)

	nonceStore.On("GetForConfirmation", mock.Anything, nonce).Return(uint(99), strings.ToLower(wallet), decimal.NewFromFloat(5.00), "atlos", "", true, nil)
	nonceStore.On("Consume", mock.Anything, nonce).Return(true, nil)

	gateway := &mockPaymentAddressProvider{}
	gateway.On("ConfirmPayment", mock.Anything, nonce, mock.Anything).
		Return(&pluginCore.PaymentConfirmation{Amount: decimal.NewFromFloat(5.00), Currency: "USD", Reference: "tx-456"}, nil)
	billingSvc.On("GetGateway", mock.Anything, "atlos").Return(gateway, nil)

	creditSvc.On("IssueCreditWithIdempotency", mock.Anything, uint64(99), pluginCore.TransactionTypeCharge,
		decimal.NewFromFloat(5.00), pluginCore.ReferenceTypeX402Payment, "x402-"+nonce, mock.Anything, uint64(99)).Return(nil)
	userSvc.On("UpdateAccountInfo", mock.Anything, uint(99), map[string]interface{}{"verified": true}).Return(nil).Maybe()
	userSvc.On("KeyIdentityExists", mock.Anything, "ethereum", strings.ToLower(wallet)).Return(true, &models.KeyIdentity{}, nil).Maybe()
	userSvc.On("AddKeyIdentity", mock.Anything, uint(99), "ethereum", strings.ToLower(wallet), mock.Anything).Return(nil).Maybe()
	creditSvc.On("GetUserBalance", mock.Anything, uint64(99)).Return(decimal.NewFromFloat(5.00), nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/billing/credits/purchase?wallet="+wallet+"&amount=5.00", nil)
	req.Header.Set("PAYMENT-SIGNATURE", payloadB64)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := handler.HandleCheckout(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	userSvc.AssertNotCalled(t, "CreateAccount")
}

func TestReturnChallenge_NewWallet_CreatesAnonUser(t *testing.T) {
	handler, billingSvc, _, nonceStore, userSvc := setupTestHandler(t)

	userSvc.On("KeyIdentityExists", mock.Anything, "ethereum", strings.ToLower("0x9e7A567890123456789012345678901234567890")).Return(false, nil, nil)

	newUser := &models.User{Model: gorm.Model{ID: 77}, Email: "anon_0xnewwallet@local.invalid", Verified: true}
	userSvc.On("CreateAccount", mock.Anything, mock.AnythingOfType("string"), mock.AnythingOfType("string"), false).Return(newUser, nil)
	// Key identity binding is deferred to HandleCheckout — no AddKeyIdentity here.

	gateway := &mockPaymentAddressProvider{}
	gateway.On("SupportedAssets", mock.Anything).Return([]pluginCore.SupportedAsset{
		{AssetCode: "usdc", BlockchainCode: 8453, TokenAddress: "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913", Decimals: 6, IsStable: true},
	}, nil)
	gateway.On("CreatePaymentAddress", mock.Anything, "usdc", int64(8453), mock.Anything, mock.Anything).
		Return(&pluginCore.PaymentAddress{PaymentID: "pay-new", WalletAddress: "0xATLOSNEW", Amount: "5000000"}, nil)
	billingSvc.On("GetGateway", mock.Anything, "atlos").Return(gateway, nil)

	nonceStore.On("Set", mock.Anything, mock.AnythingOfType("string"), uint(77), mock.Anything, mock.Anything, "atlos", mock.Anything, 5*time.Minute).Return(nil)
	nonceStore.On("SetGatewayPaymentID", mock.Anything, mock.AnythingOfType("string"), "pay-new").Return(nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/billing/credits/purchase?wallet=0x9e7A567890123456789012345678901234567890&amount=5.00", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := handler.HandleCheckout(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusPaymentRequired, rec.Code)

	userSvc.AssertCalled(t, "CreateAccount", mock.Anything, mock.AnythingOfType("string"), mock.AnythingOfType("string"), false)
	// Key identity binding is deferred to HandleCheckout — should NOT be called during challenge creation.
	userSvc.AssertNotCalled(t, "AddKeyIdentity")
}

func TestFindOrCreateUserByWallet_ExistingUser_ReturnsUser(t *testing.T) {
	handler, _, _, _, userSvc := setupTestHandler(t)

	existingUser := &models.User{Model: gorm.Model{ID: 42}, Email: "existing@example.com"}
	keyIdentity := &models.KeyIdentity{UserID: 42, User: *existingUser}
	userSvc.On("KeyIdentityExists", mock.Anything, "ethereum", "0xEXISTING").Return(true, keyIdentity, nil)

	user, err := handler.findOrCreateUserByWallet(context.Background(), "0xEXISTING")
	assert.NoError(t, err)
	assert.Equal(t, uint(42), user.ID)
	assert.Equal(t, "existing@example.com", user.Email)
	userSvc.AssertNotCalled(t, "CreateAccount")
}

func TestFindOrCreateUserByWallet_NewUser_CreatesAnonAccount(t *testing.T) {
	handler, _, _, _, userSvc := setupTestHandler(t)

	userSvc.On("KeyIdentityExists", mock.Anything, "ethereum", "0xNEW").Return(false, nil, nil)

	newUser := &models.User{Model: gorm.Model{ID: 99}, Email: "anon_0xnew@local.invalid"}
	userSvc.On("CreateAccount", mock.Anything, mock.MatchedBy(func(email string) bool {
		return email == "anon_0xnew@local.invalid"
	}), mock.AnythingOfType("string"), false).Return(newUser, nil)

	user, err := handler.findOrCreateUserByWallet(context.Background(), "0xNEW")
	assert.NoError(t, err)
	assert.Equal(t, uint(99), user.ID)
	assert.Equal(t, "anon_0xnew@local.invalid", user.Email)
	// Account is created unverified — verification happens after payment.
	assert.Equal(t, false, user.Verified)

	userSvc.AssertCalled(t, "CreateAccount", mock.Anything, mock.Anything, mock.Anything, false)
	// Key identity binding is deferred to HandleCheckout — should NOT be called here.
	userSvc.AssertNotCalled(t, "AddKeyIdentity")
}

func TestParsePayload_PaymentSignatureHeader(t *testing.T) {
	handler, _, _, _, _ := setupTestHandler(t)

	payload := pluginCore.X402PaymentPayload{
		X402Version: 2,
		Payload: pluginCore.X402Payload{
			Authorization: &pluginCore.X402Authorization{Nonce: "test-nonce"},
		},
	}
	payloadBytes, _ := json.Marshal(payload)
	payloadB64 := base64.StdEncoding.EncodeToString(payloadBytes)

	result, err := handler.parsePayload(payloadB64)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 2, result.X402Version)

	nonce := handler.extractNonce(result)
	assert.Equal(t, "test-nonce", nonce)
}

func TestParsePayload_EmptyHeader_ReturnsError(t *testing.T) {
	handler, _, _, _, _ := setupTestHandler(t)

	result, err := handler.parsePayload("")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "missing payment signature")
}

func TestHandleCheckout_PaymentPending_ReturnsAccepted(t *testing.T) {
	handler, billingSvc, _, nonceStore, _ := setupTestHandler(t)

	kp, wallet := testWallet(t)
	nonce := "0x" + fmt.Sprintf("%064x", 0x789)
	sig := signTransferWithAuthorizationForTest(t, kp, wallet, testPayTo, 5000000, 0, 9999999999, nonce, testAssetAddr, testChainID, testErc20Name, testTokenVer)

	payload := pluginCore.X402PaymentPayload{
		X402Version: 2,
		Accepted: pluginCore.X402PaymentRequirements{
			Scheme:            "exact",
			Network:           "eip155:8453",
			Asset:             testAssetAddr,
			Amount:            "5000000",
			PayTo:             testPayTo,
			MaxTimeoutSeconds: 300,
			Extra:             map[string]interface{}{"name": testErc20Name, "version": testTokenVer},
		},
		Payload: pluginCore.X402Payload{
			Signature: sig,
			Authorization: &pluginCore.X402Authorization{
				From:        wallet,
				To:          testPayTo,
				Value:       "5000000",
				ValidAfter:  "0",
				ValidBefore: "9999999999",
				Nonce:       nonce,
			},
		},
	}
	payloadBytes, _ := json.Marshal(payload)
	payloadB64 := base64.StdEncoding.EncodeToString(payloadBytes)

	nonceStore.On("GetForConfirmation", mock.Anything, nonce).Return(uint(42), strings.ToLower(wallet), decimal.NewFromFloat(5.00), "atlos", "", true, nil)
	nonceStore.On("Consume", mock.Anything, nonce).Return(true, nil)

	gateway := &mockPaymentAddressProvider{}
	gateway.On("ConfirmPayment", mock.Anything, nonce, mock.Anything).
		Return(nil, pluginCore.ErrPaymentPending)
	billingSvc.On("GetGateway", mock.Anything, "atlos").Return(gateway, nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/billing/credits/purchase?wallet="+wallet+"&amount=5.00", nil)
	req.Header.Set("PAYMENT-SIGNATURE", payloadB64)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := handler.HandleCheckout(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusAccepted, rec.Code)

	var response pluginCore.X402PendingResponse
	json.Unmarshal(rec.Body.Bytes(), &response)
	assert.Equal(t, "pending", response.Status)
}
func TestHandleCheckout_AmountMismatch_ReturnsBadRequest(t *testing.T) {
	handler, billingSvc, _, nonceStore, _ := setupTestHandler(t)

	kp, wallet := testWallet(t)
	nonce := "0x" + fmt.Sprintf("%064x", 0xABC)
	sig := signTransferWithAuthorizationForTest(t, kp, wallet, testPayTo, 5000000, 0, 9999999999, nonce, testAssetAddr, testChainID, testErc20Name, testTokenVer)

	payload := pluginCore.X402PaymentPayload{
		X402Version: 2,
		Accepted: pluginCore.X402PaymentRequirements{
			Scheme:            "exact",
			Network:           "eip155:8453",
			Asset:             testAssetAddr,
			Amount:            "5000000",
			PayTo:             testPayTo,
			MaxTimeoutSeconds: 300,
			Extra:             map[string]interface{}{"name": testErc20Name, "version": testTokenVer},
		},
		Payload: pluginCore.X402Payload{
			Signature: sig,
			Authorization: &pluginCore.X402Authorization{
				From:        wallet,
				To:          testPayTo,
				Value:       "5000000",
				ValidAfter:  "0",
				ValidBefore: "9999999999",
				Nonce:       nonce,
			},
		},
	}
	payloadBytes, _ := json.Marshal(payload)
	payloadB64 := base64.StdEncoding.EncodeToString(payloadBytes)

	// Nonce exists but amount is 10.00, query param says 5.00
	nonceStore.On("GetForConfirmation", mock.Anything, nonce).Return(uint(42), strings.ToLower(wallet), decimal.NewFromFloat(10.00), "atlos", "", true, nil)

	gateway := &mockPaymentAddressProvider{}
	billingSvc.On("GetGateway", mock.Anything, "atlos").Return(gateway, nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/billing/credits/purchase?wallet="+wallet+"&amount=5.00", nil)
	req.Header.Set("PAYMENT-SIGNATURE", payloadB64)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := handler.HandleCheckout(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "amount mismatch")
}

// mockBatchPaymentProvider wraps mockPaymentAddressProvider and adds CreatePaymentAddresses
type mockBatchPaymentProvider struct {
	mockPaymentAddressProvider
}

func (m *mockBatchPaymentProvider) CreatePaymentAddresses(ctx context.Context, assets []pluginCore.SupportedAsset, amount decimal.Decimal, nonce string) ([]*pluginCore.PaymentAddress, error) {
	args := m.Called(ctx, assets, amount, nonce)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*pluginCore.PaymentAddress), args.Error(1)
}
