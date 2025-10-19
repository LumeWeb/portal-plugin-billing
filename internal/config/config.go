package config

import (
	z "github.com/Oudwins/zog"
	"go.lumeweb.com/portal/config"
)

var _ config.ServiceConfig = (*ServiceConfig)(nil)

type StripeConfig struct {
	WebhookSecret    string `config:"webhook_secret"`
	PublishableKey   string `config:"publishable_key"`
	PricingTableID   string `config:"pricing_table_id"`
}

func (s StripeConfig) Schema() z.ZogSchema {
	return z.Struct(z.Shape{
		"WebhookSecret":  z.String().Required(),
		"PublishableKey": z.String().Required(),
		"PricingTableID": z.String().Required(),
	})
}

func (s StripeConfig) Defaults() map[string]any {
	return map[string]any{
		"WebhookSecret":  "",
		"PublishableKey": "",
		"PricingTableID": "",
	}
}

type ServiceConfig struct {
	Stripe StripeConfig `config:"stripe"`
}

func (s ServiceConfig) Schema() z.ZogSchema {
	return z.Struct(z.Shape{})
}

func (s ServiceConfig) Defaults() map[string]any {
	return map[string]any{}
}
