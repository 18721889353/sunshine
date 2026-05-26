package template

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/18721889353/sunshine/cmd/sunshine/commands/generate"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/18721889353/sunshine/pkg/gobash"
	"github.com/18721889353/sunshine/pkg/gofile"
	"github.com/18721889353/sunshine/pkg/krand"
	"github.com/18721889353/sunshine/pkg/replacer"
)

var (
	printProtoOnce    sync.Once
	printProtoContent *strings.Builder
)

// ProtobufCommand generate code based on protobuf and custom template
// setupProtobufFlags 设置 protobuf 命令的标志
func setupProtobufFlags(cmd *cobra.Command, protobufFile, depProtoDir, tplDir, fieldsFile *string, onlyPrint *bool, outPath *string) {
	cmd.Flags().StringVarP(protobufFile, "protobuf-file", "p", "", "proto file")
	if err := cmd.MarkFlagRequired("protobuf-file"); err != nil {
		fmt.Printf("mark flag required error: %v\n", err)
	}
	cmd.Flags().StringVarP(depProtoDir, "dep-proto-dir", "d", "", "directory where the dependent proto files are located, example: ./depProtoDir")
	cmd.Flags().StringVarP(tplDir, "tpl-dir", "i", "", "directory where your template code is located")
	if err := cmd.MarkFlagRequired("tpl-dir"); err != nil {
		fmt.Printf("mark flag required error: %v\n", err)
	}
	cmd.Flags().StringVarP(fieldsFile, "fields", "f", "", "fields defined in json file")
	cmd.Flags().BoolVarP(onlyPrint, "only-print", "n", false, "only print template code and all fields, do not generate code")
	cmd.Flags().StringVarP(outPath, "out", "o", "", "output directory, default is ./protobuf_to_template_<time>")
}

// executeProtobufCommand 执行 protobuf 命令的核心逻辑
func executeProtobufCommand(protobufFile, depProtoDir, tplDir, fieldsFile, outPath string, onlyPrint bool) error {
	if files, err := gofile.ListFiles(tplDir); err != nil {
		return err
	} else if len(files) == 0 {
		return fmt.Errorf("no template files found in directory '%s'", tplDir)
	}

	m := make(map[string]interface{})
	if fieldsFile != "" {
		var err error
		m, err = parseFields(fieldsFile)
		if err != nil {
			return err
		}
	}

	thirdPartyDir, err := copyThirdPartyProtoFiles(depProtoDir)
	if err != nil {
		return err
	}
	defer deleteFileOrDir(thirdPartyDir)

	isSucceed := false
	pbfs, err := generate.ParseFuzzyProtobufFiles(protobufFile)
	if err != nil {
		return err
	}
	l := len(pbfs)
	for i, file := range pbfs {
		if !gofile.IsExists(file) || gofile.GetFileSuffixName(file) != ".proto" {
			continue
		}

		jsonFile, err := convertProtoToJSON(file, thirdPartyDir)
		if err != nil {
			return err
		}

		protoData, err := getProtoDataFromJSON(jsonFile)
		deleteFileOrDir(jsonFile)
		if err != nil {
			return err
		}
		protoMap := map[string]interface{}{"Proto": protoData}
		fields, err := mergeFields(protoMap, m)
		if err != nil {
			return err
		}

		g := protoGenerator{
			tplDir:    tplDir,
			fields:    fields,
			onlyPrint: onlyPrint,
			outPath:   outPath,
		}
		outPath, err = g.generateCode()
		if err != nil {
			return err
		}
		isSucceed = true

		if i != l-1 {
			printProtoContent.WriteString("\n    " +
				"------------------------------------------------------------------\n\n\n")
		}
	}

	if !isSucceed {
		return errors.New("no proto file found")
	}

	if onlyPrint {
		fmt.Println(printProtoContent.String())
	} else {
		fmt.Printf("generate custom code successfully, out = %s\n", outPath)
	}
	return nil
}

// ProtobufCommand 创建基于 protobuf 和自定义模板生成代码的命令
func ProtobufCommand() *cobra.Command {
	var (
		protobufFile string // protobuf file, support * matching
		depProtoDir  string // dependency protobuf files directory

		tplDir     = "" // template directory
		fieldsFile = "" // fields defined in json

		outPath   string // output directory
		onlyPrint bool   // only print template code and all fields
	)
	printProtoOnce = sync.Once{}
	printProtoContent = new(strings.Builder)

	cmd := &cobra.Command{
		Use:   "protobuf",
		Short: "Generate code based on protobuf and custom template",
		Long:  "Generate code based on protobuf and custom template.",
		Example: color.HiBlackString(`  # Generate code.
  sunshine template protobuf --protobuf-file=./test.proto --tpl-dir=yourTemplateDir

  # Generate code and specify fields defined in json file.
  sunshine template protobuf --protobuf-file=./test.proto --tpl-dir=yourTemplateDir --fields=yourDefineFields.json

  # Print template code and all fields, do not generate code.
  sunshine template protobuf --protobuf-file=./test.proto --tpl-dir=yourTemplateDir --fields=yourDefineFields.json --only-print

  # Generate code with dependency protobuf files, if your proto file import other proto files, you must specify them in the command.
  sunshine template protobuf --protobuf-file=./test.proto --dep-proto-dir=./depProtoDir --tpl-dir=yourTemplateDir

  # Generate code and specify output directory. Note: code generation will be canceled when the latest generated file already exists.
  sunshine template protobuf --protobuf-file=./test.proto --tpl-dir=yourTemplateDir --out=./yourDir`),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return executeProtobufCommand(protobufFile, depProtoDir, tplDir, fieldsFile, outPath, onlyPrint)
		},
	}

	setupProtobufFlags(cmd, &protobufFile, &depProtoDir, &tplDir, &fieldsFile, &onlyPrint, &outPath)

	return cmd
}

type protoGenerator struct {
	tplDir    string
	fields    map[string]interface{}
	onlyPrint bool
	outPath   string
}

func (g *protoGenerator) generateCode() (string, error) {
	subTplName := "protobuf_to_template"
	r, err := replacer.New(g.tplDir)
	if err != nil {
		return "", err
	}
	if r == nil {
		return "", errors.New("replacer is nil")
	}

	files := r.GetFiles()
	if len(files) == 0 {
		return "", errors.New("no template files found")
	}

	if g.onlyPrint {
		printProtoOnce.Do(func() {
			listTemplateFiles(printProtoContent, files)
			printProtoContent.WriteString("\n\nAll fields name and value:\n")
		})
		listFields(printProtoContent, g.fields)
		return "", nil
	}

	if err := r.SetOutputDir(g.outPath, subTplName); err != nil {
		return "", err
	}
	if err := r.SaveTemplateFiles(g.fields, gofile.GetSuffixDir(g.tplDir)); err != nil {
		return "", err
	}

	return r.GetOutputDir(), nil
}

func copyThirdPartyProtoFiles(depProtoDir string) (string, error) {
	thirdPartyDir := "third_party"
	if !gofile.IsExists(thirdPartyDir + "/google") {
		r := generate.Replacers[generate.TplNameSunshine]
		if r == nil {
			return "", errors.New("replacer is nil")
		}

		subDirs := []string{"sunshine/" + thirdPartyDir}
		subFiles := []string{}
		r.SetSubDirsAndFiles(subDirs, subFiles...)
		if err := r.SetOutputDir("."); err != nil { // out dir is third_party
			return "", err
		}
		err := r.SaveFiles()
		if err != nil {
			return "", err
		}
	}

	var err error
	var protoFiles []string
	if depProtoDir != "" {
		protoFiles, err = gofile.ListFiles(depProtoDir, gofile.WithSuffix(".proto"))
		if err != nil {
			return "", err
		}
	}
	for _, file := range protoFiles {
		err = copyProtoFileToDir(file, thirdPartyDir)
		if err != nil {
			return "", err
		}
	}

	return thirdPartyDir, nil
}

func convertProtoToJSON(protoFile string, thirdPartyDir string) (string, error) {
	dir := thirdPartyDir + "/" + krand.String(krand.RAll, 8)
	err := os.Mkdir(dir, 0755)
	if err != nil {
		return "", err
	}
	currentProtoFile := dir + "/" + gofile.GetFilename(protoFile)
	_, err = gobash.Exec("cp", "-f", protoFile, currentProtoFile)
	if err != nil {
		return "", err
	}

	protocArgs := []string{"--proto_path=.", fmt.Sprintf("--proto_path=%s", thirdPartyDir),
		"--json-field_out=.", "--json-field_opt=paths=source_relative", currentProtoFile}
	_, err = gobash.Exec("protoc", protocArgs...)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(currentProtoFile, ".proto") + ".json", nil
}

func getProtoDataFromJSON(jsonFile string) (map[string]interface{}, error) {
	protoData := make(map[string]interface{})
	data, err := os.ReadFile(jsonFile)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(data, &protoData)
	return protoData, err
}

func deleteFileOrDir(path string) {
	if strings.Contains(path, "third_party") || gofile.GetFileSuffixName(path) == ".json" || gofile.GetFileSuffixName(path) == ".proto" {
		for i := 0; i < 10; i++ {
			err := os.RemoveAll(path)
			if err == nil {
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
}
