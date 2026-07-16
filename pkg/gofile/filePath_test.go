package gofile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestIsExists(t *testing.T) {
	ok := IsExists("/tmp/tmp/tmp")
	assert.Equal(t, false, ok)
	ok = IsExists("README.md")
	assert.Equal(t, true, ok)
}

func TestGetRunPath(t *testing.T) {
	t.Log(GetRunPath())
}

func TestListFiles(t *testing.T) {
	dir := "."

	t.Run("all files", func(t *testing.T) {
		files, err := ListFiles(dir)
		if err != nil {
			t.Error(err)
			return
		}
		t.Log(strings.Join(files, "\n"))
	})

	t.Run("prefix name", func(t *testing.T) {
		files, err := ListFiles(dir, WithPrefix("READ"))
		if err != nil {
			t.Error(err)
			return
		}
		t.Log(strings.Join(files, "\n"))
	})

	t.Run("suffix name", func(t *testing.T) {
		files, err := ListFiles(dir, WithSuffix(".go"))
		if err != nil {
			t.Error(err)
			return
		}
		t.Log(strings.Join(files, "\n"))
	})

	t.Run("contain name", func(t *testing.T) {
		files, err := ListFiles(dir, WithContain("file"))
		if err != nil {
			t.Error(err)
			return
		}
		t.Log(strings.Join(files, "\n"))
	})

	t.Run("no filepath abs", func(t *testing.T) {
		files, err := ListFiles(dir, WithSuffix(".go"), WithNoAbsolutePath())
		if err != nil {
			t.Error(err)
			return
		}
		t.Log(strings.Join(files, "\n"))
	})
}

func TestListDirsAndFiles(t *testing.T) {
	df, err := ListDirsAndFiles(".")
	if err != nil {
		t.Fatal(err)
	}
	for dir, files := range df {
		t.Log(dir, strings.Join(files, "\n"))
	}
}

func TestGetFilename(t *testing.T) {
	name := GetFilename("./README.md")
	assert.Equal(t, "README.md", name)

	name = GetFileSuffixName("./README.md")
	assert.Equal(t, ".md", name)

	name = GetDir("gofile/README.md")
	assert.Equal(t, "gofile", name)

	name = GetSuffixDir("gofile/")
	assert.Equal(t, "gofile", name)

	name = GetFileDir("gofile/README.md")
	assert.Equal(t, "gofile/", name)

	err := CreateDir("./")
	assert.Nil(t, err)
	notfoundDir := os.TempDir() + "/notfoundDir"
	err = CreateDir(notfoundDir)
	assert.Nil(t, err)
	time.Sleep(time.Millisecond * 100)
	_ = os.RemoveAll(notfoundDir)

	name = GetFilenameWithoutSuffix("./README.md")
	assert.Equal(t, "README", name)
}

func TestGetPathDelimiter(t *testing.T) {
	d := GetPathDelimiter()
	t.Log(d)
}

func TestNotMatch(t *testing.T) {
	fn := matchPrefix("")
	assert.Equal(t, false, fn("."))

	fn = matchContain("")
	assert.Equal(t, false, fn("."))

	fn = matchSuffix("")
	assert.NotNil(t, fn)
}

func TestIsWindows(t *testing.T) {
	t.Log(IsWindows())
}

func TestErrorPath(t *testing.T) {
	dir := "/notfound"

	_, err := ListFiles(dir)
	assert.Error(t, err)

	_, err = ListDirsAndFiles(dir)
	assert.Error(t, err)

	err = walkDirWithFilter(dir, nil, nil)
	assert.Error(t, err)

	err = walkDir(dir, nil)
	assert.Error(t, err)

	err = walkDir2(dir, nil, nil)
	assert.Error(t, err)
}

func TestFuzzyMatchFiles(t *testing.T) {
	files := FuzzyMatchFiles("./README.md")
	assert.Equal(t, 1, len(files))

	files = FuzzyMatchFiles("./*_test.go")
	assert.Equal(t, 2, len(files))
}

func TestJoin(t *testing.T) {
	elements := []string{"a/b", "c"}
	path := Join(elements...)
	t.Log(path)

	elements = []string{"a\\b", "c"}
	path = Join(elements...)
	t.Log(path)
}

func TestListDirs(t *testing.T) {
	dir := ".."
	dirs, err := ListDirs(dir)
	if err != nil {
		t.Error(err)
		return
	}

	t.Log(FilterDirs(dirs, WithSuffix(".txt")))
	t.Log(FilterDirs(dirs, WithPrefix("query")))
	t.Log(FilterDirs(dirs, WithContain("auth")))
}

func TestListSubDirs(t *testing.T) {
	dir := ".."
	dirs, err := ListSubDirs(dir, "gin")
	if err != nil {
		t.Error(err)
		return
	}
	t.Log(dirs)
}

// ---------- 新增功能测试 ----------

func TestIsDir(t *testing.T) {
	// 已存在的目录
	assert.True(t, IsDir("."))
	// 已存在的文件
	assert.False(t, IsDir("README.md"))
	// 不存在的路径
	assert.False(t, IsDir("/not_exist_path_xyz"))
	// 空字符串
	assert.False(t, IsDir(""))
}

func TestIsFile(t *testing.T) {
	// 已存在的文件
	assert.True(t, IsFile("README.md"))
	// 已存在的目录
	assert.False(t, IsFile("."))
	// 不存在的路径
	assert.False(t, IsFile("/not_exist_file_xyz"))
}

func TestGetFileSize(t *testing.T) {
	// 正常文件
	size, err := GetFileSize("README.md")
	assert.NoError(t, err)
	assert.Greater(t, size, int64(0))

	// 不存在的文件
	_, err = GetFileSize("/not_exist_file_xyz")
	assert.Error(t, err)
}

func TestReadFile(t *testing.T) {
	// 正常读取
	data, err := ReadFile("README.md")
	assert.NoError(t, err)
	assert.Greater(t, len(data), 0)

	// 不存在的文件
	_, err = ReadFile("/not_exist_file_xyz")
	assert.Error(t, err)
}

func TestWriteFile(t *testing.T) {
	dir := t.TempDir()
	filePath := dir + GetPathDelimiter() + "test_write.txt"
	content := []byte("hello gofile")

	// 写入文件
	err := WriteFile(filePath, content)
	assert.NoError(t, err)

	// 验证内容
	data, err := ReadFile(filePath)
	assert.NoError(t, err)
	assert.Equal(t, content, data)

	// 覆盖写入
	newContent := []byte("overwritten")
	err = WriteFile(filePath, newContent)
	assert.NoError(t, err)
	data, err = ReadFile(filePath)
	assert.NoError(t, err)
	assert.Equal(t, newContent, data)
}

func TestRemoveFile(t *testing.T) {
	dir := t.TempDir()
	filePath := dir + GetPathDelimiter() + "to_remove.txt"

	// 先创建文件
	err := WriteFile(filePath, []byte("delete me"))
	assert.NoError(t, err)
	assert.True(t, IsExists(filePath))

	// 删除文件
	err = RemoveFile(filePath)
	assert.NoError(t, err)
	assert.False(t, IsExists(filePath))

	// 删除不存在的文件
	err = RemoveFile(filePath)
	assert.Error(t, err)
}

func TestRemoveDir(t *testing.T) {
	dir := t.TempDir()
	subDir := dir + GetPathDelimiter() + "sub"
	subFile := subDir + GetPathDelimiter() + "file.txt"

	// 创建目录和文件
	err := CreateDir(subDir)
	assert.NoError(t, err)
	err = WriteFile(subFile, []byte("content"))
	assert.NoError(t, err)

	// 删除目录
	err = RemoveDir(subDir)
	assert.NoError(t, err)
	assert.False(t, IsExists(subDir))
	assert.False(t, IsExists(subFile))

	// 删除不存在的目录（RemoveAll 不返回错误）
	err = RemoveDir("/not_exist_dir_xyz")
	assert.NoError(t, err)
}

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := "README.md"
	dst := dir + GetPathDelimiter() + "copied_readme.md"

	// 复制文件
	err := CopyFile(src, dst)
	assert.NoError(t, err)
	assert.True(t, IsExists(dst))

	// 验证内容一致
	srcData, _ := ReadFile(src)
	dstData, _ := ReadFile(dst)
	assert.Equal(t, srcData, dstData)

	// 复制到嵌套目录（自动创建中间目录）
	nestedDst := dir + GetPathDelimiter() + "a" + GetPathDelimiter() + "b" + GetPathDelimiter() + "copied.txt"
	err = CopyFile(src, nestedDst)
	assert.NoError(t, err)
	assert.True(t, IsExists(nestedDst))

	// 复制不存在的源文件
	err = CopyFile("/not_exist_file_xyz", dir+GetPathDelimiter()+"none.txt")
	assert.Error(t, err)
}

func TestCopyDir(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir() + GetPathDelimiter() + "copied_dir"

	// 准备源目录结构
	_ = CreateDir(src + GetPathDelimiter() + "sub1")
	_ = CreateDir(src + GetPathDelimiter() + "sub2")
	_ = WriteFile(src+GetPathDelimiter()+"root.txt", []byte("root"))
	_ = WriteFile(src+GetPathDelimiter()+"sub1"+GetPathDelimiter()+"a.txt", []byte("a"))

	// 复制目录
	err := CopyDir(src, dst)
	assert.NoError(t, err)

	// 验证结构
	assert.True(t, IsDir(dst))
	assert.True(t, IsDir(dst+GetPathDelimiter()+"sub1"))
	assert.True(t, IsDir(dst+GetPathDelimiter()+"sub2"))
	assert.True(t, IsFile(dst+GetPathDelimiter()+"root.txt"))
	assert.True(t, IsFile(dst+GetPathDelimiter()+"sub1"+GetPathDelimiter()+"a.txt"))

	// 验证内容
	data, _ := ReadFile(dst + GetPathDelimiter() + "sub1" + GetPathDelimiter() + "a.txt")
	assert.Equal(t, []byte("a"), data)

	// 复制到不存在的源目录
	err = CopyDir("/not_exist_dir_xyz", t.TempDir()+GetPathDelimiter()+"out")
	assert.Error(t, err)
}

func TestReadFile_Empty(t *testing.T) {
	dir := t.TempDir()
	emptyFile := dir + GetPathDelimiter() + "empty.txt"
	err := WriteFile(emptyFile, []byte{})
	assert.NoError(t, err)

	data, err := ReadFile(emptyFile)
	assert.NoError(t, err)
	assert.Empty(t, data)
}

func TestWriteFile_NewDir(t *testing.T) {
	dir := t.TempDir()
	// 写入到不存在的嵌套目录
	nestedPath := dir + GetPathDelimiter() + "new" + GetPathDelimiter() + "nested" + GetPathDelimiter() + "file.txt"
	err := WriteFile(nestedPath, []byte("auto create dir"))
	assert.NoError(t, err)
	assert.True(t, IsFile(nestedPath))
}

func TestHomeDir(t *testing.T) {
	dir := HomeDir()
	assert.NotEmpty(t, dir)
	t.Logf("home dir: %s", dir)
	// 验证返回的路径是绝对路径且是目录
	assert.True(t, filepath.IsAbs(dir))
	assert.True(t, IsDir(dir))
}

func TestRename(t *testing.T) {
	dir := t.TempDir()
	delim := GetPathDelimiter()

	// 准备源文件
	src := dir + delim + "old.txt"
	content := []byte("rename test")
	err := WriteFile(src, content)
	assert.NoError(t, err)

	// 同一目录重命名
	dst := dir + delim + "new.txt"
	err = Rename(src, dst)
	assert.NoError(t, err)
	assert.False(t, IsExists(src))
	assert.True(t, IsFile(dst))

	// 验证内容一致
	data, err := ReadFile(dst)
	assert.NoError(t, err)
	assert.Equal(t, content, data)

	// 移动到嵌套目录（自动创建目标目录）
	nestedDst := dir + delim + "sub" + delim + "nested" + delim + "moved.txt"
	err = Rename(dst, nestedDst)
	assert.NoError(t, err)
	assert.True(t, IsFile(nestedDst))

	// 重命名目录
	srcDir := dir + delim + "src_dir"
	dstDir := dir + delim + "dst_dir"
	err = CreateDir(srcDir)
	assert.NoError(t, err)
	err = Rename(srcDir, dstDir)
	assert.NoError(t, err)
	assert.False(t, IsExists(srcDir))
	assert.True(t, IsDir(dstDir))

	// 重命名不存在的路径
	err = Rename("/not_exist_file_xyz", dir+delim+"none")
	assert.Error(t, err)
}

func TestTempDir(t *testing.T) {
	// 默认临时目录
	dir, err := TempDir("gofile_test_")
	assert.NoError(t, err)
	assert.NotEmpty(t, dir)
	assert.True(t, IsDir(dir))
	_ = RemoveDir(dir)

	// 指定父目录
	subDir := t.TempDir()
	dir, err = TempDirIn(subDir, "custom_")
	assert.NoError(t, err)
	assert.True(t, IsDir(dir))
	// 验证在指定父目录下
	assert.True(t, strings.HasPrefix(dir, subDir))
	_ = RemoveDir(dir)
}

func TestGetFileMode(t *testing.T) {
	dir := t.TempDir()
	filePath := dir + GetPathDelimiter() + "mode_test.txt"
	err := WriteFile(filePath, []byte("test"))
	assert.NoError(t, err)

	mode, err := GetFileMode(filePath)
	assert.NoError(t, err)
	assert.True(t, mode.IsRegular())
	t.Logf("file mode: %v", mode)

	// 目录
	mode, err = GetFileMode(dir)
	assert.NoError(t, err)
	assert.True(t, mode.IsDir())

	// 不存在的路径
	_, err = GetFileMode("/not_exist_file_xyz")
	assert.Error(t, err)
}

// ---------- 基准测试 ----------

func BenchmarkListFiles(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := ListFiles(".")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkListFilesWithFilter(b *testing.B) {
	b.Run("prefix", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := ListFiles(".", WithPrefix("file"))
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("suffix", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := ListFiles(".", WithSuffix(".go"))
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("contain", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := ListFiles(".", WithContain("file"))
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkWalkDir(b *testing.B) {
	var files []string
	for i := 0; i < b.N; i++ {
		files = files[:0]
		err := walkDir(".", &files)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCopyFile(b *testing.B) {
	src := "README.md"
	dir := b.TempDir()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dst := dir + GetPathDelimiter() + fmt.Sprintf("bench_copy_%d.tmp", i)
		if err := CopyFile(src, dst); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFindSubBytes(b *testing.B) {
	data := []byte(`prefix_data_suffix_prefix_data_suffix`)
	start := []byte("prefix")
	end := []byte("suffix")

	b.Run("FindSubBytes", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = FindSubBytes(data, start, end)
		}
	})

	b.Run("FindAllSubBytes", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = FindAllSubBytes(data, start, end)
		}
	})

	b.Run("FindSubBytesNotIn", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = FindSubBytesNotIn(data, start, end)
		}
	})
}

func BenchmarkReadWriteFile(b *testing.B) {
	dir := b.TempDir()
	data := make([]byte, 4096)
	for i := range data {
		data[i] = byte(i % 256)
	}

	b.Run("WriteFile", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			path := dir + GetPathDelimiter() + fmt.Sprintf("bench_%d.bin", i)
			if err := WriteFile(path, data); err != nil {
				b.Fatal(err)
			}
		}
	})

	filePath := dir + GetPathDelimiter() + "bench_read.bin"
	_ = WriteFile(filePath, data)

	b.Run("ReadFile", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := ReadFile(filePath); err != nil {
				b.Fatal(err)
			}
		}
	})
}
