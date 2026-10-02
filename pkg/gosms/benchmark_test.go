package gosms

import (
	"errors"
	"testing"

	dysmsapi "github.com/alibabacloud-go/dysmsapi-20170525/v4/client"
	"github.com/alibabacloud-go/tea/tea"
	tencentSms "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/sms/v20210111"
)

// 本文件为性能回归基线，基准均为无网络 RTT 的纯函数——校验/格式化/脱敏（入口热路径）
// 与响应解析/批量聚合（每次请求必经的后处理），量的是 gosms 自身的处理开销；
// benchtime 固定 100000x，便于跨轮次横向对比（ns/op 波动大时以 allocs/op 为准）。

// BenchmarkValidatePhoneNumber 基准手机号格式校验。
func BenchmarkValidatePhoneNumber(b *testing.B) {
	b.ReportAllocs()
	benchmarkPhone := "+8613711112222"
	for i := 0; i < b.N; i++ {
		isValidPhoneNumber(benchmarkPhone)
	}
}

// BenchmarkFormatPhoneNumber 基准手机号国际格式化。
func BenchmarkFormatPhoneNumber(b *testing.B) {
	b.ReportAllocs()
	benchmarkPhone := "13711112222"
	for i := 0; i < b.N; i++ {
		FormatPhoneNumber(benchmarkPhone)
	}
}

// BenchmarkMaskPhone 基准手机号脱敏。
func BenchmarkMaskPhone(b *testing.B) {
	b.ReportAllocs()
	benchmarkPhone := "+8613711112222"
	for i := 0; i < b.N; i++ {
		maskPhone(benchmarkPhone)
	}
}

// BenchmarkParseSendSmsStatus 基准腾讯发送响应解析（多号码路径：
// 逐号码构造子结果 + Extra 拷贝 + 状态聚合，单请求最重的解析分支）。
func BenchmarkParseSendSmsStatus(b *testing.B) {
	b.ReportAllocs()
	fee := uint64(1)
	code := "Ok"
	resp := &tencentSms.SendSmsResponse{
		Response: &tencentSms.SendSmsResponseParams{
			SendStatusSet: []*tencentSms.SendStatus{
				{SerialNo: strPtr("sn-1"), PhoneNumber: strPtr("+8613711112222"), Code: &code, Fee: &fee},
				{SerialNo: strPtr("sn-2"), PhoneNumber: strPtr("+8613711112223"), Code: &code, Fee: &fee},
				{SerialNo: strPtr("sn-3"), PhoneNumber: strPtr("+8613711112224"), Code: &code, Fee: &fee},
			},
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = parseSendSmsStatus(resp)
	}
}

// BenchmarkParsePullSendStatus 基准腾讯下发状态列表解析（查询大列表的热路径）。
func BenchmarkParsePullSendStatus(b *testing.B) {
	b.ReportAllocs()
	receiveTime := uint64(1700000000)
	set := []*tencentSms.PullSmsSendStatus{
		{SerialNo: strPtr("sn-1"), PhoneNumber: strPtr("+8613711112222"), ReportStatus: strPtr("SUCCESS"), UserReceiveTime: &receiveTime},
		{SerialNo: strPtr("sn-2"), PhoneNumber: strPtr("+8613711112223"), ReportStatus: strPtr("FAIL"), Description: strPtr("用户关机")},
		{SerialNo: strPtr("sn-3"), PhoneNumber: strPtr("+8613711112224")},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = parsePullSendStatus(set)
	}
}

// BenchmarkParseSendDetailItems 基准阿里发送明细 DTO 解析（查询大列表的热路径）。
func BenchmarkParseSendDetailItems(b *testing.B) {
	b.ReportAllocs()
	status := int64(3)
	details := []*dysmsapi.QuerySendDetailsResponseBodySmsSendDetailDTOsSmsSendDetailDTO{
		{OutId: strPtr("out-1"), PhoneNum: strPtr("13711112222"), SendStatus: &status, ErrCode: tea.String("DELIVERED"), ReceiveDate: tea.String("2026-10-01 12:00:00")},
		{OutId: strPtr("out-2"), PhoneNum: strPtr("13711112223"), SendStatus: &status, ErrCode: tea.String("DELIVERED"), ReceiveDate: tea.String("2026-10-01 12:00:01")},
		{OutId: strPtr("out-3"), PhoneNum: strPtr("13711112224"), SendStatus: &status, ErrCode: tea.String("DELIVERED"), ReceiveDate: tea.String("2026-10-01 12:00:02")},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = parseSendDetailItems(details)
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
