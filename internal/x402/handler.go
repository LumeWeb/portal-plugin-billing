package x402

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/shopspring/decimal"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	core "go.lumeweb.com/portal/core"
	"go.lumeweb.com/portal/db/models"
)

// Handler handles x402 challenge/response for credit purchases.
type Handler struct {
	billingService pluginCore.BillingService
	creditService  pluginCore.CreditService
	nonceStore     NonceStore
	userService    core.UserService
}

// NewHandler creates a new x402 handler.
func NewHandler(billing pluginCore.BillingService, credit pluginCore.CreditService, store NonceStore, users core.UserService) *Handler {
	return &Handler{
		billingService: billing,
		creditService:  credit,
		nonceStore:     store,
		userService:    users,
	}
}

// Challenge represents an x402 payment challenge.
type Challenge struct {
	X402Version int                `json:"x402Version"`
	Accepts     []ChallengeAccepts `json:"accepts"`
	Nonce       string             `json:"nonce"`
	ExpiresAt   time.Time          `json:"expiresAt"`
}

// ChallengeAccepts defines what payment schemes the server accepts.
type ChallengeAccepts struct {
	Scheme          string `json:"scheme"`
	Network         string `json:"network"`
	Asset           string `json:"asset"`
	Amount          string `json:"amount"`
	PayTo           string `json:"payTo"`
	MaxTimeoutSec   int    `json:"maxTimeoutSeconds"`
}

// HandleCheckout handles POST /api/account/billing/checkout with x402.
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

	// Get ATLOS gateway from registry (hardcoded)
	gatewayIdentity, err := h.billingService.GetGateway(ctx, gatewayType)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "gateway not found"})
	}

	// Check if gateway supports PaymentProcessor
	processor, ok := gatewayIdentity.(pluginCore.PaymentProcessor)
	if !ok {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "gateway does not support x402"})
	}

	// No signature → return challenge
	sig := c.Request().Header.Get("PAYMENT-SIGNATURE")
	if sig == "" {
		return h.returnChallenge(c, ctx, wallet, amount)
	}

	// Parse payment payload from signature header
	nonce, payer, signature, payloadAmount, err := h.parsePayload(sig)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid payment payload"})
	}

	// Verify recovered address matches wallet
	if payer != wallet {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "wallet mismatch"})
	}

	// Check nonce exists
	userID, expectedAmount, _, ok, err := h.nonceStore.Get(ctx, nonce)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "nonce lookup failed"})
	}
	if !ok {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid or expired nonce"})
	}

	if !expectedAmount.Equal(amount) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "amount mismatch"})
	}

	// Verify signature cryptographically
	if err := processor.VerifyPaymentSignature(ctx, nonce, payer, signature, payloadAmount); err != nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid signature"})
	}

	// Confirm payment settled
	confirmation, err := processor.ConfirmPayment(ctx, nonce, amount)
	if errors.Is(err, pluginCore.ErrPaymentPending) {
		return c.JSON(http.StatusAccepted, map[string]string{
			"status":  "pending",
			"message": "payment not yet confirmed, retry with same signature",
		})
	}
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	// Issue credit
	if err := h.creditService.IssueCreditFromGateway(
		ctx,
		uint64(userID),
		pluginCore.TransactionTypeCharge,
		confirmation.Amount,
		pluginCore.ReferenceTypeAtlosPayment,
		confirmation.Reference,
		fmt.Sprintf("x402 payment via %s", gatewayType),
		uint64(userID),
	); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to issue credit"})
	}

	// Clean up nonce
	h.nonceStore.Delete(ctx, nonce)

	// Return balance + token
	balance, _ := h.creditService.GetUserBalance(ctx, uint64(userID))
	token := generateJWT(userID)

	return c.JSON(http.StatusOK, map[string]interface{}{
		"credit_balance": balance,
		"token":          token,
	})
}

func (h *Handler) returnChallenge(c echo.Context, ctx context.Context, wallet string, amount decimal.Decimal) error {
	nonce, err := generateNonce()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to generate nonce"})
	}

	// Find or create user by wallet
	user, err := h.userHelper(ctx, wallet)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "user lookup failed"})
	}

	if err := h.nonceStore.Set(ctx, nonce, user.ID, amount, DefaultGatewayType, 5*time.Minute); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to store nonce"})
	}
	challenge := Challenge{
		X402Version: 2,
		Nonce:       nonce,
		ExpiresAt:   time.Now().Add(5 * time.Minute),
		Accepts: []ChallengeAccepts{{
			Scheme:          "evm_exact",
			Network:         "eip155:8453", // Base mainnet
			Asset:           "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913", // USDC
			Amount:          amount.Mul(decimal.NewFromInt(1e6)).String(), // 6 decimals
			PayTo:           "0x", // TODO: set actual ATLOS deposit address
			MaxTimeoutSec:   300,
		}},
	}

	challengeJSON, err := json.Marshal(challenge)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to marshal challenge"})
	}

	c.Response().Header().Set("Payment-Required", base64.StdEncoding.EncodeToString(challengeJSON))
	return c.NoContent(http.StatusPaymentRequired)
}

// userHelper finds or creates a user by wallet address.
func (h *Handler) userHelper(ctx context.Context, wallet string) (*models.User, error) {
	// Check if user exists with this pubkey/wallet
	exists, pubkey, err := h.userService.PubkeyExists(ctx, wallet)
	if err != nil {
		return nil, err
	}
	if exists {
		return &pubkey.User, nil
	}

	// Create anonymous user — portal doesn't have built-in wallet-based account creation
	// For now, return error. This needs user service extension.
	return nil, fmt.Errorf("user not found for wallet %s", wallet)
}

// parsePayload extracts nonce, payer, signature, and amount from the PAYMENT-SIGNATURE header.
func (h *Handler) parsePayload(sig string) (nonce string, payer string, signature string, amount decimal.Decimal, err error) {
	// TODO: implement actual x402 payload parsing
	// This is a placeholder — will be replaced with real EIP-712 parsing
	return "", "", "", decimal.Zero, errors.New("not implemented")
}

func generateNonce() (string, error) {
	// TODO: use crypto/rand or ulid
	return "test-nonce", nil
}

func generateJWT(userID uint) string {
	// TODO: integrate with existing JWT service
	return ""
}
