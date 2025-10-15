package config

import (
	"github.com/Oudwins/zog"
	"go.lumeweb.com/portal/config"
)

var _ config.ServiceConfig = (*ServiceConfig)(nil)

type ServiceConfig struct {
}

func (s ServiceConfig) Schema() zog.ZogSchema {
	return nil // Return appropriate schema when adding config fields
}

func (s ServiceConfig) Defaults() map[string]any {
	return map[string]any{
		// Add default values for config fields here
	}
}
