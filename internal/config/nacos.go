// Package config 统一管理应用配置的加载、全局访问与运行时热更新。
//
// 核心职责：
//   - Nacos 配置集成：GetConfigFromNacos 从 Nacos 配置中心拉取配置，支持 ENC(hex) 格式凭据的 SM4 自动解密，
//     启用 watch 时通过后台 goroutine 监听变更并触发回调链。
//   - 全局配置状态：Init/Get/Set 管理全局 *Config 实例，供全应用读取。
//   - 热更新回调链：RegisterBuiltinReloads 注册按功能域分组的回调（Sentinel 限流熔断、JWT/签名认证、
//     应用开关、数据库连接池、GORM 日志、链路追踪、定时任务、HTTP 超时），配置变更时按序执行，
//     单个回调 panic 不影响后续执行。
//   - 服务注册辅助：register_helper.go 提供 Etcd/Nacos 客户端与注册中心的 Option 构建方法。
package config

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sync"

	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"

	"github.com/18721889353/sunshine/pkg/conf"
	"github.com/18721889353/sunshine/pkg/logger"

	"github.com/18721889353/sunshine/pkg/nacoscli"
)

// nacosSharedLogDir 进程级单例日志目录，避免 BuildNamingClientOptions 多次调用导致目录泄漏。
var (
	nacosSharedLogDirOnce sync.Once
	nacosSharedLogDir     string
)

func getNacosSharedLogDir() string {
	nacosSharedLogDirOnce.Do(func() {
		var cacheDir string
		switch runtime.GOOS {
		case "windows":
			cacheDir = "NUL"
		default:
			cacheDir = "/dev/null"
		}
		dir, err := os.MkdirTemp("", "nacos-sdk-log")
		if err != nil {
			nacosSharedLogDir = cacheDir
			return
		}
		nacosSharedLogDir = dir
	})
	return nacosSharedLogDir
}

// buildNacosOpts 构建共享的 Nacos 连接选项。
// 返回值 logDir 使用进程级单例目录，由进程退出时 OS 回收，无需调用方清理。
func buildNacosOpts(nacosConf *Center) []nacoscli.Option {
	clientConfig := buildNacosClientConfig(
		nacosConf.Nacos.NamespaceID,
		nacosConf.Nacos.Username,
		nacosConf.Nacos.Password,
		5000,
	)

	grpcPort := nacosConf.Nacos.GrpcPort
	if grpcPort == 0 {
		// 如果配置未指定 gRPC 端口，自动计算（HTTP 端口 + 1000）
		grpcPort = nacosConf.Nacos.Port + 1000
	}

	opts := []nacoscli.Option{
		nacoscli.WithClientConfig(clientConfig),
		nacoscli.WithServerConfigs([]constant.ServerConfig{{
			IpAddr:      nacosConf.Nacos.IPAddr,
			Port:        uint64(nacosConf.Nacos.Port),
			GrpcPort:    uint64(grpcPort),
			Scheme:      nacosConf.Nacos.Scheme,
			ContextPath: nacosConf.Nacos.ContextPath,
		}}),
	}
	return opts
}

// GetConfigFromNacos 从 Nacos 配置中心获取配置并设置到全局。
// 先读取 configFile（Nacos 连接配置 YAML），再从 Nacos 拉取业务配置，解析后设置到 config.Get()。
// 如果配置中 enableWatch=true，还会自动启动配置变更监听（后台 goroutine）。
//
// 返回值:
//   - stop: 优雅关闭函数，进程退出时调用以停止 Nacos watch goroutine。enableWatch=false 时返回 nil。
//   - err: 任何初始化阶段的错误。
func GetConfigFromNacos(configFile string) (stop func(), err error) {
	nacosConf, err := NewCenter(configFile)
	if err != nil {
		return nil, fmt.Errorf("读取 Nacos 连接配置失败: %w", err)
	}

	// 解密 Nacos 凭据（支持 ENC(hex) 格式），必须在连接 Nacos 前完成
	if err = DecryptNacosCredentials(nacosConf); err != nil {
		return nil, fmt.Errorf("解密 Nacos 凭据失败: %w", err)
	}

	// 检查必要配置
	if nacosConf.Nacos.DataID == "" || nacosConf.Nacos.Group == "" {
		return nil, fmt.Errorf("nacos 配置缺少 DataID 或 Group")
	}
	if nacosConf.Nacos.IPAddr == "" || nacosConf.Nacos.Port == 0 {
		return nil, fmt.Errorf("nacos 服务器地址或端口未配置")
	}

	// 1. 构建 Params（仅保留查询所需字段）
	params := &nacoscli.Params{
		NamespaceID: nacosConf.Nacos.NamespaceID,
		Group:       nacosConf.Nacos.Group,
		DataID:      nacosConf.Nacos.DataID,
		Format:      nacosConf.Nacos.Format,
	}

	// 2. 构建连接选项（使用进程级单例日志目录，无需清理）
	opts := buildNacosOpts(nacosConf)

	// 3. 从 Nacos 获取配置
	format, data, err := nacoscli.GetConfig(params, opts...)
	if err != nil {
		return nil, fmt.Errorf("从 Nacos 获取配置失败: %w", err)
	}

	// 4. 解析配置
	appConfig := &Config{}
	if err = conf.ParseConfigData(data, format, appConfig); err != nil {
		return nil, fmt.Errorf("解析配置数据失败: %w", err)
	}
	if appConfig.App.Name == "" {
		return nil, fmt.Errorf("从配置中心读取的配置数据为空（App.Name 缺失）")
	}

	// 5. 设置全局配置
	Set(appConfig)

	// 6. 如果启用配置中心监听，启动后台监听并返回关闭函数
	if nacosConf.Nacos.EnableWatch {
		// 用闭包捕获 format，确保热更新时使用与首次拉取一致的格式
		handler := func(_, group, dataID, data string) {
			onNacosConfigChange(format, group, dataID, data)
		}
		stop, watchErr := nacoscli.WatchConfig(context.Background(), params, handler, opts...)
		if watchErr != nil {
			return nil, fmt.Errorf("启动 Nacos 配置监听失败: %w", watchErr)
		}
		return stop, nil
	}

	return nil, nil
}

// onNacosConfigChange Nacos 配置变更回调。
// 解析新配置，校验关键字段，触发 reload 回调链。
func onNacosConfigChange(format, group, dataID, data string) {
	ctx := context.Background()
	logger.InfoWithCtx(ctx, "[nacos watch] 收到配置变更",
		logger.String("group", group),
		logger.String("dataId", dataID),
	)

	// 1. 解析新配置
	newCfg := &Config{}
	if err := conf.ParseConfigData([]byte(data), format, newCfg); err != nil {
		logger.WarnWithCtx(ctx, "[nacos watch] 解析新配置失败",
			logger.Err(err),
		)
		return
	}

	// 2. 校验关键字段
	if newCfg.App.Name == "" {
		logger.WarnWithCtx(ctx, "[nacos watch] 配置无效：App.Name 为空")
		return
	}

	// 3. 触发 reload 回调链
	Reload(newCfg)

	logger.InfoWithCtx(ctx, "[nacos watch] 配置已重载",
		logger.String("version", newCfg.App.Version),
	)
}

// buildNacosClientConfig 构建 Nacos SDK ClientConfig。
// 使用进程级单例日志目录（getNacosSharedLogDir），避免多次调用导致目录泄漏。
// CacheDir 指向空设备，禁止快照缓存落盘。
func buildNacosClientConfig(namespaceID, username, password string, timeoutMs int) *constant.ClientConfig {
	var cacheDir string
	switch runtime.GOOS {
	case "windows":
		cacheDir = "NUL"
	default:
		cacheDir = "/dev/null"
	}

	return &constant.ClientConfig{
		DisableUseSnapShot:  true, // 禁止读取本地缓存
		NamespaceId:         namespaceID,
		TimeoutMs:           uint64(timeoutMs),
		NotLoadCacheAtStart: true,                   // 启动时不加载本地缓存
		CacheDir:            cacheDir,               // 缓存目录指向空设备（快照缓存无敏感数据）
		LogDir:              getNacosSharedLogDir(), // 日志目录使用进程级单例
		Username:            username,
		Password:            password,
	}
}
