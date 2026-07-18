package app

import "github.com/lolgopher/synology-filesync/protocol"

type Stage string

const (
	DownloadStage Stage = "download"
	UploadStage   Stage = "upload"
)

type StageError struct {
	Stage Stage
	Err   error
}

func (e *StageError) Error() string {
	return e.Err.Error()
}

func (e *StageError) Unwrap() error {
	return e.Err
}

type Coordinator struct {
	downloader DownloadRunner
	uploader   UploadRunner
	logger     Logger
}

func NewCoordinator(downloader DownloadRunner, uploader UploadRunner, logger Logger) *Coordinator {
	return &Coordinator{
		downloader: downloader,
		uploader:   uploader,
		logger:     logger,
	}
}

func (c *Coordinator) Run(synologyInfo, remoteInfo *protocol.ConnectionInfo) error {
	if err := c.downloader.Run(synologyInfo); err != nil {
		return &StageError{Stage: DownloadStage, Err: err}
	}

	c.logger.Print("Upload...")
	if err := c.uploader.Run(remoteInfo); err != nil {
		return &StageError{Stage: UploadStage, Err: err}
	}
	c.logger.Print("Done!")

	return nil
}
