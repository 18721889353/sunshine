package gosms

import (
	"context"
	"strings"
	"testing"

	tencentSms "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/sms/v20210111"
)

// TestNewTencentSMSClient 验证腾讯云客户端创建的必填校验与默认值。
func TestNewTencentSMSClient(t *testing.T) {
	t.Parallel()

	t.Run("缺少密钥返回错误", func(t *testing.T) {
		t.Parallel()
		client, err := newTencentSMSClient(&Config{ProviderType: ProviderTypeTencentSMS, TencentAppID: "1400000000"})
		if err == nil {
			t.Fatal("缺少 SecretId/SecretKey 应返回错误")
		}
		if client != nil {
			t.Errorf("出错时客户端应为 nil, 实际 %v", client)
		}
		if !strings.Contains(err.Error(), "SecretId 与 SecretKey 不能为空") {
			t.Errorf("错误消息应为中文且说明原因, 实际 %q", err.Error())
		}
	})

	t.Run("缺少AppID返回错误", func(t *testing.T) {
		t.Parallel()
		cfg := tencentTestConfig()
		cfg.TencentAppID = ""
		_, err := newTencentSMSClient(cfg)
		if err == nil || !strings.Contains(err.Error(), "TencentAppID 不能为空") {
			t.Fatalf("期望缺 AppID 错误, 实际 %v", err)
		}
	})

	t.Run("Region为空默认上海", func(t *testing.T) {
		t.Parallel()
		cfg := tencentTestConfig()
		cfg.Region = ""
		client, err := newTencentSMSClient(cfg)
		if err != nil {
			t.Fatalf("创建客户端失败: %v", err)
		}
		if client.config.Region != "ap-shanghai" {
			t.Errorf("Region 应默认 ap-shanghai, 实际 %q", client.config.Region)
		}
	})

	t.Run("不修改调用方Config且后续修改互不影响", func(t *testing.T) {
		t.Parallel()
		// 旧实现直接 cfg.Region = "ap-shanghai"，复用同一 *Config 的调用方被意外写入默认值
		cfg := tencentTestConfig()
		cfg.Region = ""
		client, err := newTencentSMSClient(cfg)
		if err != nil {
			t.Fatalf("创建客户端失败: %v", err)
		}
		if cfg.Region != "" {
			t.Errorf("调用方 cfg.Region 不应被修改, 实际 %q", cfg.Region)
		}
		// 客户端持副本：调用方后续修改不影响已创建客户端
		cfg.TencentSignName = "changed-after-create"
		if client.config.TencentSignName == "changed-after-create" {
			t.Error("调用方后续修改不应传导到已创建客户端（应持有 Config 副本）")
		}
	})
}

// TestTencentSendSMSEntryValidation 验证发送入口在发起网络调用前拦截全部非法入参
// （nil 请求 / 空列表 / 缺模板 / 缺签名 / 非法号码），且失败必返回非 nil result。
func TestTencentSendSMSEntryValidation(t *testing.T) {
	t.Parallel()
	client := mustTencentClient(t)
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
		{"缺签名且无默认兜底", func() *SendRequest {
			r := validSendRequest()
			r.SignName = ""
			cfg := tencentTestConfig()
			cfg.TencentSignName = ""
			c, _ := newTencentSMSClient(cfg)
			_ = c
			return r
		}(), "签名不能为空"},
		{"非法号码", func() *SendRequest {
			r := validSendRequest()
			r.PhoneNumbers = []string{"13711112222"}
			return r
		}(), "手机号格式错误"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			useClient := client
			if tt.name == "缺签名且无默认兜底" {
				cfg := tencentTestConfig()
				cfg.TencentSignName = ""
				c, err := newTencentSMSClient(cfg)
				if err != nil {
					t.Fatalf("创建无默认签名客户端失败: %v", err)
				}
				useClient = c
			}
			result, err := useClient.SendSMS(ctx, tt.req)
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

// TestBuildTemplateParamSet 验证腾讯云位置参数严格按切片顺序构建
// （守护 map 遍历随机序导致 %1%/%2% 错位的修复）。
func TestBuildTemplateParamSet(t *testing.T) {
	t.Parallel()

	t.Run("按切片顺序输出", func(t *testing.T) {
		t.Parallel()
		params := []TemplateParam{
			{Key: "1", Value: "aaaaaa"},
			{Key: "2", Value: "bbbbbb"},
			{Key: "3", Value: "cccccc"},
		}
		got := buildTemplateParamSet(params)
		if len(got) != 3 {
			t.Fatalf("期望 3 个参数, 实际 %d", len(got))
		}
		want := []string{"aaaaaa", "bbbbbb", "cccccc"}
		for i, v := range got {
			if *v != want[i] {
				t.Errorf("第 %d 个参数期望 %q, 实际 %q（顺序错位会导致模板占位符内容错乱）", i+1, want[i], *v)
			}
		}
	})

	t.Run("空参数返回空切片", func(t *testing.T) {
		t.Parallel()
		got := buildTemplateParamSet(nil)
		if len(got) != 0 {
			t.Errorf("空参数应返回空切片, 实际 %d 个", len(got))
		}
	})
}

// TestParseSendSmsStatus 验证发送响应解析：结构异常 fail loud、业务失败保持 err==nil 语义。
func TestParseSendSmsStatus(t *testing.T) {
	t.Parallel()

	t.Run("nil响应返回错误", func(t *testing.T) {
		t.Parallel()
		if _, err := parseSendSmsStatus(nil); err == nil {
			t.Fatal("nil 响应应返回错误")
		}
	})

	t.Run("Response为空返回错误", func(t *testing.T) {
		t.Parallel()
		if _, err := parseSendSmsStatus(&tencentSms.SendSmsResponse{}); err == nil {
			t.Fatal("Response 为空应返回错误")
		}
	})

	t.Run("状态集合为空返回错误且不panic", func(t *testing.T) {
		t.Parallel()
		// 旧实现只判 != nil 未判长度，空切片时 SendStatusSet[0] 越界 panic
		resp := &tencentSms.SendSmsResponse{
			Response: &tencentSms.SendSmsResponseParams{SendStatusSet: []*tencentSms.SendStatus{}},
		}
		_, err := parseSendSmsStatus(resp)
		if err == nil {
			t.Fatal("空状态集合应返回错误（不 panic 即为修复生效）")
		}
		if !strings.Contains(err.Error(), "无发送状态记录") {
			t.Errorf("错误消息应说明无状态记录, 实际 %q", err.Error())
		}
	})

	t.Run("单条成功", func(t *testing.T) {
		t.Parallel()
		serialNo := "12345:67890"
		phone := "+8613711112222"
		code := "Ok"
		message := "send success"
		fee := uint64(1)
		resp := &tencentSms.SendSmsResponse{
			Response: &tencentSms.SendSmsResponseParams{
				SendStatusSet: []*tencentSms.SendStatus{
					{
						SerialNo:    &serialNo,
						PhoneNumber: &phone,
						Code:        &code,
						Message:     &message,
						Fee:         &fee,
					},
				},
			},
		}
		result, err := parseSendSmsStatus(resp)
		if err != nil {
			t.Fatalf("成功响应不应返回 error: %v", err)
		}
		if result.Status != StatusSuccess {
			t.Errorf("状态应为 %s, 实际 %s", StatusSuccess, result.Status)
		}
		if result.MessageID != serialNo {
			t.Errorf("MessageID 应为 %q, 实际 %q", serialNo, result.MessageID)
		}
		if result.PhoneNumber != phone {
			t.Errorf("PhoneNumber 应为 %q, 实际 %q", phone, result.PhoneNumber)
		}
		if result.Error != nil {
			t.Errorf("成功时 Error 应为 nil, 实际 %v", result.Error)
		}
		if got := result.Extra["Fee"]; got != uint64(1) {
			t.Errorf("Extra[Fee] 应为 1, 实际 %v", got)
		}
		if result.MultipleResults != nil {
			t.Errorf("单号码时 MultipleResults 应为 nil, 实际 %d 条", len(result.MultipleResults))
		}
	})

	t.Run("业务失败保持err为nil", func(t *testing.T) {
		t.Parallel()
		code := "ErrMessage"
		message := "template not exist"
		resp := &tencentSms.SendSmsResponse{
			Response: &tencentSms.SendSmsResponseParams{
				SendStatusSet: []*tencentSms.SendStatus{
					{Code: &code, Message: &message},
				},
			},
		}
		result, err := parseSendSmsStatus(resp)
		if err != nil {
			t.Fatalf("业务失败应保持 err==nil（调用方以 result.Status 判断）, 实际 %v", err)
		}
		if result.Status != StatusFailed {
			t.Errorf("状态应为 %s, 实际 %s", StatusFailed, result.Status)
		}
		if result.Error == nil {
			t.Fatal("业务失败时 result.Error 应非 nil")
		}
		if !strings.Contains(result.Error.Error(), "ErrMessage") {
			t.Errorf("错误应包含错误码, 实际 %q", result.Error.Error())
		}
	})

	t.Run("多号码全部成功返回逐号码状态", func(t *testing.T) {
		t.Parallel()
		// R2-P1-1 守护：旧实现只取 SendStatusSet[0]，第 2、3 个号码的
		// SerialNo 与状态被静默丢弃（功能缺失）
		resp := &tencentSms.SendSmsResponse{
			Response: &tencentSms.SendSmsResponseParams{
				SendStatusSet: []*tencentSms.SendStatus{
					{SerialNo: strPtr("sn-1"), PhoneNumber: strPtr("+8613711112222"), Code: strPtr("Ok")},
					{SerialNo: strPtr("sn-2"), PhoneNumber: strPtr("+8613711112223"), Code: strPtr("Ok")},
					{SerialNo: strPtr("sn-3"), PhoneNumber: strPtr("+8613711112224"), Code: strPtr("Ok")},
					nil, // nil 元素应跳过，不 panic
				},
			},
		}
		result, err := parseSendSmsStatus(resp)
		if err != nil {
			t.Fatalf("不应返回 error: %v", err)
		}
		if result.Status != StatusSuccess {
			t.Errorf("全部成功时整体状态应为 %s, 实际 %s", StatusSuccess, result.Status)
		}
		if len(result.MultipleResults) != 3 {
			t.Fatalf("MultipleResults 应含 3 条（nil 元素跳过）, 实际 %d 条", len(result.MultipleResults))
		}
		for i, want := range []string{"sn-1", "sn-2", "sn-3"} {
			if result.MultipleResults[i].MessageID != want {
				t.Errorf("第 %d 条 MessageID 应为 %q, 实际 %q（丢失号码状态）", i+1, want, result.MultipleResults[i].MessageID)
			}
			if result.MultipleResults[i].Status != StatusSuccess {
				t.Errorf("第 %d 条状态应为 %s, 实际 %s", i+1, StatusSuccess, result.MultipleResults[i].Status)
			}
		}
		if result.MessageID != "sn-1" || result.PhoneNumber != "+8613711112222" {
			t.Errorf("主结果应取首个号码, 实际 messageID=%q phone=%q", result.MessageID, result.PhoneNumber)
		}
		if result.Error != nil {
			t.Errorf("全部成功时主结果 Error 应为 nil, 实际 %v", result.Error)
		}
	})

	t.Run("多号码任一失败整体failed且保留各号码状态", func(t *testing.T) {
		t.Parallel()
		resp := &tencentSms.SendSmsResponse{
			Response: &tencentSms.SendSmsResponseParams{
				SendStatusSet: []*tencentSms.SendStatus{
					{SerialNo: strPtr("sn-1"), PhoneNumber: strPtr("+8613711112222"), Code: strPtr("Ok")},
					{SerialNo: strPtr("sn-2"), PhoneNumber: strPtr("+8613711112223"), Code: strPtr("ErrMessage"), Message: strPtr("limit")},
				},
			},
		}
		result, err := parseSendSmsStatus(resp)
		if err != nil {
			t.Fatalf("业务失败应保持 err==nil, 实际 %v", err)
		}
		if result.Status != StatusFailed {
			t.Errorf("存在失败号码时整体状态应为 %s（否则第 2 条失败被静默）, 实际 %s", StatusFailed, result.Status)
		}
		if result.Error == nil || !strings.Contains(result.Error.Error(), "1/2") {
			t.Errorf("聚合错误应含失败比例 1/2, 实际 %v", result.Error)
		}
		if result.MultipleResults[0].Status != StatusSuccess {
			t.Errorf("第 1 条应为成功, 实际 %s", result.MultipleResults[0].Status)
		}
		if result.MultipleResults[1].Status != StatusFailed {
			t.Errorf("第 2 条应为失败, 实际 %s", result.MultipleResults[1].Status)
		}
		if result.MultipleResults[1].Error == nil {
			t.Error("失败子结果应携带 Error 供逐号码定位")
		}
		// 主结果 Extra 应为独立拷贝，不与子结果共享 map
		result.MultipleResults[0].Extra["Code"] = "mutated"
		if result.Extra["Code"] == "mutated" {
			t.Error("主结果 Extra 不应与子结果共享同一 map")
		}
	})
}

// TestParsePullSendStatus 验证状态拉取响应的状态映射与边界。
func TestParsePullSendStatus(t *testing.T) {
	t.Parallel()

	t.Run("空集合返回空列表", func(t *testing.T) {
		t.Parallel()
		got := parsePullSendStatus(nil)
		if len(got) != 0 {
			t.Errorf("空集合应返回 0 条, 实际 %d 条", len(got))
		}
	})

	t.Run("状态映射", func(t *testing.T) {
		t.Parallel()
		receiveTime := uint64(1700000000)
		set := []*tencentSms.PullSmsSendStatus{
			{ReportStatus: strPtr("SUCCESS"), UserReceiveTime: &receiveTime},
			{ReportStatus: strPtr("FAIL"), Description: strPtr("用户关机")},
			{ReportStatus: nil}, // 未回执 → 处理中
			nil,                 // nil 元素应跳过
		}
		got := parsePullSendStatus(set)
		if len(got) != 3 {
			t.Fatalf("期望 3 条（nil 元素跳过）, 实际 %d 条", len(got))
		}
		if got[0].Status != StatusDelivered {
			t.Errorf("SUCCESS 应映射为 %s, 实际 %s", StatusDelivered, got[0].Status)
		}
		if got[0].DeliverTime.Unix() != int64(receiveTime) {
			t.Errorf("送达时间应解析为 %d, 实际 %v", receiveTime, got[0].DeliverTime)
		}
		if got[1].Status != StatusFailed {
			t.Errorf("FAIL 应映射为 %s, 实际 %s", StatusFailed, got[1].Status)
		}
		if got[1].StatusMessage != "用户关机" {
			t.Errorf("状态描述应为 用户关机, 实际 %q", got[1].StatusMessage)
		}
		if got[2].Status != StatusPending {
			t.Errorf("未回执应映射为 %s, 实际 %s", StatusPending, got[2].Status)
		}
	})

	t.Run("接收时间为0不设置投递时间", func(t *testing.T) {
		t.Parallel()
		zero := uint64(0)
		got := parsePullSendStatus([]*tencentSms.PullSmsSendStatus{{UserReceiveTime: &zero}})
		if len(got) != 1 {
			t.Fatalf("期望 1 条, 实际 %d 条", len(got))
		}
		if !got[0].DeliverTime.IsZero() {
			t.Errorf("UserReceiveTime=0 时 DeliverTime 应保持零值, 实际 %v", got[0].DeliverTime)
		}
	})
}

// TestNormalizeTencentStatusQuery 验证腾讯查询归一化与 Limit 上限截断。
func TestNormalizeTencentStatusQuery(t *testing.T) {
	t.Parallel()

	t.Run("Limit超过100截断", func(t *testing.T) {
		t.Parallel()
		q, err := normalizeTencentStatusQuery(&SMSStatusQuery{
			PhoneNumber: "+8613711112222",
			Limit:       500,
		})
		if err != nil {
			t.Fatalf("归一化失败: %v", err)
		}
		if q.Limit != tencentMaxPageSize {
			t.Errorf("Limit=500 应截断为 %d, 实际 %d", tencentMaxPageSize, q.Limit)
		}
	})

	t.Run("透传通用归一化错误", func(t *testing.T) {
		t.Parallel()
		if _, err := normalizeTencentStatusQuery(nil); err == nil {
			t.Fatal("nil 查询应返回错误")
		}
	})
}

// TestTencentSendBatchSMS 验证批量发送的健壮性（空列表、nil 元素不 panic）。
func TestTencentSendBatchSMS(t *testing.T) {
	t.Parallel()
	client := mustTencentClient(t)
	ctx := context.Background()

	t.Run("空列表", func(t *testing.T) {
		results, err := client.SendBatchSMS(ctx, nil)
		if err != nil {
			t.Fatalf("空批量应返回 nil error, 实际 %v", err)
		}
		if len(results) != 0 {
			t.Errorf("空批量应返回 0 条结果, 实际 %d 条", len(results))
		}
	})

	t.Run("nil元素不panic且返回失败结果与聚合error", func(t *testing.T) {
		// 旧实现的失败日志取 req.PhoneNumbers[0]，req 为 nil/无号码时 panic
		results, err := client.SendBatchSMS(ctx, []*SendRequest{nil})
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

// TestTencentGetSMSStatusEntryValidation 验证查询入口归一化拦截（不发网络）。
func TestTencentGetSMSStatusEntryValidation(t *testing.T) {
	t.Parallel()
	client := mustTencentClient(t)

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

// strPtr 构造字符串指针（测试用）。
func strPtr(s string) *string { return &s }
