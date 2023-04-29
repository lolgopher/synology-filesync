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
	sumOfSize = 0
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
				log.Fatalf("fail to close sftp client: %v", err)
			}
		}()

		if err := searchLocal(client, filepath.Join(localPath, synoPath)); err != nil {
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
			targetMetadata, err := ReadMetadata(filepath.Dir(targetPath))
			if err != nil {
				return err
			}

			metadata, ok := targetMetadata[targetPath]
			if !ok {
				return fmt.Errorf("fail to find %s in metadata", targetPath)
			}

			switch FileTransferStatus(metadata.Status) {
			case Init:
				log.Printf("%s is init metadata status", targetPath)
				return nil
			case Sent:
				log.Printf("%s has already been sent", targetPath)
				return nil
			case Failed:
				log.Printf("%s sent failed", targetPath)
				return nil
			case NotSent:
				size := 0
				for i := 0; i < retryCount; i++ {
					destPath, _ := strings.CutPrefix(targetPath, localPath)
					destPath = filepath.Join(remotePath, destPath)

					// 파일 전송
					size, err = sftp.SendFile(targetPath, destPath)
					if err != nil {
						log.Printf("fail to %s send file over sftp: %v", targetPath, err)

						errStr := errors.Cause(err).Error()
						if strings.Contains(errStr, "connection lost") {
							// ssh client 재생성
							sftp, err = protocol.NewSFTPClient(sftp.ConnInfo)
							if err != nil {
								log.Fatalf("fail to make srtp client: %v", err)
							}
						} else if strings.Contains(errStr, "already exist") {
							// 기존 파일 삭제
							if err := sftp.RemoveFile(destPath); err != nil {
								log.Printf("fail to remove %s remote file: %v", destPath, err)
							}
						}

						log.Printf("retrying...")
						time.Sleep(delay / 5)
					} else {
						log.Printf("%s: %d", targetPath, size)
						break
					}
				}

				var result FileTransferStatus
				if size > 0 {
					// 전송에 성공했을때
					result = Sent
				} else {
					// 전송에 실패했을때
					result = Failed
				}
				if err := WriteMetadata(targetPath, 0, result); err != nil {
					return err
				}
				time.Sleep(delay)
			default:
				log.Printf("%s is unknown status", metadata.Status)
				return nil
			}
		}

		return nil
	})

	return err
}
