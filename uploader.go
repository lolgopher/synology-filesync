package main

import (
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

			if metadata, ok := targetMetadata[targetPath]; ok && metadata.Status == string(Sent) {
				log.Printf("%s has already been sent", targetPath)
				return nil
			}

			size := 0
			for {
				destPath, _ := strings.CutPrefix(targetPath, localPath)
				destPath = filepath.Join(remotePath, destPath)

				// 파일 전송
				size, err = sftp.SendFile(targetPath, destPath)
				if err != nil {
					log.Printf("fail to %s send file over sftp: %v", targetPath, err)
					log.Printf("retrying...")
					time.Sleep(delay / 5)

					if errors.Cause(err).Error() == "connection lost" {
						// ssh client 재생성
						sftp, err = protocol.NewSFTPClient(sftp.ConnInfo)
						if err != nil {
							log.Fatalf("fail to make srtp client: %v", err)
						}
					}

					continue
				} else {
					log.Printf("%s: %d", targetPath, size)
					break
				}
			}

			if size > 0 {
				// 전송에 성공했다면 메타데이터 파일 업데이트
				if err := WriteMetadata(targetPath, 0, Sent); err != nil {
					return err
				}
				time.Sleep(delay)
			}
		}

		return nil
	})

	return err
}
