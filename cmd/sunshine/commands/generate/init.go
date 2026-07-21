package generate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/18721889353/sunshine/pkg/gofile"
	"github.com/18721889353/sunshine/pkg/replacer"
)

const warnSymbol = "⚠ " // 警告符号

// 初始化函数
func init() {
	// Go 1.20+ 不再需要手动设置随机种子
}

// Replacers 存储替换器的映射
var Replacers = map[string]replacer.Replacer{}

// SunshineDir .sunshine 目录的路径
//var SunshineDir = getHomeDir() + gofile.GetPathDelimiter() + ".sunshine"

//var SunshineDir = build.Default.GOPATH + gofile.GetPathDelimiter() + "src" + gofile.GetPathDelimiter() + "sun" + gofile.GetPathDelimiter() + "sunshine"

// SunshineDir .sunshine 目录的路径 - 动态检测
var SunshineDir = getSunshineDir()

// getSunshineDir 动态获取 sunshine 项目根目录路径
//
// 按优先级依次尝试以下策略：
//  1. 环境变量 SUNSHINE_TEMPLATE_DIR
//  2. 从 go.mod 的 replace 指令中解析路径
//  3. 从可执行文件路径向上查找项目根目录
//  4. 从当前工作目录向上查找项目根目录
//  5. 回退到用户主目录下的 .sunshine
//
// 返回值：
//
//	string - 项目根目录路径，所有策略均失败则返回空字符串
func getSunshineDir() string {
	// 1. 优先使用环境变量
	if envDir := os.Getenv("SUNSHINE_TEMPLATE_DIR"); envDir != "" {
		return envDir
	}

	// 2. 从当前目录的 go.mod 中读取 replace 指令(适用于在生成的项目中调用)
	if gofile.IsExists("go.mod") {
		data, err := gofile.ReadFile("go.mod")
		if err == nil {
			lines := strings.Split(string(data), "\n")
			for _, line := range lines {
				if strings.Contains(line, "replace github.com/18721889353/sunshine =>") {
					parts := strings.Split(line, "=>")
					if len(parts) == 2 {
						path := strings.TrimSpace(parts[1])
						path = filepath.ToSlash(path)
						if gofile.IsExists(path) {
							return path
						}
					}
				}
			}
		}
	}

	// 3. 从可执行文件路径向上查找项目根目录
	if dir := gofile.GetRunPath(); dir != "" {
		if found := searchUpward(dir, 10, "cmd", "pkg", "internal"); found != "" {
			return found
		}
	}

	// 4. 尝试从当前工作目录查找
	workDir, err := os.Getwd()
	if err == nil {
		if found := searchUpward(workDir, 10, "cmd", "pkg", "internal"); found != "" {
			return found
		}
	}

	// 5. 回退到 ~/.sunshine
	homeDir := gofile.HomeDir()
	if homeDir == "" {
		fmt.Println("get user home directory error")
		return ""
	}
	return filepath.Join(homeDir, ".sunshine")
}

// searchUpward 向上查找包含指定目录的路径
func searchUpward(startDir string, maxDepth int, checkDirs ...string) string {
	dir := startDir
	for i := 0; i < maxDepth; i++ {
		allExist := true
		for _, d := range checkDirs {
			if !gofile.IsExists(filepath.Join(dir, d)) {
				allExist = false
				break
			}
		}
		if allExist {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// Init 初始化模板
func Init() error {
	// 判断 .sunshine 目录是否存在，如果不存在则提示用户先初始化
	if !gofile.IsExists(SunshineDir) {
		if isShowCommand() {
			return nil
		}
		return fmt.Errorf("%s not yet initialized, run the command \"sunshine init\"", warnSymbol)
	}

	var err error
	// 检查模板名称是否已存在
	if _, ok := Replacers[TplNameSunshine]; ok {
		panic(fmt.Sprintf("template name \"%s\" already exists", TplNameSunshine))
	}

	// 如果当前工作目录是 sunshine 源码目录，优先使用本地源码
	localSrcDir := detectLocalSunshineSource()
	if localSrcDir != "" {
		fmt.Printf("Using LOCAL sunshine source: %s\n", localSrcDir)
		Replacers[TplNameSunshine], err = replacer.New(localSrcDir)
	} else {
		// 否则使用 ~/.sunshine 或检测到的目录
		Replacers[TplNameSunshine], err = replacer.New(SunshineDir)
	}

	if err != nil {
		return err
	}

	return nil
}

// detectLocalSunshineSource 检测是否在 sunshine 源码目录中运行，并返回本地源码路径
//
// 按优先级依次尝试以下策略：
//  1. 从当前工作目录向上查找包含 go.mod 的源码根目录
//  2. 从可执行文件路径向上查找（适用于 make proto 等间接调用场景）
//  3. 从 go.mod 的 replace 指令中解析本地源码路径
//
// 返回值：
//
//	string - 本地 sunshine 源码根目录路径，未检测到则返回空字符串
func detectLocalSunshineSource() string {
	// 1. 从当前工作目录向上查找
	if wd, err := os.Getwd(); err == nil {
		if found := searchUpwardWithGoMod(wd, 5); found != "" {
			return found
		}
	}

	// 2. 从可执行文件路径向上查找(适用于 make proto 调用的情况)
	if dir := gofile.GetRunPath(); dir != "" {
		if found := searchUpwardWithGoMod(dir, 10); found != "" {
			return found
		}
	}

	// 3. 从当前目录的 go.mod 中读取 replace 指令(适用于在生成的项目中调用)
	return findFromGoModReplace()
}

// searchUpwardWithGoMod 向上查找包含指定目录且有go.mod的路径
func searchUpwardWithGoMod(startDir string, maxDepth int) string {
	dir := startDir
	for i := 0; i < maxDepth; i++ {
		if gofile.IsExists(filepath.Join(dir, "cmd")) &&
			gofile.IsExists(filepath.Join(dir, "pkg")) &&
			gofile.IsExists(filepath.Join(dir, "internal")) &&
			gofile.IsExists(filepath.Join(dir, "go.mod")) {
			return filepath.ToSlash(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// findFromGoModReplace 从go.mod的replace指令中查找路径
func findFromGoModReplace() string {
	if !gofile.IsExists("go.mod") {
		return ""
	}
	data, err := gofile.ReadFile("go.mod")
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.Contains(line, "replace github.com/18721889353/sunshine =>") {
			parts := strings.Split(line, "=>")
			if len(parts) == 2 {
				path := strings.TrimSpace(parts[1])
				path = filepath.ToSlash(path)
				if gofile.IsExists(path) {
					return path
				}
			}
		}
	}
	return ""
}

// isShowCommand 判断是否仅显示命令信息，不执行实际生成操作
//
// 当用户执行 "sunshine"、"sunshine init" 或 "sunshine -h" 等命令时，
// 只需展示帮助信息或初始化提示，无需继续执行模板生成流程。
//
// 返回值：
//
//	bool - 如果是仅显示命令则返回 true
func isShowCommand() bool {
	l := len(os.Args)

	// 只有 sunshine 命令
	if l == 1 {
		return true
	}

	// sunshine init 或 sunshine -h
	if l == 2 {
		if os.Args[1] == "init" || os.Args[1] == "-h" {
			return true
		}
		return false
	}
	// 多个参数，检查前三个参数中是否包含 init
	if l > 2 {
		return strings.Contains(strings.Join(os.Args[:3], ""), "init")
	}

	return false
}
