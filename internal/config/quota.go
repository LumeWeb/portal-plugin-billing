package config

import "go.lumeweb.com/portal/config"

var _ config.Defaults = (*QuotaConfig)(nil)

type QuotaConfig struct {
	Enabled bool `config:"enabled"`
}

func (c QuotaConfig) Defaults() map[string]any {
	return map[string]any{
		"enabled": false,
	}
}
