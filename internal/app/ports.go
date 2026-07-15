package app

import (
	"time"

	"github.com/lolgopher/synology-filesync/protocol"
)

type MetadataStore interface {
	Read(folderPath, filename string) (map[string]protocol.FileMetadata, error)
	Write(filePath, filename string, size uint64, status protocol.FileTransferStatus) error
	Exists(path string) bool
}

type SynologyClient interface {
	GetFileList(folderPath string) (*protocol.FileListResponse, error)
	DownloadFile(filePath, destPath string) (string, int64, error)
}

type SynologyFactory func(*protocol.ConnectionInfo) (SynologyClient, error)

type SFTPClient interface {
	SendFile(localFilePath, remoteFilePath string) (int, error)
	RemoveFile(targetFilePath string) error
	FreeSpace(path string) (uint64, error)
	Close() error
}

type SFTPFactory func(*protocol.ConnectionInfo) (SFTPClient, error)

type Logger interface {
	Print(v ...any)
	Printf(format string, v ...any)
}

type SleepFunc func(time.Duration)

type DownloadRunner interface {
	Run(*protocol.ConnectionInfo) error
}

type UploadRunner interface {
	Run(*protocol.ConnectionInfo) error
}
