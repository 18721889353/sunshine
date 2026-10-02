// Package gosms 提供多平台短信发送功能
// 支持腾讯云SMS、阿里云SMS等多种发送方式
package gosms

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

// ProviderType 短信服务提供商类型
type ProviderType string

const (
	// ProviderTypeTencentSMS 腾讯云短信服务
	ProviderTypeTencentSMS ProviderType = "tencent_sms"
	// ProviderTypeAliyunSMS 阿里云短信服务
	ProviderTypeAliyunSMS ProviderType = "aliyun_sms"
)

// SMSClient 短信客户端接口
type SMSClient interface {
	// SendSMS 发送短信
	SendSMS(ctx context.Context, req *SendRequest) (*SendResult, error)
	// SendBatchSMS 批量发送短信
	SendBatchSMS(ctx context.Context, reqs []*SendRequest) ([]*SendResult, error)
	// GetSMSStatus 查询短信发送状态
	GetSMSStatus(ctx context.Context, query *SMSStatusQuery) (*SMSStatusResult, error)
	// GetProviderType 获取提供商类型
	GetProviderType() ProviderType
}

// SendRequest 发送请求
type SendRequest struct {
	PhoneNumbers   []string          // 手机号列表（国际格式，如 +8613711112222）
	TemplateID     string            // 模板ID
	TemplateParams []TemplateParam   // 模板参数（有序，语义见 TemplateParam 注释）
	SignName       string            // 签名名称（为空时回退 Config 中对应云的默认签名）
	Tags           map[string]string // 标签（预留字段，当前未使用）
}

// TemplateParam 单个模板参数。
// 腾讯云为位置参数：按切片顺序对应模板中的 %1%、%2%…占位符，Key 仅用于代码自文档（发送时不使用）；
// 阿里云为命名参数：Key/Value 写入 TemplateParam JSON 对象，顺序无关。
// 注意：不使用 map 是因为 Go map 遍历顺序随机，腾讯云位置参数会因此错位（验证码等短信内容错乱）。
type TemplateParam struct {
	Key   string // 参数名（腾讯云侧仅自文档；阿里云侧为 JSON 键）
	Value string // 参数值
}

// SendResult 发送结果
type SendResult struct {
	MessageID   string                 // 消息ID
	PhoneNumber string                 // 手机号
	Status      string                 // 状态: success/failed
	Error       error                  // 错误信息
	Extra       map[string]interface{} // 额外信息（不同提供商返回的数据）
	// MultipleResults 逐号码子结果（仅腾讯云一次提交多号码时非 nil，按请求顺序排列；
	// 单号码与阿里云恒为 nil）。主结果的 MessageID/PhoneNumber/Status/Error 反映
	// 首个号码，多号码时任一号码失败则主结果 Status=failed——完整逐号码状态以本字段为准。
	// 注意：子结果（本字段中的元素）的本字段恒为 nil——只展开一层，无递归嵌套。
	MultipleResults []*SendResult
}

// IsSuccess 判断发送结果是否成功。
// 提醒：SendSMS 返回 err == nil 不等于发送成功——云业务失败是 err == nil + Status=failed；
// 建议调用方统一以本方法（或 result.Status == StatusSuccess）判定最终结果。
// nil receiver 返回 false，可直接对可能为 nil 的 result 调用。
func (r *SendResult) IsSuccess() bool {
	return r != nil && r.Status == StatusSuccess
}

// SMSStatus 短信状态信息
type SMSStatus struct {
	MessageID     string                 // 短信消息ID
	PhoneNumber   string                 // 手机号
	Status        string                 // 发送状态: delivered, failed, pending, rejected
	StatusCode    int                    // 状态码
	StatusMessage string                 // 状态描述
	SendTime      time.Time              // 发送时间
	DeliverTime   time.Time              // 投递时间
	Extra         map[string]interface{} // 额外信息
}

// SMSStatusResult 短信状态查询结果
type SMSStatusResult struct {
	Status string                 // 查询状态: success, failed
	Data   []*SMSStatus           // 短信状态列表
	Error  error                  // 错误信息
	Extra  map[string]interface{} // 额外信息
}

// SMSStatusQuery 短信状态查询参数
type SMSStatusQuery struct {
	MessageID   string    // 短信消息ID（可选）
	PhoneNumber string    // 手机号（必填，国际格式）
	FromDate    time.Time // 开始日期（零值时回退为 ToDate 前 24 小时）
	ToDate      time.Time // 结束日期（零值时回退为当前时间）
	Offset      uint64    // 偏移量
	Limit       uint64    // 拉取条数（0 时归一为默认值 10）
}

// Config 基础配置
type Config struct {
	ProviderType ProviderType // 提供商类型
	Region       string       // 区域（云服务商需要）
	AccessKeyID  string       // Access Key ID / Secret ID
	SecretKey    string       // Secret Key
	// BatchConcurrency 批量发送的并发上限（SendBatchSMS 用）：
	// 0 或负数回落默认 batchConcurrency（10），正值原样生效（见 resolveBatchConcurrency）
	BatchConcurrency int
	// 腾讯云专用配置
	TencentAppID    string // 腾讯云短信应用ID
	TencentSignName string // 腾讯云默认签名（SendRequest.SignName 为空时兜底）
	// 阿里云专用配置
	AliyunSignName string // 阿里云默认签名（SendRequest.SignName 为空时兜底）
}

// String 实现 fmt.Stringer：对 Config 执行 %v/%+v/%s 打印时 SecretKey 与 AccessKeyID
// 自动脱敏（首2尾2 / 首4尾4），防止 fmt.Printf("%+v", cfg) 把密钥明文写入日志
// （与手机号 maskPhone 同一安全边界）。
// 注意：仅影响格式化输出，字段本身仍为明文——不要把 Config 序列化后对外发送。
func (c Config) String() string {
	return fmt.Sprintf(
		"Config{ProviderType:%s, Region:%s, AccessKeyID:%s, SecretKey:%s, BatchConcurrency:%d, TencentAppID:%s, TencentSignName:%s, AliyunSignName:%s}",
		c.ProviderType, c.Region, maskAccessKey(c.AccessKeyID), maskSecret(c.SecretKey), c.BatchConcurrency,
		c.TencentAppID, c.TencentSignName, c.AliyunSignName)
}

// 短信状态常量
const (
	// StatusSuccess 短信发送成功状态
	StatusSuccess = "success"
	// StatusFailed 短信发送失败状态
	StatusFailed = "failed"
	// StatusPending 短信发送中状态
	StatusPending = "pending"
	// StatusDelivered 短信已送达状态
	StatusDelivered = "delivered"
)

// defaultQueryLimit 状态查询默认拉取条数。
// 归一为 ≥1 的值，同时保证阿里云页码换算（Offset/Limit）的除数不为 0。
const defaultQueryLimit uint64 = 10

// NewSMSClient 创建短信客户端。
// 出错时返回真正的 nil 接口：具体构造函数返回的是带类型信息的 nil 指针（*TencentSMSClient 等），
// 若直接透传，接口会变成「非 nil 接口包着 nil 指针」的 typed-nil，调用方 if client != nil 判不出来，
// 后续方法调用即 panic（与 goemail NewEmailClient R3-2 修复完全同型，跨包同步）。
func NewSMSClient(cfg *Config) (SMSClient, error) {
	if cfg == nil {
		return nil, fmt.Errorf("短信配置不能为空")
	}
	switch cfg.ProviderType {
	case ProviderTypeTencentSMS:
		return asSMSClient(newTencentSMSClient(cfg))
	case ProviderTypeAliyunSMS:
		return asSMSClient(newAliyunSMSClient(cfg))
	default:
		return nil, fmt.Errorf("不支持的短信服务商类型: %s", cfg.ProviderType)
	}
}

// asSMSClient 把具体构造函数的多返回值转成接口返回：失败时丢弃 typed-nil、只返回 err。
func asSMSClient(client SMSClient, err error) (SMSClient, error) {
	if err != nil {
		return nil, err
	}
	return client, nil
}

// isValidPhoneNumber 纯格式校验：+ 开头、其余全数字、数字部分 7~15 位（E.164 上限 15 位）。
// 无 span、无日志、无 ctx，供 Validate 与 ValidatePhoneNumber 复用；
// 抽为纯函数的原因是 Validate() 无 ctx 参数，旧实现内部用 context.Background()
// 启 span 会丢失链路上下文（request_id 为空）。
func isValidPhoneNumber(phone string) bool {
	if len(phone) == 0 || phone[0] != '+' {
		return false
	}
	for i := 1; i < len(phone); i++ {
		if phone[i] < '0' || phone[i] > '9' {
			return false
		}
	}
	digitLen := len(phone) - 1
	return digitLen >= 7 && digitLen <= 15
}

// ValidatePhoneNumber 验证手机号格式（带链路追踪）。
// 仅做格式校验，不发送任何请求；校验结果以 sms.is_valid 属性写入 span。
// 手机号属 PII，日志与 span 属性中一律经 maskPhone 脱敏。
func ValidatePhoneNumber(ctx context.Context, phoneNumber string) bool {
	tracer := otel.Tracer("gosms")
	ctx, span := tracer.Start(ctx, "sms.validate.phone", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	valid := isValidPhoneNumber(phoneNumber)

	span.SetAttributes(
		attribute.String("sms.phone_to_validate", maskPhone(phoneNumber)),
		attribute.Bool("sms.is_valid", valid),
		requestIDAttr(ctx),
	)
	span.SetStatus(codes.Ok, "validation completed")

	logger.DebugWithCtx(ctx, "手机号格式校验",
		logger.String("phone", maskPhone(phoneNumber)),
		logger.Bool("valid", valid))

	return valid
}

// maskPhone 手机号脱敏：保留前 3 位与后 4 位，中间以 * 替代；总长 ≤7 位时整体替换为 *。
// 仅用于日志与 span 属性（PII 不入观测系统）；
// SendResult/SMSStatus 等业务结构体保留原文——它们是返回给调用方的业务数据，
// 错误消息也保留原文，便于调用方定位自己传入的号码。
func maskPhone(phone string) string {
	if phone == "" {
		return ""
	}
	if len(phone) <= 7 {
		return strings.Repeat("*", len(phone))
	}
	return phone[:3] + strings.Repeat("*", len(phone)-7) + phone[len(phone)-4:]
}

// FormatPhoneNumber 格式化手机号为标准国际格式
func FormatPhoneNumber(phoneNumber string) string {
	// 如果已经有+号，直接返回
	if len(phoneNumber) > 0 && phoneNumber[0] == '+' {
		return phoneNumber
	}

	// 如果是国内手机号（11位），添加+86
	if len(phoneNumber) == 11 && phoneNumber[0] == '1' {
		return "+86" + phoneNumber
	}

	// 其他情况，假设已经是国际格式但没有+号
	return "+" + phoneNumber
}

// Validate 验证发送请求的合法性（纯校验，不带 span/日志，不发起网络调用）。
// 签名必填检查在各 provider 入口完成「Config 默认签名兜底」之后调用，
// 因此 SendRequest.SignName 为空但 Config 已配置默认签名时校验可通过。
func (r *SendRequest) Validate() error {
	if r == nil {
		return fmt.Errorf("发送请求不能为空")
	}
	if len(r.PhoneNumbers) == 0 {
		return fmt.Errorf("手机号列表不能为空")
	}
	if r.TemplateID == "" {
		return fmt.Errorf("模板ID不能为空")
	}
	if r.SignName == "" {
		return fmt.Errorf("签名不能为空")
	}
	// 验证手机号格式
	for _, phone := range r.PhoneNumbers {
		if !isValidPhoneNumber(phone) {
			return fmt.Errorf("手机号格式错误（需+开头的国际格式）: %s", phone)
		}
	}
	return nil
}

// withSignFallback 返回签名兜底后的请求副本（值拷贝，不修改调用方结构体）。
// 各 provider 入口在调用 Validate 之前使用，使「SendRequest.SignName 为空但
// Config 已配置默认签名」的请求能通过必填校验。
func withSignFallback(req *SendRequest, defaultSign string) SendRequest {
	effective := *req
	if effective.SignName == "" {
		effective.SignName = defaultSign
	}
	return effective
}

// failSpan 统一失败埋点：RecordError + SetStatus(Error)，并返回原 error 便于链式 return。
// 成功路径的属性与 codes.Ok 只在各 provider 确认成功后设置一次，
// 避免「先记 Error 再被末尾无条件 SetStatus(Ok) 覆盖」的埋点反转。
func failSpan(span trace.Span, err error) error {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	return err
}

// failedResult 构造统一的失败发送结果，保持「失败必返回非 nil result」的既有语义
// （调用方惯例：err != nil 或 result.Status == StatusFailed 均按失败处理）。
func failedResult(err error) *SendResult {
	return &SendResult{Status: StatusFailed, Error: err}
}

// failedStatusResult 构造统一的失败状态查询结果，语义同 failedResult。
func failedStatusResult(err error) *SMSStatusResult {
	return &SMSStatusResult{Status: StatusFailed, Error: err}
}

// normalizeStatusQuery 校验并归一化状态查询参数（在任何网络调用前执行，fail loud）：
//   - nil / 手机号为空 → 错误；
//   - ToDate 零值 → 当前时间；FromDate 零值 → ToDate 前 24 小时；
//   - 开始时间晚于结束时间 → 错误；
//   - Limit 为 0 → 默认 10（保证页码换算除数 ≥1，同时避免阿里云 PageSize=0 被 API 拒绝）。
func normalizeStatusQuery(query *SMSStatusQuery) (SMSStatusQuery, error) {
	if query == nil {
		return SMSStatusQuery{}, fmt.Errorf("查询参数不能为空")
	}
	q := *query
	if q.PhoneNumber == "" {
		return SMSStatusQuery{}, fmt.Errorf("查询手机号不能为空")
	}
	if q.ToDate.IsZero() {
		q.ToDate = time.Now()
	}
	if q.FromDate.IsZero() {
		q.FromDate = q.ToDate.Add(-24 * time.Hour)
	}
	if q.FromDate.After(q.ToDate) {
		return SMSStatusQuery{}, fmt.Errorf("查询时间范围无效: 开始时间晚于结束时间")
	}
	if q.Limit == 0 {
		q.Limit = defaultQueryLimit
	}
	return q, nil
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

// maskAccessKey 密钥标识（AccessKeyID / SecretId）脱敏：保留前 4 后 4、中间以 * 替代；
// 长度 ≤8 时整体替换为 *（首4尾4 相当于没掩码，直接全覆盖）。
// 保留首4尾4 是安全取舍：仍可区分「用的哪把密钥」便于排查，同时不落完整标识
// ——AccessKeyID 与 SecretKey 组合使用，安全审计通常要求两者都不以明文入日志。
func maskAccessKey(accessKey string) string {
	if accessKey == "" {
		return ""
	}
	if len(accessKey) <= 8 {
		return strings.Repeat("*", len(accessKey))
	}
	return accessKey[:4] + strings.Repeat("*", len(accessKey)-8) + accessKey[len(accessKey)-4:]
}

// batchConcurrency 批量发送的单批并发默认上限：兼顾吞吐与可控性——瞬时并发受控，
// 不会一批把云侧速率限制打满；超出上限的条目在任务队列中排队等待空闲 worker。
// 调用方可通过 Config.BatchConcurrency 覆盖（见 resolveBatchConcurrency）。
const batchConcurrency = 10

// resolveBatchConcurrency 解析批量并发上限：Config.BatchConcurrency 非正数时回落默认 batchConcurrency。
// 不对正值封顶——腾讯/阿里云 API 速率限制差异大，由调用方自行选择（与 goemail 同型）。
func resolveBatchConcurrency(n int) int {
	if n <= 0 {
		return batchConcurrency
	}
	return n
}

// runBatchSMS 批量发送的并发调度骨架（两云 provider 复用，保证语义对称）：
//   - worker 数取 min(resolveBatchConcurrency(concurrency), len(reqs))，按任务队列消费，单条失败不中断批量；
//   - results 按输入下标写入，与串行执行等价（输出顺序恒等于请求顺序，便于逐条定位）；
//   - ctx 取消后 worker 不再发起后续任务：未执行条目回填 failed + ctx.Err()，
//     results 仍完整保序，聚合 error 可定位到具体条目（首个取消打一条 Warn）；
//   - 全部条目跑完后返回 batchErrors 聚合 error，无论成败 results 都是完整逐条结果；
//   - send 回调负责各自的校验/日志/span，本函数只做调度与聚合。
func runBatchSMS(ctx context.Context, reqs []*SendRequest, concurrency int, send func(context.Context, *SendRequest) (*SendResult, error)) ([]*SendResult, error) {
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
					// 此处不取 req.PhoneNumbers[0]（旧实现在 req 无号码时会 panic），
					// 失败原因已含在 err 中，这里补充序号便于定位是哪一条
					logger.WarnWithCtx(ctx, "批量发送中单条短信失败",
						logger.Int("index", i),
						logger.Err(err))
				}
				// SendSMS 所有失败路径均返回非 nil result（failedResult 保证）；
				// 若极端情况下仍为 nil，由 batchErrors 的 nil 分支兜底
				results[i] = result
			}
		}()
	}
	wg.Wait()

	return results, batchErrors(results)
}

// batchErrors 将批量发送的逐条结果聚合为单个 error（纯函数，便于单测）：
// 收集所有 Status != success 的条目（用 errors.Join 拼接，带序号便于定位），全成功返回 nil。
// SendBatchSMS「单条失败不中断批量」的特性不变——聚合 error 在全部条目跑完后才返回，
// results 始终包含完整的逐条结果，两者可同时使用。
func batchErrors(results []*SendResult) error {
	errs := make([]error, 0)
	for i, r := range results {
		switch {
		case r == nil:
			// 防御：SendSMS 所有路径均返回非 nil result，此分支仅为健壮性
			errs = append(errs, fmt.Errorf("第 %d 条结果为空", i+1))
		case r.Status == StatusSuccess:
			// 成功路径 SendSMS 保证 Error==nil；此处防御状态矛盾（不静默吞掉 Error）
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

// requestIDAttr 从 context 中提取 request_id 并返回 span 属性键值对。
func requestIDAttr(ctx context.Context) attribute.KeyValue {
	if ctx != nil {
		if reqID, ok := ctx.Value(logger.ContextKeyRequestID).(string); ok && reqID != "" {
			return attribute.String("sms.request_id", reqID)
		}
	}
	return attribute.String("sms.request_id", "")
}

// getStringValue 安全获取字符串值（内部辅助函数）
func getStringValue(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}
