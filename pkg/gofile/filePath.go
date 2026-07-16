package gofile

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// IsExists 判断文件或目录是否存在
//
// 参数：
//
//	f - 文件或目录路径
//
// 返回值：
//
//	bool - 如果文件或目录存在则返回 true
func IsExists(f string) bool {
	_, err := os.Stat(f)
	if err != nil {
		return !os.IsNotExist(err)
	}
	return true
}

// GetRunPath 获取程序执行的绝对路径
//
// 返回值：
//
//	string - 程序所在目录路径，获取失败则返回空字符串
func GetRunPath() string {
	// 获取 exe 文件路径，例如 /home/user/app/myapp
	dir, err := os.Executable()
	if err != nil {
		return ""
	}
	// 去掉文件名，只保留目录 /home/user/app/
	return filepath.Dir(dir)
}

// GetFilename 获取文件名（包含扩展名）
//
// 参数：
//
//	filePath - 文件路径
//
// 返回值：
//
//	string - 文件名称
func GetFilename(filePath string) string {
	_, name := filepath.Split(filePath)
	return name
}

// GetFileSuffixName 获取文件扩展名，例如 .txt
//
// 参数：
//
//	filePath - 文件路径
//
// 返回值：
//
//	string - 文件扩展名
func GetFileSuffixName(filePath string) string {
	return filepath.Ext(filePath)
}

// GetDir 获取文件所在目录路径（不含末尾分隔符）
//
// 参数：
//
//	filePath - 文件路径
//
// 返回值：
//
//	string - 目录路径
func GetDir(filePath string) string {
	return filepath.Dir(filePath)
}

// GetSuffixDir 获取文件或目录的末级目录名，不包含末尾分隔符
//
// 参数：
//
//	filePath - 文件或目录路径
//
// 返回值：
//
//	string - 末级目录名，获取失败则返回 filepath.Base 的结果
func GetSuffixDir(filePath string) string {
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return filepath.Base(filePath)
	}
	if !fileInfo.IsDir() {
		filePath = strings.TrimSuffix(filePath, fileInfo.Name())
	}
	return filepath.Base(filePath)
}

// GetFileDir 获取文件所在目录路径（包含末尾分隔符）
//
// 参数：
//
//	filePath - 文件路径
//
// 返回值：
//
//	string - 目录路径（含末尾分隔符）
func GetFileDir(filePath string) string {
	dir, _ := filepath.Split(filePath)
	return dir
}

// CreateDir 创建目录，如果目录已存在则不执行任何操作
//
// 参数：
//
//	dir - 要创建的目录路径
//
// 返回值：
//
//	error - 如果目录创建失败则返回错误
func CreateDir(dir string) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return os.MkdirAll(dir, 0766)
	}
	return nil
}

// GetFilenameWithoutSuffix 获取文件名（不含扩展名）
//
// 参数：
//
//	filePath - 文件路径
//
// 返回值：
//
//	string - 不含扩展名的文件名称
func GetFilenameWithoutSuffix(filePath string) string {
	_, name := filepath.Split(filePath)

	return strings.TrimSuffix(name, path.Ext(name))
}

// Join 将多个路径元素拼接为单一路径，并根据系统类型使用正确的分隔符
//
// 参数：
//
//	elem - 路径元素列表
//
// 返回值：
//
//	string - 拼接后的完整路径
func Join(elem ...string) string {
	dir := strings.Join(elem, "/")

	if IsWindows() {
		return strings.ReplaceAll(dir, "/", "\\")
	}

	return dir
}

// IsWindows 判断当前操作系统是否为 Windows
//
// 返回值：
//
//	bool - 如果是 Windows 系统则返回 true
func IsWindows() bool {
	return runtime.GOOS == "windows"
}

// GetPathDelimiter 获取当前系统的路径分隔符
//
// Windows 返回 "\\"，其他系统返回 "/"
//
// 返回值：
//
//	string - 系统路径分隔符
func GetPathDelimiter() string {
	delimiter := "/"
	if IsWindows() {
		delimiter = "\\"
	}

	return delimiter
}

// ListFiles 遍历指定目录下的所有文件，返回文件路径列表。
//
// 支持通过 Option 参数进行文件名过滤（前缀、后缀、包含匹配）和控制路径格式。
// 默认情况下会将路径转换为绝对路径，可通过 WithNoAbsolutePath() 禁用。
//
// 参数：
//
//	dirPath - 要遍历的目录路径
//	opts    - 可选参数：
//	          WithPrefix(name)    - 按文件名前缀匹配过滤
//	          WithSuffix(name)    - 按文件名后缀匹配过滤
//	          WithContain(name)   - 按文件名包含匹配过滤
//	          WithNoAbsolutePath() - 禁用路径转换为绝对路径
//
// 返回值：
//
//	[]string - 文件路径列表
//	error    - 如果目录不存在或路径解析失败则返回错误
func ListFiles(dirPath string, opts ...Option) ([]string, error) {
	files := []string{}
	err := error(nil)

	o := defaultOptions()
	o.apply(opts...)

	if !o.noAbsolutePath {
		dirPath, err = filepath.Abs(dirPath)
		if err != nil {
			return files, err
		}
	}

	switch o.filter {
	case prefix:
		return files, walkDirWithFilter(dirPath, &files, matchPrefix(o.name))
	case suffix:
		return files, walkDirWithFilter(dirPath, &files, matchSuffix(o.name))
	case contain:
		return files, walkDirWithFilter(dirPath, &files, matchContain(o.name))
	}

	return files, walkDir(dirPath, &files)
}

// ListDirsAndFiles 遍历指定目录下的所有子目录和文件，返回按类型分类的路径映射
//
// 参数：
//
//	dirPath - 要遍历的目录路径
//
// 返回值：
//
//	map[string][]string - 包含 "dirs"（子目录列表）和 "files"（文件列表）的映射
//	error              - 如果目录不存在或路径解析失败则返回错误
func ListDirsAndFiles(dirPath string) (map[string][]string, error) {
	df := make(map[string][]string, 2)

	dirPath, err := filepath.Abs(dirPath)
	if err != nil {
		return df, err
	}

	dirs := []string{}
	files := []string{}
	err = walkDir2(dirPath, &dirs, &files)
	if err != nil {
		return df, err
	}

	df["dirs"] = dirs
	df["files"] = files

	return df, nil
}

// FuzzyMatchFiles 模糊匹配文件，仅支持 * 通配符
//
// 参数：
//
//	f - 包含通配符的文件路径模式
//
// 返回值：
//
//	[]string - 匹配到的文件路径列表
func FuzzyMatchFiles(f string) []string {
	var files []string
	dir, filenameReg := filepath.Split(f)
	if !strings.Contains(filenameReg, "*") {
		files = append(files, f)
		return files
	}

	lFiles, err := ListFiles(dir)
	if err != nil {
		return files
	}
	for _, file := range lFiles {
		_, filename := filepath.Split(file)
		isMatch, err := path.Match(filenameReg, filename)
		if err != nil {
			continue
		}
		if isMatch {
			files = append(files, file)
		}
	}

	return files
}

// ListDirs 递归列出指定目录下的所有子目录（不包含自身）
//
// 参数：
//
//	specifiedDir - 要遍历的目录路径
//
// 返回值：
//
//	[]string - 所有子目录路径列表
//	error    - 如果目录读取失败则返回错误
func ListDirs(specifiedDir string) ([]string, error) {
	dir, err := os.ReadDir(specifiedDir)
	if err != nil {
		return nil, err
	}

	var dirs []string
	for _, fi := range dir {
		if fi.IsDir() {
			dirs = append(dirs, filepath.Join(specifiedDir, fi.Name()))
			tmpDirs, err := ListDirs(filepath.Join(specifiedDir, fi.Name()))
			if err != nil {
				return nil, err
			}
			dirs = append(dirs, tmpDirs...)
		}
	}

	return dirs, nil
}

// checkDirMatch 检查目录是否匹配过滤条件
//
// 参数：
//
//	dir        - 目录路径
//	file       - 目录条目
//	filterType - 过滤类型（prefix/suffix/contain）
//	name       - 匹配名称
//	existDir   - 已匹配的目录集合，用于去重
//
// 返回值：
//
//	bool     - 是否匹配成功
//	[]string - 匹配的目录列表
func checkDirMatch(dir string, file os.DirEntry, filterType string, name string, existDir map[string]struct{}) (bool, []string) {
	matched := false
	var filteredDirs []string

	switch filterType {
	case prefix:
		matched = matchPrefix(name)(file.Name())
	case suffix:
		matched = matchSuffix(name)(file.Name())
	case contain:
		matched = matchContain(name)(file.Name())
	}

	if matched {
		if _, ok := existDir[dir]; !ok {
			existDir[dir] = struct{}{}
			filteredDirs = append(filteredDirs, dir)
		}
	}

	return matched, filteredDirs
}

// FilterDirs 根据过滤条件筛选目录，返回包含匹配文件的目录列表
//
// 参数：
//
//	dirs - 要筛选的目录列表
//	opts - 过滤选项，支持 WithPrefix/WithSuffix/WithContain
//
// 返回值：
//
//	[]string - 包含匹配文件的目录路径列表
func FilterDirs(dirs []string, opts Option) []string {
	o := defaultOptions()
	o.apply(opts)

	var filteredDirs []string
	for _, dir := range dirs {
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}

		existDir := map[string]struct{}{}
		for _, file := range files {
			if file.IsDir() {
				continue
			}
			_, newDirs := checkDirMatch(dir, file, o.filter, o.name, existDir)
			filteredDirs = append(filteredDirs, newDirs...)
		}
	}

	return filteredDirs
}

// walkDirWithFilter 递归遍历目录并使用过滤函数筛选文件
//
// 参数：
//
//	dirPath  - 目录路径
//	allFiles - 用于收集匹配文件路径的切片指针
//	filter   - 文件过滤函数
//
// 返回值：
//
//	error - 如果目录读取失败则返回错误
func walkDirWithFilter(dirPath string, allFiles *[]string, filter filterFn) error {
	files, err := os.ReadDir(dirPath)
	if err != nil {
		return err
	}

	for _, file := range files {
		deepFile := dirPath + GetPathDelimiter() + file.Name()
		if file.IsDir() {
			err = walkDirWithFilter(deepFile, allFiles, filter)
			if err != nil {
				return err
			}
			continue
		}
		if filter(deepFile) {
			*allFiles = append(*allFiles, deepFile)
		}
	}

	return nil
}

// walkDir2 递归遍历目录，分别收集子目录和文件路径
//
// 参数：
//
//	dirPath  - 目录路径
//	allDirs  - 用于收集子目录路径的切片指针
//	allFiles - 用于收集文件路径的切片指针
//
// 返回值：
//
//	error - 如果目录读取失败则返回错误
func walkDir2(dirPath string, allDirs *[]string, allFiles *[]string) error {
	files, err := os.ReadDir(dirPath)
	if err != nil {
		return err
	}

	for _, file := range files {
		deepFile := dirPath + GetPathDelimiter() + file.Name()
		if file.IsDir() {
			*allDirs = append(*allDirs, deepFile)
			err = walkDir2(deepFile, allDirs, allFiles)
			if err != nil {
				return err
			}
			continue
		}
		*allFiles = append(*allFiles, deepFile)
	}

	return nil
}

// filterFn 文件路径过滤函数类型
type filterFn func(string) bool

// matchSuffix 创建后缀匹配过滤函数
//
// 参数：
//
//	suffixName - 要匹配的后缀名
//
// 返回值：
//
//	filterFn - 文件路径过滤函数
func matchSuffix(suffixName string) filterFn {
	return func(filename string) bool {
		if suffixName == "" {
			return false
		}

		size := len(filename) - len(suffixName)
		if size >= 0 && filename[size:] == suffixName {
			return true
		}
		return false
	}
}

// matchPrefix 创建前缀匹配过滤函数
//
// 参数：
//
//	prefixName - 要匹配的前缀名
//
// 返回值：
//
//	filterFn - 文件路径过滤函数
func matchPrefix(prefixName string) filterFn {
	return func(filePath string) bool {
		if prefixName == "" {
			return false
		}
		filename := GetFilename(filePath)
		size := len(filename) - len(prefixName)
		if size >= 0 && filename[:len(prefixName)] == prefixName {
			return true
		}
		return false
	}
}

// matchContain 创建包含匹配过滤函数
//
// 参数：
//
//	containName - 要匹配的字符串
//
// 返回值：
//
//	filterFn - 文件路径过滤函数
func matchContain(containName string) filterFn {
	return func(filePath string) bool {
		if containName == "" {
			return false
		}
		filename := GetFilename(filePath)
		return strings.Contains(filename, containName)
	}
}

// walkDir 递归遍历目录，收集所有文件路径
//
// 参数：
//
//	dirPath  - 目录路径
//	allFiles - 用于收集文件路径的切片指针
//
// 返回值：
//
//	error - 如果目录读取失败则返回错误
func walkDir(dirPath string, allFiles *[]string) error {
	files, err := os.ReadDir(dirPath)
	if err != nil {
		return err
	}

	for _, file := range files {
		deepFile := dirPath + GetPathDelimiter() + file.Name()
		if file.IsDir() {
			err = walkDir(deepFile, allFiles)
			if err != nil {
				return err
			}
			continue
		}
		*allFiles = append(*allFiles, deepFile)
	}

	return nil
}

// ListSubDirs 列出包含指定子目录的所有目录。如果 subDir 为空，则返回所有子目录。
//
// 参数：
//
//	root   - 根目录路径
//	subDir - 要查找的子目录名称，为空时返回所有子目录
//
// 返回值：
//
//	[]string - 符合条件的目录路径列表
//	error    - 如果遍历失败则返回错误
func ListSubDirs(root string, subDir string) ([]string, error) {
	var dirs []string
	err := filepath.Walk(root, func(dirPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() && hasSubDir(dirPath, subDir) {
			if subDir == "" {
				dirs = append(dirs, dirPath)
			} else {
				dirs = append(dirs, dirPath+GetPathDelimiter()+subDir)
			}
		}
		return nil
	})
	return dirs, err
}

// hasSubDir 判断指定目录下是否存在目标子目录
//
// 参数：
//
//	dirPath - 父目录路径
//	subDir  - 子目录名称
//
// 返回值：
//
//	bool - 如果子目录存在则返回 true
func hasSubDir(dirPath string, subDir string) bool {
	_, err := os.Stat(filepath.Join(dirPath, subDir))
	return err == nil || os.IsExist(err)
}

// IsDir 判断路径是否为目录
//
// 参数：
//
//	path - 文件或目录路径
//
// 返回值：
//
//	bool - 如果是目录则返回 true，路径不存在或无法访问时返回 false
func IsDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// IsFile 判断路径是否为文件
//
// 参数：
//
//	path - 文件或目录路径
//
// 返回值：
//
//	bool - 如果是文件则返回 true，路径不存在或无法访问时返回 false
func IsFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// GetFileSize 获取文件大小
//
// 参数：
//
//	path - 文件路径
//
// 返回值：
//
//	int64 - 文件大小（字节）
//	error - 如果文件不存在或无法访问则返回错误
func GetFileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// ReadFile 读取文件内容
//
// 参数：
//
//	path - 文件路径
//
// 返回值：
//
//	[]byte - 文件内容
//	error  - 如果文件不存在或读取失败则返回错误
func ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// WriteFile 将数据写入文件，自动创建所需目录。如果文件不存在则创建，存在则覆盖。
//
// 参数：
//
//	path - 文件路径
//	data - 要写入的数据
//
// 返回值：
//
//	error - 如果文件创建或写入失败则返回错误
func WriteFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0666)
}

// RemoveFile 删除文件
//
// 参数：
//
//	path - 要删除的文件路径
//
// 返回值：
//
//	error - 如果文件不存在或删除失败则返回错误
func RemoveFile(path string) error {
	return os.Remove(path)
}

// RemoveDir 删除目录及其所有子内容
//
// 参数：
//
//	path - 要删除的目录路径
//
// 返回值：
//
//	error - 如果目录不存在或删除失败则返回错误
func RemoveDir(path string) error {
	return os.RemoveAll(path)
}

// CopyFile 复制文件到目标路径，自动创建目标目录
//
// 参数：
//
//	src - 源文件路径
//	dst - 目标文件路径
//
// 返回值：
//
//	error - 如果源文件不存在或复制失败则返回错误
func CopyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	return dstFile.Sync()
}

// CopyDir 递归复制目录到目标路径
//
// 参数：
//
//	src - 源目录路径
//	dst - 目标目录路径
//
// 返回值：
//
//	error - 如果源目录不存在或复制失败则返回错误
func CopyDir(src, dst string) error {
	src = filepath.Clean(src)
	dst = filepath.Clean(dst)

	rel, err := filepath.Rel(src, dst)
	if err == nil && !strings.HasPrefix(rel, "..") {
		return fmt.Errorf("cannot copy directory into itself: %s -> %s", src, dst)
	}

	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, _ := filepath.Rel(src, path)
		targetPath := filepath.Join(dst, relPath)

		if d.IsDir() {
			return os.MkdirAll(targetPath, 0755)
		}

		return CopyFile(path, targetPath)
	})
}

// HomeDir 获取当前用户的主目录路径
//
// 返回值：
//
//	string - 用户主目录路径，获取失败则返回空字符串
func HomeDir() string {
	dir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return dir
}

// Rename 重命名或移动文件/目录。如果目标目录不存在则自动创建。
//
// 当 oldPath 和 newPath 在同一文件系统时执行重命名，否则执行复制+删除。
//
// 参数：
//
//	oldPath - 原路径
//	newPath - 新路径
//
// 返回值：
//
//	error - 如果操作失败则返回错误
func Rename(oldPath, newPath string) error {
	if err := os.MkdirAll(filepath.Dir(newPath), 0755); err != nil {
		return err
	}
	return os.Rename(oldPath, newPath)
}

// TempDir 在系统临时目录中创建以 pattern 为前缀的临时目录
//
// 参数：
//
//	pattern - 目录名前缀，可包含 "*" 号占位符
//
// 返回值：
//
//	string - 创建的临时目录路径
//	error  - 如果创建失败则返回错误
func TempDir(pattern string) (string, error) {
	return os.MkdirTemp("", pattern)
}

// TempDirIn 在指定目录中创建以 pattern 为前缀的临时目录
//
// 参数：
//
//	dir     - 父目录路径，为空则使用系统临时目录
//	pattern - 目录名前缀，可包含 "*" 号占位符
//
// 返回值：
//
//	string - 创建的临时目录路径
//	error  - 如果创建失败则返回错误
func TempDirIn(dir, pattern string) (string, error) {
	return os.MkdirTemp(dir, pattern)
}

// GetFileMode 获取文件或目录的权限模式
//
// 参数：
//
//	path - 文件或目录路径
//
// 返回值：
//
//	os.FileMode - 文件权限模式
//	error       - 如果文件不存在或无法访问则返回错误
func GetFileMode(path string) (os.FileMode, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Mode(), nil
}
