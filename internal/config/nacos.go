// Package config 提供配置初始化和全局配置访问。
package config

import (
	"fmt"
	"os"
	"runtime"

	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"

	"github.com/18721889353/sunshine/pkg/conf"
	"github.com/18721889353/sunshine/pkg/nacoscli"
)

// GetConfigFromNacos 从 Nacos 配置中心获取配置并设置到全局。
// 先读取 configFile（Nacos 连接配置 YAML），再从 Nacos 拉取业务配置，解析后设置到 config.Get()。
func GetConfigFromNacos(configFile string) error {
	nacosConf, err := NewCenter(configFile)
	if err != nil {
		return fmt.Errorf("读取 Nacos 连接配置失败: %w", err)
	}

	// 解密 Nacos 凭据（支持 ENC(hex) 格式），必须在连接 Nacos 前完成
	if err = DecryptNacosCredentials(nacosConf); err != nil {
		return fmt.Errorf("解密 Nacos 凭据失败: %w", err)
	}

	// 检查必要配置
	if nacosConf.Nacos.DataID == "" || nacosConf.Nacos.Group == "" {
		return fmt.Errorf("nacos 配置缺少 DataID 或 Group")
	}
	if nacosConf.Nacos.IPAddr == "" || nacosConf.Nacos.Port == 0 {
		return fmt.Errorf("nacos 服务器地址或端口未配置")
	}

	// 1. 构建 Params（仅保留查询所需字段）
	params := &nacoscli.Params{
		NamespaceID: nacosConf.Nacos.NamespaceID,
		Group:       nacosConf.Nacos.Group,
		DataID:      nacosConf.Nacos.DataID,
		Format:      nacosConf.Nacos.Format,
	}

	// 2. 构建 ClientConfig（包含认证信息）
	clientConfig, logDir := buildNacosClientConfig(
		nacosConf.Nacos.NamespaceID,
		nacosConf.Nacos.Username,
		nacosConf.Nacos.Password,
		5000,
	)

	// 3. 构建 ServerConfig（显式指定 HTTP 和 gRPC 端口）
	grpcPort := nacosConf.Nacos.GrpcPort
	if grpcPort == 0 {
		// 如果配置未指定 gRPC 端口，自动计算（HTTP 端口 + 1000）
		// 但注意 NodePort 环境可能不是 +1000，建议配置中明确指定
		grpcPort = nacosConf.Nacos.Port + 1000
		// 可在此处记录警告日志：logger.Warn("grpcPort not set, using default: %d", grpcPort)
	}
	serverConfig := constant.ServerConfig{
		IpAddr:      nacosConf.Nacos.IPAddr,
		Port:        uint64(nacosConf.Nacos.Port),
		GrpcPort:    uint64(grpcPort),
		Scheme:      nacosConf.Nacos.Scheme,
		ContextPath: nacosConf.Nacos.ContextPath,
	}

	// 4. 组装 opts（覆盖默认配置）
	opts := []nacoscli.Option{
		nacoscli.WithClientConfig(clientConfig),
		nacoscli.WithServerConfigs([]constant.ServerConfig{serverConfig}),
	}

	// 5. 从 Nacos 获取配置
	format, data, err := nacoscli.GetConfig(params, opts...)
	_ = os.RemoveAll(logDir) //nolint SDK 已关闭，立即清理临时日志目录（best-effort）
	if err != nil {
		return fmt.Errorf("从 Nacos 获取配置失败: %w", err)
	}

	// 6. 解析配置
	appConfig := &Config{}
	if err = conf.ParseConfigData(data, format, appConfig); err != nil {
		return fmt.Errorf("解析配置数据失败: %w", err)
	}
	if appConfig.App.Name == "" {
		return fmt.Errorf("从配置中心读取的配置数据为空（App.Name 缺失）")
	}

	// 7. 设置全局配置
	Set(appConfig)
	return nil
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
