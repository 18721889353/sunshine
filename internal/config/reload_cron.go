package config

import (
	"context"
	"reflect"
	"strings"

	"github.com/18721889353/sunshine/pkg/gocron"
	"github.com/18721889353/sunshine/pkg/logger"
)

// CronTask 单个定时任务的配置（cron.tasks 的 value 结构）。
// 本类型定义在手写文件中：yml 通过 @type:map[string]CronTask 注解指定
// 生成的字段类型为 map[string]CronTask，value 结构由本文件提供。
// enable/timeSpec 由框架统一消费；任务专属字段由任务在执行时自行读取（config.Get()）。
type CronTask struct {
	Enable   *bool  `yaml:"enable" json:"enable"`     // 是否启用该任务, nil: 默认启用, true: 启用, false: 停用(从调度器移除)
	TimeSpec string `yaml:"timeSpec" json:"timeSpec"` // 时间表达式, 为空则使用代码内注册的默认值

}

// cronTaskSource 全量定时任务注册表（任务名 -> 任务定义），由 SetCronTasks 在启动时注入。
// 热更新需要根据任务定义重新挂载任务（Run 需要任务函数 Fn），
// 未注入时热更新会跳过并输出警告日志（与 cronScheduler 的注入惯例一致）。
var cronTaskSource map[string]*gocron.Task

// SetCronTasks 注入全量定时任务注册表，用于 cron.tasks 配置热更新。
// 必须传入未经过滤的全量任务列表：启动时被 enable=false 过滤掉的任务，
// 热更新改为启用时仍需从这里取任务定义重新挂载。
// 必须在 NewCronServer() 之前调用（仓内惯例：与 SetCronScheduler 同处接线）。
func SetCronTasks(tasks []*gocron.Task) {
	source := make(map[string]*gocron.Task, len(tasks))
	for _, task := range tasks {
		source[task.Name] = task
	}
	cronTaskSource = source
}

// ApplyCronTasks 启动时按 cron.tasks 配置过滤并覆盖任务时间表达式。
// 生效规则：
//   - 未在 cron.tasks 中配置的任务：保持代码内默认行为（启用 + 注册时的 TimeSpec）
//   - enable=false 的任务：从返回列表剔除，不挂载到调度器
//   - timeSpec 非空的任务：用配置值覆盖注册时的 TimeSpec（返回副本，不修改注册表原对象）
func ApplyCronTasks(tasks []*gocron.Task) []*gocron.Task {
	cfg := Get()
	taskCfgs := cfg.Cron.Tasks
	if len(taskCfgs) == 0 {
		return tasks
	}

	ctx := context.Background()
	// 1. 逐个任务应用配置：停用的剔除，配置了 timeSpec 的覆盖（拷贝副本）
	result := make([]*gocron.Task, 0, len(tasks))
	for _, task := range tasks {
		taskCfg, exist := lookupCronTask(taskCfgs, task.Name)
		if !exist {
			result = append(result, task)
			continue
		}
		// 2.1 enable=false：剔除任务，不挂载到调度器（enable 未配置即 nil，视为启用）
		if taskCfg.Enable != nil && !*taskCfg.Enable {
			logger.InfoWithCtx(ctx, "[cron] 任务已按配置停用", logger.String("task", task.Name))
			continue
		}
		// 2.2 timeSpec 非空且与代码默认值不同：覆盖执行时间表达式
		if taskCfg.TimeSpec != "" && taskCfg.TimeSpec != task.TimeSpec {
			overridden := *task
			overridden.TimeSpec = taskCfg.TimeSpec
			task = &overridden
			logger.InfoWithCtx(ctx, "[cron] 任务时间表达式已按配置覆盖",
				logger.String("task", task.Name),
				logger.String("timeSpec", taskCfg.TimeSpec),
			)
		}
		result = append(result, task)
	}

	// 2. 配置了但未注册的任务名：大概率是拼写错误，告警提示
	//    （大小写不敏感匹配，原因见 lookupCronTask：viper 会把配置 key 转小写）
	for name := range taskCfgs {
		matched := false
		for _, task := range tasks {
			if strings.EqualFold(name, task.Name) {
				matched = true
				break
			}
		}
		if !matched {
			logger.WarnWithCtx(ctx, "[cron] cron.tasks 配置了未注册的任务名，已忽略（检查任务名拼写）",
				logger.String("task", name),
			)
		}
	}

	return result
}

// reloadCronTasks 定时任务单任务配置热更新回调。
// 当 Nacos 配置中的 cron.tasks 发生变更时，按任务粒度执行挂载/移除，
// 精确控制单个任务的启停与执行时间表达式，替代 openCron 的整体暂停/恢复。
// 前提：启动时 openCron=true（cronServer 已创建且调度器已注入），否则跳过。
func reloadCronTasks(oldCfg, newCfg *Config) {
	ctx := context.Background()

	// 1. cron 配置未发生变更则跳过
	if reflect.DeepEqual(oldCfg.Cron, newCfg.Cron) {
		return
	}

	// 2. 调度器未注入时跳过（启动时 openCron=false，不会创建 cronServer）
	if cronScheduler == nil {
		logger.WarnWithCtx(ctx, "[config reload] cronScheduler 未注入，跳过 cron.tasks 热更新")
		return
	}
	// 3. 任务注册表未注入时跳过（挂载任务需要任务函数定义）
	if len(cronTaskSource) == 0 {
		logger.WarnWithCtx(ctx, "[config reload] cron 任务注册表未注入，跳过 cron.tasks 热更新")
		return
	}

	// 4. 遍历全量已注册任务，逐个对比新旧生效配置并调整
	scheduleChanged := false
	oldCfgs, newCfgs := oldCfg.Cron.Tasks, newCfg.Cron.Tasks
	for name, def := range cronTaskSource {
		oldEnabled := effectiveTaskEnable(oldCfgs, name)
		newEnabled := effectiveTaskEnable(newCfgs, name)
		oldSpec := effectiveTaskSpec(oldCfgs, name, def.TimeSpec)
		newSpec := effectiveTaskSpec(newCfgs, name, def.TimeSpec)
		if oldEnabled == newEnabled && oldSpec == newSpec {
			continue
		}
		scheduleChanged = true

		running := cronScheduler.IsRunningTask(name)
		switch {
		case !newEnabled:
			// 4.1 停用：从调度器移除（DeleteTask 幂等，未挂载时不做事）
			if running {
				cronScheduler.DeleteTask(name)
				logger.InfoWithCtx(ctx, "[config reload] 定时任务已停用",
					logger.String("task", name),
				)
			}
		case !running:
			// 4.2 启用（此前未挂载，含启动时被过滤的任务）：按生效时间表达式挂载
			runConfiguredTask(ctx, def, newSpec)
		case oldSpec != newSpec:
			// 4.3 时间表达式变更：先删除再按新表达式重挂（Run 拒绝重名任务）
			cronScheduler.DeleteTask(name)
			runConfiguredTask(ctx, def, newSpec)
			logger.InfoWithCtx(ctx, "[config reload] 定时任务时间表达式已更新",
				logger.String("task", name),
				logger.String("old", oldSpec),
				logger.String("new", newSpec),
			)
		}
	}

	// 4.1 enable/timeSpec 均未变更说明本次变更的是任务参数（如延迟窗口/回看窗口）；
	//     参数由任务执行时通过 config.Get() 读取，无需重挂调度，下次执行即生效
	if !scheduleChanged {
		logger.InfoWithCtx(ctx, "[config reload] cron.tasks 任务参数已更新，将在任务下次执行时生效")
	}

	// 5. 配置中出现未注册的任务名：告警提示拼写错误
	//    （大小写不敏感匹配，原因见 lookupCronTask：viper 会把配置 key 转小写）
	for name := range newCfgs {
		matched := false
		for regName := range cronTaskSource {
			if strings.EqualFold(name, regName) {
				matched = true
				break
			}
		}
		if !matched {
			logger.WarnWithCtx(ctx, "[config reload] cron.tasks 配置了未注册的任务名，已忽略（检查任务名拼写）",
				logger.String("task", name),
			)
		}
	}
}

// runConfiguredTask 按指定时间表达式挂载任务到调度器。
// 拷贝任务定义后再覆盖 TimeSpec，避免污染任务注册表中的原始定义
// （后续热更新需用注册表里的代码默认值参与生效表达式计算）。
func runConfiguredTask(ctx context.Context, def *gocron.Task, timeSpec string) {
	task := *def
	task.TimeSpec = timeSpec
	if err := cronScheduler.Run(&task); err != nil {
		logger.ErrorWithCtx(ctx, "[config reload] 定时任务挂载失败",
			logger.String("task", def.Name),
			logger.String("timeSpec", timeSpec),
			logger.Err(err),
		)
	}
}

// effectiveTaskEnable 计算任务的生效启用开关：未在 cron.tasks 中配置或 enable=nil 时默认启用。
func effectiveTaskEnable(taskCfgs map[string]CronTask, name string) bool {
	taskCfg, exist := lookupCronTask(taskCfgs, name)
	if !exist || taskCfg.Enable == nil {
		return true
	}
	return *taskCfg.Enable
}

// effectiveTaskSpec 计算任务的生效时间表达式：配置了 timeSpec 则覆盖，否则使用代码内注册的默认值。
func effectiveTaskSpec(taskCfgs map[string]CronTask, name, defaultSpec string) string {
	taskCfg, exist := lookupCronTask(taskCfgs, name)
	if !exist || taskCfg.TimeSpec == "" {
		return defaultSpec
	}
	return taskCfg.TimeSpec
}

// lookupCronTask 按任务名查找 cron.tasks 配置（大小写不敏感）。
// why: sunshine 配置解析链路基于 viper（pkg/conf.ParseConfigData → viper.ReadConfig/Unmarshal），
// viper 会递归把所有 map key 转小写（insensitiviseMap），yml 中的驼峰任务名
// （如 cancelOrderTimeoutTask）进入 map 后实际是全小写，精确匹配会永远落空，
// 因此先精确命中再按 EqualFold 兜底。
// 返回 false 表示该任务未配置，调用方应回落代码内默认值。
func lookupCronTask(taskCfgs map[string]CronTask, name string) (CronTask, bool) {
	// 1. 精确匹配（key 未被小写化时直接命中）
	if taskCfg, exist := taskCfgs[name]; exist {
		return taskCfg, true
	}
	// 2. 大小写不敏感匹配（viper 小写化 key 的适配）
	for cfgName, taskCfg := range taskCfgs {
		if strings.EqualFold(cfgName, name) {
			return taskCfg, true
		}
	}
	return CronTask{}, false
}

// GetCronTask 供任务在执行时按注册名读取自己的 cron.tasks 配置。
// 查找逻辑同 lookupCronTask（大小写不敏感，兼容 viper 的 key 小写化）；
// 未配置返回 false，调用方回落代码内默认值。
func GetCronTask(name string) (CronTask, bool) {
	return lookupCronTask(Get().Cron.Tasks, name)
}
