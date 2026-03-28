package dto

import "go.lumeweb.com/httputil"

var (
	_ httputil.DTOResponse[*GatewayPublicInfo]  = (*GatewayPublicInfo)(nil)
	_ httputil.DTOResponse[*GatewayListResponse] = (*GatewayListResponse)(nil)
)

// GatewayPublicInfo represents public-safe gateway metadata
type GatewayPublicInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	LogoURL     string `json:"logo_url"`
	IsActive    bool   `json:"is_active"`
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
