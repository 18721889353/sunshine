// Package gocron 封装 robfig/cron/v3，提供定时任务的注册、删除、暂停与恢复能力。
//
// 核心功能：
//   - 工厂创建：New(opts...) 创建并启动调度器实例，生命周期归调用方，用完调用 Stop；
//     需要「在途任务跑完再退出」时用 Shutdown(ctx) 排空。
//   - 任务管理：Run 批量注册任务（重名、空函数、非法表达式均返回中文错误），DeleteTask 删除任务。
//   - 单次任务：Task.IsRunOnce 的任务执行一次后自动注销，无需手动删除。
//   - 暂停与恢复：Pause/Resume 配合配置热更新（如 openCron）暂停或恢复全部任务。
//   - 时间表达式：EverySecond/EveryMinute/EveryHour/EveryDay 生成 @every 表达式。
//   - 日志适配：robfig/cron 的日志转发到 pkg/logger，并将 EntryID 还原为任务名。
//
// 设计要点：
//   - 纯工厂 + 实例方法：包级无任何可变全局状态，多个 Scheduler 实例互不干扰，
//     应用侧（internal/server）持有单例并按需注入 config 供热更新使用；
//   - 每个实例由单一互斥锁 mu 统一保护 c/cronOpts/paused/taskFuncs，可安全并发调用；
//   - taskFuncs 是任务注册表的唯一事实来源，IsRunningTask/GetRunningTasks 均以此为准，
//     因此暂停期间注册的任务同样可见；
//   - nameID（任务名 → EntryID）由 mu 保护，供删除时反查；idName（EntryID → 任务名）
//     供 cron 日志回调读取（不经过 mu，故为 sync.Map），Resume/Stop 时整体重建，避免同号错配。
package gocron

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/robfig/cron/v3"

	"github.com/18721889353/sunshine/pkg/logger"
)

const (
	// SecondType 秒级粒度：cron 表达式为 6 字段（含秒位），如 "*/5 * * * * *"。
	SecondType = iota
	// MinuteType 分钟级粒度：cron 表达式为标准 5 字段，如 "0 15,45 9-12 * *"。
	MinuteType
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

	granularity int // 粒度：SecondType=秒级, MinuteType=分钟级
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
// 传入值 >= MinuteType 按分钟级处理，其余一律按秒级处理。
func WithGranularity(granularity int) Option {
	return func(o *options) {
		if granularity >= MinuteType {
			o.granularity = MinuteType
			return
		}
		o.granularity = SecondType
	}
}

// WithOnlyPrintError 设置仅打印错误日志。
func WithOnlyPrintError(enable bool) Option {
	return func(o *options) {
		o.isOnlyPrintError = enable
	}
}

// --- 调度器核心 ---

// Scheduler 定时任务调度器实例，由 New 创建并启动，生命周期归调用方。
// **零值不可用**：必须经 New 创建（零值的 taskFuncs/nameID 为 nil，调 Run 会 panic），
// 同包测试中的 &Scheduler{} 仅限只读 parseKVs 这类不触碰 nil 字段的场景。
// 所有方法内部自带锁，可安全并发调用；对已 Stop 的实例调用管理方法是安全的空操作。
type Scheduler struct {
	// mu 保护 c、cronOpts、paused 与任务注册表 taskFuncs 的并发访问；
	// 注册/删除/暂停/恢复/停止全部经由该锁串行化。
	// 任务回调内注销任务（如单次任务自动删除）也走同一把锁，
	// 与调度操作互不持锁等待，不会死锁。
	mu sync.Mutex

	// c 当前调度器实例，nil 表示已 Stop。
	c *cron.Cron
	// cronOpts 保存创建时的调度器选项，供 Resume 重建实例使用。
	cronOpts []cron.Option
	// paused 调度器是否处于暂停状态。
	paused bool
	// shuttingDown 标记 Shutdown 正在执行（含等待在途任务阶段），由 mu 保护。
	// 为 true 时拒绝 Run/Resume：否则并发的 Resume 会重建新实例，让 Shutdown
	// 等待错对象（旧实例）且新实例的在途任务不被排空；Run 则会把任务挂上
	// 即将被清空的旧实例，造成注册成功却静默丢失。
	shuttingDown bool
	// taskFuncs 任务注册表（唯一事实来源）：任务名 → 时间表达式与执行函数。
	taskFuncs map[string]taskFunc
	// nameID 任务名 → 当前调度器实例的 EntryID，供删除时反查；由 mu 保护。
	nameID map[string]cron.EntryID

	// idName 当前调度器实例的 EntryID → 任务名，供日志解析时还原任务名。
	// 读取方是 cron 的日志回调（不经过 mu），故用 sync.Map 而非普通 map；
	// 仅由持有 mu 的写方（注册/删除/重建/清空）更新。
	idName sync.Map

	// resumeFailures 累计 Resume 重挂失败次数（一次含失败的 Resume 计 1），
	// 供 Stats 观测；进程级累计，Stop 不重置。原子操作，不占 mu。
	resumeFailures atomic.Int64
}

// SchedulerStats 调度器状态快照，由 Stats 返回，供运维观测。
type SchedulerStats struct {
	Registered     int   // 已注册任务数（含暂停期间注册的；len(map)，O(1) 无切片分配）
	Paused         bool  // 是否处于暂停状态
	Stopped        bool  // 是否已停止（s.c == nil；含零值/从未 New 的情况——零值不可用，见 Scheduler 注释）
	ResumeFailures int64 // 累计 Resume 重挂失败次数（进程级累计，Stop 不重置）
}

// New 创建并启动定时任务调度器实例（纯工厂：无包级状态、无重复初始化错误）。
// 可通过 Option 自定义配置，如粒度（秒级/分钟级）与日志级别。
// 实例生命周期归调用方：停止后调用 Stop，实例不可复用，如需再用请重新 New。
//
// 不返回 error 是有意设计：当前选项均为纯值（粒度、日志开关），无 IO、无外部依赖、
// 无构造期失败路径；若未来引入需要校验的选项（如分布式锁依赖），签名将改为 (*Scheduler, error)。
// 这与同仓 jwt.New/gohttp.New 返回 error 的差异在于它们的选项含外部依赖校验。
func New(opts ...Option) *Scheduler {
	o := defaultOptions()
	o.apply(opts...)

	// 1. 组装调度器选项：日志适配 + panic 恢复
	log := &projectLog{isOnlyPrintError: o.isOnlyPrintError}
	localCronOpts := []cron.Option{
		cron.WithLogger(log),
		cron.WithChain(
			cron.Recover(log),
		),
	}
	// 2. 秒级粒度需要 6 字段表达式（含秒位），分钟级使用标准 5 字段
	if o.granularity == SecondType {
		localCronOpts = append(localCronOpts, cron.WithSeconds())
	}

	// 3. 创建并启动调度器
	s := &Scheduler{
		cronOpts:  localCronOpts,
		taskFuncs: make(map[string]taskFunc),
		nameID:    make(map[string]cron.EntryID),
	}
	log.scheduler = s
	s.c = cron.New(localCronOpts...)
	s.c.Start()

	return s
}

// Run 注册并运行任务列表。已存在的任务会被跳过并返回错误。
// 任一任务校验失败不会中断其余任务的注册，所有错误用 " || " 聚合返回。
// 注意：暂停期间注册的任务同样登记成功（挂在已停止的实例上，不触发执行），
// 将在 Resume 后生效。调度器已 Stop 时返回错误。
func (s *Scheduler) Run(tasks ...*Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Shutdown 期间拒绝注册：任务会挂上即将被清空的旧实例，造成注册成功却静默丢失
	if s.shuttingDown {
		return errors.New("cron 调度器正在关闭")
	}
	if s.c == nil {
		return errors.New("cron 调度器已停止")
	}

	var errs []string
	for _, task := range tasks {
		if err := s.addTaskLocked(task); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, " || "))
	}

	return nil
}

// addTaskLocked 校验并登记单个任务，调用方必须持有 s.mu。
// 执行顺序：参数校验 → 构造执行函数 → 挂载到调度器 → 登记任务注册表；
// 挂载失败时注册表不落盘，避免残留无效任务在 Resume 时被复活。
func (s *Scheduler) addTaskLocked(task *Task) error {
	// 1. 参数校验
	if task == nil {
		return errors.New("任务不能为空")
	}
	if task.Name == "" {
		return errors.New("任务名不能为空")
	}
	if task.Fn == nil {
		return fmt.Errorf("任务 '%s' 的函数为空", task.Name)
	}
	if _, exist := s.taskFuncs[task.Name]; exist {
		return fmt.Errorf("任务 '%s' 已存在", task.Name)
	}

	// 2. 构造实际执行函数：单次任务包装为执行后自动注销（不修改入参 task.Fn）
	fn := s.buildTaskFn(task)

	// 3. 挂载到调度器，非法时间表达式在此处报错。
	// 暂停期间调度器已停止，robfig/cron v3 的 Schedule（cron.go:168 `!c.running` 分支）
	// 仅把条目本地 append、不与调度循环通信，因此安全且不会触发执行；
	// Resume 时会按任务注册表整体重建所有条目。
	id, err := s.c.AddFunc(task.TimeSpec, fn)
	if err != nil {
		return fmt.Errorf("运行任务 '%s' 失败: %w", task.Name, err)
	}

	// 4. 登记任务注册表与 EntryID 映射
	s.taskFuncs[task.Name] = taskFunc{spec: task.TimeSpec, fn: fn}
	s.nameID[task.Name] = id
	s.idName.Store(id, task.Name)

	return nil
}

// buildTaskFn 构造任务的实际执行函数。
// IsRunOnce 的任务包装为「执行后自动注销」，保证只执行一次；
// 包装不修改入参 task.Fn，调用方结构体保持原样。
func (s *Scheduler) buildTaskFn(task *Task) func() {
	job := task.Fn
	if !task.IsRunOnce {
		return job
	}

	name := task.Name
	return func() {
		job()
		s.DeleteTask(name)
	}
}

// IsRunningTask 判断指定名称的任务是否已注册（含暂停期间注册的任务）。
func (s *Scheduler) IsRunningTask(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, exist := s.taskFuncs[name]
	return exist
}

// GetRunningTasks 获取所有已注册的任务名称列表，按字典序排序。
// 仅需数量时请用 Stats().Registered（O(1) 无切片分配），本方法每次分配并排序，不宜高频调用。
func (s *Scheduler) GetRunningTasks() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	names := make([]string, 0, len(s.taskFuncs))
	for name := range s.taskFuncs {
		names = append(names, name)
	}
	slices.Sort(names)

	return names
}

// DeleteTask 停止并删除指定名称的任务。对不存在的任务是幂等操作。
func (s *Scheduler) DeleteTask(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteTaskLocked(name)
}

// deleteTaskLocked 删除任务注册表条目与 EntryID 映射，调用方必须持有 s.mu。
// 先删注册表再摘条目，保证任何后续 Resume 都不会复活该任务。
func (s *Scheduler) deleteTaskLocked(name string) {
	// 1. 从任务注册表移除
	delete(s.taskFuncs, name)

	// 2. 从当前调度器实例摘除条目
	id, exist := s.nameID[name]
	if !exist {
		return
	}
	delete(s.nameID, name)
	s.idName.Delete(id)
	if s.c != nil {
		s.c.Remove(id)
	}
}

// --- 暂停与恢复 ---

// Pause 暂停所有定时任务：停止调度器，任务注册表保持不变。
// 即使已暂停或已 Stop 也可安全调用（幂等）。
func (s *Scheduler) Pause() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.c == nil || s.paused {
		return
	}
	// Stop 仅停止调度循环（cron.go:327 `c.stop <- struct{}{}`），
	// 不等待执行中的任务结束，因此不会因任务回调等待本锁而阻塞
	s.c.Stop()
	s.paused = true
}

// Resume 恢复所有定时任务（需先调用 Pause），返回恢复失败的聚合错误。
// 使用创建时保存的选项重建调度器，并按任务注册表（名字典序）重新注册全部任务；
// 单个任务恢复失败会记警告日志并聚合进返回值（与 Run 的 " || " 聚合先例一致），
// 调用方必须检查返回值，否则会静默丢任务（看似恢复成功、实际部分任务未执行）。
// 未暂停或已 Stop 时为空操作，返回 nil；Shutdown 期间返回「正在关闭」错误。
func (s *Scheduler) Resume() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. Shutdown 期间拒绝恢复：并发重建新实例会让 Shutdown 等待错对象，
	//    且新实例的在途任务不被排空
	if s.shuttingDown {
		return errors.New("cron 调度器正在关闭")
	}
	// 2. 已停止或未暂停时不做事
	if s.c == nil || !s.paused {
		return nil
	}

	// 3. 重建调度器（robfig/cron 的 Stop 不可逆，必须新建实例）
	s.c = cron.New(s.cronOpts...)
	s.c.Start()
	s.paused = false

	// 4. 新实例的 EntryID 从 1 重新计数（cron.go:161 `c.nextID++`），
	//    必须先清空旧映射再重建，否则同号 EntryID 会映射到错误任务名
	s.clearEntryIDs()
	var errs []string
	// 按任务名字典序重挂：map 遍历顺序随机，字典序使 EntryID 分配与聚合错误顺序可预测
	for _, name := range slices.Sorted(maps.Keys(s.taskFuncs)) {
		tf := s.taskFuncs[name]
		id, err := s.c.AddFunc(tf.spec, tf.fn)
		if err != nil {
			// 单个任务失败不中断其余任务恢复，但必须对外暴露（否则丢任务不可感知）
			logger.WarnWithCtx(context.Background(), "[gocron] 恢复任务失败",
				logger.String("task", name),
				logger.Err(err),
			)
			errs = append(errs, fmt.Sprintf("恢复任务 '%s' 失败: %v", name, err))
			continue
		}
		s.nameID[name] = id
		s.idName.Store(id, name)
	}

	if len(errs) > 0 {
		s.resumeFailures.Add(1)
		return errors.New(strings.Join(errs, " || "))
	}
	return nil
}

// IsPaused 返回调度器是否处于暂停状态（已 Stop 时返回 false）。
func (s *Scheduler) IsPaused() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.paused
}

// Stats 返回当前状态快照，供运维观测（高频调用安全：任务数取 len(map)，
// 不分配 GetRunningTasks 那样的切片）。
func (s *Scheduler) Stats() SchedulerStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return SchedulerStats{
		Registered:     len(s.taskFuncs),
		Paused:         s.paused,
		Stopped:        s.c == nil,
		ResumeFailures: s.resumeFailures.Load(),
	}
}

// Stop 停止调度器并清空全部状态（任务注册表与 EntryID 映射）。
// 实例不可复用，如需再调度请重新 New。对已停止的调度器是幂等操作。
// 注意：Stop 不等待在途任务，进程退出时正在执行的任务可能被截断；
// 需要任务跑完再退出时用 Shutdown(ctx)。
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.c != nil {
		// 不等待执行中的任务结束（返回的 ctx 被忽略），避免任务回调反向等待本锁造成阻塞
		s.c.Stop()
		s.c = nil
	}
	s.paused = false
	s.cronOpts = nil
	s.taskFuncs = make(map[string]taskFunc)
	s.nameID = make(map[string]cron.EntryID)
	s.clearEntryIDs()
}

// Shutdown 优雅关闭：停止调度循环后，等待在途任务全部结束（或 ctx 超时），再清空状态。
// 与 Stop 的区别：Stop 立即返回、不等待在途任务；进程退出时需要任务（如发通知、写 DB）
// 完整跑完再退出，应使用本方法。
//
// 等待期间不持有 s.mu，任务回调内的 DeleteTask 等操作可正常进行，不会死锁；
// 同时置 shuttingDown 标记拒绝并发的 Run/Resume（否则 Resume 重建新实例会让
// Shutdown 等待错对象、新实例在途任务不被排空；Run 会把任务挂上即将清空的实例）。
// 在途信号由 robfig/cron 返回的 ctx 提供（v3.0.1 cron.go Stop 中 jobWaiter.Wait 后 cancel）。
// ctx 超时后仍会清空状态（调度已停，残留任务不会继续执行），并返回包裹 ctx.Err() 的错误，
// 由调用方决定是否继续强退。对已停止的调度器是幂等操作，返回 nil。
func (s *Scheduler) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	if s.c == nil {
		s.mu.Unlock()
		return nil
	}
	c := s.c
	s.shuttingDown = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.shuttingDown = false
		s.mu.Unlock()
	}()

	// 1. c.Stop() 停止调度循环并返回「在途任务全部结束」的信号；
	//    等待阶段不持锁，避免任务回调反向等锁造成死锁
	var waitErr error
	select {
	case <-c.Stop().Done():
	case <-ctx.Done():
		waitErr = fmt.Errorf("等待在途任务排空超时: %w", ctx.Err())
	}

	// 2. 无论排空是否成功都清空状态（幂等，重复 Stop 安全）
	s.Stop()
	return waitErr
}

// clearEntryIDs 清空 EntryID → 任务名映射。调用方必须持有 s.mu。
// Resume 重建实例后 EntryID 会从 1 重新计数，必须整体清空防止同号错配任务名。
func (s *Scheduler) clearEntryIDs() {
	s.idName.Range(func(key, _ any) bool {
		s.idName.Delete(key)
		return true
	})
}

// parseKVs 将 robfig/cron 的 key-value 对转换为 logger.Field 列表。
// 同时将 entry ID 替换为任务名称，便于日志阅读；奇数参数对直接丢弃。
// 该方法由 cron 日志回调触发（不持有 s.mu），只读取 sync.Map 保护的 idName。
func (s *Scheduler) parseKVs(kvs []any) []logger.Field {
	if len(kvs)%2 != 0 {
		return nil
	}

	fields := make([]logger.Field, 0, len(kvs)/2)
	for i := 0; i < len(kvs); i += 2 {
		key, ok := kvs[i].(string)
		if !ok {
			continue
		}
		value := kvs[i+1]

		// 将 cron EntryID 替换为任务名称。
		// 依赖 robfig/cron/v3 的内部日志键名 "entry"（v3.0.1 cron.go 的 run loop 日志调用，
		// 非 API 层承诺）；升级依赖时若该键改名，映射会静默退化为 entry=数字，
		// 守护测试见 TestSchedulerParseKVs（5 子测试）与 FuzzParseKVs（偶数路径固定追加 entry 对）。
		if key == "entry" {
			id, isID := value.(cron.EntryID)
			if isID {
				key = "task"
				if taskName, exist := s.idName.Load(id); exist {
					value = taskName
				}
			}
		}

		fields = append(fields, logger.Any(key, value))
	}

	return fields
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
// robfig/cron 的 @every 走 time.ParseDuration，不支持天单位，故换算为小时：
// 语义是固定 24h×N 间隔，从注册时刻起计时，不是「每天同一时刻」，也不受时区/DST 影响。
func Everyday(size int) string {
	return fmt.Sprintf("@every %dh", size*24)
}

// --- 日志适配 ---

// projectLog 适配 robfig/cron/v3 的 Logger 接口，将日志转发到 sunshine logger。
type projectLog struct {
	isOnlyPrintError bool

	// scheduler 指向所属实例，用于将 EntryID 还原为任务名。
	scheduler *Scheduler
}

// Info 输出 info 级别日志。
func (l *projectLog) Info(msg string, keysAndValues ...any) {
	if l.isOnlyPrintError {
		return
	}
	if msg == "wake" { // 忽略 wake 信号，无需记录
		return
	}
	fields := l.scheduler.parseKVs(keysAndValues)
	logger.InfoWithCtx(context.Background(), "cron_"+msg, fields...)
}

// Error 输出 error 级别日志。
func (l *projectLog) Error(err error, msg string, keysAndValues ...any) {
	fields := l.scheduler.parseKVs(keysAndValues)
	fields = append(fields, logger.Err(err))
	logger.ErrorWithCtx(context.Background(), "cron_"+msg, fields...)
}
