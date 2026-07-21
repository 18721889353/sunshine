package prof

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// Profile 类型别名，用于 iota 常量定义
type ProfileType int

const (
	profileTypeCPU          ProfileType = iota // CPU profile
	profileTypeMem                             // 堆内存 profile
	profileTypeGoroutine                       // 协程 profile
	profileTypeBlock                           // 阻塞 profile
	profileTypeMutex                           // 互斥锁 profile
	profileTypeThreadCreate                    // 线程创建 profile
	profileTypeTrace                           // 运行时 trace
)

// profileTypeNames  profile 类型对应的文件名标识
var profileTypeNames = map[ProfileType]string{
	profileTypeCPU:          "cpu",
	profileTypeMem:          "mem",
	profileTypeGoroutine:    "goroutine",
	profileTypeBlock:        "block",
	profileTypeMutex:        "mutex",
	profileTypeThreadCreate: "threadcreate",
	profileTypeTrace:        "trace",
}

const (
	// DefaultDuration 默认采样时长（秒）
	DefaultDuration = 60
	// DefaultOutputDir 默认输出目录名格式（追加到系统临时目录后）
	DefaultOutputDirSuffix = "_profile"
	// TimeFormat 采样文件时间戳格式
	TimeFormat = "20060102T150405"
)

// 包级默认配置
var (
	defaultDurationSec uint32 = DefaultDuration
	defaultOutputDir          = filepath.Join(os.TempDir(), getServerName()+DefaultOutputDirSuffix)
	timeFormat                = TimeFormat
	pid                       = syscall.Getpid()
)

// Profile 表示一次 profiling 采样会话。
// 每次 StartOrStop() 调用会开启或停止采样，采样文件保存至输出目录。
type Profile struct {
	files    []string      // 本次采样产生的文件路径列表
	closeFns []func()      // 停止采样时需执行的关闭函数列表
	stopCh   chan struct{} // 用于超时控制的停止信号通道

	// 可通过 Options 配置的字段
	durationSec uint32      // 采样持续时间（秒）
	traceOn     bool        // 是否采集 trace
	outputDir   string      // 采样文件输出目录
	errHandler  func(error) // 错误处理回调，默认使用 fmt.Println
}

// ProfileOption 定义 Profile 的配置选项函数
type ProfileOption func(p *profileOptions)

type profileOptions struct {
	durationSec uint32
	traceOn     bool
	outputDir   string
	errHandler  func(error)
}

// apply 按顺序应用所有配置选项
func (o *profileOptions) apply(opts ...ProfileOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// WithProfileDuration 设置采样持续时间（秒），默认 60 秒。
// 如果设置为 0，使用默认值。
func WithProfileDuration(sec uint32) ProfileOption {
	return func(o *profileOptions) {
		if sec > 0 {
			o.durationSec = sec
		}
	}
}

// WithProfileTrace 启用或禁用 trace 采样，默认禁用。
func WithProfileTrace(enabled bool) ProfileOption {
	return func(o *profileOptions) {
		o.traceOn = enabled
	}
}

// WithProfileOutputDir 设置采样文件输出目录，默认在系统临时目录下。
func WithProfileOutputDir(dir string) ProfileOption {
	return func(o *profileOptions) {
		if dir != "" {
			o.outputDir = dir
		}
	}
}

// WithProfileErrorHandler 设置采样过程中错误处理回调函数。
// 默认为 fmt.Println 输出到标准输出。
func WithProfileErrorHandler(fn func(error)) ProfileOption {
	return func(o *profileOptions) {
		if fn != nil {
			o.errHandler = fn
		}
	}
}

// NewProfile 创建一个新的 Profile 采样器。
// 支持通过 ProfileOption 配置采样时长、trace 开关、输出目录等。
//
// 示例:
//
//	p := NewProfile(
//	    WithProfileDuration(30),
//	    WithProfileTrace(true),
//	    WithProfileOutputDir("/tmp/myapp_profile"),
//	    WithProfileErrorHandler(func(err error) {
//	        log.Printf("profile error: %v", err)
//	    }),
//	)
func NewProfile(opts ...ProfileOption) *Profile {
	o := &profileOptions{
		durationSec: defaultDurationSec,
		traceOn:     defaultTraceOn.Load(),
		outputDir:   defaultOutputDir,
		errHandler:  func(err error) { fmt.Println(err) },
	}
	o.apply(opts...)

	return &Profile{
		stopCh:      make(chan struct{}, 1),
		durationSec: o.durationSec,
		traceOn:     o.traceOn,
		outputDir:   o.outputDir,
		errHandler:  o.errHandler,
	}
}

// StartOrStop 开关式启动/停止采样。
//   - 第一次调用：启动采样（如果当前状态为停止）
//   - 第二次调用：停止采样（如果当前状态为启动）
//
// 启动后，若在 durationSec 内未收到停止信号，自动停止采样。
func (p *Profile) StartOrStop() {
	if p == nil {
		return
	}
	if atomic.CompareAndSwapUint32(&status, statusStop, statusStart) {
		p.startProfile()
	} else if atomic.CompareAndSwapUint32(&status, statusStart, statusStop) {
		p.stopProfile()
	}
}

// Files 返回本次采样产生的所有文件路径列表。
func (p *Profile) Files() []string {
	if p == nil {
		return nil
	}
	result := make([]string, len(p.files))
	copy(result, p.files)
	return result
}

// Cleanup 删除本次采样产生的所有 profile 文件。
// 通常在分析完成文件后调用。
func (p *Profile) Cleanup() {
	if p == nil {
		return
	}
	for _, f := range p.files {
		if err := os.Remove(f); err != nil {
			p.errHandler(fmt.Errorf("remove profile file %s: %w", f, err))
		}
	}
	p.files = p.files[:0]
}

func (p *Profile) handleError(err error) {
	if err != nil && p != nil && p.errHandler != nil {
		p.errHandler(err)
	}
}

func (p *Profile) startProfile() {
	fmt.Printf("[profile] 开始采样 profile，状态=%d\n", atomic.LoadUint32(&status))

	p.files = p.files[:0]
	p.closeFns = p.closeFns[:0]

	// 按顺序启动各类型采样
	samplers := []struct {
		name string
		fn   func() error
	}{
		{"cpu", p.sampleCPU},
		{"mem", p.sampleMem},
		{"goroutine", p.sampleGoroutine},
		{"block", p.sampleBlock},
		{"mutex", p.sampleMutex},
		{"threadcreate", p.sampleThreadCreate},
	}

	for _, s := range samplers {
		if err := s.fn(); err != nil {
			p.handleError(fmt.Errorf("启动 %s 采样失败: %w", s.name, err))
		}
	}

	if p.traceOn {
		if err := p.sampleTrace(); err != nil {
			p.handleError(fmt.Errorf("启动 trace 采样失败: %w", err))
		}
	}

	go p.checkTimeout()
}

func (p *Profile) stopProfile() {
	fmt.Printf("[profile] 停止采样 profile，状态=%d\n", atomic.LoadUint32(&status))

	if len(p.closeFns) == 0 {
		return
	}

	for _, fn := range p.closeFns {
		fn()
	}

	// 通知超时控制协程停止
	select {
	case p.stopCh <- struct{}{}:
	default:
	}

	// 重置内部状态，使下次 StartOrStop 可重新开始
	p.closeFns = p.closeFns[:0]
}

func (p *Profile) checkTimeout() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(p.durationSec)*time.Second)
	defer cancel()

	select {
	case <-p.stopCh:
		fmt.Println("[profile] 停止采样：手动触发")
	case <-ctx.Done():
		if atomic.LoadUint32(&status) == statusStart {
			atomic.StoreUint32(&status, statusStop)
			p.stopProfile()
		}
		fmt.Println("[profile] 停止采样：采样时间到")
	}
}

// 内部状态管理（使用 uint32 配合原子操作）
var (
	status      uint32
	statusStart uint32 = 1
	statusStop  uint32
)

// sampleCPU 启动 CPU profile 采样
func (p *Profile) sampleCPU() error {
	file := p.getFilePath(profileTypeCPU)
	f, err := os.Create(file)
	if err != nil {
		return fmt.Errorf("创建 CPU profile 文件失败: %w", err)
	}

	if err := pprof.StartCPUProfile(f); err != nil {
		_ = f.Close()
		return fmt.Errorf("启动 CPU profile 失败: %w", err)
	}

	p.files = append(p.files, file)
	p.closeFns = append(p.closeFns, func() {
		pprof.StopCPUProfile()
		_ = f.Close()
	})

	return nil
}

// sampleMem 启动堆内存 profile 采样（关闭时写入）
func (p *Profile) sampleMem() error {
	file := p.getFilePath(profileTypeMem)
	f, err := os.Create(file)
	if err != nil {
		return fmt.Errorf("创建 mem profile 文件失败: %w", err)
	}

	old := runtime.MemProfileRate
	runtime.MemProfileRate = 4096

	p.files = append(p.files, file)
	p.closeFns = append(p.closeFns, func() {
		if err := pprof.Lookup("heap").WriteTo(f, 0); err != nil {
			p.handleError(fmt.Errorf("写入 mem profile 失败: %w", err))
		}
		_ = f.Close()
		runtime.MemProfileRate = old
	})

	return nil
}

// sampleGoroutine 启动 goroutine profile 采样（关闭时写入）
func (p *Profile) sampleGoroutine() error {
	file := p.getFilePath(profileTypeGoroutine)
	f, err := os.Create(file)
	if err != nil {
		return fmt.Errorf("创建 goroutine profile 文件失败: %w", err)
	}

	p.files = append(p.files, file)
	p.closeFns = append(p.closeFns, func() {
		if err := pprof.Lookup("goroutine").WriteTo(f, 0); err != nil {
			p.handleError(fmt.Errorf("写入 goroutine profile 失败: %w", err))
		}
		_ = f.Close()
	})

	return nil
}

// sampleBlock 启动阻塞 profile 采样（关闭时写入）
func (p *Profile) sampleBlock() error {
	file := p.getFilePath(profileTypeBlock)
	f, err := os.Create(file)
	if err != nil {
		return fmt.Errorf("创建 block profile 文件失败: %w", err)
	}

	runtime.SetBlockProfileRate(1)

	p.files = append(p.files, file)
	p.closeFns = append(p.closeFns, func() {
		if err := pprof.Lookup("block").WriteTo(f, 0); err != nil {
			p.handleError(fmt.Errorf("写入 block profile 失败: %w", err))
		}
		_ = f.Close()
		runtime.SetBlockProfileRate(0)
	})

	return nil
}

// sampleMutex 启动互斥锁 profile 采样（关闭时写入）
func (p *Profile) sampleMutex() error {
	file := p.getFilePath(profileTypeMutex)
	f, err := os.Create(file)
	if err != nil {
		return fmt.Errorf("创建 mutex profile 文件失败: %w", err)
	}

	runtime.SetMutexProfileFraction(1)

	p.files = append(p.files, file)
	p.closeFns = append(p.closeFns, func() {
		if mp := pprof.Lookup("mutex"); mp != nil {
			if err := mp.WriteTo(f, 0); err != nil {
				p.handleError(fmt.Errorf("写入 mutex profile 失败: %w", err))
			}
		}
		_ = f.Close()
		runtime.SetMutexProfileFraction(0)
	})

	return nil
}

// sampleThreadCreate 启动线程创建 profile 采样（关闭时写入）
func (p *Profile) sampleThreadCreate() error {
	file := p.getFilePath(profileTypeThreadCreate)
	f, err := os.Create(file)
	if err != nil {
		return fmt.Errorf("创建 threadcreate profile 文件失败: %w", err)
	}

	p.files = append(p.files, file)
	p.closeFns = append(p.closeFns, func() {
		if mp := pprof.Lookup("threadcreate"); mp != nil {
			if err := mp.WriteTo(f, 0); err != nil {
				p.handleError(fmt.Errorf("写入 threadcreate profile 失败: %w", err))
			}
		}
		_ = f.Close()
	})

	return nil
}

// sampleTrace 启动运行时 trace 采样
func (p *Profile) sampleTrace() error {
	file := p.getFilePath(profileTypeTrace)
	f, err := os.Create(file)
	if err != nil {
		return fmt.Errorf("创建 trace 文件失败: %w", err)
	}

	if err := trace.Start(f); err != nil {
		_ = f.Close()
		return fmt.Errorf("启动 trace 失败: %w", err)
	}

	p.files = append(p.files, file)
	p.closeFns = append(p.closeFns, func() {
		trace.Stop()
		_ = f.Close()
	})

	return nil
}

// getFilePath 根据 profile 类型生成完整的输出文件路径。
// 文件名格式: {时间戳}_{pid}_{服务名}_{profile类型}.out
func (p *Profile) getFilePath(t ProfileType) string {
	name, ok := profileTypeNames[t]
	if !ok {
		name = "unknown"
	}

	filename := fmt.Sprintf("%s_%d_%s_%s.out",
		time.Now().Format(timeFormat), pid, getServerName(), name)

	return filepath.Join(p.outputDir, filename)
}

// SetDurationSecond 设置包级默认采样时长（秒）。
// 此函数影响全局默认值，仅对后续 NewProfile() 调用有效。
// 推荐使用 WithProfileDuration() 替代。
func SetDurationSecond(d uint32) {
	if d > 0 {
		atomic.StoreUint32(&defaultDurationSec, d)
	}
}

// EnableTrace 启用包级默认 trace 采样。
// 此函数影响全局默认值，仅对后续 NewProfile() 调用有效。
// 推荐使用 WithProfileTrace(true) 替代。
func EnableTrace() {
	defaultTraceOn.Store(true)
}

// defaultTraceOn 包级默认 trace 开关
var defaultTraceOn atomic.Bool

// getServerName 从可执行文件路径中提取服务名称（不含扩展名）
func getServerName() string {
	_, name := filepath.Split(os.Args[0])
	return strings.TrimSuffix(name, filepath.Ext(name))
}

// init 初始化包级默认 trace 开关
func init() {
	defaultTraceOn.Store(false)
}
