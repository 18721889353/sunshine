package commands

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/18721889353/sunshine/pkg/gobash"
	"github.com/18721889353/sunshine/pkg/gofile"
	"github.com/18721889353/sunshine/pkg/utils"
)

// UpgradeCommand 创建并返回升级 sunshine 二进制文件的命令实例
func UpgradeCommand() *cobra.Command {
	var targetVersion string

	cmd := &cobra.Command{
		// 定义命令的名称
		Use: "upgrade",
		// 定义命令的简短描述信息
		Short: "升级 sunshine 版本",
		// 定义命令的详细描述信息
		Long: "升级 sunshine 版本。",
		// 定义命令的使用示例
		Example: color.HiBlackString(`  # 升级到最新版本
  sunshine upgrade

  # 升级到指定版本
  sunshine upgrade --version=v1.5.6`),
		// 设置为静默错误输出
		SilenceErrors: true,
		// 设置为静默使用信息输出
		SilenceUsage: true,
		// 定义命令执行时的操作
		RunE: func(_ *cobra.Command, _ []string) error {
			if targetVersion == "" {
				targetVersion = latestVersion
			}
			ver, err := runUpgrade(targetVersion)
			if err != nil {
				return err
			}
			fmt.Printf("成功升级到版本 %s。\n", ver)
			return nil
		},
	}

	// 添加命令参数，允许用户指定目标版本
	cmd.Flags().StringVarP(&targetVersion, "version", "v", latestVersion, "升级 sunshine 版本")
	return cmd
}

// runUpgrade 执行升级操作
func runUpgrade(targetVersion string) (string, error) {
	// 升级 sunshine 二进制文件
	runningTip := "正在升级 sunshine 二进制文件 "
	finishTip := "升级 sunshine 二进制文件完成 " + installedSymbol
	failTip := "升级 sunshine 二进制文件失败 " + lackSymbol
	p := utils.NewWaitPrinter(time.Millisecond * 500)
	p.LoopPrint(runningTip)
	err := runUpgradeCommand(targetVersion)
	if err != nil {
		p.StopPrint(failTip)
		return "", err
	}
	p.StopPrint(finishTip)

	// 升级模板代码
	runningTip = "正在升级模板代码 "
	finishTip = "升级模板代码完成 " + installedSymbol
	failTip = "升级模板代码失败 " + lackSymbol
	p = utils.NewWaitPrinter(time.Millisecond * 500)
	p.LoopPrint(runningTip)
	ver, err := copyToTempDir(targetVersion)
	if err != nil {
		p.StopPrint(failTip)
		return "", err
	}
	p.StopPrint(finishTip)

	// 升级 sunshine 内置插件
	runningTip = "正在升级 sunshine 内置插件 "
	finishTip = "升级 sunshine 内置插件完成 " + installedSymbol
	failTip = "升级 sunshine 内置插件失败 " + lackSymbol
	p = utils.NewWaitPrinter(time.Millisecond * 500)
	p.LoopPrint(runningTip)
	err = updateSunshineInternalPlugin(ver)
	if err != nil {
		p.StopPrint(failTip)
		return "", err
	}
	p.StopPrint(finishTip)
	return ver, nil
}

// runUpgradeCommand 执行升级 sunshine 二进制文件的命令
func runUpgradeCommand(targetVersion string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*3) // 设置超时时间
	defer cancel()
	// 使用 gobash 运行 go install 命令来安装指定版本的 sunshine 命令
	result := gobash.Run(ctx, "go", "install", "github.com/18721889353/sunshine/cmd/sunshine@"+targetVersion)
	// 遍历 result.StdOut 通道，忽略输出内容
	// 注意：这里假设 StdOut 通道不需要处理，如果需要处理输出，可以在这里进行相应的操作
	for v := range result.StdOut {
		_ = v // 忽略输出内容
	}
	// 检查命令执行过程中是否发生错误
	if result.Err != nil {
		// 记录错误日志
		fmt.Printf("Error during go install: %v\n", result.Err)
		return result.Err // 返回错误信息
	}
	// 如果没有错误，返回 nil 表示成功
	return nil
}

// copyToTempDir 将模板文件复制到临时目录
func copyToTempDir(targetVersion string) (string, error) {
	result, err := gobash.Exec("go", "env", "GOPATH")
	if err != nil {
		return "", fmt.Errorf("执行命令失败, %v", err)
	}
	gopath := strings.ReplaceAll(string(result), "\n", "")
	if gopath == "" {
		return "", fmt.Errorf("$GOPATH 为空，你需要在 $PATH 中设置 $GOPATH")
	}

	sunshineDirName := ""
	if targetVersion == latestVersion {
		// 查找最新的 sunshine 代码目录
		arg := fmt.Sprintf("%s/pkg/mod/github.com/18721889353", gopath)
		result, err = gobash.Exec("ls", adaptPathDelimiter(arg))
		if err != nil {
			return "", fmt.Errorf("执行命令失败, %v", err)
		}

		sunshineDirName = getLatestVersion(string(result))
		if sunshineDirName == "" {
			return "", fmt.Errorf("未找到 sunshine 目录在 '$GOPATH/pkg/mod/github.com/18721889353'")
		}
	} else {
		sunshineDirName = "sunshine@" + targetVersion
	}

	srcDir := adaptPathDelimiter(fmt.Sprintf("%s/pkg/mod/github.com/18721889353/%s", gopath, sunshineDirName))
	destDir := adaptPathDelimiter(GetSunshineDir() + "/")
	targetDir := adaptPathDelimiter(destDir + ".sunshine")

	err = executeCommand("rm", "-rf", targetDir)
	if err != nil {
		return "", err
	}
	err = executeCommand("cp", "-rf", srcDir, targetDir)
	if err != nil {
		return "", err
	}
	err = executeCommand("chmod", "-R", "744", targetDir)
	if err != nil {
		return "", err
	}
	_ = executeCommand("rm", "-rf", targetDir+"/cmd/sunshine")
	_ = executeCommand("rm", "-rf", targetDir+"/cmd/protoc-gen-go-gin")
	_ = executeCommand("rm", "-rf", targetDir+"/cmd/protoc-gen-go-rpc-tmpl")
	_ = executeCommand("rm", "-rf", targetDir+"/cmd/protoc-gen-json-field")
	_ = executeCommand("rm", "-rf", targetDir+"/pkg")
	_ = executeCommand("rm", "-rf", targetDir+"/test")
	_ = executeCommand("rm", "-rf", targetDir+"/assets")

	versionNum := strings.Replace(sunshineDirName, "sunshine@", "", 1)
	err = os.WriteFile(versionFile, []byte(versionNum), 0644)
	if err != nil {
		return "", err
	}

	return versionNum, nil
}

// executeCommand 执行外部命令
func executeCommand(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*60) // 设置超时时间
	defer cancel()
	result := gobash.Run(ctx, name, args...)
	for v := range result.StdOut {
		_ = v
	}
	if result.Err != nil {
		return fmt.Errorf("执行命令失败, %v", result.Err)
	}
	return nil
}

// adaptPathDelimiter 根据操作系统调整路径分隔符
func adaptPathDelimiter(filePath string) string {
	if gofile.IsWindows() {
		filePath = strings.ReplaceAll(filePath, "/", "\\")
	}
	return filePath
}

// getLatestVersion 获取最新的 sunshine 版本目录名称
func getLatestVersion(s string) string {
	var dirNames = make(map[int]string)
	var nums []int

	dirs := strings.Split(s, "\n")
	for _, dirName := range dirs {
		if strings.Contains(dirName, "sunshine@") {
			tmp := strings.ReplaceAll(dirName, "sunshine@", "")
			ss := strings.Split(tmp, ".")
			if len(ss) != 3 {
				continue
			}
			if strings.Contains(ss[2], "v0.0.0") {
				continue
			}
			num := utils.StrToInt(ss[0])*10000 + utils.StrToInt(ss[1])*100 + utils.StrToInt(ss[2])
			nums = append(nums, num)
			dirNames[num] = dirName
		}
	}
	if len(nums) == 0 {
		return ""
	}

	sort.Ints(nums)
	return dirNames[nums[len(nums)-1]]
}

// updateSunshineInternalPlugin 更新 sunshine 内置插件
func updateSunshineInternalPlugin(targetVersion string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute*3) // 设置超时时间
	result := gobash.Run(ctx, "go", "install", "github.com/18721889353/sunshine/cmd/protoc-gen-go-gin@"+targetVersion)
	// 遍历 result.StdOut 通道，忽略输出内容
	// 注意：这里假设 StdOut 通道不需要处理，如果需要处理输出，可以在这里进行相应的操作
	for v := range result.StdOut {
		_ = v // 忽略输出内容
	}
	cancel() // 释放资源
	// 检查命令执行过程中是否发生错误
	if result.Err != nil {
		// 记录错误日志
		fmt.Printf("Error during go install: %v\n", result.Err)
		return result.Err // 返回错误信息
	}

	ctx, cancel = context.WithTimeout(context.Background(), time.Minute*3) // 设置超时时间
	result = gobash.Run(ctx, "go", "install", "github.com/18721889353/sunshine/cmd/protoc-gen-go-rpc-tmpl@"+targetVersion)

	// 遍历 result.StdOut 通道，忽略输出内容
	// 注意：这里假设 StdOut 通道不需要处理，如果需要处理输出，可以在这里进行相应的操作
	for v := range result.StdOut {
		_ = v // 忽略输出内容
	}
	cancel() // 释放资源
	// 检查命令执行过程中是否发生错误
	if result.Err != nil {
		// 记录错误日志
		fmt.Printf("Error during go install: %v\n", result.Err)
		return result.Err // 返回错误信息
	}

	// v1.x.x 版本不支持 protoc-gen-json-field
	if !strings.HasPrefix(targetVersion, "v1") {
		ctx, cancel = context.WithTimeout(context.Background(), time.Minute*3) // 设置超时时间
		result = gobash.Run(ctx, "go", "install", "github.com/18721889353/sunshine/cmd/protoc-gen-json-field@"+targetVersion)
		// 遍历 result.StdOut 通道，忽略输出内容
		// 注意：这里假设 StdOut 通道不需要处理，如果需要处理输出，可以在这里进行相应的操作
		for v := range result.StdOut {
			_ = v // 忽略输出内容
		}
		cancel() // 释放资源
		// 检查命令执行过程中是否发生错误
		if result.Err != nil {
			// 记录错误日志
			fmt.Printf("Error during go install: %v\n", result.Err)
			return result.Err // 返回错误信息
		}
	}

	return nil
}
