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

// UpgradeCommand upgrade sunshine binaries
func UpgradeCommand() *cobra.Command {
	var targetVersion string

	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade sunshine version",
		Long: color.HiBlackString(`upgrade sunshine version.

Examples:
  # upgrade to latest version
  sunshine upgrade
  # upgrade to specified version
  sunshine upgrade --version=v1.5.6
`),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("upgrading sunshine, please wait a moment ......")
			if targetVersion == "" {
				targetVersion = latestVersion
			}
			ver, err := runUpgrade(targetVersion)
			if err != nil {
				return err
			}
			fmt.Printf("upgraded version to %s successfully.\n", ver)
			return nil
		},
	}

	cmd.Flags().StringVarP(&targetVersion, "version", "v", latestVersion, "upgrade sunshine version")
	return cmd
}

func runUpgrade(targetVersion string) (string, error) {
	err := runUpgradeCommand(targetVersion)
	if err != nil {
		fmt.Println(lackSymbol + "upgrade sunshine binary.")
		return "", err
	}
	fmt.Println(installedSymbol + "upgraded sunshine binary.")
	ver, err := copyToTempDir(targetVersion)
	if err != nil {
		fmt.Println(lackSymbol + "upgrade template code.")
		return "", err
	}
	fmt.Println(installedSymbol + "upgraded template code.")
	err = updateSunshineInternalPlugin(ver)
	if err != nil {
		fmt.Println(lackSymbol + "upgrade protoc plugins.")
		return "", err
	}
	fmt.Println(installedSymbol + "upgraded protoc plugins.")
	return ver, nil
}

func runUpgradeCommand(targetVersion string) error {
	ctx, _ := context.WithTimeout(context.Background(), time.Minute*3) //nolint
	result := gobash.Run(ctx, "go", "install", "github.com/18721889353/sunshine/cmd/sunshine@"+targetVersion)
	for v := range result.StdOut {
		_ = v
	}
	if result.Err != nil {
		return result.Err
	}
	return nil
}

// copy the template files to a temporary directory
func copyToTempDir(targetVersion string) (string, error) {
	result, err := gobash.Exec("go", "env", "GOPATH")
	if err != nil {
		return "", fmt.Errorf("execute command failed, %v", err)
	}
	gopath := strings.ReplaceAll(string(result), "\n", "")
	if gopath == "" {
		return "", fmt.Errorf("$GOPATH is empty, you need set $GOPATH in your $PATH")
	}

	sunshineDirName := ""
	if targetVersion == latestVersion {
		// find the new version of the sunshine code directory
		arg := fmt.Sprintf("%s/pkg/mod/github.com/18721889353", gopath)
		result, err = gobash.Exec("ls", adaptPathDelimiter(arg))
		if err != nil {
			return "", fmt.Errorf("execute command failed, %v", err)
		}

		sunshineDirName = getLatestVersion(string(result))
		if sunshineDirName == "" {
			return "", fmt.Errorf("not found sunshine directory in '$GOPATH/pkg/mod/github.com/18721889353'")
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

func executeCommand(name string, args ...string) error {
	ctx, _ := context.WithTimeout(context.Background(), time.Second*30) //nolint
	result := gobash.Run(ctx, name, args...)
	for v := range result.StdOut {
		_ = v
	}
	if result.Err != nil {
		return fmt.Errorf("execute command failed, %v", result.Err)
	}
	return nil
}

func adaptPathDelimiter(filePath string) string {
	if gofile.IsWindows() {
		filePath = strings.ReplaceAll(filePath, "/", "\\")
	}
	return filePath
}

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

func updateSunshineInternalPlugin(targetVersion string) error {
	ctx, _ := context.WithTimeout(context.Background(), time.Minute) //nolint
	result := gobash.Run(ctx, "go", "install", "github.com/18721889353/sunshine/cmd/protoc-gen-go-gin@"+targetVersion)
	for v := range result.StdOut {
		_ = v
	}
	if result.Err != nil {
		return result.Err
	}

	ctx, _ = context.WithTimeout(context.Background(), time.Minute) //nolint
	result = gobash.Run(ctx, "go", "install", "github.com/18721889353/sunshine/cmd/protoc-gen-go-rpc-tmpl@"+targetVersion)
	for v := range result.StdOut {
		_ = v
	}
	if result.Err != nil {
		return result.Err
	}

	return nil
}
