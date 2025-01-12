package config

import (
	"errors"
	"go.lumeweb.com/portal/config"
	"regexp"
	"strings"
)

var _ config.Defaults = (*BillingConfig)(nil)
var _ config.Validator = (*BillingConfig)(nil)

type BillingConfig struct {
	Enabled          bool              `config:"enabled"`
	PaidPlansEnabled bool              `config:"paid_plans_enabled"`
	KillBill         KillBillConfig    `config:"kill_bill"`
	Hyperswitch      HyperswitchConfig `config:"hyperswitch"`
	FreePlanName     string            `config:"free_plan_name"`
	FreePlanID       string            `config:"free_plan_id"`
	FreeStorage      uint64            `config:"free_storage"`
	FreeUpload       uint64            `config:"free_upload"`
	FreeDownload     uint64            `config:"free_download"`
}

func (c BillingConfig) Defaults() map[string]any {

	freePlanName := c.FreePlanName

	if freePlanName == "" {
		freePlanName = "Free"
	}

	return map[string]any{
		"enabled":            false,
		"paid_plans_enabled": false,
		"free_plan_name":     freePlanName,
		"free_plan_id":       slugify(freePlanName),
		"free_storage":       0,
		"free_upload":        0,
		"free_download":      0,
	}
}

func (c BillingConfig) Validate() error {
	if !c.Enabled {
		return nil
	}

	if c.PaidPlansEnabled {
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
	}

	return nil
}

func slugify(name string) string {
	// Convert to lowercase
	name = strings.ToLower(name)

	// Replace spaces with hyphens
	name = strings.ReplaceAll(name, " ", "-")

	// Remove any character that is not a letter, number, or hyphen
	reg := regexp.MustCompile("[^a-z0-9-]")
	name = reg.ReplaceAllString(name, "")

	// Remove leading and trailing hyphens
	name = strings.Trim(name, "-")

	// Collapse multiple consecutive hyphens into a single hyphen
	reg = regexp.MustCompile("-+")
	name = reg.ReplaceAllString(name, "-")

	return name
}
