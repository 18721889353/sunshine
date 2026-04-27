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
	SetReplacementFields(fields []Field)                                   // 设置替换字段
	SetSubDirsAndFiles(subDirs []string, subFiles ...string)               // 设置子目录和文件列表
	SetIgnoreSubDirs(dirs ...string)                                       // 设置忽略的子目录
	SetIgnoreSubFiles(filenames ...string)                                 // 设置忽略的文件
	SetOutputDir(absDir string, name ...string) error                      // 设置输出目录
	GetOutputDir() string                                                  // 获取输出目录
	GetSourcePath() string                                                 // 获取源目录
	SaveFiles() error                                                      // 保存文件
	ReadFile(filename string) ([]byte, error)                              // 读取文件内容
	GetFiles() []string                                                    // 获取文件列表
	SaveTemplateFiles(m map[string]interface{}, parentDir ...string) error // 保存模板文件
}

// replacerInfo 替换器信息结构体
type replacerInfo struct {
	path              string   // 模板目录或文件
	fs                embed.FS // 模板目录对应的二进制对象
	isActual          bool     // true: 使用 os 操作文件，false: 使用 fs 操作文件
	files             []string // 模板文件列表
	ignoreFiles       []string // 忽略的文件列表，例如 ignore.txt 或 myDir/ignore.txt
	ignoreDirs        []string // 忽略的子目录列表
	replacementFields []Field  // 从模板文件转换为新文件时需要替换的字符
	outPath           string   // 替换后文件保存的目录
}

// New 创建使用本地目录的替换器
func New(path string) (Replacer, error) {
	files, err := gofile.ListFiles(path)
	if err != nil {
		return nil, err
	}

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

// NewFS 创建使用嵌入目录的替换器
func NewFS(path string, fs embed.FS) (Replacer, error) {
	files, err := listFiles(path, fs)
	if err != nil {
		return nil, err
	}

	return &replacerInfo{
		path:              path,
		fs:                fs,
		isActual:          false,
		files:             files,
		replacementFields: []Field{},
	}, nil
}

// Field 替换字段信息结构体
type Field struct {
	Old             string // 要被替换的旧字段
	New             string // 新字段
	IsCaseSensitive bool   // 是否区分大小写
}

// SetReplacementFields 设置替换字段，注意：旧字符不应包含关系，如果存在，设置 Field 时需注意优先级
func (r *replacerInfo) SetReplacementFields(fields []Field) {
	var newFields []Field
	for _, field := range fields {
		if field.IsCaseSensitive && isFirstAlphabet(field.Old) { // 分割首字母字段
			if field.New == "" {
				continue
			}
			newFields = append(newFields,
				Field{ // 将首字母转换为大写
					Old: strings.ToUpper(field.Old[:1]) + field.Old[1:],
					New: strings.ToUpper(field.New[:1]) + field.New[1:],
				},
				Field{ // 将首字母转换为小写
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
func (r *replacerInfo) SetSubDirsAndFiles(subDirs []string, subFiles ...string) {
	subDirs = r.convertPathsDelimiter(subDirs...)
	subFiles = r.convertPathsDelimiter(subFiles...)

	var files []string
	isExistFile := make(map[string]struct{}) // 使用 map 避免重复文件
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
func (r *replacerInfo) SetIgnoreSubFiles(filenames ...string) {
	r.ignoreFiles = append(r.ignoreFiles, filenames...)
}

// SetIgnoreSubDirs 设置忽略的子目录
func (r *replacerInfo) SetIgnoreSubDirs(dirs ...string) {
	dirs = r.convertPathsDelimiter(dirs...)
	r.ignoreDirs = append(r.ignoreDirs, dirs...)
}

// SetOutputDir 设置输出目录，建议使用绝对路径，如果绝对路径为空，则根据参数名称在当前目录自动生成输出目录
func (r *replacerInfo) SetOutputDir(absPath string, name ...string) error {
	// 输出到指定目录
	if absPath != "" {
		abs, err := filepath.Abs(absPath)
		if err != nil {
			return err
		}

		r.outPath = abs
		return nil
	}

	// 输出到当前目录
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

// SaveFiles 根据设置保存文件
func (r *replacerInfo) SaveFiles() error {
	if r.outPath == "" {
		r.outPath = gofile.GetRunPath() + gofile.GetPathDelimiter() + "generate_" + time.Now().Format("150405")
	}

	var existFiles []string
	var writeData = make(map[string][]byte)

	for _, file := range r.files {
		if r.isInIgnoreDir(file) || r.isIgnoreFile(file) {
			continue
		}

		var data []byte
		var err error

		if r.isActual {
			data, err = os.ReadFile(file) // 从本地文件读取
		} else {
			data, err = r.fs.ReadFile(file) // 从嵌入的 FS 读取
		}
		if err != nil {
			return err
		}

		// 替换文本内容
		for _, field := range r.replacementFields {
			data = bytes.ReplaceAll(data, []byte(field.Old), []byte(field.New))
		}

		// 获取新的文件路径
		newFilePath := r.getNewFilePath(file)
		dir, filename := filepath.Split(newFilePath)
		// 替换文件名和目录名
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

	for file, data := range writeData {
		if isForbiddenFile(file, r.path) {
			return fmt.Errorf("禁止将文件(%s)写入目录(%s)，文件大小=%d", file, r.path, len(data))
		}
	}

	for file, data := range writeData {
		err := saveToNewFile(file, data)
		if err != nil {
			return err
		}
	}

	return nil
}

// SaveTemplateFiles 根据设置保存模板文件
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

// replaceTemplateData 替换模板数据
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

// replaceTemplateFilePath 替换模板文件路径
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

// trimExt 去除文件扩展名
func trimExt(file string) string {
	file = strings.TrimSuffix(file, ".tmpl")
	file = strings.TrimSuffix(file, ".tpl")
	file = strings.TrimSuffix(file, ".template")
	return file
}

// isIgnoreFile 判断文件是否在忽略列表中
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
func isForbiddenFile(file string, path string) bool {
	if gofile.IsWindows() {
		path = strings.ReplaceAll(path, "/", "\\")
		file = strings.ReplaceAll(file, "/", "\\")
	}
	return strings.Contains(file, path)
}

// getNewFilePath 获取新的文件路径
func (r *replacerInfo) getNewFilePath(file string) string {
	newFilePath := r.outPath + strings.Replace(file, r.path, "", 1)

	if gofile.IsWindows() {
		newFilePath = strings.ReplaceAll(newFilePath, "/", "\\")
	}

	return newFilePath
}

// getNewFilePath2 获取新的文件路径（带父目录）
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

// 如果是 Windows，转换路径分隔符
func (r *replacerInfo) convertPathDelimiter(filePath string) string {
	if r.isActual && gofile.IsWindows() {
		filePath = strings.ReplaceAll(filePath, "/", "\\")
	}
	return filePath
}

// 如果是 Windows，批量转换路径分隔符
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

// 保存新文件
func saveToNewFile(filePath string, data []byte) error {
	// 创建目录
	dir, _ := filepath.Split(filePath)
	err := os.MkdirAll(dir, 0766)
	if err != nil {
		return err
	}

	// 保存文件
	err = os.WriteFile(filePath, data, 0666)
	if err != nil {
		return err
	}

	return nil
}

// 遍历嵌入目录中的所有文件，返回文件的绝对路径
func listFiles(path string, fs embed.FS) ([]string, error) {
	var files []string
	err := walkDir(path, &files, fs)
	return files, err
}

// 遍历嵌入目录
func walkDir(dirPath string, allFiles *[]string, fs embed.FS) error {
	files, err := fs.ReadDir(dirPath)
	if err != nil {
		return err
	}

	for _, file := range files {
		deepFile := dirPath + "/" + file.Name()
		if file.IsDir() {
			if err := walkDir(deepFile, allFiles, fs); err != nil {
				return err
			}
			continue
		}
		*allFiles = append(*allFiles, deepFile)
	}

	return nil
}

// 判断字符串的第一个字符是否为字母
func isFirstAlphabet(str string) bool {
	if len(str) == 0 {
		return false
	}

	if (str[0] >= 'A' && str[0] <= 'Z') || (str[0] >= 'a' && str[0] <= 'z') {
		return true
	}

	return false
}

// 判断文件路径是否为子路径
func isSubPath(filePath string, subPath string) bool {
	dir, _ := filepath.Split(filePath)
	return strings.Contains(dir, subPath)
}

// 判断文件是否匹配
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
