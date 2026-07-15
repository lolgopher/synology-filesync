package main

import (
	"errors"
	"log"
	"time"

	"github.com/lolgopher/synology-filesync/internal/app"
	metadatainfra "github.com/lolgopher/synology-filesync/internal/infra/metadata"
	sftpinfra "github.com/lolgopher/synology-filesync/internal/infra/sftp"
	"github.com/lolgopher/synology-filesync/protocol"
)

func uploadRemote(info *protocol.ConnectionInfo) {
	store := metadatainfra.NewStore()
	factory := app.SFTPFactory(func(info *protocol.ConnectionInfo) (app.SFTPClient, error) {
		return sftpinfra.NewClient(info)
	})
	uploader := app.NewUploader(app.UploadOptions{
		LocalPath:        config.LocalPath,
		SynologyPath:     config.Synology.Path,
		SSHPath:          config.SSH.Path,
		YAMLFilename:     config.YAML.Filename,
		SpareSpace:       config.SpareSpace,
		UploadDelay:      time.Duration(config.UploadDelay) * time.Second,
		UploadRetryDelay: time.Duration(config.UploadRetryDelay) * time.Second,
		UploadRetryCount: config.UploadRetryCount,
	}, store, factory, log.Default(), time.Sleep)

	wg.Add(1)
	go func() {
		defer wg.Done()

		if err := uploader.Run(info); err != nil {
			var initialErr *app.InitialSFTPError
			if errors.As(err, &initialErr) {
				log.Fatalf("fail to make srtp client: %v", err)
			}

			var reconnectErr *app.ReconnectSFTPError
			if errors.As(err, &reconnectErr) {
				log.Fatalf("fail to make sftp client: %v", err)
			}

			log.Fatalf("fail to search local: %v", err)
		}
	}()
	log.Print("Upload...")
	wg.Wait()
	log.Print("Done!")
}
