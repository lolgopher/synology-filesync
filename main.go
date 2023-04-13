package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/sync/semaphore"
)

const (
	programName = "synology-filesync"

	delay         = 10 * time.Second
	downloadCycle = 12 * time.Hour
	resultPath    = "./synology_files.txt"
)

var (
	buildTag   = "unknown"
	gitHash    = "unknown"
	buildStamp = "unknown"
	programVer = fmt.Sprintf("%s-%s(%s)", buildTag, gitHash, buildStamp)

	synoPath   string
	remotePath string
	localPath  string

	writer    *bufio.Writer
	sumOfSize int64

	wg         sync.WaitGroup
	maxWorkers = runtime.GOMAXPROCS(0)
	sem        = semaphore.NewWeighted(int64(maxWorkers))
)

func main() {
	rootPath, _ := os.Getwd()

	// flag로 입력받을 변수 선언
	synoIP := *flag.String("synoip", "", "FileStation IP address")
	synoPort := *flag.String("synoport", "", "FileStation port")
	synoUsername := *flag.String("synoid", "", "FileStation account username")
	synoPassword := *flag.String("synopw", "", "FileStation account password")
	flag.StringVar(&synoPath, "synopath", "", "FileStation path to download files")

	remoteIP := *flag.String("remoteip", "", "Remote SSH IP address")
	remotePort := *flag.String("remoteport", "", "Remote SSH port")
	remoteUsername := *flag.String("remoteid", "", "Remote SSH username")
	remotePassword := *flag.String("remotepw", "", "Remote SSH password")
	flag.StringVar(&remotePath, "remotepath", "", "Remote path to download files")

	flag.StringVar(&localPath, "localpath", rootPath, "Local path to save download files")
	flagVer := *flag.Bool("v", false, "Show version")

	// 입력받은 flag 값을 parsing
	flag.Parse()

	// print version
	if flagVer {
		log.Printf("%s-%s", programName, programVer)
		os.Exit(0)
	}
	log.Printf("%s start (version: %s)", programName, programVer)

	// verify ip address
	if synoIP == "" || remoteIP == "" {
		log.Fatal("ip address is required")
	}

	// verify port number
	if synoPort == "" || remotePort == "" {
		log.Fatal("port is required")
	}
	if _, err := net.LookupPort("tcp", synoPort); err != nil {
		log.Fatal("invalid synology port number")
	}
	if _, err := net.LookupPort("tcp", remotePort); err != nil {
		log.Fatal("invalid remote port number")
	}

	// verify username and password
	if synoUsername == "" || synoPassword == "" {
		log.Fatal("synology username and password are required")
	}
	if remoteUsername == "" || remotePassword == "" {
		log.Fatal("remote username and password are required")
	}

	// verify path
	if synoPath == "" {
		log.Fatal("filestation path is required")
	}
	if remotePath == "" {
		log.Fatal("remote path is required")
	}
	if localPath == "" {
		log.Fatal("local path is required")
	}

	// session id 가져오기
	sid, err := GetSessionID(synoIP, synoPort, synoUsername, synoPassword)
	if err != nil {
		log.Fatalf("fail to get session id: %v", err)
	}

	// 디운로드 file list 생성
	f, err := os.Create(resultPath)
	if err != nil {
		log.Fatalf("fail to create %s file: %v", resultPath, err)
	}
	defer func() {
		if err := writer.Flush(); err != nil {
			log.Print(err)
		}

		if err := f.Close(); err != nil {
			log.Print(err)
		}
	}()
	writer = bufio.NewWriter(f)

	// Interrupt Signal 받기
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)

		// Ctrl+C
		<-c
		log.Print("got terminated signal")
		os.Exit(0)
	}()

	ticker := time.NewTicker(downloadCycle)
	defer ticker.Stop()

	for ; true; <-ticker.C {
		// FileStation.List API 호출
		downloadSynology(synoIP, synoPort, sid)

		// 파일 전송
		uploadRemote(remoteIP, remotePort, remoteUsername, remotePassword)
	}
}

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

func searchSynologyRecursive(ip, port, sid, folderPath string, depth int) (*FileListResponse, error) {
	fileListResp, err := GetFileList(ip, port, sid, folderPath)
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
			if !FileExists(filepath.Join(filepath.Dir(initFilePath), "metadata.yaml")) {
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
					if FileExists(initFilePath) {
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

func downloadSynologyRecursive(ip, port, sid string, fileList *FileListResponse) error {
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

				downloadFilePath, size, err := DownloadFile(ip, port, sid, filePath, targetPath)
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

func uploadRemote(ip, port, username, password string) {
	sumOfSize = 0
	wg.Add(1)
	go func() {
		defer func() {
			wg.Done()
		}()

		// ssh client 생성
		client, err := NewSFTPClient(ip, port, username, password)
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

func searchLocal(client *sftp.Client, folderPath string) error {
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
				// 파일 전송
				size, err = SendFileOverSFTP(client, targetPath, filepath.Join(remotePath, strings.ReplaceAll(targetPath, localPath, "")))
				if err != nil {
					log.Printf("fail to %s send file over sftp: %v", targetPath, err)
					log.Printf("retrying...")
					time.Sleep(delay / 5)
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
