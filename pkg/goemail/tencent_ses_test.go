package goemail

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	ses "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ses/v20201002"
)

// TestNewTencentSESClient 验证腾讯云 SES 客户端创建的入口防护与区域不回写。
func TestNewTencentSESClient(t *testing.T) {
	t.Parallel()

	t.Run("缺少密钥返回错误", func(t *testing.T) {
		t.Parallel()
		cfg := tencentTestConfig()
		cfg.SecretKey = ""
		client, err := newTencentSESClient(cfg)
		if err == nil {
			t.Fatal("SecretKey 为空时应返回错误")
		}
		if client != nil {
			t.Errorf("出错时客户端应为 nil, 实际 %v", client)
		}
		if !strings.Contains(err.Error(), "腾讯云 SES") {
			t.Errorf("错误消息应为中文且说明服务商, 实际 %q", err.Error())
		}
	})

	t.Run("区域为空时本地默认香港且不回写共享配置", func(t *testing.T) {
		t.Parallel()
		cfg := tencentTestConfig()
		cfg.Region = ""
		client, err := newTencentSESClient(cfg)
		if err != nil {
			t.Fatalf("创建客户端失败: %v", err)
		}
		if client.region != "ap-hongkong" {
			t.Errorf("默认区域应为 ap-hongkong, 实际 %q", client.region)
		}
		if cfg.Region != "" {
			t.Errorf("构造函数不得回写共享 cfg.Region, 实际 %q", cfg.Region)
		}
	})
}

// TestDerefHelpers 验证 SDK 指针字段的 nil 安全取值（P0 panic 防护的基础构件）。
func TestDerefHelpers(t *testing.T) {
	t.Parallel()

	if got := derefString(nil); got != "" {
		t.Errorf("nil 指针应返回零值, 实际 %q", got)
	}
	if got := derefString(common.StringPtr("x")); got != "x" {
		t.Errorf("非 nil 应解引用, 实际 %q", got)
	}
	if got := derefInt64(nil); got != 0 {
		t.Errorf("nil 指针应返回零值, 实际 %d", got)
	}
	if got := derefInt64(common.Int64Ptr(42)); got != 42 {
		t.Errorf("非 nil 应解引用, 实际 %d", got)
	}
}

// TestParseSendStatus 验证发送状态码到语义状态的映射。
func TestParseSendStatus(t *testing.T) {
	t.Parallel()
	client := mustTencentClient(t)

	tests := []struct {
		name       string
		code       *int64
		wantStatus string
	}{
		{"nil状态码返回unknown", nil, "unknown"},
		{"0处理成功", common.Int64Ptr(0), "accepted"},
		{"1006触发频率控制", common.Int64Ptr(1006), StatusFailed},
		{"1007地址黑名单", common.Int64Ptr(1007), StatusFailed},
		{"未知码返回unknown", common.Int64Ptr(999999), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, msg := client.parseSendStatus(tt.code)
			if got != tt.wantStatus {
				t.Errorf("parseSendStatus(%v) = %q, 期望 %q", tt.code, got, tt.wantStatus)
			}
			if msg == "" {
				t.Error("状态描述不应为空")
			}
		})
	}
}

// TestParseDeliverStatus 验证投递状态码到语义状态的映射。
func TestParseDeliverStatus(t *testing.T) {
	t.Parallel()
	client := mustTencentClient(t)

	tests := []struct {
		name       string
		code       *int64
		wantStatus string
	}{
		{"nil状态码返回pending", nil, "pending"},
		{"0进入队列", common.Int64Ptr(0), "pending"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, _ := client.parseDeliverStatus(tt.code)
			if got != tt.wantStatus {
				t.Errorf("parseDeliverStatus(%v) = %q, 期望 %q", tt.code, got, tt.wantStatus)
			}
		})
	}
}

// TestParseEmailStatusList 验证状态列表解析的 nil 防御（P0 panic 回归守护）：
// 服务端在特定状态下可能不返回任意指针字段，任何一条都不得 panic。
func TestParseEmailStatusList(t *testing.T) {
	t.Parallel()
	client := mustTencentClient(t)

	t.Run("nil列表返回nil", func(t *testing.T) {
		t.Parallel()
		if got := client.parseEmailStatusList(nil); got != nil {
			t.Errorf("nil 输入应返回 nil, 实际 %v", got)
		}
	})

	t.Run("列表含nil元素跳过", func(t *testing.T) {
		t.Parallel()
		got := client.parseEmailStatusList([]*ses.SendEmailStatus{nil})
		if len(got) != 0 {
			t.Errorf("nil 元素应被跳过, 实际 %d 条", len(got))
		}
	})

	t.Run("全nil字段不panic", func(t *testing.T) {
		t.Parallel()
		// 旧实现逐字段裸解引用（*status.DeliverStatus 等），任意字段缺失即 panic
		got := client.parseEmailStatusList([]*ses.SendEmailStatus{{}})
		if len(got) != 1 {
			t.Fatalf("应解析出 1 条, 实际 %d 条", len(got))
		}
		if got[0].Status != "unknown" {
			t.Errorf("状态码缺失时应为 unknown, 实际 %q", got[0].Status)
		}
		if _, ok := got[0].Extra["deliver_status"]; ok {
			t.Error("DeliverStatus 缺失时不应出现在 Extra（不能填零值冒充）")
		}
	})

	t.Run("字段齐全正常解析", func(t *testing.T) {
		t.Parallel()
		got := client.parseEmailStatusList([]*ses.SendEmailStatus{{
			MessageId:        common.StringPtr("msg-1"),
			ToEmailAddress:   common.StringPtr("to@example.com"),
			FromEmailAddress: common.StringPtr("from@example.com"),
			SendStatus:       common.Int64Ptr(0),
			DeliverStatus:    common.Int64Ptr(1),
			DeliverMessage:   common.StringPtr("delivered"),
			RequestTime:      common.Int64Ptr(1700000000),
			UserOpened:       common.BoolPtr(true),
		}})
		if len(got) != 1 {
			t.Fatalf("应解析出 1 条, 实际 %d 条", len(got))
		}
		s := got[0]
		if s.MessageID != "msg-1" || s.ToAddress != "to@example.com" || s.FromAddress != "from@example.com" {
			t.Errorf("地址字段解析错误: %+v", s)
		}
		if !s.RequestTime.Equal(time.Unix(1700000000, 0)) {
			t.Errorf("请求时间解析错误: %v", s.RequestTime)
		}
		if !s.UserOpened {
			t.Error("UserOpened 应为 true")
		}
		if _, ok := s.Extra["deliver_status"]; !ok {
			t.Error("DeliverStatus 存在时应写入 Extra")
		}
	})
}

// TestResolveRequestDate 验证查询日期解析不突变入参（入参为值类型副本）。
func TestResolveRequestDate(t *testing.T) {
	t.Parallel()

	zero := time.Time{}
	got := resolveRequestDate(zero)
	if got.IsZero() {
		t.Error("零值入参应解析为当前时间")
	}
	if !zero.IsZero() {
		t.Error("函数不得改变调用方持有的入参（值副本语义）")
	}

	nonZero := time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC)
	if got := resolveRequestDate(nonZero); !got.Equal(nonZero) {
		t.Errorf("非零入参应原样返回, 实际 %v", got)
	}
}

// TestTencentSendTemplateEmailEntryValidation 验证模板发送入口校验（无网络调用）。
func TestTencentSendTemplateEmailEntryValidation(t *testing.T) {
	t.Parallel()
	client := mustTencentClient(t)
	ctx := context.Background()

	tests := []struct {
		name       string
		from       string
		to         []string
		templateID uint64
		subject    string
		wantCause  string
	}{
		{"发件人为空", "", []string{"a@b.com"}, 1, "主题", "发件人地址不能为空"},
		{"发件人格式非法", "not-an-email", []string{"a@b.com"}, 1, "主题", "发件人地址格式不合法"},
		{"收件人为空", "a@b.com", nil, 1, "主题", "至少需要一个收件人"},
		{"收件人格式非法", "a@b.com", []string{"bad"}, 1, "主题", "收件人地址格式不合法"},
		{"模板ID为0", "a@b.com", []string{"a@b.com"}, 0, "主题", "模板 ID 不能为空"},
		{"主题为空", "a@b.com", []string{"a@b.com"}, 1, "", "邮件主题不能为空"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := client.SendTemplateEmail(ctx, tt.from, tt.to, tt.templateID, nil, tt.subject)
			if err == nil {
				t.Fatal("非法参数应返回 error")
			}
			if result == nil || result.Status != StatusFailed {
				t.Errorf("失败路径应返回 failed 结果, 实际 %+v", result)
			}
			if !strings.Contains(err.Error(), tt.wantCause) {
				t.Errorf("错误消息应包含 %q, 实际 %q", tt.wantCause, err.Error())
			}
		})
	}
}

// TestBuildSimpleBody 验证 Simple 正文构造的 base64 编码与两字段独立分设（终审 R7-1/R7-2 回归）：
//   - 原实现传原文且 TextBody 只能塞进 Html 字段——服务端按 base64 解码轻则乱码重则报错，
//     纯文本被当 HTML 渲染、两者同传时 TextBody 被静默丢弃；
//   - SDK 字段注释：Html = 「base64之后的Html代码」、Text = 「base64之后的纯文本信息」。
func TestBuildSimpleBody(t *testing.T) {
	t.Parallel()

	encode := base64.StdEncoding.EncodeToString

	tests := []struct {
		name     string
		req      *SendRequest
		wantHtml string // 空表示期望字段为 nil
		wantText string
	}{
		{"仅HTML时Html编码且Text缺省", &SendRequest{HTMLBody: "<p>你好</p>"}, encode([]byte("<p>你好</p>")), ""},
		{"仅纯文本时Text编码且Html缺省", &SendRequest{TextBody: "纯文本\n换行"}, "", encode([]byte("纯文本\n换行"))},
		{"两者同传时各自独立编码不丢字段", &SendRequest{HTMLBody: "<h1>t</h1>", TextBody: "t"},
			encode([]byte("<h1>t</h1>")), encode([]byte("t"))},
		{"都为空时两字段均缺省", &SendRequest{}, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			simple := buildSimpleBody(tt.req)
			if simple == nil {
				t.Fatal("应返回非 nil 的 Simple")
			}
			if tt.wantHtml == "" {
				if simple.Html != nil {
					t.Errorf("Html 应缺省, 实际 %q", *simple.Html)
				}
			} else if simple.Html == nil || *simple.Html != tt.wantHtml {
				t.Errorf("Html 应为 base64 编码 %q, 实际 %v", tt.wantHtml, simple.Html)
			}
			if tt.wantText == "" {
				if simple.Text != nil {
					t.Errorf("Text 应缺省, 实际 %q", *simple.Text)
				}
			} else if simple.Text == nil || *simple.Text != tt.wantText {
				t.Errorf("Text 应为 base64 编码 %q, 实际 %v", tt.wantText, simple.Text)
			}
			// 编码后必须是合法 base64 且解码回原文（防「编码了个寂寞」的写法）
			if simple.Html != nil {
				if _, err := base64.StdEncoding.DecodeString(*simple.Html); err != nil {
					t.Errorf("Html 不是合法 base64: %v", err)
				}
			}
		})
	}
}

// TestTencentNilInputGuard 验证 nil 入参在触碰字段前被拒绝（终审 P0 回归守护）：
// SendBatchEmail 会把切片元素直接交给 SendEmail，nil 元素若解引用 panic
// 会直接打死 worker 所在进程——入口必须 fail loud 而非崩进程。
func TestTencentNilInputGuard(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("SendEmail拒绝nil请求", func(t *testing.T) {
		t.Parallel()
		// 修复前首行即 maskEmail(req.From) 解引用 nil panic
		result, err := mustTencentClient(t).SendEmail(ctx, nil)
		assertNilInputSendGuard(t, "tencent_ses.SendEmail(nil)", result, err)
	})

	t.Run("GetEmailStatus拒绝nil查询", func(t *testing.T) {
		t.Parallel()
		// 修复前首个字段访问是 query.MessageID 解引用 nil panic
		result, err := mustTencentClient(t).GetEmailStatus(ctx, nil)
		if err == nil {
			t.Fatal("nil 查询必返回 error（且不得 panic）")
		}
		if result == nil || result.Status != StatusFailed || result.Error == nil {
			t.Errorf("失败路径应返回 failed 且 Error 非空的结果, 实际 %+v", result)
		}
	})
}
