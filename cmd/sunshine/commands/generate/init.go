package generate

import (
	"embed"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/18721889353/sunshine/pkg/gofile"
	"github.com/18721889353/sunshine/pkg/replacer"
)

const warnSymbol = "⚠ "

func init() {
	rand.Seed(time.Now().UnixNano()) //nolint
}

// Replacers replacer name
var Replacers = map[string]replacer.Replacer{}

// SunshineDir sunshine directory
var SunshineDir = getHomeDir() + gofile.GetPathDelimiter() + ".sunshine"

// Template information
type Template struct {
	Name     string
	FS       embed.FS
	FilePath string
}

// Init initializing the template
func Init() error {
	// determine if the template file exists, if not, prompt to initialize first
	if !gofile.IsExists(SunshineDir) {
		if isShowCommand() {
			return nil
		}
		return fmt.Errorf("%s not yet initialized, run the command \"sunshine init\"", warnSymbol)
	}

	var err error
	if _, ok := Replacers[TplNameSunshine]; ok {
		panic(fmt.Sprintf("template name \"%s\" already exists", TplNameSunshine))
	}
	Replacers[TplNameSunshine], err = replacer.New(SunshineDir)
	if err != nil {
		return err
	}

	return nil
}

// InitFS initializing th FS templates
func InitFS(name string, filepath string, fs embed.FS) {
	var err error
	if _, ok := Replacers[name]; ok {
		panic(fmt.Sprintf("template name \"%s\" already exists", name))
	}
	Replacers[name], err = replacer.NewFS(filepath, fs)
	if err != nil {
		panic(err)
	}
}

func isShowCommand() bool {
	l := len(os.Args)

	// sunshine
	if l == 1 {
		return true
	}

	// sunshine init or sunshine -h
	if l == 2 {
		if os.Args[1] == "init" || os.Args[1] == "-h" {
			return true
		}
		return false
	}
	if l > 2 {
		return strings.Contains(strings.Join(os.Args[:3], ""), "init")
	}

	return false
}

func getHomeDir() string {
	dir, err := os.UserHomeDir()
	if err != nil {
		fmt.Println("can't get home directory'")
		return ""
	}

	return dir
}
