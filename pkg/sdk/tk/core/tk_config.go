package core

type TkConfig struct {
	AppId           string
	AppSecret       string
	HttpReadTimeout int64
	OpenRequestUrl  string

	Headers map[string]string
}

func NewTkConfig() *TkConfig {
	config := &TkConfig{
		HttpReadTimeout: 10000, //默认10s超时
		OpenRequestUrl:  "https://new-test.tongkask.com",
	}
	return config
}

var GlobalConfig *TkConfig = NewTkConfig()
