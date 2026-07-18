package main

import (
	"errors"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/lolgopher/synology-filesync/internal/app"
	"github.com/lolgopher/synology-filesync/protocol"
)

const mainVersionHelperEnv = "SYNology_FILESYNC_MAIN_VERSION_HELPER"

func TestMainVersion(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestMainVersionHelper$")
	cmd.Env = append(os.Environ(), mainVersionHelperEnv+"=1")

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("version subprocess failed: %v\noutput:\n%s", err, output)
	}

	timestamp := regexp.MustCompile(`^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2} `)
	lines := strings.Split(strings.TrimSuffix(string(output), "\n"), "\n")
	want := []string{
		"config: ",
		"v: true",
		"synology-filesync-unknown-unknown(unknown)",
	}
	if len(lines) != len(want) {
		t.Fatalf("version output lines = %q, want %q", lines, want)
	}
	for i := range lines {
		if !timestamp.MatchString(lines[i]) {
			t.Errorf("version output line %d = %q, want timestamp prefix", i+1, lines[i])
			continue
		}
		got := timestamp.ReplaceAllString(lines[i], "")
		if got != want[i] {
			t.Errorf("version output line %d = %q, want %q", i+1, got, want[i])
		}
	}
}

func TestMainVersionHelper(t *testing.T) {
	if os.Getenv(mainVersionHelperEnv) != "1" {
		return
	}

	os.Args = []string{os.Args[0], "-v"}
	main()
	t.Fatal("main returned after -v; want process exit")
}

func TestFormatCycleError(t *testing.T) {
	downloadErr := errors.New("download failed")
	uploadErr := errors.New("upload failed")
	initialErr := newInitialSFTPError(t, errors.New("initial failed"))

	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "download text unchanged",
			err:  &app.StageError{Stage: app.DownloadStage, Err: downloadErr},
			want: "download failed",
		},
		{
			name: "other upload error",
			err:  &app.StageError{Stage: app.UploadStage, Err: uploadErr},
			want: "fail to search local: upload failed",
		},
		{
			name: "initial sftp upload error",
			err:  &app.StageError{Stage: app.UploadStage, Err: initialErr},
			want: "fail to make sftp client: initial failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatCycleError(tt.err); got != tt.want {
				t.Errorf("formatCycleError() = %q, want %q", got, tt.want)
			}
		})
	}
}

func newInitialSFTPError(t *testing.T, cause error) error {
	t.Helper()

	uploader := app.NewUploader(app.UploadOptions{}, nil, func(*protocol.ConnectionInfo) (app.SFTPClient, error) {
		return nil, cause
	}, nil, nil)
	err := uploader.Run(&protocol.ConnectionInfo{})
	if err == nil {
		t.Fatal("Run() error = nil, want *app.InitialSFTPError")
	}
	var initialErr *app.InitialSFTPError
	if !errors.As(err, &initialErr) {
		t.Fatalf("Run() error = %T %v, want *app.InitialSFTPError", err, err)
	}
	return err
}
