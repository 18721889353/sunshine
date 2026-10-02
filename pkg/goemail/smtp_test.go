package goemail

import (
	"context"
	"os"
	"strings"
	"testing"
)

// TestNewSMTPClient 验证 SMTP 客户端创建的入口防护与端口默认值语义。
func TestNewSMTPClient(t *testing.T) {
	t.Parallel()

	t.Run("缺少服务器地址返回错误", func(t *testing.T) {
		t.Parallel()
		cfg := smtpTestConfig()
		cfg.SMTPHost = ""
		client, err := newSMTPClient(cfg)
		if err == nil {
			t.Fatal("SMTPHost 为空时应返回错误")
		}
		if client != nil {
			t.Errorf("出错时客户端应为 nil, 实际 %v", client)
		}
		if !strings.Contains(err.Error(), "服务器地址不能为空") {
			t.Errorf("错误消息应为中文且说明原因, 实际 %q", err.Error())
		}
	})

	t.Run("缺少用户名或密码返回错误", func(t *testing.T) {
		t.Parallel()
		cfg := smtpTestConfig()
		cfg.SMTPPassword = ""
		if _, err := newSMTPClient(cfg); err == nil {
			t.Fatal("SMTPPassword 为空时应返回错误")
		}
	})

	t.Run("端口为0时本地默认465且不回写共享配置", func(t *testing.T) {
		t.Parallel()
		cfg := smtpTestConfig()
		cfg.SMTPPort = 0
		client, err := newSMTPClient(cfg)
		if err != nil {
			t.Fatalf("创建客户端失败: %v", err)
		}
		if client.port != 465 {
			t.Errorf("默认端口应为 465, 实际 %d", client.port)
		}
		if cfg.SMTPPort != 0 {
			t.Errorf("构造函数不得回写共享 cfg.SMTPPort, 实际 %d", cfg.SMTPPort)
		}
	})
}

// TestSMTPValidateRequest 验证请求校验的中文错误与全量分支。
func TestSMTPValidateRequest(t *testing.T) {
	t.Parallel()
	client := mustSMTPClient(t)
	ctx := context.Background()

	tests := []struct {
		name      string
		mutate    func(*SendRequest)
		wantCause string
	}{
		{"发件人为空", func(r *SendRequest) { r.From = "" }, "发件人地址不能为空"},
		{"发件人格式非法", func(r *SendRequest) { r.From = "not-an-email" }, "发件人地址格式不合法"},
		{"收件人为空", func(r *SendRequest) { r.To = nil }, "至少需要一个收件人"},
		{"收件人格式非法", func(r *SendRequest) { r.To = []string{"bad"} }, "收件人地址格式不合法"},
		{"抄送格式非法", func(r *SendRequest) { r.Cc = []string{"bad"} }, "抄送地址格式不合法"},
		{"密送格式非法", func(r *SendRequest) { r.Bcc = []string{"bad"} }, "密送地址格式不合法"},
		{"回复地址格式非法", func(r *SendRequest) { r.ReplyTo = []string{"bad"} }, "回复地址格式不合法"},
		{"主题为空", func(r *SendRequest) { r.Subject = "" }, "邮件主题不能为空"},
		{"正文全空", func(r *SendRequest) { r.HTMLBody = ""; r.TextBody = "" }, "至少填一个"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := validSMTPRequest()
			tt.mutate(req)
			err := client.validateRequest(ctx, req)
			if err == nil {
				t.Fatal("非法请求应返回错误")
			}
			if !strings.Contains(err.Error(), tt.wantCause) {
				t.Errorf("错误消息应包含 %q, 实际 %q", tt.wantCause, err.Error())
			}
		})
	}

	t.Run("合法请求通过校验", func(t *testing.T) {
		t.Parallel()
		if err := client.validateRequest(ctx, validSMTPRequest()); err != nil {
			t.Errorf("合法请求不应报错, 实际 %v", err)
		}
	})
}

// TestSMTPGetEmailStatus 验证 SMTP 状态查询的提示语义（协议不支持，返回 note）。
func TestSMTPGetEmailStatus(t *testing.T) {
	t.Parallel()
	client := mustSMTPClient(t)

	result, err := client.GetEmailStatus(context.Background(), &EmailStatusQuery{
		MessageID: "msg-1",
	})
	if err != nil {
		t.Fatalf("提示型查询不应报错, 实际 %v", err)
	}
	if result.Status != StatusSuccess {
		t.Errorf("状态应为 %s, 实际 %s", StatusSuccess, result.Status)
	}
	note, _ := result.Extra["note"].(string)
	if !strings.Contains(note, "SMTP协议不支持") {
		t.Errorf("应明确提示协议不支持状态查询, 实际 %q", note)
	}
}

// TestSMTPSendEmailEntryValidation 验证发送入口的校验失败路径：
// 校验不过时不产生任何网络调用，返回 failed 结果且 Error 非空。
func TestSMTPSendEmailEntryValidation(t *testing.T) {
	t.Parallel()
	client := mustSMTPClient(t)

	req := &SendRequest{
		From:     "bad-address",
		Subject:  "主题",
		HTMLBody: "<p>正文</p>",
	}
	result, err := client.SendEmail(context.Background(), req)
	if err == nil {
		t.Fatal("非法请求应返回 error")
	}
	if result == nil {
		t.Fatal("失败路径应返回非 nil result（batchErrors 依赖）")
	}
	if result.Status != StatusFailed {
		t.Errorf("状态应为 %s, 实际 %s", StatusFailed, result.Status)
	}
	if result.Error == nil {
		t.Error("result.Error 应携带失败原因")
	}
}

// TestSMTPAttachmentSafety 验证附件处理的两项安全守护（R1-3 回归）：
//  1. 非法文件名（空名与 . / ..）在拨号前被拒绝——不写盘、不拨号；
//  2. 拨号失败后不留 goemail-attach-* 临时目录（defer 清理生效，防内容残留）。
//
// 拨号目标固定为 127.0.0.1:1（本地不可达端口，TCP 立即拒绝），无外网流量；
// 即便净化逻辑回退，失败也只会打到本地端口而非真实邮件服务器。
func TestSMTPAttachmentSafety(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	localUnreachable := func(t *testing.T) *SMTPClient {
		t.Helper()
		cfg := smtpTestConfig()
		cfg.SMTPHost = "127.0.0.1"
		cfg.SMTPPort = 1
		client, err := newSMTPClient(cfg)
		if err != nil {
			t.Fatalf("创建测试客户端失败: %v", err)
		}
		return client
	}

	// 子测试不并行：临时目录快照需串行读取，避免把对方的在飞目录误判为残留
	t.Run("非法文件名在拨号前被拒绝", func(t *testing.T) {
		client := localUnreachable(t)
		for _, name := range []string{"", ".", "..", "  "} {
			req := validSMTPRequest()
			req.Attachments = []*Attachment{{Filename: name, Content: []byte("x")}}
			result, err := client.SendEmail(ctx, req)
			if err == nil {
				t.Fatalf("附件名 %q 应被拒绝", name)
			}
			if !strings.Contains(err.Error(), "附件文件名不合法") {
				t.Errorf("错误消息应说明附件名不合法, 实际 %q", err.Error())
			}
			if result == nil || result.Status != StatusFailed {
				t.Errorf("失败路径应返回 failed 结果, 实际 %+v", result)
			}
		}
	})

	t.Run("拨号失败后无附件临时目录残留", func(t *testing.T) {
		client := localUnreachable(t)
		before := attachDirNames()

		req := validSMTPRequest()
		req.Attachments = []*Attachment{{Filename: "report.txt", Content: []byte("x")}}
		if _, err := client.SendEmail(ctx, req); err == nil {
			t.Fatal("本地不可达端口的拨号应失败")
		}

		for name := range attachDirNames() {
			if !before[name] {
				t.Errorf("发送失败后残留附件临时目录 %q（defer 清理丢失或路径被改写）", name)
			}
		}
	})
}

// attachDirNames 列出 os.TempDir 下 goemail-attach-* 目录集合（读失败视为空集）。
func attachDirNames() map[string]bool {
	names := make(map[string]bool)
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		return names
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "goemail-attach-") {
			names[e.Name()] = true
		}
	}
	return names
}

// TestSMTPNilInputGuard 验证 nil 入参在触碰字段前被拒绝（终审 P0 回归守护）：
// SendBatchEmail 会把切片元素直接交给 SendEmail，nil 元素若解引用 panic
// 会直接打死 worker 所在进程——入口必须 fail loud 而非崩进程。
func TestSMTPNilInputGuard(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("SendEmail拒绝nil请求", func(t *testing.T) {
		t.Parallel()
		result, err := mustSMTPClient(t).SendEmail(ctx, nil)
		assertNilInputSendGuard(t, "smtp.SendEmail(nil)", result, err)
	})

	t.Run("GetEmailStatus拒绝nil查询", func(t *testing.T) {
		t.Parallel()
		result, err := mustSMTPClient(t).GetEmailStatus(ctx, nil)
		if err == nil {
			t.Fatal("nil 查询必返回 error（且不得 panic）")
		}
		if result == nil || result.Status != StatusFailed || result.Error == nil {
			t.Errorf("失败路径应返回 failed 且 Error 非空的结果, 实际 %+v", result)
		}
	})

	t.Run("批量含nil元素不panic且聚合报错", func(t *testing.T) {
		t.Parallel()
		// 第二条用非法请求走校验失败路径，全程零网络调用
		req := validSMTPRequest()
		req.To = nil
		results, err := mustSMTPClient(t).SendBatchEmail(ctx, []*SendRequest{nil, req})
		if err == nil {
			t.Fatal("含 nil 元素的批量应返回聚合 error")
		}
		if len(results) != 2 {
			t.Fatalf("结果条数应恒等于请求条数, 期望 2, 实际 %d", len(results))
		}
		for i, r := range results {
			if r == nil || r.Status != StatusFailed {
				t.Errorf("第 %d 条应为 failed 结果, 实际 %+v", i+1, r)
			}
		}
		if !strings.Contains(err.Error(), "第 1 条") {
			t.Errorf("聚合错误应指向 nil 元素的第 1 条, 实际 %q", err.Error())
		}
	})
}
