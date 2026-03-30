package atlos

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/shopspring/decimal"
	"go.lumeweb.com/atlos-sdk"
	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
	"go.lumeweb.com/portal-plugin-billing/internal/gateway"
	quotaCore "go.lumeweb.com/portal-plugin-quota/core"
	core "go.lumeweb.com/portal/core"
	"go.lumeweb.com/portal/db/models"
	"go.uber.org/zap"
)

//go:embed templates/*.tpl
var templatesFS embed.FS

//go:embed assets/*.svg
var gatewayLogoFiles embed.FS

const (
	GatewayID = "atlos"
)

// Setup creates and configures an ATLOS gateway if merchant ID and API key are configured.
// Returns a log message (empty if not configured), the gateway instance (nil if not configured), and an error.
func Setup(opts pluginCore.GatewaySetupOptions, apiSecret string, merchantID string) (string, pluginCore.PaymentGateway, error) {
	if merchantID == "" {
		return "", nil, nil
	}

	if apiSecret == "" {
		return "", nil, nil
	}

	gw := New(opts.Logger, apiSecret, merchantID, opts.HTTP, opts.Quota, opts.User, opts.BillingSvc, opts.PricingSvc, opts.CreditSvc)
	return fmt.Sprintf("ATLOS gateway registered successfully (merchant_id=%s)", merchantID), gw, nil
}

// AtlosGateway implements the PaymentGateway interface for ATLOS payment widget
type AtlosGateway struct {
	logger     *core.Logger
	apiSecret  string
	merchantID string
	http       core.HTTPService
	quota      quotaCore.QuotaService
	users      core.UserService
	billing    pluginCore.BillingService
	pricing    pluginCore.PricingService
	credit     pluginCore.CreditService
}

// New creates a new AtlosGateway instance
func New(
	logger *core.Logger,
	apiSecret string,
	merchantID string,
	http core.HTTPService,
	quota quotaCore.QuotaService,
	users core.UserService,
	billing pluginCore.BillingService,
	pricing pluginCore.PricingService,
	credit pluginCore.CreditService,
) *AtlosGateway {
	return &AtlosGateway{
		logger:     logger,
		apiSecret:  apiSecret,
		merchantID: merchantID,
		http:       http,
		quota:      quota,
		users:      users,
		billing:    billing,
		pricing:    pricing,
		credit:     credit,
	}
}

// SetQuota sets the quota service
func (g *AtlosGateway) SetQuota(quota quotaCore.QuotaService) {
	g.quota = quota
}

// ID returns the gateway identifier
func (g *AtlosGateway) ID(ctx context.Context) string {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.ID")
	defer span.End()

	return GatewayID
}

// SignatureHeader returns the signature header name for webhook verification
func (g *AtlosGateway) SignatureHeader(ctx context.Context) string {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.SignatureHeader")
	defer span.End()

	return atlos.ApiSecretHeader
}

// ExtractEventID extracts the event ID from a webhook payload
func (g *AtlosGateway) ExtractEventID(ctx context.Context, payload []byte) (string, error) {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.ExtractEventID")
	defer span.End()

	var notification atlos.PostbackNotification
	if err := json.Unmarshal(payload, &notification); err != nil {
		return "", fmt.Errorf("failed to parse postback notification: %w", err)
	}

	if notification.TransactionId == "" {
		return "", fmt.Errorf("empty transaction ID in postback notification")
	}

	return notification.TransactionId, nil
}

// ExtractEventType extracts the event type from a webhook payload
func (g *AtlosGateway) ExtractEventType(ctx context.Context, payload []byte) (string, error) {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.ExtractEventType")
	defer span.End()

	var notification atlos.PostbackNotification
	if err := json.Unmarshal(payload, &notification); err != nil {
		return "", fmt.Errorf("failed to parse postback notification: %w", err)
	}

	// ATLOS postback occurs only on successful payments (Status == 100)
	// The SDK's Validate() method enforces this, so we always return a payment confirmed event
	return "payment.confirmed", nil
}

// GetCustomerPortalURL returns the customer portal URL
func (g *AtlosGateway) GetCustomerPortalURL(ctx context.Context, userID uint, returnUrl string) (string, error) {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.GetCustomerPortalURL")
	defer span.End()

	// ATLOS uses widget-based checkout, customer portal may not be applicable
	return "", fmt.Errorf("customer portal not supported by ATLOS widget")
}

// CreateOrUpdateSubscriber creates or updates a subscriber record
func (g *AtlosGateway) CreateOrUpdateSubscriber(ctx context.Context, userID uint, externalID string, subscriptionID string, isActive bool, planID *uint) error {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.CreateOrUpdateSubscriber")
	defer span.End()

	// Delegate to billing service
	return g.billing.CreateOrUpdateSubscriber(ctx, userID, g.ID(ctx), externalID, subscriptionID, isActive, planID)
}

// DeactivateSubscriber deactivates a subscriber
func (g *AtlosGateway) DeactivateSubscriber(ctx context.Context, userID uint, gatewayType string) error {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.DeactivateSubscriber")
	defer span.End()

	// Delegate to billing service
	return g.billing.DeactivateSubscriber(ctx, userID, g.ID(ctx))
}

// ExecuteCancel cancels the subscription via Atlos SDK and deactivates the subscriber locally.
// This implements the SubscriptionExecutor interface for API-based cancellation.
func (g *AtlosGateway) ExecuteCancel(ctx context.Context, userID uint) error {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.ExecuteCancel")
	defer span.End()

	// Get active subscriber to retrieve the subscription ID
	subscriber, err := g.billing.GetActiveSubscription(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to get active subscription: %w", err)
	}
	if subscriber == nil || subscriber.GatewayType != GatewayID {
		return fmt.Errorf("no active Atlas subscription found for user %d", userID)
	}

	// Create Atlos SDK client
	client, err := atlos.NewClient(g.apiSecret)
	if err != nil {
		return fmt.Errorf("failed to create Atlos client: %w", err)
	}

	// Call Atlos cancel API with the subscription ID
	req := atlos.CancelPostRequest{
		SubscriptionId: &subscriber.SubscriptionID,
	}
	if err := client.Cancel(ctx, req); err != nil {
		return fmt.Errorf("failed to cancel subscription in Atlos: %w", err)
	}

	g.logger.Debug("subscription cancelled in Atlos",
		zap.Uint("user_id", userID),
		zap.String("subscription_id", subscriber.SubscriptionID),
	)

	// Deactivate subscriber locally
	if err := g.DeactivateSubscriber(ctx, userID, GatewayID); err != nil {
		return fmt.Errorf("failed to deactivate subscriber: %w", err)
	}

	return nil
}

// ValidateWebhook validates a webhook signature
func (g *AtlosGateway) ValidateWebhook(ctx context.Context, signature string, payload []byte) error {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.ValidateWebhook")
	defer span.End()

	if signature == "" {
		return fmt.Errorf("missing signature header")
	}

	var notification atlos.PostbackNotification
	if err := json.Unmarshal(payload, &notification); err != nil {
		return fmt.Errorf("failed to parse postback notification: %w", err)
	}

	valid, err := notification.VerifySignature(g.apiSecret, signature)
	if err != nil {
		return fmt.Errorf("signature verification failed: %w", err)
	}
	if !valid {
		return fmt.Errorf("invalid signature")
	}

	return nil
}

// HandleWebhook handles incoming webhook events
func (g *AtlosGateway) HandleWebhook(ctx context.Context, payload []byte) error {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.HandleWebhook")
	defer span.End()

	var notification atlos.PostbackNotification
	if err := json.Unmarshal(payload, &notification); err != nil {
		return fmt.Errorf("failed to parse postback notification: %w", err)
	}

	// Validate the notification structure
	if err := notification.Validate(); err != nil {
		return fmt.Errorf("postback notification validation failed: %w", err)
	}

	// Parse OrderId to extract userID and planID (format: "userID-planID")
	userID, planID, err := parseOrderID(notification.OrderId)
	if err != nil {
		return fmt.Errorf("failed to parse order ID: %w", err)
	}

	// Validate the plan exists
	plan, err := g.pricing.GetPricingPlan(ctx, planID)
	if err != nil {
		return fmt.Errorf("failed to get pricing plan: %w", err)
	}
	if plan == nil {
		return fmt.Errorf("pricing plan not found")
	}
	if !plan.IsActive {
		return fmt.Errorf("plan is not active")
	}

	// TransactionId is the external account identifier
	// SubscriptionId is the subscription object ID for cancellation
	externalID := notification.TransactionId
	subscriptionID := notification.SubscriptionId

	// Create or update subscriber
	if err := g.billing.CreateOrUpdateSubscriber(ctx, userID, g.ID(ctx), externalID, subscriptionID, true, &planID); err != nil {
		return fmt.Errorf("failed to create or update subscriber: %w", err)
	}

	// Issue payment credit (if credit service available)
	if g.credit != nil && notification.Amount > 0 {
		g.logger.Debug("atlos payment has amount - credit integration available",
			zap.Uint("user_id", userID),
			zap.String("transaction_id", notification.TransactionId),
			zap.Float64("amount", notification.Amount))

		// Convert amount to decimal
		amount := decimal.NewFromFloat(notification.Amount)

		// Issue credit with idempotency to prevent duplicate credits from webhook retries
		if err := g.credit.IssueCreditWithIdempotency(
			ctx,
			uint64(userID),
			pluginCore.CreditTypeCharge,
			amount,
			pluginCore.ReferenceTypeAtlosPayment,
			notification.TransactionId,
			"ATLOS payment completed",
			0, // createdBy: 0 for system
		); err != nil {
			return fmt.Errorf("failed to issue ATLOS payment credit: %w", err)
		}

		g.logger.Info("ATLOS payment credit issued successfully",
			zap.Uint("user_id", userID),
			zap.String("transaction_id", notification.TransactionId),
			zap.String("amount", amount.String()))
	}

	g.logger.Debug("ATLOS payment webhook processed successfully",
		zap.Uint("user_id", userID),
		zap.Uint("plan_id", planID),
		zap.String("transaction_id", notification.TransactionId),
		zap.String("order_id", notification.OrderId),
		zap.Float64("amount", notification.Amount),
		zap.String("asset", notification.Asset),
		zap.String("blockchain", notification.Blockchain),
	)

	return nil
}

// GetName returns the display name for the gateway
func (g *AtlosGateway) GetName(ctx context.Context) string {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.GetName")
	defer span.End()

	return "ATLOS"
}

// GetDescription returns the description for the gateway
func (g *AtlosGateway) GetDescription(ctx context.Context) string {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.GetDescription")
	defer span.End()

	return "Accept crypto payments using the ATLOS payment widget"
}

// GetLogo returns the logo image data for this gateway
func (g *AtlosGateway) GetLogo(ctx context.Context) ([]byte, error) {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.GetLogo")
	defer span.End()

	return gateway.ReadGatewayLogo(GatewayID, gatewayLogoFiles, nil)
}

// GetCheckoutUI returns UI fragments for ATLOS checkout flows
// Returns script and button fragments that load the ATLOS widget and initialize it
func (g *AtlosGateway) GetCheckoutUI(ctx context.Context, userID uint, planID uint) (*pluginCore.CheckoutUIResponse, error) {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.GetCheckoutUI")
	defer span.End()

	return core.MetricTrackResult(
		nil,
		CheckoutUIDisplayed.WithLabelValues(LabelStatusError),
		func() (*pluginCore.CheckoutUIResponse, error) {
			// 1. Validate services are available
			if err := g.validateServices(); err != nil {
				return nil, err
			}

			// 2. Get plan details and validate
			plan, err := g.pricing.GetPricingPlan(ctx, planID)
			if err != nil {
				return nil, fmt.Errorf("failed to get pricing plan: %w", err)
			}
			if plan == nil {
				return nil, fmt.Errorf("pricing plan not found")
			}
			if !plan.IsActive {
				return nil, fmt.Errorf("plan is not active")
			}

			// Check if plan has monthly price
			if plan.MonthlyPriceUSD == nil {
				return nil, fmt.Errorf("plan does not have a monthly price configured")
			}

			// 3. Get user details
			user, err := g.getUser(ctx, userID)
			if err != nil {
				return nil, fmt.Errorf("failed to get user: %w", err)
			}

			// 4. Generate unique order ID
			orderID := fmt.Sprintf("%d-plan%d", userID, planID)

			// 5. Build response with script and button fragments
			scriptFragment, err := g.buildScriptFragment()
			if err != nil {
				return nil, fmt.Errorf("failed to build script fragment: %w", err)
			}

			userName := fmt.Sprintf("%s %s", user.FirstName, user.LastName)
			buttonFragment, err := g.buildButtonFragment(orderID, *plan.MonthlyPriceUSD, plan.Currency, userName, user.Email)
			if err != nil {
				return nil, fmt.Errorf("failed to build button fragment: %w", err)
			}

			response := &pluginCore.CheckoutUIResponse{
				SessionID: orderID,
				ExpiresAt: time.Now().Add(1 * time.Hour),
				Fragments: []pluginCore.CheckoutUIFragment{
					scriptFragment,
					buttonFragment,
				},
			}

			g.logger.Debug("ATLOS checkout UI fragments created",
				zap.Uint("user_id", userID),
				zap.Uint("plan_id", planID),
				zap.String("order_id", orderID),
			)

			return response, nil
		},
	)
}

// GetCustomerPortalMetadata returns metadata for ATLOS customer portal
func (g *AtlosGateway) GetCustomerPortalMetadata(ctx context.Context, userID uint) (map[string]interface{}, error) {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.GetCustomerPortalMetadata")
	defer span.End()

	return map[string]any{}, nil
}

// SupportsProductSync returns false - ATLOS does not require product sync
func (g *AtlosGateway) SupportsProductSync() bool {
	return false
}

// SyncPlan synchronizes a pricing plan with ATLOS (not supported)
// ATLOS uses widget-based checkout with inline configuration
func (g *AtlosGateway) SyncPlan(ctx context.Context, plan *pluginCore.PricingPlanInfo) (*pluginCore.SyncResult, error) {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.SyncPlan")
	defer span.End()

	return &pluginCore.SyncResult{
		Success: false,
		Error:   fmt.Errorf("ATLOS does not require product synchronization"),
	}, nil
}

// validateServices validates that required services are available
func (g *AtlosGateway) validateServices() error {
	if g.users == nil {
		return fmt.Errorf("user service not configured")
	}
	if g.quota == nil {
		return fmt.Errorf("quota service not configured")
	}
	return nil
}

// getUser retrieves and validates a user exists
func (g *AtlosGateway) getUser(ctx context.Context, userID uint) (*models.User, error) {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.getUser")
	defer span.End()

	exists, user, err := g.users.AccountExists(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to check user existence: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("user with ID %d not found", userID)
	}
	return user, nil
}

// buildScriptFragment creates a script fragment that loads the ATLOS JavaScript SDK
func (g *AtlosGateway) buildScriptFragment() (pluginCore.CheckoutUIFragment, error) {
	return pluginCore.CheckoutUIFragment{
		Type:   pluginCore.FragmentTypeScript,
		Script: `<script async src="https://atlos.io/packages/app/atlos.js"></script>`,
	}, nil
}

// buildButtonFragment creates a button fragment that initializes and triggers the ATLOS payment widget
func (g *AtlosGateway) buildButtonFragment(orderID string, amount float64, currency string, userName string, userEmail string) (pluginCore.CheckoutUIFragment, error) {
	// Generate unique button ID
	buttonID := fmt.Sprintf("atlos-pay-btn-%s", orderID)

	// Use FuncMap with printf "%q" for proper string quoting
	tmpl, err := template.New("atlosPaymentConfig").Funcs(template.FuncMap{
		"quote": func(s string) string {
			return fmt.Sprintf("%q", s)
		},
	}).ParseFS(templatesFS, "templates/payment_button.tpl")
	if err != nil {
		return pluginCore.CheckoutUIFragment{}, fmt.Errorf("failed to parse template: %w", err)
	}

	data := struct {
		ButtonID    string
		MerchantID  string
		OrderID     string
		Amount      float64
		Currency    string
		UserName    string
		UserEmail   string
		PostbackURL string
	}{
		ButtonID:    buttonID,
		MerchantID:  g.getMerchantID(),
		OrderID:     orderID,
		Amount:      amount,
		Currency:    currency,
		UserName:    userName,
		UserEmail:   userEmail,
		PostbackURL: g.getPostbackURL(),
	}

	var scriptBuf strings.Builder
	if err := tmpl.Execute(&scriptBuf, data); err != nil {
		return pluginCore.CheckoutUIFragment{}, fmt.Errorf("failed to execute template: %w", err)
	}

	// Build button HTML with unique ID and script with event listener
	buttonHTML := fmt.Sprintf(`<button id="%s">Pay with Crypto</button>`, buttonID)
	scriptHTML := fmt.Sprintf(`<script>%s</script>`, scriptBuf.String())

	return pluginCore.CheckoutUIFragment{
		Type:   pluginCore.FragmentTypeButton,
		HTML:   buttonHTML,
		Script: scriptHTML,
	}, nil
}

// getMerchantID retrieves the ATLOS merchant ID from configuration
func (g *AtlosGateway) getMerchantID() string {
	return g.merchantID
}

// getPostbackURL returns the postback URL for payment notifications
// Uses the HTTP service to build full URL with account subdomain and protocol
func (g *AtlosGateway) getPostbackURL() string {
	if g.http == nil {
		return "/api/billing/webhook/atlos"
	}

	subdomain := g.http.APISubdomain("account", true)
	u, err := url.Parse(subdomain)
	if err != nil {
		return "/api/billing/webhook/atlos"
	}
	u.Path = "/api/billing/webhook/atlos"
	return u.String()
}

// parseOrderID parses an order ID in the format "userID-planID" to extract user and plan IDs
// GetManagementInfo returns management capabilities for operations
func (g *AtlosGateway) GetManagementInfo(ctx context.Context, userID uint) (*pluginCore.ManagementCapabilities, error) {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.GetManagementInfo")
	defer span.End()

	// Atlas supports only API-based operations
	operations := map[pluginCore.ManagementOperation]bool{
		pluginCore.OperationCancel:     true,
		pluginCore.OperationChangePlan: false, // Coming soon
	}

	return &pluginCore.ManagementCapabilities{
		ManagementMode: pluginCore.ModeAPI,
		Operations:     operations,
	}, nil
}

// GetManagementURL returns the appropriate action for a management operation
func (g *AtlosGateway) GetManagementURL(ctx context.Context, userID uint, operation pluginCore.ManagementOperation) (*pluginCore.ManagementResult, error) {
	ctx, span := core.TraceMethod(ctx, "AtlosGateway.GetManagementURL")
	defer span.End()

	// Check if user has an active Atlas subscription
	subscriber, err := g.billing.GetActiveSubscription(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get active subscription: %w", err)
	}
	if subscriber == nil || subscriber.GatewayType != GatewayID {
		return nil, fmt.Errorf("no active Atlas subscription found for user %d", userID)
	}

	switch operation {
	case pluginCore.OperationCancel:
		endpoint := pluginCore.GetManagementAPIEndpoint(pluginCore.OperationCancel)
		if endpoint == nil {
			return &pluginCore.ManagementResult{
				Action:       pluginCore.ActionError,
				ErrorMessage: "Operation not configured with a predefined endpoint",
			}, nil
		}
		return &pluginCore.ManagementResult{
			Action:      pluginCore.ActionAPIRequired,
			APIEndpoint: endpoint,
		}, nil

	case pluginCore.OperationChangePlan:
		// Not implemented yet
		return &pluginCore.ManagementResult{
			Action:       pluginCore.ActionUnsupported,
			ErrorMessage: "Plan changes are not yet supported for ATLOS subscriptions",
		}, nil

	default:
		return &pluginCore.ManagementResult{
			Action:       pluginCore.ActionUnsupported,
			ErrorMessage: fmt.Sprintf("operation %s is not supported by ATLOS", operation),
		}, nil
	}
}

func parseOrderID(orderID string) (uint, uint, error) {
	parts := strings.Split(orderID, "-")
	if len(parts) != 2 || !strings.HasPrefix(parts[1], "plan") {
		return 0, 0, fmt.Errorf("invalid order ID format: expected 'userID-planID', got: %s", orderID)
	}

	userID, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid user ID in order ID: %w", err)
	}

	planID, err := strconv.ParseUint(strings.TrimPrefix(parts[1], "plan"), 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid plan ID in order ID: %w", err)
	}

	return uint(userID), uint(planID), nil
}
