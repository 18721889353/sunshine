package core

import (
	"sync"
	"time"
)

// 使用 sync.Once 确保单例只初始化一次
var (
	globalConfig *TkConfig
	once         sync.Once
)

type TkConfig struct {
	AppId           string
	AppSecret       string
	HttpReadTimeout int64
	OpenRequestUrl  string

	Headers map[string]string
	// 添加HTTP客户端配置字段
	InsecureSkipVerify    bool          // 是否跳过SSL证书验证，true表示跳过验证（不安全），false表示验证证书
	DisableKeepAlives     bool          // 是否禁用HTTP Keep-Alive连接复用，true表示禁用，false表示启用连接复用
	MaxIdleCons           int           // HTTP客户端连接池中最大空闲连接数
	MaxIdleConsPerHost    int           // 每个目标主机的最大空闲连接数
	IdleConnTimeout       time.Duration // 空闲连接的超时时间，超过该时间未使用的连接将被关闭
	ResponseHeaderTimeout time.Duration // 从请求发送到接收到响应头的超时时间
	DialTimeout           time.Duration // 建立TCP连接的超时时间
	DialKeepAlive         time.Duration // TCP连接的Keep-Alive探测时间间隔

	SignFunc func(params map[string]any, appSecret string) string
}
type TkOption func(*TkConfig)

func NewTkConfig(opts ...TkOption) *TkConfig {
	config := &TkConfig{
		HttpReadTimeout: 10000, //默认10s超时
		OpenRequestUrl:  "https://new-test.tongkask.com",
	}
	// 应用选项
	for _, opt := range opts {
		opt(config)
	}

	// 确保全局配置被正确设置
	once.Do(func() {
		if globalConfig == nil {
			globalConfig = config
		}
	})

	return config
}

// WithAppId 设置AppId
func WithAppId(appId string) TkOption {
	return func(config *TkConfig) {
		config.AppId = appId
	}
}

// WithAppSecret 设置AppSecret
func WithAppSecret(appSecret string) TkOption {
	return func(config *TkConfig) {
		config.AppSecret = appSecret
	}
}

// WithHttpReadTimeout 设置HTTP读取超时时间
func WithHttpReadTimeout(timeout int64) TkOption {
	return func(config *TkConfig) {
		if timeout <= 0 {
			config.HttpReadTimeout = 10000 //默认10s超时
		} else {
			config.HttpReadTimeout = timeout
		}
	}
}

// WithOpenRequestUrl 设置请求URL
func WithOpenRequestUrl(url string) TkOption {
	return func(config *TkConfig) {
		config.OpenRequestUrl = url
	}
}

// WithHeaders 设置请求头
func WithHeaders(headers map[string]string) TkOption {
	return func(config *TkConfig) {
		config.Headers = headers
	}
}

// WithInsecureSkipVerify 设置是否跳过证书验证
func WithInsecureSkipVerify(insecureSkipVerify bool) TkOption {
	return func(config *TkConfig) {
		config.InsecureSkipVerify = insecureSkipVerify
	}
}

// WithDisableKeepAlives 设置是否禁用Keep-Alive
func WithDisableKeepAlives(disableKeepAlives bool) TkOption {
	return func(config *TkConfig) {
		config.DisableKeepAlives = disableKeepAlives
	}
}

// WithMaxIdleCons 设置最大空闲连接数
func WithMaxIdleCons(maxIdleCons int) TkOption {
	return func(config *TkConfig) {
		if maxIdleCons <= 0 {
			config.MaxIdleCons = 1000
		} else {
			config.MaxIdleCons = maxIdleCons
		}
	}
}

// WithMaxIdleConsPerHost 设置每个主机的最大空闲连接数
func WithMaxIdleConsPerHost(maxIdleConsPerHost int) TkOption {
	return func(config *TkConfig) {
		if maxIdleConsPerHost <= 0 {
			config.MaxIdleConsPerHost = 1000
		} else {
			config.MaxIdleConsPerHost = maxIdleConsPerHost
		}
	}
}

// WithIdleConnTimeout 设置空闲连接超时时间
func WithIdleConnTimeout(idleConnTimeout time.Duration) TkOption {
	return func(config *TkConfig) {
		if idleConnTimeout <= 0 {
			config.IdleConnTimeout = 30 * time.Second
		} else {
			config.IdleConnTimeout = idleConnTimeout
		}
	}
}

// WithResponseHeaderTimeout 设置响应头超时时间
func WithResponseHeaderTimeout(responseHeaderTimeout time.Duration) TkOption {
	return func(config *TkConfig) {
		if responseHeaderTimeout <= 0 {
			config.ResponseHeaderTimeout = 5 * time.Second
		} else {
			config.ResponseHeaderTimeout = responseHeaderTimeout
		}

	}
}

// WithDialTimeout 设置连接超时时间
func WithDialTimeout(dialTimeout time.Duration) TkOption {
	return func(config *TkConfig) {
		if dialTimeout <= 0 {
			config.DialTimeout = 10 * time.Second
		} else {
			config.DialTimeout = dialTimeout
		}

	}
}

// WithDialKeepAlive 设置连接保持时间
func WithDialKeepAlive(dialKeepAlive time.Duration) TkOption {
	return func(config *TkConfig) {
		if dialKeepAlive <= 0 {
			config.DialKeepAlive = 30 * time.Second
		} else {
			config.DialKeepAlive = dialKeepAlive
		}

	}
}

func WithSignFunc(funcName func(params map[string]any, appSecret string) string) TkOption {
	return func(config *TkConfig) {
		config.SignFunc = funcName
	}
}

// GetTkConfig 获取全局配置实例（线程安全的单例模式）
func GetTkConfig() *TkConfig {
	once.Do(func() {
		if globalConfig == nil {
			globalConfig = NewTkConfig()
		}
	})
	return globalConfig
}
