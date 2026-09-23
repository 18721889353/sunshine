package nacoscli_test

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/18721889353/sunshine/pkg/nacoscli"
)

// 以下示例需要真实 Nacos 服务，未设置 NACOS_ADDR 时 godoc 仅展示代码，不实际执行。

// ExampleGetConfig 演示一次性拉取配置（便捷函数）：创建客户端 -> 拉取 -> 关闭。
// 适用于应用启动时读取配置，读完即关闭连接的场景。
func ExampleGetConfig() {
	// 一次性拉取配置（创建客户端 -> 拉取 -> 关闭）
	format, data, err := nacoscli.GetConfig(&nacoscli.Params{
		IPAddr:      "192.168.3.37",
		Port:        8848,
		NamespaceID: "de7b176e-91cd-49a3-ac83-beb725979775",
		Group:       "dev",
		DataID:      "user-srv.yml",
		Format:      "yaml",
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("format: %s, bytes: %d\n", format, len(data))
}

// ExampleNewConfigClient 演示复用配置客户端多次获取配置。
// 适用于同一服务需要频繁读取多个 Nacos 配置文件的场景。
func ExampleNewConfigClient() {
	// 复用配置客户端多次获取配置
	client, err := nacoscli.NewConfigClient(
		nacoscli.WithIPAddr("192.168.3.37"),
		nacoscli.WithPort(8848),
		nacoscli.WithNamespaceID("de7b176e-91cd-49a3-ac83-beb725979775"),
		nacoscli.WithAuth("admin", "password"),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	format, data, err := client.GetConfig(context.Background(), &nacoscli.Params{
		Group:  "dev",
		DataID: "app.yml",
		Format: "yaml",
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("format: %s, bytes: %d\n", format, len(data))
}

// ExampleWatchConfig 演示监听配置变更。
// 启动后台 goroutine 监听，配置变更时触发 handler，调用 stop() 优雅停止。
func ExampleWatchConfig() {
	params := &nacoscli.Params{
		NamespaceID: "de7b176e-91cd-49a3-ac83-beb725979775",
		Group:       "dev",
		DataID:      "user-srv.yml",
		Format:      "yaml",
	}

	handler := func(_, _, _, data string) {
		fmt.Printf("配置变更，新长度: %d\n", len(data))
	}

	// 启动后台监听，配置变更时触发 handler
	stop, err := nacoscli.WatchConfig(context.Background(), params, handler,
		nacoscli.WithIPAddr("192.168.3.37"),
		nacoscli.WithPort(8848),
		nacoscli.WithMaxRetries(5),
		nacoscli.WithCreateDelay(3*time.Second),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer stop()

	// 监听运行中... 直到进程退出或调用 stop()
	select {}
}
