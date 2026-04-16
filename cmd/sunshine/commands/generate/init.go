package generate

import (
	"embed"     // 导入嵌入文件系统包
	"fmt"       // 导入格式化输入输出包
	"math/rand" // 导入随机数生成包
	"os"        // 导入操作系统包
	"path/filepath"
	"strings" // 导入字符串处理包
	"time"    // 导入时间处理包

	"github.com/18721889353/sunshine/pkg/gofile"   // 导入文件操作包
	"github.com/18721889353/sunshine/pkg/replacer" // 导入替换器包
)

const warnSymbol = "⚠ " // 警告符号

// 初始化函数，设置随机种子
func init() {
	rand.Seed(time.Now().UnixNano()) // 使用当前时间的纳秒值作为随机数生成器的种子
}

// Replacers 存储替换器的映射
var Replacers = map[string]replacer.Replacer{}

// SunshineDir .sunshine 目录的路径
//var SunshineDir = getHomeDir() + gofile.GetPathDelimiter() + ".sunshine"

//var SunshineDir = build.Default.GOPATH + gofile.GetPathDelimiter() + "src" + gofile.GetPathDelimiter() + "sun" + gofile.GetPathDelimiter() + "sunshine"

// SunshineDir .sunshine 目录的路径 - 动态检测
var SunshineDir = getSunshineDir()

// getSunshineDir 动态获取 sunshine 项目根目录
func getSunshineDir() string {
	// 1. 优先使用环境变量
	if envDir := os.Getenv("SUNSHINE_TEMPLATE_DIR"); envDir != "" {
		return envDir
	}

	// 2. 从当前目录的 go.mod 中读取 replace 指令(适用于在生成的项目中调用)
	if gofile.IsExists("go.mod") {
		data, err := os.ReadFile("go.mod")
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
	exePath, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exePath)
		// 最多向上查找 10 层
		for i := 0; i < 10; i++ {
			// 检查是否包含关键目录来判断是否为项目根目录
			if gofile.IsExists(filepath.Join(dir, "cmd")) &&
				gofile.IsExists(filepath.Join(dir, "pkg")) &&
				gofile.IsExists(filepath.Join(dir, "internal")) {
				return dir
			}
			parent := filepath.Dir(dir)
			if parent == dir { // 已经到达文件系统根目录
				break
			}
			dir = parent
		}
	}

	// 4. 尝试从当前工作目录查找
	workDir, err := os.Getwd()
	if err == nil {
		dir := workDir
		for i := 0; i < 10; i++ {
			if gofile.IsExists(filepath.Join(dir, "cmd")) &&
				gofile.IsExists(filepath.Join(dir, "pkg")) &&
				gofile.IsExists(filepath.Join(dir, "internal")) {
				return dir
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	// 5. 回退到 ~/.sunshine
	homeDir, _ := os.UserHomeDir()
	if homeDir != "" {
		return filepath.Join(homeDir, ".sunshine")
	}

	return ""
}

// Template 模板信息结构体
type Template struct {
	Name     string   // 模板名称
	FS       embed.FS // 嵌入的文件系统
	FilePath string   // 文件路径
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

// detectLocalSunshineSource 检测是否在 sunshine 源码目录运行
func detectLocalSunshineSource() string {
	// 1. 从当前工作目录向上查找
	wd, err := os.Getwd()
	if err == nil {
		searchDir := wd
		for i := 0; i < 5; i++ {
			if gofile.IsExists(filepath.Join(searchDir, "cmd")) &&
				gofile.IsExists(filepath.Join(searchDir, "pkg")) &&
				gofile.IsExists(filepath.Join(searchDir, "internal")) {
				if gofile.IsExists(filepath.Join(searchDir, "go.mod")) {
					return filepath.ToSlash(searchDir)
				}
			}
			parent := filepath.Dir(searchDir)
			if parent == searchDir {
				break
			}
			searchDir = parent
		}
	}
	
	// 2. 从可执行文件路径向上查找(适用于 make proto 调用的情况)
	exePath, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exePath)
		for i := 0; i < 10; i++ {
			if gofile.IsExists(filepath.Join(dir, "cmd")) &&
				gofile.IsExists(filepath.Join(dir, "pkg")) &&
				gofile.IsExists(filepath.Join(dir, "internal")) {
				if gofile.IsExists(filepath.Join(dir, "go.mod")) {
					return filepath.ToSlash(dir)
				}
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	
	// 3. 从当前目录的 go.mod 中读取 replace 指令(适用于在生成的项目中调用)
	if gofile.IsExists("go.mod") {
		data, err := os.ReadFile("go.mod")
		if err == nil {
			lines := strings.Split(string(data), "\n")
			for _, line := range lines {
				if strings.Contains(line, "replace github.com/18721889353/sunshine =>") {
					parts := strings.Split(line, "=>")
					if len(parts) == 2 {
						path := strings.TrimSpace(parts[1])
						// Convert Windows path to Unix path
						path = filepath.ToSlash(path)
						if gofile.IsExists(path) {
							return path
						}
					}
				}
			}
		}
	}
	
	return ""
}

// InitFS 初始化嵌入文件系统的模板
func InitFS(name string, filepath string, fs embed.FS) {
	var err error
	// 检查模板名称是否已存在
	if _, ok := Replacers[name]; ok {
		panic(fmt.Sprintf("template name \"%s\" already exists", name))
	}
	// 创建新的嵌入文件系统替换器并存储
	Replacers[name], err = replacer.NewFS(filepath, fs)
	if err != nil {
		panic(err)
	}
}

// isShowCommand 判断是否显示命令
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

// getHomeDir 获取用户的主目录
func getHomeDir() string {
	dir, err := os.UserHomeDir()
	if err != nil {
		fmt.Println("can't get home directory'")
		return ""
	}

	return dir
}
