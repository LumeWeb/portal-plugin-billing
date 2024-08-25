package config

type HyperswitchConfig struct {
	APIServer string `mapstructure:"api_server"`
	APIKey    string `mapstructure:"api_key"`
}
