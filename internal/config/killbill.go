package config

type KillBillConfig struct {
	APIServer string `config:"api_server"`
	Username  string `config:"username"`
	Password  string `config:"password"`
	APIKey    string `config:"api_key"`
	APISecret string `config:"api_secret"`
}
