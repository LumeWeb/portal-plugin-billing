package config

type HyperswitchConfig struct {
	PublishableKey string `config:"publishable_key"`
	WebhookSecret  string `config:"webhook_secret"`
}
