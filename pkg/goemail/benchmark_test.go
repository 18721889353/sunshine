package goemail

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	ses "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ses/v20201002"
)

// 本文件为性能回归基线：全部为无网络 RTT 的纯函数与本地解析——脱敏/校验/格式化
// （入口热路径）与状态列表解析/批量聚合/并发骨架（每次请求必经的后处理），
// 量的是 goemail 自身的处理开销。
//
// 口径：benchtime 固定 100000x（README「性能基线」表已增列标注该口径），
// 便于跨轮次横向对比；ns/op 受机器噪声影响大，跨轮比较以 allocs/op 为准。
//
// 不纳入基线的路径：ValidateEmail/validateRequest 每调用都会写一条 Info 日志并创建
// span（可观测路径，非纯计算），放基准里输出会被日志淹没；其纯函数核心 isValidEmail
// 已单独列为基准。

// BenchmarkIsValidEmail 基准邮箱格式校验的纯函数核心（SendEmail 入口必经）。
func BenchmarkIsValidEmail(b *testing.B) {
	b.ReportAllocs()
	email := "zhangsan@example.com"
	for i := 0; i < b.N; i++ {
		isValidEmail(email)
	}
}

// BenchmarkMaskEmail 基准邮箱脱敏（span 属性与日志的 PII 掩码热路径）。
func BenchmarkMaskEmail(b *testing.B) {
	b.ReportAllocs()
	email := "zhangsan@example.com"
	for i := 0; i < b.N; i++ {
		maskEmail(email)
	}
}

// BenchmarkMaskSecret 基准密钥脱敏。
func BenchmarkMaskSecret(b *testing.B) {
	b.ReportAllocs()
	secret := "super-secret-value-123"
	for i := 0; i < b.N; i++ {
		maskSecret(secret)
	}
}

// BenchmarkMaskAccessKey 基准密钥标识脱敏。
func BenchmarkMaskAccessKey(b *testing.B) {
	b.ReportAllocs()
	accessKey := "AKIDabcdefghijkl"
	for i := 0; i < b.N; i++ {
		maskAccessKey(accessKey)
	}
}

// BenchmarkConfigString 基准 Config 的 Stringer 输出（日志打印 Config 的热路径）。
func BenchmarkConfigString(b *testing.B) {
	b.ReportAllocs()
	cfg := Config{
		ProviderType: ProviderTypeSMTP,
		Region:       "ap-guangzhou",
		AccessKeyID:  "AKIDtest1234567890",
		SecretKey:    "super-secret-value",
		SMTPHost:     "smtp.example.com",
		SMTPPort:     465,
		SMTPUsername: "user@example.com",
		SMTPPassword: "smtp-password-123",
		UseTLS:       true,
	}
	for i := 0; i < b.N; i++ {
		_ = cfg.String()
	}
}

// BenchmarkParseEmailStatusList 基准腾讯状态列表解析（查询热路径：
// 逐条 nil 安全取值 + 状态码映射 + Extra 组装，单查询最重的解析分支）。
func BenchmarkParseEmailStatusList(b *testing.B) {
	b.ReportAllocs()
	client, err := newTencentSESClient(tencentTestConfig())
	if err != nil {
		b.Fatalf("创建腾讯云 SES 客户端失败: %v", err)
	}
	list := []*ses.SendEmailStatus{
		{
			MessageId:        common.StringPtr("msg-1"),
			ToEmailAddress:   common.StringPtr("to1@example.com"),
			FromEmailAddress: common.StringPtr("from@example.com"),
			SendStatus:       common.Int64Ptr(0),
			DeliverStatus:    common.Int64Ptr(1),
			DeliverMessage:   common.StringPtr("delivered"),
			RequestTime:      common.Int64Ptr(1700000000),
			UserOpened:       common.BoolPtr(true),
		},
		{
			MessageId:      common.StringPtr("msg-2"),
			ToEmailAddress: common.StringPtr("to2@example.com"),
			SendStatus:     common.Int64Ptr(1006),
		},
		{}, // 字段全缺的服务端形态：nil 安全分支也要计入开销
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = client.parseEmailStatusList(list)
	}
}

// BenchmarkBatchErrors 基准批量失败聚合（并发批量收尾必经，
// 含 fmt.Errorf 格式化与 errors.Join 拼接的分配）。
func BenchmarkBatchErrors(b *testing.B) {
	b.ReportAllocs()
	bizErr := errors.New("云返回错误码")
	results := make([]*SendResult, 10)
	for i := range results {
		if i%3 == 0 {
			results[i] = &SendResult{Status: StatusFailed, Error: bizErr}
		} else {
			results[i] = &SendResult{Status: StatusSuccess}
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = batchErrors(results)
	}
}

// BenchmarkRunBatchEmail 基准批量并发骨架的调度开销（send 回调无网络 RTT，
// 量的是 jobs 灌注 + worker 调度 + 保序回填 + 聚合本身）。
func BenchmarkRunBatchEmail(b *testing.B) {
	b.ReportAllocs()
	reqs := make([]*SendRequest, 10)
	for i := range reqs {
		reqs[i] = &SendRequest{Subject: fmt.Sprintf("req-%d", i)}
	}
	send := func(_ context.Context, r *SendRequest) (*SendResult, error) {
		return &SendResult{Status: StatusSuccess, MessageID: r.Subject}, nil
	}
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = runBatchEmail(ctx, reqs, 0, send)
	}
}
