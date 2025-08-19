package es

// Config Elasticsearch配置
type Config struct {
	Addresses []string `json:"addresses"`
	Username  string   `json:"username"`
	Password  string   `json:"password"`
	APIKey    string   `json:"api_key"`
}
