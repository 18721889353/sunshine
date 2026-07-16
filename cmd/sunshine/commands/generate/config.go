package generate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/18721889353/sunshine/pkg/gofile"
	"github.com/18721889353/sunshine/pkg/jy2struct"
)

// ConfigCommand 创建 config 子命令，用于将 YAML 配置文件转换为 Go 结构体代码。
// 支持两种模式:
//  1. 指定服务目录（--server-dir），自动扫描 configs 目录下的所有 YAML 文件。
//  2. 指定单个 YAML 文件（--yaml-file），转换为独立的 Go 结构体文件。
//
// 返回值:
//   - 配置了 RunE 逻辑的 cobra.Command 指针。
func ConfigCommand() *cobra.Command {
	var (
		ysArgs = jy2struct.Args{
			Format:    "yaml",
			Tags:      "json",
			SubStruct: true,
		}
		serverDir = ""
		outPath   string // output directory
	)

	cmd := &cobra.Command{
		Use:   "config",                                   // 子命令名称：sunshine config
		Short: "从 YAML 配置文件生成 Go 结构体代码",                   // 简短描述（help 中一行展示）
		Long:  "从 YAML 配置文件自动生成对应的 Go 结构体代码，支持单个文件或批量转换。", // 详细描述（help 中详细展示）
		Example: color.HiBlackString(`  # =====================================================================
  # 基本用法：从服务目录批量转换配置文件
  # 执行后会扫描 <server-dir>/configs 下所有 .yml/.yaml 文件，
  # 在 <server-dir>/internal/config 生成对应的 Go 结构体
  # =====================================================================
  sunshine config 
	--server-dir=/yourServerDir \
	--out=/d/Temp/web


  # =====================================================================
  # 参数说明：
  #   --server-dir  服务根目录，自动扫描其 configs 目录下的 YAML 文件
  #   --yaml-file   YAML 配置文件路径（与 --server-dir 二选一）
  #   --out         输出目录（可选，默认 ./config_<时间戳>/internal/config）
  #
  # 提示：
  #   - --server-dir 和 --yaml-file 两个参数至少指定一个
  #   - 以 cc.yml / cc.yaml 结尾的文件视为配置中心文件，生成 Center 结构体
  #   - 其他配置文件生成 Config 结构体
`), // 使用示例，彩色高亮显示
		SilenceErrors: true, // 不打印错误信息（由上层命令统一处理）
		SilenceUsage:  true, // 执行出错时不打印 usage 信息（避免冗余）
		RunE: func(_ *cobra.Command, _ []string) error {
			// 模式一：指定单个 YAML 文件 → 直接转换输出
			if ysArgs.InputFile != "" {
				return convertToGoFile(ysArgs, outPath)
			}

			// 校验：两种模式都未指定时提示错误
			if serverDir == "" {
				return errors.New("set at least one of the parameters \"service-dir\" and \"yaml-file\"")
			}

			// 模式二：指定服务目录 → 扫描 configs 目录下的所有 YAML 文件
			files, err := getYAMLFile(serverDir)
			if err != nil {
				return err
			}

			if len(files) == 0 {
				return fmt.Errorf("not found yaml configuration files in server directory %s/configs", serverDir)
			}

			// 遍历所有配置文件，逐个执行 YAML 转 Go 结构体
			err = runGenConfigCommand(files, ysArgs)
			if err != nil {
				return err
			}
			fmt.Println("convert yaml to go struct successfully.")
			return nil
		},
	}

	// 注册命令行参数
	// --server-dir / -d: 指定服务根目录，自动扫描 configs/*.yml
	cmd.Flags().StringVarP(&serverDir, "server-dir", "d", "", "服务根目录，自动扫描其 configs 目录下的 YAML 文件")
	// --yaml-file / -f: 指定单个 YAML 文件，转换为独立 Go 结构体
	cmd.Flags().StringVarP(&ysArgs.InputFile, "yaml-file", "f", "", "YAML 配置文件路径，指定后仅转换该单个文件")
	// --out / -o: 指定输出目录，默认生成到 ./config_<timestamp>/internal/config
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "输出目录，默认为 ./config_<时间戳>/internal/config")

	// 注册子命令：k8s configmap 生成
	cmd.AddCommand(
		ConfigmapCommand(),
	)

	return cmd
}

// runGenConfigCommand 遍历所有配置文件，调用 jy2struct 进行 YAML 转 Go 结构体转换，
// 并通过 saveFile 将生成的代码写入对应的 .go 文件。
// 参数:
//   - files: 输出文件路径到配置信息的映射。
//   - ysArgs: jy2struct 转换参数（格式、标签等）。
//
// 返回值:
//   - 如果任一文件转换或写入失败，返回对应的错误。
func runGenConfigCommand(files map[string]configType, ysArgs jy2struct.Args) error {
	for outputFile, config := range files {
		ysArgs.Format = "yaml"
		ysArgs.InputFile = config.configFile

		var startCode string
		if config.isConfigCenter {
			ysArgs.Name = "Center"
			startCode = configFileCcCode
		} else {
			ysArgs.Name = "Config"
			startCode = configFileCode
		}
		structCodes, err := jy2struct.Convert(&ysArgs)
		if err != nil {
			return err
		}
		err = saveFile(config.configFile, outputFile, startCode+structCodes)
		if err != nil {
			return err
		}
	}

	return nil
}

// configType 描述一个配置文件的来源和目标信息。
type configType struct {
	configFile     string
	isConfigCenter bool
}

// getYAMLFile 扫描服务目录下的 configs 文件夹，收集所有 .yml 或 .yaml 配置文件。
// 参数:
//   - serverDir: 服务根目录路径。
//
// 返回值:
//   - 输出 Go 文件路径到配置信息的映射。
//   - 如果目录不存在、同时存在 .yml 和 .yaml 或没有配置文件，返回对应的错误。
//
// 文件名约定:
//   - 以 cc.yml / cc.yaml 结尾的视为配置中心文件（生成 Center 结构体）。
//   - 其他视为普通配置文件（生成 Config 结构体）。
func getYAMLFile(serverDir string) (map[string]configType, error) {
	files := make(map[string]configType)
	configsDir := serverDir + gofile.GetPathDelimiter() + "configs"
	goConfigDir := serverDir + gofile.GetPathDelimiter() + "internal" + gofile.GetPathDelimiter() + "config"

	ymlFiles, err := gofile.ListFiles(configsDir, gofile.WithSuffix(".yml"))
	if err != nil {
		return nil, err
	}

	yamlFiles, err := gofile.ListFiles(configsDir, gofile.WithSuffix(".yaml"))
	if err != nil {
		return nil, err
	}

	if len(ymlFiles) == 0 && len(yamlFiles) == 0 {
		return nil, fmt.Errorf("not found config files in directory %s", configsDir)
	}

	if len(ymlFiles) > 0 && len(yamlFiles) > 0 {
		return nil, fmt.Errorf("please use \"yml\" or \"yaml\" suffixes for configuration files, do not mix them")
	}

	cfFiles := ymlFiles
	ext := ".yml"
	ccExt := "cc.yml"
	if len(yamlFiles) > 0 {
		cfFiles = yamlFiles
		ext = ".yaml"
		ccExt = "cc.yaml"
	}

	for _, file := range cfFiles {
		name := gofile.GetFilename(file)
		files[goConfigDir+gofile.GetPathDelimiter()+strings.ReplaceAll(name, ext, ".go")] = configType{
			configFile:     file,
			isConfigCenter: strings.Contains(name, ccExt),
		}
	}

	return files, nil
}

// saveFile 将生成的 Go 代码写入目标文件，并打印转换路径信息。
// 参数:
//   - inputFile: 源 YAML 配置文件路径（仅用于日志展示）。
//   - outputFile: 目标 Go 文件路径。
//   - code: 要写入的 Go 代码内容。
//
// 返回值:
//   - 如果文件写入失败，返回对应的错误。
func saveFile(inputFile string, outputFile string, code string) error {
	err := os.WriteFile(outputFile, []byte(code), 0666)
	if err != nil {
		return err
	}

	fmt.Printf("    %s  -->  %s\n", cutPath(inputFile), cutPath(outputFile))
	return nil
}

// convertToGoFile 将单个 YAML 文件转换为 Go 结构体代码并写入指定输出目录。
// 参数:
//   - ysArgs: jy2struct 转换参数，包含输入文件路径等信息。
//   - outPath: 输出目录路径，为空时自动创建以时间戳命名的目录。
//
// 返回值:
//   - 如果转换或文件写入失败，返回对应的错误。
func convertToGoFile(ysArgs jy2struct.Args, outPath string) error {
	ysArgs.Name = "Config"
	data, err := jy2struct.Convert(&ysArgs)
	if err != nil {
		return err
	}
	if outPath == "" {
		outPath, err = os.Getwd()
		if err != nil {
			return err
		}
		outPath = filepath.Join(outPath, "yaml-to-go-struct-"+time.Now().Format("150405"), "internal", "config")
	} else {
		outPath, err = filepath.Abs(outPath)
		if err != nil {
			return err
		}
		outPath = filepath.Join(outPath, "internal", "config")
	}
	if mkdirErr := os.MkdirAll(outPath, 0766); mkdirErr != nil {
		return fmt.Errorf("create directory %s error: %v", outPath, mkdirErr)
	}
	name := gofile.GetFilenameWithoutSuffix(ysArgs.InputFile)

	outPath = filepath.Join(outPath, name+".go")

	err = os.WriteFile(outPath, []byte(configFileCode+data), 0666)
	if err != nil {
		return err
	}

	fmt.Printf("convert yaml to go struct successfully, out=%s\n", cutPath(outPath))

	return nil
}
