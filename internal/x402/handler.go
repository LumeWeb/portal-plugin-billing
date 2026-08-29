package x402

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/shopspring/decimal"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	core "go.lumeweb.com/portal/core"
	"go.lumeweb.com/portal/db/models"
	"go.uber.org/zap"
)

const (
	// keyIdentityType is the KeyIdentityHandler type for EVM wallet auth.
	keyIdentityType = "ethereum"
)

const (
	// x402 credit purchase defaults
	x402Scheme        = "exact"
	x402MaxTimeoutSec = 300
)

type Handler struct {
	billingService   pluginCore.BillingService
	creditService    pluginCore.CreditService
	nonceStore       NonceStore
	paymentAddrStore *PaymentAddressStore
	userService      core.UserService
	tokenGen         TokenGenerator
}

type TokenGenerator func(userID uint) (string, error)

func NewHandler(billing pluginCore.BillingService, credit pluginCore.CreditService, store NonceStore, addrStore *PaymentAddressStore, users core.UserService, tokenGen TokenGenerator) *Handler {
	return &Handler{
		billingService:   billing,
		creditService:    credit,
		nonceStore:       store,
		paymentAddrStore: addrStore,
		userService:      users,
		tokenGen:         tokenGen,
	}
}

type Challenge struct {
	X402Version int                          `json:"x402Version"`
	Accepts     []ChallengeAccepts           `json:"accepts"`
	Resource    *pluginCore.X402ResourceInfo `json:"resource,omitempty"`
	Nonce       string                       `json:"nonce"`
	ExpiresAt   time.Time                    `json:"expiresAt"`
}

type ChallengeAccepts struct {
	Scheme        string `json:"scheme"`
	Network       string `json:"network"`
	Asset         string `json:"asset"`
	Amount        string `json:"amount"`
	PayTo         string `json:"payTo"`
	MaxTimeoutSec int    `json:"maxTimeoutSeconds"`
}

func (h *Handler) HandleCheckout(c echo.Context) error {
	ctx := c.Request().Context()
	wallet := c.QueryParam("wallet")
	amountStr := c.QueryParam("amount")
	gatewayType := DefaultGatewayType

	if wallet == "" {
		return c.NoContent(http.StatusUnauthorized)
	}

	amount, err := decimal.NewFromString(amountStr)
	if err != nil {
		return h.writeError(c, http.StatusBadRequest, "invalid amount")
	}

	sig := c.Request().Header.Get("PAYMENT-SIGNATURE")
	if sig == "" {
		return h.returnChallenge(c, ctx, wallet, amount)
	}

	gatewayIdentity, err := h.billingService.GetGateway(ctx, gatewayType)
	if err != nil {
		return h.writeError(c, http.StatusBadRequest, "gateway not found")
	}

	processor, ok := gatewayIdentity.(pluginCore.PaymentProcessor)
	if !ok {
		return h.writeError(c, http.StatusBadRequest, "gateway does not support x402")
	}

	x402Payload, err := h.parsePayload(sig)
	if err != nil {
		return h.writeError(c, http.StatusBadRequest, "invalid payment payload")
	}

	nonce := h.extractNonce(x402Payload)
	if nonce == "" {
		return h.writeError(c, http.StatusBadRequest, "missing nonce in payload")
	}

	userID, expectedAmount, _, found, err := h.nonceStore.Get(ctx, nonce)
	if err != nil {
		return h.writeError(c, http.StatusInternalServerError, "nonce lookup failed")
	}
	if !found {
		return h.writeError(c, http.StatusUnauthorized, "invalid or expired nonce")
	}

	if !expectedAmount.Equal(amount) {
		return h.writeError(c, http.StatusBadRequest, "amount mismatch")
	}

	confirmation, err := processor.ConfirmPayment(ctx, nonce, amount)
	if errors.Is(err, pluginCore.ErrPaymentPending) {
		return c.JSON(http.StatusAccepted, pluginCore.X402PendingResponse{
			Status:  "pending",
			Message: "payment not yet confirmed, retry with same proof",
		})
	}
	if err != nil {
		return h.writeError(c, http.StatusBadRequest, err.Error())
	}

	if err := h.creditService.IssueCreditFromGateway(
		ctx,
		uint64(userID),
		pluginCore.TransactionTypeCharge,
		confirmation.Amount,
		pluginCore.ReferenceTypeX402Payment,
		confirmation.Reference,
		fmt.Sprintf("x402 payment via %s", gatewayType),
		uint64(userID),
	); err != nil {
		return h.writeError(c, http.StatusInternalServerError, "failed to issue credit")
	}

	h.nonceStore.Delete(ctx, nonce)

	balance, _ := h.creditService.GetUserBalance(ctx, uint64(userID))

	response := pluginCore.X402PaymentResponse{
		CreditBalance: balance.String(),
		AmountPaid:    confirmation.Amount.String(),
		Currency:      confirmation.Currency,
	}

	// tokenGen is always supplied by the API extension (via AuthService.LoginID).
	// Guard remains for defensive coding (e.g. direct handler construction in tests).
	// If nil, we simply omit the JWT token from the response (no fallback token generation).
	if h.tokenGen != nil {
		token, err := h.tokenGen(userID)
		if err != nil {
			h.creditService.Logger().Error("failed to generate JWT for x402 payment",
				zap.Uint("user_id", userID),
				zap.Error(err),
			)
		} else {
			response.Token = token
		}
	}

	settlement := pluginCore.X402SettlementResponse{
		Success:     true,
		Transaction: confirmation.Reference,
		Network:     x402Payload.Accepted.Network,
		Payer:       wallet,
		Amount:      confirmation.Amount.String(),
	}
	if settlementJSON, err := json.Marshal(settlement); err == nil {
		c.Response().Header().Set("PAYMENT-RESPONSE", base64.StdEncoding.EncodeToString(settlementJSON))
	}

	return c.JSON(http.StatusOK, response)
}

func (h *Handler) returnChallenge(c echo.Context, ctx context.Context, wallet string, amount decimal.Decimal) error {
	nonce, err := generateNonce()
	if err != nil {
		return h.writeError(c, http.StatusInternalServerError, "failed to generate nonce")
	}

	user, err := h.findOrCreateUserByWallet(ctx, wallet)
	if err != nil {
		return h.writeError(c, http.StatusInternalServerError, "user setup failed: "+err.Error())
	}

	if err := h.nonceStore.Set(ctx, nonce, user.ID, amount, DefaultGatewayType, 5*time.Minute); err != nil {
		return h.writeError(c, http.StatusInternalServerError, "failed to store nonce")
	}

	gatewayIdentity, err := h.billingService.GetGateway(ctx, DefaultGatewayType)
	if err != nil {
		return h.writeError(c, http.StatusInternalServerError, "gateway not found")
	}

	addrProvider, ok := gatewayIdentity.(pluginCore.PaymentAddressProvider)
	if !ok {
		return h.writeError(c, http.StatusInternalServerError, "gateway does not support payment addresses")
	}

	assets, err := addrProvider.SupportedAssets(ctx)
	if err != nil {
		return h.writeError(c, http.StatusInternalServerError, "failed to get supported assets: "+err.Error())
	}

	accepts := make([]ChallengeAccepts, 0, len(assets))
	for _, asset := range assets {
		paymentAddr, err := addrProvider.CreatePaymentAddress(ctx, asset.AssetCode, asset.BlockchainCode, amount, nonce)
		if err != nil {
			return h.writeError(c, http.StatusInternalServerError, "failed to create payment address: "+err.Error())
		}

		if h.paymentAddrStore != nil {
			if err := h.paymentAddrStore.Create(ctx, X402PaymentAddress{
				Nonce:          nonce,
				PaymentID:      paymentAddr.PaymentID,
				WalletAddress:  paymentAddr.WalletAddress,
				AssetCode:      asset.AssetCode,
				BlockchainCode: asset.BlockchainCode,
				Amount:         paymentAddr.Amount,
			}); err != nil {
				return h.writeError(c, http.StatusInternalServerError, "failed to store payment address")
			}
		}

		if err := h.nonceStore.SetGatewayPaymentID(ctx, nonce, paymentAddr.PaymentID); err != nil {
			return h.writeError(c, http.StatusInternalServerError, "failed to store payment ID")
		}

		accepts = append(accepts, ChallengeAccepts{
			Scheme:        x402Scheme,
			Network:       fmt.Sprintf("eip155:%d", int64(asset.BlockchainCode)),
			Asset:         asset.TokenAddress,
			Amount:        paymentAddr.Amount,
			PayTo:         paymentAddr.WalletAddress,
			MaxTimeoutSec: x402MaxTimeoutSec,
		})
	}

	challenge := Challenge{
		X402Version: 2,
		Nonce:       nonce,
		ExpiresAt:   time.Now().Add(5 * time.Minute),
		Accepts:     accepts,
		Resource: &pluginCore.X402ResourceInfo{
			URL:         "https://" + c.Request().Host + c.Request().URL.Path,
			Description: "Billing credits purchase",
			MimeType:    "application/json",
		},
	}

	challengeJSON, err := json.Marshal(challenge)
	if err != nil {
		return h.writeError(c, http.StatusInternalServerError, "failed to marshal challenge")
	}

	c.Response().Header().Set("Payment-Required", base64.StdEncoding.EncodeToString(challengeJSON))
	return c.NoContent(http.StatusPaymentRequired)
}

func (h *Handler) writeError(c echo.Context, status int, msg string) error {
	return c.JSON(status, pluginCore.X402ErrorResponse{Error: msg})
}

func (h *Handler) findOrCreateUserByWallet(ctx context.Context, wallet string) (*models.User, error) {
	exists, keyIdentity, err := h.userService.KeyIdentityExists(ctx, keyIdentityType, wallet)
	if err != nil {
		return nil, fmt.Errorf("key identity lookup failed: %w", err)
	}
	if exists {
		return &keyIdentity.User, nil
	}

	email := core.AnonEmail(wallet)
	password := core.GenerateSecurityToken() + core.GenerateSecurityToken()

	user, err := h.userService.CreateAccount(ctx, email, password, false)
	if err != nil {
		if acctErr := core.AsAccountError(err); acctErr != nil && acctErr.IsErrorType(core.ErrKeyEmailAlreadyExists) {
			_, existingUser, lookupErr := h.userService.EmailExists(ctx, email)
			if lookupErr != nil {
				return nil, fmt.Errorf("email lookup failed: %w", lookupErr)
			}
			if existingUser == nil {
				return nil, fmt.Errorf("email exists but user not found")
			}
			return existingUser, nil
		}
		return nil, fmt.Errorf("create account failed: %w", err)
	}

	if err := h.userService.UpdateAccountInfo(ctx, user.ID, map[string]interface{}{"verified": true}); err != nil {
		return nil, fmt.Errorf("verify account failed: %w", err)
	}
	user.Verified = true

	if err := h.userService.AddKeyIdentity(ctx, user.ID, keyIdentityType, wallet, nil); err != nil {
		return nil, fmt.Errorf("add key identity failed: %w", err)
	}

	return user, nil
}

func (h *Handler) parsePayload(header string) (*pluginCore.X402PaymentPayload, error) {
	if header == "" {
		return nil, errors.New("missing payment signature")
	}

	decoded, err := base64.StdEncoding.DecodeString(header)
	if err != nil {
		decoded = []byte(header)
	}

	var payload pluginCore.X402PaymentPayload
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return nil, fmt.Errorf("invalid payment signature format: %w", err)
	}

	return &payload, nil
}

func (h *Handler) extractNonce(payload *pluginCore.X402PaymentPayload) string {
	if payload == nil {
		return ""
	}
	if payload.Payload.Nonce != "" {
		return payload.Payload.Nonce
	}
	if payload.Payload.Authorization != nil && payload.Payload.Authorization.Nonce != "" {
		return payload.Payload.Authorization.Nonce
	}
	return ""
}

func generateNonce() (string, error) {
	uid, err := uuid.NewRandom()
	if err != nil {
		return "", err
	}
	return uid.String(), nil
}
