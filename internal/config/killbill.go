package config

type KillBillConfig struct {
	APIServer string `mapstructure:"api_server"`
	Username  string `mapstructure:"username"`
	Password  string `mapstructure:"password"`
	APIKey    string `mapstructure:"api_key"`
	APISecret string `mapstructure:"api_secret"`
}
