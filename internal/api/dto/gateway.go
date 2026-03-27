package dto

// GatewayPublicInfo represents public-safe gateway metadata
type GatewayPublicInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	LogoURL     string `json:"logo_url"`
	IsActive    bool   `json:"is_active"`
}
