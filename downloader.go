package main

import (
	"log"

	"github.com/lolgopher/synology-filesync/internal/app"
	metadatainfra "github.com/lolgopher/synology-filesync/internal/infra/metadata"
	synologyinfra "github.com/lolgopher/synology-filesync/internal/infra/synology"
	"github.com/lolgopher/synology-filesync/protocol"
)

func downloadSynology(info *protocol.ConnectionInfo) {
	store := metadatainfra.NewStore()
	factory := app.SynologyFactory(func(info *protocol.ConnectionInfo) (app.SynologyClient, error) {
		return synologyinfra.NewClient(info)
	})

	downloader := app.NewDownloader(app.DownloadOptions{
		RootRemotePath:   config.Synology.Path,
		LocalPath:        config.LocalPath,
		MetadataFilename: config.YAML.Filename,
		WorkerLimit:      int64(config.DownloadWorker),
		SynologyFactory:  factory,
		MetadataStore:    store,
		Logger:           log.Default(),
	})

	if err := downloader.Run(info); err != nil {
		log.Fatal(err)
	}
}
