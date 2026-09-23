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

	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"

	"github.com/18721889353/sunshine/pkg/conf"
	"github.com/18721889353/sunshine/pkg/logger"

	"github.com/18721889353/sunshine/pkg/nacoscli"
)

// buildNacosOpts 封装 Nacos 连接参数，构建 nacoscli.Option 列表。
// 消除 GetConfigFromNacos 和 WatchConfig 之间的重复构建逻辑。
// 返回值 logDir 由调用方负责清理。
func buildNacosOpts(nacosConf *Center) ([]nacoscli.Option, string) {
	clientConfig, logDir := buildNacosClientConfig(
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
	return opts, logDir
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

	// 2. 构建连接选项（复用公共函数消除重复逻辑）
	opts, logDir := buildNacosOpts(nacosConf)
	defer func() {
		if removeErr := os.RemoveAll(logDir); removeErr != nil {
			logger.WarnWithCtx(context.Background(), "[nacos] 删除日志目录失败",
				logger.Err(removeErr))
		}
	}()

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
		stop, watchErr := nacoscli.WatchConfig(context.Background(), params, onNacosConfigChange, opts...)
		if watchErr != nil {
			return nil, fmt.Errorf("启动 Nacos 配置监听失败: %w", watchErr)
		}
		return stop, nil
	}

	return nil, nil
}

// onNacosConfigChange Nacos 配置变更回调。
// 解析新的 YAML 配置，校验关键字段，触发 reload 回调链。
func onNacosConfigChange(_ string, group, dataID, data string) {
	ctx := context.Background()
	logger.InfoWithCtx(ctx, "[nacos watch] 收到配置变更",
		logger.String("group", group),
		logger.String("dataId", dataID),
	)

	// 1. 解析新配置
	newCfg := &Config{}
	if err := conf.ParseConfigData([]byte(data), "yaml", newCfg); err != nil {
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
// 内部创建临时目录供 SDK 写日志（SDK 内部固定拼接 nacos-sdk.log，LogDir 必须是真实目录），
// 调用方通过返回的 logDir 负责清理。CacheDir 指向空设备，禁止快照缓存落盘。
func buildNacosClientConfig(namespaceID, username, password string, timeoutMs int) (*constant.ClientConfig, string) {
	var cacheDir string
	switch runtime.GOOS {
	case "windows":
		cacheDir = "NUL" // Windows 空设备
	default:
		cacheDir = "/dev/null" // Linux/macOS 空设备
	}

	logDir, err := os.MkdirTemp("", "nacos-sdk-log")
	if err != nil {
		logDir = cacheDir // 兜底：MkdirTemp 失败时使用空设备
	}

	return &constant.ClientConfig{
		DisableUseSnapShot:  true, // 禁止读取本地缓存
		NamespaceId:         namespaceID,
		TimeoutMs:           uint64(timeoutMs),
		NotLoadCacheAtStart: true,     // 启动时不加载本地缓存
		CacheDir:            cacheDir, // 缓存目录指向空设备（快照缓存无敏感数据）
		LogDir:              logDir,   // 日志目录指向临时目录
		Username:            username,
		Password:            password,
	}, logDir
}
