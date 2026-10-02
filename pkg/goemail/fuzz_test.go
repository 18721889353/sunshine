package goemail

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// 本文件为 fuzz 不变量守护（种子随常规 go test 执行，无需任何环境变量）。
// 约定：并发执行的 send 回调里不碰 testing.T（断言一律在 runBatchEmail 返回后做），
// 否则回调与测试框架之间会产生竞态。

// FuzzIsValidEmail 守护邮箱校验的不变量（种子随常规 go test 执行）：
//  1. 对任意输入不 panic；
//  2. 校验通过的地址必含 @，且 @ 不在首尾、本地部分与域名均非空；
//  3. 校验通过的地址域名必含点（与 isValidEmail 的判定口径一致）。
//
// 说明：导出的 ValidateEmail 只是在本函数外包了一层日志与 span，其外壳行为由
// TestValidateEmail 覆盖，fuzz 只压纯函数核心，避免每次迭代写一条 Info 日志。
func FuzzIsValidEmail(f *testing.F) {
	seeds := []string{
		"test@example.com",
		"a@b.c",
		"123456@qq.com",
		"user@163.com",
		"",
		"plaintext",
		"@",
		"a@",
		"@b.com",
		"user@localhost",
		"a@@b.com",
		"a@b@c.com",
		"用户@例子.中国",
		" spaced@example.com ",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, email string) {
		valid := isValidEmail(email)
		if !valid {
			return
		}
		at := strings.Index(email, "@")
		if at <= 0 || at >= len(email)-1 {
			t.Fatalf("校验通过的地址 @ 不得位于首尾: %q (at=%d, len=%d)", email, at, len(email))
		}
		local, domain := email[:at], email[at+1:]
		if local == "" || domain == "" {
			t.Fatalf("校验通过的地址本地部分与域名都不得为空: %q", email)
		}
		if !strings.Contains(domain, ".") {
			t.Fatalf("校验通过的地址域名必含点: %q", email)
		}
	})
}

// FuzzMaskEmail 守护 PII 脱敏的不变量：
//  1. 对任意输入不 panic；
//  2. 长度守恒——掩码只替换字符、不增删长度（日志字段对齐不被打乱）；
//  3. 形如邮箱的输入，@域名 原样保留在末尾（排查投递问题靠域名）；
//  4. 本地部分按结构掩码——短的（≤4）整体为 *，长的保留首 2 尾 2、中间全为 *。
//
// 退化输入 "00*00@0" 的回归语料保留在 testdata/fuzz/FuzzMaskEmail/：
// 它掩码后恰好与原串相等（中段本就是 *），用来守护断言不退化成「输出 ≠ 输入」这种
// 会被巧合击穿的写法。
func FuzzMaskEmail(f *testing.F) {
	seeds := []string{
		"zhangsan@example.com",
		"ab@x.com",
		"abcd@x.com",
		"plaintext",
		"abc",
		"",
		"a@b",
		"用户@例子.中国",
		"a@@b.com",
		" trailing @space.com",
		"****",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, email string) {
		got := maskEmail(email)
		// 长度守恒按 rune 断言：掩码按字符粒度（多字节输入按 byte 切分会截断 UTF-8，
		// 输出不再有效 UTF-8），byte 长度对非 ASCII 输入不守恒是预期行为
		if len([]rune(got)) != len([]rune(email)) {
			t.Fatalf("脱敏必须按字符长度守恒: maskEmail(%q) = %q", email, got)
		}
		if email == "" {
			return
		}

		// 与 maskEmail 实现一致：按最后一个 @ 切分本地部分与域名（@ 为 ASCII，
		// 不可能是多字节字符的组成部分，LastIndex 结果必在 rune 边界上）
		at := strings.LastIndex(email, "@")
		if at <= 0 || at >= len(email)-1 {
			return // 格式非法：只要求字符长度守恒与不 panic
		}

		local, domain := email[:at], email[at+1:]
		if !strings.HasSuffix(got, "@"+domain) {
			t.Fatalf("@域名 应原样保留: maskEmail(%q) = %q, 期望以 %q 结尾", email, got, "@"+domain)
		}
		gotLocal := strings.TrimSuffix(got, "@"+domain)
		localRunes := []rune(local)
		if len(localRunes) <= 4 {
			if strings.Trim(gotLocal, "*") != "" {
				t.Fatalf("短本地部分应整体替换为 *: %q → %q", local, gotLocal)
			}
			return
		}
		// 长本地部分：保留首 2 尾 2、中间全部替换为 *（按字符粒度；结构断言而非
		// 「输出 ≠ 输入」——退化输入如 "00*00@0" 掩码后恰好与原串相等，但其中间段本就是 *，并无明文泄漏）
		wantLocal := string(localRunes[:2]) + strings.Repeat("*", len(localRunes)-4) + string(localRunes[len(localRunes)-2:])
		if gotLocal != wantLocal {
			t.Fatalf("长本地部分应保留首2尾2、中间全为 *: %q → %q, 期望 %q",
				local, gotLocal, wantLocal)
		}
		if middle := string(localRunes[2 : len(localRunes)-2]); strings.Trim(middle, "*") != "" && gotLocal == local {
			t.Fatalf("中间段含明文字符时输出必须与原串不同（PII 泄漏）: %q", local)
		}
	})
}

// FuzzSendFailFast 守护「校验失败不留半截产物 + 错误消息可安全打印」的不变量：
//  1. 校验不过时必返回非 nil error 与 failed 结果（SendBatchEmail 的聚合依赖）；
//  2. 半截产物为空——不返回 MessageID/Extra 等成功侧字段（调用方不会误读半截成功）；
//  3. 错误消息不含裸换行——外部可控值走 %q 转义，单行日志与 span 不被注入断行；
//  4. nil req / nil query 在触碰字段前 fail fast 而非 panic（终审 P0 回归种子：
//     入参侧 nil 曾直接解引用打死进程，仅测「置空必填字段」测不到该路径）。
//
// 各条失败路径都不产生任何网络调用（云调用在所有校验之后）。
func FuzzSendFailFast(f *testing.F) {
	seeds := []string{
		"sender@example.com",
		"",
		"bad-address",
		"line\nbreak",
		"quote\"inside",
		"名前 <a@b.com>",
		"@leading-dot.com",
		"semi;colon@example.com",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, from string) {
		ctx := context.Background()

		// 三个 provider 的测试客户端（new*Client 仅构造、无网络）
		smtpClient := mustSMTPClient(t)
		aliyunClient := mustAliyunClient(t)
		tencentClient := mustTencentClient(t)

		// SMTP：To 置空 → 必在校验处失败，绝不拨号
		smtpResult, smtpErr := smtpClient.SendEmail(ctx, &SendRequest{
			From:     from,
			Subject:  "主题",
			HTMLBody: "<p>正文</p>",
		})
		assertFailFast(t, "smtp.SendEmail", smtpResult, smtpErr)

		// 阿里云模板入口：receiversName 置空 → 必在参数校验处失败，绝不调云
		aliyunResult, aliyunErr := aliyunClient.SendTemplateEmail(ctx, from, "", "tpl")
		assertFailFast(t, "aliyun.SendTemplateEmail", aliyunResult, aliyunErr)

		// nil req / nil query：三 provider 必须返回 failed+error 而非 panic
		for _, tc := range []struct {
			name   string
			client EmailClient
		}{
			{"smtp", smtpClient},
			{"tencent_ses", tencentClient},
			{"aliyun_dm", aliyunClient},
		} {
			nilResult, nilErr := tc.client.SendEmail(ctx, nil)
			assertFailFast(t, tc.name+".SendEmail(nil)", nilResult, nilErr)

			if _, nilQueryErr := tc.client.GetEmailStatus(ctx, nil); nilQueryErr == nil {
				t.Fatalf("%s.GetEmailStatus(nil): nil 入参必返回 error", tc.name)
			}
		}
	})
}

// assertFailFast 校验失败路径的统一断言（含「无半截产物」「错误消息不含裸换行」）。
func assertFailFast(t *testing.T, where string, result *SendResult, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: 校验失败必返回 error", where)
	}
	msg := err.Error()
	if msg == "" {
		t.Fatalf("%s: 错误消息不得为空", where)
	}
	if strings.ContainsAny(msg, "\r\n") {
		t.Fatalf("%s: 错误消息不得含裸换行（外部值须 %%q 转义）: %q", where, msg)
	}
	if result == nil {
		t.Fatalf("%s: 失败路径必返回非 nil result（batchErrors 依赖）", where)
	}
	if result.Status != StatusFailed {
		t.Fatalf("%s: 失败状态应为 %s, 实际 %s", where, StatusFailed, result.Status)
	}
	if result.Error == nil {
		t.Fatalf("%s: result.Error 应携带失败原因", where)
	}
	if result.MessageID != "" {
		t.Fatalf("%s: 校验失败不得返回半截 MessageID: %q", where, result.MessageID)
	}
	if result.Extra != nil {
		t.Fatalf("%s: 校验失败不得返回半截 Extra: %v", where, result.Extra)
	}
}

// FuzzRunBatchOrdering 守护批量并发骨架的保序与聚合不变量：
//  1. 结果条数恒等于请求条数（并发不丢结果，含 ctx 取消后的回填路径）；
//  2. 输出顺序恒等于请求顺序（results[i] 对应 reqs[i]，「第 N 条」聚合序号才准确）;
//  3. 注入失败时聚合 error 带正确的输入序号，全成功时聚合为 nil。
//
// send 回调只做纯计算，断言全部在 runBatchEmail 返回后执行。
func FuzzRunBatchOrdering(f *testing.F) {
	for _, seed := range []int{0, 1, 2, 5, 10, 30, 64, 100, -1} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, n int) {
		size := n % 65
		if size < 0 {
			size = -size
		}

		reqs := make([]*SendRequest, size)
		indexOf := make(map[string]int, size)
		for i := range reqs {
			reqs[i] = &SendRequest{Subject: fmt.Sprintf("req-%d", i)}
			indexOf[reqs[i].Subject] = i
		}

		// 回调内不触碰 testing.T：它在 worker goroutine 中并发执行
		send := func(_ context.Context, r *SendRequest) (*SendResult, error) {
			if indexOf[r.Subject]%5 == 0 {
				// 业务失败返回 err==nil + failed 结果（云侧真实形态），不走 Warn 日志分支
				return &SendResult{Status: StatusFailed, Error: errors.New("注入失败")}, nil
			}
			return &SendResult{Status: StatusSuccess, MessageID: r.Subject}, nil
		}

		results, err := runBatchEmail(context.Background(), reqs, 0, send)

		if len(results) != size {
			t.Fatalf("结果条数应恒等于请求条数: 期望 %d, 实际 %d", size, len(results))
		}

		wantFailCount, lastFailIdx := 0, -1
		for i, got := range results {
			if got == nil {
				t.Fatalf("第 %d 条结果为空（并发写入丢结果）", i+1)
			}
			if i%5 == 0 {
				wantFailCount++
				lastFailIdx = i
				if got.Status != StatusFailed {
					t.Fatalf("第 %d 条应为注入失败, 实际 %s", i+1, got.Status)
				}
				continue
			}
			if got.MessageID != reqs[i].Subject {
				t.Fatalf("输出顺序应与请求顺序一致: 第 %d 条 = %q, 期望 %q",
					i+1, got.MessageID, reqs[i].Subject)
			}
		}

		if wantFailCount == 0 {
			if err != nil {
				t.Fatalf("全部成功时聚合 error 应为 nil, 实际 %v", err)
			}
			return
		}
		if err == nil {
			t.Fatalf("存在 %d 条失败时应返回聚合 error", wantFailCount)
		}
		wantSeq := fmt.Sprintf("第 %d 条", lastFailIdx+1)
		if !strings.Contains(err.Error(), wantSeq) {
			t.Fatalf("聚合错误应含输入序号 %q, 实际 %q", wantSeq, err.Error())
		}
	})
}
