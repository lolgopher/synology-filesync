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
	ResultPath    = "filelist.txt"
)

var (
	ip         string
	port       string
	localPath  string
	remotePath string

	writer    *bufio.Writer
	sumOfSize int64

	wg         sync.WaitGroup
	maxWorkers = runtime.GOMAXPROCS(0)
	sem        = semaphore.NewWeighted(int64(maxWorkers))
)

func main() {
	rootPath, _ := os.Getwd()

	// flag로 입력받을 변수 선언
	ip = *flag.String("ip", "", "FileStation IP address")
	port = *flag.String("port", "", "FileStation port")
	username := *flag.String("id", "", "FileStation account username")
	password := *flag.String("pw", "", "FileStation account password")
	localPath = *flag.String("local", rootPath, "Local path to save download files")
	remotePath = *flag.String("remote", "", "Remote path to download files")

	// 입력받은 flag 값을 parsing
	flag.Parse()

	// verify ip address
	if ip == "" {
		log.Fatal("ip address is required")
	}

	// verify port number
	if _, err := net.LookupPort("tcp", port); err != nil {
		log.Fatal("invalid port number")
	}

	// verify username and password
	if username == "" || password == "" {
		log.Fatal("username and password are required")
	}

	// verify local path
	if localPath == "" {
		log.Fatal("local path is required")
	}

	// verify remote path
	if remotePath == "" {
		log.Fatal("remote path is required")
	}

	// session id 가져오기
	sid, err := GetSessionID(ip, port, username, password)
	if err != nil {
		log.Fatalf("fail to get session id: %v", err)
	}

	// 디운로드 file list 생성
	f, err := os.Create(filepath.Join(localPath, ResultPath))
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		writer.Flush()
		f.Close()
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
		downloadRemote(sid)
	}
}

func downloadRemote(sid string) {
	stopChan := make(chan int, 1)

	sumOfSize = 0
	wg.Add(1)
	go func() {
		defer func() {
			stopChan <- 1
			wg.Done()
		}()
		searchRemoteRecursive(remotePath, sid, 0)
	}()
	go printProgress("Download...", stopChan)
	wg.Wait()

	close(stopChan)
	log.Print("Done!")
}

func searchRemoteRecursive(folderPath, sid string, depth int) error {
	fileListResp, err := GetFileList(ip, port, sid, folderPath)
	if err != nil {
		return err
	}

	for _, file := range fileListResp.Data.Files {
		writer.WriteString(strings.Repeat("\t", depth) + file.Name + "\n")

		if file.IsDir {
			os.MkdirAll(filepath.Join(localPath, file.Path), os.ModePerm)
			searchRemoteRecursive(file.Path, sid, depth+1)
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
				_, size, err := DownloadFile(ip, port, sid, filePath, filepath.Join(localPath, folderPath, fileName))
				if err != nil {
					log.Fatal(err)
				}
				atomic.AddInt64(&sumOfSize, size)
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
