package config

type HyperswitchConfig struct {
	APIServer      string `config:"api_server"`
	APIKey         string `config:"api_key"`
	PublishableKey string `config:"publishable_key"`
}
