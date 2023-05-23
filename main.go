package main

import (
	"bufio"
	"errors"
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

	spareSpace    = 1073741824 // 1GByte
	retryCount    = 10
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

	// print flag value
	log.Printf("synoip: %v", synoIP)
	log.Printf("synoport: %v", synoPort)
	log.Printf("synoid: %v", synoUsername)
	log.Printf("synopw: %v", synoPassword)
	log.Printf("synopath: %v", synoPath)

	log.Printf("remoteip: %v", remoteIP)
	log.Printf("remoteport: %v", remotePort)
	log.Printf("remoteid: %v", remoteUsername)
	log.Printf("remotepw: %v", remotePassword)
	log.Printf("remotepath: %v", remotePath)

	log.Printf("localpath: %v", localPath)
	log.Printf("v: %v", flagVer)

	// print version
	if flagVer {
		log.Printf("%s-%s", programName, programVer)
		os.Exit(0)
	}

	log.Printf("%s start (version: %s)", programName, programVer)
	if err := verifyFlag(synoIP, remoteIP, synoPort, remotePort, synoUsername, remoteUsername, synoPassword, remotePassword); err != nil {
		log.Fatalf("fail to verify flag: %v", err)
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

	// 연결 정보 설정
	synologyInfo := &protocol.ConnectionInfo{
		IP:       synoIP,
		Port:     synoPort,
		Username: synoUsername,
		Password: synoPassword,
	}
	remoteInfo := &protocol.ConnectionInfo{
		IP:       remoteIP,
		Port:     remotePort,
		Username: remoteUsername,
		Password: remotePassword,
	}

	for ; true; <-ticker.C {
		// FileStation.List API 호출
		downloadSynology(synologyInfo)

		// 파일 전송
		uploadRemote(remoteInfo)
	}
}

func verifyFlag(
	synoIP, remoteIP,
	synoPort, remotePort,
	synoUsername, remoteUsername,
	synoPassword, remotePassword string) error {

	// verify ip address
	if synoIP == "" || remoteIP == "" {
		return errors.New("ip address is required")
	}

	// verify port number
	if synoPort == "" || remotePort == "" {
		return errors.New("port is required")
	}
	if _, err := net.LookupPort("tcp", synoPort); err != nil {
		return errors.New("invalid synology port number")
	}
	if _, err := net.LookupPort("tcp", remotePort); err != nil {
		return errors.New("invalid remote port number")
	}

	// verify username and password
	if synoUsername == "" || synoPassword == "" {
		return errors.New("synology username and password are required")
	}
	if remoteUsername == "" || remotePassword == "" {
		return errors.New("remote username and password are required")
	}

	// verify path
	if synoPath == "" {
		return errors.New("filestation path is required")
	}
	if remotePath == "" {
		return errors.New("remote path is required")
	}
	if localPath == "" {
		return errors.New("local path is required")
	}

	return nil
}
