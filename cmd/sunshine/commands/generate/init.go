package generate

import (
	"embed"     // 导入嵌入文件系统包
	"fmt"       // 导入格式化输入输出包
	"math/rand" // 导入随机数生成包
	"os"        // 导入操作系统包
	"strings"   // 导入字符串处理包
	"time"      // 导入时间处理包

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
var SunshineDir = getHomeDir() + gofile.GetPathDelimiter() + ".sunshine"

//var SunshineDir = build.Default.GOPATH + gofile.GetPathDelimiter() + "src" + gofile.GetPathDelimiter() + "sun" + gofile.GetPathDelimiter() + "sunshine"

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
	// 创建新的替换器并存储
	Replacers[TplNameSunshine], err = replacer.New(SunshineDir)
	if err != nil {
		return err
	}

	return nil
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
