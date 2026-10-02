package gosms

import (
	"context"
	"strings"
	"testing"

	dysmsapi "github.com/alibabacloud-go/dysmsapi-20170525/v4/client"
	"github.com/alibabacloud-go/tea/tea"
)

// TestNewAliyunSMSClient 验证阿里云客户端创建的必填校验。
func TestNewAliyunSMSClient(t *testing.T) {
	t.Parallel()

	t.Run("缺少密钥返回错误", func(t *testing.T) {
		t.Parallel()
		client, err := newAliyunSMSClient(&Config{ProviderType: ProviderTypeAliyunSMS})
		if err == nil {
			t.Fatal("缺少 AccessKeyID/SecretKey 应返回错误")
		}
		if client != nil {
			t.Errorf("出错时客户端应为 nil, 实际 %v", client)
		}
		if !strings.Contains(err.Error(), "AccessKeyID 与 SecretKey 不能为空") {
			t.Errorf("错误消息应为中文且说明原因, 实际 %q", err.Error())
		}
	})

	t.Run("创建成功", func(t *testing.T) {
		t.Parallel()
		client, err := newAliyunSMSClient(aliyunTestConfig())
		if err != nil {
			t.Fatalf("创建客户端失败: %v", err)
		}
		if client.client == nil {
			t.Error("SDK 客户端不应为 nil")
		}
	})

	t.Run("持有Config副本且不修改调用方", func(t *testing.T) {
		t.Parallel()
		// 与腾讯侧一致的副本语义：调用方后续修改 *Config 不影响已创建客户端
		cfg := aliyunTestConfig()
		client, err := newAliyunSMSClient(cfg)
		if err != nil {
			t.Fatalf("创建客户端失败: %v", err)
		}
		cfg.AliyunSignName = "changed-after-create"
		if client.config.AliyunSignName == "changed-after-create" {
			t.Error("调用方后续修改不应传导到已创建客户端（应持有 Config 副本）")
		}
		if client.config == cfg {
			t.Error("客户端应持 Config 副本而非共享同一指针")
		}
	})
}

// TestNewAliyunRuntime 验证请求超时选项已按设计设置（阿里 SDK 无 ctx 透传的兜底）。
func TestNewAliyunRuntime(t *testing.T) {
	t.Parallel()
	runtime := newAliyunRuntime()
	if runtime.ConnectTimeout == nil || *runtime.ConnectTimeout != aliyunConnectTimeoutMs {
		t.Errorf("ConnectTimeout 应为 %d ms, 实际 %v", aliyunConnectTimeoutMs, runtime.ConnectTimeout)
	}
	if runtime.ReadTimeout == nil || *runtime.ReadTimeout != aliyunReadTimeoutMs {
		t.Errorf("ReadTimeout 应为 %d ms, 实际 %v", aliyunReadTimeoutMs, runtime.ReadTimeout)
	}
}

// TestAliyunSendSMSEntryValidation 验证发送入口在发起网络调用前拦截全部非法入参。
func TestAliyunSendSMSEntryValidation(t *testing.T) {
	t.Parallel()
	client := mustAliyunClient(t)
	ctx := context.Background()

	tests := []struct {
		name       string
		req        *SendRequest
		wantSubStr string
	}{
		{"nil请求", nil, "发送请求不能为空"},
		{"空号码列表", func() *SendRequest {
			r := validSendRequest()
			r.PhoneNumbers = nil
			return r
		}(), "手机号列表不能为空"},
		{"缺模板ID", func() *SendRequest {
			r := validSendRequest()
			r.TemplateID = ""
			return r
		}(), "模板ID不能为空"},
		{"非法号码", func() *SendRequest {
			r := validSendRequest()
			r.PhoneNumbers = []string{"+86137abc2222"}
			return r
		}(), "手机号格式错误"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := client.SendSMS(ctx, tt.req)
			if err == nil {
				t.Fatal("非法入参应返回错误（且在发起网络调用前拦截）")
			}
			if !strings.Contains(err.Error(), tt.wantSubStr) {
				t.Errorf("错误消息应包含 %q, 实际 %q", tt.wantSubStr, err.Error())
			}
			if result == nil {
				t.Fatal("失败时必须返回非 nil result（既有语义）")
			}
			if result.Status != StatusFailed {
				t.Errorf("状态应为 %s, 实际 %s", StatusFailed, result.Status)
			}
		})
	}
}

// TestAliyunSendSMSMultiPhoneRejected 验证多号码 fail loud（守护「静默只发第一个」的修复，
// 引导调用方使用 SendBatchSMS）。
func TestAliyunSendSMSMultiPhoneRejected(t *testing.T) {
	t.Parallel()
	client := mustAliyunClient(t)

	req := validSendRequest()
	req.PhoneNumbers = []string{"+8613711112222", "+8613711112223", "+8613711112224"}

	result, err := client.SendSMS(context.Background(), req)
	if err == nil {
		t.Fatal("阿里云多号码应返回错误（旧实现静默只发第一个）")
	}
	if !strings.Contains(err.Error(), "SendBatchSMS") {
		t.Errorf("错误消息应指引使用 SendBatchSMS, 实际 %q", err.Error())
	}
	if result == nil || result.Status != StatusFailed {
		t.Errorf("失败时应返回 StatusFailed 的非 nil result, 实际 %+v", result)
	}
}

// TestParseSendSmsResult 验证发送响应解析的结构异常与业务失败分支。
func TestParseSendSmsResult(t *testing.T) {
	t.Parallel()

	t.Run("nil响应返回错误", func(t *testing.T) {
		t.Parallel()
		if _, err := parseSendSmsResult(nil); err == nil {
			t.Fatal("nil 响应应返回错误")
		}
	})

	t.Run("响应体为空返回错误", func(t *testing.T) {
		t.Parallel()
		// 旧实现 Resp.Body 为 nil 时 result.Status 留空字符串（既非 success 也非 failed）
		if _, err := parseSendSmsResult(&dysmsapi.SendSmsResponse{}); err == nil {
			t.Fatal("空响应体应返回错误")
		}
	})

	t.Run("发送成功", func(t *testing.T) {
		t.Parallel()
		resp := &dysmsapi.SendSmsResponse{
			Body: &dysmsapi.SendSmsResponseBody{
				Code:    tea.String("OK"),
				Message: tea.String("OK"),
				BizId:   tea.String("biz-123"),
			},
		}
		result, err := parseSendSmsResult(resp)
		if err != nil {
			t.Fatalf("成功响应不应返回 error: %v", err)
		}
		if result.Status != StatusSuccess {
			t.Errorf("状态应为 %s, 实际 %s", StatusSuccess, result.Status)
		}
		if result.MessageID != "biz-123" {
			t.Errorf("MessageID 应为 biz-123, 实际 %q", result.MessageID)
		}
		if got := result.Extra["Code"]; got != "OK" {
			t.Errorf("Extra[Code] 应为 OK, 实际 %v", got)
		}
	})

	t.Run("业务失败保持err为nil", func(t *testing.T) {
		t.Parallel()
		resp := &dysmsapi.SendSmsResponse{
			Body: &dysmsapi.SendSmsResponseBody{
				Code:    tea.String("isv.MOBILE_NUMBER_ILLEGAL"),
				Message: tea.String("illegal mobile number"),
			},
		}
		result, err := parseSendSmsResult(resp)
		if err != nil {
			t.Fatalf("业务失败应保持 err==nil（调用方以 result.Status 判断）, 实际 %v", err)
		}
		if result.Status != StatusFailed {
			t.Errorf("状态应为 %s, 实际 %s", StatusFailed, result.Status)
		}
		if result.Error == nil {
			t.Fatal("业务失败时 result.Error 应非 nil")
		}
		if !strings.Contains(result.Error.Error(), "isv.MOBILE_NUMBER_ILLEGAL") {
			t.Errorf("错误应包含错误码, 实际 %q", result.Error.Error())
		}
	})
}

// TestParseQuerySendDetails 验证查询响应解析：
// 结构异常/查询失败 fail loud（守护「查询失败被当作 0 条记录静默吞掉」的修复）。
func TestParseQuerySendDetails(t *testing.T) {
	t.Parallel()

	t.Run("nil响应返回错误", func(t *testing.T) {
		t.Parallel()
		if _, _, err := parseQuerySendDetails(nil); err == nil {
			t.Fatal("nil 响应应返回错误")
		}
	})

	t.Run("响应体为空返回错误", func(t *testing.T) {
		t.Parallel()
		if _, _, err := parseQuerySendDetails(&dysmsapi.QuerySendDetailsResponse{}); err == nil {
			t.Fatal("空响应体应返回错误")
		}
	})

	t.Run("查询失败返回错误而非空列表", func(t *testing.T) {
		t.Parallel()
		resp := &dysmsapi.QuerySendDetailsResponse{
			Body: &dysmsapi.QuerySendDetailsResponseBody{
				Code:    tea.String("isv.MOBILE_NUMBER_ILLEGAL"),
				Message: tea.String("illegal mobile number"),
			},
		}
		_, _, err := parseQuerySendDetails(resp)
		if err == nil {
			t.Fatal("查询失败（Code 非 OK）应返回错误——旧实现吞掉后返回空列表+成功")
		}
		if !strings.Contains(err.Error(), "isv.MOBILE_NUMBER_ILLEGAL") {
			t.Errorf("错误应包含错误码, 实际 %q", err.Error())
		}
	})

	t.Run("查询成功含总数", func(t *testing.T) {
		t.Parallel()
		resp := &dysmsapi.QuerySendDetailsResponse{
			Body: &dysmsapi.QuerySendDetailsResponseBody{
				Code:       tea.String("OK"),
				TotalCount: tea.String("2"),
				SmsSendDetailDTOs: &dysmsapi.QuerySendDetailsResponseBodySmsSendDetailDTOs{
					SmsSendDetailDTO: []*dysmsapi.QuerySendDetailsResponseBodySmsSendDetailDTOsSmsSendDetailDTO{
						{PhoneNum: tea.String("13700000000")},
						{PhoneNum: tea.String("13800000000")},
					},
				},
			},
		}
		data, extra, err := parseQuerySendDetails(resp)
		if err != nil {
			t.Fatalf("成功响应不应返回 error: %v", err)
		}
		if len(data) != 2 {
			t.Errorf("期望 2 条记录, 实际 %d 条", len(data))
		}
		if got := extra["TotalCount"]; got != "2" {
			t.Errorf("Extra[TotalCount] 应为 2, 实际 %v", got)
		}
	})

	t.Run("查询成功但零条是合法结果", func(t *testing.T) {
		t.Parallel()
		resp := &dysmsapi.QuerySendDetailsResponse{
			Body: &dysmsapi.QuerySendDetailsResponseBody{Code: tea.String("OK")},
		}
		data, _, err := parseQuerySendDetails(resp)
		if err != nil {
			t.Fatalf("零条记录是合法结果, 不应返回 error: %v", err)
		}
		if len(data) != 0 {
			t.Errorf("期望 0 条, 实际 %d 条", len(data))
		}
	})
}

// TestParseSendDetailItems 验证明细 DTO 的状态映射、时间解析与边界。
func TestParseSendDetailItems(t *testing.T) {
	t.Parallel()

	t.Run("空列表返回空", func(t *testing.T) {
		t.Parallel()
		got := parseSendDetailItems(nil)
		if len(got) != 0 {
			t.Errorf("期望 0 条, 实际 %d 条", len(got))
		}
	})

	t.Run("发送状态映射", func(t *testing.T) {
		t.Parallel()
		three, two, one := int64(3), int64(2), int64(1)
		got := parseSendDetailItems([]*dysmsapi.QuerySendDetailsResponseBodySmsSendDetailDTOsSmsSendDetailDTO{
			{SendStatus: &three},
			{SendStatus: &two},
			{SendStatus: &one},
			{SendStatus: nil},
			nil, // nil 元素应跳过
		})
		if len(got) != 4 {
			t.Fatalf("期望 4 条（nil 元素跳过）, 实际 %d 条", len(got))
		}
		if got[0].Status != StatusDelivered {
			t.Errorf("SendStatus=3 应映射为 %s, 实际 %s", StatusDelivered, got[0].Status)
		}
		if got[1].Status != StatusFailed {
			t.Errorf("SendStatus=2 应映射为 %s, 实际 %s", StatusFailed, got[1].Status)
		}
		if got[2].Status != StatusPending {
			t.Errorf("SendStatus=1 应映射为 %s, 实际 %s", StatusPending, got[2].Status)
		}
	})

	t.Run("回执码映射与时间解析", func(t *testing.T) {
		t.Parallel()
		got := parseSendDetailItems([]*dysmsapi.QuerySendDetailsResponseBodySmsSendDetailDTOsSmsSendDetailDTO{
			{
				ErrCode:     tea.String("DELIVERED"),
				ReceiveDate: tea.String("2026-10-01 12:00:00"),
				OutId:       tea.String("out-1"),
			},
			{
				ErrCode:     tea.String("UNKNOWN_CARRIER_ERROR"),
				ReceiveDate: tea.String("not-a-time"), // 非法时间串保持零值
			},
		})
		if len(got) != 2 {
			t.Fatalf("期望 2 条, 实际 %d 条", len(got))
		}
		if got[0].StatusCode != 0 || got[0].StatusMessage != "送达成功" {
			t.Errorf("DELIVERED 应映射 0/送达成功, 实际 %d/%q", got[0].StatusCode, got[0].StatusMessage)
		}
		if got[0].DeliverTime.IsZero() {
			t.Error("合法时间串应被解析")
		}
		if got[0].MessageID != "out-1" {
			t.Errorf("MessageID 应取 OutId, 实际 %q", got[0].MessageID)
		}
		if got[1].StatusCode != 1 || got[1].StatusMessage != "UNKNOWN_CARRIER_ERROR" {
			t.Errorf("非 DELIVERED 应映射 1/原码, 实际 %d/%q", got[1].StatusCode, got[1].StatusMessage)
		}
		if !got[1].DeliverTime.IsZero() {
			t.Error("非法时间串应保持零值不 panic")
		}
	})
}

// TestAliyunCurrentPage 验证页码换算（守护写死 Offset/10+1 的分页 bug）。
func TestAliyunCurrentPage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		offset uint64
		limit  uint64
		want   int64
	}{
		{"首页", 0, 10, 1},
		{"恰好翻页（Limit=20, Offset=20 → 第2页，旧实现算出第3页）", 20, 20, 2},
		{"页中偏移", 21, 20, 2},
		{"第三页", 40, 20, 3},
		{"limit为0防御返回首页", 50, 0, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := aliyunCurrentPage(tt.offset, tt.limit)
			if got != tt.want {
				t.Errorf("aliyunCurrentPage(%d, %d) = %d, 期望 %d", tt.offset, tt.limit, got, tt.want)
			}
		})
	}
}

// TestNormalizeAliyunStatusQuery 验证阿里查询归一化与 PageSize 上限截断。
func TestNormalizeAliyunStatusQuery(t *testing.T) {
	t.Parallel()

	t.Run("Limit超过50截断", func(t *testing.T) {
		t.Parallel()
		q, err := normalizeAliyunStatusQuery(&SMSStatusQuery{
			PhoneNumber: "+8613711112222",
			Limit:       80,
		})
		if err != nil {
			t.Fatalf("归一化失败: %v", err)
		}
		if q.Limit != aliyunMaxPageSize {
			t.Errorf("Limit=80 应截断为 %d, 实际 %d", aliyunMaxPageSize, q.Limit)
		}
	})

	t.Run("Limit为0归一为10", func(t *testing.T) {
		t.Parallel()
		q, err := normalizeAliyunStatusQuery(&SMSStatusQuery{PhoneNumber: "+8613711112222"})
		if err != nil {
			t.Fatalf("归一化失败: %v", err)
		}
		if q.Limit != defaultQueryLimit {
			t.Errorf("Limit=0 应归一为 %d, 实际 %d", defaultQueryLimit, q.Limit)
		}
	})

	t.Run("透传通用归一化错误", func(t *testing.T) {
		t.Parallel()
		if _, err := normalizeAliyunStatusQuery(nil); err == nil {
			t.Fatal("nil 查询应返回错误")
		}
	})
}

// TestAliyunSendBatchSMS 验证批量发送的健壮性（nil 元素不 panic）。
func TestAliyunSendBatchSMS(t *testing.T) {
	t.Parallel()
	client := mustAliyunClient(t)

	t.Run("空列表", func(t *testing.T) {
		results, err := client.SendBatchSMS(context.Background(), nil)
		if err != nil {
			t.Fatalf("空批量应返回 nil error, 实际 %v", err)
		}
		if len(results) != 0 {
			t.Errorf("空批量应返回 0 条结果, 实际 %d 条", len(results))
		}
	})

	t.Run("nil元素不panic且返回失败结果与聚合error", func(t *testing.T) {
		// 旧实现的失败日志取 req.PhoneNumbers[0]，req 为 nil/无号码时 panic
		results, err := client.SendBatchSMS(context.Background(), []*SendRequest{nil})
		// R2 语义变更：外层 error 由恒 nil 改为聚合失败（不再让「全部失败」伪装成成功）
		if err == nil {
			t.Fatal("存在失败条目时外层 error 应为聚合错误")
		}
		if !strings.Contains(err.Error(), "第 1 条") {
			t.Errorf("聚合错误应含条目序号便于定位, 实际 %q", err.Error())
		}
		if len(results) != 1 {
			t.Fatalf("单条失败不中断批量，期望 1 条结果, 实际 %d 条", len(results))
		}
		if results[0] == nil || results[0].Status != StatusFailed {
			t.Errorf("nil 请求应产出失败结果, 实际 %+v", results[0])
		}
	})
}

// TestAliyunGetSMSStatusEntryValidation 验证查询入口归一化拦截（不发网络）。
func TestAliyunGetSMSStatusEntryValidation(t *testing.T) {
	t.Parallel()
	client := mustAliyunClient(t)

	result, err := client.GetSMSStatus(context.Background(), nil)
	if err == nil {
		t.Fatal("nil 查询参数应返回错误")
	}
	if !strings.Contains(err.Error(), "查询参数不能为空") {
		t.Errorf("错误消息应为中文且说明原因, 实际 %q", err.Error())
	}
	if result == nil || result.Status != StatusFailed {
		t.Errorf("失败时应返回 StatusFailed 的非 nil result, 实际 %+v", result)
	}
}
