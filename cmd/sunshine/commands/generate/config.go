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
		Use:   "config",
		Short: "Generate go config code from yaml file",
		Long:  "Generate go config code from yaml file.",
		Example: color.HiBlackString(`  # Generate config code in server directory, the yaml configuration file must be in <yourServerDir>/configs directory.
  sunshine config --server-dir=/yourServerDir

  # Generate config code from yaml file.
  sunshine config --yaml-file=yourConfig.yml`),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(_ *cobra.Command, _ []string) error {
			if ysArgs.InputFile != "" {
				return convertToGoFile(ysArgs, outPath)
			}

			if serverDir == "" {
				return errors.New("set at least one of the parameters \"service-dir\" and \"yaml-file\"")
			}

			files, err := getYAMLFile(serverDir)
			if err != nil {
				return err
			}

			if len(files) == 0 {
				return fmt.Errorf("not found yaml configuration files in server directory %s/configs", serverDir)
			}

			err = runGenConfigCommand(files, ysArgs)
			if err != nil {
				return err
			}
			fmt.Println("convert yaml to go struct successfully.")
			return nil
		},
	}

	cmd.Flags().StringVarP(&serverDir, "server-dir", "d", "", "server directory")
	cmd.Flags().StringVarP(&ysArgs.InputFile, "yaml-file", "f", "", "yaml file")
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "output directory, default is ./config_<time>")

	cmd.AddCommand(
		ConfigmapCommand(), // k8s configmap command
	)

	return cmd
}

// runGenConfigCommand 遍历所有配置文件，调用 jy2struct 进行 YAML 转 Go 结构体转换，
// 并通过 saveFile 将生成的代码写入对应的 .go 文件。
// 参数:
//   - files: 输出文件路径到配置信息的映射。
//   - ysArgs: jy2struct 转换参数（格式、标签等）。
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
// 返回值:
//   - 输出 Go 文件路径到配置信息的映射。
//   - 如果目录不存在、同时存在 .yml 和 .yaml 或没有配置文件，返回对应的错误。
//
// 文件名约定:
//   - 以 cc.yml / cc.yaml 结尾的视为配置中心文件（生成 Center 结构体）。
//   - 其他视为普通配置文件（生成 Config 结构体）。
func getYAMLFile(serverDir string) (map[string]configType, error) {
	// generate target file:configuration file
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

	if len(ymlFiles) != 0 && len(yamlFiles) != 0 {
		return nil, fmt.Errorf("please use \"yml\" or \"yaml\" suffixes for configuration files, do not mix them")
	}

	if len(ymlFiles) > 0 {
		for _, file := range ymlFiles {
			name := gofile.GetFilename(file)
			files[goConfigDir+gofile.GetPathDelimiter()+strings.ReplaceAll(name, ".yml", ".go")] = configType{
				configFile:     file,
				isConfigCenter: strings.Contains(name, "cc.yml"),
			}
		}
		return files, nil
	}

	if len(yamlFiles) > 0 {
		for _, file := range yamlFiles {
			name := gofile.GetFilename(file)
			files[goConfigDir+gofile.GetPathDelimiter()+strings.ReplaceAll(name, ".yaml", ".go")] = configType{
				configFile:     file,
				isConfigCenter: strings.Contains(name, "cc.yaml"),
			}
		}
	}

	return files, nil
}

// saveFile 将生成的 Go 代码写入目标文件，并打印转换路径信息。
// 参数:
//   - inputFile: 源 YAML 配置文件路径（仅用于日志展示）。
//   - outputFile: 目标 Go 文件路径。
//   - code: 要写入的 Go 代码内容。
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
		outPath += "/yaml-to-go-struct-" + time.Now().Format("150405") + "/internal/config"
	} else {
		outPath, err = filepath.Abs(outPath)
		if err != nil {
			return err
		}
		outPath += "/internal/config"
	}
	if mkdirErr := os.MkdirAll(outPath, 0766); mkdirErr != nil {
		return fmt.Errorf("create directory %s error: %v", outPath, mkdirErr)
	}
	name := gofile.GetFilenameWithoutSuffix(ysArgs.InputFile)

	outPath += "/" + name + ".go"
	if gofile.IsWindows() {
		outPath = strings.ReplaceAll(outPath, "/", "\\")
	}

	err = os.WriteFile(outPath, []byte(configFileCode+data), 0666)
	if err != nil {
		return err
	}

	fmt.Printf("convert yaml to go struct successfully, out=%s\n", cutPath(outPath))

	return nil
}
