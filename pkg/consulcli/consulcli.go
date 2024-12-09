// Package consulcli 是连接到 Consul 服务的客户端包。
package consulcli

import (
	"fmt"

	"github.com/hashicorp/consul/api"
)

// Init 初始化连接到 Consul 服务
// 参数：
// - addr: Consul 服务器的地址，如果设置了 WithConfig(*api.Config) 参数，则此参数会被忽略。
// - opts: 可选参数，用于设置连接的其他选项。
// 返回值：
// - *api.Client: 连接到 Consul 的客户端实例。
// - error: 如果初始化过程中发生错误，则返回相应的错误信息。
// 注意：
// - 如果设置了 WithConfig(*api.Config) 参数，则会忽略 addr 参数！
func Init(addr string, opts ...Option) (*api.Client, error) {
	// 获取默认选项
	o := defaultOptions()
	// 应用传入的选项
	o.apply(opts...)

	// 如果配置不为空，则直接使用配置创建客户端
	if o.config != nil {
		return api.NewClient(o.config)
	}

	// 检查地址是否为空
	if addr == "" {
		return nil, fmt.Errorf("consul 地址不能为空")
	}

	// 使用提供的地址和其他选项创建客户端
	return api.NewClient(&api.Config{
		Address:    addr,         // Consul 服务器的地址
		Scheme:     o.scheme,     // 协议方案（如 "http" 或 "https"）
		WaitTime:   o.waitTime,   // 阻塞查询的最大等待时间
		Datacenter: o.datacenter, // 数据中心名称
	})
}
