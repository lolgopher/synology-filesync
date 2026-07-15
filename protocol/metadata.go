package protocol

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v2"
)

type FileMetadata struct {
	Size   uint64             `yaml:"size"`
	Status FileTransferStatus `yaml:"status"`
}

type FileTransferStatus string

const (
	Init    = FileTransferStatus("INIT")
	NotSent = FileTransferStatus("NOT_SENT")
	Sent    = FileTransferStatus("SENT")
	Failed  = FileTransferStatus("FAILED")
)

var mu sync.Mutex

func metadataPath(folderPath, filename string) string {
	return filepath.Join(folderPath, filename)
}

func readMetadataFile(metadataFilePath string, allowMissing bool) ([]byte, error) {
	data, err := os.ReadFile(metadataFilePath)
	if err != nil {
		if allowMissing && os.IsNotExist(err) {
			return []byte{}, nil
		}
		return nil, fmt.Errorf("fail to read %s metadata file: %v", metadataFilePath, err)
	}

	return data, nil
}

func unmarshalMetadataFile(data []byte, metadataFilePath, operation string, metadata *map[string]FileMetadata) error {
	if err := yaml.Unmarshal(data, metadata); err != nil {
		log.Printf("error to unmarshal %s data: %s", operation, string(data))
		return fmt.Errorf("fail to unmarshal %s metadata file: %v", metadataFilePath, err)
	}

	return nil
}

func marshalAndWriteMetadataFile(metadata map[string]FileMetadata, metadataFilePath, filePath string, status FileTransferStatus) error {
	metadataData, err := yaml.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("fail to marshal %s : %s metadata file: %v", filePath, status, err)
	}
	if err := os.WriteFile(metadataFilePath, metadataData, 0644); err != nil {
		return fmt.Errorf("fail to write %s file: %v", metadataFilePath, err)
	}

	return nil
}

func ReadMetadata(folderPath, filename string) (map[string]FileMetadata, error) {
	// 크리티컬 섹션 설정
	mu.Lock()
	defer mu.Unlock()

	// metadata.yaml 파일 경로 생성
	metadataFilePath := metadataPath(folderPath, filename)

	// 파일 읽기
	data, err := readMetadataFile(metadataFilePath, false)
	if err != nil {
		return nil, err
	}

	// YAML 언마샬링
	var metadata map[string]FileMetadata
	if err := unmarshalMetadataFile(data, metadataFilePath, "read", &metadata); err != nil {
		return nil, err
	}

	return metadata, nil
}

func WriteMetadata(filePath, filename string, size uint64, status FileTransferStatus) error {
	// 크리티컬 섹션 설정
	mu.Lock()
	defer mu.Unlock()

	// 폴더 경로와 메타데이터 파일 경로 설정
	folderPath := filepath.Dir(filePath)
	metadataFilePath := metadataPath(folderPath, filename)

	// 메타데이터 파일 읽기
	data, err := readMetadataFile(metadataFilePath, true)
	if err != nil {
		return err
	}

	// 메타데이터 맵 생성 또는 업데이트
	metadata := make(map[string]FileMetadata)
	if err := unmarshalMetadataFile(data, metadataFilePath, "write", &metadata); err != nil {
		return err
	}
	if status != Init {
		size = metadata[filePath].Size
	}
	metadata[filePath] = FileMetadata{
		Size:   size,
		Status: status,
	}

	// 메타데이터 파일 쓰기
	if err := marshalAndWriteMetadataFile(metadata, metadataFilePath, filePath, status); err != nil {
		return err
	}

	return nil
}
