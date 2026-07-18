package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/lolgopher/synology-filesync/internal/app"
	internalconfig "github.com/lolgopher/synology-filesync/internal/config"
	"github.com/lolgopher/synology-filesync/protocol"
)

const (
	mainVersionHelperEnv        = "SYNology_FILESYNC_MAIN_VERSION_HELPER"
	mainRuntimeSupportHelperEnv = "SYNology_FILESYNC_MAIN_RUNTIME_SUPPORT_HELPER"
	mainRuntimeSupportConfigEnv = "SYNology_FILESYNC_MAIN_RUNTIME_SUPPORT_CONFIG"
)

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

func TestValidateRuntimeSupport(t *testing.T) {
	tests := []struct {
		name   string
		config *internalconfig.Config
		want   string
	}{
		{
			name: "supported",
			config: &internalconfig.Config{
				DownloadType: "synology",
				UploadType:   "ssh",
				DBType:       "yaml",
			},
		},
		{
			name: "disabled download checked first",
			config: &internalconfig.Config{
				DownloadType: "disabled",
				UploadType:   "skip",
				DBType:       "json",
			},
			want: `download type "disabled" is not supported`,
		},
		{
			name: "skip upload checked before db",
			config: &internalconfig.Config{
				DownloadType: "synology",
				UploadType:   "skip",
				DBType:       "json",
			},
			want: `upload type "skip" is not supported`,
		},
		{
			name: "other upload",
			config: &internalconfig.Config{
				DownloadType: "synology",
				UploadType:   "other",
				DBType:       "yaml",
			},
			want: `upload type "other" is not supported`,
		},
		{
			name: "json db",
			config: &internalconfig.Config{
				DownloadType: "synology",
				UploadType:   "ssh",
				DBType:       "json",
			},
			want: `db type "json" is not supported`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRuntimeSupport(tt.config)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("validateRuntimeSupport() error = %v, want nil", err)
				}
				return
			}
			if err == nil || err.Error() != tt.want {
				t.Fatalf("validateRuntimeSupport() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestMainRejectsUnsupportedRuntimeBeforeDereference(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	config := []byte("download_type: disabled\nupload_type: disabled\ndb_type: disabled\nlocal_path: /tmp\n")
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestMainRuntimeSupportHelper$")
	cmd.Env = append(os.Environ(),
		mainRuntimeSupportHelperEnv+"=1",
		mainRuntimeSupportConfigEnv+"="+configPath,
	)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("runtime support subprocess succeeded; want failure\noutput:\n%s", output)
	}

	got := string(output)
	want := `fail to init runtime: download type "disabled" is not supported`
	if !strings.Contains(got, want) {
		t.Errorf("runtime support output = %q, want substring %q", got, want)
	}
	lower := strings.ToLower(got)
	for _, unwanted := range []string{"panic", "nil pointer"} {
		if strings.Contains(lower, unwanted) {
			t.Errorf("runtime support output = %q, must not contain %q", got, unwanted)
		}
	}
}

func TestMainRuntimeSupportHelper(t *testing.T) {
	if os.Getenv(mainRuntimeSupportHelperEnv) != "1" {
		return
	}

	os.Args = []string{os.Args[0], "-config", os.Getenv(mainRuntimeSupportConfigEnv)}
	main()
	t.Fatal("main returned for unsupported runtime; want process exit")
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
