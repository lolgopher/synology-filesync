package main

import (
	"bufio"
	"context"
	"flag"
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

	"golang.org/x/sync/semaphore"
)

const (
	DELAY         = 10 * time.Second
	DownloadCycle = 12 * time.Hour
	ResultPath    = "./files.txt"
)

var (
	localPath string
	synoPath  string

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
	synoPath = *flag.String("synopath", "", "FileStation path to download files")
	localPath = *flag.String("local", rootPath, "Local path to save download files")

	// 입력받은 flag 값을 parsing
	flag.Parse()

	// verify ip address
	if synoIP == "" {
		log.Fatal("ip address is required")
	}

	// verify port number
	if _, err := net.LookupPort("tcp", synoPort); err != nil {
		log.Fatal("invalid port number")
	}

	// verify username and password
	if synoUsername == "" || synoPassword == "" {
		log.Fatal("username and password are required")
	}

	// verify local path
	if localPath == "" {
		log.Fatal("local path is required")
	}

	// verify syno path
	if synoPath == "" {
		log.Fatal("filestation path is required")
	}

	// session id 가져오기
	sid, err := GetSessionID(synoIP, synoPort, synoUsername, synoPassword)
	if err != nil {
		log.Fatalf("fail to get session id: %v", err)
	}

	// 디운로드 file list 생성
	f, err := os.Create(ResultPath)
	if err != nil {
		log.Fatalf("fail to create %s file: %v", ResultPath, err)
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

	ticker := time.NewTicker(DownloadCycle)
	defer ticker.Stop()

	for ; true; <-ticker.C {
		// FileStation.List API 호출
		downloadSynology(synoIP, synoPort, sid)
		if err := writer.Flush(); err != nil {
			log.Print(err)
		}
	}
}

func downloadSynology(ip, port, sid string) {
	stopChan := make(chan int, 1)

	sumOfSize = 0
	wg.Add(1)
	go func() {
		defer func() {
			stopChan <- 1
			wg.Done()
		}()

		if err := searchSynoRecursive(ip, port, sid, synoPath, 0); err != nil {
			log.Fatalf("fail to search synology filestation: %v", err)
		}
	}()
	go printProgress("Download...", stopChan)
	wg.Wait()

	close(stopChan)
	log.Print("Done!")
}

func searchSynoRecursive(ip, port, sid, folderPath string, depth int) error {
	fileListResp, err := GetFileList(ip, port, sid, folderPath)
	if err != nil {
		return err
	}

	for _, file := range fileListResp.Data.Files {
		_, _ = writer.WriteString(strings.Repeat("\t", depth) + file.Name + "\n")

		if file.IsDir {
			if file.Name != "#recycle" {
				if err := os.MkdirAll(filepath.Join(localPath, file.Path), os.ModePerm); err != nil {
					log.Fatalf("fail to make download folder: %v", err)
				}

				if err := searchSynoRecursive(ip, port, sid, file.Path, depth+1); err != nil {
					return err
				}
			}
		} else {
			for {
				if err := sem.Acquire(context.TODO(), 1); err != nil {
					log.Printf("Failed to acquire semaphore: %v", err)
					continue
				} else {
					break
				}
			}

			filePath := file.Path
			fileName := file.Name

			wg.Add(1)
			go func() {
				defer sem.Release(1)
				defer wg.Done()
				downloadFilePath, size, err := DownloadFile(ip, port, sid, filePath, filepath.Join(localPath, folderPath, fileName))
				if err != nil {
					log.Fatalf("fail to %s download file: %v", filePath, err)
				}
				atomic.AddInt64(&sumOfSize, size)

				if err := WriteMetadata(downloadFilePath, NotSent); err != nil {
					log.Fatalf("fail to %s write metadata: %v", downloadFilePath, err)
				}
			}()
		}
	}

	return nil
}

func printProgress(title string, stop <-chan int) {
	ticker := time.NewTicker(DELAY)
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
