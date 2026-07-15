package main

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"github.com/lolgopher/synology-filesync/protocol"
)

func downloadSynology(info *protocol.ConnectionInfo) {
	// synology client 생성
	synoClient, err := protocol.NewSynologyClient(info)
	if err != nil {
		log.Fatalf("fail to make synology client: %v", err)
	}

	wg.Add(1)
	go func() {
		defer func() {
			wg.Done()
		}()

		fileListResp, err := searchSynologyRecursive(synoClient, config.Synology.Path, 0)
		if err != nil {
			log.Fatalf("fail to search from synology filestation: %v", err)
		}

		if err := downloadSynologyRecursive(synoClient, fileListResp); err != nil {
			log.Fatalf("fail to download from synology filestation: %v", err)
		}
	}()
	wg.Wait()

	log.Print("Done!")
}

func isRecycleDirectory(name string) bool {
	return name == "#recycle"
}

func initializeMetadata(filePath string, size uint64) error {
	if !protocol.FileExists(filepath.Join(filepath.Dir(filePath), config.YAML.Filename)) {
		if err := protocol.WriteMetadata(filePath, config.YAML.Filename, size, protocol.Init); err != nil {
			log.Fatalf("fail to %s write metadata: %v", filePath, err)
		}
		log.Printf("init %s metadata", filePath)
	} else {
		targetMetadata, err := protocol.ReadMetadata(filepath.Dir(filePath), config.YAML.Filename)
		if err != nil {
			return err
		}

		if metadata, ok := targetMetadata[filePath]; !ok || metadata.Size != size {
			if err := protocol.WriteMetadata(filePath, config.YAML.Filename, size, protocol.Init); err != nil {
				log.Fatalf("fail to %s write metadata: %v", filePath, err)
			}
			log.Printf("init %s metadata", filePath)

			if protocol.FileExists(filePath) {
				if err := os.Remove(filePath); err != nil {
					log.Fatalf("fail to %s remove file: %v", filePath, err)
				}
				log.Printf("remove %s file", filePath)
			}
		} else {
			log.Printf("%s metedata already exist", filePath)
		}
	}

	return nil
}

func searchSynologyRecursive(client *protocol.SynologyClient, folderPath string, depth int) (*protocol.FileListResponse, error) {
	fileListResp, err := client.GetFileList(folderPath)
	if err != nil {
		return nil, err
	}

	for _, file := range fileListResp.Data.Files {
		// 폴더이고 휴지통이 아니면 검색
		if file.IsDir {
			if !isRecycleDirectory(file.Name) {
				if err := os.MkdirAll(filepath.Join(config.LocalPath, file.Path), os.ModePerm); err != nil {
					log.Fatalf("fail to make download folder: %v", err)
				}

				file.List, err = searchSynologyRecursive(client, file.Path, depth+1)
				if err != nil {
					return nil, err
				}
			}
		} else {
			initFilePath := filepath.Join(config.LocalPath, file.Path)

			// 메타데이터가 없으면 초기화
			if err := initializeMetadata(initFilePath, file.Additional.Size); err != nil {
				return nil, err
			}
		}
	}

	return fileListResp, nil
}

func downloadSynologyRecursive(client *protocol.SynologyClient, fileList *protocol.FileListResponse) error {
	ctx := context.Background()

	for _, file := range fileList.Data.Files {
		// 폴더이고 휴지통이 아니면 검색
		if file.IsDir {
			if !isRecycleDirectory(file.Name) {
				if err := downloadSynologyRecursive(client, file.List); err != nil {
					return err
				}
			}
		} else {
			// 파일이면 다운로드
			for {
				if err := sem.Acquire(ctx, 1); err != nil {
					log.Printf("fail to acquire semaphore: %v", err)
					continue
				} else {
					break
				}
			}

			filePath := file.Path

			wg.Add(1)
			go func() {
				defer func() {
					sem.Release(1)
					wg.Done()
				}()

				targetPath := filepath.Join(config.LocalPath, filePath)

				// 초기화 상태인지 확인
				targetMetadata, err := protocol.ReadMetadata(filepath.Dir(targetPath), config.YAML.Filename)
				if err != nil {
					log.Fatal(err)
				}

				if metadata, ok := targetMetadata[targetPath]; ok && metadata.Status != protocol.Init {
					log.Printf("%s has already been download", targetPath)
					return
				}

				downloadFilePath, _, err := client.DownloadFile(filePath, targetPath)
				if err != nil {
					log.Fatalf("fail to %s download file: %v", filePath, err)
				}

				if err := protocol.WriteMetadata(downloadFilePath, config.YAML.Filename, 0, protocol.NotSent); err != nil {
					log.Fatalf("fail to %s write metadata: %v", downloadFilePath, err)
				}
				log.Printf("%s success download", targetPath)
			}()
		}
	}

	return nil
}
