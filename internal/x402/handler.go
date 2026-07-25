package x402

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/shopspring/decimal"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	core "go.lumeweb.com/portal/core"
	"go.lumeweb.com/portal/db/models"
	"go.uber.org/zap"
)

type Handler struct {
	billingService pluginCore.BillingService
	creditService  pluginCore.CreditService
	nonceStore     NonceStore
	userService    core.UserService
	tokenGen       TokenGenerator
}

type TokenGenerator func(userID uint) (string, error)

func NewHandler(billing pluginCore.BillingService, credit pluginCore.CreditService, store NonceStore, users core.UserService, tokenGen TokenGenerator) *Handler {
	return &Handler{
		billingService: billing,
		creditService:  credit,
		nonceStore:     store,
		userService:    users,
		tokenGen:       tokenGen,
	}
}

type Challenge struct {
	X402Version int                `json:"x402Version"`
	Accepts     []ChallengeAccepts `json:"accepts"`
	Nonce       string             `json:"nonce"`
	ExpiresAt   time.Time          `json:"expiresAt"`
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
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid amount"})
	}

	sig := c.Request().Header.Get("PAYMENT-SIGNATURE")
	if sig == "" {
		return h.returnChallenge(c, ctx, wallet, amount)
	}

	gatewayIdentity, err := h.billingService.GetGateway(ctx, gatewayType)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "gateway not found"})
	}

	processor, ok := gatewayIdentity.(pluginCore.PaymentProcessor)
	if !ok {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "gateway does not support x402"})
	}

	x402Payload, err := h.parsePayload(sig)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid payment payload"})
	}

	nonce := h.extractNonce(x402Payload)
	if nonce == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "missing nonce in payload"})
	}

	userID, expectedAmount, _, found, err := h.nonceStore.Get(ctx, nonce)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "nonce lookup failed"})
	}
	if !found {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid or expired nonce"})
	}

	if !expectedAmount.Equal(amount) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "amount mismatch"})
	}

	confirmation, err := processor.ConfirmPayment(ctx, nonce, amount)
	if errors.Is(err, pluginCore.ErrPaymentPending) {
		return c.JSON(http.StatusAccepted, map[string]string{
			"status":  "pending",
			"message": "payment not yet confirmed, retry with same proof",
		})
	}
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
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
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to issue credit"})
	}

	h.nonceStore.Delete(ctx, nonce)

	balance, _ := h.creditService.GetUserBalance(ctx, uint64(userID))

	response := map[string]interface{}{
		"credit_balance": balance,
		"amount_paid":    confirmation.Amount.String(),
		"currency":       confirmation.Currency,
	}

	if h.tokenGen != nil {
		token, err := h.tokenGen(userID)
		if err != nil {
			h.creditService.Logger().Error("failed to generate JWT for x402 payment",
				zap.Uint("user_id", userID),
				zap.Error(err),
			)
		} else {
			response["token"] = token
		}
	}

	return c.JSON(http.StatusOK, response)
}

func (h *Handler) returnChallenge(c echo.Context, ctx context.Context, wallet string, amount decimal.Decimal) error {
	nonce, err := generateNonce()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to generate nonce"})
	}

	user, err := h.findOrCreateUserByWallet(ctx, wallet)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "user setup failed: " + err.Error()})
	}

	if err := h.nonceStore.Set(ctx, nonce, user.ID, amount, DefaultGatewayType, 5*time.Minute); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to store nonce"})
	}

	gatewayIdentity, err := h.billingService.GetGateway(ctx, DefaultGatewayType)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "gateway not found"})
	}

	addrProvider, ok := gatewayIdentity.(pluginCore.PaymentAddressProvider)
	if !ok {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "gateway does not support payment addresses"})
	}

	paymentAddr, err := addrProvider.CreatePaymentAddress(ctx, "USDC", 8453, amount, nonce)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create payment address: " + err.Error()})
	}

	if err := h.nonceStore.SetGatewayPaymentID(ctx, nonce, paymentAddr.PaymentID); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to store payment ID"})
	}

	challenge := Challenge{
		X402Version: 2,
		Nonce:       nonce,
		ExpiresAt:   time.Now().Add(5 * time.Minute),
		Accepts: []ChallengeAccepts{{
			Scheme:        "exact",                                      // direct transfer, no signed authorization
			Network:       "eip155:8453",                                // Base mainnet
			Asset:         "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913", // USDC
			Amount:        amount.Mul(decimal.NewFromInt(1e6)).String(), // 6 decimals
			PayTo:         paymentAddr.WalletAddress,
			MaxTimeoutSec: 300,
		}},
	}

	challengeJSON, err := json.Marshal(challenge)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to marshal challenge"})
	}

	c.Response().Header().Set("Payment-Required", base64.StdEncoding.EncodeToString(challengeJSON))
	return c.NoContent(http.StatusPaymentRequired)
}

func (h *Handler) findOrCreateUserByWallet(ctx context.Context, wallet string) (*models.User, error) {
	exists, pubkey, err := h.userService.PubkeyExists(ctx, wallet)
	if err != nil {
		return nil, fmt.Errorf("pubkey lookup failed: %w", err)
	}
	if exists {
		return &pubkey.User, nil
	}

	email := fmt.Sprintf("anon_%s@local.invalid", strings.ToLower(wallet))
	password := core.GenerateSecurityToken() + core.GenerateSecurityToken() // 12 random chars

	user, err := h.userService.CreateAccount(ctx, email, password, false) // verifyEmail=false
	if err != nil {
		// If email already exists (race condition), look up the existing user
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

	if err := h.userService.AddPubkeyToAccount(ctx, *user, wallet); err != nil {
		return nil, fmt.Errorf("add pubkey failed: %w", err)
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
	if n, ok := payload.Payload["nonce"].(string); ok && n != "" {
		return n
	}
	if auth, ok := payload.Payload["authorization"].(map[string]interface{}); ok {
		if n, ok := auth["nonce"].(string); ok {
			return n
		}
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
