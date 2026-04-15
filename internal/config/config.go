package config

import (
	z "github.com/Oudwins/zog"
	"go.lumeweb.com/portal/config"
)

var _ config.ServiceConfig = (*ServiceConfig)(nil)

type StripeConfig struct {
	WebhookSecret string `config:"webhook_secret"`
	PublishableKey  string `config:"publishable_key"`
	SecretKey      string `config:"secret_key"`
	TestMode       bool   `config:"test_mode"`
}

type AtlosConfig struct {
	MerchantID string `config:"merchant_id"`
	APIKey     string `config:"api_key"`
}

func (s StripeConfig) Schema() z.ZogSchema {
	return z.Struct(z.Shape{
		"WebhookSecret": z.String().Required(),
		"PublishableKey": z.String().Required(),
		"SecretKey":     z.String().Required(),
		"TestMode":      z.Bool(),
	})
}

func (s StripeConfig) Defaults() map[string]any {
	return map[string]any{
		"WebhookSecret": "",
		"PublishableKey": "",
		"SecretKey":     "",
		"TestMode":      false,
	}
}

func (s AtlosConfig) Schema() z.ZogSchema {
	return z.Struct(z.Shape{
		"MerchantID": z.String().Required(),
		"APIKey":     z.String().Required(),
	})
}

func (s AtlosConfig) Defaults() map[string]any {
	return map[string]any{
		"MerchantID": "",
		"APIKey":     "",
	}
}

type ServiceConfig struct {
	Stripe StripeConfig `config:"stripe"`
	Atlos  AtlosConfig  `config:"atlos"`
}

func (s ServiceConfig) Schema() z.ZogSchema {
	return z.Struct(z.Shape{})
}

func (s ServiceConfig) Defaults() map[string]any {
	return map[string]any{}
}
