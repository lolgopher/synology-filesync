package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func IsSameFileSize(targetFile string, compareFile fs.FileInfo) (bool, error) {
	target, err := os.Stat(targetFile)
	if err != nil {
		return false, fmt.Errorf("fail to get stat %s file: %v", targetFile, err)
	}

	if target.Size() != compareFile.Size() {
		return false, nil
	}

	return true, nil
}

func GetUniqueFilePath(filePath string) string {
	dir := filepath.Dir(filePath)
	ext := filepath.Ext(filePath)
	name := strings.TrimSuffix(filepath.Base(filePath), ext)

	// 파일 이름 뒤에 1부터 차례대로 숫자를 붙여가며 존재하지 않는 파일 이름 찾기
	for i := 1; ; i++ {
		newName := fmt.Sprintf("%s_%d%s", name, i, ext)
		if !FileExists(filepath.Join(dir, newName)) {
			return filepath.Join(dir, newName)
		}
	}
}

func FileExists(filePath string) bool {
	_, err := os.Stat(filePath)
	return !os.IsNotExist(err)
}
