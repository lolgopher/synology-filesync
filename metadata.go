package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v2"
)

type FileMetadata struct {
	Size   int    `yaml:"size"`
	Status string `yaml:"status"`
}

type FileTransferStatus string

const (
	Init    FileTransferStatus = "INIT"
	NotSent FileTransferStatus = "NOT_SENT"
	Sent    FileTransferStatus = "SENT"
)

var mu sync.Mutex

func ReadMetadata(folderPath string) (map[string]FileMetadata, error) {
	// metadata.yaml 파일 경로 생성
	metadataFilePath := filepath.Join(folderPath, "metadata.yaml")

	// 파일 읽기
	data, err := os.ReadFile(metadataFilePath)
	if err != nil {
		return nil, fmt.Errorf("fail to read %s metadata file: %v", metadataFilePath, err)
	}

	// YAML 언마샬링
	var metadata map[string]FileMetadata
	if err := yaml.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("fail to unmarshal %s metadata file: %v", metadataFilePath, err)
	}

	return metadata, nil
}

func WriteMetadata(filePath string, size int, status FileTransferStatus) error {
	// 크리티컬 섹션 설정
	mu.Lock()
	defer mu.Unlock()

	// 폴더 경로와 메타데이터 파일 경로 설정
	folderPath := filepath.Dir(filePath)
	metadataFilePath := filepath.Join(folderPath, "metadata.yaml")

	// 메타데이터 파일 읽기
	data, err := os.ReadFile(metadataFilePath)
	if err != nil {
		// 파일이 존재하지 않으면 빈 데이터 생성
		if !os.IsNotExist(err) {
			return fmt.Errorf("fail to read %s metadata file: %v", metadataFilePath, err)
		}
		data = []byte{}
	}

	// 메타데이터 맵 생성 또는 업데이트
	metadata := make(map[string]FileMetadata)
	if err := yaml.Unmarshal(data, &metadata); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("fail to unmarshal %s metadata file: %v", metadataFilePath, err)
	}
	if status != Init {
		size = metadata[filePath].Size
	}
	metadata[filePath] = FileMetadata{
		Size:   size,
		Status: string(status),
	}

	// 메타데이터 파일 쓰기
	metadataData, err := yaml.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("fail to marshal %s : %s metadata file: %v", filePath, status, err)
	}
	if err := os.WriteFile(metadataFilePath, metadataData, 0644); err != nil {
		return fmt.Errorf("fail to write %s file: %v", metadataFilePath, err)
	}

	return nil
}
