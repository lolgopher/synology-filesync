package app

import (
	"bytes"
	"errors"
	"log"
	"reflect"
	"testing"

	"github.com/lolgopher/synology-filesync/protocol"
)

type runnerFunc func(*protocol.ConnectionInfo) error

func (f runnerFunc) Run(info *protocol.ConnectionInfo) error {
	return f(info)
}

func TestCoordinatorRun(t *testing.T) {
	synologyInfo := &protocol.ConnectionInfo{IP: "synology"}
	remoteInfo := &protocol.ConnectionInfo{IP: "remote"}

	tests := []struct {
		name        string
		downloadErr error
		uploadErr   error
		wantOrder   []string
		wantLog     string
		wantStage   Stage
	}{
		{
			name:      "success",
			wantOrder: []string{"download", "upload"},
			wantLog:   "Upload...\nDone!\n",
		},
		{
			name:        "download error stops before upload",
			downloadErr: errors.New("download failed"),
			wantOrder:   []string{"download"},
			wantStage:   DownloadStage,
		},
		{
			name:      "upload error",
			uploadErr: errors.New("upload failed"),
			wantOrder: []string{"download", "upload"},
			wantLog:   "Upload...\n",
			wantStage: UploadStage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var order []string
			var output bytes.Buffer
			downloader := runnerFunc(func(info *protocol.ConnectionInfo) error {
				order = append(order, "download")
				if info != synologyInfo {
					t.Errorf("download info = %p, want %p", info, synologyInfo)
				}
				return tt.downloadErr
			})
			uploader := runnerFunc(func(info *protocol.ConnectionInfo) error {
				order = append(order, "upload")
				if info != remoteInfo {
					t.Errorf("upload info = %p, want %p", info, remoteInfo)
				}
				return tt.uploadErr
			})

			err := NewCoordinator(downloader, uploader, log.New(&output, "", 0)).Run(synologyInfo, remoteInfo)

			if !reflect.DeepEqual(order, tt.wantOrder) {
				t.Errorf("order = %v, want %v", order, tt.wantOrder)
			}
			if output.String() != tt.wantLog {
				t.Errorf("log = %q, want %q", output.String(), tt.wantLog)
			}
			if tt.wantStage == "" {
				if err != nil {
					t.Fatalf("Run() error = %v", err)
				}
				return
			}

			underlying := tt.downloadErr
			if underlying == nil {
				underlying = tt.uploadErr
			}
			if !errors.Is(err, underlying) {
				t.Errorf("errors.Is(%v, %v) = false", err, underlying)
			}
			var stageErr *StageError
			if !errors.As(err, &stageErr) {
				t.Fatalf("errors.As(%v, *StageError) = false", err)
			}
			if stageErr.Stage != tt.wantStage {
				t.Errorf("stage = %q, want %q", stageErr.Stage, tt.wantStage)
			}
			if stageErr.Error() != underlying.Error() {
				t.Errorf("error text = %q, want %q", stageErr.Error(), underlying.Error())
			}
		})
	}
}
