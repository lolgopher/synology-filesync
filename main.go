package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lolgopher/synology-filesync/internal/app"
	internalconfig "github.com/lolgopher/synology-filesync/internal/config"
	metadatainfra "github.com/lolgopher/synology-filesync/internal/infra/metadata"
	sftpinfra "github.com/lolgopher/synology-filesync/internal/infra/sftp"
	synologyinfra "github.com/lolgopher/synology-filesync/internal/infra/synology"
	"github.com/lolgopher/synology-filesync/protocol"
)

const programName = "synology-filesync"

var (
	buildTag   = "unknown"
	gitHash    = "unknown"
	buildStamp = "unknown"
	programVer = fmt.Sprintf("%s-%s(%s)", buildTag, gitHash, buildStamp)
)

func main() {
	var configPath string
	var flagVer bool
	flag.StringVar(&configPath, "config", "", "Config file path")
	flag.BoolVar(&flagVer, "v", false, "Show version")
	flag.Parse()

	log.Printf("config: %v", configPath)
	log.Printf("v: %v", flagVer)
	if flagVer {
		log.Printf("%s-%s", programName, programVer)
		os.Exit(0)
	}
	log.Printf("%s start (version: %s)", programName, programVer)

	config, err := internalconfig.Load(configPath)
	if err != nil {
		log.Fatalf("fail to init config: %v", err)
	}

	store := metadatainfra.NewStore()
	synologyFactory := app.SynologyFactory(func(info *protocol.ConnectionInfo) (app.SynologyClient, error) {
		return synologyinfra.NewClient(info)
	})
	sftpFactory := app.SFTPFactory(func(info *protocol.ConnectionInfo) (app.SFTPClient, error) {
		return sftpinfra.NewClient(info)
	})
	downloader := app.NewDownloader(app.DownloadOptions{
		RootRemotePath:   config.Synology.Path,
		LocalPath:        config.LocalPath,
		ExcludePaths:     config.ExcludePaths,
		MetadataFilename: config.YAML.Filename,
		WorkerLimit:      int64(config.DownloadWorker),
		SynologyFactory:  synologyFactory,
		MetadataStore:    store,
		Logger:           log.Default(),
		WorkerErrorHandler: func(err error) {
			log.Fatal(err)
		},
	})
	uploader := app.NewUploader(app.UploadOptions{
		LocalPath:        config.LocalPath,
		ExcludePaths:     config.ExcludePaths,
		SynologyPath:     config.Synology.Path,
		SSHPath:          config.SSH.Path,
		YAMLFilename:     config.YAML.Filename,
		SpareSpace:       config.SpareSpace,
		UploadDelay:      time.Duration(config.UploadDelay) * time.Second,
		UploadRetryDelay: time.Duration(config.UploadRetryDelay) * time.Second,
		UploadRetryCount: config.UploadRetryCount,
	}, store, sftpFactory, log.Default(), time.Sleep)
	coordinator := app.NewCoordinator(downloader, uploader, log.Default())

	synologyInfo := &protocol.ConnectionInfo{
		IP:       config.Synology.IP,
		Port:     config.Synology.Port,
		Username: config.Synology.Username,
		Password: config.Synology.Password,
	}
	remoteInfo := &protocol.ConnectionInfo{
		IP:       config.SSH.IP,
		Port:     config.SSH.Port,
		Username: config.SSH.Username,
		Password: config.SSH.Password,
	}

	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		<-c
		log.Print("got terminated signal")
		os.Exit(0)
	}()

	ticker := time.NewTicker(time.Duration(config.SyncCycle) * time.Hour)
	defer ticker.Stop()
	for ; true; <-ticker.C {
		if err := coordinator.Run(synologyInfo, remoteInfo); err != nil {
			log.Fatal(formatCycleError(err))
		}
	}
}

func formatCycleError(err error) string {
	var stageErr *app.StageError
	if !errors.As(err, &stageErr) || stageErr.Stage == app.DownloadStage {
		return err.Error()
	}

	var initialErr *app.InitialSFTPError
	if errors.As(err, &initialErr) {
		return fmt.Sprintf("fail to make srtp client: %v", err)
	}
	var reconnectErr *app.ReconnectSFTPError
	if errors.As(err, &reconnectErr) {
		return fmt.Sprintf("fail to make sftp client: %v", err)
	}
	return fmt.Sprintf("fail to search local: %v", err)
}
