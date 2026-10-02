package gosms

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestNewSMSClient 验证客户端创建的入口防护与正常分支。
func TestNewSMSClient(t *testing.T) {
	t.Parallel()

	t.Run("nil配置返回错误", func(t *testing.T) {
		t.Parallel()
		client, err := NewSMSClient(nil)
		if err == nil {
			t.Fatal("cfg 为 nil 时应返回错误（旧实现直接解引用 panic）")
		}
		if client != nil {
			t.Errorf("出错时客户端应为 nil, 实际 %v", client)
		}
		if !strings.Contains(err.Error(), "配置不能为空") {
			t.Errorf("错误消息应为中文且说明原因, 实际 %q", err.Error())
		}
	})

	t.Run("未知服务商类型返回错误", func(t *testing.T) {
		t.Parallel()
		client, err := NewSMSClient(&Config{ProviderType: ProviderType("unknown")})
		if err == nil {
			t.Fatal("未知服务商类型应返回错误")
		}
		if client != nil {
			t.Errorf("出错时客户端应为 nil, 实际 %v", client)
		}
		if !strings.Contains(err.Error(), "不支持的短信服务商类型") {
			t.Errorf("错误消息应为中文且说明原因, 实际 %q", err.Error())
		}
	})

	// 以下两条守护 typed-nil 泄漏（R6 全仓复核 P0）：缺密钥走的是具体构造函数
	// 的「return nil, err」路径，typed-nil 指针装箱进接口后 client != nil 恒成立，
	// 调用方按非空判定即 panic——与 goemail NewEmailClient R3-2 完全同型
	t.Run("腾讯云缺密钥时客户端必须为nil", func(t *testing.T) {
		t.Parallel()
		client, err := NewSMSClient(&Config{ProviderType: ProviderTypeTencentSMS})
		if err == nil {
			t.Fatal("缺密钥应返回错误")
		}
		if client != nil {
			t.Errorf("typed-nil 泄漏：出错时接口应为真 nil, 实际 %v（调用方按非空判定会 panic）", client)
		}
	})

	t.Run("阿里云缺密钥时客户端必须为nil", func(t *testing.T) {
		t.Parallel()
		client, err := NewSMSClient(&Config{ProviderType: ProviderTypeAliyunSMS})
		if err == nil {
			t.Fatal("缺密钥应返回错误")
		}
		if client != nil {
			t.Errorf("typed-nil 泄漏：出错时接口应为真 nil, 实际 %v（调用方按非空判定会 panic）", client)
		}
	})

	t.Run("腾讯云创建成功", func(t *testing.T) {
		t.Parallel()
		client, err := NewSMSClient(tencentTestConfig())
		if err != nil {
			t.Fatalf("创建腾讯云客户端失败: %v", err)
		}
		if client.GetProviderType() != ProviderTypeTencentSMS {
			t.Errorf("期望提供商类型 %s, 实际 %s", ProviderTypeTencentSMS, client.GetProviderType())
		}
	})

	t.Run("阿里云创建成功", func(t *testing.T) {
		t.Parallel()
		client, err := NewSMSClient(aliyunTestConfig())
		if err != nil {
			t.Fatalf("创建阿里云客户端失败: %v", err)
		}
		if client.GetProviderType() != ProviderTypeAliyunSMS {
			t.Errorf("期望提供商类型 %s, 实际 %s", ProviderTypeAliyunSMS, client.GetProviderType())
		}
	})
}

// TestSendRequestValidate 验证发送请求校验的全分支与中文错误消息。
func TestSendRequestValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		req        *SendRequest
		wantErr    bool
		wantSubStr string // 期望错误消息包含的子串（wantErr=false 时忽略）
	}{
		{
			name: "有效请求",
			req: &SendRequest{
				PhoneNumbers: []string{"+8613711112222", "+8613711112223"},
				TemplateID:   "123456",
				SignName:     "测试签名",
			},
			wantErr: false,
		},
		{
			name:       "nil请求",
			req:        nil,
			wantErr:    true,
			wantSubStr: "发送请求不能为空",
		},
		{
			name: "缺少手机号",
			req: &SendRequest{
				PhoneNumbers: []string{},
				TemplateID:   "123456",
				SignName:     "测试签名",
			},
			wantErr:    true,
			wantSubStr: "手机号列表不能为空",
		},
		{
			name: "缺少模板ID",
			req: &SendRequest{
				PhoneNumbers: []string{"+8613711112222"},
				SignName:     "测试签名",
			},
			wantErr:    true,
			wantSubStr: "模板ID不能为空",
		},
		{
			name: "缺少签名",
			req: &SendRequest{
				PhoneNumbers: []string{"+8613711112222"},
				TemplateID:   "123456",
			},
			wantErr:    true,
			wantSubStr: "签名不能为空",
		},
		{
			name: "手机号缺加号前缀",
			req: &SendRequest{
				PhoneNumbers: []string{"13711112222"},
				TemplateID:   "123456",
				SignName:     "测试签名",
			},
			wantErr:    true,
			wantSubStr: "手机号格式错误",
		},
		{
			name: "手机号含字母",
			req: &SendRequest{
				PhoneNumbers: []string{"+86abc11112222"},
				TemplateID:   "123456",
				SignName:     "测试签名",
			},
			wantErr:    true,
			wantSubStr: "手机号格式错误",
		},
		{
			name: "列表中一个号码非法",
			req: &SendRequest{
				PhoneNumbers: []string{"+8613711112222", "not-a-phone"},
				TemplateID:   "123456",
				SignName:     "测试签名",
			},
			wantErr:    true,
			wantSubStr: "手机号格式错误",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.req.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && !strings.Contains(err.Error(), tt.wantSubStr) {
				t.Errorf("错误消息应包含 %q, 实际 %q", tt.wantSubStr, err.Error())
			}
		})
	}
}

// TestValidatePhoneNumber 验证手机号校验的导出版与纯函数判定一致，且覆盖 E.164 边界。
func TestValidatePhoneNumber(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name     string
		phone    string
		expected bool
	}{
		{"有效国内号码", "+8613711112222", true},
		{"有效国际号码", "+1234567890", true},
		{"数字位恰好7位", "+1234567", true},
		{"数字位恰好15位", "+123456789012345", true},
		{"空串", "", false},
		{"太短（6位数字）", "+123456", false},
		{"太长（16位数字）", "+1234567890123456", false},
		{"无国家码前缀", "13711112222", false},
		{"含字母", "+86137abc2222", false},
		{"仅加号", "+", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ValidatePhoneNumber(ctx, tt.phone)
			if got != tt.expected {
				t.Errorf("ValidatePhoneNumber(%q) = %v, 期望 %v", tt.phone, got, tt.expected)
			}
			if pure := isValidPhoneNumber(tt.phone); pure != tt.expected {
				t.Errorf("纯函数 isValidPhoneNumber(%q) = %v, 期望 %v（应与导出版一致）", tt.phone, pure, tt.expected)
			}
		})
	}
}

// TestFormatPhoneNumber 验证手机号格式化的国际前缀补全规则。
func TestFormatPhoneNumber(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"国内手机号不带+86", "13711112222", "+8613711112222"},
		{"国内手机号已带+86", "+8613711112222", "+8613711112222"},
		{"国际手机号", "+1234567890", "+1234567890"},
		{"非11位号码补加号", "1234567890", "+1234567890"},
		{"空串", "", "+"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := FormatPhoneNumber(tt.input)
			if result != tt.expected {
				t.Errorf("期望 %s, 实际得到 %s", tt.expected, result)
			}
		})
	}
}

// TestMaskPhone 验证手机号脱敏的边界（PII 只进日志/span，业务结构体保留原文）。
func TestMaskPhone(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"空串原样返回", "", ""},
		{"短号码整体掩码", "123", "***"},
		{"恰好7位整体掩码", "1234567", "*******"},
		{"国内11位保留首3尾4", "13711112222", "137****2222"},
		{"国际格式保留前3后4", "+8613711112222", "+86*******2222"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := maskPhone(tt.input)
			if got != tt.expected {
				t.Errorf("maskPhone(%q) = %q, 期望 %q", tt.input, got, tt.expected)
			}
		})
	}
}

// TestWithSignFallback 验证签名兜底的副本语义（不修改调用方结构体）。
func TestWithSignFallback(t *testing.T) {
	t.Parallel()

	t.Run("空签名回退默认值", func(t *testing.T) {
		t.Parallel()
		req := &SendRequest{SignName: ""}
		got := withSignFallback(req, "默认签名")
		if got.SignName != "默认签名" {
			t.Errorf("期望兜底为 %q, 实际 %q", "默认签名", got.SignName)
		}
		if req.SignName != "" {
			t.Errorf("原结构体不应被修改, 实际 %q", req.SignName)
		}
	})

	t.Run("非空签名保持不变", func(t *testing.T) {
		t.Parallel()
		req := &SendRequest{SignName: "显式签名"}
		got := withSignFallback(req, "默认签名")
		if got.SignName != "显式签名" {
			t.Errorf("期望保持 %q, 实际 %q", "显式签名", got.SignName)
		}
	})
}

// TestNormalizeStatusQuery 验证查询参数归一化的兜底与 fail loud 分支。
func TestNormalizeStatusQuery(t *testing.T) {
	t.Parallel()

	t.Run("nil返回错误", func(t *testing.T) {
		t.Parallel()
		_, err := normalizeStatusQuery(nil)
		if err == nil || !strings.Contains(err.Error(), "查询参数不能为空") {
			t.Fatalf("期望 nil 查询参数错误, 实际 %v", err)
		}
	})

	t.Run("缺手机号返回错误", func(t *testing.T) {
		t.Parallel()
		_, err := normalizeStatusQuery(&SMSStatusQuery{})
		if err == nil || !strings.Contains(err.Error(), "查询手机号不能为空") {
			t.Fatalf("期望缺手机号错误, 实际 %v", err)
		}
	})

	t.Run("日期倒置返回错误", func(t *testing.T) {
		t.Parallel()
		now := time.Now()
		_, err := normalizeStatusQuery(&SMSStatusQuery{
			PhoneNumber: "+8613711112222",
			FromDate:    now,
			ToDate:      now.Add(-time.Hour),
		})
		if err == nil || !strings.Contains(err.Error(), "开始时间晚于结束时间") {
			t.Fatalf("期望日期倒置错误, 实际 %v", err)
		}
	})

	t.Run("零值日期回退为近24小时", func(t *testing.T) {
		t.Parallel()
		q, err := normalizeStatusQuery(&SMSStatusQuery{PhoneNumber: "+8613711112222"})
		if err != nil {
			t.Fatalf("归一化失败: %v", err)
		}
		if q.ToDate.IsZero() {
			t.Error("ToDate 零值应回退为当前时间")
		}
		if got := q.ToDate.Sub(q.FromDate); got != 24*time.Hour {
			t.Errorf("FromDate 应为 ToDate 前 24 小时, 实际差值 %v", got)
		}
	})

	t.Run("Limit为0归一为默认值", func(t *testing.T) {
		t.Parallel()
		q, err := normalizeStatusQuery(&SMSStatusQuery{PhoneNumber: "+8613711112222"})
		if err != nil {
			t.Fatalf("归一化失败: %v", err)
		}
		if q.Limit != defaultQueryLimit {
			t.Errorf("Limit=0 应归一为 %d, 实际 %d", defaultQueryLimit, q.Limit)
		}
	})

	t.Run("合法参数透传且不改调用方", func(t *testing.T) {
		t.Parallel()
		from := time.Now().Add(-2 * time.Hour)
		to := time.Now()
		orig := &SMSStatusQuery{
			PhoneNumber: "+8613711112222",
			FromDate:    from,
			ToDate:      to,
			Limit:       20,
		}
		q, err := normalizeStatusQuery(orig)
		if err != nil {
			t.Fatalf("归一化失败: %v", err)
		}
		if q.Limit != 20 {
			t.Errorf("Limit=20 应保持不变, 实际 %d", q.Limit)
		}
		if orig.Limit != 20 || !orig.FromDate.Equal(from) {
			t.Error("归一化不应修改调用方结构体")
		}
	})
}

// TestRequestIDAttr 验证 request_id 属性提取的三分支（有值/无值/nil ctx）。
func TestRequestIDAttr(t *testing.T) {
	t.Parallel()

	t.Run("上下文含request_id", func(t *testing.T) {
		t.Parallel()
		ctx := context.WithValue(context.Background(), logger.ContextKeyRequestID, "req-123")
		kv := requestIDAttr(ctx)
		if kv.Key != attribute.Key("sms.request_id") {
			t.Errorf("属性键应为 sms.request_id, 实际 %s", kv.Key)
		}
		if got := kv.Value.AsString(); got != "req-123" {
			t.Errorf("属性值应为 req-123, 实际 %q", got)
		}
	})

	t.Run("上下文不含request_id返回空串", func(t *testing.T) {
		t.Parallel()
		kv := requestIDAttr(context.Background())
		if got := kv.Value.AsString(); got != "" {
			t.Errorf("缺失时属性值应为空串, 实际 %q", got)
		}
	})

	t.Run("nil上下文返回空串", func(t *testing.T) {
		t.Parallel()
		kv := requestIDAttr(nil)
		if got := kv.Value.AsString(); got != "" {
			t.Errorf("nil ctx 属性值应为空串, 实际 %q", got)
		}
	})
}

// TestFailSpan 验证统一失败埋点：RecordError 事件 + codes.Error 状态 + 原 error 返回。
func TestFailSpan(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	_, span := tp.Tracer("test").Start(context.Background(), "test.span")

	orig := errors.New("boom")
	returned := failSpan(span, orig)
	if !errors.Is(returned, orig) {
		t.Errorf("failSpan 应原样返回传入的 error, 实际 %v", returned)
	}
	span.End()

	finished := recorder.Ended()
	if len(finished) != 1 {
		t.Fatalf("期望恰好 1 个已结束 span, 实际 %d", len(finished))
	}
	recorded := finished[0]
	if recorded.Status().Code != codes.Error {
		t.Errorf("span 状态应为 Error, 实际 %v", recorded.Status().Code)
	}
	if recorded.Status().Description != "boom" {
		t.Errorf("span 状态描述应为 boom, 实际 %q", recorded.Status().Description)
	}
	hasException := false
	for _, event := range recorded.Events() {
		if event.Name == "exception" {
			hasException = true
		}
	}
	if !hasException {
		t.Error("span 应记录 exception 事件（RecordError）")
	}
}

// TestFailedResult 验证失败结果构造器的统一语义（失败必返回非 nil result）。
func TestFailedResult(t *testing.T) {
	t.Parallel()

	orig := errors.New("发送失败")
	result := failedResult(orig)
	if result == nil {
		t.Fatal("failedResult 不应返回 nil")
	}
	if result.Status != StatusFailed {
		t.Errorf("状态应为 %s, 实际 %s", StatusFailed, result.Status)
	}
	if !errors.Is(result.Error, orig) {
		t.Errorf("Error 应保留原错误, 实际 %v", result.Error)
	}

	statusResult := failedStatusResult(orig)
	if statusResult == nil {
		t.Fatal("failedStatusResult 不应返回 nil")
	}
	if statusResult.Status != StatusFailed {
		t.Errorf("状态应为 %s, 实际 %s", StatusFailed, statusResult.Status)
	}
	if !errors.Is(statusResult.Error, orig) {
		t.Errorf("Error 应保留原错误, 实际 %v", statusResult.Error)
	}
}

// TestSendResultIsSuccess 验证结果判定助手（R2-P1-3：消除「err==nil 即成功」的 API 陷阱）。
func TestSendResultIsSuccess(t *testing.T) {
	t.Parallel()

	var nilResult *SendResult
	if nilResult.IsSuccess() {
		t.Error("nil receiver 应返回 false（可对可能为 nil 的 result 直接调用）")
	}
	if (&SendResult{Status: StatusFailed}).IsSuccess() {
		t.Error("failed 状态应返回 false")
	}
	if !(&SendResult{Status: StatusSuccess}).IsSuccess() {
		t.Error("success 状态应返回 true")
	}
	// 云业务失败的典型形态：err == nil + Status=failed —— IsSuccess 必须判 false
	if (&SendResult{Status: StatusFailed, Error: errors.New("云返回错误码")}).IsSuccess() {
		t.Error("业务失败（Error 非空）应返回 false")
	}
}

// TestMaskSecret 验证密钥脱敏的边界（与 maskPhone 同一安全边界）。
func TestMaskSecret(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		secret string
		want   string
	}{
		{"空串", "", ""},
		{"长度4以内整体掩码", "ab", "**"},
		{"恰好4位整体掩码", "abcd", "****"},
		{"长密钥保留首2尾2", "abcdefghijkl", "ab********kl"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := maskSecret(tt.secret)
			if got != tt.want {
				t.Errorf("maskSecret(%q) = %q, 期望 %q", tt.secret, got, tt.want)
			}
			if tt.secret != "" && strings.Contains(got, tt.secret) {
				t.Errorf("脱敏结果不应包含完整明文 %q, 实际 %q", tt.secret, got)
			}
		})
	}
}

// TestMaskAccessKey 验证密钥标识脱敏的边界（R3-P2-2：AccessKeyID 同样不落明文）。
func TestMaskAccessKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ak   string
		want string
	}{
		{"空串", "", ""},
		{"长度8以内整体掩码", "abc123", "******"},
		{"恰好8位整体掩码", "abcd1234", "********"},
		{"长标识保留首4尾4", "AKIDabcdefghijkl", "AKID********ijkl"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := maskAccessKey(tt.ak)
			if got != tt.want {
				t.Errorf("maskAccessKey(%q) = %q, 期望 %q", tt.ak, got, tt.want)
			}
			if tt.ak != "" && strings.Contains(got, tt.ak) {
				t.Errorf("脱敏结果不应包含完整明文 %q, 实际 %q", tt.ak, got)
			}
		})
	}
}

// TestConfigString 验证 Config 打印时 SecretKey 与 AccessKeyID 自动脱敏（R2-P1-5 + R3-P2-2）。
func TestConfigString(t *testing.T) {
	t.Parallel()

	cfg := Config{
		ProviderType:    ProviderTypeTencentSMS,
		Region:          "ap-guangzhou",
		AccessKeyID:     "AKIDtest123",
		SecretKey:       "super-secret-key-abcdef",
		TencentAppID:    "1400282665",
		TencentSignName: "测试签名",
	}

	for _, format := range []string{"%v", "%+v", "%s"} {
		out := fmt.Sprintf(format, cfg)
		if strings.Contains(out, cfg.SecretKey) {
			t.Errorf("%s 打印不得包含 SecretKey 明文, 实际输出: %s", format, out)
		}
		if !strings.Contains(out, maskSecret(cfg.SecretKey)) {
			t.Errorf("%s 打印应包含脱敏后的密钥（首2尾2）, 实际输出: %s", format, out)
		}
		if strings.Contains(out, cfg.AccessKeyID) {
			t.Errorf("%s 打印不得包含 AccessKeyID 明文（与 SecretKey 组合使用）, 实际输出: %s", format, out)
		}
		if !strings.Contains(out, maskAccessKey(cfg.AccessKeyID)) {
			t.Errorf("%s 打印应包含脱敏后的 AccessKeyID（首4尾4）, 实际输出: %s", format, out)
		}
		if !strings.Contains(out, "ap-guangzhou") {
			t.Errorf("%s 打印应保留非敏感字段便于排查, 实际输出: %s", format, out)
		}
	}

	// 指针接收同样生效（Config 存储为 *Config）
	out := fmt.Sprintf("%+v", &cfg)
	if strings.Contains(out, cfg.SecretKey) || strings.Contains(out, cfg.AccessKeyID) {
		t.Errorf("指针打印同样不得泄露密钥明文, 实际输出: %s", out)
	}
}

// TestBatchErrors 验证批量失败聚合（R2-P1-4：外层 error 由恒 nil 改为聚合结果）。
func TestBatchErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		results     []*SendResult
		wantNil     bool
		wantContain string
	}{
		{"空列表", nil, true, ""},
		{"全成功", []*SendResult{{Status: StatusSuccess}, {Status: StatusSuccess}}, true, ""},
		{"单条业务失败", []*SendResult{{Status: StatusSuccess}, {Status: StatusFailed, Error: errors.New("云返回错误码")}}, false, "第 2 条: 云返回错误码"},
		{"失败但无Error信息", []*SendResult{{Status: StatusFailed}}, false, "第 1 条发送失败"},
		{"多条失败均聚合", []*SendResult{{Status: StatusFailed, Error: errors.New("a")}, {Status: StatusFailed, Error: errors.New("b")}}, false, "第 2 条: b"},
		{"nil结果防御", []*SendResult{nil}, false, "第 1 条结果为空"},
		{"状态矛盾success但Error非空", []*SendResult{{Status: StatusSuccess, Error: errors.New("异常残留")}}, false, "状态矛盾"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := batchErrors(tt.results)
			if tt.wantNil {
				if err != nil {
					t.Errorf("期望 nil error, 实际 %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("存在失败条目时应返回聚合 error")
			}
			if !strings.Contains(err.Error(), tt.wantContain) {
				t.Errorf("聚合错误应包含 %q, 实际 %q", tt.wantContain, err.Error())
			}
		})
	}
}

// TestRunBatchSMS 验证批量并发骨架的保序、并发上限与聚合（R3-P2-3：串行改并发）。
func TestRunBatchSMS(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("空列表返回空结果与nilerror", func(t *testing.T) {
		t.Parallel()
		results, err := runBatchSMS(ctx, nil, 0, func(context.Context, *SendRequest) (*SendResult, error) {
			t.Error("空列表不应调用 send")
			return nil, nil
		})
		if err != nil {
			t.Errorf("空批量应返回 nil error, 实际 %v", err)
		}
		if len(results) != 0 {
			t.Errorf("空批量应返回 0 条结果, 实际 %d 条", len(results))
		}
	})

	t.Run("保序且按输入序聚合", func(t *testing.T) {
		t.Parallel()
		// 并发写入最易引入的回归就是结果错位——输出顺序必须恒等于请求顺序，
		// 否则「第 N 条」聚合序号会指向错误的请求，批量诊断全部失效
		reqs := make([]*SendRequest, 30)
		for i := range reqs {
			reqs[i] = &SendRequest{TemplateID: fmt.Sprintf("req-%d", i)}
		}
		results, err := runBatchSMS(ctx, reqs, 0, func(_ context.Context, r *SendRequest) (*SendResult, error) {
			if r.TemplateID == "req-7" {
				return &SendResult{Status: StatusFailed, Error: errors.New("boom")}, nil
			}
			return &SendResult{Status: StatusSuccess, PhoneNumber: r.TemplateID}, nil
		})
		if err == nil {
			t.Fatal("存在失败条目时应返回聚合 error")
		}
		if !strings.Contains(err.Error(), "第 8 条") {
			t.Errorf("聚合序号应指向输入序的第 8 条, 实际 %q", err.Error())
		}
		if len(results) != len(reqs) {
			t.Fatalf("并发不得丢结果, 期望 %d 条, 实际 %d 条", len(reqs), len(results))
		}
		for i, got := range results {
			if got == nil {
				t.Fatalf("第 %d 条结果为空（并发写入丢结果）", i+1)
			}
			if i == 7 {
				if got.Status != StatusFailed {
					t.Errorf("第 8 条应为失败, 实际 %s", got.Status)
				}
				continue
			}
			want := fmt.Sprintf("req-%d", i)
			if got.PhoneNumber != want {
				t.Errorf("输出顺序应与请求顺序一致: 第 %d 条 = %q, 期望 %q", i+1, got.PhoneNumber, want)
			}
		}
	})

	t.Run("并发峰值不超过上限且确实并行", func(t *testing.T) {
		t.Parallel()
		var cur, peak int32
		reqs := make([]*SendRequest, 30)
		for i := range reqs {
			reqs[i] = &SendRequest{}
		}
		_, err := runBatchSMS(ctx, reqs, 0, func(context.Context, *SendRequest) (*SendResult, error) {
			n := atomic.AddInt32(&cur, 1)
			for {
				old := atomic.LoadInt32(&peak)
				if n <= old || atomic.CompareAndSwapInt32(&peak, old, n) {
					break
				}
			}
			time.Sleep(time.Millisecond) // 保持在飞，让后续 worker 重叠
			atomic.AddInt32(&cur, -1)
			return &SendResult{Status: StatusSuccess}, nil
		})
		if err != nil {
			t.Fatalf("全部成功时应返回 nil error, 实际 %v", err)
		}
		if got := atomic.LoadInt32(&peak); got > batchConcurrency {
			t.Errorf("并发峰值 %d 不应超过上限 %d（限流失控会打满云侧速率限制）", got, batchConcurrency)
		}
		if got := atomic.LoadInt32(&peak); got < 2 {
			t.Errorf("并发峰值 %d，应确实并行执行（退化为串行则失去吞吐意义）", got)
		}
	})

	t.Run("自定义并发上限生效", func(t *testing.T) {
		t.Parallel()
		var cur, peak int32
		reqs := make([]*SendRequest, 30)
		for i := range reqs {
			reqs[i] = &SendRequest{}
		}
		_, err := runBatchSMS(ctx, reqs, 3, func(context.Context, *SendRequest) (*SendResult, error) {
			n := atomic.AddInt32(&cur, 1)
			for {
				old := atomic.LoadInt32(&peak)
				if n <= old || atomic.CompareAndSwapInt32(&peak, old, n) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			atomic.AddInt32(&cur, -1)
			return &SendResult{Status: StatusSuccess}, nil
		})
		if err != nil {
			t.Fatalf("全部成功时应返回 nil error, 实际 %v", err)
		}
		if got := atomic.LoadInt32(&peak); got > 3 {
			t.Errorf("自定义并发 3 的峰值不应超过 3, 实际 %d", got)
		}
		if got := atomic.LoadInt32(&peak); got < 2 {
			t.Errorf("自定义并发 3 应确实并行, 峰值 %d", got)
		}
	})

	t.Run("上下文取消后全部条目标记失败且零发送", func(t *testing.T) {
		t.Parallel()
		// 预取消 ctx：worker 在首次 select 即命中 Done，一条都不应发出去（R6 全仓复核 P2）
		canceledCtx, cancel := context.WithCancel(context.Background())
		cancel()

		var sent int32
		reqs := make([]*SendRequest, 15)
		for i := range reqs {
			reqs[i] = &SendRequest{}
		}
		results, err := runBatchSMS(canceledCtx, reqs, 0, func(context.Context, *SendRequest) (*SendResult, error) {
			atomic.AddInt32(&sent, 1)
			return &SendResult{Status: StatusSuccess}, nil
		})
		if got := atomic.LoadInt32(&sent); got != 0 {
			t.Errorf("ctx 已取消时不应发起任何发送, 实际调用 %d 次", got)
		}
		if len(results) != len(reqs) {
			t.Fatalf("取消也必须保序回填完整结果, 期望 %d 条, 实际 %d 条", len(reqs), len(results))
		}
		for i, r := range results {
			if r == nil || r.Status != StatusFailed || r.Error == nil {
				t.Errorf("第 %d 条应回填 failed + ctx 错误, 实际 %+v", i+1, r)
			}
		}
		if err == nil {
			t.Fatal("全部取消时应返回聚合 error")
		}
		if !strings.Contains(err.Error(), "第 1 条") || !strings.Contains(err.Error(), "上下文取消") {
			t.Errorf("聚合错误应含条目序号与取消原因, 实际 %q", err.Error())
		}
	})

	t.Run("中途取消后不再发起新任务", func(t *testing.T) {
		t.Parallel()
		ctx2, cancel := context.WithCancel(context.Background())
		defer cancel()

		var sent int32
		reqs := make([]*SendRequest, 30)
		for i := range reqs {
			reqs[i] = &SendRequest{}
		}
		results, err := runBatchSMS(ctx2, reqs, 0, func(context.Context, *SendRequest) (*SendResult, error) {
			if atomic.AddInt32(&sent, 1) == 1 {
				cancel() // 首条执行中取消：已进入 send 的批次允许跑完，后续任务不再发起
			}
			return &SendResult{Status: StatusSuccess}, nil
		})
		// 不断言具体 send 次数（取消时刻与其他 worker 的 select 存在竞态窗口），
		// 但必须证明「未全量发出」——取消失效时 30 条会全部进 send
		if got := atomic.LoadInt32(&sent); got >= int32(len(reqs)) {
			t.Errorf("取消后不应发起全部任务: send 调用数 %d/%d", got, len(reqs))
		}
		if len(results) != len(reqs) {
			t.Fatalf("取消也必须回填完整结果, 期望 %d 条, 实际 %d 条", len(reqs), len(results))
		}
		for i, r := range results {
			if r == nil {
				t.Fatalf("第 %d 条结果为空", i+1)
			}
		}
		if err == nil {
			t.Fatal("存在未执行条目时应返回聚合 error")
		}
		if !strings.Contains(err.Error(), "上下文取消") {
			t.Errorf("聚合错误应含取消原因, 实际 %q", err.Error())
		}
	})
}

// TestResolveBatchConcurrency 验证批量并发上限的解析：零值/负值回落默认 10，正值原样生效。
func TestResolveBatchConcurrency(t *testing.T) {
	t.Parallel()
	tests := []struct {
		n    int
		want int
	}{
		{0, batchConcurrency},
		{-1, batchConcurrency},
		{1, 1},
		{64, 64},
	}
	for _, tt := range tests {
		if got := resolveBatchConcurrency(tt.n); got != tt.want {
			t.Errorf("resolveBatchConcurrency(%d) = %d, 期望 %d", tt.n, got, tt.want)
		}
	}
}
