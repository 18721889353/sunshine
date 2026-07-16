package patch

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/18721889353/sunshine/pkg/gofile"
)

func cutPath(srcProtoFile string) string {
	dirPath, err := filepath.Abs("..")
	if err != nil {
		fmt.Printf("get absolute path error: %v\n", err)
		return srcProtoFile
	}
	srcProtoFile = strings.ReplaceAll(srcProtoFile, dirPath, "..")
	return strings.ReplaceAll(srcProtoFile, "\\", "/")
}

func cutPathPrefix(srcProtoFile string) string {
	dirPath, err := filepath.Abs(".")
	if err != nil {
		fmt.Printf("get absolute path error: %v\n", err)
		return srcProtoFile
	}
	srcProtoFile = strings.ReplaceAll(srcProtoFile, dirPath, ".")
	return strings.ReplaceAll(srcProtoFile, "\\", "/")
}

func listErrCodeFiles(dir string) ([]string, error) {
	files, err := gofile.ListFiles(dir)
	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return nil, errors.New("not found files")
	}

	filterFiles := []string{}
	for _, file := range files {
		if strings.Contains(file, "systemCode_http.go") || strings.Contains(file, "systemCode_rpc.go") {
			continue
		}
		if strings.Contains(file, "_http.go") || strings.Contains(file, "_rpc.go") {
			filterFiles = append(filterFiles, file)
		}
	}

	return filterFiles, nil
}

func getSubFiles(selectedFiles map[string][]string) []string {
	subFiles := []string{}
	for dir, files := range selectedFiles {
		for _, file := range files {
			subFiles = append(subFiles, dir+"/"+file)
		}
	}
	return subFiles
}
