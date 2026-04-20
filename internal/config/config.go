package config

import (
	z "github.com/Oudwins/zog"
	"go.lumeweb.com/portal/config"
	"go.lumeweb.com/portal-plugin-billing/pkg/subscription"
)

var _ config.ServiceConfig = (*ServiceConfig)(nil)

type StripeConfig struct {
	WebhookSecret   string `config:"webhook_secret"`
	PublishableKey  string `config:"publishable_key"`
	SecretKey       string `config:"secret_key"`
	TestMode        bool   `config:"test_mode"`
}

type AtlosConfig struct {
	MerchantID string `config:"merchant_id"`
	APIKey     string `config:"api_key"`
	Endpoint   string `config:"endpoint"`
}

func (s StripeConfig) Schema() z.ZogSchema {
	return z.Struct(z.Shape{
		"WebhookSecret":  z.String().Required(),
		"PublishableKey": z.String().Required(),
		"SecretKey":      z.String().Required(),
		"TestMode":       z.Bool(),
	})
}

func (s StripeConfig) Defaults() map[string]any {
	return map[string]any{
		"WebhookSecret":  "",
		"PublishableKey": "",
		"SecretKey":      "",
		"TestMode":       false,
	}
}

func (s AtlosConfig) Schema() z.ZogSchema {
	return z.Struct(z.Shape{
		"MerchantID": z.String().Required(),
		"APIKey":     z.String().Required(),
		"Endpoint":   z.String().Optional(),
	})
}

func (s AtlosConfig) Defaults() map[string]any {
	return map[string]any{
		"MerchantID": "",
		"APIKey":     "",
		"Endpoint":   "",
	}
}

type ServiceConfig struct {
	Stripe              StripeConfig `config:"stripe"`
	Atlos               AtlosConfig  `config:"atlos"`
	DefaultPriceCadence string       `config:"default_price_cadence"`
}

func (s ServiceConfig) Schema() z.ZogSchema {
	return z.Struct(z.Shape{
		"DefaultPriceCadence": z.String().Optional().OneOf([]string{
			string(subscription.CadenceDaily),
			string(subscription.CadenceWeekly),
			string(subscription.CadenceMonthly),
			string(subscription.CadenceQuarterly),
			string(subscription.CadenceYearly),
			string(subscription.CadenceRolling),
			"", // empty is valid (will use default)
		}),
	})
}

func (s ServiceConfig) Defaults() map[string]any {
	return map[string]any{
		"DefaultPriceCadence": string(subscription.CadenceMonthly),
	}
}
