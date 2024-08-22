package config

type KillBillConfig struct {
	APIServer string `json:"api_server"`
	APIKey    string `json:"api_key"`
	APISecret string `json:"api_secret"`

	StorageUsageUnitName  string `json:"storage_usage_unit_name"`
	DownloadUsageUnitName string `json:"download_usage_unit_name"`
	UploadUsageUnitName   string `json:"upload_usage_unit_name"`
}
