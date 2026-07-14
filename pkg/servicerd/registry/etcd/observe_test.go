package etcd

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/18721889353/sunshine/pkg/etcdcli"
	"github.com/18721889353/sunshine/pkg/servicerd/registry"
)

func TestObserve_KeepForOneMinute(t *testing.T) {
	cli, err := etcdcli.NewClient(
		[]string{"43.143.78.234:2379"},
		etcdcli.WithDialTimeout(5*time.Second),
		etcdcli.WithAuth("root", "sunshine"),
	)
	if err != nil {
		t.Fatalf("连接 etcd 失败: %v", err)
	}
	defer cli.Close()

	svcName := "my-service"
	instID := fmt.Sprintf("instance-%d", time.Now().UnixNano())
	instance := registry.NewServiceInstance(
		instID,
		svcName,
		[]string{"grpc://127.0.0.1:8282"},
		registry.WithVersion("v1.0.0"),
		registry.WithMetadata(map[string]string{"env": "test", "desc": "观察用实例"}),
	)
	key := fmt.Sprintf("/test_sunshine/%s/%s", svcName, instID)

	r := New(cli,
		WithNamespace("/test_sunshine"),
		WithRegisterTTL(30*time.Second),
		WithCheckInterval(1),
	)
	defer r.Close()

	err = r.Register(context.Background(), instance)
	if err != nil {
		t.Fatalf("注册失败: %v", err)
	}

	fmt.Printf("\n========== 数据已写入 etcd ==========\n")
	fmt.Printf("Key   : %s\n", key)
	fmt.Printf("Value : 请刷新 Etcd Workbench 查看\n")
	fmt.Printf("服务名 : %s\n", svcName)
	fmt.Printf("实例ID : %s\n", instID)
	fmt.Printf("提示   : 数据将保持 120 秒，请尽情查看\n")
	fmt.Printf("提示   : 也可以试试删除 key，观察自动恢复\n")
	fmt.Printf("======================================\n\n")

	// 保持 120 秒，供用户观察
	time.Sleep(120 * time.Second)

	fmt.Println("时间到，正在注销...")
	err = r.Deregister(context.Background(), instance)
	if err != nil {
		t.Logf("注销失败: %v", err)
	}
	fmt.Println("数据已清理完毕")
}
