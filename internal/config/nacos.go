// Package config 提供配置初始化和全局配置访问。
package config

import (
	"fmt"

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

	// 将 Nacos 连接配置转换为查询参数
	params := &nacoscli.Params{
		IPAddr:      nacosConf.Nacos.IPAddr,
		Port:        nacosConf.Nacos.Port,
		NamespaceID: nacosConf.Nacos.NamespaceID,
		Scheme:      nacosConf.Nacos.Scheme,
		ContextPath: nacosConf.Nacos.ContextPath,
		Group:       nacosConf.Nacos.Group,
		DataID:      nacosConf.Nacos.DataID,
		Format:      nacosConf.Nacos.Format,
	}

	var opts []nacoscli.Option
	if nacosConf.Nacos.Username != "" || nacosConf.Nacos.Password != "" {
		opts = append(opts, nacoscli.WithAuth(nacosConf.Nacos.Username, nacosConf.Nacos.Password))
	}

	format, data, err := nacoscli.GetConfig(params, opts...)
	if err != nil {
		return fmt.Errorf("从配置中心获取配置失败: %w", err)
	}

	appConfig := &Config{}
	if err = conf.ParseConfigData(data, format, appConfig); err != nil {
		return fmt.Errorf("解析配置数据失败: %w", err)
	}

	if appConfig.App.Name == "" {
		return fmt.Errorf("从配置中心读取的配置数据为空")
	}

	Set(appConfig)

	return nil
}
