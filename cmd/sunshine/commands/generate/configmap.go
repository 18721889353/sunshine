package generate

import (
	"errors"
	"fmt"

	"github.com/fatih/color"
	"github.com/huandu/xstrings"
	"github.com/spf13/cobra"

	"github.com/18721889353/sunshine/pkg/replacer"
)

// ConfigmapCommand 创建 configmap 子命令，用于生成 Kubernetes ConfigMap 配置文件。
//
// 返回值:
//   - 配置了 RunE 逻辑的 cobra.Command 指针。
func ConfigmapCommand() *cobra.Command {
	var (
		serverName  = ""
		projectName = ""
		configFile  = ""
		outPath     = ""
	)

	cmd := &cobra.Command{
		Use:   "cm",                                           // 子命令名称：sunshine config cm
		Short: "生成 Kubernetes ConfigMap 配置文件",                 // 简短描述（help 中一行展示）
		Long:  "将项目配置嵌入到 Kubernetes ConfigMap 模板中，生成完整的部署配置。", // 详细描述（help 中详细展示）
		Example: color.HiBlackString(`  # =====================================================================
  # 基本用法：生成 K8s ConfigMap 配置
  # 执行后会读取 config-file 配置内容，嵌入到 ConfigMap 模板中
  # =====================================================================
  sunshine config cm \
	--server-name=yourServerName \
	--project-name=yourProjectName \
	--config-file=yourConfigFile.yml \
	--out=/d/Temp/web


  # =====================================================================
  # 参数说明：
  #   --server-name  服务名称（必填），用于替换模板中的占位符
  #   --project-name 项目名称（必填），用于替换模板中的占位符
  #   --config-file  服务配置文件路径（必填），YAML 格式
  #   --out          输出目录（可选，默认 ./configmap_<时间戳>）
`), // 使用示例，彩色高亮显示
		SilenceErrors: true, // 不打印错误信息（由上层命令统一处理）
		SilenceUsage:  true, // 执行出错时不打印 usage 信息（避免冗余）
		RunE: func(_ *cobra.Command, _ []string) error {
			// 读取并转换 YAML 配置内容（每行添加缩进以适应 ConfigMap 格式）
			content, err := convertYamlConfig(configFile)
			if err != nil {
				return err
			}
			g := copyConfigGenerator{
				serverName:  serverName,
				projectName: projectName,
				content:     content,
				outPath:     outPath,
			}
			// 执行代码生成
			outPath, err = g.generateCode()
			if err != nil {
				return err
			}
			fmt.Printf("\ngenerate \"configmap\" code successfully, out = %s\n", cutPath(outPath))
			return nil
		},
	}

	// 注册命令行参数
	// --server-name / -s: 服务名称，用于替换 ConfigMap 模板中的占位符
	cmd.Flags().StringVarP(&serverName, "server-name", "s", "", "服务名称，用于替换模板中的占位符")
	if err := cmd.MarkFlagRequired("server-name"); err != nil {
		fmt.Printf("标记必填参数失败: %v\n", err)
	}
	// --project-name / -p: 项目名称
	cmd.Flags().StringVarP(&projectName, "project-name", "p", "", "项目名称")
	if err := cmd.MarkFlagRequired("project-name"); err != nil {
		fmt.Printf("标记必填参数失败: %v\n", err)
	}
	// --config-file / -f: 服务配置文件路径
	cmd.Flags().StringVarP(&configFile, "config-file", "f", "", "服务配置文件路径")
	// --out / -o: 输出目录
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "输出目录，默认为 ./configmap_<时间戳>")

	return cmd
}

// copyConfigGenerator ConfigMap 配置生成器
type copyConfigGenerator struct {
	serverName  string // 服务名称
	projectName string // 项目名称
	content     string // ConfigMap 配置内容
	outPath     string // 输出目录
}

func (g *copyConfigGenerator) generateCode() (string, error) {
	subTplName := "configmap"
	r := Replacers[TplNameSunshine]
	if r == nil {
		return "", errors.New("replacer is nil")
	}

	// 指定子目录和文件
	var subDirs = []string{
		"deployments/kubernetes",
	}
	ignoreDirs := []string{}
	ignoreFiles := []string{
		"projectNameExample-namespace.yml", "README.md", "serverNameExample-deployment.yml", "serverNameExample-svc.yml",
	}

	r.SetSubDirsAndFiles(subDirs)
	r.SetIgnoreSubDirs(ignoreDirs...)
	r.SetIgnoreSubFiles(ignoreFiles...)
	// 设置输出目录
	if err := r.SetOutputDir(g.outPath, subTplName); err != nil {
		return "", err
	}
	// 构建字段替换规则
	fields := g.addFields(r)
	// 应用替换规则
	r.SetReplacementFields(fields)
	// 保存文件
	if err := r.SaveFiles(); err != nil {
		return "", err
	}

	return r.GetOutputDir(), nil
}

// addFields 添加字段替换规则
func (g *copyConfigGenerator) addFields(_ replacer.Replacer) []replacer.Field {
	var fields []replacer.Field

	// 添加核心字段替换规则
	fields = append(fields, []replacer.Field{
		{ // 替换 ConfigMap 模板中的配置内容标记
			Old: configmapFileMark,
			New: g.content,
		},
		{ // 替换服务名称占位符（大小写敏感）
			Old:             "serverNameExample",
			New:             g.serverName,
			IsCaseSensitive: true,
		},
		{ // 替换服务名称占位符（kebab-case 格式）
			Old: "server-name-example",
			New: xstrings.ToKebabCase(g.serverName),
		},
		{ // 替换项目名称占位符
			Old: "project-name-example",
			New: g.projectName,
		},
	}...)

	return fields
}
