package main

import (
	"context"
	"github.com/lolgopher/synology-filesync/protocol"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
)

func downloadSynology(ip, port, sid string) {
	stopChan := make(chan int, 1)

	sumOfSize = 0
	wg.Add(1)
	go func() {
		defer func() {
			wg.Done()
		}()

		fileListResp, err := searchSynologyRecursive(ip, port, sid, synoPath, 0)
		if err != nil {
			log.Fatalf("fail to search from synology filestation: %v", err)
		}

		if err := writer.Flush(); err != nil {
			log.Print(err)
		}

		if err := downloadSynologyRecursive(ip, port, sid, fileListResp); err != nil {
			log.Fatalf("fail to download from synology filestation: %v", err)
		}
	}()
	go printProgress("Download...", stopChan)
	wg.Wait()
	stopChan <- 1

	close(stopChan)
	log.Print("Done!")
}

func searchSynologyRecursive(ip, port, sid, folderPath string, depth int) (*protocol.FileListResponse, error) {
	fileListResp, err := protocol.GetFileList(ip, port, sid, folderPath)
	if err != nil {
		return nil, err
	}

	for i, file := range fileListResp.Data.Files {
		_, _ = writer.WriteString(strings.Repeat("\t", depth) + file.Name + "\n")

		// 폴더이고 휴지통이 아니면 검색
		if file.IsDir {
			if file.Name != "#recycle" {
				if err := os.MkdirAll(filepath.Join(localPath, file.Path), os.ModePerm); err != nil {
					log.Fatalf("fail to make download folder: %v", err)
				}

				fileListResp.Data.Files[i].List, err = searchSynologyRecursive(ip, port, sid, file.Path, depth+1)
				if err != nil {
					return nil, err
				}
			}
		} else {
			initFilePath := filepath.Join(localPath, file.Path)

			// 메타데이터가 없으면 초기화
			if !protocol.FileExists(filepath.Join(filepath.Dir(initFilePath), "metadata.yaml")) {
				if err := WriteMetadata(initFilePath, file.Additional.Size, Init); err != nil {
					log.Fatalf("fail to %s write metadata: %v", initFilePath, err)
				}
			} else {
				// 이미 메타데이터가 존재하는지 확인
				targetMetadata, err := ReadMetadata(filepath.Dir(initFilePath))
				if err != nil {
					return nil, err
				}

				// 메타데이터에 정보가 없거나 파일 크기가 다르면 초기화
				if metadata, ok := targetMetadata[initFilePath]; !ok || metadata.Size != file.Additional.Size {
					if err := WriteMetadata(initFilePath, file.Additional.Size, Init); err != nil {
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

func downloadSynologyRecursive(ip, port, sid string, fileList *protocol.FileListResponse) error {
	ctx := context.Background()

	for _, file := range fileList.Data.Files {
		// 폴더이고 휴지통이 아니면 검색
		if file.IsDir {
			if file.Name != "#recycle" {
				if err := downloadSynologyRecursive(ip, port, sid, file.List); err != nil {
					return err
				}
			}
		} else {
			// 파일이면 다운로드
			for {
				if err := sem.Acquire(ctx, 1); err != nil {
					log.Printf("failed to acquire semaphore: %v", err)
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
				targetMetadata, err := ReadMetadata(filepath.Dir(targetPath))
				if err != nil {
					log.Fatal(err)
				}

				if metadata, ok := targetMetadata[targetPath]; ok && metadata.Status != string(Init) {
					log.Printf("%s has already been download", targetPath)
					return
				}

				downloadFilePath, size, err := protocol.DownloadFile(ip, port, sid, filePath, targetPath)
				if err != nil {
					log.Fatalf("fail to %s download file: %v", filePath, err)
				}
				atomic.AddInt64(&sumOfSize, size)

				if err := WriteMetadata(downloadFilePath, 0, NotSent); err != nil {
					log.Fatalf("fail to %s write metadata: %v", downloadFilePath, err)
				}
			}()
		}
	}

	return nil
}
