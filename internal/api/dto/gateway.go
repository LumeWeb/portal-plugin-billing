package dto

import (
	"go.lumeweb.com/httputil"

	pluginCore "go.lumeweb.com/portal-plugin-billing/core"
)

var (
	_ httputil.DTOResponse[*GatewayPublicInfo]  = (*GatewayPublicInfo)(nil)
	_ httputil.DTOResponse[*GatewayListResponse] = (*GatewayListResponse)(nil)
)

// GatewayAbilities represents the checkout-related capabilities of a gateway
type GatewayAbilities struct {
	Checkout       bool `json:"checkout"`        // Gateway provides checkout UI fragments
	SessionStatus  bool `json:"session_status"`  // Gateway supports polling session status
	CustomerPortal bool `json:"customer_portal"` // Gateway provides hosted customer portal
}

// FromModel converts core PublicAbilities to GatewayAbilities
func (g *GatewayAbilities) FromModel(abilities pluginCore.PublicAbilities) {
	g.Checkout = abilities.Checkout
	g.SessionStatus = abilities.SessionStatus
	g.CustomerPortal = abilities.CustomerPortal
}

// GatewayPublicInfo represents public-safe gateway metadata
type GatewayPublicInfo struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	LogoURL     string           `json:"logo_url"`
	IsActive    bool             `json:"is_active"`
	Abilities   GatewayAbilities `json:"abilities"`
}

// FromModel implements the DTOResponse interface for responses without a core model
func (g *GatewayPublicInfo) FromModel(info *GatewayPublicInfo) error {
	if info == nil {
		return nil
	}
	*g = *info
	return nil
}

// GatewayListResponse represents a list of gateways
type GatewayListResponse []GatewayPublicInfo

// FromModel implements the DTOResponse interface for responses without a core model
func (g *GatewayListResponse) FromModel(response *GatewayListResponse) error {
	if response == nil {
		return nil
	}
	*g = *response
	return nil
}
