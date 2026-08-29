package x402

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

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
	// createAddressMaxConcurrency limits parallel CreatePaymentAddress calls
	// to avoid exhausting the ATLOS connection pool or API quota.
	createAddressMaxConcurrency = 4
)

type Handler struct {
	billingService   pluginCore.BillingService
	creditService    pluginCore.CreditService
	nonceStore       NonceStore
	paymentAddrStore *PaymentAddressStore
	userService      core.UserService
	jwtIssuer        JWTIssuer
}

type JWTIssuer func(userID uint) (string, error)

func NewHandler(billing pluginCore.BillingService, credit pluginCore.CreditService, store NonceStore, addrStore *PaymentAddressStore, users core.UserService, jwtIssuer JWTIssuer) *Handler {
	return &Handler{
		billingService:   billing,
		creditService:    credit,
		nonceStore:       store,
		paymentAddrStore: addrStore,
		userService:      users,
		jwtIssuer:        jwtIssuer,
	}
}



func (h *Handler) HandleCheckout(c echo.Context) error {
	ctx := c.Request().Context()
	wallet := c.QueryParam("wallet")
	amountStr := c.QueryParam("amount")
	gatewayType := DefaultGatewayType

	if wallet == "" {
		return c.NoContent(http.StatusUnauthorized)
	}

	// EVM addresses are case-insensitive — normalize to lowercase so
	// findOrCreateUserByWallet, AnonEmail, and bindKeyIdentity all use
	// the same canonical address regardless of the client's casing.
	wallet = strings.ToLower(wallet)

	// Validate EVM wallet address format (0x + 40 hex chars)
	if !isValidEVMAddressFormat(wallet) {
		return h.writeError(c, http.StatusBadRequest, "invalid wallet address format")
	}

	amount, err := decimal.NewFromString(amountStr)
	if err != nil {
		return h.writeError(c, http.StatusBadRequest, "invalid amount")
	}
	if amount.LessThanOrEqual(decimal.Zero) {
		return h.writeError(c, http.StatusBadRequest, "amount must be greater than zero")
	}
	// ATLOS invoices are in USD with 2 decimal places. Reject amounts that
	// would truncate to $0.00 (e.g. 0.005).
	if amount.Truncate(2).LessThanOrEqual(decimal.Zero) {
		return h.writeError(c, http.StatusBadRequest, "amount must be at least $0.01")
	}
	// Normalize to 2 decimals so the invoice, challenge Amount, and credited
	// amount all agree. Without this, a request like $5.009 would create a $5.00
	// invoice but compute the on-chain token amount from $5.009.
	amount = amount.Truncate(2)

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
		return h.writeError(c, http.StatusBadRequest, "missing nonce in payload authorization")
	}

	// Per x402 v2 spec, verify the EIP-712 TransferWithAuthorization signature
	// to prove the payer signed the authorization. This prevents replay attacks
	// where someone who intercepted the 402 challenge nonce uses it to obtain a JWT.
	if x402Payload.Payload.Authorization == nil {
		return h.writeError(c, http.StatusBadRequest, "missing authorization in payload")
	}
	if x402Payload.Payload.Signature == "" {
		return h.writeError(c, http.StatusUnauthorized, "missing signature in payment payload")
	}

	// Extract chain ID from the accepted network (e.g. "eip155:84532" -> 84532).
	chainID, err := parseChainID(x402Payload.Accepted.Network)
	if err != nil {
		return h.writeError(c, http.StatusBadRequest, "invalid network in accepted payment requirements: "+err.Error())
	}

	// Extract token name/version from extra.
	tokenName, _ := x402Payload.Accepted.Extra["name"].(string)
	tokenVersion, _ := x402Payload.Accepted.Extra["version"].(string)

	recoveredAddr, err := verifyPaymentSignature(
		*x402Payload.Payload.Authorization,
		x402Payload.Payload.Signature,
		x402Payload.Accepted.Asset,
		chainID,
		tokenName,
		tokenVersion,
	)
	if err != nil {
		h.creditService.Logger().Warn("x402 payment signature verification failed",
			zap.String("wallet", wallet),
			zap.String("nonce", nonce),
			zap.Error(err),
		)
		return h.writeError(c, http.StatusUnauthorized, "payment signature verification failed")
	}

	// Verify the payer address from the authorization matches the wallet
	// query param. This is the first line of defense against nonce theft.
	if !strings.EqualFold(recoveredAddr, wallet) {
		return h.writeError(c, http.StatusUnauthorized, "payer address does not match wallet")
	}

	userID, challengeWallet, expectedAmount, _, challengeAcceptsJSON, found, err := h.nonceStore.GetForConfirmation(ctx, nonce)
	if err != nil {
		return h.writeError(c, http.StatusInternalServerError, "nonce lookup failed")
	}
	if !found {
		return h.writeError(c, http.StatusUnauthorized, "invalid or expired nonce")
	}

	// The wallet that initiated the challenge is stored on the nonce record.
	// The EIP-712 signer must match that wallet — prevents an attacker from
	// completing a stolen nonce with their own signature.
	if !strings.EqualFold(recoveredAddr, challengeWallet) {
		return h.writeError(c, http.StatusUnauthorized, "payer address does not match challenge wallet")
	}

	// Allow a $0.01 USD tolerance to match ConfirmPayment and ATLOS webhook tolerance.
	// Without this, a callback amount rounded within one cent of the invoice
	// would be rejected, blocking the x402 payment flow.
	tolerance := decimal.NewFromFloat(0.01)
	if expectedAmount.Sub(amount).Abs().GreaterThan(tolerance) {
		return h.writeError(c, http.StatusBadRequest, "amount mismatch")
	}

	// Validate that the submitted Accepted payment requirements match the
	// challenge we issued. This prevents a client from substituting a
	// different asset, payTo address, network, or token params.
	if err := h.validateAcceptedMatchesChallenge(challengeAcceptsJSON, x402Payload.Accepted); err != nil {
		return h.writeError(c, http.StatusBadRequest, "accepted payment requirements do not match challenge")
	}

	// Verify the EIP-3009 authorization's To and Value fields match the
	// accepted payment requirements' PayTo and Amount. EVM addresses are
	// case-insensitive.
	if !strings.EqualFold(x402Payload.Payload.Authorization.To, x402Payload.Accepted.PayTo) {
		return h.writeError(c, http.StatusBadRequest, "authorization payTo does not match accepted payTo")
	}
	if x402Payload.Payload.Authorization.Value != x402Payload.Accepted.Amount {
		return h.writeError(c, http.StatusBadRequest, "authorization value does not match accepted amount")
	}

	// Confirm payment first. If pending, the client retries with the same
	// proof — the nonce must remain intact for retries.
	// Double-credit is prevented by the shared idempotency key (x402-{nonce})
	// in IssueCreditWithIdempotency, which both the webhook and callback use.
	confirmation, err := processor.ConfirmPayment(ctx, nonce, expectedAmount)
	if errors.Is(err, pluginCore.ErrPaymentPending) {
		return c.JSON(http.StatusAccepted, pluginCore.X402PendingResponse{
			Status:  "pending",
			Message: "payment not yet confirmed, retry with same proof",
		})
	}
	if err != nil {
		h.creditService.Logger().Error("x402 ConfirmPayment failed",
			zap.String("nonce", nonce),
			zap.Error(err),
		)
		return h.writeError(c, http.StatusBadRequest, "payment confirmation failed")
	}

	// Atomically consume the nonce BEFORE issuing credit. Consume deletes the
	// pending nonce; if it returns false, the webhook already settled and
	// credited — skip IssueCreditWithIdempotency entirely.
	consumed, err := h.nonceStore.Consume(ctx, nonce)
	if err != nil {
		h.creditService.Logger().Error("failed to consume x402 nonce",
			zap.String("nonce", nonce),
			zap.Error(err),
		)
		return h.writeError(c, http.StatusInternalServerError, "failed to consume nonce")
	}
	if !consumed {
		// Webhook already settled and credited. Return success without
		// issuing a second credit.
		balance, balErr := h.creditService.GetUserBalance(ctx, uint64(userID))
		if balErr != nil {
			balance = decimal.Zero
		}
		return c.JSON(http.StatusOK, pluginCore.X402PaymentResponse{
			CreditBalance: balance.String(),
			AmountPaid:    confirmation.Amount.String(),
			Currency:      confirmation.Currency,
		})
	}

	// We won the consume race — we're the sole credit issuer.
	if err := h.creditService.IssueCreditWithIdempotency(
		ctx,
		uint64(userID),
		pluginCore.TransactionTypeCharge,
		confirmation.Amount,
		pluginCore.ReferenceTypeX402Payment,
		fmt.Sprintf("x402-%s", nonce),
		fmt.Sprintf("x402 payment via %s", gatewayType),
		uint64(userID),
	); err != nil {
		return h.writeError(c, http.StatusInternalServerError, "failed to issue credit")
	}

	// Mark the account verified now that payment is confirmed and signature verified.
	if err := h.userService.UpdateAccountInfo(ctx, userID, map[string]interface{}{"verified": true}); err != nil {
		return h.writeError(c, http.StatusInternalServerError, "failed to verify account")
	}

	// Bind the wallet key identity now that the EIP-712 signature has been
	// verified, proving the caller controls the wallet's private key.
	if err := h.bindKeyIdentity(ctx, userID, wallet); err != nil {
		return h.writeError(c, http.StatusInternalServerError, "failed to bind key identity: "+err.Error())
	}

	balance, err := h.creditService.GetUserBalance(ctx, uint64(userID))
	if err != nil {
		h.creditService.Logger().Error("failed to fetch user balance after credit issuance",
			zap.Uint("user_id", userID),
			zap.Error(err),
		)
		return c.JSON(http.StatusOK, pluginCore.X402PaymentResponse{
			CreditBalance: "0",
			AmountPaid:    confirmation.Amount.String(),
			Currency:      confirmation.Currency,
		})
	}

	response := pluginCore.X402PaymentResponse{
		CreditBalance: balance.String(),
		AmountPaid:    confirmation.Amount.String(),
		Currency:      confirmation.Currency,
	}

	// jwtIssuer is always supplied by the API extension (via AuthService.LoginID).
	// Guard remains for defensive coding (e.g. direct handler construction in tests).
	// If nil, we simply omit the JWT from the response (no fallback generation).
	if h.jwtIssuer != nil {
		jwt, err := h.jwtIssuer(userID)
		if err != nil {
			h.creditService.Logger().Error("failed to generate JWT for x402 payment",
				zap.Uint("user_id", userID),
				zap.Error(err),
			)
		} else {
			response.Token = jwt
		}
	}

	// Set the PAYMENT-RESPONSE header per x402 v2 spec (SettlementResponse).
	settlement := pluginCore.X402SettlementResponse{
		Success:     true,
		Payer:       wallet,
		Transaction: confirmation.Reference,
		Network:     x402Payload.Accepted.Network,
	}
	if settlementJSON, err := json.Marshal(settlement); err == nil {
		c.Response().Header().Set("PAYMENT-RESPONSE", base64.StdEncoding.EncodeToString(settlementJSON))
	}

	return c.JSON(http.StatusOK, response)
}

func (h *Handler) returnChallenge(c echo.Context, ctx context.Context, wallet string, amount decimal.Decimal) error {
	// Verify gateway capabilities before creating any state (user, nonce).
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
	if len(assets) == 0 {
		return h.writeError(c, http.StatusServiceUnavailable, "no supported assets available for payment")
	}

	// Cap the number of assets to bound response size, DB writes, and ATLOS API calls.
	// Reduced from 8 to 4 to limit synchronous API fan-out per challenge request
	// before any payment is made. The 10 req/min/IP limiter plus batch address
	// creation (single invoice, N sessions) provides defense-in-depth.
	const maxChallengeAssets = 4
	if len(assets) > maxChallengeAssets {
		assets = assets[:maxChallengeAssets]
	}

	// Generate nonce and persist challenge data.
	nonce, err := generateNonce()
	if err != nil {
		return h.writeError(c, http.StatusInternalServerError, "failed to generate nonce")
	}

	// Create the user account now (unverified, no key identity bound) so the
	// webhook can issue credit immediately when payment arrives. Key identity
	// binding is deferred to HandleCheckout after EIP-712 verification.
	//
	// Resource generation (DB rows, ATLOS invoices) before signature verification
	// is mitigated by:
	//   1. Per-IP rate limiting (10 req/min/IP) in x402RateLimitMiddleware.
	//   2. maxChallengeAssets=4 cap bounding ATLOS API calls per request.
	//   3. 5-min nonce expiry auto-cleans unconsumed challenges.
	//   4. findOrCreateUserByWallet is idempotent — repeat challenges for the
	//      same wallet reuse the same shell account, not duplicates.
	// A per-wallet rate limiter could further tighten this, but the IP limiter
	// + asset cap + nonce expiry are sufficient for the current threat model.
	user, err := h.findOrCreateUserByWallet(ctx, wallet)
	if err != nil {
		return h.writeError(c, http.StatusInternalServerError, "user setup failed: "+err.Error())
	}

	type assetResult struct {
		asset      pluginCore.SupportedAsset
		paymentAddr *pluginCore.PaymentAddress
		err        error
	}

	// Use batch creation if the gateway supports it (single invoice, N payment sessions).
	// Otherwise fall back to per-asset creation (N invoices).
	var results []assetResult
	if batchProvider, ok := gatewayIdentity.(pluginCore.BatchPaymentAddressProvider); ok {
		addresses, err := batchProvider.CreatePaymentAddresses(ctx, assets, amount, nonce)
		if err != nil {
			return h.writeError(c, http.StatusInternalServerError, "failed to create payment addresses: "+err.Error())
		}
		if len(addresses) != len(assets) {
			// Cancel the shared invoice that CreatePaymentAddresses created
			// internally. Without this, the orphaned invoice stays open in ATLOS.
			if len(addresses) > 0 && addresses[0] != nil && addresses[0].InvoiceID != "" {
				_ = addrProvider.CancelPaymentAddress(ctx, addresses[0].InvoiceID)
			}
			return h.writeError(c, http.StatusInternalServerError, "gateway returned wrong number of payment addresses")
		}
		results = make([]assetResult, len(assets))
		for i, addr := range addresses {
			results[i] = assetResult{asset: assets[i], paymentAddr: addr}
		}
	} else {
		results = make([]assetResult, len(assets))
		var wg sync.WaitGroup
		sem := make(chan struct{}, createAddressMaxConcurrency)
		for i, asset := range assets {
			wg.Add(1)
			sem <- struct{}{}
			go func(idx int, ast pluginCore.SupportedAsset) {
				defer wg.Done()
				defer func() { <-sem }()
				pa, err := addrProvider.CreatePaymentAddress(ctx, ast.AssetCode, ast.BlockchainCode, amount, nonce)
				results[idx] = assetResult{asset: ast, paymentAddr: pa, err: err}
			}(i, asset)
		}
		wg.Wait()
	}

	// cancelCreatedAddrs cancels all ATLOS invoices up to the given index and
	// deletes any DB payment-address rows for this nonce. Errors are logged,
	// not returned, so cleanup doesn't mask the original error.
	cancelCreatedAddrs := func(failureIdx int) {
		for j := 0; j < failureIdx; j++ {
			if results[j].paymentAddr != nil && results[j].paymentAddr.InvoiceID != "" {
				if err := addrProvider.CancelPaymentAddress(ctx, results[j].paymentAddr.InvoiceID); err != nil {
					h.creditService.Logger().Error("failed to rollback ATLOS invoice",
						zap.String("invoice_id", results[j].paymentAddr.InvoiceID),
						zap.Error(err),
					)
				}
			}
		}
		// Delete any DB payment-address rows that were already inserted.
		if h.paymentAddrStore != nil {
			if err := h.paymentAddrStore.DeleteByNonce(ctx, nonce); err != nil {
				h.creditService.Logger().Error("failed to delete payment address rows during rollback",
					zap.String("nonce", nonce),
					zap.Error(err),
				)
			}
		}
	}

	accepts := make([]pluginCore.X402PaymentRequirements, 0, len(assets))
	for i, res := range results {
		if res.err != nil {
			// Rollback: cancel ALL successfully-created payment addresses.
			cancelCreatedAddrs(len(results))
			return h.writeError(c, http.StatusInternalServerError, "failed to create payment address: "+res.err.Error())
		}

		if h.paymentAddrStore != nil {
			if err := h.paymentAddrStore.Create(ctx, X402PaymentAddress{
				Nonce:          nonce,
				PaymentID:      res.paymentAddr.PaymentID,
				WalletAddress:  res.paymentAddr.WalletAddress,
				AssetCode:      res.asset.AssetCode,
				BlockchainCode: res.asset.BlockchainCode,
				Amount:         res.paymentAddr.Amount,
			}); err != nil {
				cancelCreatedAddrs(len(results))
				return h.writeError(c, http.StatusInternalServerError, "failed to store payment address")
			}
		}

		// Set the nonce's gateway_payment_id only once (primary asset).
		// Per-asset payment IDs are stored in paymentAddrStore.
		if i == 0 {
			if err := h.nonceStore.SetGatewayPaymentID(ctx, nonce, res.paymentAddr.PaymentID); err != nil {
				cancelCreatedAddrs(len(results))
				return h.writeError(c, http.StatusInternalServerError, "failed to store payment ID")
			}
		}

		accepts = append(accepts, pluginCore.X402PaymentRequirements{
			Scheme:            x402Scheme,
			Network:           fmt.Sprintf("eip155:%d", res.asset.BlockchainCode),
			Asset:             res.asset.TokenAddress,
			Amount:            res.paymentAddr.Amount,
			PayTo:             res.paymentAddr.WalletAddress,
			MaxTimeoutSeconds: x402MaxTimeoutSec,
			Extra: map[string]interface{}{
				"name":    res.asset.AssetName,
				"version": res.asset.TokenVersion,
				"nonce":   nonce,
			},
		})
		}

		// Persist the nonce with the challenge accepts as JSON for later validation.
	acceptsJSON, err := json.Marshal(accepts)
	if err != nil {
		cancelCreatedAddrs(len(results))
		return h.writeError(c, http.StatusInternalServerError, "failed to marshal challenge accepts")
	}

	if err := h.nonceStore.Set(ctx, nonce, user.ID, wallet, amount, DefaultGatewayType, string(acceptsJSON), 5*time.Minute); err != nil {
		cancelCreatedAddrs(len(results))
		return h.writeError(c, http.StatusInternalServerError, "failed to store nonce")
	}

	paymentRequired := pluginCore.X402PaymentRequired{
		X402Version: 2,
		Error:       "PAYMENT-SIGNATURE header is required",
		Resource: &pluginCore.X402ResourceInfo{
			URL:         h.challengeResourceURL(c),
			Description: "Billing credits purchase",
			MimeType:    "application/json",
		},
		Accepts:     accepts,
		}

		challengeJSON, err := json.Marshal(paymentRequired)
		if err != nil {
		return h.writeError(c, http.StatusInternalServerError, "failed to marshal payment required response")
		}

		c.Response().Header().Set("PAYMENT-REQUIRED", base64.StdEncoding.EncodeToString(challengeJSON))
		return c.NoContent(http.StatusPaymentRequired)
}

// challengeResourceURL derives the URL of the protected resource advertised in
// the PAYMENT-REQUIRED challenge. The scheme is taken from the request's TLS
// state rather than hardcoded so the URL stays correct behind TLS-terminating
// proxies or over plain HTTP.
func (h *Handler) challengeResourceURL(c echo.Context) string {
	scheme := "http"
	if c.Request().TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + c.Request().Host + c.Request().URL.Path
}

func (h *Handler) writeError(c echo.Context, status int, msg string) error {
	return c.JSON(status, pluginCore.X402ErrorResponse{Error: msg})
}

// findOrCreateUserByWallet resolves or creates an unverified account for the
// given wallet address. Called from returnChallenge (before payment) so the
// webhook can issue credit when the ATLOS payment arrives.
//
// Account squatting is NOT a concern here:
//   - The account is created unverified with no key identity bound.
//   - Key identity binding only happens in HandleCheckout after EIP-712
//     signature verification proves the caller controls the wallet's
//     private key. An attacker cannot bind someone else's wallet.
//   - The deterministic anon email (core.AnonEmail) means a second challenge
//     for the same wallet reuses the same shell account, not a duplicate.
//   - Wallet addresses are public identifiers; creating an empty unverified
//     account for one leaks no information and grants no access.
func (h *Handler) findOrCreateUserByWallet(ctx context.Context, wallet string) (*models.User, error) {
	exists, keyIdentity, err := h.userService.KeyIdentityExists(ctx, keyIdentityType, wallet)
	if err != nil {
		return nil, fmt.Errorf("key identity lookup failed: %w", err)
	}
	if exists {
		return &keyIdentity.User, nil
	}

	// Create the account but do NOT bind the wallet key identity yet.
	// See findOrCreateUserByWallet doc comment for why this is safe.
	email := core.AnonEmail(wallet)
	authToken := core.GenerateSecurityToken() + core.GenerateSecurityToken()

	user, err := h.userService.CreateAccount(ctx, email, authToken, false)
	if err != nil {
		if acctErr := core.AsAccountError(err); acctErr != nil && acctErr.IsErrorType(core.ErrKeyEmailAlreadyExists) {
			_, existingUser, lookupErr := h.userService.EmailExists(ctx, email)
			if lookupErr != nil {
				return nil, fmt.Errorf("email lookup failed: %w", lookupErr)
			}
			if existingUser == nil {
				return nil, fmt.Errorf("email exists but user not found")
			}
			// Return the existing account that was created by a prior
			// challenge request with the same deterministic anon email.
			// The key identity binding will happen in HandleCheckout.
			return existingUser, nil
		}
		return nil, fmt.Errorf("create account failed: %w", err)
	}

	// Note: account is created unverified and without a key identity binding.
	// The x402 handler verifies payment and signature in HandleCheckout, then
	// binds the key identity and marks the account as verified.
	return user, nil
}

// bindKeyIdentity binds a wallet address to a user account. Called from
// HandleCheckout after the EIP-712 signature has been verified, proving
// the caller controls the wallet's private key.
func (h *Handler) bindKeyIdentity(ctx context.Context, userID uint, wallet string) error {
	exists, _, err := h.userService.KeyIdentityExists(ctx, keyIdentityType, wallet)
	if err != nil {
		return fmt.Errorf("key identity lookup failed: %w", err)
	}
	if exists {
		return nil // already bound
	}
	if err := h.userService.AddKeyIdentity(ctx, userID, keyIdentityType, wallet, nil); err != nil {
		// A concurrent callback may have inserted the same key identity.
		// Re-check existence; if it's there now, the race is harmless.
		exists2, _, err2 := h.userService.KeyIdentityExists(ctx, keyIdentityType, wallet)
		if err2 == nil && exists2 {
			return nil
		}
		return fmt.Errorf("add key identity failed: %w", err)
	}
	return nil
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
	if payload == nil || payload.Payload.Authorization == nil {
		return ""
	}
	return payload.Payload.Authorization.Nonce
}

func generateNonce() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "0x" + hex.EncodeToString(b), nil
}

// parseChainID extracts the numeric chain ID from a CAIP-2 network string
// (e.g. "eip155:84532" -> 84532).
func parseChainID(network string) (int64, error) {
	const prefix = "eip155:"
	if !strings.HasPrefix(network, prefix) {
		return 0, fmt.Errorf("network must be eip155:<id>, got %q", network)
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(network, prefix), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid chain ID in network %q: %w", network, err)
	}
	return id, nil
}


// validateAcceptedMatchesChallenge compares the submitted Accepted payment
// requirements against those stored when the challenge was issued. This
// prevents a client from substituting a different asset, payTo, network,
// or token EIP-712 domain params than what the server challenged with.
func (h *Handler) validateAcceptedMatchesChallenge(challengeAcceptsJSON string, submitted pluginCore.X402PaymentRequirements) error {
	if challengeAcceptsJSON == "" {
		// Challenge was issued before this feature was added — allow through
		// for backward compatibility.
		return nil
	}

	var challengeReqs []pluginCore.X402PaymentRequirements
	if err := json.Unmarshal([]byte(challengeAcceptsJSON), &challengeReqs); err != nil {
		return fmt.Errorf("failed to parse challenge accepts: %w", err)
	}

	// Find a matching challenge entry by network+asset.
	for _, ch := range challengeReqs {
		if !strings.EqualFold(ch.Network, submitted.Network) {
			continue
		}
		if !strings.EqualFold(ch.Asset, submitted.Asset) {
			continue
		}
		// Network + Asset match — now verify the critical fields.
		if !strings.EqualFold(ch.PayTo, submitted.PayTo) {
			return fmt.Errorf("payTo mismatch: expected %s, got %s", ch.PayTo, submitted.PayTo)
		}
		if ch.Amount != submitted.Amount {
			return fmt.Errorf("amount mismatch: expected %s, got %s", ch.Amount, submitted.Amount)
		}
		// Verify token EIP-712 domain name and version.
		chName, _ := ch.Extra["name"].(string)
		subName, _ := submitted.Extra["name"].(string)
		if chName != subName {
			return fmt.Errorf("token name mismatch: expected %s, got %s", chName, subName)
		}
		chVersion, _ := ch.Extra["version"].(string)
		subVersion, _ := submitted.Extra["version"].(string)
		if chVersion != subVersion {
			return fmt.Errorf("token version mismatch: expected %s, got %s", chVersion, subVersion)
		}
		return nil
	}

	return fmt.Errorf("no matching challenge for network %s and asset %s", submitted.Network, submitted.Asset)
}
