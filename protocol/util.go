package protocol

import (
	"fmt"
	"io/fs"
	"os"
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

func FileExists(filePath string) bool {
	_, err := os.Stat(filePath)
	return !os.IsNotExist(err)
}
