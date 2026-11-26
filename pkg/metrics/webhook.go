package metrics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// ============================================================================
// 企业微信机器人推送。
//
// 关键口径（从网关移植，线上验证）：
//   - 只有 HTTP 200 且 errcode=0 才算送达——企微在限频/机器人被移除/key 失效时
//     仍返回 HTTP 200 + errcode!=0，只看状态码会把失败当成功，告警静默丢。
//   - 错误信息必须脱敏：URL 含 key，进日志会泄露。
//   - content 截断到 1800 字节（企微 text 上限 2048，留余量）。
// ============================================================================

const (
	// webhookTimeout 单次推送 HTTP 超时（非主链路，短超时即可）。
	webhookTimeout = 5 * time.Second
	// maxContentLen 企业微信 text content 字节上限 2048，留余量。
	maxContentLen = 1800
)

// webhookClient 报告器专属 HTTP client（大厂标准）：
//   - 独立 Transport：不与业务共享 http.DefaultClient 的连接池，
//     避免报告推送占用/污染业务连接，也避免业务侧改全局 Transport 影响报告；
//   - 开启 keep-alive 复用：周期推送走同一 host，复用 TCP 连接降低延迟与 TIME_WAIT；
//   - 限制空闲连接：报告器低频（60s），少量空闲连接即可。
var webhookClient = &http.Client{
	Timeout: webhookTimeout,
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          4,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   3 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	},
}

// webhookKeyPattern 匹配 webhook URL 中的 key 参数（脱敏用）。
var webhookKeyPattern = regexp.MustCompile(`(?i)([?&]key=)[^&\s"']+`)

// PushToWebhook 把报告文本 POST 到企业微信机器人。
// 返回 error 供调用方决定是否提交窗口状态。
// 保持原有签名（无 @ 人）——需要 @ 时用 PushToWebhookWithMentions。
func PushToWebhook(ctx context.Context, webhookURL, content string) error {
	return PushToWebhookWithMentions(ctx, webhookURL, content, nil)
}

// webhookPayload 企微 text 消息体。
// mentioned_list 用 omitempty：无 @ 人时不输出该字段，与旧版 payload 完全一致（向后兼容）。
type webhookPayload struct {
	MsgType string `json:"msgtype"`
	Text    struct {
		Content       string   `json:"content"`
		MentionedList []string `json:"mentioned_list,omitempty"`
	} `json:"text"`
}

// PushToWebhookWithMentions 推送文本并 @ 指定成员（企微 userid 列表，如 ["zhangsan", "@all"]）。
// mentions 为空时行为与 PushToWebhook 完全一致。
func PushToWebhookWithMentions(ctx context.Context, webhookURL, content string, mentions []string) error {
	var text struct {
		Content       string   `json:"content"`
		MentionedList []string `json:"mentioned_list,omitempty"`
	}
	text.Content = truncateUTF8(content, maxContentLen)
	if len(mentions) > 0 {
		text.MentionedList = mentions
	}
	payload, err := json.Marshal(webhookPayload{MsgType: "text", Text: text})
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, webhookTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := webhookClient.Do(req)
	if err != nil {
		return fmt.Errorf("post webhook: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // 关闭响应体的错误不影响结果

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return fmt.Errorf("read webhook response: %w", err)
	}

	// 关键：HTTP 200 还不够，必须 errcode=0（企微限频/失效仍返 200）
	var result struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("webhook 响应非 JSON (http %d): %s", resp.StatusCode, truncateUTF8(string(body), 200))
	}
	if result.ErrCode != 0 {
		return fmt.Errorf("webhook errcode=%d: %s", result.ErrCode, result.ErrMsg)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("webhook http %d", resp.StatusCode)
	}
	return nil
}

// RedactWebhookError 对错误信息脱敏（URL 含 key），供日志使用。
func RedactWebhookError(err error, webhookURL string) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	// 优先用正则脱敏（覆盖错误链中嵌入的 URL）
	redacted := webhookKeyPattern.ReplaceAllString(msg, "${1}***")
	// 兜底：直接替换完整 URL
	if webhookURL != "" {
		redacted = strings.ReplaceAll(redacted, webhookURL, MaskWebhookURL(webhookURL))
	}
	return redacted
}

// MaskWebhookURL 遮蔽 webhook URL 的 key 部分（日志/报告展示用）。
func MaskWebhookURL(raw string) string {
	if raw == "" {
		return ""
	}
	return webhookKeyPattern.ReplaceAllString(raw, "${1}***")
}

// truncateUTF8 按字节截断字符串（保留完整 UTF-8 字符），超长加省略号。
func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	// 为省略号（"…" = 3 字节）预留空间，确保结果不超 maxBytes。
	// 否则 s[:maxBytes] + "…" 会达到 maxBytes+2，超出企微 content 上限。
	limit := maxBytes - len("…")
	if limit < 0 {
		limit = 0
	}
	// 从 limit 往前退到字符边界
	cut := limit
	for cut > 0 && !isUTF8Start(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

// isUTF8Start 判断字节是否为 UTF-8 字符起始字节（非 continuation byte）。
func isUTF8Start(b byte) bool {
	return b&0xC0 != 0x80
}

// IsWeComWebhookHost 判断 host 是否企业微信机器人域名（防误配推到外部）。
// 必须精确匹配（或标准子域后缀），不能用 Contains：
// "qyapi.weixin.qq.com.evil.com" / "evil-qyapi.weixin.qq.com" 含子串但属外部域名，
// 用 Contains 会把推送到攻击者的地址当成安全地址（仅 WARN 不阻断，等于防护失效）。
func IsWeComWebhookHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	const weComHost = "qyapi.weixin.qq.com"
	return host == weComHost || strings.HasSuffix(host, "."+weComHost)
}
