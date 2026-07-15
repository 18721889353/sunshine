package generate

import (
	"errors"
	"fmt"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/18721889353/sunshine/pkg/replacer"
)

// CacheCommand 创建缓存代码生成命令
// 该命令用于根据用户指定的参数生成 Redis 缓存相关的 Go 代码
// 参数说明：
//   - parentName: 父命令名称（如 "web"、"micro" 等），用于构建完整的命令路径
//
// 返回值：配置好的 cobra.Command 指针
func CacheCommand(parentName string) *cobra.Command {
	// 定义命令行标志变量
	var (
		moduleName string // 模块名称，对应 go.mod 文件中的 module 声明
		outPath    string // 代码输出目录路径
		cacheName  string // 缓存业务名称，用于生成便捷方法名（如 UserToken）
		prefixKey  string // Redis 键前缀，用于区分不同业务的缓存（如 user:token:）
		keyName    string // 缓存键的参数名（如 uid、id）
		keyType    string // 缓存键的 Go 类型（如 string、uint64）
		valueName  string // 缓存值的变量名（如 token、data）
		valueType  string // 缓存值的 Go 类型（如 string、*User）

		serverName     string // 服务器名称，用于单体仓库模式
		suitedMonoRepo bool   // 是否适配单体仓库结构，true 表示生成的代码适合 mono-repo
	)

	// 创建 cobra 命令对象
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "生成缓存代码",
		Long:  "根据指定的模块名、缓存名、键值类型等参数，生成基于 Redis 的缓存管理代码。",
		Example: color.HiBlackString(fmt.Sprintf(`  # =====================================================================
  # 基本用法：生成 string 类型的键值缓存
  # 执行后会生成: internal/cache/{moduleName}Cache.go（含 Get/Set/Del 三个方法）
  # =====================================================================
  sunshine %[1]s cache \
    --module-name=myModule \
    --cache-name=userToken \
    --prefix-key=user:token: \
    --key-name=id --key-type=uint64 \
    --value-name=token --value-type=string \
    --suited-mono-repo=false --server-name=user-service \
    --out=/d/Temp/web


  # =====================================================================
  # 参数说明：
  #   --module-name   Go 模块名（必填，除非 out 目录已有 gen.info）
  #   --cache-name    缓存业务名，生成 GetCacheName/SetCacheName/DelCacheName 方法（必填）
  #   --prefix-key    Redis key 前缀，默认 cache-name + ":"
  #   --key-name      方法参数名（必填）
  #   --key-type      键 Go 类型，如 uint64 / string（必填）
  #   --value-name    值变量名（必填）
  #   --value-type    值 Go 类型，如 string / *User（必填）
  #   --out           输出目录（可选，默认 ./cache_<时间戳>）
  #   --server-name   服务名（mono-repo 模式必填）
  #   --suited-mono-repo 是否启用单体仓库模式（可选，默认 false）
  #
  # 提示：
  #   - value-type 为 *User 等指针类型时，自动使用 "variable := &Type{}" 声明
  #   - 如果 --out 目录已有 docs/gen.info 文件，会自动读取 moduleName 等配置
`, parentName, parentName)),
		// 命令执行逻辑
		RunE: func(_ *cobra.Command, _ []string) error {
			// 从输出目录中读取之前保存的模块名和服务器名（如果存在）
			mdName, srvName, smr := getNamesFromOutDir(outPath)
			if mdName != "" {
				// 如果目录中存在 gen.info 文件，使用其中保存的配置
				moduleName = mdName
				serverName = srvName
				suitedMonoRepo = smr
			} else if moduleName == "" {
				// 否则必须通过命令行参数指定模块名
				return errors.New(`required flag(s) "module-name" not set, use "sunshine micro cache -h" for help`)
			}

			// 如果是单体仓库模式，需要验证服务器名称并调整输出路径
			if suitedMonoRepo {
				if serverName == "" {
					return fmt.Errorf(`required flag(s) "server-name" not set, use "sunshine %s cache -h" for help`, parentName)
				}
				// 转换服务器名称格式（将连字符替换为下划线）
				serverName = convertServerName(serverName)
				// 调整输出路径以适配单体仓库结构
				outPath = changeOutPath(outPath, serverName)
			}

			// 移除缓存名称中的冒号（避免文件名问题）
			cacheName = strings.ReplaceAll(cacheName, ":", "")

			// 处理缓存前缀键：如果为空或只有冒号，则使用缓存名称作为前缀
			if prefixKey == "" || prefixKey == ":" {
				prefixKey = cacheName + ":"
			} else if prefixKey[len(prefixKey)-1] != ':' {
				// 确保前缀以冒号结尾
				prefixKey += ":"
			}

			// 创建缓存代码生成器实例
			var err error
			var g = &stringCacheGenerator{
				moduleName: moduleName,
				cacheName:  cacheName,
				prefixKey:  prefixKey,
				keyName:    keyName,
				keyType:    keyType,
				valueName:  valueName,
				valueType:  valueType,
				outPath:    outPath,

				serverName:     serverName,
				suitedMonoRepo: suitedMonoRepo,
			}

			// 执行代码生成
			outPath, err = g.generateCode()
			if err != nil {
				return err
			}

			// 输出使用说明
			fmt.Printf(`
using help:
  move the folder "internal" to your project code folder.

`)
			fmt.Printf("generate \"cache\" code successfully, out = %s\n", outPath)
			return nil
		},
	}

	// 定义命令行标志及其说明
	cmd.Flags().StringVarP(&moduleName, "module-name", "m", "", "模块名称，对应 go.mod 文件中的 module 声明")
	cmd.Flags().StringVarP(&cacheName, "cache-name", "c", "", "缓存业务名称，例如 userToken")
	if err := cmd.MarkFlagRequired("cache-name"); err != nil {
		fmt.Printf("mark flag required error: %v\n", err)
	}
	cmd.Flags().StringVarP(&prefixKey, "prefix-key", "p", "", "Redis 缓存键前缀，例如 user:token")
	cmd.Flags().StringVarP(&keyName, "key-name", "k", "", "缓存键的参数名，例如 id、uid")
	if err := cmd.MarkFlagRequired("key-name"); err != nil {
		fmt.Printf("mark flag required error: %v\n", err)
	}
	cmd.Flags().StringVarP(&keyType, "key-type", "t", "", "缓存键的 Go 类型，例如 uint64、string")
	if err := cmd.MarkFlagRequired("key-type"); err != nil {
		fmt.Printf("mark flag required error: %v\n", err)
	}
	cmd.Flags().StringVarP(&valueName, "value-name", "v", "", "缓存值的变量名，例如 token、data")
	if err := cmd.MarkFlagRequired("value-name"); err != nil {
		fmt.Printf("mark flag required error: %v\n", err)
	}
	cmd.Flags().StringVarP(&valueType, "value-type", "w", "", "缓存值的 Go 类型，例如 string、*User")
	if err := cmd.MarkFlagRequired("value-type"); err != nil {
		fmt.Printf("mark flag required error: %v\n", err)
	}
	cmd.Flags().StringVarP(&serverName, "server-name", "s", "", "服务器名称，用于单体仓库模式")
	cmd.Flags().BoolVarP(&suitedMonoRepo, "suited-mono-repo", "l", false, "是否适配单体仓库结构")
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "代码输出目录，默认为 ./cache_<时间戳>，可通过 module-name 自动推断")

	return cmd
}

// stringCacheGenerator 字符串缓存代码生成器
// 负责根据用户提供的参数生成完整的缓存管理代码
type stringCacheGenerator struct {
	moduleName string // 模块名称，用于导入路径和命名
	cacheName  string // 缓存业务名称，用于生成方法名
	prefixKey  string // Redis 键前缀
	keyName    string // 缓存键参数名
	keyType    string // 缓存键类型
	valueName  string // 缓存值变量名
	valueType  string // 缓存值类型
	outPath    string // 输出目录路径

	serverName     string // 服务器名称（单体仓库模式使用）
	suitedMonoRepo bool   // 是否适配单体仓库结构
}

// generateCode 执行缓存代码生成流程
// 该方法负责：
// 1. 获取模板替换器实例
// 2. 指定要处理的模板文件
// 3. 设置输出目录
// 4. 构建字段替换规则
// 5. 执行文件保存
// 返回值：生成的代码目录路径和可能的错误
func (g *stringCacheGenerator) generateCode() (string, error) {
	subTplName := codeNameCache
	// 获取 sunshine 模板的替换器实例
	r := Replacers[TplNameSunshine]
	if r == nil {
		return "", errors.New("replacer is nil")
	}

	// 指定要处理的子目录和文件列表
	// 这里只处理 cacheNameExample.go 模板文件
	subDirs := []string{}
	subFiles := []string{"internal/cache/cacheNameExample.go"}

	// 配置替换器的子目录和文件
	r.SetSubDirsAndFiles(subDirs, subFiles...)
	// 设置输出目录
	if err := r.SetOutputDir(g.outPath, subTplName); err != nil {
		return "", err
	}
	// 构建字段替换规则
	fields := g.addFields(r)
	// 应用替换规则
	r.SetReplacementFields(fields)
	// 执行文件保存操作
	if err := r.SaveFiles(); err != nil {
		return "", err
	}

	// 返回生成的代码所在目录
	return r.GetOutputDir(), nil
}

// addFields 构建缓存代码生成的字段替换规则
// 该函数负责将模板中的占位符替换为实际的模块名、缓存名、键值类型等信息
func (g *stringCacheGenerator) addFields(r replacer.Replacer) []replacer.Field {
	var fields []replacer.Field

	// 删除模板文件中标记的代码块（用于让 sunshine 项目本身能够编译通过的占位代码）
	fields = append(fields, deleteFieldsMark(r, cacheFile, startMark, endMark)...)

	// 处理值类型为指针的特殊情况（如 *User、*Token 等）
	// 当 valueType 以 '*' 开头时，需要特殊处理变量声明和取地址操作
	if g.valueType[0] == '*' {
		fields = append(fields, []replacer.Field{
			{
				// 将 "var valueNameExample valueTypeExample" 替换为 "token := &SomeType{}"
				Old:             "var valueNameExample valueTypeExample",
				New:             fmt.Sprintf("%s := &%s{}", g.valueName, g.valueType[1:]),
				IsCaseSensitive: false,
			},
			{
				// 将 "&valueNameExample" 替换为实际的变量名（因为已经是指针，不需要再取地址）
				Old:             "&valueNameExample",
				New:             g.valueName,
				IsCaseSensitive: false,
			},
		}...)
	}

	// 添加核心的字段替换规则
	fields = append(fields, []replacer.Field{
		{
			// 替换内部包的导入路径，将 sunshine 框架的路径替换为用户项目的模块路径
			// 例如：github.com/18721889353/sunshine/internal/database -> fuliApiGo/internal/database
			Old: "github.com/18721889353/sunshine/internal/database",
			New: g.moduleName + "/internal/database",
		},
		{
			// 替换生成的文件名，基于模块名生成 {moduleName}Cache.go
			// 例如：cacheNameExample.go -> adminServiceCache.go
			Old: "cacheNameExample.go",
			New: g.moduleName + "Cache.go",
		},
		{
			// 替换接口名称（大写开头，公开可导出）
			// 例如：NameExample -> AdminServiceCache
			// 使用大小写敏感匹配，避免与结构体名称冲突
			Old:             "NameExample",
			New:             strings.ToUpper(g.moduleName[:1]) + g.moduleName[1:] + "Cache",
			IsCaseSensitive: true,
		},
		{
			// 替换结构体名称（小写开头，包内私有）
			// 例如：nameExample -> adminServiceCache
			// 使用大小写敏感匹配，避免与接口名称冲突
			Old:             "nameExample",
			New:             strings.ToLower(g.moduleName[:1]) + g.moduleName[1:] + "Cache",
			IsCaseSensitive: true,
		},
		{
			// 替换便捷方法名称（基于 cache-name 参数）
			// 例如：CacheName -> GetUserToken（当 --cache-name=UserToken 时）
			// 用于生成 GetCacheName、SetCacheName、DelCacheName 等方法
			Old:             "CacheName",
			New:             strings.ToUpper(g.cacheName[:1]) + g.cacheName[1:],
			IsCaseSensitive: true,
		},
		{
			// 替换键名参数名
			// 例如：keyNameExample -> uid（当 --key-name=uid 时）
			Old:             "keyNameExample",
			New:             g.keyName,
			IsCaseSensitive: false,
		},
		{
			// 替换缓存前缀键
			// 例如：prefixKeyExample: -> user:token:（当 --prefix-key=user:token: 时）
			Old:             "prefixKeyExample:",
			New:             g.prefixKey,
			IsCaseSensitive: false,
		},
		{
			// 替换键的类型
			// 例如：keyTypeExample -> string（当 --key-type=string 时）
			// 用于接口和方法的参数类型定义
			Old:             "keyTypeExample",
			New:             g.keyType,
			IsCaseSensitive: false,
		},
		{
			// 替换值的类型
			// 例如：valueTypeExample -> string（当 --value-type=string 时）
			// 用于接口和方法的返回类型定义
			Old:             "valueTypeExample",
			New:             g.valueType,
			IsCaseSensitive: false,
		},
		{
			// 替换值变量的声明语句
			// 例如：var valueNameExample interface{} -> var token string
			Old:             "var valueNameExample interface{}",
			New:             fmt.Sprintf("var %s %s", g.valueName, g.valueType),
			IsCaseSensitive: false,
		},
		{
			// 替换值变量名
			// 例如：valueNameExample -> token（当 --value-name=token 时）
			Old:             "valueNameExample",
			New:             g.valueName,
			IsCaseSensitive: false,
		},
	}...)

	// 如果生成的代码需要适配单体仓库（mono-repo）结构
	// 则添加服务器相关的子目录路径替换规则
	if g.suitedMonoRepo {
		fs := SubServerCodeFields(g.moduleName, g.serverName)
		fields = append(fields, fs...)
	}

	return fields
}
