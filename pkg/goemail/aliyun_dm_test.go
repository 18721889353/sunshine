package goemail

import (
	"context"
	"strings"
	"testing"
)

// TestNewAliyunDMClient 验证阿里云 DM 客户端创建的入口防护与区域不回写。
func TestNewAliyunDMClient(t *testing.T) {
	t.Parallel()

	t.Run("缺少密钥返回错误", func(t *testing.T) {
		t.Parallel()
		cfg := aliyunTestConfig()
		cfg.SecretKey = ""
		client, err := newAliyunDMClient(cfg)
		if err == nil {
			t.Fatal("SecretKey 为空时应返回错误")
		}
		if client != nil {
			t.Errorf("出错时客户端应为 nil, 实际 %v", client)
		}
		if !strings.Contains(err.Error(), "阿里云 DM") {
			t.Errorf("错误消息应为中文且说明服务商, 实际 %q", err.Error())
		}
	})

	t.Run("区域为空时本地默认杭州且不回写共享配置", func(t *testing.T) {
		t.Parallel()
		cfg := aliyunTestConfig()
		cfg.Region = ""
		client, err := newAliyunDMClient(cfg)
		if err != nil {
			t.Fatalf("创建客户端失败: %v", err)
		}
		if client.region != "cn-hangzhou" {
			t.Errorf("默认区域应为 cn-hangzhou, 实际 %q", client.region)
		}
		if cfg.Region != "" {
			t.Errorf("构造函数不得回写共享 cfg.Region, 实际 %q", cfg.Region)
		}
	})
}

// TestAliyunValidateRequest 验证请求校验的中文错误与云侧限制分支。
func TestAliyunValidateRequest(t *testing.T) {
	t.Parallel()
	client := mustAliyunClient(t)
	ctx := context.Background()

	t.Run("发件人为空", func(t *testing.T) {
		t.Parallel()
		req := validSMTPRequest()
		req.From = ""
		err := client.validateRequest(ctx, req)
		if err == nil || !strings.Contains(err.Error(), "发件人地址不能为空") {
			t.Errorf("期望中文错误, 实际 %v", err)
		}
	})

	t.Run("收件人超过100个返回错误", func(t *testing.T) {
		t.Parallel()
		req := validSMTPRequest()
		req.To = make([]string, 101)
		for i := range req.To {
			req.To[i] = "user@example.com"
		}
		err := client.validateRequest(ctx, req)
		if err == nil || !strings.Contains(err.Error(), "100") {
			t.Errorf("应触发单次 100 收件人限制, 实际 %v", err)
		}
	})

	t.Run("恰好100个收件人通过", func(t *testing.T) {
		t.Parallel()
		req := validSMTPRequest()
		req.To = make([]string, 100)
		for i := range req.To {
			req.To[i] = "user@example.com"
		}
		if err := client.validateRequest(ctx, req); err != nil {
			t.Errorf("100 个收件人应在限制内, 实际 %v", err)
		}
	})

	t.Run("合法请求通过校验", func(t *testing.T) {
		t.Parallel()
		if err := client.validateRequest(ctx, validSMTPRequest()); err != nil {
			t.Errorf("合法请求不应报错, 实际 %v", err)
		}
	})
}

// TestAliyunSendTemplateEmailEntryValidation 验证模板发送入口校验：
// receiversName 是预先创建的收件人列表名称（非地址列表），为空即拒绝，无网络调用。
func TestAliyunSendTemplateEmailEntryValidation(t *testing.T) {
	t.Parallel()
	client := mustAliyunClient(t)
	ctx := context.Background()

	tests := []struct {
		name          string
		from          string
		receiversName string
		templateName  string
		wantCause     string
	}{
		{"发件人为空", "", "my_receivers", "tpl", "发件人地址不能为空"},
		{"发件人格式非法", "not-an-email", "my_receivers", "tpl", "发件人地址格式不合法"},
		{"收件人列表名为空", "a@b.com", "", "tpl", "收件人列表名称不能为空"},
		{"模板名为空", "a@b.com", "my_receivers", "", "模板名称不能为空"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := client.SendTemplateEmail(ctx, tt.from, tt.receiversName, tt.templateName)
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

// TestAliyunGetEmailStatusNote 验证状态查询的提示语义（阿里云无单封查询 API）。
func TestAliyunGetEmailStatusNote(t *testing.T) {
	t.Parallel()
	client := mustAliyunClient(t)

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
	if !strings.Contains(note, "GetTrackListByMailFromAndTagName") {
		t.Errorf("应指引替代查询方式, 实际 %q", note)
	}
}

// TestAliyunNilInputGuard 验证 nil 入参在触碰字段前被拒绝（终审 P0 回归守护）：
// SendBatchEmail 会把切片元素直接交给 SendEmail，nil 元素若解引用 panic
// 会直接打死 worker 所在进程——入口必须 fail loud 而非崩进程。
func TestAliyunNilInputGuard(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("SendEmail拒绝nil请求", func(t *testing.T) {
		t.Parallel()
		// 修复前首行即 maskEmail(req.From) 解引用 nil panic
		result, err := mustAliyunClient(t).SendEmail(ctx, nil)
		assertNilInputSendGuard(t, "aliyun_dm.SendEmail(nil)", result, err)
	})

	t.Run("GetEmailStatus拒绝nil查询", func(t *testing.T) {
		t.Parallel()
		// 修复前首个字段访问是 query.MessageID 解引用 nil panic
		result, err := mustAliyunClient(t).GetEmailStatus(ctx, nil)
		if err == nil {
			t.Fatal("nil 查询必返回 error（且不得 panic）")
		}
		if result == nil || result.Status != StatusFailed || result.Error == nil {
			t.Errorf("失败路径应返回 failed 且 Error 非空的结果, 实际 %+v", result)
		}
	})
}
