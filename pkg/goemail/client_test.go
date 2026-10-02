package goemail

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
)

// TestValidateEmail 测试邮箱格式校验（表驱动，子测试名中文）。
func TestValidateEmail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		email string
		want  bool
	}{
		{"普通邮箱合法", "test@example.com", true},
		{"QQ邮箱合法", "123456@qq.com", true},
		{"163邮箱合法", "user@163.com", true},
		{"缺少@符号非法", "testexample.com", false},
		{"缺少本地部分非法", "@example.com", false},
		{"缺少域名非法", "test@", false},
		{"空串非法", "", false},
		{"域名无点非法", "test@example", false},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ValidateEmail(ctx, tt.email); got != tt.want {
				t.Errorf("ValidateEmail(%q) = %v, 期望 %v", tt.email, got, tt.want)
			}
		})
	}
}

// TestValidateEmails 测试批量邮箱校验：返回的是非法列表而非错误。
func TestValidateEmails(t *testing.T) {
	t.Parallel()

	emails := []string{
		"valid@example.com",
		"invalid",
		"another@test.com",
		"@bad.com",
	}

	invalid := ValidateEmails(context.Background(), emails)
	if len(invalid) != 2 {
		t.Errorf("期望 2 个非法地址, 实际 %d: %v", len(invalid), invalid)
	}
}

// TestNewEmailClient 测试客户端创建入口的防护与正常分支。
func TestNewEmailClient(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     *Config
		wantErr bool
	}{
		{
			name: "SMTP客户端创建成功",
			cfg: &Config{
				ProviderType: ProviderTypeSMTP,
				SMTPHost:     "smtp.qq.com",
				SMTPPort:     465,
				SMTPUsername: "test@qq.com",
				SMTPPassword: "password",
			},
			wantErr: false,
		},
		{
			name: "SMTP缺少服务器地址返回错误",
			cfg: &Config{
				ProviderType: ProviderTypeSMTP,
				SMTPUsername: "test@qq.com",
				SMTPPassword: "password",
			},
			wantErr: true,
		},
		{
			name:    "不支持的服务商返回错误",
			cfg:     &Config{ProviderType: ProviderType("unknown")},
			wantErr: true,
		},
		{
			// 终审 P0：nil cfg 曾直接 panic——同仓其他包的 New* 均有此防护，
			// 构造入口必须 fail loud 而非崩进程
			name:    "nil配置返回错误",
			cfg:     nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client, err := NewEmailClient(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewEmailClient() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && client != nil {
				t.Errorf("出错时客户端应为 nil, 实际 %v", client)
			}
		})
	}
}

// TestSendResultIsSuccess 验证 IsSuccess 判定助手（nil-safe + err==nil≠成功）。
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

// TestMaskEmail 验证邮箱脱敏（PII）的边界：本地部分掩码、域名保留。
func TestMaskEmail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		email string
		want  string
	}{
		{"空串原样", "", ""},
		{"本地部分保留首2尾2", "zhangsan@example.com", "zh****an@example.com"},
		{"短本地部分整体掩码", "ab@x.com", "**@x.com"},
		{"恰好4位本地部分整体掩码", "abcd@x.com", "****@x.com"},
		{"格式非法也脱敏（无@）", "plaintext", "pl*****xt"},
		{"格式非法短串整体掩码", "abc", "***"},
		{"短中文本地部分整体掩码", "用户@例子.中国", "**@例子.中国"},
		{"长中文本地部分保留首2尾2", "中文用户名@x.com", "中文*户名@x.com"},
		{"格式非法中文按字符脱敏", "中文测试", "****"},
		{"格式非法长中文按字符脱敏", "中文用户名字", "中文**名字"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := maskEmail(tt.email)
			if got != tt.want {
				t.Errorf("maskEmail(%q) = %q, 期望 %q", tt.email, got, tt.want)
			}
		})
	}

	// 脱敏后不得包含完整本地部分（PII 守护）
	got := maskEmail("zhangsan@example.com")
	if strings.Contains(got, "zhangsan") {
		t.Errorf("脱敏结果不应包含完整本地部分, 实际 %q", got)
	}
	if !strings.HasSuffix(got, "@example.com") {
		t.Errorf("域名应原样保留便于排查投递问题, 实际 %q", got)
	}
}

// TestMaskSecret 验证密钥脱敏的边界。
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

// TestMaskAccessKey 验证密钥标识脱敏：保留首4尾4、短串全覆盖。
func TestMaskAccessKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		accessKey string
		want      string
	}{
		{"空串", "", ""},
		{"长度8以内整体掩码", "abcd1234", "********"},
		{"长标识保留首4尾4", "AKIDabcdefghijkl", "AKID********ijkl"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := maskAccessKey(tt.accessKey)
			if got != tt.want {
				t.Errorf("maskAccessKey(%q) = %q, 期望 %q", tt.accessKey, got, tt.want)
			}
		})
	}
}

// TestConfigString 验证 Config 的 Stringer 脱敏：走 Stringer 的格式化路径
// （%v/%+v/%s/%q）不落密钥明文；%#v 属于 Go 语法还原、不走 Stringer，已在
// String() 注释中声明为禁用路径，故此处不覆盖。
func TestConfigString(t *testing.T) {
	t.Parallel()

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

	for _, format := range []string{"%v", "%+v", "%s", "%q"} {
		out := fmt.Sprintf(format, cfg)
		if strings.Contains(out, cfg.SecretKey) {
			t.Errorf("%s 打印不得包含 SecretKey 明文, 实际输出: %s", format, out)
		}
		if strings.Contains(out, cfg.SMTPPassword) {
			t.Errorf("%s 打印不得包含 SMTPPassword 明文, 实际输出: %s", format, out)
		}
		if strings.Contains(out, cfg.AccessKeyID) {
			t.Errorf("%s 打印不得包含 AccessKeyID 明文（与 SecretKey 组合使用）, 实际输出: %s", format, out)
		}
		if !strings.Contains(out, maskSecret(cfg.SecretKey)) {
			t.Errorf("%s 打印应包含脱敏后的 SecretKey, 实际输出: %s", format, out)
		}
		if !strings.Contains(out, "smtp.example.com") {
			t.Errorf("%s 打印应保留非敏感字段便于排查, 实际输出: %s", format, out)
		}
	}

	// 指针接收同样生效（Config 存储为 *Config）
	out := fmt.Sprintf("%+v", &cfg)
	if strings.Contains(out, cfg.SecretKey) || strings.Contains(out, cfg.SMTPPassword) {
		t.Errorf("指针打印同样不得泄露密钥明文, 实际输出: %s", out)
	}
}

// TestRequestIDAttr 验证 request_id 属性提取的三分支（有值/无值/nil ctx）。
func TestRequestIDAttr(t *testing.T) {
	t.Parallel()

	t.Run("上下文含request_id", func(t *testing.T) {
		t.Parallel()
		ctx := context.WithValue(context.Background(), logger.ContextKeyRequestID, "req-123")
		kv := requestIDAttr(ctx)
		if kv.Key != attribute.Key("email.request_id") {
			t.Errorf("属性键应为 email.request_id, 实际 %s", kv.Key)
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

// TestBatchErrors 验证批量失败聚合：逐条带序号、全成功返回 nil、防御分支齐全。
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

// TestResolveBatchConcurrency 验证批量并发上限的解析：零值/负值回落默认 10，正值原样生效。
func TestResolveBatchConcurrency(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		n    int
		want int
	}{
		{"零值用默认10", 0, 10},
		{"负值用默认10", -3, 10},
		{"正值原样生效", 3, 3},
		{"大值不封顶", 100, 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := resolveBatchConcurrency(tt.n); got != tt.want {
				t.Errorf("resolveBatchConcurrency(%d) = %d, 期望 %d", tt.n, got, tt.want)
			}
		})
	}
}

// TestRunBatchEmail 验证批量并发骨架的保序、并发上限与聚合。
func TestRunBatchEmail(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("空列表返回空结果与nilerror", func(t *testing.T) {
		t.Parallel()
		results, err := runBatchEmail(ctx, nil, 0, func(context.Context, *SendRequest) (*SendResult, error) {
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
			reqs[i] = &SendRequest{Subject: fmt.Sprintf("req-%d", i)}
		}
		results, err := runBatchEmail(ctx, reqs, 0, func(_ context.Context, r *SendRequest) (*SendResult, error) {
			if r.Subject == "req-7" {
				return &SendResult{Status: StatusFailed, Error: errors.New("boom")}, nil
			}
			return &SendResult{Status: StatusSuccess, MessageID: r.Subject}, nil
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
			if got.MessageID != want {
				t.Errorf("输出顺序应与请求顺序一致: 第 %d 条 = %q, 期望 %q", i+1, got.MessageID, want)
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
		_, err := runBatchEmail(ctx, reqs, 0, func(context.Context, *SendRequest) (*SendResult, error) {
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
		_, err := runBatchEmail(ctx, reqs, 3, func(context.Context, *SendRequest) (*SendResult, error) {
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
		// 预取消 ctx：worker 在首次 select 即命中 Done，一条都不应发出去（终审 P2-2）
		canceledCtx, cancel := context.WithCancel(context.Background())
		cancel()

		var sent int32
		reqs := make([]*SendRequest, 15)
		for i := range reqs {
			reqs[i] = &SendRequest{}
		}
		results, err := runBatchEmail(canceledCtx, reqs, 0, func(context.Context, *SendRequest) (*SendResult, error) {
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
		results, err := runBatchEmail(ctx2, reqs, 0, func(context.Context, *SendRequest) (*SendResult, error) {
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
