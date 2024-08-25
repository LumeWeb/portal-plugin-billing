package config

import (
	"errors"
	"go.lumeweb.com/portal/config"
)

var _ config.Defaults = (*BillingConfig)(nil)
var _ config.Validator = (*BillingConfig)(nil)

type BillingConfig struct {
	Enabled     bool              `mapstructure:"enabled"`
	KillBill    KillBillConfig    `mapstructure:"kill_bill"`
	Hyperswitch HyperswitchConfig `mapstructure:"hyperswitch"`
}

func (c BillingConfig) Defaults() map[string]any {
	return map[string]any{
		"enabled": false,
	}
}

func (c BillingConfig) Validate() error {
	if !c.Enabled {
		return nil
	}

	if c.KillBill.APIServer == "" {
		return errors.New("billing.kill_bill.api_server is required")
	}

	if c.KillBill.Username == "" {
		return errors.New("billing.kill_bill.username is required")
	}

	if c.KillBill.Password == "" {
		return errors.New("billing.kill_bill.password is required")
	}

	if c.KillBill.APIKey == "" {
		return errors.New("billing.kill_bill.api_key is required")
	}

	if c.KillBill.APISecret == "" {
		return errors.New("billing.kill_bill.api_secret is required")
	}

	if c.Hyperswitch.APIServer == "" {
		return errors.New("billing.hyperswitch.api_server is required")
	}

	if c.Hyperswitch.APIKey == "" {
		return errors.New("billing.hyperswitch.api_key is required")
	}

	if c.Hyperswitch.PublishableKey == "" {
		return errors.New("billing.hyperswitch.publishable_key is required")
	}

	return nil
}
