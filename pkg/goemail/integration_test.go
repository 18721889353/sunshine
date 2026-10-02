//go:build integration

// 本文件为真实外部服务集成测试（带 integration 构建标签）：
//
//	go test -tags=integration -v -count=1 ./pkg/goemail/
//
// 环境变量与 env.example、README「集成测试」章节的变量表一一对应（只读 os.Getenv 常量表）。
// 边界约定：
//   - 凭据缺失 → Skip（环境未就绪是环境选择，不是代码缺陷）；
//   - 凭据已配置仍失败 → Fail（保留原始云错误，不 t.Logf 吞掉）；
//   - 真实发送是付费操作 → 额外要求 GOEMAIL_ALLOW_PAID_SEND=1，未授权即 Skip。
package goemail

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// 集成测试环境变量（缺凭据自动 Skip；真实发送额外要求 GOEMAIL_ALLOW_PAID_SEND=1）：
//
//	GOEMAIL_SMTP_HOST              SMTP 服务器地址
//	GOEMAIL_SMTP_PORT              可选，SMTP 端口（默认 465）
//	GOEMAIL_SMTP_USERNAME          SMTP 用户名（一般为完整邮箱）
//	GOEMAIL_SMTP_PASSWORD          SMTP 密码/授权码
//	GOEMAIL_SMTP_USE_TLS           可选，"true"=强制隐式 SSL（465）；未设置或 "false" 按端口自适应
//	GOEMAIL_TENCENT_SECRET_ID      腾讯云 SecretId（即 AccessKeyID）
//	GOEMAIL_TENCENT_SECRET_KEY     腾讯云 SecretKey
//	GOEMAIL_TENCENT_REGION         可选，腾讯云 SES 区域（默认 ap-hongkong）
//	GOEMAIL_TENCENT_TEMPLATE_ID    腾讯云模板 ID（模板发送用）
//	GOEMAIL_TENCENT_TEMPLATE_DATA  可选，模板变量 JSON 对象（如 {"code":"123456"}）
//	GOEMAIL_ALIYUN_ACCESS_KEY_ID   阿里云 AccessKeyId
//	GOEMAIL_ALIYUN_ACCESS_KEY_SECRET 阿里云 AccessKeySecret
//	GOEMAIL_ALIYUN_REGION          可选，阿里云 DM 区域（默认 cn-hangzhou）
//	GOEMAIL_ALIYUN_RECEIVERS_NAME  阿里云预先创建的收件人列表名称（BatchSendMail 用）
//	GOEMAIL_ALIYUN_TEMPLATE_NAME   阿里云模板名称
//	GOEMAIL_TEST_FROM              测试发件人地址（须为服务商已验证域名）
//	GOEMAIL_TEST_TO                测试收件人地址
//	GOEMAIL_ALLOW_PAID_SEND        设为 "1" 才执行真实发送（付费操作授权开关）
//	GOEMAIL_TEST_TIMEOUT           TestMain 总时长上限（Go duration，默认 120s）
const (
	envSMTPHost            = "GOEMAIL_SMTP_HOST"
	envSMTPPort            = "GOEMAIL_SMTP_PORT"
	envSMTPUsername        = "GOEMAIL_SMTP_USERNAME"
	envSMTPPassword        = "GOEMAIL_SMTP_PASSWORD"
	envSMTPUseTLS          = "GOEMAIL_SMTP_USE_TLS"
	envTencentSecretID     = "GOEMAIL_TENCENT_SECRET_ID"
	envTencentSecretKey    = "GOEMAIL_TENCENT_SECRET_KEY"
	envTencentRegion       = "GOEMAIL_TENCENT_REGION"
	envTencentTemplateID   = "GOEMAIL_TENCENT_TEMPLATE_ID"
	envTencentTemplateData = "GOEMAIL_TENCENT_TEMPLATE_DATA"
	envAliyunAccessKeyID   = "GOEMAIL_ALIYUN_ACCESS_KEY_ID"
	envAliyunAccessKeySec  = "GOEMAIL_ALIYUN_ACCESS_KEY_SECRET"
	envAliyunRegion        = "GOEMAIL_ALIYUN_REGION"
	envAliyunReceiversName = "GOEMAIL_ALIYUN_RECEIVERS_NAME"
	envAliyunTemplateName  = "GOEMAIL_ALIYUN_TEMPLATE_NAME"
	envTestFrom            = "GOEMAIL_TEST_FROM"
	envTestTo              = "GOEMAIL_TEST_TO"
	envAllowPaidSend       = "GOEMAIL_ALLOW_PAID_SEND"
	envTestTimeout         = "GOEMAIL_TEST_TIMEOUT"
)

// defaultIntegrationTimeout TestMain 总时长默认上限。
// 固定等待的必要性：外部 SDK 后台 goroutine 泄漏会让 go test 永远挂起，而挂起的测试拿不到任何读数；
// 取值依据：单次发送 + 查询轮询 ≈ 45s，120s 是约 2.5 倍经验余量（未在真实云环境实测收敛值，
// 可用 GOEMAIL_TEST_TIMEOUT 上调）；不会导致误判——超时直接以退出码 1 失败，不会把断言翻转成通过。
const defaultIntegrationTimeout = 120 * time.Second

// integrationReadyTimeout 真实网络调用的单次等待上限，
// 仅用于失败快速返回（凭据已配置即期望真工作，超时按 Fail 呈现）。
const integrationReadyTimeout = 20 * time.Second

// defaultSMTPPort SMTP 端口未配置时的默认值（与 newSMTPClient 的本地默认保持一致）。
const defaultSMTPPort = 465

func TestMain(m *testing.M) {
	loadDotEnv()

	timeout := defaultIntegrationTimeout
	if v := os.Getenv(envTestTimeout); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			timeout = d
		} else {
			fmt.Fprintf(os.Stderr, "忽略非法 %s=%q（解析失败: %v），使用默认 %s\n",
				envTestTimeout, v, err, defaultIntegrationTimeout)
		}
	}

	// 看门狗：限制总时长，防 SDK goroutine 泄漏导致 go test 永远挂起
	go func() {
		time.Sleep(timeout)
		fmt.Fprintf(os.Stderr, "集成测试总时长超过 %s，强制退出\n", timeout)
		os.Exit(1)
	}()

	os.Exit(m.Run())
}

// loadDotEnv 手写 dotenv 解析（不引入第三方依赖，语义承自原 test/env_loader.go）：
// 依次尝试当前目录与测试二进制所在目录的 .env，取第一个存在的文件解析；
// 只补缺、不覆盖已设置的变量；跳过空行与 # 注释；按第一个 = 分割（值可含 =）；
// 去成对引号；不做变量展开。文件不存在属正常情况（CI 用真实环境变量）。
func loadDotEnv() {
	for _, path := range dotEnvCandidates() {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		if parseDotEnv(path) {
			return // 取第一个存在的文件，与原 test/env_loader.go 的搜索顺序一致
		}
	}
}

// dotEnvCandidates 返回 .env 的候选路径：当前目录优先，其次是测试二进制所在目录。
func dotEnvCandidates() []string {
	paths := []string{".env"}
	if exePath, err := os.Executable(); err == nil {
		paths = append(paths, filepath.Join(filepath.Dir(exePath), ".env"))
	}
	return paths
}

// parseDotEnv 解析单个 .env 文件，返回是否成功读取（打开失败返回 false）。
func parseDotEnv(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue // 只补缺、不覆盖
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		if setErr := os.Setenv(key, value); setErr != nil {
			fmt.Fprintf(os.Stderr, "设置环境变量 %s 失败: %v\n", key, setErr)
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		fmt.Fprintf(os.Stderr, "读取 %s 失败: %v\n", path, scanErr)
	}
	return true
}

// requireEnv 缺少任一必需环境变量时 Skip（环境选择，不是代码缺陷）。
func requireEnv(t *testing.T, names ...string) {
	t.Helper()
	for _, name := range names {
		if os.Getenv(name) == "" {
			t.Skipf("缺少环境变量 %s，集成测试跳过", name)
		}
	}
}

// requirePaidSend 真实发送是付费操作，需显式授权（防 CI 误配凭据白花钱）。
func requirePaidSend(t *testing.T) {
	t.Helper()
	if os.Getenv(envAllowPaidSend) != "1" {
		t.Skipf("未设置 %s=1，跳过付费发送测试", envAllowPaidSend)
	}
}

// smtpPort 解析 SMTP 端口：未设置取 465；已设置但非法直接 Fail（配置错误不静默吞掉）。
func smtpPort(t *testing.T) int {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv(envSMTPPort))
	if raw == "" {
		return defaultSMTPPort
	}
	port, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatalf("%s=%q 不是合法端口: %v", envSMTPPort, raw, err)
	}
	return port
}

// integrationSMTPConfig 组装 SMTP 集成测试配置（凭据由 requireEnv 保证非空）。
func integrationSMTPConfig(t *testing.T) *Config {
	t.Helper()
	return &Config{
		ProviderType: ProviderTypeSMTP,
		SMTPHost:     os.Getenv(envSMTPHost),
		SMTPPort:     smtpPort(t),
		SMTPUsername: os.Getenv(envSMTPUsername),
		SMTPPassword: os.Getenv(envSMTPPassword),
		UseTLS:       strings.EqualFold(strings.TrimSpace(os.Getenv(envSMTPUseTLS)), "true"),
	}
}

// integrationTencentClient 创建腾讯云 SES 客户端（凭据缺失 Skip，创建失败 Fail）。
func integrationTencentClient(t *testing.T) EmailClient {
	t.Helper()
	requireEnv(t, envTencentSecretID, envTencentSecretKey)
	client, err := NewEmailClient(&Config{
		ProviderType: ProviderTypeTencentSES,
		Region:       os.Getenv(envTencentRegion),
		AccessKeyID:  os.Getenv(envTencentSecretID),
		SecretKey:    os.Getenv(envTencentSecretKey),
	})
	if err != nil {
		t.Fatalf("创建腾讯云 SES 客户端失败: %v", err)
	}
	return client
}

// integrationAliyunClient 创建阿里云 DM 客户端（凭据缺失 Skip，创建失败 Fail）。
func integrationAliyunClient(t *testing.T) EmailClient {
	t.Helper()
	requireEnv(t, envAliyunAccessKeyID, envAliyunAccessKeySec)
	client, err := NewEmailClient(&Config{
		ProviderType: ProviderTypeAliyunDM,
		Region:       os.Getenv(envAliyunRegion),
		AccessKeyID:  os.Getenv(envAliyunAccessKeyID),
		SecretKey:    os.Getenv(envAliyunAccessKeySec),
	})
	if err != nil {
		t.Fatalf("创建阿里云 DM 客户端失败: %v", err)
	}
	return client
}

// integrationRequest 组装一封最小可用的测试邮件（发件/收件地址来自环境变量）。
func integrationRequest(subject string) *SendRequest {
	return &SendRequest{
		From:     os.Getenv(envTestFrom),
		To:       []string{os.Getenv(envTestTo)},
		Subject:  subject,
		HTMLBody: "<p>goemail integration test</p>",
		TextBody: "goemail integration test",
	}
}

// assertSendSuccess 统一的成功断言：err == nil 不等于成功，必须看 IsSuccess。
func assertSendSuccess(t *testing.T, result *SendResult, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("发送失败: %v", err)
	}
	if !result.IsSuccess() {
		t.Fatalf("发送状态期望 %s, 实际 %s（Error=%v, Extra=%v）",
			StatusSuccess, result.Status, result.Error, result.Extra)
	}
	t.Logf("发送成功: message_id=%s", result.MessageID)
}

// TestIntegration_SendSMTP 真实发送一封 SMTP 邮件（付费，需 GOEMAIL_ALLOW_PAID_SEND=1）。
func TestIntegration_SendSMTP(t *testing.T) {
	requirePaidSend(t)
	requireEnv(t, envSMTPHost, envSMTPUsername, envSMTPPassword, envTestFrom, envTestTo)

	client, err := NewEmailClient(integrationSMTPConfig(t))
	if err != nil {
		t.Fatalf("创建 SMTP 客户端失败: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), integrationReadyTimeout)
	defer cancel()

	result, err := client.SendEmail(ctx, integrationRequest("goemail integration: SMTP"))
	assertSendSuccess(t, result, err)
}

// TestIntegration_SendBatchSMTP 真实批量发送 SMTP 邮件（付费，验证保序与聚合）。
func TestIntegration_SendBatchSMTP(t *testing.T) {
	requirePaidSend(t)
	requireEnv(t, envSMTPHost, envSMTPUsername, envSMTPPassword, envTestFrom, envTestTo)

	client, err := NewEmailClient(integrationSMTPConfig(t))
	if err != nil {
		t.Fatalf("创建 SMTP 客户端失败: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), integrationReadyTimeout)
	defer cancel()

	reqs := []*SendRequest{
		integrationRequest("goemail integration: batch 1"),
		integrationRequest("goemail integration: batch 2"),
	}
	results, err := client.SendBatchEmail(ctx, reqs)
	if err != nil {
		// 批量单条失败会聚合成 error，但 results 仍应完整逐条返回
		t.Logf("批量聚合 error（含失败明细）: %v", err)
	}
	if len(results) != len(reqs) {
		t.Fatalf("批量结果数应与请求数一致, 期望 %d, 实际 %d", len(reqs), len(results))
	}
	for i, result := range results {
		if result == nil {
			t.Fatalf("第 %d 条结果为空（保序回填丢结果）", i+1)
		}
		if !result.IsSuccess() {
			t.Errorf("第 %d 条未成功: status=%s error=%v", i+1, result.Status, result.Error)
			continue
		}
		t.Logf("第 %d 条成功: message_id=%s", i+1, result.MessageID)
	}
}

// TestIntegration_SendTencent 真实发送腾讯云 SES 邮件（付费，需 GOEMAIL_ALLOW_PAID_SEND=1）。
func TestIntegration_SendTencent(t *testing.T) {
	requirePaidSend(t)
	requireEnv(t, envTestFrom, envTestTo)
	client := integrationTencentClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), integrationReadyTimeout)
	defer cancel()

	result, err := client.SendEmail(ctx, integrationRequest("goemail integration: Tencent SES"))
	assertSendSuccess(t, result, err)
}

// TestIntegration_SendTencentTemplate 真实发送腾讯云模板邮件（付费）。
// 模板变量来自 GOEMAIL_TENCENT_TEMPLATE_DATA（JSON 对象，可不设置）。
func TestIntegration_SendTencentTemplate(t *testing.T) {
	requirePaidSend(t)
	requireEnv(t, envTestFrom, envTestTo, envTencentTemplateID)
	client := integrationTencentClient(t)

	templateID, err := strconv.ParseUint(os.Getenv(envTencentTemplateID), 10, 64)
	if err != nil {
		t.Fatalf("%s=%q 不是合法的模板 ID: %v", envTencentTemplateID, os.Getenv(envTencentTemplateID), err)
	}

	var templateData map[string]interface{}
	if raw := strings.TrimSpace(os.Getenv(envTencentTemplateData)); raw != "" {
		if jsonErr := json.Unmarshal([]byte(raw), &templateData); jsonErr != nil {
			t.Fatalf("%s 不是合法 JSON 对象: %v", envTencentTemplateData, jsonErr)
		}
	}

	tencentClient, ok := client.(*TencentSESClient)
	if !ok {
		t.Fatalf("客户端类型断言失败: %T", client)
	}

	ctx, cancel := context.WithTimeout(context.Background(), integrationReadyTimeout)
	defer cancel()

	result, err := tencentClient.SendTemplateEmail(ctx,
		os.Getenv(envTestFrom),
		[]string{os.Getenv(envTestTo)},
		templateID,
		templateData,
		"goemail integration: Tencent 模板邮件")
	assertSendSuccess(t, result, err)
}

// TestIntegration_QueryTencent 真实查询腾讯云 SES 发送状态（查询不计费，无需付费授权）。
func TestIntegration_QueryTencent(t *testing.T) {
	requireEnv(t, envTestTo)
	client := integrationTencentClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), integrationReadyTimeout)
	defer cancel()

	query := &EmailStatusQuery{
		ToAddress: os.Getenv(envTestTo),
		FromDate:  time.Now().Add(-24 * time.Hour),
		ToDate:    time.Now(),
		Offset:    0,
		Limit:     10,
	}
	result, err := client.GetEmailStatus(ctx, query)
	if err != nil {
		// 凭据已配置仍失败 → 断言级失败（不是 Skip），保留原始云错误便于定位
		t.Fatalf("查询失败: %v", err)
	}
	if result.Status != StatusSuccess {
		t.Fatalf("查询状态期望 %s, 实际 %s（Error=%v）", StatusSuccess, result.Status, result.Error)
	}
	t.Logf("查询成功: %d 条记录（Extra=%v）", len(result.Data), result.Extra)
}

// TestIntegration_SendAliyun 真实发送阿里云 DM 邮件（付费，需 GOEMAIL_ALLOW_PAID_SEND=1）。
func TestIntegration_SendAliyun(t *testing.T) {
	requirePaidSend(t)
	requireEnv(t, envTestFrom, envTestTo)
	client := integrationAliyunClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), integrationReadyTimeout)
	defer cancel()

	result, err := client.SendEmail(ctx, integrationRequest("goemail integration: Aliyun DM"))
	assertSendSuccess(t, result, err)
}

// TestIntegration_SendAliyunTemplate 真实发送阿里云批量模板邮件（付费）。
// receiversName 是预先创建并上传了收件人的收件人列表名称，不是内联地址列表。
func TestIntegration_SendAliyunTemplate(t *testing.T) {
	requirePaidSend(t)
	requireEnv(t, envTestFrom, envAliyunReceiversName, envAliyunTemplateName)
	client := integrationAliyunClient(t)

	aliyunClient, ok := client.(*AliyunDMClient)
	if !ok {
		t.Fatalf("客户端类型断言失败: %T", client)
	}

	ctx, cancel := context.WithTimeout(context.Background(), integrationReadyTimeout)
	defer cancel()

	result, err := aliyunClient.SendTemplateEmail(ctx,
		os.Getenv(envTestFrom),
		os.Getenv(envAliyunReceiversName),
		os.Getenv(envAliyunTemplateName))
	assertSendSuccess(t, result, err)
}
