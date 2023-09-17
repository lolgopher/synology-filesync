package main

import (
	"flag"
	"fmt"
	"github.com/lolgopher/synology-filesync/protocol"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sync/semaphore"
)

const programName = "synology-filesync"

var (
	buildTag   = "unknown"
	gitHash    = "unknown"
	buildStamp = "unknown"
	programVer = fmt.Sprintf("%s-%s(%s)", buildTag, gitHash, buildStamp)

	config *Config
	wg     sync.WaitGroup
	sem    *semaphore.Weighted
)

func main() {
	var configPath string
	var flagVer bool

	// flag로 입력받을 변수 선언
	flag.StringVar(&configPath, "config", "", "Config file path")
	flag.BoolVar(&flagVer, "v", false, "Show version")

	// 입력받은 flag 값을 parsing
	flag.Parse()

	// print flag value
	log.Printf("config: %v", configPath)
	log.Printf("v: %v", flagVer)

	// print version
	if flagVer {
		log.Printf("%s-%s", programName, programVer)
		os.Exit(0)
	}
	log.Printf("%s start (version: %s)", programName, programVer)

	// config init
	var err error
	config, err = initConfig(configPath)
	if err != nil {
		log.Fatalf("fail to init config: %v", err)
	}
	sem = semaphore.NewWeighted(int64(config.DownloadWorker))

	// Interrupt Signal 받기
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)

		// Ctrl+C
		<-c
		log.Print("got terminated signal")
		os.Exit(0)
	}()

	ticker := time.NewTicker(time.Duration(config.SyncCycle) * time.Hour)
	defer ticker.Stop()

	// 연결 정보 설정
	synologyInfo := &protocol.ConnectionInfo{
		IP:       config.Synology.IP,
		Port:     config.Synology.Port,
		Username: config.Synology.Username,
		Password: config.Synology.Password,
	}
	remoteInfo := &protocol.ConnectionInfo{
		IP:       config.Remote.IP,
		Port:     config.Remote.Port,
		Username: config.Remote.Username,
		Password: config.Remote.Password,
	}

	for ; true; <-ticker.C {
		// FileStation.List API 호출
		downloadSynology(synologyInfo)

		// 파일 전송
		uploadRemote(remoteInfo)
	}
}
