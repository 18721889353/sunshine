package metrics_test

import (
	"testing"

	// 副作用 import：触发各包 init() 里的 MustRegister，让全仓 Collector 都进入统一中心。
	_ "github.com/18721889353/sunshine/pkg/es"
	_ "github.com/18721889353/sunshine/pkg/etcdcli"
	_ "github.com/18721889353/sunshine/pkg/goMq/gorabbitmq"
	_ "github.com/18721889353/sunshine/pkg/goemail"
	_ "github.com/18721889353/sunshine/pkg/gogroutine"
	_ "github.com/18721889353/sunshine/pkg/goredis"
	_ "github.com/18721889353/sunshine/pkg/gosms"

	pkgmetrics "github.com/18721889353/sunshine/pkg/metrics"
)

// TestSmoke_AllCollectorsGather 生产前冒烟：全仓 Collector 一起 Gather 不冲突。
//
// 为什么重要：prometheus 的 duplicate metrics / desc 冲突只在 Gather 时暴露
// （MustRegister 阶段不报错），是上线后 /metrics 返回 500 的高发原因。
// 用外部测试包才能副作用 import 所有业务包（包内测试会形成 import cycle）。
func TestSmoke_AllCollectorsGather(t *testing.T) {
	families, err := pkgmetrics.Default().GatherDefault()
	if err != nil {
		t.Fatalf("全仓 Collector Gather 失败（生产 /metrics 会 500）: %v", err)
	}
	if len(families) == 0 {
		t.Fatal("Gather 应至少返回一组指标")
	}

	// 每个 family 必须有 name，且 metric 的 label 数一致（Prometheus 会自行校验，
	// 这里额外确认没有空 name 的脏指标）
	for _, f := range families {
		if f.GetName() == "" {
			t.Error("存在无名指标 family")
		}
	}
}
