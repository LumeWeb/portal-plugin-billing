package billing

import (
	"context"
	"fmt"

	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
)

// GetCheckoutUI validates and returns checkout UI fragments for a plan
// This orchestrates validation and delegates to the gateway for UI fragments
func (s *BillingServiceDefault) GetCheckoutUI(
	ctx context.Context,
	userID uint,
	planID uint,
	gatewayType string,
) (*pluginCore.CheckoutUIResponse, error) {
	// 1. Validate subscription state
	sub, err := s.GetActiveSubscription(ctx, userID)
	if err == nil && sub != nil {
		return nil, ErrUserAlreadySubscribed
	}

	// 2. Validate plan availability
	plan, err := s.pricingService.GetPricingPlan(ctx, planID)
	if err != nil {
		return nil, fmt.Errorf("plan not found: %w", err)
	}
	if !plan.IsActive {
		return nil, fmt.Errorf("plan is not active")
	}
	if !plan.IsPublic {
		return nil, fmt.Errorf("plan is not publicly available")
	}

	// 3. Resolve gateway type
	if gatewayType == "" {
		gatewayType = "stripe" // default for now - could be configurable
	}

	// 4. Get gateway
	gateway, err := s.GetGateway(ctx, gatewayType)
	if err != nil {
		return nil, fmt.Errorf("failed to get payment gateway: %w", err)
	}

	// 5. Get checkout UI from gateway
	checkoutProvider, err := pluginCore.AsCheckoutProvider(gateway)
	if err != nil {
		CheckoutUIErrors.WithLabelValues(gatewayType, "interface_not_implemented").Inc()
		return nil, fmt.Errorf("gateway %s does not implement CheckoutProvider", gatewayType)
	}
	return checkoutProvider.GetCheckoutUI(ctx, userID, planID)
}

// Predefined checkout errors
var (
	ErrUserAlreadySubscribed = fmt.Errorf("user already has an active subscription")
)
