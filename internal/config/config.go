package config

import (
	z "github.com/Oudwins/zog"
	"go.lumeweb.com/portal/config"
)

var _ config.ServiceConfig = (*ServiceConfig)(nil)

type StripeConfig struct {
	WebhookSecret string `config:"webhook_secret"`
}

type ServiceConfig struct {
	Stripe StripeConfig `config:"stripe"`
}

func (s ServiceConfig) Schema() z.ZogSchema {
	return z.Struct(z.Shape{
		"stripe": z.Struct(z.Shape{
			"WebhookSecret": z.String().Required(),
		}),
	})
}

func (s ServiceConfig) Defaults() map[string]any {
	return map[string]any{
		"stripe": map[string]any{
			"WebhookSecret": "",
		},
	}
}
