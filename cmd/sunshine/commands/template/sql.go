package template

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/18721889353/sunshine/pkg/gofile"
	"github.com/18721889353/sunshine/pkg/replacer"
	"github.com/18721889353/sunshine/pkg/sql2code"
	"github.com/18721889353/sunshine/pkg/sql2code/parser"
)

var (
	printSQLOnce    sync.Once
	printSQLContent *strings.Builder
)

// SQLCommand generate code based on sql and custom template
// sqlCommandFlags SQL 命令的标志集合
type sqlCommandFlags struct {
	dbDriver    *string
	dbDsn       *string
	dbTable     *string
	tablePrefix *string
	tplDir      *string
	fieldsFile  *string
	outPath     *string
	onlyPrint   *bool
}

// setupSQLFlags 设置 SQL 命令的标志
func setupSQLFlags(cmd *cobra.Command, flags *sqlCommandFlags) {
	cmd.Flags().StringVarP(flags.dbDriver, "db-driver", "k", "", "database driver, support mysql")
	if err := cmd.MarkFlagRequired("db-driver"); err != nil {
		fmt.Printf("mark flag required error: %v\n", err)
	}
	cmd.Flags().StringVarP(flags.dbDsn, "db-dsn", "d", "", "database content address, e.g. user:password@(host:port)/database") //nolint
	if err := cmd.MarkFlagRequired("db-dsn"); err != nil {
		fmt.Printf("mark flag required error: %v\n", err)
	}
	cmd.Flags().StringVarP(flags.dbTable, "db-table", "t", "", "table name, multiple names separated by commas")
	if err := cmd.MarkFlagRequired("db-table"); err != nil {
		fmt.Printf("mark flag required error: %v\n", err)
	}
	cmd.Flags().StringVarP(flags.tablePrefix, "table-prefix", "p", "", "table name prefix, e.g. t_")

	cmd.Flags().StringVarP(flags.tplDir, "tpl-dir", "i", "", "directory where your template code is located")
	if err := cmd.MarkFlagRequired("tpl-dir"); err != nil {
		fmt.Printf("mark flag required error: %v\n", err)
	}
	cmd.Flags().StringVarP(flags.fieldsFile, "fields", "f", "", "fields defined in json file")
	cmd.Flags().BoolVarP(flags.onlyPrint, "only-print", "n", false, "only print template code and all fields, do not generate code")
	cmd.Flags().StringVarP(flags.outPath, "out", "o", "", "output directory, default is ./sql_to_template_<time>")
}

// executeSQLCommand 执行 SQL 命令的核心逻辑
func executeSQLCommand(sqlArgs *sql2code.Args, dbTables, tplDir, fieldsFile, outPath string, onlyPrint bool) (string, error) {
	if files, err := gofile.ListFiles(tplDir); err != nil {
		return "", err
	} else if len(files) == 0 {
		return "", fmt.Errorf("no template files found in directory '%s'", tplDir)
	}

	var m map[string]interface{}
	if fieldsFile != "" {
		var err error
		m, err = parseFields(fieldsFile)
		if err != nil {
			return "", err
		}
	}

	tableNames := strings.Split(dbTables, ",")
	l := len(tableNames)
	for i, tableName := range tableNames {
		if tableName == "" {
			continue
		}

		sqlArgs.DBTable = tableName
		codes, err := sql2code.Generate(sqlArgs)
		if err != nil {
			return "", err
		}

		tableInfo, err := parser.UnMarshalTableInfo(codes[parser.CodeTypeTableInfo])
		if err != nil {
			return "", err
		}
		fields, err := mergeFields(tableInfo, m)
		if err != nil {
			return "", err
		}

		g := sqlGenerator{
			tplDir:    tplDir,
			fields:    fields,
			onlyPrint: onlyPrint,
			outPath:   outPath,
		}
		outPath, err = g.generateCode()
		if err != nil {
			return "", err
		}

		if i != l-1 {
			printSQLContent.WriteString("\n    " +
				"------------------------------------------------------------------\n\n\n")
		}
	}

	return outPath, nil
}

// SQLCommand 创建基于 SQL 和自定义模板生成代码的命令
func SQLCommand() *cobra.Command {
	var (
		tplDir     = ""   // template directory
		fieldsFile = ""   // fields defined in json
		dbTables   string // table names
		outPath    string // output directory
		onlyPrint  bool   // only print template code and all fields

		sqlArgs = sql2code.Args{
			JSONTag:          true,
			GormType:         true,
			IsCustomTemplate: true,
		}
	)
	printSQLOnce = sync.Once{}
	printSQLContent = new(strings.Builder)

	cmd := &cobra.Command{
		Use:   "sql",
		Short: "Generate code based on sql and custom template",
		Long:  "Generate code based on sql and custom template.",
		Example: color.HiBlackString(`  # Generate code.
  sunshine template sql --db-driver=mysql --db-dsn=root:123456@(192.168.3.37:3306)/test --db-table=user --tpl-dir=yourTemplateDir

  # Generate code and specify fields defined in json file.
  sunshine template sql --db-driver=mysql --db-dsn=root:123456@(192.168.3.37:3306)/test --db-table=user --tpl-dir=yourTemplateDir --fields=yourDefineFields.json

  # Print template code and all fields, do not generate code.
  sunshine template sql --db-driver=mysql --db-dsn=root:123456@(192.168.3.37:3306)/test --db-table=user --tpl-dir=yourTemplateDir --fields=yourDefineFields.json --only-print

  # Generate code with multiple table names, each table generates code independently based on the template files.
  sunshine template sql --db-driver=mysql --db-dsn=root:123456@(192.168.3.37:3306)/test --db-table=t1,t2 --tpl-dir=yourTemplateDir

  # Generate code and specify output directory. Note: code generation will be canceled when the latest generated file already exists.
  sunshine template sql --db-driver=mysql --db-dsn=root:123456@(192.168.3.37:3306)/test --db-table=user --tpl-dir=yourTemplateDir --out=./yourDir`),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(_ *cobra.Command, _ []string) error {
			finalOutPath, err := executeSQLCommand(&sqlArgs, dbTables, tplDir, fieldsFile, outPath, onlyPrint)
			if err != nil {
				return err
			}

			if onlyPrint {
				fmt.Printf("%s", printSQLContent.String())
			} else {
				fmt.Printf("generate custom code successfully, out = %s\n", finalOutPath)
			}
			return nil
		},
	}

	setupSQLFlags(cmd, &sqlCommandFlags{
		dbDriver:    &sqlArgs.DBDriver,
		dbDsn:       &sqlArgs.DBDsn,
		dbTable:     &dbTables,
		tablePrefix: &sqlArgs.TablePrefix,
		tplDir:      &tplDir,
		fieldsFile:  &fieldsFile,
		outPath:     &outPath,
		onlyPrint:   &onlyPrint,
	})

	return cmd
}

type sqlGenerator struct {
	tplDir    string
	fields    map[string]interface{}
	onlyPrint bool
	outPath   string
}

func (g *sqlGenerator) generateCode() (string, error) {
	subTplName := "sql_to_template"
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
		printSQLOnce.Do(func() {
			listTemplateFiles(printSQLContent, files)
			printSQLContent.WriteString("\n\nAll fields name and value:\n")
		})
		listFields(printSQLContent, g.fields)
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
