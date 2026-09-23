// Package gocron 提供定时任务调度功能，封装 robfig/cron/v3。
package gocron

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/robfig/cron/v3"

	"github.com/18721889353/sunshine/pkg/logger"
)

var (
	// SecondType 秒级粒度
	SecondType = 0
	// MinuteType 分钟级粒度
	MinuteType = 1
)

var (
	c *cron.Cron
	// nameID 任务名称 → cron.EntryID 映射，用于增删改查
	nameID = sync.Map{}
	// idName cron.EntryID → 任务名称映射，用于日志打印
	idName = sync.Map{}

	// pauseMu 保护 c、paused、cronOpts 在 Pause/Resume 期间的并发安全
	pauseMu sync.Mutex
	paused  bool
	// cronOpts 保存 Init 时的选项，供 Resume 重建调度器使用
	cronOpts []cron.Option
	// taskFuncs 保存任务函数包装，供 Resume 重新注册任务使用
	taskFuncs = map[string]taskFunc{}
)

// taskFunc 存储任务的时间表达式和执行函数，用于暂停后恢复时重新注册。
type taskFunc struct {
	spec string
	fn   func()
}

// Task 定时任务
type Task struct {
	// 时间表达式，格式: 秒(0-59) 分(0-59) 时(0-23) 日(1-31) 月(1-12) 周(0-6)
	// "*/5 * * * * *" 每5秒执行
	// "0 15,45 9-12 * * *" 每天9点到12点的第15和45分钟执行
	TimeSpec string

	Name      string // 任务名称
	Fn        func() // 任务函数
	IsRunOnce bool   // 是否只执行一次
}

// --- 配置选项 ---

type options struct {
	isOnlyPrintError bool // 仅打印错误日志，默认 false

	granularity int // 粒度：0=秒级, 1=分钟级
}

func defaultOptions() *options {
	return &options{
		isOnlyPrintError: false,

		granularity: SecondType,
	}
}

func (o *options) apply(opts ...Option) {
	for _, opt := range opts {
		opt(o)
	}
}

// Option 定时任务配置选项。
type Option func(*options)

// WithGranularity 设置调度粒度（秒级或分钟级）。
func WithGranularity(granularity int) Option {
	return func(o *options) {
		if granularity >= MinuteType {
			granularity = MinuteType
		} else {
			granularity = SecondType
		}
		o.granularity = granularity
	}
}

// WithOnlyPrintError 设置仅打印错误日志。
func WithOnlyPrintError(enable bool) Option {
	return func(o *options) {
		o.isOnlyPrintError = enable
	}
}

// --- 调度器核心 ---

// Init 初始化并启动定时任务调度器。
// 可通过 Option 自定义配置，如粒度（秒级/分钟级）。
func Init(opts ...Option) error {
	o := defaultOptions()
	o.apply(opts...)

	log := &projectLog{isOnlyPrintError: o.isOnlyPrintError}
	localCronOpts := []cron.Option{
		cron.WithLogger(log),
		cron.WithChain(
			cron.Recover(log),
		),
	}
	if o.granularity == SecondType {
		localCronOpts = append(localCronOpts, cron.WithSeconds()) // 秒级粒度，默认分钟级
	}

	c = cron.New(localCronOpts...)
	c.Start()
	cronOpts = localCronOpts

	return nil
}

// Run 注册并运行任务列表。已存在的任务会被跳过并返回错误。
func Run(tasks ...*Task) error {
	if c == nil {
		return errors.New("cron 未初始化")
	}

	var errs []string
	for _, task := range tasks {
		if IsRunningTask(task.Name) {
			errs = append(errs, fmt.Sprintf("任务 '%s' 已存在", task.Name))
			continue
		}

		if err := checkRunOnce(task); err != nil {
			errs = append(errs, err.Error())
			continue
		}

		pauseMu.Lock()
		taskFuncs[task.Name] = taskFunc{spec: task.TimeSpec, fn: task.Fn}
		pauseMu.Unlock()

		id, err := c.AddFunc(task.TimeSpec, task.Fn)
		if err != nil {
			errs = append(errs, fmt.Sprintf("运行任务 '%s' 失败: %v", task.Name, err))
			continue
		}
		idName.Store(id, task.Name)
		nameID.Store(task.Name, id)
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, " || "))
	}

	return nil
}

// checkRunOnce 检查任务函数是否为空，如果是单次执行任务则包装函数。
func checkRunOnce(task *Task) error {
	if task.Fn == nil {
		return fmt.Errorf("任务 '%s' 的函数为空", task.Name)
	}
	if task.IsRunOnce {
		job := task.Fn
		task.Fn = func() {
			job()
			DeleteTask(task.Name)
		}
	}
	return nil
}

// IsRunningTask 判断指定名称的任务是否在运行。
func IsRunningTask(name string) bool {
	_, ok := nameID.Load(name)
	return ok
}

// GetRunningTasks 获取所有正在运行的任务名称列表。
func GetRunningTasks() []string {
	var names []string
	nameID.Range(func(key, _ any) bool {
		if name, ok := key.(string); ok {
			names = append(names, name)
		}
		return true
	})
	return names
}

// DeleteTask 停止并删除指定名称的任务。
func DeleteTask(name string) {
	if id, ok := nameID.Load(name); ok {
		entryID, isOk := id.(cron.EntryID)
		if !isOk {
			return
		}
		c.Remove(entryID)
		nameID.Delete(name)
		idName.Delete(entryID)
	}
}

// --- 暂停与恢复 ---

// Pause 暂停所有定时任务。
// 即使 cron 未初始化或已暂停也可安全调用。
func Pause() {
	pauseMu.Lock()
	defer pauseMu.Unlock()

	if paused || c == nil {
		return
	}
	c.Stop()
	paused = true
}

// Resume 恢复所有定时任务（需先调用 Pause）。
// 使用 Init 时保存的选项重建调度器，并重新注册所有已注册的任务。
// 如果 cron 未初始化或未暂停则不做任何操作。
func Resume() {
	pauseMu.Lock()

	if !paused || c == nil {
		pauseMu.Unlock()
		return
	}

	// 持锁期间复制任务列表，释放锁后再重新注册，避免长时间持锁
	paused = false
	snapshot := make(map[string]taskFunc, len(taskFuncs))
	for name, tf := range taskFuncs {
		snapshot[name] = tf
	}
	pauseMu.Unlock()

	// 重建调度器并重新注册任务（robfig/cron 的 Stop 不可逆，必须新建实例）
	c = cron.New(cronOpts...)
	c.Start()

	for name, tf := range snapshot {
		id, err := c.AddFunc(tf.spec, tf.fn)
		if err != nil {
			continue
		}
		idName.Store(id, name)
		nameID.Store(name, id)
	}
}

// IsPaused 返回调度器是否处于暂停状态。
func IsPaused() bool {
	pauseMu.Lock()
	defer pauseMu.Unlock()
	return paused
}

// Stop 停止所有定时任务调度器。
func Stop() {
	if c != nil {
		c.Stop()
	}
}

// --- 时间表达式便捷函数 ---

// EverySecond 生成每隔指定秒数执行的时间表达式（1~59）。
func EverySecond(size int) string {
	return fmt.Sprintf("@every %ds", size)
}

// EveryMinute 生成每隔指定分钟数执行的时间表达式（1~59）。
func EveryMinute(size int) string {
	return fmt.Sprintf("@every %dm", size)
}

// EveryHour 生成每隔指定小时数执行的时间表达式（1~23）。
func EveryHour(size int) string {
	return fmt.Sprintf("@every %dh", size)
}

// Everyday 生成每隔指定天数执行的时间表达式（1~31）。
func Everyday(size int) string {
	return fmt.Sprintf("@every %dh", size*24)
}

// --- 日志适配 ---

// projectLog 适配 robfig/cron/v3 的 Logger 接口，将日志转发到 sunshine logger。
type projectLog struct {
	isOnlyPrintError bool
}

// Info 输出 info 级别日志。
func (l *projectLog) Info(msg string, keysAndValues ...any) {
	if l.isOnlyPrintError {
		return
	}
	if msg == "wake" { // 忽略 wake 信号，无需记录
		return
	}
	msg = "cron_" + msg
	fields := parseKVs(keysAndValues)
	logger.InfoWithCtx(context.Background(), msg, fields...)
}

// Error 输出 error 级别日志。
func (l *projectLog) Error(err error, msg string, keysAndValues ...any) {
	fields := parseKVs(keysAndValues)
	fields = append(fields, logger.Err(err))
	msg = "cron_" + msg
	logger.ErrorWithCtx(context.Background(), msg, fields...)
}

// parseKVs 将 robfig/cron 的 key-value 对转换为 logger.Field 列表。
// 同时将 entry ID 替换为任务名称，便于日志阅读。
func parseKVs(kvs any) []logger.Field {
	var fields []logger.Field

	infos, ok := kvs.([]any)
	if !ok {
		return fields
	}

	l := len(infos)
	if l%2 == 1 {
		return fields
	}

	for i := 0; i < l; i += 2 {
		key, ok := infos[i].(string)
		if !ok {
			continue
		}
		value := infos[i+1]

		// 将 cron EntryID 替换为任务名称
		if key == "entry" {
			if id, ok := value.(cron.EntryID); ok {
				key = "task"
				if v, isExist := idName.Load(id); isExist {
					value = v
				}
			}
		}

		fields = append(fields, logger.Any(key, value))
	}

	return fields
}
