// Package replacer 是一个用于替换文件内容的库，支持本地目录和嵌入目录文件的替换。
package replacer

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/18721889353/sunshine/pkg/gofile"
)

var _ Replacer = (*replacerInfo)(nil)

// Replacer 接口定义了文件替换器的行为
type Replacer interface {
	// SetReplacementFields 设置替换字段
	SetReplacementFields(fields []Field)
	// SetSubDirsAndFiles 设置子目录和文件列表，其他目录中的文件将被忽略
	SetSubDirsAndFiles(subDirs []string, subFiles ...string)
	// SetIgnoreSubDirs 设置忽略的子目录
	SetIgnoreSubDirs(dirs ...string)
	// SetIgnoreSubFiles 设置忽略的文件
	SetIgnoreSubFiles(filenames ...string)
	// SetOutputDir 设置输出目录
	SetOutputDir(absDir string, name ...string) error
	// GetOutputDir 获取输出目录
	GetOutputDir() string
	// GetSourcePath 获取源目录
	GetSourcePath() string
	// SaveFiles 保存文件
	SaveFiles() error
	// ReadFile 读取文件内容
	ReadFile(filename string) ([]byte, error)
	// GetFiles 获取文件列表
	GetFiles() []string
	// SaveTemplateFiles 保存模板文件
	SaveTemplateFiles(m map[string]interface{}, parentDir ...string) error
}

// replacerInfo 替换器信息结构体
type replacerInfo struct {
	path              string
	fs                embed.FS
	isActual          bool
	files             []string
	ignoreFiles       []string
	ignoreDirs        []string
	replacementFields []Field
	outPath           string
}

// New 创建使用本地目录的替换器
//
// 参数：
//
//	path - 本地模板目录路径
//
// 返回值：
//
//	Replacer - 替换器实例
//	error - 如果目录不存在或路径解析失败则返回错误
func New(path string) (Replacer, error) {
	files, err := gofile.ListFiles(path)
	if err != nil {
		return nil, err
	}
	//是跨平台的，它会根据操作系统自动处理路径分隔符
	//处理操作系统特定的分隔符（Windows 是 \，Linux 是 /）。操作本地文件时，永远使用 filepath 包。
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	return &replacerInfo{
		path:              path,
		isActual:          true,
		files:             files,
		replacementFields: []Field{},
	}, nil
}

// Field 替换字段信息结构体，定义单个字段的替换规则
//
// 字段说明：
//
//	Old             - 要被替换的旧字符串
//	New             - 替换后的新字符串
//	IsCaseSensitive - 是否区分大小写匹配
//	                  当为 true 且首字符为字母时，会自动生成首字母大写和小写两个替换规则
type Field struct {
	Old             string
	New             string
	IsCaseSensitive bool
}

// SetReplacementFields 设置替换字段
//
// 注意：旧字符不应包含关系，如果存在，设置 Field 时需注意优先级。
// 当字段区分大小写且首字符为字母时，会同时生成首字母大写和小写两个替换字段。
//
// 参数：
//
//	fields - 替换字段列表
func (r *replacerInfo) SetReplacementFields(fields []Field) {
	var newFields []Field
	for _, field := range fields {
		if field.IsCaseSensitive && isFirstAlphabet(field.Old) {
			if field.New == "" {
				continue
			}
			newFields = append(newFields,
				Field{
					Old: strings.ToUpper(field.Old[:1]) + field.Old[1:],
					New: strings.ToUpper(field.New[:1]) + field.New[1:],
				},
				Field{
					Old: strings.ToLower(field.Old[:1]) + field.Old[1:],
					New: strings.ToLower(field.New[:1]) + field.New[1:],
				},
			)
		} else {
			newFields = append(newFields, field)
		}
	}
	r.replacementFields = newFields
}

// GetFiles 获取文件列表
func (r *replacerInfo) GetFiles() []string {
	return r.files
}

// SetSubDirsAndFiles 设置指定子目录和文件的处理，其他目录中的文件将被忽略
//
// 参数：
//
//	subDirs  - 需要处理的子目录列表
//	subFiles - 需要处理的文件列表（可变参数）
func (r *replacerInfo) SetSubDirsAndFiles(subDirs []string, subFiles ...string) {
	subDirs = r.convertPathsDelimiter(subDirs...)
	subFiles = r.convertPathsDelimiter(subFiles...)

	var files []string
	isExistFile := make(map[string]struct{})
	for _, file := range r.files {
		for _, dir := range subDirs {
			if isSubPath(file, dir) {
				if _, ok := isExistFile[file]; ok {
					continue
				}
				isExistFile[file] = struct{}{}
				files = append(files, file)
			}
		}
		for _, sf := range subFiles {
			if isMatchFile(file, sf) {
				if _, ok := isExistFile[file]; ok {
					continue
				}
				isExistFile[file] = struct{}{}
				files = append(files, file)
			}
		}
	}

	if len(files) == 0 {
		return
	}
	r.files = files
}

// SetIgnoreSubFiles 设置忽略的文件
//
// 参数：
//
//	filenames - 需要忽略的文件名列表（可变参数）
func (r *replacerInfo) SetIgnoreSubFiles(filenames ...string) {
	r.ignoreFiles = append(r.ignoreFiles, filenames...)
}

// SetIgnoreSubDirs 设置忽略的子目录
//
// 参数：
//
//	dirs - 需要忽略的子目录列表（可变参数）
func (r *replacerInfo) SetIgnoreSubDirs(dirs ...string) {
	dirs = r.convertPathsDelimiter(dirs...)
	r.ignoreDirs = append(r.ignoreDirs, dirs...)
}

// SetOutputDir 设置输出目录
//
// 参数：
//
//	absPath - 绝对路径，如果为空则根据 name 参数在当前目录自动生成输出目录
//	name    - 用于自动生成输出目录的子路径名称（可变参数）
//
// 返回值：
//
//	error - 如果路径解析失败则返回错误
func (r *replacerInfo) SetOutputDir(absPath string, name ...string) error {
	if absPath != "" {
		abs, err := filepath.Abs(absPath)
		if err != nil {
			return err
		}

		r.outPath = abs
		return nil
	}

	subPath := strings.Join(name, "_")
	pwd, err := os.Getwd()
	if err != nil {
		return err
	}
	r.outPath = pwd + gofile.GetPathDelimiter() + subPath + "_" + time.Now().Format("150405")
	return nil
}

// GetOutputDir 获取输出目录
func (r *replacerInfo) GetOutputDir() string {
	return r.outPath
}

// GetSourcePath 获取源目录
func (r *replacerInfo) GetSourcePath() string {
	return r.path
}

// ReadFile 读取文件内容
//
// 参数：
//
//	filename - 要读取的文件名
//
// 返回值：
//
//	[]byte - 文件内容
//	error  - 如果文件不存在或读取失败则返回错误
func (r *replacerInfo) ReadFile(filename string) ([]byte, error) {
	filename = r.convertPathDelimiter(filename)

	var foundFile []string
	for _, file := range r.files {
		if strings.Contains(file, filename) && gofile.GetFilename(file) == gofile.GetFilename(filename) {
			foundFile = append(foundFile, file)
		}
	}
	if len(foundFile) != 1 {
		return nil, fmt.Errorf("total %d file named '%s', files=%+v", len(foundFile), filename, foundFile)
	}

	if r.isActual {
		return os.ReadFile(foundFile[0])
	}
	return r.fs.ReadFile(foundFile[0])
}

// processFileContent 处理单个文件的内容替换
//
// 参数：
//
//	file - 文件路径
//
// 返回值：
//
//	[]byte - 替换后的文件内容
//	error  - 如果文件读取失败则返回错误
func (r *replacerInfo) processFileContent(file string) ([]byte, error) {
	var data []byte
	var err error

	if r.isActual {
		data, err = os.ReadFile(file)
	} else {
		data, err = r.fs.ReadFile(file)
	}
	if err != nil {
		return nil, err
	}

	for _, field := range r.replacementFields {
		data = bytes.ReplaceAll(data, []byte(field.Old), []byte(field.New))
	}

	return data, nil
}

// getProcessedFilePath 获取处理后的文件路径，替换路径中的文件名和目录名
//
// 参数：
//
//	file - 原始文件路径
//
// 返回值：
//
//	string - 替换字段后的新文件路径
func (r *replacerInfo) getProcessedFilePath(file string) string {
	newFilePath := r.getNewFilePath(file)
	dir, filename := filepath.Split(newFilePath)

	for _, field := range r.replacementFields {
		if strings.Contains(dir, field.Old) {
			dir = strings.ReplaceAll(dir, field.Old, field.New)
		}
		if strings.Contains(filename, field.Old) {
			filename = strings.ReplaceAll(filename, field.Old, field.New)
		}

		if newFilePath != dir+filename {
			newFilePath = dir + filename
		}
	}

	return newFilePath
}

// validateWriteFiles 验证文件写入合法性，禁止将文件写入源目录
//
// 参数：
//
//	writeData - 待写入的文件路径与内容映射
//
// 返回值：
//
//	error - 如果存在禁止写入的文件则返回错误
func (r *replacerInfo) validateWriteFiles(writeData map[string][]byte) error {
	for file, data := range writeData {
		if isForbiddenFile(file, r.path) {
			return fmt.Errorf("禁止将文件(%s)写入目录(%s)，文件大小=%d", file, r.path, len(data))
		}
	}
	return nil
}

// SaveFiles 根据替换字段设置保存所有文件
//
// 返回值：
//
//	error - 如果文件已存在、写入被禁止或保存失败则返回错误
func (r *replacerInfo) SaveFiles() error {
	if r.outPath == "" {
		r.outPath = gofile.GetRunPath() + gofile.GetPathDelimiter() + "generate_" + time.Now().Format("150405")
	}

	var existFiles []string
	writeData := make(map[string][]byte)

	for _, file := range r.files {
		if r.isInIgnoreDir(file) || r.isIgnoreFile(file) {
			continue
		}

		data, err := r.processFileContent(file)
		if err != nil {
			return err
		}

		newFilePath := r.getProcessedFilePath(file)

		if gofile.IsExists(newFilePath) {
			existFiles = append(existFiles, newFilePath)
		}
		writeData[newFilePath] = data
	}

	if len(existFiles) > 0 {
		//nolint
		return fmt.Errorf("检测到已存在的文件\n    %s\n代码生成已取消\n",
			strings.Join(existFiles, "\n    "))
	}

	if err := r.validateWriteFiles(writeData); err != nil {
		return err
	}

	for file, data := range writeData {
		err := saveToNewFile(file, data)
		if err != nil {
			return err
		}
	}

	return nil
}

// SaveTemplateFiles 根据模板数据保存模板文件
//
// 参数：
//
//	m         - 模板数据映射，用于替换模板中的 {{.Field}} 占位符
//	parentDir - 输出目录下的父级子目录（可变参数）
//
// 返回值：
//
//	error - 如果模板解析失败或文件已存在则返回错误
func (r *replacerInfo) SaveTemplateFiles(m map[string]interface{}, parentDir ...string) error {
	refDir := ""
	if len(parentDir) > 0 {
		refDir = strings.Join(parentDir, gofile.GetPathDelimiter())
	}

	writeData := make(map[string][]byte, len(r.files))
	for _, file := range r.files {
		data, err := replaceTemplateData(file, m)
		if err != nil {
			return err
		}
		newFilePath := r.getNewFilePath2(file, refDir)
		newFilePath = trimExt(newFilePath)
		if gofile.IsExists(newFilePath) {
			return fmt.Errorf("文件 %s 已存在，取消代码生成", newFilePath)
		}
		newFilePath, err = replaceTemplateFilePath(newFilePath, m)
		if err != nil {
			return err
		}
		writeData[newFilePath] = data
	}

	for file, data := range writeData {
		err := saveToNewFile(file, data)
		if err != nil {
			return err
		}
	}

	return nil
}

// replaceTemplateData 替换模板文件中的数据，使用 text/template 渲染
//
// 参数：
//
//	file - 模板文件路径
//	m    - 模板数据映射
//
// 返回值：
//
//	[]byte - 渲染后的文件内容
//	error  - 如果文件读取或模板解析执行失败则返回错误
func replaceTemplateData(file string, m map[string]interface{}) ([]byte, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败，err=%s", err)
	}
	if !bytes.Contains(data, []byte("{{")) {
		return data, nil
	}

	builder := bytes.Buffer{}
	tmpl, err := template.New(file).Parse(string(data))
	if err != nil {
		return nil, fmt.Errorf("解析数据失败，err=%s", err)
	}
	err = tmpl.Execute(&builder, m)
	if err != nil {
		return nil, fmt.Errorf("执行数据失败，err=%s", err)
	}
	return builder.Bytes(), nil
}

// replaceTemplateFilePath 替换文件路径中的模板占位符
//
// 参数：
//
//	file - 文件路径字符串
//	m    - 模板数据映射
//
// 返回值：
//
//	string - 渲染后的文件路径
//	error  - 如果模板解析执行失败则返回错误
func replaceTemplateFilePath(file string, m map[string]interface{}) (string, error) {
	if !strings.Contains(file, "{{") {
		return file, nil
	}

	builder := strings.Builder{}
	tmpl, err := template.New("file: " + file).Parse(file)
	if err != nil {
		return file, fmt.Errorf("解析文件失败，err=%s", err)
	}
	err = tmpl.Execute(&builder, m)
	if err != nil {
		return file, fmt.Errorf("执行文件失败，err=%s", err)
	}
	return builder.String(), nil
}

// trimExt 去除文件扩展名，支持 .tmpl 和 .template 后缀
//
// 参数：
//
//	file - 文件名或文件路径
//
// 返回值：
//
//	string - 去除扩展名后的文件名或文件路径
func trimExt(file string) string {
	file = strings.TrimSuffix(file, ".tmpl")
	file = strings.TrimSuffix(file, ".template")
	return file
}

// isIgnoreFile 判断文件是否在忽略列表中
//
// 参数：
//
//	file - 文件名或文件路径
//
// 返回值：
//
//	bool - 如果在忽略列表中则返回 true
func (r *replacerInfo) isIgnoreFile(file string) bool {
	isIgnore := false
	for _, v := range r.ignoreFiles {
		if isMatchFile(file, v) {
			isIgnore = true
			break
		}
	}
	return isIgnore
}

// isInIgnoreDir 判断文件是否在忽略的子目录中
//
// 参数：
//
//	file - 文件名或文件路径
//
// 返回值：
//
//	bool - 如果在忽略的子目录中则返回 true
func (r *replacerInfo) isInIgnoreDir(file string) bool {
	isIgnore := false
	dir, _ := filepath.Split(file)
	for _, v := range r.ignoreDirs {
		if strings.Contains(dir, v) {
			isIgnore = true
			break
		}
	}
	return isIgnore
}

// isForbiddenFile 判断文件是否被禁止写入指定目录
//
// 参数：
//
//	file - 要写入的文件路径
//	path - 源目录路径
//
// 返回值：
//
//	bool - 如果文件路径包含源目录则返回 true
func isForbiddenFile(file string, path string) bool {
	if gofile.IsWindows() {
		path = strings.ReplaceAll(path, "/", "\\")
		file = strings.ReplaceAll(file, "/", "\\")
	}
	return strings.Contains(file, path)
}

// getNewFilePath 获取新的文件路径，将源路径前缀替换为输出路径
//
// 参数：
//
//	file - 原始文件路径
//
// 返回值：
//
//	string - 替换后的新文件路径
func (r *replacerInfo) getNewFilePath(file string) string {
	newFilePath := r.outPath + strings.Replace(file, r.path, "", 1)

	if gofile.IsWindows() {
		newFilePath = strings.ReplaceAll(newFilePath, "/", "\\")
	}

	return newFilePath
}

// getNewFilePath2 获取新的文件路径，支持在输出目录下添加父级子目录
//
// 参数：
//
//	file   - 原始文件路径
//	refDir - 输出目录下的父级子目录
//
// 返回值：
//
//	string - 替换后的新文件路径
func (r *replacerInfo) getNewFilePath2(file string, refDir string) string {
	if refDir == "" {
		return r.getNewFilePath(file)
	}

	newFilePath := r.outPath + gofile.GetPathDelimiter() + refDir + gofile.GetPathDelimiter() + strings.Replace(file, r.path, "", 1)
	if gofile.IsWindows() {
		newFilePath = strings.ReplaceAll(newFilePath, "/", "\\")
	}
	return newFilePath
}

// convertPathDelimiter 转换路径分隔符，仅在非嵌入模式且系统为 Windows 时将 "/" 转换为 "\"
//
// 参数：
//
//	filePath - 文件路径
//
// 返回值：
//
//	string - 转换后的文件路径
func (r *replacerInfo) convertPathDelimiter(filePath string) string {
	if r.isActual && gofile.IsWindows() {
		filePath = strings.ReplaceAll(filePath, "/", "\\")
	}
	return filePath
}

// convertPathsDelimiter 批量转换路径分隔符，仅在非嵌入模式且系统为 Windows 时将 "/" 转换为 "\"
//
// 参数：
//
//	filePaths - 文件路径列表
//
// 返回值：
//
//	[]string - 转换后的文件路径列表
func (r *replacerInfo) convertPathsDelimiter(filePaths ...string) []string {
	if r.isActual && gofile.IsWindows() {
		var filePathsTmp []string
		for _, dir := range filePaths {
			filePathsTmp = append(filePathsTmp, strings.ReplaceAll(dir, "/", "\\"))
		}
		return filePathsTmp
	}
	return filePaths
}

// saveToNewFile 将数据保存到新文件中，自动创建必要的目录
//
// 参数：
//
//	filePath - 文件保存路径
//	data     - 文件内容
//
// 返回值：
//
//	error - 如果目录创建或文件写入失败则返回错误
func saveToNewFile(filePath string, data []byte) error {
	dir, _ := filepath.Split(filePath)
	err := os.MkdirAll(dir, 0766)
	if err != nil {
		return err
	}

	err = os.WriteFile(filePath, data, 0666)
	if err != nil {
		return err
	}

	return nil
}

// isFirstAlphabet 判断字符串的第一个字符是否为字母
//
// 参数：
//
//	str - 输入的字符串
//
// 返回值：
//
//	bool - 如果第一个字符是字母则返回 true
func isFirstAlphabet(str string) bool {
	if len(str) == 0 {
		return false
	}

	if (str[0] >= 'A' && str[0] <= 'Z') || (str[0] >= 'a' && str[0] <= 'z') {
		return true
	}

	return false
}

// isSubPath 判断文件路径是否为指定子路径
//
// 参数：
//
//	filePath - 文件路径
//	subPath  - 子路径
//
// 返回值：
//
//	bool - 如果文件路径包含子路径则返回 true
func isSubPath(filePath string, subPath string) bool {
	dir, _ := filepath.Split(filePath)
	return strings.Contains(dir, subPath)
}

// isMatchFile 判断文件路径是否匹配指定的文件名和目录
//
// 参数：
//
//	filePath - 文件路径
//	sf       - 要匹配的文件名（可包含目录）
//
// 返回值：
//
//	bool - 如果文件名和目录都匹配则返回 true
func isMatchFile(filePath string, sf string) bool {
	dir1, file1 := filepath.Split(filePath)
	dir2, file2 := filepath.Split(sf)
	if file1 != file2 {
		return false
	}

	if gofile.IsWindows() {
		dir1 = strings.ReplaceAll(dir1, "/", "\\")
		dir2 = strings.ReplaceAll(dir2, "/", "\\")
	} else {
		dir1 = strings.ReplaceAll(dir1, "\\", "/")
		dir2 = strings.ReplaceAll(dir2, "\\", "/")
	}

	l1, l2 := len(dir1), len(dir2)
	if l1 >= l2 && dir1[l1-l2:] == dir2 {
		return true
	}
	return false
}
