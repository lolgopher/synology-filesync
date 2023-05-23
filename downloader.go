package main

import (
	"context"
	"github.com/lolgopher/synology-filesync/protocol"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

func downloadSynology(info *protocol.ConnectionInfo) {
	// synology client 생성
	synoClient, err := protocol.NewSynologyClient(info)
	if err != nil {
		log.Fatalf("fail to make synology client: %v", err)
	}

	stopChan := make(chan int, 1)

	sumOfSize = 0
	wg.Add(1)
	go func() {
		defer func() {
			wg.Done()
		}()

		fileListResp, err := searchSynologyRecursive(synoClient, synoPath, 0)
		if err != nil {
			log.Fatalf("fail to search from synology filestation: %v", err)
		}

		if err := writer.Flush(); err != nil {
			log.Print(err)
		}

		if err := downloadSynologyRecursive(synoClient, fileListResp); err != nil {
			log.Fatalf("fail to download from synology filestation: %v", err)
		}
	}()
	go printProgress("Download...", stopChan)
	wg.Wait()
	stopChan <- 1

	close(stopChan)
	log.Print("Done!")
}

func searchSynologyRecursive(client *protocol.SynologyClient, folderPath string, depth int) (*protocol.FileListResponse, error) {
	fileListResp, err := client.GetFileList(folderPath)
	if err != nil {
		return nil, err
	}

	for _, file := range fileListResp.Data.Files {
		_, _ = writer.WriteString(strings.Repeat("\t", depth) + file.Name + "\n")

		// 폴더이고 휴지통이 아니면 검색
		if file.IsDir {
			if file.Name != "#recycle" {
				if err := os.MkdirAll(filepath.Join(localPath, file.Path), os.ModePerm); err != nil {
					log.Fatalf("fail to make download folder: %v", err)
				}

				file.List, err = searchSynologyRecursive(client, file.Path, depth+1)
				if err != nil {
					return nil, err
				}
			}
		} else {
			initFilePath := filepath.Join(localPath, file.Path)

			// 메타데이터가 없으면 초기화
			if !protocol.FileExists(filepath.Join(filepath.Dir(initFilePath), "metadata.yaml")) {
				if err := protocol.WriteMetadata(initFilePath, file.Additional.Size, protocol.Init); err != nil {
					log.Fatalf("fail to %s write metadata: %v", initFilePath, err)
				}
			} else {
				// 이미 메타데이터가 존재하는지 확인
				targetMetadata, err := protocol.ReadMetadata(filepath.Dir(initFilePath))
				if err != nil {
					return nil, err
				}

				// 메타데이터에 정보가 없거나 파일 크기가 다르면 초기화
				if metadata, ok := targetMetadata[initFilePath]; !ok || metadata.Size != file.Additional.Size {
					if err := protocol.WriteMetadata(initFilePath, file.Additional.Size, protocol.Init); err != nil {
						log.Fatalf("fail to %s write metadata: %v", initFilePath, err)
					}

					// 기존 파일이 존재하면 삭제
					if protocol.FileExists(initFilePath) {
						if err := os.Remove(initFilePath); err != nil {
							log.Fatalf("fail to %s remove file: %v", initFilePath, err)
						}
					}
				}
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
			if file.Name != "#recycle" {
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

				targetPath := filepath.Join(localPath, filePath)

				// 초기화 상태인지 확인
				targetMetadata, err := protocol.ReadMetadata(filepath.Dir(targetPath))
				if err != nil {
					log.Fatal(err)
				}

				if metadata, ok := targetMetadata[targetPath]; ok && metadata.Status != string(protocol.Init) {
					log.Printf("%s has already been download", targetPath)
					return
				}

				downloadFilePath, size, err := client.DownloadFile(filePath, targetPath)
				if err != nil {
					log.Fatalf("fail to %s download file: %v", filePath, err)
				}
				atomic.AddInt64(&sumOfSize, size)

				if err := protocol.WriteMetadata(downloadFilePath, 0, protocol.NotSent); err != nil {
					log.Fatalf("fail to %s write metadata: %v", downloadFilePath, err)
				}
			}()
		}
	}

	return nil
}

func printProgress(title string, stop <-chan int) {
	ticker := time.NewTicker(delay)
	defer ticker.Stop()

	log.Print(title)
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			log.Printf("%07d MBytes", sumOfSize/1024/1024)
		}
	}
}
