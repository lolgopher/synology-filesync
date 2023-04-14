package main

import (
	"bufio"
	"flag"
	"fmt"
	"github.com/lolgopher/synology-filesync/protocol"
	"log"
	"net"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"

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

	var (
		synoIP, remoteIP             string
		synoPort, remotePort         string
		synoUsername, remoteUsername string
		synoPassword, remotePassword string
		flagVer                      bool
	)

	// flag로 입력받을 변수 선언
	flag.StringVar(&synoIP, "synoip", "", "FileStation IP address")
	flag.StringVar(&synoPort, "synoport", "", "FileStation port")
	flag.StringVar(&synoUsername, "synoid", "", "FileStation account username")
	flag.StringVar(&synoPassword, "synopw", "", "FileStation account password")
	flag.StringVar(&synoPath, "synopath", "", "FileStation path to download files")

	flag.StringVar(&remoteIP, "remoteip", "", "Remote SSH IP address")
	flag.StringVar(&remotePort, "remoteport", "", "Remote SSH port")
	flag.StringVar(&remoteUsername, "remoteid", "", "Remote SSH username")
	flag.StringVar(&remotePassword, "remotepw", "", "Remote SSH password")
	flag.StringVar(&remotePath, "remotepath", "", "Remote path to download files")

	flag.StringVar(&localPath, "localpath", rootPath, "Local path to save download files")
	flag.BoolVar(&flagVer, "v", false, "Show version")

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
	sid, err := protocol.GetSessionID(synoIP, synoPort, synoUsername, synoPassword)
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
