package goredis

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/18721889353/sunshine/pkg/logger"
)

// requestIDHook 自定义 Redis Hook，用于从 Context 提取 request_id 并设置到 Span 属性
// request_id 的 context key 是全项目约定（由 pkg/logger 定义），因此本 Hook 无需任何注入点
type requestIDHook struct{}

// DialHook 实现 redis.DialHook 接口
func (h *requestIDHook) DialHook(next redis.DialHook) redis.DialHook {
	return next
}

// ProcessHook 实现 redis.ProcessHook 接口
func (h *requestIDHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		// 此时 redisotel (外层 Hook) 已创建 Redis Span 并注入 Context
		// 增强当前 Redis Span 的诊断属性
		enhanceRedisSpan(ctx, cmd)

		// 执行底层 Redis 命令
		err := next(ctx, cmd)

		// 大厂标准：记录错误到 Span（关键！）
		if err != nil {
			if span := trace.SpanFromContext(ctx); span.IsRecording() {
				span.RecordError(err,
					trace.WithAttributes(
						attribute.String("error.type", fmt.Sprintf("%T", err)),
						attribute.String("error.context", "redis-command-failed"),
						attribute.String("db.redis.command", cmd.Name()),
					),
				)
				span.SetStatus(codes.Error, fmt.Sprintf("redis %s failed: %v", cmd.Name(), err))
			}
		}

		return err
	}
}

// ProcessPipelineHook 实现 redis.ProcessPipelineHook 接口
func (h *requestIDHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		// 此时 redisotel (外层 Hook) 已创建 Pipeline Span
		// 直接设置 request_id 到当前的 Pipeline Span
		setRequestIDToRedisSpan(ctx)

		// 执行底层 Pipeline 命令
		err := next(ctx, cmds)

		// Pipeline 错误记录：先遍历每个命令，将失败命令记录为 Span Event（精确定位哪条命令失败），
		// 再记录整体错误与错误状态
		if err != nil {
			if span := trace.SpanFromContext(ctx); span.IsRecording() {
				recordFailedPipelineCmds(span, cmds)
				span.RecordError(err,
					trace.WithAttributes(
						attribute.String("error.type", fmt.Sprintf("%T", err)),
						attribute.String("error.context", "redis-pipeline-failed"),
					),
				)
				span.SetStatus(codes.Error, fmt.Sprintf("redis pipeline failed: %v", err))
			}
		}

		return err
	}
}

// recordFailedPipelineCmds 遍历 Pipeline 中的命令，将执行失败的命令记录为 Span Event
// Pipeline 场景排障刚需：整体错误只告知“失败了”，逐命令 Event 才能定位“哪条失败了”
func recordFailedPipelineCmds(span trace.Span, cmds []redis.Cmder) {
	for _, cmd := range cmds {
		cmdErr := cmd.Err()
		if cmdErr == nil {
			continue
		}
		span.AddEvent("redis.pipeline.command.failed",
			trace.WithAttributes(
				attribute.String("db.redis.command", cmd.Name()),
				attribute.String("error.type", fmt.Sprintf("%T", cmdErr)),
				attribute.String("error.message", cmdErr.Error()),
			),
		)
	}
}

// eval/evalsha 脚本命令参数索引常量
// 命令结构: evalsha SHA1 numkeys key [key ...] arg [arg ...]
// 索引      0=命令名  1=SHA1/脚本  2=numkeys  3=第一个 key
const (
	// evalScriptIdxSHA SHA1/脚本参数的索引（evalsha 的 SHA1、eval 的脚本文本）
	evalScriptIdxSHA = 1
	// evalScriptIdxCount numkeys 参数的索引
	evalScriptIdxCount = 2
	// evalScriptIdxKey 第一个 key 的起始索引
	evalScriptIdxKey = 3
)

// Span 属性显示长度限制常量
const (
	// maxKeyDisplayLen key 属性值的最大显示长度，避免 Span 属性过长
	maxKeyDisplayLen = 100

	// maxLockNameLen 分布式锁名称在 Span 中的最大显示长度
	maxLockNameLen = 20

	// maxSHANameLen Span 名称中 SHA1 前缀的显示长度
	maxSHANameLen = 8
)

// enhanceRedisSpan 增强 Redis Span 信息（大厂标准：添加 Key、耗时等诊断属性）
func enhanceRedisSpan(ctx context.Context, cmd redis.Cmder) {
	// 1. 提取 request_id
	setRequestIDToRedisSpan(ctx)

	// 2. 获取当前 Span
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}

	// 缓存 cmd.Args()，避免重复调用
	args := cmd.Args()
	commandName := cmd.Name()

	// 3. 统一 Span 名称为 redis.{command} 格式（大厂标准规范）
	// 特殊处理 eval/evalsha 脚本命令：按 numkeys 提取第一个 key，根据 Key 前缀自动识别分布式锁操作
	spanName := "redis." + commandName
	if keyStr, ok := evalScriptFirstKey(commandName, args); ok {
		switch {
		case strings.Contains(keyStr, "lock:") || strings.Contains(keyStr, "/dlock/"):
			spanName = "redis.lock:" + trimLockName(keyStr)
		case commandName == "evalsha":
			spanName = "redis.evalsha:" + trimScriptSHA(args[evalScriptIdxSHA])
		}
	}
	span.SetName(spanName)

	// 4. 添加 Redis 诊断属性
	// db.operation 为 OpenTelemetry 语义约定的标准键；db.redis.command 保留以兼容既有看板
	span.SetAttributes(
		attribute.String("db.system", "redis"),
		attribute.String("db.operation", commandName),
		attribute.String("db.redis.command", commandName),
	)

	// 5. 提取 Key（截取前 maxKeyDisplayLen 个字符，避免过长）
	// 脚本命令（eval/evalsha）按 numkeys 提取真实 key；非脚本命令取索引 1
	keyStr, hasKey := evalScriptFirstKey(commandName, args)
	if !hasKey && !isEvalCommand(commandName) && len(args) > 1 {
		keyStr = fmt.Sprintf("%v", args[1])
		hasKey = true
	}
	if hasKey {
		span.SetAttributes(attribute.String("db.redis.key", truncateKey(keyStr)))
	}
}

// isEvalCommand 判断是否为 Lua 脚本命令（eval/evalsha）
// 脚本命令的 args[1] 是脚本文本而非 key，不能作为 db.redis.key 上报
func isEvalCommand(commandName string) bool {
	return commandName == "eval" || commandName == "evalsha"
}

// evalScriptFirstKey 提取 eval/evalsha 脚本命令的第一个 key
// 按 numkeys 参数判断是否存在 key，避免把脚本参数误报为 key
// 非脚本命令或无 key 时返回 ok=false
func evalScriptFirstKey(commandName string, args []any) (string, bool) {
	if !isEvalCommand(commandName) || len(args) <= evalScriptIdxKey {
		return "", false
	}
	if evalScriptKeyCount(args[evalScriptIdxCount]) <= 0 {
		return "", false
	}
	return fmt.Sprintf("%v", args[evalScriptIdxKey]), true
}

// evalScriptKeyCount 解析 numkeys 参数（可能为 int/int64/string 类型），解析失败返回 0
func evalScriptKeyCount(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case string:
		count, err := strconv.Atoi(n)
		if err != nil {
			return 0
		}
		return count
	default:
		return 0
	}
}

// trimLockName 提取锁名称（去掉 : 与 / 前缀），并按最大长度截断（按 rune 截断，避免切断中文/emoji）
func trimLockName(keyStr string) string {
	lockName := keyStr
	if idx := strings.LastIndex(lockName, ":"); idx != -1 {
		lockName = lockName[idx+1:]
	} else if idx := strings.LastIndex(lockName, "/"); idx != -1 {
		lockName = lockName[idx+1:]
	}
	// 始终采用 truncateRunes 的返回值：它除了截断长度，还负责把非法 UTF-8 归一化
	name, _ := truncateRunes(lockName, maxLockNameLen)
	return name
}

// trimScriptSHA 截取 SHA1 前 maxSHANameLen 位用于 Span 名称
func trimScriptSHA(v any) string {
	sha1 := fmt.Sprintf("%v", v)
	// 同 trimLockName：截断与 UTF-8 归一化是一体的，不能只取截断分支
	short, _ := truncateRunes(sha1, maxSHANameLen)
	return short
}

// truncateRunes 按字符（rune）截断字符串到 limit 个字符，返回（结果, 是否被截断）。
// 两个职责都服务于「Span 属性/名称可安全上报」：
//   - 不按字节截断：多字节字符（中文/emoji）落在截断边界时会被切成乱码；
//   - 归一化非法 UTF-8：Redis key 常由业务方拼接外部输入而来，可能含非法字节，
//     而 OTLP 的 protobuf string 字段要求合法 UTF-8，原样上报会让整批 Span 被后端丢弃。
func truncateRunes(s string, limit int) (string, bool) {
	runes := []rune(s)
	if len(runes) <= limit {
		if utf8.ValidString(s) {
			return s, false
		}
		// 未超长但含非法字节：替换为 U+FFFD（不标记为截断，避免调用方误加省略号）
		return strings.ToValidUTF8(s, string(utf8.RuneError)), false
	}

	// 截断路径已由 []rune 转换完成非法字节的替换
	return string(runes[:limit]), true
}

// truncateKey 截取 key 到最大显示长度，超长追加省略号（按 rune 截断，避免切断中文/emoji）
func truncateKey(keyStr string) string {
	head, truncated := truncateRunes(keyStr, maxKeyDisplayLen)
	if !truncated {
		return head
	}
	return head + "..."
}

// setRequestIDToRedisSpan 从 Context 提取 request_id 并设置到当前 Span 属性
// 读取的是全项目约定的 logger.ContextKeyRequestID（与 gin/grpc middleware 写入的 key 一致），
// 提取不到时不设置属性（避免上报空值污染看板）
func setRequestIDToRedisSpan(ctx context.Context) {
	reqID, ok := ctx.Value(logger.ContextKeyRequestID).(string)
	if !ok || reqID == "" {
		return
	}

	// 获取当前 Span 并设置属性
	if span := trace.SpanFromContext(ctx); span.IsRecording() {
		span.SetAttributes(attribute.String("request_id", reqID))
	}
}
