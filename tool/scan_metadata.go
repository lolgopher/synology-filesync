package tool

import (
	"os"
	"path/filepath"

	metadata "github.com/lolgopher/synology-filesync/protocol"
)

func GetInitStatus(folderPath string) (map[string]metadata.FileMetadata, error) {
	return searchMetadata(folderPath, metadata.Init)
}

func GetNotSentStatus(folderPath string) (map[string]metadata.FileMetadata, error) {
	return searchMetadata(folderPath, metadata.NotSent)
}

func GetSentStatus(folderPath string) (map[string]metadata.FileMetadata, error) {
	return searchMetadata(folderPath, metadata.Sent)
}

func GetFailedStatus(folderPath string) (map[string]metadata.FileMetadata, error) {
	return searchMetadata(folderPath, metadata.Failed)
}

func searchMetadata(folderPath string, status metadata.FileTransferStatus) (map[string]metadata.FileMetadata, error) {
	result := make(map[string]metadata.FileMetadata)

	err := filepath.Walk(folderPath, func(targetPath string, info os.FileInfo, err error) error {
		if info.IsDir() {
			if data, err := metadata.ReadMetadata(targetPath, "metadata.yaml"); err == nil {
				for key, value := range data {
					if value.Status == status {
						result[key] = value
					}
				}
			}
		}

		return nil
	})

	return result, err
}
