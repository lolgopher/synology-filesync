package main

import (
	"fmt"
	"github.com/lolgopher/synology-filesync/protocol"
	"github.com/pkg/errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func uploadRemote(info *protocol.ConnectionInfo) {
	wg.Add(1)
	go func() {
		defer func() {
			wg.Done()
		}()

		// ssh client 생성
		client, err := protocol.NewSFTPClient(info)
		if err != nil {
			log.Fatalf("fail to make srtp client: %v", err)
		}
		defer func() {
			if err := client.Close(); err != nil {
				log.Printf("fail to close sftp client: %v", err)
			}
		}()

		if err := searchLocal(client, filepath.Join(config.Local.Path, config.Synology.Path)); err != nil {
			log.Fatalf("fail to search local: %v", err)
		}
	}()
	log.Print("Upload...")
	wg.Wait()

	log.Print("Done!")
}

func searchLocal(sftp *protocol.SFTPClient, folderPath string) error {
	// 파일 시스템에서 파일 검색
	err := filepath.Walk(folderPath, func(targetPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && info.Name() != "metadata.yaml" {
			// 전송에 성공했는지 확인
			targetMetadata, err := protocol.ReadMetadata(filepath.Dir(targetPath))
			if err != nil {
				return err
			}

			metadata, ok := targetMetadata[targetPath]
			if !ok {
				return fmt.Errorf("fail to find %s in metadata", targetPath)
			}

			switch protocol.FileTransferStatus(metadata.Status) {
			case protocol.Init:
				log.Printf("%s is init metadata status", targetPath)
				return nil
			case protocol.Sent:
				log.Printf("%s has already been sent", targetPath)
				return nil
			case protocol.Failed:
				log.Printf("%s sent failed", targetPath)
				return nil
			case protocol.NotSent:
				var result protocol.FileTransferStatus
				if size, err := sendFileOverSFTP(&sftp, targetPath); err != nil {
					// 전송에 실패했을때
					result = protocol.Failed
					log.Printf("fail to %s not sent file: %v", targetPath, err)
				} else {
					// 전송에 성공했을때
					result = protocol.Sent

					// 이미 전송되었다면
					if size == 0 {
						log.Printf("same size file %s already exist", targetPath)
					} else {
						log.Printf("%s: %d", targetPath, size)
					}
				}

				if err := protocol.WriteMetadata(targetPath, 0, result); err != nil {
					return err
				}
				time.Sleep(time.Duration(config.UploadDelay) * time.Second)
			default:
				log.Printf("%s is unknown status", metadata.Status)
				return nil
			}
		}

		return nil
	})

	return err
}

func sendFileOverSFTP(sftp **protocol.SFTPClient, targetPath string) (int, error) {
	var lastError error
	size := 0
	for i := 0; i < config.UploadRetryCount; i++ {
		destPath, _ := strings.CutPrefix(targetPath, config.Local.Path)
		destPath = filepath.Join(config.Remote.Path, destPath)

		// 용량 확인
		targetFileInfo, err := os.Stat(targetPath)
		if err != nil {
			lastError = fmt.Errorf("fail to get %s file info: %v", targetFileInfo, err)
			log.Print(lastError.Error())
		}
		stat, err := (*sftp).Client.StatVFS("/storage/emulated")
		if err != nil {
			lastError = errors.Wrap(err, "fail to get storage directory information")
			log.Print(lastError.Error())
		}
		if targetFileInfo != nil && stat != nil {
			if targetSize, freeSize := uint64(targetFileInfo.Size()), stat.FreeSpace(); targetSize+config.SpareSpace > freeSize {
				lastError = fmt.Errorf("not enough space (\n"+
					"\ttarget file size: %d\n"+
					"\tfree space: %d\n"+
					"\tspare space: %d\n)", targetSize, freeSize, config.SpareSpace)
				log.Printf(lastError.Error())
				log.Printf("retrying...")
				time.Sleep(time.Duration(config.UploadRetryDelay) * time.Second)
				continue
			}
		}

		// 파일 전송
		size, err = (*sftp).SendFile(targetPath, destPath)
		if err != nil {
			lastError = fmt.Errorf("fail to %s send file over sftp: %v", targetPath, err)
			log.Print(lastError.Error())

			errStr := errors.Cause(err).Error()
			if strings.Contains(errStr, "connection lost") ||
				strings.Contains(errStr, "no route to host") {
				// ssh client 재생성
				newSFTP, err := protocol.NewSFTPClient((*sftp).ConnInfo)
				if err != nil {
					log.Fatalf("fail to make sftp client: %v", err)
				} else {
					_ = (*sftp).Close()
					sftp = &newSFTP
				}
			}

			// 기존 파일 삭제
			if err := (*sftp).RemoveFile(destPath); err != nil {
				log.Printf("fail to remove %s remote file: %v", destPath, err)
			}
			log.Printf("retrying...")
			time.Sleep(time.Duration(config.UploadRetryDelay) * time.Second)
		} else {
			break
		}
	}

	return size, lastError
}
