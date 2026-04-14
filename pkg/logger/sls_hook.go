package logger

import (
	"context"
	"encoding/json"
	"fmt"
	sls "github.com/aliyun/aliyun-log-go-sdk"
	"github.com/aliyun/aliyun-log-go-sdk/producer"
	"go.opentelemetry.io/otel/trace"
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
}

// SLSHook 阿里云 SLS 日志钩子
type SLSHook struct {
	config      *SLSConfig
	producer    *producer.Producer
	serviceName string
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
		return nil, fmt.Errorf("SLS config is incomplete: endpoint, accessKeyId, and accessKeySecret are required")
	}

	if config.ProjectName == "" || config.LogStoreName == "" {
		return nil, fmt.Errorf("SLS config is incomplete: projectName and logStoreName are required")
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

	// 创建生产者配置
	producerConfig := producer.GetDefaultProducerConfig()
	producerConfig.Endpoint = config.Endpoint
	producerConfig.AccessKeyID = config.AccessKeyID
	producerConfig.AccessKeySecret = config.AccessKeySecret
	producerConfig.TotalSizeLnBytes = 512 * 1024 * 1024 // 512MB (基于压测结果优化)
	producerConfig.MaxBatchCount = 4096                 // 单个 Batch 最大日志条数（已优化）
	producerConfig.MaxBatchSize = 3 * 1024 * 1024       // 3MB (减少网络请求次数)
	producerConfig.LingerMs = 2000                      // 2秒刷新间隔（平衡实时性与吞吐量）
	producerConfig.Retries = config.MaxRetries
	// 注意: TimeoutMilliseconds 字段可能不存在，使用默认值

	// 创建生产者
	p, err := producer.NewProducer(producerConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create SLS producer: %w", err)
	}

	// 启动生产者（Start 方法没有返回值）
	p.Start()

	hook := &SLSHook{
		config:   config,
		producer: p,
	}

	return hook, nil
}

// Hook 实现 CustomHookWithCtx 接口
func (h *SLSHook) Hook(ctx context.Context, entry zapcore.Entry, fields []Field) error {
	// 构建日志内容（预分配容量，减少扩容）
	logData := make(map[string]interface{}, len(fields)+8)

	// 基础字段
	logData["timestamp"] = entry.Time.Format("2006-01-02 15:04:05.000000")
	logData["level"] = entry.Level.String()
	logData["message"] = entry.Message
	logData["caller"] = entry.Caller.TrimmedPath()

	// 服务信息
	logData["service_name"] = h.config.Source

	// 添加上下文字段（request_id, trace_id）- 只在有值时添加
	if reqID := getRequestIDFromCtx(ctx); reqID != "" {
		logData["request_id"] = reqID
	}

	// 提取 trace_id (OpenTelemetry) - 仅在需要时提取
	if spanCtx := trace.SpanContextFromContext(ctx); spanCtx.IsValid() {
		if traceID := spanCtx.TraceID().String(); traceID != "00000000000000000000000000000000" {
			logData["trace_id"] = traceID
		}
	}

	// 添加自定义字段
	for _, field := range fields {
		switch field.Type {
		case zapcore.StringType:
			logData[field.Key] = field.String
		case zapcore.Int64Type, zapcore.Int32Type:
			logData[field.Key] = field.Integer
		case zapcore.Uint64Type, zapcore.Uint32Type:
			logData[field.Key] = field.Integer
		case zapcore.BoolType:
			logData[field.Key] = field.Integer == 1
		case zapcore.Float64Type, zapcore.Float32Type:
			logData[field.Key] = float64(field.Integer)
		default:
			// 其他类型尝试序列化为 JSON
			if field.Interface != nil {
				jsonBytes, _ := json.Marshal(field.Interface)
				logData[field.Key] = string(jsonBytes)
			} else {
				logData[field.Key] = field.String
			}
		}
	}

	// 序列化日志
	logBytes, err := json.Marshal(logData)
	if err != nil {
		return fmt.Errorf("failed to marshal log data: %w", err)
	}

	// 创建 SLS Log
	unixTime := uint32(entry.Time.Unix())
	log := &sls.Log{
		Time: &unixTime,
		Contents: []*sls.LogContent{
			{
				Key:   ptrString("__topic__"),
				Value: ptrString(h.config.Topic),
			},
			{
				Key:   ptrString("__source__"),
				Value: ptrString(h.config.Source),
			},
			{
				Key:   ptrString("content"),
				Value: ptrString(string(logBytes)),
			},
		},
	}

	// 异步发送日志到 SLS
	err = h.producer.SendLog(h.config.ProjectName, h.config.LogStoreName, h.config.Topic, h.config.Source, log)
	if err != nil {
		return fmt.Errorf("failed to send log to SLS: %w", err)
	}

	return nil
}

// Close 关闭 SLS Producer，确保所有日志都被发送
func (h *SLSHook) Close() error {
	if h.producer != nil {
		// 等待所有日志发送完成，最多等待 30 秒
		h.producer.Close(30000)
	}
	return nil
}

// ptrString 辅助函数：返回字符串指针
func ptrString(s string) *string {
	return &s
}
