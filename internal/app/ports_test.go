package app

import (
	"time"

	"github.com/lolgopher/synology-filesync/protocol"
)

type fakeMetadataStore struct{}

func (*fakeMetadataStore) Read(string, string) (map[string]protocol.FileMetadata, error) {
	return nil, nil
}

func (*fakeMetadataStore) Write(string, string, uint64, protocol.FileTransferStatus) error {
	return nil
}

func (*fakeMetadataStore) Exists(string) bool {
	return false
}

type fakeSynologyClient struct{}

func (*fakeSynologyClient) GetFileList(string) (*protocol.FileListResponse, error) {
	return nil, nil
}

func (*fakeSynologyClient) DownloadFile(string, string) (string, int64, error) {
	return "", 0, nil
}

func fakeSynologyFactory(*protocol.ConnectionInfo) (SynologyClient, error) {
	return &fakeSynologyClient{}, nil
}

type fakeSFTPClient struct{}

func (*fakeSFTPClient) SendFile(string, string) (int, error) {
	return 0, nil
}

func (*fakeSFTPClient) RemoveFile(string) error {
	return nil
}

func (*fakeSFTPClient) FreeSpace(string) (uint64, error) {
	return 0, nil
}

func (*fakeSFTPClient) Close() error {
	return nil
}

func fakeSFTPFactory(*protocol.ConnectionInfo) (SFTPClient, error) {
	return &fakeSFTPClient{}, nil
}

type fakeLogger struct{}

func (*fakeLogger) Print(...any) {}

func (*fakeLogger) Printf(string, ...any) {}

func fakeSleep(time.Duration) {}

type fakeDownloadRunner struct{}

func (*fakeDownloadRunner) Run(*protocol.ConnectionInfo) error {
	return nil
}

type fakeUploadRunner struct{}

func (*fakeUploadRunner) Run(*protocol.ConnectionInfo) error {
	return nil
}

var (
	_ MetadataStore   = (*fakeMetadataStore)(nil)
	_ SynologyClient  = (*fakeSynologyClient)(nil)
	_ SynologyFactory = fakeSynologyFactory
	_ SFTPClient      = (*fakeSFTPClient)(nil)
	_ SFTPFactory     = fakeSFTPFactory
	_ Logger          = (*fakeLogger)(nil)
	_ SleepFunc       = fakeSleep
	_ DownloadRunner  = (*fakeDownloadRunner)(nil)
	_ UploadRunner    = (*fakeUploadRunner)(nil)
)
