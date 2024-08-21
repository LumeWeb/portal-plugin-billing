package config

import "go.lumeweb.com/portal/config"

var _ config.Defaults = (*BillingConfig)(nil)
var _ config.Defaults = (*QuotaConfig)(nil)

type BillingConfig struct {
	Enabled bool `json:"enabled"`
}
type QuotaConfig struct {
	Enabled bool `json:"enabled"`
}

func (c BillingConfig) Defaults() map[string]any {
	return map[string]any{
		"enabled": false,
	}
}

func (c QuotaConfig) Defaults() map[string]any {
	return map[string]any{
		"enabled": false,
	}
}
