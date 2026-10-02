// Package goemail 提供多平台邮件发送功能
// 支持腾讯云SES、阿里云DM、SMTP等多种发送方式
package goemail

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/18721889353/sunshine/pkg/logger"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// ProviderType 邮件服务提供商类型
type ProviderType string

const (
	// ProviderTypeTencentSES 腾讯云简单邮件服务
	ProviderTypeTencentSES ProviderType = "tencent_ses"
	// ProviderTypeAliyunDM 阿里云邮件推送
	ProviderTypeAliyunDM ProviderType = "aliyun_dm"
	// ProviderTypeSMTP SMTP协议
	ProviderTypeSMTP ProviderType = "smtp"
)

// EmailClient 邮件客户端接口
type EmailClient interface {
	// SendEmail 发送邮件
	SendEmail(ctx context.Context, req *SendRequest) (*SendResult, error)
	// SendBatchEmail 批量发送邮件
	SendBatchEmail(ctx context.Context, reqs []*SendRequest) ([]*SendResult, error)
	// GetEmailStatus 查询邮件发送状态
	GetEmailStatus(ctx context.Context, query *EmailStatusQuery) (*EmailStatusResult, error)
	// GetProviderType 获取提供商类型
	GetProviderType() ProviderType
}

// SendRequest 发送请求
type SendRequest struct {
	From        string            // 发件人地址
	To          []string          // 收件人列表
	Cc          []string          // 抄送列表
	Bcc         []string          // 密送列表
	Subject     string            // 邮件主题
	HTMLBody    string            // HTML正文
	TextBody    string            // 纯文本正文
	ReplyTo     []string          // 回复地址
	Attachments []*Attachment     // 附件列表
	Tags        map[string]string // 标签（用于追踪）
	Headers     map[string]string // 自定义邮件头（含 Reply-To 时会覆盖上方 ReplyTo 设置的头，因其后写入）
}

// Attachment 附件
type Attachment struct {
	Filename    string // 文件名
	Content     []byte // 文件内容
	ContentType string // MIME类型
}

// SendResult 发送结果
type SendResult struct {
	MessageID string                 // 消息ID
	Status    string                 // 状态: success/failed
	Error     error                  // 错误信息
	Extra     map[string]interface{} // 额外信息（不同提供商返回的数据）
}

// IsSuccess 判断发送结果是否成功。
// 提醒：SendEmail 返回 err == nil 不等于发送成功——云业务失败是 err == nil + Status=failed；
// 建议调用方统一以本方法（或 result.Status == StatusSuccess）判定最终结果。
// nil receiver 返回 false，可直接对可能为 nil 的 result 调用。
func (r *SendResult) IsSuccess() bool {
	return r != nil && r.Status == StatusSuccess
}

// EmailStatus 邮件状态信息
type EmailStatus struct {
	MessageID        string                 // 邮件消息ID
	ToAddress        string                 // 收件人地址
	FromAddress      string                 // 发件人地址
	Status           string                 // 发送状态: delivered, failed, pending, rejected
	StatusCode       int                    // 状态码
	StatusMessage    string                 // 状态描述
	RequestTime      time.Time              // 请求时间
	DeliverTime      time.Time              // 投递时间
	UserOpened       bool                   // 是否已打开
	UserClicked      bool                   // 是否已点击
	UserUnsubscribed bool                   // 是否退订
	UserComplained   bool                   // 是否投诉
	Extra            map[string]interface{} // 额外信息
}

// EmailStatusResult 邮件状态查询结果
type EmailStatusResult struct {
	Status string                 // 查询状态: success, failed
	Data   []*EmailStatus         // 邮件状态列表
	Error  error                  // 错误信息
	Extra  map[string]interface{} // 额外信息
}

// EmailStatusQuery 邮件状态查询参数
type EmailStatusQuery struct {
	MessageID string    // 邮件消息ID（可选）
	ToAddress string    // 收件人地址（可选）
	FromDate  time.Time // 开始日期
	ToDate    time.Time // 结束日期
	Offset    uint64    // 偏移量
	Limit     uint64    // 拉取条数
}

// Config 基础配置
type Config struct {
	ProviderType ProviderType // 提供商类型
	Region       string       // 区域（云服务商需要）
	AccessKeyID  string       // Access Key ID
	SecretKey    string       // Secret Key
	// SMTP专用配置
	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	UseTLS       bool
	// BatchConcurrency 批量发送的并发上限（SendBatchEmail 用）：
	// 0 或负值使用默认 10；正值由调用方按自家 provider 的速率限制自行设定，不额外封顶
	BatchConcurrency int
}

// 邮件状态常量
const (
	// StatusSuccess 邮件发送成功状态
	StatusSuccess = "success"
	// StatusFailed 邮件发送失败状态
	StatusFailed = "failed"
)

// String 值接收者的格式化输出：密钥字段一律脱敏，避免 Config 被 %v/%+v/%s 等
// 走 Stringer 的格式化路径或日志拼接时泄露。
// 注意：fmt 的 %#v（Go 语法还原）与 zap 等按字段反射的序列化不会调用 String()，
// 含密钥的 Config 不要交给这两条路径；字段本身仍为明文，也不要序列化后对外发送。
func (c Config) String() string {
	return fmt.Sprintf(
		"Config{ProviderType:%s, Region:%s, AccessKeyID:%s, SecretKey:%s, SMTPHost:%s, SMTPPort:%d, SMTPUsername:%s, SMTPPassword:%s, UseTLS:%v, BatchConcurrency:%d}",
		c.ProviderType, c.Region, maskAccessKey(c.AccessKeyID), maskSecret(c.SecretKey),
		c.SMTPHost, c.SMTPPort, c.SMTPUsername, maskSecret(c.SMTPPassword), c.UseTLS, c.BatchConcurrency)
}

// maskSecret 密钥脱敏：保留前 2 后 2、中间以 * 替代；长度 ≤4 时整体替换为 *。
// 仅用于 Config.String 等展示场景，不参与任何业务判定。
func maskSecret(secret string) string {
	if secret == "" {
		return ""
	}
	if len(secret) <= 4 {
		return strings.Repeat("*", len(secret))
	}
	return secret[:2] + strings.Repeat("*", len(secret)-4) + secret[len(secret)-2:]
}

// maskAccessKey 密钥标识（AccessKeyID）脱敏：保留前 4 后 4、中间以 * 替代；
// 长度 ≤8 时整体替换为 *（首4尾4 相当于没掩码，直接全覆盖）。
// 保留首4尾4 是安全取舍：仍可区分「用的哪把密钥」便于排查，同时不落完整标识。
func maskAccessKey(accessKey string) string {
	if accessKey == "" {
		return ""
	}
	if len(accessKey) <= 8 {
		return strings.Repeat("*", len(accessKey))
	}
	return accessKey[:4] + strings.Repeat("*", len(accessKey)-8) + accessKey[len(accessKey)-4:]
}

// maskEmail 邮箱地址脱敏（PII）：本地部分保留首 2 尾 2、中间以 * 替代，
// @域名 原样保留——域名用于排查投递问题（是哪家邮局），不是个人标识。
// 本地部分 ≤4 时整体替换为 *。
// 掩码按 rune（字符）粒度而非 byte——多字节字符（如中文）按 byte 切分会切断
// UTF-8 序列，日志/span 里出现乱码字节序列（终审 R7-5）。
func maskEmail(email string) string {
	if email == "" {
		return ""
	}
	at := strings.LastIndex(email, "@")
	if at <= 0 {
		// 格式非法的输入也一律脱敏，不能因为格式错就原样落日志
		r := []rune(email)
		if len(r) <= 4 {
			return strings.Repeat("*", len(r))
		}
		return string(r[:2]) + strings.Repeat("*", len(r)-4) + string(r[len(r)-2:])
	}
	local, domain := email[:at], email[at+1:]
	r := []rune(local)
	if len(r) <= 4 {
		local = strings.Repeat("*", len(r))
	} else {
		local = string(r[:2]) + strings.Repeat("*", len(r)-4) + string(r[len(r)-2:])
	}
	return local + "@" + domain
}

// batchConcurrency 批量发送的单批并发默认上限：兼顾吞吐与可控性——瞬时并发受控，
// 不会一批把云侧速率限制打满；超出上限的条目在任务队列中排队等待空闲 worker。
// 调用方可通过 Config.BatchConcurrency 覆盖（见 resolveBatchConcurrency）。
const batchConcurrency = 10

// resolveBatchConcurrency 解析批量并发上限：Config.BatchConcurrency 非正数时回落默认 batchConcurrency。
// 不对正值封顶——不同 provider（SMTP 服务器 / 云 API）速率限制差异大，由调用方自行选择。
func resolveBatchConcurrency(n int) int {
	if n <= 0 {
		return batchConcurrency
	}
	return n
}

// runBatchEmail 批量发送的并发调度骨架（三 provider 复用，保证语义对称）：
//   - worker 数取 min(resolveBatchConcurrency(concurrency), len(reqs))，按任务队列消费，单条失败不中断批量；
//   - results 按输入下标写入，与串行执行等价（输出顺序恒等于请求顺序，便于逐条定位）；
//   - ctx 取消后 worker 不再发起后续任务：未执行条目回填 failed + ctx.Err()，
//     results 仍完整保序，聚合 error 可定位到具体条目（首个取消打一条 Warn）；
//   - 全部条目跑完后返回 batchErrors 聚合 error，无论成败 results 都是完整逐条结果；
//   - send 回调负责各自的校验/日志/span，本函数只做调度与聚合。
func runBatchEmail(ctx context.Context, reqs []*SendRequest, concurrency int, send func(context.Context, *SendRequest) (*SendResult, error)) ([]*SendResult, error) {
	results := make([]*SendResult, len(reqs))
	if len(reqs) == 0 {
		return results, nil
	}

	// 任务队列一次性灌满并关闭，worker 用 range 消费到队列空
	jobs := make(chan int, len(reqs))
	for i := range reqs {
		jobs <- i
	}
	close(jobs)

	workers := resolveBatchConcurrency(concurrency)
	if len(reqs) < workers {
		workers = len(reqs)
	}
	var wg sync.WaitGroup
	var cancelLogOnce sync.Once
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			for i := range jobs {
				// ctx 感知退出：取消后剩余条目不再发起任务，逐条回填失败原因，
				// 避免明知取消仍把请求逐个送进云 SDK（Done 已关闭，select 每轮必命中）
				select {
				case <-ctx.Done():
					cancelLogOnce.Do(func() {
						logger.WarnWithCtx(ctx, "批量发送因上下文取消终止，剩余条目标记为失败",
							logger.Err(ctx.Err()))
					})
					results[i] = &SendResult{
						Status: StatusFailed,
						Error:  fmt.Errorf("批量任务因上下文取消未执行: %w", ctx.Err()),
					}
					continue
				default:
				}

				result, err := send(ctx, reqs[i])
				if err != nil {
					// 失败原因已含在 err 中，这里补充序号便于定位是哪一条
					logger.WarnWithCtx(ctx, "批量发送中单条邮件失败",
						logger.Int("index", i),
						logger.Err(err))
				}
				// 所有失败路径均返回非 nil result；若极端情况下仍为 nil，由 batchErrors 的 nil 分支兜底
				results[i] = result
			}
		}()
	}
	wg.Wait()

	return results, batchErrors(results)
}

// batchErrors 将批量发送的逐条结果聚合为单个 error（纯函数，便于单测）：
// 收集所有 Status != success 的条目（用 errors.Join 拼接，带序号便于定位），全成功返回 nil。
// SendBatchEmail「单条失败不中断批量」的特性不变——聚合 error 在全部条目跑完后才返回，
// results 始终包含完整的逐条结果，两者可同时使用。
func batchErrors(results []*SendResult) error {
	errs := make([]error, 0)
	for i, r := range results {
		switch {
		case r == nil:
			// 防御：正常路径均返回非 nil result，此分支仅为健壮性
			errs = append(errs, fmt.Errorf("第 %d 条结果为空", i+1))
		case r.Status == StatusSuccess:
			// 成功路径保证 Error==nil；此处防御状态矛盾（不静默吞掉 Error）
			if r.Error != nil {
				errs = append(errs, fmt.Errorf("第 %d 条状态矛盾（success 但 Error 非空）: %w", i+1, r.Error))
			}
		case r.Error != nil:
			errs = append(errs, fmt.Errorf("第 %d 条: %w", i+1, r.Error))
		default:
			errs = append(errs, fmt.Errorf("第 %d 条发送失败", i+1))
		}
	}
	return errors.Join(errs...)
}

// NewEmailClient 创建邮件客户端。
// 出错时返回真正的 nil 接口：具体构造函数返回的是带类型信息的 nil 指针（*SMTPClient 等），
// 若直接透传，接口会变成「非 nil 接口包着 nil 指针」的 typed-nil，调用方 if client != nil 判不出来。
func NewEmailClient(cfg *Config) (EmailClient, error) {
	if cfg == nil {
		return nil, fmt.Errorf("邮件配置不能为空")
	}
	switch cfg.ProviderType {
	case ProviderTypeTencentSES:
		return asEmailClient(newTencentSESClient(cfg))
	case ProviderTypeAliyunDM:
		return asEmailClient(newAliyunDMClient(cfg))
	case ProviderTypeSMTP:
		return asEmailClient(newSMTPClient(cfg))
	default:
		return nil, fmt.Errorf("不支持的邮件服务商类型: %q", cfg.ProviderType)
	}
}

// asEmailClient 把具体构造函数的多返回值转成接口返回：失败时丢弃 typed-nil、只返回 err。
func asEmailClient(client EmailClient, err error) (EmailClient, error) {
	if err != nil {
		return nil, err
	}
	return client, nil
}

// requestIDAttr 从 context 中提取 request_id 并返回 span 属性键值对。
func requestIDAttr(ctx context.Context) attribute.KeyValue {
	if ctx != nil {
		if reqID, ok := ctx.Value(logger.ContextKeyRequestID).(string); ok && reqID != "" {
			return attribute.String("email.request_id", reqID)
		}
	}
	return attribute.String("email.request_id", "")
}

// ValidateEmail 验证邮箱地址格式
func ValidateEmail(ctx context.Context, email string) bool {
	// 链路追踪
	tracer := otel.Tracer("goemail")
	spanName := "email.validate.single"
	ctx, span := tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	logger.InfoWithCtx(ctx, "开始校验邮箱地址格式",
		logger.String("email", maskEmail(email)))

	// 设置追踪属性（邮箱为 PII，span 属性只写掩码值）
	span.SetAttributes(
		attribute.String("email.to_validate", maskEmail(email)),
		requestIDAttr(ctx),
	)

	startTime := time.Now()

	valid := isValidEmail(email)

	// 设置追踪属性：校验结果与耗时统一在一处写入（早返回分支不再各自重复一遍）
	duration := time.Since(startTime)
	span.SetAttributes(
		attribute.Bool("email.is_valid", valid),
		attribute.Float64("email.validate.duration_ms", float64(duration.Milliseconds())),
		requestIDAttr(ctx),
	)
	span.SetStatus(codes.Ok, "validation completed")

	return valid
}

// isValidEmail 邮箱格式校验的纯函数核心（ValidateEmail 的日志/span 外壳包在导出函数上）：
// 非空、含 @ 且 @ 不在首尾、本地部分与域名均非空、域名含点。
// 独立成纯函数的目的：可直接被单测与 fuzz 覆盖，不必承担每调用一次的 Info 日志与 span 开销。
func isValidEmail(email string) bool {
	if len(email) == 0 {
		return false
	}

	atIndex := -1
	for i, c := range email {
		if c == '@' {
			atIndex = i
			break
		}
	}

	if atIndex <= 0 || atIndex >= len(email)-1 {
		return false
	}

	localPart := email[:atIndex]
	domainPart := email[atIndex+1:]
	if len(localPart) == 0 || len(domainPart) == 0 {
		return false
	}

	// 检查域名是否包含点
	for _, c := range domainPart {
		if c == '.' {
			return true
		}
	}
	return false
}

// ValidateEmails 批量验证邮箱地址
func ValidateEmails(ctx context.Context, emails []string) []string {
	// 链路追踪
	tracer := otel.Tracer("goemail")
	spanName := "email.validate.batch"
	ctx, span := tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	logger.InfoWithCtx(ctx, "开始批量校验邮箱地址格式",
		logger.Int("count", len(emails)))

	// 设置追踪属性
	span.SetAttributes(
		attribute.Int("email.count", len(emails)),
		requestIDAttr(ctx),
	)

	startTime := time.Now()

	var invalid []string
	for _, email := range emails {
		if !ValidateEmail(ctx, email) {
			invalid = append(invalid, email)
		}
	}

	// 设置成功的追踪属性
	duration := time.Since(startTime)
	span.SetAttributes(
		attribute.Int("email.invalid_count", len(invalid)),
		attribute.Float64("email.validate.duration_ms", float64(duration.Milliseconds())),
		requestIDAttr(ctx),
	)
	span.SetStatus(codes.Ok, "validation completed")

	return invalid
}
