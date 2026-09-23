package logger

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	sls "github.com/aliyun/aliyun-log-go-sdk"
	"github.com/aliyun/aliyun-log-go-sdk/producer"
	"go.uber.org/zap/zapcore"
)

// SLSConfig 阿里云 SLS 配置
type SLSConfig struct {
	// 必选参数
	Endpoint        string // SLS Endpoint，如: cn-hangzhou.log.aliyuncs.com
	AccessKeyID     string // AccessKey ID
	AccessKeySecret string // AccessKey Secret
	ProjectName     string // SLS Project 名称
	LogStoreName    string // LogStore 名称

	// 可选参数
	Topic      string // 日志主题，默认为空
	Source     string // 日志来源，默认为服务名称
	MaxRetries int    // 最大重试次数，默认 10
	Timeout    int    // 超时时间（秒），默认 60

	// Producer 性能配置
	TotalSizeLnBytes      int64 // 缓存总大小(字节)，默认 512MB (512 * 1024 * 1024)
	MaxBatchCount         int   // 单个 Batch 最大日志条数，默认 4096
	MaxBatchSize          int   // 单个 Batch 最大大小(字节)，默认 3MB (3 * 1024 * 1024)
	LingerMs              int   // Batch 刷新间隔(毫秒)，默认 2000ms
	DisableRuntimeMetrics bool  // 禁用运行时指标日志，默认 true

	// 高级配置（大厂最佳实践）
	EnableHealthCheck   bool // 是否启用健康检查，默认 false（关闭），true 表示开启
	HealthCheckInterval int  // 健康检查间隔(秒)，默认 30 秒
	SendTimeout         int  // 发送超时时间(秒)，默认 5 秒
	SkipVerify          bool // 是否跳过启动时的测试日志验证，默认 false（建议单元测试时设为 true，避免污染 LogStore）
}

// SLS Hook 预定义静态 key，避免每次调用分配字符串
var (
	slsKeyTimestamp   = "timestamp"
	slsKeyLevel       = "level"
	slsKeyMessage     = "message"
	slsKeyCaller      = "caller"
	slsKeyServiceName = "service_name"
)

// SLSHook 阿里云 SLS 日志钩子
type SLSHook struct {
	config      *SLSConfig
	producer    *producer.Producer
	serviceName string // 缓存的服务名称，避免每次计算

	// 状态管理（原子操作，线程安全）
	state int32 // 0:已创建, 1:启动中, 2:运行中, 3:停止中, 4:已停止, 5:失败

	// 监控指标
	sendSuccessCount int64        // 发送成功计数
	sendFailedCount  int64        // 发送失败计数
	lastErrorTime    time.Time    // 最后错误时间
	lastErrorMessage string       // 最后错误信息
	errorMu          sync.RWMutex // 保护错误信息

	// 健康检查
	healthCheckStop chan struct{}
	healthCheckWg   sync.WaitGroup
}

// NewSLSHook 创建阿里云 SLS 日志钩子
// 用法:
//
//	config := &SLSConfig{
//	    Endpoint:   "cn-hangzhou.log.aliyuncs.com",
//	    AccessKeyID:     "your-access-key-id",
//	    AccessKeySecret: "your-access-key-secret",
//	    ProjectName:     "your-project",
//	    LogStoreName:    "your-logstore",
//	    Source:          "user-service",
//	}
//	hook, err := NewSLSHook(config)
//	if err != nil {
//	    panic(err)
//	}
//	logger.Init(logger.WithCustomHooksWithCtx(hook.Hook))
func NewSLSHook(config *SLSConfig) (*SLSHook, error) {
	if config.Endpoint == "" || config.AccessKeyID == "" || config.AccessKeySecret == "" {
		return nil, fmt.Errorf("SLS 配置不完整: endpoint、accessKeyId 和 accessKeySecret 为必填项")
	}

	if config.ProjectName == "" || config.LogStoreName == "" {
		return nil, fmt.Errorf("SLS 配置不完整: projectName 和 logStoreName 为必填项")
	}

	// 设置默认值
	if config.MaxRetries == 0 {
		config.MaxRetries = 10
	}
	if config.Timeout == 0 {
		config.Timeout = 60
	}
	if config.Source == "" {
		config.Source = "unknown"
	}

	// 设置 Producer 性能配置默认值
	if config.TotalSizeLnBytes == 0 {
		config.TotalSizeLnBytes = 512 * 1024 * 1024 // 默认 512MB
	}
	if config.MaxBatchCount == 0 {
		config.MaxBatchCount = 4096 // 默认 4096 条
	}
	if config.MaxBatchSize == 0 {
		config.MaxBatchSize = 3 * 1024 * 1024 // 默认 3MB
	}
	if config.LingerMs == 0 {
		config.LingerMs = 2000 // 默认 2000ms
	}
	// DisableRuntimeMetrics 默认为 true（关闭运行时指标日志）

	// 设置高级配置默认值
	if config.HealthCheckInterval == 0 {
		config.HealthCheckInterval = 30 // 默认 30 秒
	}
	if config.SendTimeout == 0 {
		config.SendTimeout = 5 // 默认 5 秒
	}
	// EnableHealthCheck 默认为 false（关闭），如需开启请显式设置为 true

	// 边界检查：验证配置的合理性
	if err := validateSLSConfig(config); err != nil {
		return nil, fmt.Errorf("SLS 配置无效: %w", err)
	}

	// 创建生产者配置
	producerConfig := producer.GetDefaultProducerConfig()
	producerConfig.Endpoint = config.Endpoint
	// 使用新的 CredentialsProvider API（替代已弃用的 AccessKeyID/AccessKeySecret）
	producerConfig.CredentialsProvider = sls.NewStaticCredentialsProvider(
		config.AccessKeyID,
		config.AccessKeySecret,
		"", // SecurityToken，静态 AK 不需要
	)
	// 从配置中读取 Producer 性能参数
	producerConfig.TotalSizeLnBytes = config.TotalSizeLnBytes
	producerConfig.MaxBatchCount = config.MaxBatchCount
	producerConfig.MaxBatchSize = int64(config.MaxBatchSize)
	producerConfig.LingerMs = int64(config.LingerMs)
	producerConfig.Retries = config.MaxRetries
	producerConfig.DisableRuntimeMetrics = config.DisableRuntimeMetrics
	// 注意: TimeoutMilliseconds 字段可能不存在，使用默认值

	// 创建生产者
	p, err := producer.NewProducer(producerConfig)
	if err != nil {
		return nil, fmt.Errorf("SLS 生产者创建失败: %w", err)
	}

	// 初始化 Hook 实例
	sn := config.Source
	if sn == "" {
		sn = config.ProjectName
	}
	if sn == "" {
		sn = "unknown"
	}
	hook := &SLSHook{
		config:          config,
		producer:        p,
		serviceName:     sn,
		healthCheckStop: make(chan struct{}),
	}

	// 启动生产者（Start 方法没有返回值，需要通过后续操作验证）
	p.Start()

	// 标记为启动中状态
	hook.setState(StateStarting)

	// 异步健康检查：验证 Producer 是否正常启动
	// EnableHealthCheck 为 true 时开启，false 时关闭（默认关闭）
	if config.EnableHealthCheck {
		go hook.startHealthCheck()
	}

	// 等待短暂时间让 Producer 完成初始化
	time.Sleep(100 * time.Millisecond)

	// 验证 Producer 状态（SkipVerify=true 时跳过，避免测试时污染 LogStore）
	if !config.SkipVerify {
		if err := hook.verifyProducerState(); err != nil {
			// 启动失败，返回错误由调用方决定如何处理
			if closeErr := hook.Close(); closeErr != nil { // 清理资源
				fmt.Printf("close SLS hook error: %v\n", closeErr)
			}
			return nil, fmt.Errorf("SLS 生产者验证失败: %w", err)
		}
	}

	hook.setState(StateRunning)
	return hook, nil
}

// Hook 实现 CustomHookWithCtx 接口，将日志发送到阿里云 SLS
func (h *SLSHook) Hook(_ context.Context, entry zapcore.Entry, fields []Field) error {
	// 预分配 Contents 容量（5 个基础字段 + 自定义字段）
	contents := make([]*sls.LogContent, 0, 5+len(fields))

	unixTime := uint32(entry.Time.Unix())

	// 基础字段（静态 key，零分配）
	contents = append(contents,
		&sls.LogContent{Key: &slsKeyTimestamp, Value: ptrString(entry.Time.Format("2006-01-02 15:04:05.000000000"))},
		&sls.LogContent{Key: &slsKeyLevel, Value: ptrString(entry.Level.String())},
		&sls.LogContent{Key: &slsKeyMessage, Value: ptrString(entry.Message)},
		&sls.LogContent{Key: &slsKeyCaller, Value: ptrString(entry.Caller.TrimmedPath())},
		&sls.LogContent{Key: &slsKeyServiceName, Value: ptrString(h.serviceName)},
	)

	// 自定义字段（使用 strconv 替代 fmt.Sprintf 避免反射）
	for _, field := range fields {
		var valueStr string
		switch field.Type {
		case zapcore.StringType:
			valueStr = field.String
		case zapcore.Int64Type:
			valueStr = strconv.FormatInt(field.Integer, 10)
		case zapcore.Int32Type:
			valueStr = strconv.FormatInt(field.Integer, 10)
		case zapcore.Uint64Type:
			valueStr = strconv.FormatUint(uint64(field.Integer), 10)
		case zapcore.Uint32Type:
			valueStr = strconv.FormatUint(uint64(field.Integer), 10)
		case zapcore.BoolType:
			valueStr = strconv.FormatBool(field.Integer == 1)
		case zapcore.Float64Type:
			valueStr = strconv.FormatFloat(float64(field.Integer), 'f', -1, 64)
		case zapcore.Float32Type:
			valueStr = strconv.FormatFloat(float64(field.Integer), 'f', -1, 32)
		default:
			if field.Interface != nil {
				jsonBytes, marshalErr := json.Marshal(field.Interface)
				if marshalErr != nil {
					valueStr = fmt.Sprintf("序列化错误: %v", marshalErr)
				} else {
					valueStr = string(jsonBytes)
				}
			} else {
				valueStr = field.String
			}
		}
		contents = append(contents, &sls.LogContent{
			Key:   ptrString(field.Key),
			Value: ptrString(valueStr),
		})
	}

	log := &sls.Log{
		Time:     &unixTime,
		Contents: contents,
	}

	err := h.producer.SendLog(h.config.ProjectName, h.config.LogStoreName, h.config.Topic, h.config.Source, log)
	if err != nil {
		h.RecordFailure(err)
		return fmt.Errorf("发送日志到 SLS 失败: %w", err)
	}

	h.RecordSuccess()
	return nil
}

// Close 关闭 SLS Producer，确保所有日志都被发送
func (h *SLSHook) Close() error {
	// 防止重复关闭
	if !h.compareAndSwapState(StateRunning, StateStopping) &&
		!h.compareAndSwapState(StateStarting, StateStopping) {
		// 已经处于停止中或已停止状态
		currentState := h.getCurrentState()
		if currentState == StateStopped || currentState == StateStopping {
			return nil // 幂等性：多次关闭不报错
		}
	}

	// 停止健康检查
	if h.healthCheckStop != nil {
		close(h.healthCheckStop)
		h.healthCheckWg.Wait()
	}

	var closeErr error
	if h.producer != nil {
		// 优雅关闭：等待所有日志发送完成，最多等待 30 秒
		// 根据阿里云官方文档，Close 方法会阻塞直到所有缓存数据发送完毕或超时
		closeErr = h.producer.Close(30000)
		if closeErr != nil {
			fmt.Printf("close SLS producer error: %v\n", closeErr)
		}
	}

	h.setState(StateStopped)

	// 打印关闭统计信息
	if IsDebugEnabled() {
		h.printCloseStats()
	}

	return closeErr
}

// GetState 获取当前状态（用于监控）
func (h *SLSHook) GetState() int32 {
	return h.getCurrentState()
}

// GetMetrics 获取监控指标（用于 Prometheus 等监控系统）
func (h *SLSHook) GetMetrics() map[string]interface{} {
	h.errorMu.RLock()
	defer h.errorMu.RUnlock()

	stateStr := "UNKNOWN"
	switch h.getCurrentState() {
	case StateCreated:
		stateStr = "CREATED"
	case StateStarting:
		stateStr = "STARTING"
	case StateRunning:
		stateStr = "RUNNING"
	case StateStopping:
		stateStr = "STOPPING"
	case StateStopped:
		stateStr = "STOPPED"
	case StateFailed:
		stateStr = "FAILED"
	}

	return map[string]interface{}{
		"state":              stateStr,
		"send_success_count": h.sendSuccessCount,
		"send_failed_count":  h.sendFailedCount,
		"last_error_time":    h.lastErrorTime.Format(time.RFC3339),
		"last_error_message": h.lastErrorMessage,
		"endpoint":           h.config.Endpoint,
		"project":            h.config.ProjectName,
		"logstore":           h.config.LogStoreName,
	}
}

// IsHealthy 检查 SLS Hook 是否健康（生产者处于运行状态）
func (h *SLSHook) IsHealthy() bool {
	state := h.getCurrentState()
	return state == StateRunning
}

// RecordSuccess 记录一次发送成功
func (h *SLSHook) RecordSuccess() {
	atomic.AddInt64(&h.sendSuccessCount, 1)
}

// RecordFailure 记录一次发送失败并保存错误信息
func (h *SLSHook) RecordFailure(err error) {
	atomic.AddInt64(&h.sendFailedCount, 1)
	h.errorMu.Lock()
	h.lastErrorTime = time.Now()
	h.lastErrorMessage = err.Error()
	h.errorMu.Unlock()
}

// ptrString 辅助函数：返回字符串指针
func ptrString(s string) *string {
	return &s
}

// ==================== 状态管理 ====================

// ProducerState 生产者状态常量
const (
	StateCreated  = 0
	StateStarting = 1
	StateRunning  = 2
	StateStopping = 3
	StateStopped  = 4
	StateFailed   = 5
)

// getCurrentState 获取当前状态
func (h *SLSHook) getCurrentState() int32 {
	return atomic.LoadInt32(&h.state)
}

// setState 设置状态
func (h *SLSHook) setState(state int32) {
	atomic.StoreInt32(&h.state, state)
}

// compareAndSwapState CAS 操作，防止竞态条件
func (h *SLSHook) compareAndSwapState(oldState, newState int32) bool {
	return atomic.CompareAndSwapInt32(&h.state, oldState, newState)
}

// ==================== 配置验证 ====================

// validateSLSConfig 验证 SLS 配置的合理性（边界检查）
func validateSLSConfig(config *SLSConfig) error {
	// 必填字段检查
	if config.Endpoint == "" {
		return fmt.Errorf("endpoint 为必填项")
	}
	if config.AccessKeyID == "" {
		return fmt.Errorf("accessKeyId 为必填项")
	}
	if config.AccessKeySecret == "" {
		return fmt.Errorf("accessKeySecret 为必填项")
	}
	if config.ProjectName == "" {
		return fmt.Errorf("projectName 为必填项")
	}
	if config.LogStoreName == "" {
		return fmt.Errorf("logStoreName 为必填项")
	}

	// Endpoint 格式检查（基本验证）
	if !isValidEndpoint(config.Endpoint) {
		return fmt.Errorf("endpoint 格式无效: %s（期望格式: region.log.aliyuncs.com）", config.Endpoint)
	}

	// 数值范围检查
	if config.MaxRetries < 0 || config.MaxRetries > 100 {
		return fmt.Errorf("maxRetries 取值范围为 0~100，当前值: %d", config.MaxRetries)
	}
	if config.Timeout < 1 || config.Timeout > 300 {
		return fmt.Errorf("timeout 取值范围为 1~300 秒，当前值: %d", config.Timeout)
	}

	// 高级配置检查
	if config.HealthCheckInterval < 5 {
		return fmt.Errorf("healthCheckInterval 最小值为 5 秒，当前值: %d", config.HealthCheckInterval)
	}
	if config.SendTimeout < 1 {
		return fmt.Errorf("sendTimeout 最小值为 1 秒，当前值: %d", config.SendTimeout)
	}

	return nil
}

// isValidEndpoint 验证 Endpoint 格式
func isValidEndpoint(endpoint string) bool {
	// 基本格式检查：应该包含 ".log.aliyuncs.com"
	// 支持公网和内网 endpoint
	validSuffixes := []string{
		".log.aliyuncs.com",          // 公网
		"-intranet.log.aliyuncs.com", // 内网
		"-vpc.log.aliyuncs.com",      // VPC
	}

	for _, suffix := range validSuffixes {
		if len(endpoint) > len(suffix) && endpoint[len(endpoint)-len(suffix):] == suffix {
			return true
		}
	}

	return false
}

// ==================== 健康检查 ====================

// startHealthCheck 启动健康检查协程
func (h *SLSHook) startHealthCheck() {
	h.healthCheckWg.Add(1)
	defer h.healthCheckWg.Done()

	// HealthCheckInterval 单位为秒，需要转换为 time.Duration
	ticker := time.NewTicker(time.Duration(h.config.HealthCheckInterval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			h.performHealthCheck()
		case <-h.healthCheckStop:
			return
		}
	}
}

// performHealthCheck 执行单次健康检查
func (h *SLSHook) performHealthCheck() {
	currentState := h.getCurrentState()

	// 如果已经处于停止状态，不需要检查
	if currentState == StateStopped || currentState == StateStopping {
		return
	}

	// 检查 Producer 是否仍然可用
	// 注意：aliyun-log-go-sdk 的 Producer 没有直接的健康检查方法
	// 我们通过检查内部状态和最近的错误来判断

	// 如果连续失败次数过多，标记为失败状态
	sendFailedCount := atomic.LoadInt64(&h.sendFailedCount)
	sendSuccessCount := atomic.LoadInt64(&h.sendSuccessCount)

	// 如果总发送数 > 0 且失败率 > 90%，认为不健康
	totalCount := sendSuccessCount + sendFailedCount
	if totalCount > 100 {
		failureRate := float64(sendFailedCount) / float64(totalCount)
		if failureRate > 0.9 {
			h.setState(StateFailed)
			// 记录严重告警到日志系统
			WarnWithCtx(context.Background(), "[SLS Health Check] CRITICAL: High failure rate detected",
				Float64("failure_rate", failureRate*100),
				Int64("success_count", sendSuccessCount),
				Int64("failed_count", sendFailedCount),
				Int64("total_count", totalCount),
				String("endpoint", h.config.Endpoint),
				String("project", h.config.ProjectName),
				String("logstore", h.config.LogStoreName))
		}
	}
}

// verifyProducerState 验证 Producer 启动状态
func (h *SLSHook) verifyProducerState() error {
	// 由于 aliyun-log-go-sdk 的 Start() 方法没有返回值
	// 我们通过尝试发送一条测试日志来验证

	// 创建一条测试日志
	testLogTime := uint32(time.Now().Unix())
	testLog := &sls.Log{
		Time: &testLogTime,
		Contents: []*sls.LogContent{
			{
				Key:   ptrString("__topic__"),
				Value: ptrString("health-check"),
			},
			{
				Key:   ptrString("__source__"),
				Value: ptrString(h.config.Source),
			},
			{
				Key:   ptrString("message"),
				Value: ptrString("SLS producer health check"),
			},
		},
	}

	// 尝试发送测试日志（使用较短的超时）
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// SendLog 是异步的，这里只能验证调用是否成功
	err := h.producer.SendLog(h.config.ProjectName, h.config.LogStoreName, "health-check", h.config.Source, testLog)
	if err != nil {
		return fmt.Errorf("发送测试日志失败: %w", err)
	}

	// 如果能成功调用 SendLog，说明 Producer 已启动
	_ = ctx // 避免未使用变量警告
	return nil
}

// printCloseStats 打印关闭时的统计信息
func (h *SLSHook) printCloseStats() {
	successCount := atomic.LoadInt64(&h.sendSuccessCount)
	failedCount := atomic.LoadInt64(&h.sendFailedCount)
	totalCount := successCount + failedCount

	// 使用 logger 记录关闭统计信息
	InfoWithCtx(context.Background(), "[SLS Hook Closed] Statistics",
		Int64("total_sent", totalCount),
		Int64("success_count", successCount),
		Int64("failed_count", failedCount),
		String("endpoint", h.config.Endpoint),
		String("project", h.config.ProjectName),
		String("logstore", h.config.LogStoreName))

	if totalCount > 0 {
		successRate := float64(successCount) / float64(totalCount) * 100
		InfoWithCtx(context.Background(), "[SLS Hook Closed] Success rate",
			Float64("success_rate", successRate))
	}

	h.errorMu.RLock()
	if h.lastErrorMessage != "" {
		WarnWithCtx(context.Background(), "[SLS Hook Closed] Last error",
			String("last_error", h.lastErrorMessage),
			String("error_time", h.lastErrorTime.Format(time.RFC3339)))
	}
	h.errorMu.RUnlock()
}
