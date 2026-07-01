package gows

import "time"

// clientConfig 客户端通用配置，在 Upgrade→Client 间直接传递。
// 嵌入 upgradeOptions，替代 clientOptions，消除二阶段 Option 转换的中间层。
type clientConfig struct {
	writeChSize  int                    // 写入通道缓冲区容量（默认 1024）
	readChSize   int                    // 读取通道缓冲区容量（默认 1024）
	readTimeout  time.Duration          // msgFromWsToCh 读取超时时间（0=不限制）
	writeTimeout time.Duration          // msgFromChToWs 写入超时时间（0=默认 10s）
	readLimit    int64                  // 单条消息读取大小限制（0=不限制）
	writeLimit   int64                  // 单条消息写入大小限制（0=不限制）
	dispatcher   *DistributedDispatcher // 关联的分发中心（nil=未注册）
}

// defaultClientConfig 返回客户端通用配置默认值。
func defaultClientConfig() *clientConfig {
	return &clientConfig{
		writeChSize: 1024,
		readChSize:  1024,
	}
}

// ClientOption 客户端配置选项函数类型。
// 采用 Functional Options 模式，支持可扩展的配置传递，
// 新增配置项无需修改 NewClient 的函数签名。
type ClientOption func(*clientConfig)

// applyClientOptions 将 ClientOption 列表应用到 clientConfig 并返回。
func applyClientOptions(opts ...ClientOption) *clientConfig {
	o := defaultClientConfig()
	for _, opt := range opts {
		opt(o)
	}
	if o.writeTimeout <= 0 {
		o.writeTimeout = writeDeadline
	}
	return o
}
