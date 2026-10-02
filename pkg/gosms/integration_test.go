//go:build integration

// 本文件为真实外部服务集成测试（带 integration 构建标签）：
//
//	go test -tags=integration -v -count=1 ./pkg/gosms/
//
// 环境变量与 README「集成测试」章节的变量表一一对应。
package gosms

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// 集成测试环境变量（缺凭据自动 Skip；真实发送额外要求 GOSMS_ALLOW_PAID_SEND=1）：
//
//	GOSMS_TENCENT_SECRET_ID     腾讯云 SecretId
//	GOSMS_TENCENT_SECRET_KEY    腾讯云 SecretKey
//	GOSMS_TENCENT_APP_ID        腾讯云短信应用 ID
//	GOSMS_TENCENT_SIGN_NAME     腾讯云签名（与 GOSMS_TEMPLATE_PARAM 模板匹配）
//	GOSMS_TENCENT_TEMPLATE_ID   腾讯云模板 ID
//	GOSMS_ALIYUN_ACCESS_KEY_ID  阿里云 AccessKeyId
//	GOSMS_ALIYUN_ACCESS_KEY_SECRET 阿里云 AccessKeySecret
//	GOSMS_ALIYUN_SIGN_NAME      阿里云签名
//	GOSMS_ALIYUN_TEMPLATE_ID    阿里云模板 ID
//	GOSMS_TEST_PHONE            测试手机号（国际格式，如 +8613711112222）
//	GOSMS_TEMPLATE_PARAM_KEY    可选，模板参数名（默认 "1"）
//	GOSMS_TEMPLATE_PARAM_VALUE  可选，模板参数值；不设置时发送不带参数的请求
//	GOSMS_ALLOW_PAID_SEND       设为 "1" 才执行真实发送（付费操作授权开关）
//	GOSMS_TEST_TIMEOUT          TestMain 总时长上限（Go duration，默认 120s）
const (
	envTencentSecretID    = "GOSMS_TENCENT_SECRET_ID"
	envTencentSecretKey   = "GOSMS_TENCENT_SECRET_KEY"
	envTencentAppID       = "GOSMS_TENCENT_APP_ID"
	envTencentSignName    = "GOSMS_TENCENT_SIGN_NAME"
	envTencentTemplateID  = "GOSMS_TENCENT_TEMPLATE_ID"
	envAliyunAccessKeyID  = "GOSMS_ALIYUN_ACCESS_KEY_ID"
	envAliyunAccessKeySec = "GOSMS_ALIYUN_ACCESS_KEY_SECRET"
	envAliyunSignName     = "GOSMS_ALIYUN_SIGN_NAME"
	envAliyunTemplateID   = "GOSMS_ALIYUN_TEMPLATE_ID"
	envTestPhone          = "GOSMS_TEST_PHONE"
	envTemplateParamKey   = "GOSMS_TEMPLATE_PARAM_KEY"
	envTemplateParamValue = "GOSMS_TEMPLATE_PARAM_VALUE"
	envAllowPaidSend      = "GOSMS_ALLOW_PAID_SEND"
	envTestTimeout        = "GOSMS_TEST_TIMEOUT"
)

// defaultIntegrationTimeout TestMain 总时长默认上限。
// 固定等待的必要性：外部 SDK 后台 goroutine 泄漏会让 go test 永远挂起，而挂起的测试拿不到任何读数；
// 取值依据：单次发送 + 最多 6 轮轮询 ≈ 45s，120s 是约 2.5 倍经验余量（未在真实云环境实测收敛值，
// 可用 GOSMS_TEST_TIMEOUT 上调）；不会导致误判——超时直接以退出码 1 失败，不会把断言翻转成通过。
const defaultIntegrationTimeout = 120 * time.Second

// integrationReadyTimeout 凭据就绪探针的网络等待上限（秒级），
// 仅用于查询类测试首次调用的失败快速返回；不可达会以 Fail 呈现（凭据已配置即期望真工作）。
const integrationReadyTimeout = 20 * time.Second

func TestMain(m *testing.M) {
	loadDotEnv(".env")

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

// loadDotEnv 手写 dotenv 解析（不引入第三方依赖）：
// 只补缺、不覆盖已设置的变量；跳过空行与 # 注释；按第一个 = 分割（值可含 =）；
// 去成对引号；不做变量展开。
func loadDotEnv(path string) {
	file, err := os.Open(path)
	if err != nil {
		return // 文件不存在属正常情况（CI 用真实环境变量）
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
		_ = os.Setenv(key, value)
	}
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

// integrationTemplateParams 组装模板参数（未设置 GOSMS_TEMPLATE_PARAM_VALUE 时不带参数）。
func integrationTemplateParams() []TemplateParam {
	value := os.Getenv(envTemplateParamValue)
	if value == "" {
		return nil
	}
	key := os.Getenv(envTemplateParamKey)
	if key == "" {
		key = "1"
	}
	return []TemplateParam{{Key: key, Value: value}}
}

// TestIntegration_SendTencent 真实发送腾讯云短信（付费，需 GOSMS_ALLOW_PAID_SEND=1）。
func TestIntegration_SendTencent(t *testing.T) {
	requirePaidSend(t)
	requireEnv(t,
		envTencentSecretID, envTencentSecretKey, envTencentAppID,
		envTencentSignName, envTencentTemplateID, envTestPhone)

	client, err := NewSMSClient(&Config{
		ProviderType:    ProviderTypeTencentSMS,
		AccessKeyID:     os.Getenv(envTencentSecretID),
		SecretKey:       os.Getenv(envTencentSecretKey),
		TencentAppID:    os.Getenv(envTencentAppID),
		TencentSignName: os.Getenv(envTencentSignName),
	})
	if err != nil {
		t.Fatalf("创建腾讯云客户端失败: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), integrationReadyTimeout)
	defer cancel()

	result, err := client.SendSMS(ctx, &SendRequest{
		PhoneNumbers:   []string{os.Getenv(envTestPhone)},
		TemplateID:     os.Getenv(envTencentTemplateID),
		SignName:       os.Getenv(envTencentSignName),
		TemplateParams: integrationTemplateParams(),
	})
	if err != nil {
		t.Fatalf("发送失败: %v", err)
	}
	if result.Status != StatusSuccess {
		t.Fatalf("发送状态期望 %s, 实际 %s（Extra=%v, Error=%v）",
			StatusSuccess, result.Status, result.Extra, result.Error)
	}
	t.Logf("发送成功: message_id=%s", result.MessageID)
}

// TestIntegration_QueryTencent 真实查询腾讯云短信状态（查询不计费）。
func TestIntegration_QueryTencent(t *testing.T) {
	requireEnv(t,
		envTencentSecretID, envTencentSecretKey, envTencentAppID, envTestPhone)

	client, err := NewSMSClient(&Config{
		ProviderType:    ProviderTypeTencentSMS,
		AccessKeyID:     os.Getenv(envTencentSecretID),
		SecretKey:       os.Getenv(envTencentSecretKey),
		TencentAppID:    os.Getenv(envTencentAppID),
		TencentSignName: os.Getenv(envTencentSignName),
	})
	if err != nil {
		t.Fatalf("创建腾讯云客户端失败: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), integrationReadyTimeout)
	defer cancel()

	result, err := client.GetSMSStatus(ctx, &SMSStatusQuery{
		PhoneNumber: os.Getenv(envTestPhone),
		FromDate:    time.Now().Add(-24 * time.Hour),
		ToDate:      time.Now(),
		Limit:       10,
	})
	if err != nil {
		// 凭据已配置仍失败 → 断言级失败（不是 Skip），保留原始云错误便于定位
		t.Fatalf("查询失败: %v", err)
	}
	if result.Status != StatusSuccess {
		t.Fatalf("查询状态期望 %s, 实际 %s（Error=%v）", StatusSuccess, result.Status, result.Error)
	}
	t.Logf("查询成功: %d 条记录", len(result.Data))
}

// TestIntegration_SendAliyun 真实发送阿里云短信（付费，需 GOSMS_ALLOW_PAID_SEND=1）。
func TestIntegration_SendAliyun(t *testing.T) {
	requirePaidSend(t)
	requireEnv(t,
		envAliyunAccessKeyID, envAliyunAccessKeySec,
		envAliyunSignName, envAliyunTemplateID, envTestPhone)

	client, err := NewSMSClient(&Config{
		ProviderType:   ProviderTypeAliyunSMS,
		AccessKeyID:    os.Getenv(envAliyunAccessKeyID),
		SecretKey:      os.Getenv(envAliyunAccessKeySec),
		AliyunSignName: os.Getenv(envAliyunSignName),
	})
	if err != nil {
		t.Fatalf("创建阿里云客户端失败: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), integrationReadyTimeout)
	defer cancel()

	result, err := client.SendSMS(ctx, &SendRequest{
		PhoneNumbers:   []string{os.Getenv(envTestPhone)},
		TemplateID:     os.Getenv(envAliyunTemplateID),
		SignName:       os.Getenv(envAliyunSignName),
		TemplateParams: integrationTemplateParams(),
	})
	if err != nil {
		t.Fatalf("发送失败: %v", err)
	}
	if result.Status != StatusSuccess {
		t.Fatalf("发送状态期望 %s, 实际 %s（Extra=%v, Error=%v）",
			StatusSuccess, result.Status, result.Extra, result.Error)
	}
	t.Logf("发送成功: message_id=%s", result.MessageID)
}

// TestIntegration_QueryAliyun 真实查询阿里云短信状态（查询不计费）。
func TestIntegration_QueryAliyun(t *testing.T) {
	requireEnv(t, envAliyunAccessKeyID, envAliyunAccessKeySec, envTestPhone)

	client, err := NewSMSClient(&Config{
		ProviderType:   ProviderTypeAliyunSMS,
		AccessKeyID:    os.Getenv(envAliyunAccessKeyID),
		SecretKey:      os.Getenv(envAliyunAccessKeySec),
		AliyunSignName: os.Getenv(envAliyunSignName),
	})
	if err != nil {
		t.Fatalf("创建阿里云客户端失败: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), integrationReadyTimeout)
	defer cancel()

	result, err := client.GetSMSStatus(ctx, &SMSStatusQuery{
		PhoneNumber: os.Getenv(envTestPhone),
		FromDate:    time.Now().Add(-24 * time.Hour),
		ToDate:      time.Now(),
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if result.Status != StatusSuccess {
		t.Fatalf("查询状态期望 %s, 实际 %s（Error=%v）", StatusSuccess, result.Status, result.Error)
	}
	t.Logf("查询成功: %d 条记录", len(result.Data))
}
