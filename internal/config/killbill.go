package config

type KillBillConfig struct {
	APIServer string `mapstructure:"api_server"`
	APIKey    string `mapstructure:"api_key"`
	APISecret string `mapstructure:"api_secret"`
}
